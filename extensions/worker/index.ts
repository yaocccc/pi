import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { StringEnum } from "@earendil-works/pi-ai";
import { Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";
import { MODES, PRESETS, loadRoutingConfig, validateTask } from "./config";
import { logWorkerReturn } from "./logging";
import { beginWorkerShutdown, executeTask, killAllChildren, resetWorkerRuntime } from "./process";
import { batchRequiresSerial } from "./security";
import type { WorkerToolInput, WorkerUiDetails } from "./types";
import { cloneUiDetails, compactWorkerResult, emptyWorkerUsage, renderWorkerDetails, sanitizeStructuredValue, serializePayload } from "./ui";


export const TOOL_DESCRIPTION = `Use this tool autonomously for bounded, independently verifiable coding subtasks. Do not ask the user before delegating suitable tasks. Use Fast for clear local work, Normal for normal development, and Deep for difficult work. Deep is the highest automatic task-complexity level. Use Max only when the user explicitly requests Max, xhigh, or maximum strength, and set userExplicitMax: true. Read the worker-orchestration skill when decomposition, parallelization, routing, review, or acceptance strategy is non-trivial. Workers may not create other workers. Failed Worker tasks are returned to the main agent for direct handling and are not automatically retried. The main agent remains responsible for reviewing the diff, validation, and acceptance evidence.`;

export const TaskSchema = Type.Object({
	mode: StringEnum(MODES),
	objective: Type.String({ minLength: 1 }),
	preset: Type.Optional(StringEnum(PRESETS)),
	userExplicitMax: Type.Optional(Type.Boolean({ description: "Must be true only when the user explicitly requested Max or maximum strength" })),
	context: Type.Optional(Type.String()),
	relevantFiles: Type.Optional(Type.Array(Type.String())),
	allowedPaths: Type.Optional(Type.Array(Type.String())),
	forbiddenPaths: Type.Optional(Type.Array(Type.String())),
	acceptanceCriteria: Type.Optional(Type.Array(Type.String())),
	verificationCommands: Type.Optional(Type.Array(Type.String())),
	outputRequirements: Type.Optional(Type.Array(Type.String())),
	cwd: Type.Optional(Type.String({ description: "Relative subdirectory within the main agent cwd; absolute and escaping paths are rejected" })),
});

export const InputSchema = Type.Object({
	task: Type.Optional(TaskSchema),
	tasks: Type.Optional(Type.Array(TaskSchema, { minItems: 1, maxItems: 12 })),
	manual: Type.Optional(Type.Boolean()),
});

export async function mapWithLimit<T, R>(items: T[], limit: number, fn: (item: T, index: number) => Promise<R>): Promise<R[]> {
	const results = new Array<R>(items.length);
	let cursor = 0;
	async function runner() {
		while (true) {
			const index = cursor++;
			if (index >= items.length) return;
			try { results[index] = await fn(items[index], index); }
			catch (error) { results[index] = { status: "failed", summary: [error instanceof Error ? error.message : String(error)] } as R; }
		}
	}
	await Promise.all(Array.from({ length: Math.min(limit, items.length) }, runner));
	return results;
}

export default function workerExtension(pi: ExtensionAPI) {
	if (Number(process.env.PI_WORKER_DEPTH || "0") >= 1) return;
	resetWorkerRuntime();
	pi.on("session_shutdown", async () => beginWorkerShutdown());
	pi.registerTool({
		name: "worker",
		label: "Worker",
		description: TOOL_DESCRIPTION,
		parameters: InputSchema,
		async execute(toolCallId, input: WorkerToolInput, signal, onUpdate, ctx) {
			const respond = (text: string, details: unknown) => {
				logWorkerReturn(toolCallId, text);
				return { content: [{ type: "text" as const, text }], details };
			};
			if (Boolean(input.task) === Boolean(input.tasks)) {
				return respond("worker 参数错误：task 和 tasks 必须二选一", { status: "failed" });
			}
			const tasks = input.task ? [input.task] : input.tasks!;
			const taskErrors = tasks.map((task) => validateTask(task, ctx.cwd));
			if (taskErrors.some((errors) => errors.length)) {
				const details = taskErrors.map((errors, index) => ({ index, errors }));
				return respond(JSON.stringify({ status: "failed", validation_errors: details }, null, 2), details);
			}
			let loaded: Awaited<ReturnType<typeof loadRoutingConfig>>;
			try { loaded = await loadRoutingConfig(ctx); }
			catch (error) {
				return respond(JSON.stringify({ status: "blocked", summary: [error instanceof Error ? error.message : String(error)] }, null, 2), { status: "blocked" });
			}
			if (!loaded.config.automaticDelegationEnabled && !input.manual) {
				return respond(JSON.stringify({ status: "blocked", summary: ["自动委派已在 worker-settings.json 中关闭；手动调用请设置 manual: true"] }, null, 2), { status: "blocked" });
			}
			let limit = loaded.config.maxConcurrentWorkers;
			if (batchRequiresSerial(tasks)) limit = 1;
			const uiDetails: WorkerUiDetails = {
				kind: "worker-ui",
				startedAt: Date.now(),
				limit,
				total: tasks.length,
				completed: 0,
				tasks: tasks.map((task, index) => ({
					index,
					mode: task.mode,
					objective: task.objective,
					status: "queued",
					requestedPreset: task.preset ?? "auto",
					attempt: 0,
					phase: "等待执行",
					activities: [],
					toolCalls: 0,
					usage: emptyWorkerUsage(),
				})),
			};
			const emitUi = (message: string) => onUpdate?.({
				content: [{ type: "text", text: message }],
				details: cloneUiDetails(uiDetails),
			});
			emitUi(`准备执行 ${tasks.length} 个 Worker 任务`);
			const results = await mapWithLimit(tasks, limit, async (task, index) => {
				const uiTask = uiDetails.tasks[index]!;
				uiTask.status = "running";
				uiTask.startedAt = Date.now();
				uiTask.phase = "准备任务";
				emitUi(`${task.mode} 开始`);
				let item: Record<string, any>;
				try {
					item = await executeTask(task, loaded.config, loaded.warnings, ctx, signal, (patch) => {
						Object.assign(uiTask, patch);
						emitUi(`${task.mode} ${uiTask.phase}`);
					});
				} catch (error) {
					item = {
						status: "failed",
						summary: [error instanceof Error ? error.message : String(error)],
						changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: ["主 Agent 检查异常"],
					};
				}
				item = compactWorkerResult(sanitizeStructuredValue(item) as Record<string, any>);
				uiTask.result = item;
				uiTask.status = item.status === "completed" || item.status === "blocked" || item.status === "failed" ? item.status : "failed";
				uiTask.phase = uiTask.status === "completed" ? "已完成" : uiTask.status === "blocked" ? "已阻塞" : "执行失败";
				uiTask.finishedAt = Date.now();
				const execution = item.execution;
				if (execution) {
					uiTask.resolvedPreset = execution.resolved_preset ?? uiTask.resolvedPreset;
					uiTask.modelId = execution.actual_model_id ?? execution.resolved_model_id ?? uiTask.modelId;
					uiTask.thinking = execution.actual_thinking ?? execution.resolved_thinking ?? uiTask.thinking;
					uiTask.attempt = execution.attempt ?? uiTask.attempt;
					if (execution.usage) uiTask.usage = { ...execution.usage };
				}
				uiDetails.completed++;
				emitUi(`${task.mode} ${uiTask.phase}`);
				return item;
			});
			const payload: Record<string, any> = input.task
				? results[0]
				: { status: results.every((item) => item.status === "completed") ? "completed" : "partial", results };
			const serialized = serializePayload(payload, loaded.config.maxOutputBytes);
			uiDetails.payload = serialized.payload;
			uiDetails.finishedAt = Date.now();
			return respond(serialized.text, cloneUiDetails(uiDetails));
		},
		renderCall(args, theme) {
			const count = args.task ? 1 : Array.isArray(args.tasks) ? args.tasks.length : 0;
			const title = count > 1 ? `Workers ×${count}` : "Worker";
			return new Text(theme.fg("toolTitle", theme.bold(title)), 0, 0);
		},
		renderResult(result, _options, theme) {
			const details = result.details as WorkerUiDetails | undefined;
			if (details?.kind === "worker-ui" && details.tasks.length) return renderWorkerDetails(details, theme);
			const text = result.content.find((item) => item.type === "text");
			return new Text(text?.type === "text" ? text.text : "Worker 无输出", 0, 0);
		},
	});
}

if (!(globalThis as any).__piWorkerExitHookInstalled) {
	(globalThis as any).__piWorkerExitHookInstalled = true;
	process.once("exit", () => killAllChildren(true));
}
