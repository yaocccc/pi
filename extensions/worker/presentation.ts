import type { WorkerUiDetails } from "./types.ts";

/** Best-effort display/telemetry callbacks only; never wrap task execution or process cleanup. */
export function present(callback: () => unknown): void {
	try {
		const result = callback();
		if (result && typeof (result as PromiseLike<unknown>).then === "function") {
			void Promise.resolve(result).catch(() => {});
		}
	} catch { /* A detached/broken UI must not interrupt lifecycle cleanup. */ }
}

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
			// Legacy task input is recognized only when restoring historical cards.
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

export interface RenderRefreshContext {
	state: Record<string, unknown>;
	invalidate: () => void;
}

export interface RenderRefreshTimerApi {
	setInterval: (callback: () => void, delayMs: number) => unknown;
	clearInterval: (handle: unknown) => void;
	setTimeout: (callback: () => void, delayMs: number) => unknown;
	clearTimeout: (handle: unknown) => void;
}

const defaultTimers: RenderRefreshTimerApi = {
	setInterval: (callback, ms) => { const timer = setInterval(callback, ms); timer.unref(); return timer; },
	clearInterval: (timer) => clearInterval(timer as NodeJS.Timeout),
	setTimeout: (callback, ms) => { const timer = setTimeout(callback, ms); timer.unref(); return timer; },
	clearTimeout: (timer) => clearTimeout(timer as NodeJS.Timeout),
};
const ROW_STATE = "__workerLiveResult";
interface Row {
	owner: WorkerRenderRefresh;
	batchId: string;
	snapshot: WorkerUiDetails;
	invalidate: () => void;
}

/** Read-only snapshot used by compact task rows; never look up another session. */
export function getWorkerRenderSnapshot(state?: Record<string, unknown>): WorkerUiDetails | undefined {
	return (state?.[ROW_STATE] as Row | undefined)?.snapshot;
}

/**
 * Pi's ToolRenderContext.invalidate() rebuilds the tool row AND requests a TUI
 * render, including after execute has returned. There is no tool-row disposal
 * hook: resources therefore belong to the session, not to a returned component.
 * Only active rows have a clock. Completed live-session originals retain one
 * callback per batch for control errors; static history retains no callbacks.
 */
export class WorkerRenderRefresh {
	private rows = new Set<Row>();
	// Completed original rows need explicit redraws for later control errors, not a clock.
	private knownRows = new Map<string, Row>();
	private clock: unknown;
	private pending: unknown;
	private closed = false;
	private initialRows = new Set<Row>();
	constructor(private lookup: (batchId: string) => WorkerUiDetails | undefined, private timers = defaultTimers,
		private history: (batchId: string) => WorkerUiDetails | undefined = () => undefined,
		private awaitingHistory = false) {}

	/** Reload renders history before the first session_start supplies its branch. */
	bindHistory() {
		if (this.closed || !this.awaitingHistory) return;
		this.awaitingHistory = false;
		const rows = [...this.initialRows];
		this.initialRows.clear();
		for (const row of rows) {
			row.snapshot = this.lookup(row.batchId) ?? this.history(row.batchId) ?? row.snapshot;
			try { row.invalidate(); } catch { /* Detached startup row. */ }
		}
	}

	resolve(details: WorkerUiDetails, context?: RenderRefreshContext): { details: WorkerUiDetails; snapshot: boolean } {
		if (!context || !details.batchId) return { details, snapshot: true };
		let row = context.state[ROW_STATE] as Row | undefined;
		// A row is permanently bound to its original session. Never adopt a new
		// runtime when an old component is invalidated after a session switch.
		if (row && row.owner !== this) return { details: row.snapshot, snapshot: true };
		const live = !this.closed ? this.lookup(details.batchId) : undefined;
		if (!row) {
			row = { owner: this, batchId: details.batchId, snapshot: live ?? this.history(details.batchId) ?? details, invalidate: context.invalidate };
			context.state[ROW_STATE] = row;
		}
		row.snapshot = live ?? (!this.closed ? this.history(details.batchId) : undefined) ?? row.snapshot;
		if (this.awaitingHistory && !this.closed) this.initialRows.add(row);
		row.invalidate = context.invalidate;
		if (live) this.knownRows.set(details.batchId, row);
		if (live && !live.finishedAt) this.rows.add(row);
		else this.rows.delete(row);
		this.syncClock();
		return { details: row.snapshot, snapshot: !live };
	}

	refreshBatch(id: string) {
		const row = this.knownRows.get(id);
		const latest = !this.closed && this.lookup(id);
		if (!row || !latest) return;
		row.snapshot = latest;
		try { row.invalidate(); } catch { this.knownRows.delete(id); }
	}

	changed() {
		if (this.closed || !this.rows.size || this.pending !== undefined) return;
		this.pending = this.timers.setTimeout(() => {
			this.pending = undefined;
			this.repaint();
		}, 50);
	}

	private repaint() {
		for (const row of [...this.rows]) {
			const live = this.lookup(row.batchId);
			if (live) row.snapshot = live;
			if (!live || live.finishedAt) this.rows.delete(row);
			try { row.invalidate(); } catch { this.rows.delete(row); }
		}
		this.syncClock();
	}

	private syncClock() {
		if (!this.closed && this.rows.size) {
			if (this.clock === undefined) this.clock = this.timers.setInterval(() => this.repaint(), 1_000);
		} else {
			if (this.clock !== undefined) this.timers.clearInterval(this.clock);
			if (this.pending !== undefined) this.timers.clearTimeout(this.pending);
			this.clock = this.pending = undefined;
		}
	}

	/** Freeze the last session snapshot before releasing every callback/timer. */
	dispose() {
		if (this.closed) return;
		this.closed = true;
		this.repaint();
		this.rows.clear();
		this.initialRows.clear();
		this.knownRows.clear();
		this.syncClock();
	}
}
