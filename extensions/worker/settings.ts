import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { atomicWriteJson, PRESETS, supportedThinking, THINKING_LEVELS } from "./config.ts";
import type { RoutingConfig, Thinking } from "./types.ts";

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

export function saveWorkerSettings(ctx: ExtensionContext, value: unknown, configPath: string): RoutingConfig {
	const config = parseSettings(value);
	const available = ctx.modelRegistry.getAvailable();
	for (const preset of PRESETS) {
		const setting = config[preset];
		const model = available.find((model) => `${model.provider}/${model.id}` === setting.model);
		if (!model) fail({ code: "model-unavailable", field: preset, model: setting.model });
		if (!supportedThinking(model!).includes(setting.thinking)) fail({ code: "thinking-unsupported", field: preset, model: setting.model, thinking: setting.thinking });
	}
	try { atomicWriteJson(configPath, config); }
	catch (error) { fail({ code: "save-failed", message: error instanceof Error ? error.message : String(error) }); }
	return config;
}
