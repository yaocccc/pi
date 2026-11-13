import { colorToolTokens, formatToolTokens } from './tool-tokens.ts';
import type { Theme } from "@earendil-works/pi-coding-agent";
import { Text, stripTerminalSequences, truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import type { WorkerUiActivity, WorkerUiDetails, WorkerUiStatus, WorkerUiTask, WorkerUsage } from "../../worker/types.ts";

/** Native Pi presentation only. Data belongs to the Worker capability. */
export * from "../../worker/data.ts";
import { compactUiValue, sanitizeUiText, uiSnippet, UI_DETAIL_CAP } from "../../worker/data.ts";

export const UI_RECENT_ACTIVITY_LIMIT = 10;
export const UI_TOOL_LINE_WIDTH = 70;

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
	if (activity.type !== "tool") return `${icon} ${label}${activity.detail ? theme.fg("dim", ` · ${uiSnippet(activity.detail, 96)}`) : ""}`;
	const tokens = activity.status !== "running" && Number.isFinite(activity.contextSizeEstimate) && activity.contextSizeEstimate! >= 0
		? theme.fg("dim", " · ") + colorToolTokens(activity.contextSizeEstimate!, formatToolTokens(activity.contextSizeEstimate!).replace(/ tok$/, ""), theme) : "";
	const prefix = `${icon} ${label}${tokens}`;
	const detailWidth = UI_TOOL_LINE_WIDTH - visibleWidth(prefix) - 3; // " · "
	// truncateToWidth adds full ANSI resets when clipping; remove them before applying the theme color.
	const detail = activity.detail && detailWidth > 0
		? stripTerminalSequences(truncateToWidth(uiSnippet(activity.detail, UI_DETAIL_CAP), detailWidth, "…"))
		: undefined;
	return `${prefix}${detail ? theme.fg("dim", ` · ${detail}`) : ""}`;
}

export function uiDuration(task: Pick<WorkerUiTask, 'startedAt' | 'finishedAt'>, now = Date.now()): string | undefined {
	if (!task.startedAt) return undefined;
	const elapsed = Math.max(0, (task.finishedAt ?? now) - task.startedAt);
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

export function renderWorkerDetails(details: WorkerUiDetails, theme: Theme, options: { expanded?: boolean; snapshot?: boolean } = {}) {
	let text = "";
	for (const error of details.controlErrors ?? []) text += `${theme.fg("error", `控制错误：${sanitizeUiText(error.message)}`)}\n`;
	for (const [position, task] of details.tasks.entries()) {
		if (position > 0) text += `\n${theme.fg("borderMuted", "─".repeat(24))}\n`;
		const duration = uiDuration(task, options.snapshot ? details.snapshotAt ?? details.finishedAt ?? details.startedAt : Date.now());
		const usage = workerUsageText(task.usage, task.status === "running");
		const preset = task.resolvedPreset ?? task.requestedPreset;
		const sessionIcon = task.sessionMode === "fork" ? "" : task.sessionMode === "new" ? "󰐖" : undefined;
		const runtime = [preset, usage, duration].filter(Boolean).join(" · ");
		text += `${uiStatusIcon(task.status, theme)} ${theme.fg("accent", task.mode)}${sessionIcon ? ` ${theme.fg("muted", sessionIcon)}` : ""}${runtime ? theme.fg("muted", ` · ${runtime}`) : ""}`;
		const title = sanitizeUiText(task.title ?? "").replace(/\s+/g, " ").trim();
		const description = title || sanitizeUiText(task.objective).replace(/\s+/g, " ").trim();
		text += `\n  ${theme.fg("toolOutput", description)}`;
		const conclusions = workerConclusions(task);
		for (const activity of task.activities.slice(-UI_RECENT_ACTIVITY_LIMIT)) {
			if (activity.type === "thinking") continue;
			if (activity.type === "phase" && activity.status !== "failed" && (activity.phaseCode === "waiting_slot" || activity.phaseCode === "slot_acquired")) continue;
			text += `\n  ${uiActivityLine(activity, theme)}`;
			if (conclusions.length && activity.id === "phase:finished" && activity.status === "completed") {
				text += theme.fg("muted", "，结论:");
				for (const conclusion of conclusions) text += `\n    ${theme.fg("toolOutput", `• ${conclusion}`)}`;
			}
		}
	}
	const component = new Text(text, 0, 0);
	return {
		invalidate: () => component.invalidate(),
		render: (width: number) => width <= 0 ? [] : component.render(width).map((line) => truncateToWidth(line, width)),
	};
}
