import type { WorkerUiDetails } from "./types.ts";

export const WORKER_SNAPSHOT_ENTRY = "worker-snapshot-v1";

/** Only feed getBranch(), never getEntries(): abandoned branches are not history. */
export function workerBranchSnapshots(branch: readonly any[]): Map<string, WorkerUiDetails> {
	const starts = new Set<string>();
	const calls = new Map<string, any>();
	for (const entry of branch) {
		if (entry.type !== "message" || entry.message?.role !== "assistant") continue;
		for (const part of entry.message.content ?? []) {
			if (part.type !== "toolCall" || part.name !== "worker") continue;
			calls.set(part.id, part.arguments);
			if (part.arguments && !part.arguments.batchId && (part.arguments.task || part.arguments.tasks)) starts.add(part.id);
		}
	}
	const snapshots = new Map<string, WorkerUiDetails>();
	for (const entry of branch) {
		const message = entry.type === "message" && entry.message?.role === "toolResult" && entry.message.toolName === "worker" ? entry.message : undefined;
		const details = message?.details ?? (entry.type === "custom" && entry.customType === WORKER_SNAPSHOT_ENTRY ? entry.data : undefined);
		if (details?.kind !== "worker-ui" || typeof details.batchId !== "string" || !Array.isArray(details.tasks)) continue;
		const previous = snapshots.get(details.batchId);
		const origin = details.originToolCallId ?? (starts.has(message?.toolCallId) ? message.toolCallId : previous?.originToolCallId);
		if (!origin || !starts.has(origin) || (previous && previous.originToolCallId !== origin)) continue;
		// Results may only update their own start or the exact continuation's batch.
		if (message && message.toolCallId !== origin && calls.get(message.toolCallId)?.batchId !== details.batchId) continue;
		if (previous && (details.revision !== undefined && previous.revision !== undefined
			? details.revision < previous.revision
			: (details.snapshotAt ?? 0) < (previous.snapshotAt ?? 0))) continue;
		snapshots.set(details.batchId, { ...details, originToolCallId: origin });
	}
	return snapshots;
}

export const emptyWorkerComponent = () => ({ render: (_width: number): string[] => [], invalidate() {} });
