import { spawn } from "node:child_process";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import type { Message } from "@earendil-works/pi-ai";
import type {
	AgentDefinition,
	AgentRunResult,
	SubagentDetails,
	ToolActivity,
	UsageStats,
} from "./types/index.ts";

const MAX_STDERR_BYTES = 128 * 1024;
const MAX_PENDING_LINE_BYTES = 16 * 1024 * 1024;
const DEFAULT_TIMEOUT_MS = 30 * 60 * 1000;
const TASK_ARG_LIMIT = 8000;
const MAX_TOOL_ACTIVITIES = 20;
const TOOL_INPUT_CAP = 120;
const TOOL_OUTPUT_CAP = 160;
const LATEST_THINKING_CAP = 4000;
const PROGRESS_EMIT_INTERVAL_MS = 250;
const THINKING_EMIT_INTERVAL_MS = 150;
const RAW_DISPLAY_SCAN_CAP = 4096;
const SENSITIVE_KEY = /(?:password|passwd|token|secret|api[-_]?key|private[-_]?key|mnemonic|authorization|cookie|credential|auth)/i;

interface RunnerEvent {
	type?: string;
	message?: Message;
	toolCallId?: string;
	toolName?: string;
	args?: unknown;
	partialResult?: unknown;
	result?: unknown;
	isError?: boolean;
}

export function sanitizeDisplayText(value: string): string {
	const output: string[] = [];
	const skipStringSequence = (start: number, allowBell: boolean): number => {
		let index = start;
		while (index < value.length) {
			const code = value.charCodeAt(index);
			if (allowBell && code === 0x07) return index + 1;
			if (code === 0x9c) return index + 1;
			if (code === 0x1b && value.charCodeAt(index + 1) === 0x5c) return index + 2;
			index++;
		}
		return value.length;
	};
	const skipCsi = (start: number): number => {
		let index = start;
		while (index < value.length) {
			const code = value.charCodeAt(index++);
			if (code >= 0x40 && code <= 0x7e) return index;
		}
		return value.length;
	};

	let index = 0;
	while (index < value.length) {
		const code = value.charCodeAt(index);
		if (code === 0x1b) {
			const next = value.charCodeAt(index + 1);
			if (next === 0x5b) index = skipCsi(index + 2);
			else if (next === 0x5d) index = skipStringSequence(index + 2, true);
			else if (next === 0x50 || next === 0x58 || next === 0x5e || next === 0x5f) {
				index = skipStringSequence(index + 2, false);
			} else index += Math.min(2, value.length - index);
			continue;
		}
		if (code === 0x9b) {
			index = skipCsi(index + 1);
			continue;
		}
		if (code === 0x9d || code === 0x90 || code === 0x98 || code === 0x9e || code === 0x9f) {
			index = skipStringSequence(index + 1, code === 0x9d);
			continue;
		}
		if ((code < 0x20 && code !== 0x09 && code !== 0x0a && code !== 0x0d) || (code >= 0x7f && code <= 0x9f)) {
			output.push(" ");
			index++;
			continue;
		}
		output.push(value[index]!);
		index++;
	}
	return output.join("");
}

function redactSensitiveText(value: string): string {
	return value
		.replace(/\b((?:[a-z0-9-]*auth(?:orization)?|x-auth)\s*:\s*)[^'"\r\n]+/gi, "$1[已隐藏]")
		.replace(/\b(Cookie\s*:\s*)[^'"\r\n]+/gi, "$1[已隐藏]")
		.replace(
			/\b((?:[a-z0-9]+[_-])*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|mnemonic|credential|auth|authorization|cookie)(?:[_-][a-z0-9]+)*\s*[:=]\s*)\\?['"][\s\S]*/gi,
			"$1[已隐藏]",
		)
		.replace(/([?&][a-z0-9_.-]*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|credential|auth)[a-z0-9_.-]*=)[^&\s'"]+/gi, "$1[已隐藏]")
		.replace(/(--[a-z0-9-]*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|credential|auth)[a-z0-9-]*\s+)\\?['"][\s\S]*/gi, "$1[已隐藏]")
		.replace(/(--[a-z0-9-]*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|credential|auth)[a-z0-9-]*\s+)[^\s]+/gi, "$1[已隐藏]")
		.replace(
			/\b((?:[a-z0-9]+[_-])*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|mnemonic|credential|auth|authorization|cookie)(?:[_-][a-z0-9]+)*\s*[:=]\s*)[^\s,;&]+/gi,
			"$1[已隐藏]",
		);
}

function oneLine(value: string, maxLength: number): string {
	const bounded = value.length > RAW_DISPLAY_SCAN_CAP ? `${value.slice(0, RAW_DISPLAY_SCAN_CAP)}…` : value;
	const compact = redactSensitiveText(sanitizeDisplayText(bounded)).replace(/\s+/g, " ").trim();
	return compact.length > maxLength ? `${compact.slice(0, maxLength - 1)}…` : compact;
}

export function sanitizeDisplaySnippet(value: string, maxLength: number): string {
	return oneLine(value, maxLength);
}

function compactValue(value: unknown): string {
	if (value === undefined || typeof value === "function" || typeof value === "symbol") return "";
	if (typeof value === "string") return oneLine(value, 90);
	if (typeof value === "number" || typeof value === "boolean" || value === null) return String(value);
	try {
		return oneLine(
			JSON.stringify(value, (key, nested) => (SENSITIVE_KEY.test(key) ? "[已隐藏]" : nested)),
			90,
		);
	} catch {
		return "[无法显示]";
	}
}

function summarizeToolInput(args: unknown): string | undefined {
	if (!args || typeof args !== "object" || Array.isArray(args)) {
		const value = compactValue(args);
		return value && value !== "undefined" ? value : undefined;
	}

	const record = args as Record<string, unknown>;
	const preferred = ["command", "path", "pattern", "query", "url", "agent", "task", "offset", "limit"];
	const keys = [
		...preferred.filter((key) => key in record),
		...Object.keys(record).filter(
			(key) => !preferred.includes(key) && !["content", "oldText", "newText", "edits"].includes(key),
		),
	].slice(0, 3);
	const summary = keys
		.map((key) => `${key}=${SENSITIVE_KEY.test(key) ? "[已隐藏]" : compactValue(record[key])}`)
		.join(" · ");
	return summary ? oneLine(summary, TOOL_INPUT_CAP) : undefined;
}

function extractLatestThinking(message: Extract<Message, { role: "assistant" }>): string | undefined {
	const parts = message.content.filter(
		(part): part is Extract<(typeof message.content)[number], { type: "thinking" }> => part.type === "thinking",
	);
	const visible = parts
		.map((part) => part.thinking.trim())
		.filter(Boolean)
		.join("\n\n");
	if (visible) return visible.length > LATEST_THINKING_CAP ? `…${visible.slice(-(LATEST_THINKING_CAP - 1))}` : visible;
	return parts.some((part) => part.redacted) ? "思考内容已由模型提供商隐藏" : undefined;
}

function summarizeToolOutput(value: unknown): string | undefined {
	if (value === undefined || value === null) return undefined;
	if (typeof value === "object" && !Array.isArray(value)) {
		const record = value as Record<string, unknown>;
		if (Array.isArray(record.content)) {
			const text = record.content
				.map((part) =>
					part && typeof part === "object" && (part as Record<string, unknown>).type === "text"
						? String((part as Record<string, unknown>).text ?? "")
						: "",
				)
				.filter(Boolean)
				.join(" ");
			if (text) return oneLine(text, TOOL_OUTPUT_CAP);
		}
	}
	const summary = compactValue(value);
	return summary ? oneLine(summary, TOOL_OUTPUT_CAP) : undefined;
}

export interface RunAgentOptions {
	cwd: string;
	agent: AgentDefinition;
	task: string;
	step?: number;
	launchModel?: string;
	displayModel?: string;
	thinking?: string;
	extensionPaths?: string[];
	excludeTools?: string[];
	signal?: AbortSignal;
	onUpdate?: (result: AgentRunResult) => void;
}

function emptyUsage(): UsageStats {
	return {
		input: 0,
		output: 0,
		cacheRead: 0,
		cacheWrite: 0,
		cost: 0,
		contextTokens: 0,
		turns: 0,
	};
}

function getPiInvocation(args: string[]): { command: string; args: string[] } {
	const override = process.env.PI_SUBAGENT_PI_BINARY?.trim();
	if (override) return { command: override, args };

	const currentScript = process.argv[1];
	if (currentScript && !currentScript.startsWith("/$bunfs/root/") && fs.existsSync(currentScript)) {
		return { command: process.execPath, args: [currentScript, ...args] };
	}

	const executable = path.basename(process.execPath).toLowerCase();
	if (!/^(node|bun)(\.exe)?$/.test(executable)) {
		return { command: process.execPath, args };
	}
	return { command: "pi", args };
}

function appendStderrTail(current: Buffer, chunk: Buffer): Buffer {
	const combined = Buffer.concat([current, chunk]);
	if (combined.length <= MAX_STDERR_BYTES) return combined;
	let start = combined.length - MAX_STDERR_BYTES;
	while (start < combined.length && (combined[start]! & 0xc0) === 0x80) start++;
	return combined.subarray(start);
}

function updateUsage(result: AgentRunResult, message: Message): void {
	if (message.role !== "assistant") return;
	result.usage.turns++;
	const usage = message.usage;
	if (usage) {
		result.usage.input += usage.input ?? 0;
		result.usage.output += usage.output ?? 0;
		result.usage.cacheRead += usage.cacheRead ?? 0;
		result.usage.cacheWrite += usage.cacheWrite ?? 0;
		result.usage.cost += usage.cost?.total ?? 0;
		result.usage.contextTokens = usage.totalTokens ?? 0;
	}
	if (!result.model && message.model) result.model = message.model;
	if (message.stopReason) result.stopReason = message.stopReason;
	if (message.errorMessage) result.errorMessage = message.errorMessage;
}

export function getFinalOutput(messages: Message[]): string {
	for (let index = messages.length - 1; index >= 0; index--) {
		const message = messages[index];
		if (message?.role !== "assistant") continue;
		const text = message.content
			.filter((part): part is Extract<(typeof message.content)[number], { type: "text" }> => part.type === "text")
			.map((part) => part.text)
			.join("\n")
			.trim();
		if (text) return text;
	}
	return "";
}

export function isFailedResult(result: AgentRunResult): boolean {
	return (
		result.status === "failed" ||
		(result.exitCode !== null && result.exitCode !== 0) ||
		result.stopReason === "error" ||
		result.stopReason === "aborted"
	);
}

export function getFailureMessage(result: AgentRunResult): string {
	return result.errorMessage || result.stderr.trim() || getFinalOutput(result.messages) || "子代理未返回错误详情";
}

function createTempInput(agent: AgentDefinition, task: string): { dir: string; promptPath: string; taskArg: string } {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-subagents-zh-"));
	const promptPath = path.join(dir, `${agent.name}.md`);
	fs.writeFileSync(promptPath, agent.systemPrompt, { encoding: "utf8", mode: 0o600 });

	if (task.length <= TASK_ARG_LIMIT) return { dir, promptPath, taskArg: `Task: ${task}` };
	const taskPath = path.join(dir, "task.md");
	fs.writeFileSync(taskPath, `Task: ${task}`, { encoding: "utf8", mode: 0o600 });
	return { dir, promptPath, taskArg: `@${taskPath}` };
}

export async function runAgent(options: RunAgentOptions): Promise<AgentRunResult> {
	const result: AgentRunResult = {
		agent: options.agent.name,
		task: options.task,
		status: "running",
		exitCode: null,
		messages: [],
		toolActivities: [],
		toolCalls: 0,
		stderr: "",
		usage: emptyUsage(),
		model: options.displayModel,
		thinking: options.thinking,
		step: options.step,
	};
	const emitUpdate = () =>
		options.onUpdate?.({
			...result,
			messages: [...result.messages],
			toolActivities: result.toolActivities.map((activity) => ({ ...activity })),
			usage: { ...result.usage },
		});
	const seenToolIds = new Set<string>();
	const progressEmittedAt = new Map<string, number>();
	let thinkingEmittedAt = 0;
	const upsertTool = (id: string, name: string, args?: unknown): ToolActivity => {
		let activity = result.toolActivities.find((item) => item.id === id);
		const safeName = sanitizeDisplaySnippet(name, 48) || "未知工具";
		if (!activity) {
			activity = { id, name: safeName, status: "running" };
			result.toolActivities.push(activity);
			if (!seenToolIds.has(id)) {
				seenToolIds.add(id);
				result.toolCalls++;
			}
			if (result.toolActivities.length > MAX_TOOL_ACTIVITIES) {
				const removable = result.toolActivities.findIndex((item) => item.status !== "running");
				result.toolActivities.splice(removable >= 0 ? removable : 0, 1);
			}
		}
		activity.name = safeName;
		const input = summarizeToolInput(args);
		if (input) activity.input = input;
		return activity;
	};
	const completeTool = (id: string, name: string, isError: boolean, value?: unknown) => {
		const activity = upsertTool(id, name);
		activity.status = isError ? "failed" : "completed";
		const output = summarizeToolOutput(value);
		if (output) activity.output = output;
		progressEmittedAt.delete(id);
	};
	const recordToolResult = (message: Extract<Message, { role: "toolResult" }>) => {
		if (
			!result.messages.some(
				(item) => item.role === "toolResult" && item.toolCallId === message.toolCallId,
			)
		) {
			result.messages.push(message);
		}
		completeTool(message.toolCallId, message.toolName, message.isError, message);
	};
	const temp = createTempInput(options.agent, options.task);
	let stderrTail: Buffer<ArrayBufferLike> = Buffer.alloc(0);
	let pending = "";
	let aborted = false;
	let timedOut = false;
	let protocolError: string | undefined;
	let spawnError: string | undefined;

	const args = [
		"--mode",
		"json",
		"--print",
		"--no-session",
		"--no-extensions",
		"--no-skills",
		"--no-prompt-templates",
	];
	if (options.excludeTools?.length) args.push("--exclude-tools", options.excludeTools.join(","));
	for (const extensionPath of options.extensionPaths ?? []) args.push("--extension", extensionPath);
	if (options.launchModel) args.push("--model", options.launchModel);
	if (options.thinking) args.push("--thinking", options.thinking);
	args.push("--append-system-prompt", temp.promptPath, temp.taskArg);

	try {
		const exitCode = await new Promise<number>((resolve) => {
			const invocation = getPiInvocation(args);
			const child = spawn(invocation.command, invocation.args, {
				cwd: options.cwd,
				shell: false,
				stdio: ["ignore", "pipe", "pipe"],
				env: {
					...process.env,
					PI_SUBAGENT_CHILD: "1",
					PI_SUBAGENT_PARENT_SESSION: process.env.PI_SESSION_ID ?? process.env.PI_SUBAGENT_PARENT_SESSION ?? "",
				},
			});

			const stopChild = () => {
				if (child.exitCode !== null) return;
				child.kill("SIGTERM");
				setTimeout(() => {
					if (child.exitCode === null) child.kill("SIGKILL");
				}, 5000).unref();
			};

			const processLine = (line: string) => {
				if (!line.trim()) return;
				let event: RunnerEvent;
				try {
					event = JSON.parse(line) as RunnerEvent;
				} catch {
					return;
				}

				if (event.type === "message_update" && event.message?.role === "assistant") {
					const thinking = extractLatestThinking(event.message);
					if (thinking && thinking !== result.latestThinking) {
						result.latestThinking = thinking;
						const now = Date.now();
						if (now - thinkingEmittedAt >= THINKING_EMIT_INTERVAL_MS) {
							thinkingEmittedAt = now;
							emitUpdate();
						}
					}
					return;
				}

				if (event.type === "message_end" && event.message?.role === "assistant") {
					result.messages.push(event.message);
					result.latestThinking = extractLatestThinking(event.message) ?? result.latestThinking;
					updateUsage(result, event.message);
					for (const part of event.message.content) {
						if (part.type === "toolCall") upsertTool(part.id, part.name, part.arguments);
					}
					emitUpdate();
					return;
				}

				if (
					(event.type === "message_end" || event.type === "tool_result_end") &&
					event.message?.role === "toolResult"
				) {
					recordToolResult(event.message);
					emitUpdate();
					return;
				}

				if (event.type === "tool_execution_start" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, event.args);
					emitUpdate();
					return;
				}

				if (event.type === "tool_execution_update" && event.toolCallId && event.toolName) {
					const activity = upsertTool(event.toolCallId, event.toolName, event.args);
					const output = summarizeToolOutput(event.partialResult);
					if (output) activity.output = output;
					const now = Date.now();
					if (now - (progressEmittedAt.get(event.toolCallId) ?? 0) >= PROGRESS_EMIT_INTERVAL_MS) {
						progressEmittedAt.set(event.toolCallId, now);
						emitUpdate();
					}
					return;
				}

				if (event.type === "tool_execution_end" && event.toolCallId && event.toolName) {
					completeTool(event.toolCallId, event.toolName, Boolean(event.isError), event.result);
					emitUpdate();
				}
			};

			child.stdout.setEncoding("utf8");
			child.stdout.on("data", (chunk: string) => {
				pending += chunk;
				if (Buffer.byteLength(pending, "utf8") > MAX_PENDING_LINE_BYTES && !pending.includes("\n")) {
					protocolError = `子代理协议行超过 ${MAX_PENDING_LINE_BYTES / 1024 / 1024} MiB 限制`;
					pending = "";
					stopChild();
					return;
				}
				const lines = pending.split("\n");
				pending = lines.pop() ?? "";
				for (const line of lines) processLine(line);
			});
			child.stderr.on("data", (chunk: Buffer) => {
				stderrTail = appendStderrTail(stderrTail, chunk);
			});

			const abortHandler = () => {
				aborted = true;
				stopChild();
			};
			if (options.signal?.aborted) abortHandler();
			else options.signal?.addEventListener("abort", abortHandler, { once: true });

			const timeout = setTimeout(() => {
				timedOut = true;
				stopChild();
			}, DEFAULT_TIMEOUT_MS);
			timeout.unref();

			child.on("error", (error) => {
				spawnError = error.message;
			});
			child.on("close", (code) => {
				clearTimeout(timeout);
				options.signal?.removeEventListener("abort", abortHandler);
				if (pending.trim()) processLine(pending);
				resolve(code ?? 1);
			});
		});

		result.exitCode = exitCode;
		result.stderr = stderrTail.toString("utf8");
		if (aborted) result.errorMessage = "子代理执行已取消";
		else if (timedOut) result.errorMessage = "子代理执行超时（30 分钟）";
		else if (protocolError) result.errorMessage = protocolError;
		else if (spawnError) result.errorMessage = `无法启动子代理: ${spawnError}`;

		result.status =
			result.errorMessage || exitCode !== 0 || result.stopReason === "error" || result.stopReason === "aborted"
				? "failed"
				: "completed";
		for (const activity of result.toolActivities) {
			if (activity.status !== "running") continue;
			activity.status = result.status === "failed" ? "failed" : "completed";
			if (result.status === "failed" && !activity.output) {
				activity.output = oneLine(result.errorMessage || "子代理在工具执行期间结束", TOOL_OUTPUT_CAP);
			}
		}
		emitUpdate();
		return result;
	} finally {
		fs.rmSync(temp.dir, { recursive: true, force: true });
	}
}

export function makeDetails(
	mode: SubagentDetails["mode"],
	results: AgentRunResult[],
	totalSteps: number,
	model?: string,
	thinking?: string,
): SubagentDetails {
	return { mode, results, totalSteps, model, thinking };
}
