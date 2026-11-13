import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { RoutingConfig, Thinking, WorkerUiDetails } from "./types.ts";

export const WORKER_CAPABILITY = "worker:v1";

// Pure settings and wire contracts; no persistence, model registry, or UI implementation.
export const PRESETS = ["fast", "normal", "deep"] as const;
export const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

export interface SettingsErrorData {
	code: "invalid-config" | "invalid-number" | "model-unavailable" | "thinking-unsupported" | "save-failed";
	field?: string;
	model?: string;
	thinking?: string;
	message?: string;
}

export class SettingsError extends Error {
	constructor(readonly data: SettingsErrorData) { super(JSON.stringify(data)); }
}
const fail = (data: SettingsErrorData): never => { throw new SettingsError(data); };
const object = (value: unknown): value is Record<string, unknown> => Boolean(value && typeof value === "object" && !Array.isArray(value));

/** Unlike legacy config loading, interactive saves reject rather than clamp/coerce input. */
export function parseSettings(value: unknown): RoutingConfig {
	const fields = ["version", ...PRESETS, "maxConcurrentWorkers", "defaultTimeoutMs", "maxOutputBytes"];
	if (!object(value) || Object.keys(value).length !== fields.length || fields.some((field) => !Object.hasOwn(value, field)) || value.version !== 2) {
		return fail({ code: "invalid-config" });
	}
	for (const [field, min, max] of [["maxConcurrentWorkers", 1, 16], ["defaultTimeoutMs", 1_000, 3_600_000], ["maxOutputBytes", 8_192, 1_048_576]] as const) {
		const number = value[field];
		if (typeof number !== "number" || !Number.isInteger(number) || number < min || number > max) fail({ code: "invalid-number", field });
	}
	for (const preset of PRESETS) {
		const item = value[preset];
		if (!object(item) || Object.keys(item).length !== 2 || typeof item.model !== "string" || !/^[^/]+\/.+/.test(item.model) || !THINKING_LEVELS.includes(item.thinking as Thinking)) fail({ code: "invalid-config", field: preset });
	}
	return structuredClone(value) as RoutingConfig;
}

export interface TranscriptReadRequest {
	descriptor: { version: 1; id: string };
	/** Opaque, backend-owned continuation token. No paths or byte offsets. */
	cursor?: string;
}
export interface TranscriptReadResult {
	cursor: string;
	reset: boolean;
	records: Record<string, unknown>[];
	more: boolean;
	partial: boolean;
}
export type WorkerStateChange = Readonly<{ type: "changed" | "history" | "closed" } | { type: "batch"; batchId: string }>;

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
