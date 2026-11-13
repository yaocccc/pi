import { fileURLToPath } from "node:url";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Type } from "typebox";
import { Check } from "typebox/value";
import { MODES, PRESETS, SESSION_MODES, agentDir, loadRoutingConfig, supportedThinking, validateTask } from "./config.ts";
import { WORKER_USAGE_EVENT, type WorkerUsageSnapshot } from "./events.ts";
import { registerWorkerWriteGuard } from "./guard.ts";
import { captureWorkerSession, type WorkerSessionSnapshot } from "./session.ts";
import { beginWorkerShutdown, createWorkerOwner, executeTask } from "./process.ts";
import { workerTaskScopesConflict } from "./security.ts";
import { saveWorkerSettings } from "./settings.ts";
import { registerCapability } from "./capabilities.ts";
import { WorkerTranscriptReader } from "./transcript-reader.ts";
import { notifyFrontendEvent } from "./frontend.ts";
import { WORKER_CAPABILITY, type WorkerCapability, type WorkerSessionView, type WorkerStateChange } from "./api.ts";
import type { WorkerToolInput, WorkerUiDetails } from "./types";
import { cloneUiDetails, compactWorkerResult, emptyWorkerUsage, sanitizeStructuredValue, serializePayload } from "./data.ts";
import { WorkerRuntime, type Batch } from "./runtime.ts";
import { present } from "./callbacks.ts";
import { workerBranchSnapshots, WORKER_SNAPSHOT_ENTRY } from "./history.ts";

export const TOOL_DESCRIPTION = `Autonomously delegate bounded, verifiable tasks: fast for clear local work, normal for ordinary work, deep for difficult work. Every task requires explicit preset and sessionMode. Read worker-orchestration for non-trivial delegation. Choose each task's sessionMode yourself: prefer fork; use new only when little context handoff is needed; do not ask the user. Workers cannot delegate. Handle failures without automatic retries; verify diffs and acceptance evidence yourself.
ONE unfinished batch per session; parallel tasks go in ONE tasks array. Start: tasks array, even for one task. Continue: {batchId}. Cancel: {batchId,cancel:true}. Do not mix modes. A second start returns activeBatchId; finish or cancel it first.
Start/continue waits for completion; no polling. Finish or cancel all batches before ending your turn. Do not edit locked paths. Missing information/authorization returns blocked; handle in the main agent, then create a new bounded task if needed. Never infer authorization. Deadlines are fixed. Cancel/abort of an active call waits for cleanup and lock release; abort after return has no effect. Scope changes require cancellation, cleanup and a new task.`;

export const TaskSchema = Type.Object({
	mode: StringEnum(MODES),
	title: Type.Optional(Type.String({ description: "One-sentence UI task label; aim for 10–30 characters. Write it yourself, not by truncating objective." })),
	objective: Type.String({ minLength: 1 }),
	preset: StringEnum(PRESETS),
	sessionMode: StringEnum(SESSION_MODES, { description: "Choose explicitly: fork snapshots the current branch at batch start; new is for minimal context handoff." }),
	context: Type.Optional(Type.String()),
	relevantFiles: Type.Optional(Type.Array(Type.String({ description: "Read hints, including outside cwd; no write permission." }))),
	allowedPaths: Type.Optional(Type.Array(Type.String({ description: "Write modes require scope relative to cwd: exact/new files or globs; directory children use dir/**, not dir or dir/. Guards edit/write only, not a sandbox." }))),
	forbiddenPaths: Type.Optional(Type.Array(Type.String({ description: "Write exclusions relative to cwd: files or globs; directory children use dir/**, not dir or dir/." }))),
	acceptanceCriteria: Type.Optional(Type.Array(Type.String())),
	verificationCommands: Type.Optional(Type.Array(Type.String())),
	outputRequirements: Type.Optional(Type.Array(Type.String())),
	cwd: Type.Optional(Type.String({ description: "Subdirectory relative to main agent cwd; no absolute paths or escapes." })),
}, { additionalProperties: false });

export const InputSchema = Type.Object({
	tasks: Type.Optional(Type.Array(TaskSchema, { minItems: 1, maxItems: 12 })),
	batchId: Type.Optional(Type.String({ minLength: 1 })),
	cancel: Type.Optional(Type.Literal(true)),
}, { additionalProperties: false });

/** Validate at execution too: direct callers must not bypass schema or mode constraints. */
export function validateWorkerInput(input: unknown): asserts input is WorkerToolInput {
	if (!Check(InputSchema, input)) throw new Error("worker 参数错误：字段、类型或数量无效（不接受未知字段）");
	const value = input as WorkerToolInput;
	if (value.batchId !== undefined) {
		if (!value.batchId.trim() || "tasks" in value) throw new Error("worker 参数错误：batchId 不能与 tasks 混用");
	} else {
		if (!value.tasks) throw new Error("worker 参数错误：启动必须提供 tasks 数组");
		if ("cancel" in value) throw new Error("worker 参数错误：cancel 必须提供 batchId");
	}
}

const respond = <T>(text: string, details: T) => ({ content: [{ type: "text" as const, text }], details });

/** One sanitized, detached view shared by live rows and serialized tool results. */
export function batchUiSnapshot(batch: Batch): WorkerUiDetails {
	const details = sanitizeStructuredValue(cloneUiDetails(batch.ui)) as WorkerUiDetails;
	details.batchId = batch.id;
	// Opaque Pi routing IDs are metadata, not user text: never redact or truncate them.
	details.originToolCallId = batch.ui.originToolCallId;
	for (let i = 0; i < details.tasks.length; i++) {
		const transcript = batch.ui.tasks[i].transcript;
		if (transcript) details.tasks[i].transcript = { ...transcript };
	}
	details.controlErrors = batch.ui.controlErrors?.map((error, index) => ({
		toolCallId: error.toolCallId, message: details.controlErrors![index].message,
	}));
	details.snapshotAt = Date.now();
	return details;
}

/** Keep IDs/control state outside result compression so truncation cannot strand a batch. */
export function batchResponse(batch: Batch) {
	const results = batch.tasks.map((task) => task.result ?? { status: task.state });
	const resultPayload = { status: results.every((item) => item.status === "completed") ? "completed" : "partial", results };
	const compressed = serializePayload(resultPayload, batch.maxOutputBytes);
	const payload = {
		batchId: batch.id,
		status: batch.finished ? resultPayload.status : batch.cancelled ? "cancelling" : "running",
		finished: batch.finished,
		tasks: batch.tasks.map((task) => ({ taskId: task.id, status: task.state === "finished" ? task.result?.status : task.state })),
		result: compressed.payload,
		next_action: batch.finished ? "Review results and actual diff." : "Call worker({batchId}) to await completion, or worker({batchId, cancel:true}) to cancel and clean up. Active tasks retain path locks.",
	};
	const details = batchUiSnapshot(batch);
	details.payload = payload;
	return respond(JSON.stringify(payload), details);
}

export default function workerExtension(pi: ExtensionAPI, services = { executeTask, loadRoutingConfig }) {
	if (Number(process.env.PI_WORKER_DEPTH || "0") >= 1) {
		registerWorkerWriteGuard(pi);
		return;
	}
	pi.on("resources_discover", () => ({
		skillPaths: [fileURLToPath(new URL("./SKILL.md", import.meta.url))],
	}));
	let stopped = false;
	const createSession = (branch: readonly any[] = [], awaitingHistory = false) => {
		const history = workerBranchSnapshots(branch);
		const persisted = new Map<string, string>();
		const listeners = new Set<(change: WorkerStateChange) => void>();
		let closed = false;
		const publish = (change: WorkerStateChange) => {
			for (const listener of listeners) present(() => listener(change));
		};
		const runtime = new WorkerRuntime((left, right) => workerTaskScopesConflict(left.task, left.cwd, right.task, right.cwd), () => {
			publish({ type: "changed" });
			if (stopped) return; // Never append old-session cleanup into a newly selected branch.
			for (const batch of runtime.batches.values()) {
				const fingerprint = JSON.stringify([batch.tasks.map((t) => [t.state, t.result?.status]), batch.ui.controlErrors]);
				if (persisted.get(batch.id) === fingerprint) continue;
				persisted.set(batch.id, fingerprint);
				pi.appendEntry?.(WORKER_SNAPSHOT_ENTRY, batchUiSnapshot(batch));
			}
		});
		const view: WorkerSessionView = {
			get awaitingHistory() { return awaitingHistory; },
			get closed() { return closed; },
			lookup(id) { const batch = runtime.batches.get(id); return batch ? batchUiSnapshot(batch) : undefined; },
			history(id) { const snapshot = history.get(id); return snapshot ? structuredClone(snapshot) : undefined; },
			subscribe(listener) { if (!closed) listeners.add(listener); return () => { listeners.delete(listener); }; },
		};
		return { runtime, view, history, publish, processes: createWorkerOwner(),
			bindHistory() { awaitingHistory = false; publish({ type: "history" }); },
			close() { closed = true; publish({ type: "closed" }); listeners.clear(); },
		};
	};
	const transcripts = new WorkerTranscriptReader(agentDir);
	let session = createSession([], true);
	registerCapability<WorkerCapability>(pi, WORKER_CAPABILITY, {
		readTranscript: (request) => transcripts.read(request),
		getSession: () => session.view,
		getSettings: () => structuredClone(services.loadRoutingConfig().config),
		getModels: (ctx) => ctx.modelRegistry.getAvailable().map((model) => ({ id: `${model.provider}/${model.id}`, thinking: supportedThinking(model) })),
		saveSettings: (ctx, value) => {
			const current = services.loadRoutingConfig();
			const saved = saveWorkerSettings(ctx, value, current.path);
			if (JSON.stringify(current.config) !== JSON.stringify(saved)) {
				notifyFrontendEvent(pi, ctx, { name: "worker.settings-saved", data: {} });
			}
			return saved;
		},
	});
	let initialized = false;
	const shutdown = async () => {
		stopped = true;
		const owner = session;
		// Abort task controllers first so execution retains the session shutdown source.
		const disposal = owner.runtime.dispose();
		beginWorkerShutdown(owner.processes);
		try { await disposal; }
		finally { owner.close(); }
	};
	const restart = async (_event: unknown, ctx: any) => {
		initialized = true;
		await shutdown();
		session = createSession(ctx?.sessionManager?.getBranch() ?? []);
		stopped = false;
	};
	pi.on("session_shutdown", async () => { transcripts.clear(); await shutdown(); });
	pi.on("session_start", async (event, ctx) => {
		if (initialized || stopped) return restart(event, ctx);
		// beforeSessionStart may already have built/rendered the original cards.
		// Hydrate that initial owner only; real session replacements still freeze it.
		initialized = true;
		for (const [id, details] of workerBranchSnapshots(ctx.sessionManager.getBranch())) session.history.set(id, details);
		session.bindHistory();
	});
	pi.on("session_tree", restart);
	pi.registerTool({
		name: "worker",
		label: "Worker",
		description: TOOL_DESCRIPTION,
		parameters: InputSchema,
		async execute(toolCallId, input: WorkerToolInput, signal, onUpdate, ctx) {
			const invocationSession = session;
			const fail = (error: unknown): never => {
				const batch = input?.batchId && invocationSession.runtime.batches.get(input.batchId);
				if (batch && invocationSession === session && batch.ui.originToolCallId) {
					batch.ui.controlErrors = [...(batch.ui.controlErrors ?? []), { toolCallId, message: error instanceof Error ? error.message : String(error) }].slice(-32);
					invocationSession.runtime.changed();
					invocationSession.publish({ type: "batch", batchId: batch.id });
				}
				throw error; // Still a genuine model-facing tool error, never a successful result.
			};
			try {
				validateWorkerInput(input);
				if (stopped) throw new Error("Worker 会话已关闭");
			} catch (error) { fail(error); }
			const activeStartError = (owner: WorkerRuntime) => {
				const active = [...owner.batches.values()].find((batch) => !batch.finished);
				if (!active) return undefined;
				const error = {
					status: "blocked", error: "active_batch", activeBatchId: active.id,
					summary: ["本会话已有未完成的 Worker 批次；不能启动第二批次。并行任务须放在同一次 tasks 数组中。"],
					next_action: "使用 worker({batchId: activeBatchId}) 继续等待，或 worker({batchId: activeBatchId, cancel:true}) 取消并等待清理；完成后再启动新批次。",
				};
				return respond(JSON.stringify(error, null, 2), error);
			};
			if (input.batchId !== undefined) {
				try {
					const owner = session.runtime;
					const batch = owner.get(input.batchId);
					// A pre-aborted continuation must not leave orphan work.
					if (signal?.aborted) owner.cancel(batch.id, "Worker 调用已取消", "invocation_abort");
					else if (input.cancel) owner.cancel(batch.id);
					await owner.wait(batch.id, signal);
					return batchResponse(batch);
				} catch (error) { return fail(error); }
			}
			const blocked = activeStartError(session.runtime);
			if (blocked) return blocked;
			const tasks = input.tasks!;
			const taskErrors = tasks.map((task) => validateTask(task, ctx.cwd));
			if (taskErrors.some((errors) => errors.length)) {
				const details = taskErrors.map((errors, index) => ({ index, errors }));
				return respond(JSON.stringify({ status: "failed", validation_errors: details }, null, 2), details);
			}
			let loaded: ReturnType<typeof loadRoutingConfig>;
			try { loaded = services.loadRoutingConfig(); }
			catch (error) {
				return respond(JSON.stringify({ status: "blocked", summary: [error instanceof Error ? error.message : String(error)] }, null, 2), { status: "blocked" });
			}
			if (signal?.aborted || stopped) return respond("Worker 已取消或会话已关闭", { status: "failed" });
			const owner = session.runtime; // Session replacement must not redirect old callbacks.
			let toolActive = true;
			const uiDetails: WorkerUiDetails = {
				kind: "worker-ui", originToolCallId: toolCallId, startedAt: Date.now(), limit: loaded.config.maxConcurrentWorkers, total: tasks.length, completed: 0,
				tasks: tasks.map((task, index) => ({ index, mode: task.mode, title: task.title, objective: task.objective, status: "queued", requestedPreset: task.preset, sessionMode: task.sessionMode, attempt: 0, phase: "等待执行", activities: [], toolCalls: 0, usage: emptyWorkerUsage() })),
			};
			const emitTaskUsage = (task: WorkerUiDetails["tasks"][number]) => {
				const snapshot: WorkerUsageSnapshot = { taskId: `${toolCallId}:${task.index}`, input: task.usage.input, output: task.usage.output };
				present(() => pi.events.emit(WORKER_USAGE_EVENT, snapshot));
			};
			// Admission and start are synchronous: no second invocation can slip between this
			// check and start, including reentrant calls from configuration/UI callbacks.
			const blockedAtAdmission = activeStartError(owner);
			if (blockedAtAdmission) return blockedAtAdmission;
			let sessionSnapshot: WorkerSessionSnapshot | undefined;
			try {
				if (tasks.some((task) => task.sessionMode === "fork")) sessionSnapshot = captureWorkerSession(ctx.sessionManager);
			} catch (error) {
				return respond(JSON.stringify({ status: "blocked", summary: [`Worker 会话快照失败：${error instanceof Error ? error.message : String(error)}`] }), { status: "blocked" });
			}
			const batch = owner.start({
				limit: loaded.config.maxConcurrentWorkers, timeoutMs: loaded.config.defaultTimeoutMs,
				maxOutputBytes: loaded.config.maxOutputBytes, ui: uiDetails,
				execute: async (running) => {
					const uiTask = uiDetails.tasks[running.index];
					uiTask.startedAt = Date.now();
					const item = await services.executeTask(running.task, loaded.config, loaded.warnings, ctx, running.controller.signal, (patch) => {
						Object.assign(uiTask, patch);
						emitTaskUsage(uiTask);
						owner.changed();
						if (toolActive) present(() => onUpdate?.(respond(`${running.task.mode} ${uiTask.phase}`, batchUiSnapshot(running.batch))));
					}, () => running.overlappedWriter, true, invocationSession.processes, running.task.sessionMode === "fork" ? sessionSnapshot : undefined);
					// Preserve the host-produced locator even when no progress callback ran.
					if (item.transcript) uiTask.transcript = { ...item.transcript };
					const sanitized = sanitizeStructuredValue(item) as Record<string, any>;
					const compact = compactWorkerResult(sanitized);
					compact.observed_changed_files = Array.isArray(sanitized.observed_changed_files) ? sanitized.observed_changed_files : [];
					if (compact.execution) {
						uiTask.resolvedPreset = compact.execution.resolved_preset ?? uiTask.resolvedPreset;
						uiTask.modelId = compact.execution.actual_model_id ?? uiTask.modelId;
						uiTask.thinking = compact.execution.actual_thinking ?? uiTask.thinking;
						uiTask.attempt = compact.execution.attempt ?? uiTask.attempt;
					}
					if (item.execution?.usage) uiTask.usage = { ...item.execution.usage };
					emitTaskUsage(uiTask);
					return compact;
				},
			}, tasks, ctx.cwd);
			try {
				// Attach the original card even when every task is queued or emits no progress.
				// Presentation failures must not orphan the newly admitted batch.
				present(() => onUpdate?.(respond(`准备执行 ${tasks.length} 个 Worker 任务`, batchUiSnapshot(batch))));
				await owner.wait(batch.id, signal);
				return batchResponse(batch);
			} finally {
				// Runtime detaches the invocation's signal on return; the session owns background work.
				toolActive = false;
			}
		},
	});
}
