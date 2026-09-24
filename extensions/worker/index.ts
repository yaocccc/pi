import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";
import { Check } from "typebox/value";
import { MODES, PRESETS, loadRoutingConfig, validateTask } from "./config.ts";
import { WORKER_USAGE_EVENT, type WorkerUsageSnapshot } from "./events.ts";
import { registerWorkerWriteGuard } from "./guard.ts";
import { beginWorkerShutdown, executeTask, killAllChildren, resetWorkerRuntime } from "./process.ts";
import { workerTaskScopesConflict } from "./security.ts";
import { configureWorkerSettings } from "./settings-ui.ts";
import type { WorkerToolInput, WorkerUiDetails } from "./types";
import { cloneUiDetails, compactWorkerResult, emptyWorkerUsage, renderWorkerDetails, sanitizeStructuredValue, serializePayload } from "./ui.ts";
import { WorkerRenderRefresh } from "./render-refresh.ts";
import { WorkerRuntime, type Batch, type QuestionRecord } from "./runtime.ts";
import { ANSWER_LIMIT } from "./ipc.ts";
import { INTERACTION_EVENT, type InteractionEvent } from "../shared/interaction-lifecycle.ts";
import { emptyWorkerComponent, workerBranchSnapshots, WORKER_SNAPSHOT_ENTRY } from "./history.ts";

export const TOOL_DESCRIPTION = `Use this tool autonomously for bounded, independently verifiable coding subtasks. Do not ask the user before delegating suitable tasks. Use Fast for clear local work, Normal for normal development, and Deep for difficult work. Deep is the highest automatic task-complexity level. Use Max only when the user explicitly requests Max, xhigh, or maximum strength, and set userExplicitMax: true. Read the worker-orchestration skill when decomposition, parallelization, routing, review, or acceptance strategy is non-trivial. Workers may not create other workers. Failed Worker tasks are returned to the main agent for direct handling and are not automatically retried. The main agent remains responsible for reviewing the diff, validation, and acceptance evidence. Single-tool protocol: only ONE unfinished batch per session. To parallelize, put independent tasks in the SAME tasks array; never launch separate starts in one tool-call round. Starting another batch before the current one finishes returns activeBatchId immediately: answer/continue/cancel that batch first. Start with exactly one of task/tasks (optional manual); continue with {batchId}; answer with {batchId, answers:[{taskId, questionId, answer}]}; cancel with {batchId, cancel:true}. Do not mix start and continuation fields or answers and cancel. Uses exact questions[].id as questionId. Answers are validated together before any are applied; duplicate, expired and foreign IDs are errors. Answers cannot expand task permissions. Start/continue/answer waits until a pending question or the entire batch finishes, without a polling timeout. A question returns immediately while independent tasks continue in the background; answering then waits for the next question or completion. Waiting workers retain concurrency slots and path locks. If a decision requires user judgment, call ask_question: actual TUI interaction pauses only waiting workers' remaining task/question budgets (including newly arriving questions), at most 30 minutes; independent work continues. User answers are NOT forwarded automatically: the real main Agent must explicitly answer the exact Worker question IDs. Esc/cancellation is not consent. This is not suspension of arbitrary running tools. For global dangerous decisions first cancel relevant batches; expanding permissions requires cancellation, cleanup and a new task. The original batch card shows all live Q&A; valid follow-up calls render no new card. Cancel waits for cleanup and lock release. Aborting ANY active invocation cancels its batch and waits for cleanup; aborting after it returned has no effect. No automatic follow-up messages are queued: keep calling worker for unfinished batches before finishing your turn. Do not modify paths locked by background workers.`;

export const TaskSchema = Type.Object({
	mode: StringEnum(MODES),
	objective: Type.String({ minLength: 1 }),
	preset: Type.Optional(StringEnum(PRESETS)),
	userExplicitMax: Type.Optional(Type.Boolean({ description: "Must be true only when the user explicitly requested Max or maximum strength" })),
	context: Type.Optional(Type.String()),
	relevantFiles: Type.Optional(Type.Array(Type.String({ description: "Read hints may be relative, absolute, or outside cwd; they do not grant write access" }))),
	allowedPaths: Type.Optional(Type.Array(Type.String())),
	forbiddenPaths: Type.Optional(Type.Array(Type.String())),
	acceptanceCriteria: Type.Optional(Type.Array(Type.String())),
	verificationCommands: Type.Optional(Type.Array(Type.String())),
	outputRequirements: Type.Optional(Type.Array(Type.String())),
	cwd: Type.Optional(Type.String({ description: "Relative subdirectory within the main agent cwd; absolute and escaping paths are rejected" })),
}, { additionalProperties: false });

export const InputSchema = Type.Object({
	task: Type.Optional(TaskSchema),
	tasks: Type.Optional(Type.Array(TaskSchema, { minItems: 1, maxItems: 12 })),
	manual: Type.Optional(Type.Boolean()),
	batchId: Type.Optional(Type.String({ minLength: 1 })),
	answers: Type.Optional(Type.Array(Type.Object({
		taskId: Type.String({ minLength: 1 }), questionId: Type.String({ minLength: 1 }),
		answer: Type.String({ minLength: 1, maxLength: ANSWER_LIMIT }),
	}, { additionalProperties: false }), { minItems: 1, maxItems: 384 })),
	cancel: Type.Optional(Type.Literal(true)),
	questionOffset: Type.Optional(Type.Integer({ minimum: 0, description: "History page offset in the next question/completion response (8 records/page); does not enable polling" })),
}, { additionalProperties: false });

/** Validate at execution too: direct callers must not bypass schema or mode constraints. */
export function validateWorkerInput(input: unknown): asserts input is WorkerToolInput {
	if (!Check(InputSchema, input)) throw new Error("worker 参数错误：字段、类型或数量无效（不接受未知字段）");
	const value = input as WorkerToolInput;
	if (value.batchId !== undefined) {
		if (!value.batchId.trim() || "task" in value || "tasks" in value || "manual" in value) throw new Error("worker 参数错误：batchId 不能与 task/tasks/manual 混用");
		if (value.cancel && ("answers" in value || "questionOffset" in value)) throw new Error("worker 参数错误：cancel 不能与 answers/questionOffset 混用");
	} else {
		if (("task" in value) === ("tasks" in value)) throw new Error("worker 参数错误：task 和 tasks 必须二选一");
		if ("answers" in value || "cancel" in value || "questionOffset" in value) throw new Error("worker 参数错误：answers/cancel/questionOffset 必须提供 batchId");
	}
}

const respond = <T>(text: string, details: T) => ({ content: [{ type: "text" as const, text }], details });

/** Sanitize question text and any extra fields without rewriting validated routing IDs. */
function sanitizeQuestionRecord(record: QuestionRecord): QuestionRecord {
	const sanitized = sanitizeStructuredValue(record) as QuestionRecord;
	return {
		...sanitized,
		id: /^[A-Za-z0-9-]{1,80}$/.test(record.id) ? record.id : sanitized.id,
		batchId: record.batchId,
		taskId: record.taskId,
	};
}

/** One sanitized, detached view shared by live rows and serialized tool results. */
export function batchUiSnapshot(batch: Batch): WorkerUiDetails {
	const details = sanitizeStructuredValue(cloneUiDetails(batch.ui)) as WorkerUiDetails;
	details.batchId = batch.id;
	// Opaque Pi routing IDs are metadata, not user text: never redact or truncate them.
	details.originToolCallId = batch.ui.originToolCallId;
	details.controlErrors = batch.ui.controlErrors?.map((error, index) => ({
		toolCallId: error.toolCallId, message: details.controlErrors![index].message,
	}));
	details.snapshotAt = Date.now();
	details.questions = batch.questions.map(sanitizeQuestionRecord);
	return details;
}

/** Keep IDs/control state outside result compression so truncation cannot strand a batch. */
export function batchResponse(batch: Batch, questionOffset?: number) {
	const results = batch.tasks.map((task) => task.result ?? { status: task.state });
	const resultPayload = batch.single ? results[0] : { status: results.every((item) => item.status === "completed") ? "completed" : "partial", results };
	const compressed = serializePayload(resultPayload, batch.maxOutputBytes);
	const waiting = batch.questions.filter((q) => q.status === "waiting");
	const offset = questionOffset ?? Math.max(0, batch.questions.length - 8);
	const history = batch.questions.slice(offset, offset + 8);
	const payload = {
		batchId: batch.id,
		status: batch.finished ? resultPayload.status : batch.cancelled ? "cancelling" : waiting.length ? "waiting_for_reply" : "running",
		finished: batch.finished,
		tasks: batch.tasks.map((task) => ({ taskId: task.id, status: task.state === "finished" ? task.result?.status : waiting.some((q) => q.taskId === task.id) ? "waiting_for_reply" : task.state })),
		questions: waiting.map(sanitizeQuestionRecord),
		history: history.map(sanitizeQuestionRecord),
		historyOffset: offset,
		historyTotal: batch.questions.length,
		nextHistoryOffset: offset + history.length < batch.questions.length ? offset + history.length : null,
		result: compressed.payload,
		next_action: batch.finished ? "Review results and actual diff." : "Call worker({batchId, answers:[{taskId, questionId, answer}]}) for pending questions, worker({batchId}) to continue, or worker({batchId, cancel:true}) to cancel and clean up. Background workers retain path locks.",
	};
	const details = batchUiSnapshot(batch);
	details.payload = payload;
	return respond(JSON.stringify(payload, null, 2), details);
}

export default function workerExtension(pi: ExtensionAPI, services = { executeTask, loadRoutingConfig }) {
	if (Number(process.env.PI_WORKER_DEPTH || "0") >= 1) {
		registerWorkerWriteGuard(pi);
		return;
	}
	let stopped = false;
	const createSession = (branch: readonly any[] = [], awaitingHistory = false) => {
		const history = workerBranchSnapshots(branch);
		const persisted = new Map<string, string>();
		const runtime = new WorkerRuntime((left, right) => workerTaskScopesConflict(left.task, left.cwd, right.task, right.cwd), () => {
			refresh.changed();
			if (stopped) return; // Never append old-session cleanup into a newly selected branch.
			for (const batch of runtime.batches.values()) {
				const fingerprint = JSON.stringify([batch.tasks.map((t) => [t.state, t.result?.status]), batch.questions, batch.ui.controlErrors]);
				if (persisted.get(batch.id) === fingerprint) continue;
				persisted.set(batch.id, fingerprint);
				pi.appendEntry?.(WORKER_SNAPSHOT_ENTRY, batchUiSnapshot(batch));
			}
		});
		const refresh = new WorkerRenderRefresh((id) => {
			const batch = runtime.batches.get(id);
			return batch ? batchUiSnapshot(batch) : undefined;
		}, undefined, (id) => history.get(id), awaitingHistory);
		return { runtime, refresh, history };
	};
	let session = createSession([], true);
	let initialized = false;
	resetWorkerRuntime();
	const shutdown = async () => {
		stopped = true;
		const owner = session;
		beginWorkerShutdown();
		try { await owner.runtime.dispose(); }
		finally { owner.refresh.dispose(); }
	};
	const restart = async (_event: unknown, ctx: any) => {
		initialized = true;
		await shutdown();
		session = createSession(ctx?.sessionManager?.getBranch() ?? []);
		stopped = false;
		resetWorkerRuntime();
	};
	pi.on("session_shutdown", shutdown);
	pi.on("session_start", async (event, ctx) => {
		if (initialized || stopped) return restart(event, ctx);
		// beforeSessionStart may already have built/rendered the original cards.
		// Hydrate that initial owner only; real session replacements still freeze it.
		initialized = true;
		for (const [id, details] of workerBranchSnapshots(ctx.sessionManager.getBranch())) session.history.set(id, details);
		session.refresh.bindHistory();
	});
	pi.on("session_tree", restart);
	pi.events.on?.(INTERACTION_EVENT, (value: unknown) => {
		const event = value as InteractionEvent;
		if (!event || typeof event.waitUntil !== "function" || typeof event.token !== "string") return;
		const owner = session.runtime;
		if (event.phase === "begin") event.waitUntil(owner.beginInteraction(event.token, event.deadline));
		else if (event.phase === "end") event.waitUntil(owner.endInteraction(event.token));
	});
	pi.registerCommand("worker_settings", {
		description: "交互式配置 Worker",
		handler: async (_args, ctx) => {
			if (!ctx.hasUI) {
				pi.sendMessage({ customType: "text", content: "/worker_settings 仅支持交互式 UI。", display: true });
				return;
			}
			await ctx.waitForIdle();
			try {
				const loaded = services.loadRoutingConfig();
				const updated = await configureWorkerSettings(ctx, loaded.config, loaded.path);
				if (updated) ctx.ui.notify("Worker 设置已保存，后续任务立即生效", "info");
			} catch (error) {
				const message = error instanceof Error ? error.message : String(error);
				ctx.ui.notify(`Worker 设置保存失败：${message}`, "error");
			}
		},
	});
	pi.registerTool({
		name: "worker",
		label: "Worker",
		description: TOOL_DESCRIPTION,
		parameters: InputSchema,
		renderShell: "self",
		async execute(toolCallId, input: WorkerToolInput, signal, onUpdate, ctx) {
			const invocationSession = session;
			const fail = (error: unknown): never => {
				const batch = input?.batchId && invocationSession.runtime.batches.get(input.batchId);
				if (batch && invocationSession === session && batch.ui.originToolCallId) {
					batch.ui.controlErrors = [...(batch.ui.controlErrors ?? []), { toolCallId, message: error instanceof Error ? error.message : String(error) }].slice(-32);
					invocationSession.runtime.changed();
					invocationSession.refresh.refreshBatch(batch.id);
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
					next_action: "使用 worker({batchId: activeBatchId, answers:[{taskId, questionId, answer}]}) 回答问题、worker({batchId: activeBatchId}) 继续等待，或 worker({batchId: activeBatchId, cancel:true}) 取消并等待清理；完成后再启动新批次。",
				};
				return respond(JSON.stringify(error, null, 2), error);
			};
			if (input.batchId !== undefined) {
				try {
					const owner = session.runtime;
					const batch = owner.get(input.batchId);
					// A pre-aborted continuation must not deliver answers or leave orphan work.
					if (input.cancel || signal?.aborted) owner.cancel(batch.id);
					else if (input.answers) owner.replyMany(batch.id, input.answers);
					await owner.wait(batch.id, signal);
					return batchResponse(batch, input.questionOffset);
				} catch (error) { return fail(error); }
			}
			const blocked = activeStartError(session.runtime);
			if (blocked) return blocked;
			const tasks = input.task ? [input.task] : input.tasks!;
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
			if (!loaded.config.automaticDelegationEnabled && !input.manual) {
				return respond(JSON.stringify({ status: "blocked", summary: ["自动委派已在 worker-settings.json 中关闭；手动调用请设置 manual: true"] }, null, 2), { status: "blocked" });
			}
			if (signal?.aborted || stopped) return respond("Worker 已取消或会话已关闭", { status: "failed" });
			const owner = session.runtime; // Session replacement must not redirect old callbacks.
			let toolActive = true;
			const uiDetails: WorkerUiDetails = {
				kind: "worker-ui", originToolCallId: toolCallId, startedAt: Date.now(), limit: loaded.config.maxConcurrentWorkers, total: tasks.length, completed: 0,
				tasks: tasks.map((task, index) => ({ index, mode: task.mode, objective: task.objective, status: "queued", requestedPreset: task.preset ?? "auto", attempt: 0, phase: "等待执行", activities: [], toolCalls: 0, usage: emptyWorkerUsage() })),
			};
			const emitTaskUsage = (task: WorkerUiDetails["tasks"][number]) => {
				const snapshot: WorkerUsageSnapshot = { taskId: `${toolCallId}:${task.index}`, input: task.usage.input, output: task.usage.output };
				pi.events.emit(WORKER_USAGE_EVENT, snapshot);
			};
			// Admission and start are synchronous: no second invocation can slip between this
			// check and start, including reentrant calls from configuration/UI callbacks.
			const blockedAtAdmission = activeStartError(owner);
			if (blockedAtAdmission) return blockedAtAdmission;
			const batch = owner.start({
				limit: loaded.config.maxConcurrentWorkers, timeoutMs: loaded.config.defaultTimeoutMs,
				maxOutputBytes: loaded.config.maxOutputBytes, single: !!input.task, ui: uiDetails,
				execute: async (running) => {
					const uiTask = uiDetails.tasks[running.index];
					uiTask.startedAt = Date.now();
					const item = await services.executeTask(running.task, loaded.config, loaded.warnings, ctx, running.controller.signal, (patch) => {
						Object.assign(uiTask, patch);
						emitTaskUsage(uiTask);
						owner.changed();
						if (toolActive) onUpdate?.(respond(`${running.task.mode} ${uiTask.phase}`, batchUiSnapshot(running.batch)));
					}, () => running.overlappedWriter, (question, questionSignal, control) => owner.ask(running, question, questionSignal, control), true);
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
				try { onUpdate?.(respond(`准备执行 ${tasks.length} 个 Worker 任务`, batchUiSnapshot(batch))); }
				catch { /* The runtime's live renderer can still update an attached row. */ }
				await owner.wait(batch.id, signal);
				return batchResponse(batch);
			} finally {
				// Runtime detaches the invocation's signal on return; the session owns background work.
				toolActive = false;
			}
		},
		renderCall(args, theme) {
			// Empty partial args and ALL continuations have no title, shell, padding or spinner.
			if (!args.task && !Array.isArray(args.tasks)) return emptyWorkerComponent();
			const count = args.task ? 1 : Array.isArray(args.tasks) ? args.tasks.length : 0;
			const title = count > 1 ? `Workers ×${count}` : "Worker";
			return new Text(theme.fg("toolTitle", theme.bold(title)), 0, 0);
		},
		renderResult(result, options, theme, context) {
			const details = result.details as WorkerUiDetails | undefined;
			const batchId = details?.batchId ?? context?.args?.batchId;
			const known = batchId ? session.runtime.batches.get(batchId)?.ui ?? session.history.get(batchId) : undefined;
			const origin = known?.originToolCallId ?? details?.originToolCallId;
			if (context?.toolCallId && origin && context.toolCallId !== origin && known &&
				(details?.kind === "worker-ui" || known.controlErrors?.some((error) => error.toolCallId === context.toolCallId))) return emptyWorkerComponent();
			if (details?.kind === "worker-ui" && details.tasks.length) {
				const view = session.refresh.resolve(details, context);
				return renderWorkerDetails(view.details, theme, { expanded: options.expanded, snapshot: view.snapshot });
			}
			const text = result.content.find((item) => item.type === "text");
			return new Text(text?.type === "text" ? text.text : "Worker 无输出", 0, 0);
		},
	});
}

if (!(globalThis as any).__piWorkerExitHookInstalled) {
	(globalThis as any).__piWorkerExitHookInstalled = true;
	process.once("exit", () => killAllChildren(true));
}
