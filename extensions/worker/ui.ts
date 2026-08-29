import type { Theme } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";
import type { WorkerUiActivity, WorkerUiDetails, WorkerUiStatus, WorkerUiTask, WorkerUsage } from "./types";

export const UI_ACTIVITY_LIMIT = 20;
export const UI_RECENT_ACTIVITY_LIMIT = 10;
export const UI_DETAIL_CAP = 160;
export const UI_SENSITIVE_KEY = /(?:password|passwd|token|secret|api[-_]?key|private[-_]?key|mnemonic|authorization|cookie|credential|auth)/i;

export function sanitizeUiText(value: string): string {
	const stripped = value
		.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
		.replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/g, " ")
		.replace(/-----BEGIN [^-\r\n]*(?:PRIVATE KEY|SECRET)[^-\r\n]*-----[\s\S]*?-----END [^-\r\n]*(?:PRIVATE KEY|SECRET)[^-\r\n]*-----/gi, "[PEM 已隐藏]")
		.replace(/\b((?:authorization|cookie|x-auth)\s*:\s*)[^'"\r\n]+/gi, "$1[已隐藏]")
		.replace(/\b((?:[a-z0-9]+[_-])*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|mnemonic|credential|auth|authorization|cookie)(?:[_-][a-z0-9]+)*\s*[:=]\s*)[^\s,;&]+/gi, "$1[已隐藏]")
		.replace(/\b(?:[a-f0-9]{64,}|[A-Za-z0-9+/_=-]{64,})\b/g, "[高熵内容已隐藏]");
	const compact = stripped.trim();
	const words = compact.split(/\s+/);
	if ([12, 15, 18, 21, 24].includes(words.length) && words.every((word) => /^[a-z]{3,10}$/.test(word))) return "[疑似助记词已隐藏]";
	return stripped;
}

export function sanitizeStructuredValue(value: unknown, key = "", depth = 0): unknown {
	if (UI_SENSITIVE_KEY.test(key)) return "[已隐藏]";
	if (typeof value === "string") return truncateUtf8Text(sanitizeUiText(value), 16 * 1024);
	if (value === null || typeof value !== "object") return value;
	if (depth >= 12) return "[内容层级过深]";
	if (Array.isArray(value)) return value.map((item) => sanitizeStructuredValue(item, "", depth + 1));
	return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([nestedKey, nested]) => [nestedKey, sanitizeStructuredValue(nested, nestedKey, depth + 1)]));
}

type ResultCompactProfile = {
	stringBytes: number;
	nestedItems: number;
	objectKeys: number;
	summary: number;
	changedFiles: number;
	validation: number;
	acceptance: number;
	findings: number;
	risks: number;
	outOfScope: number;
	nextActions: number;
};

const RESULT_COMPACT_PROFILES: Record<"standard" | "minimal", ResultCompactProfile> = {
	standard: { stringBytes: 768, nestedItems: 10, objectKeys: 10, summary: 4, changedFiles: 50, validation: 8, acceptance: 8, findings: 10, risks: 6, outOfScope: 4, nextActions: 4 },
	minimal: { stringBytes: 256, nestedItems: 5, objectKeys: 8, summary: 2, changedFiles: 20, validation: 3, acceptance: 3, findings: 4, risks: 3, outOfScope: 2, nextActions: 2 },
};

function compactResultText(value: string, maxBytes: number): string {
	if (Buffer.byteLength(value, "utf8") <= maxBytes) return value;
	return `${truncateUtf8Text(value, Math.max(0, maxBytes - 3))}…`;
}

function compactResultValue(value: unknown, profile: ResultCompactProfile, depth = 0): unknown {
	if (typeof value === "string") return compactResultText(value, profile.stringBytes);
	if (value === null || typeof value !== "object") return value;
	if (depth >= 5) return "[内容已压缩]";
	if (Array.isArray(value)) return value.slice(0, profile.nestedItems).map((item) => compactResultValue(item, profile, depth + 1));
	return Object.fromEntries(
		Object.entries(value as Record<string, unknown>)
			.slice(0, profile.objectKeys)
			.map(([key, nested]) => [key, compactResultValue(nested, profile, depth + 1)]),
	);
}

function compactExecution(value: unknown, profile: ResultCompactProfile): unknown {
	if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
	const execution = value as Record<string, any>;
	const usage = execution.usage && typeof execution.usage === "object" ? execution.usage as Record<string, any> : undefined;
	return compactResultValue({
		resolved_preset: execution.resolved_preset,
		actual_model_id: execution.actual_model_id ?? execution.resolved_model_id,
		actual_thinking: execution.actual_thinking ?? execution.resolved_thinking,
		attempt: execution.attempt,
		usage: usage ? {
			input: usage.input,
			output: usage.output,
			cacheRead: usage.cacheRead,
			cacheWrite: usage.cacheWrite,
			turns: usage.turns,
		} : undefined,
		exit_code: execution.exit_code,
		timed_out: execution.timed_out,
		cancelled: execution.cancelled,
		warnings: execution.warnings,
	}, profile);
}

export function compactWorkerResult(result: Record<string, any>, level: "standard" | "minimal" = "standard"): Record<string, any> {
	const profile = RESULT_COMPACT_PROFILES[level];
	const take = (key: string, limit: number) => Array.isArray(result[key])
		? result[key].slice(0, limit).map((item: unknown) => compactResultValue(item, profile))
		: [];
	return {
		status: result.status,
		execution: compactExecution(result.execution, profile),
		failure: compactResultValue(result.failure, profile),
		summary: take("summary", profile.summary),
		changed_files: take("changed_files", profile.changedFiles),
		validation: take("validation", profile.validation),
		acceptance: take("acceptance", profile.acceptance),
		findings: take("findings", profile.findings),
		risks: take("risks", profile.risks),
		out_of_scope: take("out_of_scope", profile.outOfScope),
		recommended_next_action: take("recommended_next_action", profile.nextActions),
	};
}

export function serializePayload(payload: Record<string, any>, maxBytes: number): { payload: Record<string, any>; text: string } {
	let bounded = payload;
	let text = JSON.stringify(bounded, null, 2);
	if (Buffer.byteLength(text, "utf8") <= maxBytes) return { payload: bounded, text };
	bounded = Array.isArray(payload.results)
		? { status: payload.status, truncated: true, results: payload.results.map((item: Record<string, any>) => compactWorkerResult(item, "minimal")) }
		: { ...compactWorkerResult(payload, "minimal"), truncated: true };
	text = JSON.stringify(bounded, null, 2);
	if (Buffer.byteLength(text, "utf8") <= maxBytes) return { payload: bounded, text };
	bounded = Array.isArray(payload.results)
		? { status: payload.status, truncated: true, results: payload.results.map((item: Record<string, any>, index: number) => ({ index, status: item.status })) }
		: { status: payload.status, truncated: true, summary: ["Worker 结果超过输出上限，详细字段已省略"] };
	text = JSON.stringify(bounded, null, 2);
	if (Buffer.byteLength(text, "utf8") <= maxBytes) return { payload: bounded, text };
	bounded = { status: "failed", truncated: true, summary: ["Worker 结果超过输出上限"] };
	return { payload: bounded, text: JSON.stringify(bounded) };
}

export function truncateUtf8Text(value: string, maxBytes: number): string {
	const bytes = Buffer.from(value, "utf8");
	if (bytes.length <= maxBytes) return value;
	let end = maxBytes;
	while (end > 0 && (bytes[end]! & 0xc0) === 0x80) end--;
	return bytes.subarray(0, end).toString("utf8");
}

export function uiSnippet(value: string, maxLength: number): string {
	const compact = sanitizeUiText(value).replace(/\s+/g, " ").trim();
	return compact.length > maxLength ? `${compact.slice(0, Math.max(0, maxLength - 1))}…` : compact;
}

export function compactUiValue(value: unknown, maxLength = UI_DETAIL_CAP): string | undefined {
	if (value === undefined || value === null) return undefined;
	if (typeof value === "string") return uiSnippet(value, maxLength) || undefined;
	if (typeof value === "number" || typeof value === "boolean") return String(value);
	try {
		return uiSnippet(JSON.stringify(value, (key, nested) => UI_SENSITIVE_KEY.test(key) ? "[已隐藏]" : nested), maxLength) || undefined;
	} catch {
		return "[无法显示]";
	}
}

export function summarizeToolArgs(args: unknown): string | undefined {
	if (!args || typeof args !== "object" || Array.isArray(args)) return compactUiValue(args, 120);
	const record = args as Record<string, unknown>;
	const preferred = ["command", "path", "pattern", "query", "url", "task", "offset", "limit"];
	const keys = [...preferred.filter((key) => key in record), ...Object.keys(record).filter((key) => !preferred.includes(key) && !["content", "oldText", "newText", "edits"].includes(key))].slice(0, 3);
	const text = keys.map((key) => `${key}=${UI_SENSITIVE_KEY.test(key) ? "[已隐藏]" : compactUiValue(record[key], 80) ?? ""}`).join(" · ");
	return text ? uiSnippet(text, 140) : undefined;
}

export function summarizeToolResult(value: unknown): string | undefined {
	if (value && typeof value === "object" && !Array.isArray(value)) {
		const content = (value as Record<string, unknown>).content;
		if (Array.isArray(content)) {
			const text = content.map((part) => part && typeof part === "object" && (part as Record<string, unknown>).type === "text" ? String((part as Record<string, unknown>).text ?? "") : "").filter(Boolean).join(" ");
			if (text) return uiSnippet(text, UI_DETAIL_CAP);
		}
	}
	return compactUiValue(value, UI_DETAIL_CAP);
}

export function publicThinking(message: any): string | undefined {
	if (!message || !Array.isArray(message.content)) return undefined;
	const thinking = message.content.filter((part: any) => part?.type === "thinking" && typeof part.thinking === "string").map((part: any) => part.thinking.trim()).filter(Boolean).join("\n");
	return thinking ? uiSnippet(thinking, UI_DETAIL_CAP) : undefined;
}

export function appendUiActivity(activities: WorkerUiActivity[], activity: WorkerUiActivity): void {
	const existing = activities.findIndex((item) => item.id === activity.id);
	if (existing >= 0) activities[existing] = activity;
	else activities.push(activity);
	if (activities.length > UI_ACTIVITY_LIMIT) activities.splice(0, activities.length - UI_ACTIVITY_LIMIT);
}

export function emptyWorkerUsage(): WorkerUsage {
	return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 0 };
}

export function addWorkerUsage(left: WorkerUsage, right: WorkerUsage): WorkerUsage {
	return {
		input: left.input + right.input,
		output: left.output + right.output,
		cacheRead: left.cacheRead + right.cacheRead,
		cacheWrite: left.cacheWrite + right.cacheWrite,
		contextTokens: Math.max(left.contextTokens, right.contextTokens),
		turns: left.turns + right.turns,
	};
}

export function messageUsage(message: any, turns = 0): WorkerUsage {
	const usage = message?.usage ?? {};
	return {
		input: Number(usage.input) || 0,
		output: Number(usage.output) || 0,
		cacheRead: Number(usage.cacheRead) || 0,
		cacheWrite: Number(usage.cacheWrite) || 0,
		contextTokens: Number(usage.totalTokens) || 0,
		turns,
	};
}

export function estimateMessageTokens(message: any): number {
	if (!Array.isArray(message?.content)) return 0;
	let chars = 0;
	for (const part of message.content) {
		if (part?.type === "text" && typeof part.text === "string") chars += part.text.length;
		else if (part?.type === "thinking" && typeof part.thinking === "string") chars += part.thinking.length;
		else if (part?.type === "toolCall") chars += JSON.stringify(part.arguments ?? {}).length;
	}
	return Math.ceil(chars / 4);
}

export function formatTokenCount(value: number): string {
	if (!Number.isFinite(value) || value <= 0) return "0";
	if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
	if (value >= 1_000) return `${Math.round(value / 1_000)}k`;
	return String(Math.round(value));
}

export function workerUsageText(usage: WorkerUsage, active = false): string {
	const turn = usage.turns || (active ? 1 : 0);
	const tokenUsage = [usage.input ? `↑${formatTokenCount(usage.input)}` : undefined, usage.output ? `↓${formatTokenCount(usage.output)}` : undefined].filter(Boolean).join(" ");
	return [turn ? `Turn ${turn}` : undefined, tokenUsage || undefined].filter(Boolean).join(" · ");
}

export function cloneUiDetails(details: WorkerUiDetails): WorkerUiDetails {
	return {
		...details,
		tasks: details.tasks.map((task) => ({ ...task, activities: task.activities.map((activity) => ({ ...activity })), usage: { ...task.usage } })),
	};
}

export function uiStatusIcon(status: WorkerUiStatus, theme: Theme): string {
	if (status === "queued") return theme.fg("dim", "○");
	if (status === "running") return theme.fg("warning", "◌");
	if (status === "completed") return theme.fg("success", "✓");
	if (status === "blocked") return theme.fg("warning", "!");
	return theme.fg("error", "✗");
}

export function uiActivityLine(activity: WorkerUiActivity, theme: Theme): string {
	if (activity.type === "thinking") return `${theme.fg("mdLink", "!")} ${theme.fg("dim", uiSnippet(activity.detail ?? activity.label, 150))}`;
	const icon = activity.status === "running" ? theme.fg("warning", "→") : activity.status === "completed" ? theme.fg("success", "✓") : theme.fg("error", "✗");
	const label = activity.type === "tool" ? theme.fg("accent", activity.label) : theme.fg("muted", activity.label);
	return `${icon} ${label}${activity.detail ? theme.fg("dim", ` · ${uiSnippet(activity.detail, 96)}`) : ""}`;
}

export function uiDuration(task: WorkerUiTask): string | undefined {
	if (!task.startedAt) return undefined;
	const elapsed = (task.finishedAt ?? Date.now()) - task.startedAt;
	if (elapsed < 1000) return `${elapsed}ms`;
	if (elapsed < 60_000) return `${(elapsed / 1000).toFixed(elapsed < 10_000 ? 1 : 0)}s`;
	return `${Math.floor(elapsed / 60_000)}m${Math.floor((elapsed % 60_000) / 1000)}s`;
}

export function workerConclusions(task: WorkerUiTask): string[] {
	if (task.status !== "completed" || !Array.isArray(task.result?.summary)) return [];
	return task.result.summary
		.map((item) => compactUiValue(item, UI_DETAIL_CAP))
		.filter((item): item is string => Boolean(item));
}

export function renderWorkerDetails(details: WorkerUiDetails, theme: Theme) {
	let text = "";
	for (const [position, task] of details.tasks.entries()) {
		if (position > 0) text += `\n${theme.fg("borderMuted", "─".repeat(24))}\n`;
		const duration = uiDuration(task);
		const usage = workerUsageText(task.usage, task.status === "running");
		const preset = task.resolvedPreset ?? task.requestedPreset;
		const runtime = [preset, usage, duration].filter(Boolean).join(" · ");
		text += `${uiStatusIcon(task.status, theme)} ${theme.fg("accent", task.mode)}${runtime ? theme.fg("muted", ` · ${runtime}`) : ""}`;
		text += `\n  ${theme.fg("dim", uiSnippet(task.objective, 110))}`;
		const conclusions = workerConclusions(task);
		for (const activity of task.activities.slice(-UI_RECENT_ACTIVITY_LIMIT)) {
			text += `\n  ${uiActivityLine(activity, theme)}`;
			if (conclusions.length && activity.id === "phase:finished" && activity.status === "completed") {
				text += theme.fg("muted", "，结论:");
				for (const conclusion of conclusions) text += `\n    ${theme.fg("toolOutput", `• ${conclusion}`)}`;
			}
		}
	}
	return new Text(text, 0, 0);
}
