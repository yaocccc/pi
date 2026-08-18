import { spawn, type ChildProcess } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { parseFrontmatter } from "@earendil-works/pi-coding-agent";
import { agentDir, resolveRoute, resolveTaskCwd } from "./config";
import { WORKER_ALLOWED_PATHS_ENV, WORKER_CWD_ENV, WORKER_FORBIDDEN_PATHS_ENV, WORKER_MODE_ENV } from "./guard";
import { attributeChangedFiles, changedSince, snapshotWorkspace } from "./security";
import type { ChildProgress, ChildResult, Route, RoutingConfig, WorkerTask, WorkerUiActivity, WorkerUiActivityStatus, WorkerUiTask, WorkerUsage, WorkspaceSnapshot } from "./types";
import { UI_ACTIVITY_LIMIT, UI_DETAIL_CAP, addWorkerUsage, appendUiActivity, emptyWorkerUsage, estimateMessageTokens, messageUsage, publicThinking, summarizeToolArgs, summarizeToolResult, uiSnippet } from "./ui";

export const activeChildren = new Set<ChildProcess>();
export let runtimeShuttingDown = false;
export let activeSlots = 0;
export const slotWaiters: Array<() => void> = [];
export const MAX_WORKER_EVENT_LINE_BYTES = 8 * 1024 * 1024;
export const MAX_WORKER_STDOUT_BYTES = 64 * 1024 * 1024;
export const WORKER_TERMINATE_GRACE_MS = 3_000;
export const WORKER_FORCE_SETTLE_MS = 1_000;

export function getPiInvocation(args: string[]): { command: string; args: string[] } {
	const currentScript = process.argv[1];
	if (currentScript && !currentScript.startsWith("/$bunfs/root/") && fs.existsSync(currentScript)) {
		const runtime = path.basename(process.execPath).toLowerCase();
		if (runtime === "node" || runtime === "node.exe" || runtime === "bun" || runtime === "bun.exe") return { command: process.execPath, args: [currentScript, ...args] };
		return { command: currentScript, args };
	}
	return { command: "pi", args };
}

export function killProcessTree(child: ChildProcess, signal: NodeJS.Signals): void {
	if (!child.pid) return;
	try {
		if (process.platform !== "win32") process.kill(-child.pid, signal);
		else child.kill(signal);
	} catch {
		try { child.kill(signal); } catch { /* already exited */ }
	}
}

export function killAllChildren(force = false): void {
	for (const child of activeChildren) killProcessTree(child, force ? "SIGKILL" : "SIGTERM");
	if (!force && activeChildren.size > 0) {
		setTimeout(() => {
			for (const child of activeChildren) killProcessTree(child, "SIGKILL");
		}, 3_000).unref();
	}
}

export function releaseSlot(): void {
	activeSlots = Math.max(0, activeSlots - 1);
	for (const waiter of [...slotWaiters]) waiter();
}

export async function acquireSlot(limit: number, signal: AbortSignal | undefined, timeoutMs: number): Promise<() => void> {
	if (signal?.aborted) throw new Error("Worker 在等待执行槽位时已取消");
	if (activeSlots < limit) {
		activeSlots++;
		return releaseSlot;
	}
	return await new Promise<() => void>((resolve, reject) => {
		let settled = false;
		const cleanup = () => {
			const index = slotWaiters.indexOf(wake);
			if (index >= 0) slotWaiters.splice(index, 1);
			signal?.removeEventListener("abort", abortHandler);
			clearTimeout(timer);
		};
		const fail = (message: string) => {
			if (settled) return;
			settled = true;
			cleanup();
			reject(new Error(message));
		};
		const wake = () => {
			if (settled || activeSlots >= limit) return;
			settled = true;
			cleanup();
			activeSlots++;
			resolve(releaseSlot);
		};
		const abortHandler = () => fail("Worker 在等待执行槽位时已取消");
		const timer = setTimeout(() => fail("Worker 等待执行槽位超时"), timeoutMs);
		timer.unref();
		slotWaiters.push(wake);
		signal?.addEventListener("abort", abortHandler, { once: true });
	});
}

export function workerPromptBody(): string {
	const promptPath = path.join(agentDir(), "extensions", "worker", "agents", "worker.md");
	if (!fs.existsSync(promptPath)) throw new Error(`缺少 Worker Prompt: ${promptPath}`);
	const parsed = parseFrontmatter<Record<string, string>>(fs.readFileSync(promptPath, "utf8"));
	return parsed.body.trim();
}

export function buildTaskPrompt(task: WorkerTask, route: Route, before: WorkspaceSnapshot | null): string {
	const existing = before ? [...before.statusPaths].map((item) => path.relative(before.cwd, path.join(before.gitRoot, item)).replaceAll("\\", "/")) : [];
	const contract = {
		mode: task.mode,
		objective: task.objective.trim(),
		context: task.context ?? "",
		relevantFiles: task.relevantFiles ?? [],
		allowedPaths: task.allowedPaths ?? [],
		forbiddenPaths: task.forbiddenPaths ?? [],
		acceptanceCriteria: task.acceptanceCriteria ?? [],
		verificationCommands: task.verificationCommands ?? [],
		outputRequirements: task.outputRequirements ?? [],
		cwd: before?.cwd,
		preExistingUserChanges: existing,
		execution: { preset: route.resolvedPreset, model: route.modelId, thinking: route.thinking },
	};
	return `执行以下单一 Worker 任务。不要创建或调用其他 Worker。\n\n任务契约：\n${JSON.stringify(contract, null, 2)}\n\n最终只返回一个 JSON 对象，不要 Markdown 代码围栏，不要隐藏思考过程，不要完整日志。summary 需用 2-4 条简洁但具体的文字说明主要工作或发现、验证结果和重要限制，每条应能独立理解。格式：\n${JSON.stringify({ status: "completed | blocked | failed", summary: ["具体结论"], changed_files: ["path"], validation: [{ command: "command", result: "passed | failed | not_run", details: "简要证据" }], acceptance: [{ criterion: "验收条件", result: "passed | failed | uncertain", evidence: "证据" }], findings: [], risks: [], out_of_scope: [], recommended_next_action: [] }, null, 2)}`;
}

export function extractText(message: any): string {
	if (!message || message.role !== "assistant" || !Array.isArray(message.content)) return "";
	return message.content.filter((item: any) => item?.type === "text" && typeof item.text === "string").map((item: any) => item.text).join("\n");
}

export async function runPiWorker(
	task: WorkerTask,
	route: Route,
	cwd: string,
	systemPrompt: string,
	prompt: string,
	signal: AbortSignal | undefined,
	timeoutMs: number,
	concurrencyLimit: number,
	onProgress?: (progress: ChildProgress) => void,
): Promise<ChildResult> {
	const activities: WorkerUiActivity[] = [];
	const seenToolIds = new Set<string>();
	let phase = "等待执行槽位";
	let toolCalls = 0;
	let lastEmitAt = 0;
	let latestThinking = "";
	let settledUsage = emptyWorkerUsage();
	let streamUsage = emptyWorkerUsage();
	let currentTurn = 1;
	let activeTurn = true;
	const usageSnapshot = (): WorkerUsage => {
		const usage = addWorkerUsage(settledUsage, streamUsage);
		usage.turns = Math.max(settledUsage.turns, activeTurn ? currentTurn : 0);
		return usage;
	};
	const emit = (force = false) => {
		const now = Date.now();
		if (!force && now - lastEmitAt < 150) return;
		lastEmitAt = now;
		onProgress?.({ phase, activities: activities.map((item) => ({ ...item })), toolCalls, usage: usageSnapshot(), actualModel: undefined });
	};
	const setPhase = (value: string, status: WorkerUiActivityStatus = "running") => {
		phase = value;
		for (const activity of activities) {
			if (activity.type === "phase" && activity.status === "running") activity.status = "completed";
		}
		appendUiActivity(activities, { id: `phase:${value}`, type: "phase", status, label: value, at: Date.now() });
		emit(true);
	};
	const upsertTool = (id: string, name: string, status: WorkerUiActivityStatus, detail?: string) => {
		if (!seenToolIds.has(id)) {
			seenToolIds.add(id);
			toolCalls++;
		}
		const activityId = `tool:${id}`;
		const previous = activities.find((item) => item.id === activityId);
		appendUiActivity(activities, {
			id: activityId,
			type: "tool",
			status,
			label: uiSnippet(name, 32) || "未知工具",
			detail: detail || previous?.detail,
			at: Date.now(),
		});
		emit(status !== "running");
	};
	const recordThinking = (value: string) => {
		const text = uiSnippet(value, UI_DETAIL_CAP);
		if (!text) return;
		const existing = activities.findIndex((item) => item.id === "thinking:latest");
		if (existing >= 0) activities.splice(existing, 1);
		activities.push({ id: "thinking:latest", type: "thinking", status: "running", label: "思考", detail: text, at: Date.now() });
		if (activities.length > UI_ACTIVITY_LIMIT) activities.splice(0, activities.length - UI_ACTIVITY_LIMIT);
		emit();
	};

	emit(true);
	const tempDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "pi-worker-"));
	let release: (() => void) | undefined;
	try {
		const systemPath = path.join(tempDir, "worker-system.md");
		await fs.promises.writeFile(systemPath, systemPrompt, { encoding: "utf8", mode: 0o600 });
		const args = ["--mode", "json", "--print", "--no-session", "--no-skills", "--no-context-files", "--model", route.modelId, "--thinking", route.thinking, "--append-system-prompt", systemPath, prompt];
		const invocation = getPiInvocation(args);
		const deadline = Date.now() + timeoutMs;
		try {
			release = await acquireSlot(concurrencyLimit, signal, timeoutMs);
		} catch (error) {
			const aborted = Boolean(signal?.aborted);
			const message = error instanceof Error ? error.message : String(error);
			setPhase(aborted ? "已取消" : "等待执行槽位超时", "failed");
			return {
				exitCode: 1,
				stderr: "",
				assistantText: "",
				errorMessage: message,
				aborted,
				timedOut: !aborted,
				truncated: false,
				activities: activities.map((item) => ({ ...item })),
				toolCalls,
				usage: usageSnapshot(),
			};
		}
		return await new Promise<ChildResult>((resolve) => {
			let stdoutBuffer = "";
			let stderr = "";
			let assistantText = "";
			let actualProvider: string | undefined;
			let actualModel: string | undefined;
			let stopReason: string | undefined;
			let errorMessage: string | undefined;
			let truncated = false;
			let aborted = false;
			let timedOut = false;
			let settled = false;
			let terminating = false;
			let stdoutBytes = 0;
			let forceKillTimer: NodeJS.Timeout | undefined;
			let forceSettleTimer: NodeJS.Timeout | undefined;
			let timeout: NodeJS.Timeout;
			const child = spawn(invocation.command, invocation.args, {
				cwd,
				shell: false,
				detached: process.platform !== "win32",
				stdio: ["ignore", "pipe", "pipe"],
				env: {
					...process.env,
					PI_WORKER_DEPTH: "1",
					PI_SKIP_VERSION_CHECK: "1",
					[WORKER_MODE_ENV]: task.mode,
					[WORKER_CWD_ENV]: cwd,
					[WORKER_ALLOWED_PATHS_ENV]: JSON.stringify(task.allowedPaths ?? []),
					[WORKER_FORBIDDEN_PATHS_ENV]: JSON.stringify(task.forbiddenPaths ?? []),
				},

			});
			activeChildren.add(child);
			const finish = (exitCode: number, childClosed = true) => {
				if (settled) return;
				settled = true;
				clearTimeout(timeout);
				if (forceKillTimer) clearTimeout(forceKillTimer);
				if (forceSettleTimer) clearTimeout(forceSettleTimer);
				signal?.removeEventListener("abort", abortHandler);
				if (childClosed) activeChildren.delete(child);
				phase = aborted ? "已取消" : timedOut ? "已超时" : exitCode === 0 ? "子进程已完成" : "子进程失败";
				for (const activity of activities) {
					if (activity.status !== "running") continue;
					activity.status = activity.type === "tool" && (exitCode !== 0 || aborted || timedOut) ? "failed" : "completed";
				}
				appendUiActivity(activities, { id: "phase:finished", type: "phase", status: exitCode === 0 && !aborted && !timedOut ? "completed" : "failed", label: phase, at: Date.now() });
				emit(true);
				const usage = usageSnapshot();
				usage.turns = Math.max(usage.turns, settledUsage.turns);
				resolve({ exitCode, stderr: stderr.slice(-16_384), assistantText, actualProvider, actualModel, stopReason, errorMessage, aborted, timedOut, truncated, activities: activities.map((item) => ({ ...item })), toolCalls, usage });
			};
			const terminate = (reason: "abort" | "timeout" | "output") => {
				if (settled || terminating) return;
				terminating = true;
				aborted = reason === "abort";
				timedOut = reason === "timeout";
				setPhase(reason === "abort" ? "正在取消" : reason === "timeout" ? "正在终止超时任务" : "正在终止超限输出", "failed");
				killProcessTree(child, "SIGTERM");
				forceKillTimer = setTimeout(() => {
					if (settled) return;
					killProcessTree(child, "SIGKILL");
					forceSettleTimer = setTimeout(() => {
						if (settled) return;
						errorMessage ||= "Worker 子进程强制终止后未触发 close";
						finish(1, false);
					}, WORKER_FORCE_SETTLE_MS);
					forceSettleTimer.unref();
				}, WORKER_TERMINATE_GRACE_MS);
				forceKillTimer.unref();
			};
			const abortHandler = () => terminate("abort");
			timeout = setTimeout(() => terminate("timeout"), Math.max(1, deadline - Date.now()));
			if (signal?.aborted) abortHandler(); else signal?.addEventListener("abort", abortHandler, { once: true });
			const processEvent = (event: any) => {
				if (event.type === "turn_start") {
					currentTurn = Math.max(currentTurn, Number(event.turnIndex) + 1 || settledUsage.turns + 1);
					activeTurn = true;
					emit(true);
					return;
				}
				if (event.type === "message_update") {
					if (event.message?.role === "assistant") {
						streamUsage = messageUsage(event.message);
						streamUsage.output = Math.max(streamUsage.output, estimateMessageTokens(event.message));
						activeTurn = true;
					}
					const thinking = publicThinking(event.message);
					if (thinking) recordThinking(thinking);
					const delta = event.assistantMessageEvent;
					if (delta?.type === "thinking_delta" && typeof delta.delta === "string") {
						latestThinking = uiSnippet(`${latestThinking}${delta.delta}`, UI_DETAIL_CAP);
						streamUsage.output = Math.max(streamUsage.output, Math.ceil(latestThinking.length / 4));
						recordThinking(latestThinking);
					}
					emit();
					return;
				}
				if (event.type === "message_end" && event.message?.role === "assistant") {
					assistantText = extractText(event.message) || assistantText;
					actualProvider = event.message.provider || actualProvider;
					actualModel = event.message.model || actualModel;
					stopReason = event.message.stopReason || stopReason;
					errorMessage = event.message.errorMessage || errorMessage;
					const reported = messageUsage(event.message, 1);
					if (!reported.input) reported.input = streamUsage.input;
					if (!reported.output) reported.output = streamUsage.output || estimateMessageTokens(event.message);
					if (!reported.cacheRead) reported.cacheRead = streamUsage.cacheRead;
					if (!reported.cacheWrite) reported.cacheWrite = streamUsage.cacheWrite;
					if (!reported.contextTokens) reported.contextTokens = streamUsage.contextTokens;
					settledUsage = addWorkerUsage(settledUsage, reported);
					currentTurn = Math.max(currentTurn, settledUsage.turns);
					streamUsage = emptyWorkerUsage();
					activeTurn = false;
					const thinking = publicThinking(event.message);
					if (thinking) recordThinking(thinking);
					for (const part of event.message.content ?? []) {
						if (part?.type === "toolCall") upsertTool(part.id, part.name, "running", summarizeToolArgs(part.arguments));
					}
					return;
				}
				if (event.type === "tool_execution_start" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, "running", summarizeToolArgs(event.args));
					return;
				}
				if (event.type === "tool_execution_update" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, "running", summarizeToolResult(event.partialResult) ?? summarizeToolArgs(event.args));
					return;
				}
				if (event.type === "tool_execution_end" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, event.isError ? "failed" : "completed", summarizeToolResult(event.result));
					return;
				}
				if ((event.type === "message_end" || event.type === "tool_result_end") && event.message?.role === "toolResult") {
					upsertTool(event.message.toolCallId, event.message.toolName, event.message.isError ? "failed" : "completed", summarizeToolResult(event.message));
				}
			};
			child.stdout.on("data", (chunk: Buffer) => {
				stdoutBytes += chunk.length;
				if (stdoutBytes > MAX_WORKER_STDOUT_BYTES) {
					errorMessage = `Worker 事件流超过 ${MAX_WORKER_STDOUT_BYTES} 字节安全上限`;
					truncated = true;
					terminate("output");
					return;
				}
				stdoutBuffer += chunk.toString("utf8");
				const lines = stdoutBuffer.split("\n");
				stdoutBuffer = lines.pop() || "";
				if (Buffer.byteLength(stdoutBuffer, "utf8") > MAX_WORKER_EVENT_LINE_BYTES) {
					errorMessage = `Worker 协议行超过 ${MAX_WORKER_EVENT_LINE_BYTES} 字节安全上限`;
					truncated = true;
					stdoutBuffer = "";
					terminate("output");
					return;
				}
				for (const line of lines) {
					if (!line.trim()) continue;
					if (Buffer.byteLength(line, "utf8") > MAX_WORKER_EVENT_LINE_BYTES) {
						errorMessage = `Worker 协议行超过 ${MAX_WORKER_EVENT_LINE_BYTES} 字节安全上限`;
						truncated = true;
						terminate("output");
						break;
					}
					try { processEvent(JSON.parse(line)); }
					catch { /* ignore non-JSON diagnostics */ }
				}
			});
			child.stderr.on("data", (chunk: Buffer) => { stderr = `${stderr}${chunk.toString("utf8")}`.slice(-65_536); });
			(child as any).on("error", (error: Error) => {
				activeChildren.delete(child);
				errorMessage = error.message;
				finish(1);
			});
			(child as any).on("close", (code: number | null) => {
				activeChildren.delete(child);
				if (stdoutBuffer.trim()) {
					try { processEvent(JSON.parse(stdoutBuffer)); } catch { /* ignore trailing diagnostics */ }
				}
				finish(code ?? 1);
			});
		});
	} finally {
		release?.();
		await fs.promises.rm(tempDir, { recursive: true, force: true });
	}
}

export function parseStructuredResult(text: string): Record<string, any> | null {
	const cleaned = text.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "");
	try {
		const parsed = JSON.parse(cleaned);
		return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : null;
	} catch {
		const start = cleaned.indexOf("{");
		const end = cleaned.lastIndexOf("}");
		if (start >= 0 && end > start) {
			try { return JSON.parse(cleaned.slice(start, end + 1)); } catch { return null; }
		}
		return null;
	}
}

export function isBlockedFailure(child: ChildResult): boolean {
	const text = `${child.stderr}\n${child.errorMessage ?? ""}\n${child.assistantText}`;
	return /(permission denied|unauthorized|authentication|not found|no such file|command not found|missing dependency|external service|network is unreachable|allowedpaths|git 工作区|权限|认证|不存在|缺少依赖)/i.test(text);
}

export function baseExecution(task: WorkerTask, route: Route | null, attempt: number, warnings: string[]) {
	return {
		requested_preset: task.preset ?? "auto",
		resolved_preset: route?.resolvedPreset ?? null,
		resolved_model_id: route?.modelId ?? null,
		resolved_thinking: route?.thinking ?? null,
		attempt,
		route_reason: route?.routeReason ?? [],
		warnings,
	};
}

export function failureDetails(category: string, reason: string) {
	return {
		category,
		reason,
		retryable: false,
		next_action: "由主 Agent 直接检查并处理失败原因；不要自动重复调用 Worker。",
	};
}

export async function executeTask(task: WorkerTask, config: RoutingConfig, warnings: string[], ctx: ExtensionContext, signal: AbortSignal | undefined, onProgress?: (patch: Partial<WorkerUiTask>) => void, hadConcurrentWriter: () => boolean = () => false): Promise<Record<string, any>> {
	const cwd = resolveTaskCwd(ctx.cwd, task.cwd);
	let route: Route;
	onProgress?.({ status: "running", phase: "解析模型路由" });
	try {
		route = resolveRoute(task, config, ctx);
		onProgress?.({ resolvedPreset: route.resolvedPreset, modelId: route.modelId, thinking: route.thinking, phase: "记录 Git 状态" });
	}
	catch (error) {
		const reason = error instanceof Error ? error.message : String(error);
		const failure = failureDetails("route_or_contract", reason);
		return { status: "blocked", execution: baseExecution(task, null, 0, warnings), failure, summary: [reason], changed_files: [], observed_changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: [failure.next_action] };
	}
	let before: WorkspaceSnapshot | null = null;
	try {
		before = await snapshotWorkspace(cwd, new Set(), signal);
		onProgress?.({ phase: "Git 状态记录完成" });
	} catch (error) {
		const reason = error instanceof Error ? error.message : String(error);
		const failure = failureDetails("workspace_snapshot", reason);
		return { status: "blocked", execution: baseExecution(task, route, 0, warnings), failure, summary: [reason], changed_files: [], observed_changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: [failure.next_action] };
	}
	const systemPrompt = workerPromptBody();
	onProgress?.({ status: "running", resolvedPreset: route.resolvedPreset, modelId: route.modelId, thinking: route.thinking, attempt: 1, phase: "启动 Worker" });
	const child = await runPiWorker(
		task,
		route,
		cwd,
		systemPrompt,
		buildTaskPrompt(task, route, before),
		signal,
		config.defaultTimeoutMs,
		config.maxConcurrentWorkers,
		(progress) => onProgress?.({ phase: progress.phase, activities: progress.activities, toolCalls: progress.toolCalls, usage: progress.usage }),
	);
	onProgress?.({ phase: "校验结果与修改范围", activities: child.activities, toolCalls: child.toolCalls, usage: child.usage });
	const parsed = parseStructuredResult(child.assistantText);
	const delta = before ? await changedSince(before, signal) : { changed: [] as string[] };
	// Evaluate overlap after the snapshot delta: the scheduler mutates this
	// execution context when a sibling starts after this task.
	const attribution = attributeChangedFiles(task, ctx.cwd, delta.changed, hadConcurrentWriter());
	const actualMismatch = Boolean(child.actualProvider && child.actualModel && `${child.actualProvider}/${child.actualModel}` !== route.modelId);
	let status: "completed" | "blocked" | "failed" = parsed?.status === "completed" || parsed?.status === "blocked" || parsed?.status === "failed" ? parsed.status : "failed";
	const risks = Array.isArray(parsed?.risks) ? [...parsed.risks] : [];
	if (attribution.attributedToDeclaredPaths) risks.unshift("任务与兄弟写任务真实重叠并共享 Git worktree；changed_files 已按本任务规范化 allowedPaths 取交集。observed_changed_files 保留快照观察到的完整原始 delta，其中范围外变化的来源无法由共享 worktree 快照证明。");
	if (runtimeShuttingDown || child.aborted || child.timedOut || child.exitCode !== 0 || child.errorMessage || !parsed || actualMismatch) status = isBlockedFailure(child) ? "blocked" : "failed";
	if (actualMismatch) risks.push(`实际模型 ${child.actualProvider}/${child.actualModel} 与请求 ${route.modelId} 不一致`);
	if (child.truncated) risks.push("Worker 事件输出超过上限，已截断");
	const summary = Array.isArray(parsed?.summary) ? parsed.summary : [child.aborted ? "Worker 已取消" : child.timedOut ? "Worker 超时" : child.errorMessage || child.stderr || "Worker 未返回可解析 JSON"];
	let failure: ReturnType<typeof failureDetails> | undefined;
	if (status !== "completed") {
		const category = runtimeShuttingDown || child.aborted ? "cancelled"
			: child.timedOut ? "timeout"
				: actualMismatch ? "model_mismatch"
					: status === "blocked" ? "worker_blocked"
						: child.truncated ? "protocol_output_limit"
							: child.exitCode !== 0 ? "process_exit"
								: child.errorMessage ? "runtime_error"
									: !parsed ? "invalid_result" : "worker_failed";
		const reason = category === "model_mismatch" ? `实际模型与请求不一致: ${child.actualProvider}/${child.actualModel}`
			: category === "cancelled" ? "Worker 已取消"
				: category === "timeout" ? "Worker 超时"
					: String(child.errorMessage ?? summary[0] ?? child.stderr ?? "Worker 执行失败");
		failure = failureDetails(category, reason);
	}
	return {
		status,
		execution: { ...baseExecution(task, route, 1, warnings), actual_model_id: child.actualProvider && child.actualModel ? `${child.actualProvider}/${child.actualModel}` : route.modelId, actual_thinking: route.thinking, usage: child.usage, exit_code: child.exitCode, timed_out: child.timedOut, cancelled: child.aborted || runtimeShuttingDown },
		failure,
		summary,
		changed_files: attribution.changedFiles,
		observed_changed_files: attribution.observedChangedFiles,
		validation: Array.isArray(parsed?.validation) ? parsed.validation : [],
		acceptance: Array.isArray(parsed?.acceptance) ? parsed.acceptance : [],
		findings: Array.isArray(parsed?.findings) ? parsed.findings : [],
		risks,
		out_of_scope: Array.isArray(parsed?.out_of_scope) ? parsed.out_of_scope : [],
		recommended_next_action: failure ? [failure.next_action] : Array.isArray(parsed?.recommended_next_action) ? parsed.recommended_next_action : ["主 Agent 检查实际 diff 和验证证据"],
	};
}
export function resetWorkerRuntime(): void {
	runtimeShuttingDown = false;
}

export function beginWorkerShutdown(): void {
	runtimeShuttingDown = true;
	killAllChildren();
}

