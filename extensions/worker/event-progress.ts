import { estimateTokens } from "@earendil-works/pi-coding-agent";
import { estimateToolTokens } from "./token-estimate.ts";
import { addWorkerUsage, appendUiActivity, emptyWorkerUsage, estimateMessageTokens, messageUsage, summarizeToolArgs, summarizeToolResult, uiSnippet, createThinkingActivityRecorder } from "./data.ts";
import type { WorkerUsage, WorkerUiActivity, WorkerUiActivityStatus } from "./types";

// Project child protocol events into display activities and token counts.
// Process completion, transcript recording and error handling stay with the caller.
export function createWorkerEventProgress(activities: WorkerUiActivity[], emit: (force?: boolean) => void) {
	const seenToolIds = new Set<string>();
	// Keep only counts, not potentially large or sensitive raw arguments.
	const toolArgumentTokens = new Map<string, number>();
	const recordToolArgs = (id: string, args: unknown) => {
		if (args !== undefined) toolArgumentTokens.set(id, estimateToolTokens(args, { content: [] }));
	};
	const toolTokens = (id: string, result: any): number | undefined => Array.isArray(result?.content)
		? (toolArgumentTokens.get(id) ?? 0) + estimateTokens({ ...result, role: 'toolResult' })
		: undefined;
	let settledUsage = emptyWorkerUsage();
	let streamUsage = emptyWorkerUsage();
	let currentTurn = 1;
	let activeTurn = true;
	const usageSnapshot = (): WorkerUsage => {
		const usage = addWorkerUsage(settledUsage, streamUsage);
		usage.turns = Math.max(settledUsage.turns, activeTurn ? currentTurn : 0);
		return usage;
	};
	const upsertTool = (id: string, name: string, status: WorkerUiActivityStatus, detail?: string, contextSizeEstimate?: number) => {
		seenToolIds.add(id);
		const activityId = `tool:${id}`;
		const previous = activities.find((item) => item.id === activityId);
		appendUiActivity(activities, {
			id: activityId,
			type: "tool",
			status,
			label: uiSnippet(name, 32) || "未知工具",
			detail: detail || previous?.detail,
			contextSizeEstimate: contextSizeEstimate ?? previous?.contextSizeEstimate,
			at: Date.now(),
		});
		emit(status !== "running");
	};
	const recordThinking = createThinkingActivityRecorder(activities, () => emit());
	function record(event: any) {
		recordThinking(event);
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
			const thinking = activities.find((item) => item.type === "thinking" && item.status === "running");
			if (thinking?.detail) streamUsage.output = Math.max(streamUsage.output, Math.ceil(thinking.detail.length / 4));
			emit();
			return;
		}
		if (event.type === "message_end" && event.message?.role === "assistant") {
			const reported = messageUsage(event.message, 1);
			if (!reported.input) reported.input = streamUsage.input;
			// Match main-session output: explicit usage (even zero) is authoritative.
			if (event.message.usage == null) reported.output = Math.max(streamUsage.output, estimateMessageTokens(event.message));
			if (!reported.cacheRead) reported.cacheRead = streamUsage.cacheRead;
			if (!reported.cacheWrite) reported.cacheWrite = streamUsage.cacheWrite;
			if (!reported.contextTokens) reported.contextTokens = streamUsage.contextTokens;
			settledUsage = addWorkerUsage(settledUsage, reported);
			currentTurn = Math.max(currentTurn, settledUsage.turns);
			streamUsage = emptyWorkerUsage();
			activeTurn = false;
			for (const part of event.message.content ?? []) {
				if (part?.type === "toolCall") {
					recordToolArgs(part.id, part.arguments);
					upsertTool(part.id, part.name, "running", summarizeToolArgs(part.arguments));
				}
			}
			return;
		}
		if (event.type === "tool_execution_start" && event.toolCallId && event.toolName) {
			recordToolArgs(event.toolCallId, event.args);
			upsertTool(event.toolCallId, event.toolName, "running", summarizeToolArgs(event.args));
			return;
		}
		if (event.type === "tool_execution_update" && event.toolCallId && event.toolName) {
			upsertTool(event.toolCallId, event.toolName, "running", summarizeToolResult(event.partialResult) ?? summarizeToolArgs(event.args));
			return;
		}
		if (event.type === "tool_execution_end" && event.toolCallId && event.toolName) {
			upsertTool(event.toolCallId, event.toolName, event.isError ? "failed" : "completed", summarizeToolResult(event.result), toolTokens(event.toolCallId, event.result));
			return;
		}
		if ((event.type === "message_end" || event.type === "tool_result_end") && event.message?.role === "toolResult") {
			upsertTool(event.message.toolCallId, event.message.toolName, event.message.isError ? "failed" : "completed", summarizeToolResult(event.message), toolTokens(event.message.toolCallId, event.message));
		}
	}
	return { record, snapshot: () => ({ toolCalls: seenToolIds.size, usage: usageSnapshot() }) };
}
