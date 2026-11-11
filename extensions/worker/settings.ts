import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { emitFrontendEvent, isGoUI } from "../shared/frontend.ts";
import { atomicWriteJson, PRESETS, supportedThinking, THINKING_LEVELS } from "./config.ts";
import type { RoutingConfig, Thinking } from "./types.ts";

export interface SettingsErrorData {
	code: "invalid-config" | "invalid-number" | "model-unavailable" | "thinking-unsupported" | "save-failed";
	field?: string;
	model?: string;
	thinking?: string;
	message?: string;
}

class SettingsError extends Error {
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

export async function configureWorkerSettings(ctx: ExtensionContext, current: RoutingConfig, configPath: string): Promise<RoutingConfig | undefined> {
	if (!ctx.hasUI) return undefined;
	if (!isGoUI(ctx)) {
		// Native TUI and SDK-compatible generic RPC clients keep the existing menu.
		const { configureWorkerSettings: configureNative } = await import("./settings-ui.ts");
		return configureNative(ctx, current, configPath);
	}
	let draft = structuredClone(current);
	let error: SettingsErrorData | undefined;
	while (true) {
		const models = ctx.modelRegistry.getAvailable().map((model) => ({ id: `${model.provider}/${model.id}`, thinking: supportedThinking(model) }));
		const response = await ctx.ui.input("pi-goui:worker-settings:v1", JSON.stringify({ config: draft, original: current, models, error }));
		if (response === undefined) return undefined;
		try {
			let parsed: unknown;
			try { parsed = JSON.parse(response); } catch { fail({ code: "invalid-config" }); }
			draft = parseSettings(parsed);
			const saved = saveWorkerSettings(ctx, draft, configPath);
			emitFrontendEvent(ctx, "worker.settings-saved", {});
			return saved;
		} catch (cause) {
			error = cause instanceof SettingsError ? cause.data : { code: "save-failed", message: cause instanceof Error ? cause.message : String(cause) };
		}
	}
}
