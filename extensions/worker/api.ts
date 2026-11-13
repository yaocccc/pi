import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { RoutingConfig, Thinking, WorkerUiDetails } from "./types.ts";
import type { TranscriptReadResult } from "./transcript-reader.ts";
export type { TranscriptReadRequest, TranscriptReadResult } from "./transcript-reader.ts";

export const WORKER_CAPABILITY = "worker:v1";
export type WorkerStateChange = { type: "changed" | "history" | "closed" } | { type: "batch"; batchId: string };

/** A session-bound, read-only view. Retain this view, not the current session,
 * when subscribing: late updates must never attach to another session. */
export interface WorkerSessionView {
	readonly awaitingHistory: boolean;
	readonly closed: boolean;
	lookup(batchId: string): WorkerUiDetails | undefined;
	history(batchId: string): WorkerUiDetails | undefined;
	subscribe(listener: (change: WorkerStateChange) => void): () => void;
}

/** Frontend-independent state and settings operations. No UI must be loaded. */
export interface WorkerCapability {
	getSession(): WorkerSessionView;
	/** Validates opaque descriptors/cursors and reads bounded recording data. */
	readTranscript(request: unknown): TranscriptReadResult;
	getSettings(): RoutingConfig;
	/** Available models and their authoritative supported Thinking choices. */
	getModels(ctx: ExtensionContext): Array<{ id: string; thinking: Thinking[] }>;
	/** Validates and persists; publishes worker.settings-saved only after a changed save succeeds. */
	saveSettings(ctx: ExtensionContext, value: unknown): RoutingConfig;
}
