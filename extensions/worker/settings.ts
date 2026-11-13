import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { atomicWriteJson, supportedThinking } from "./config.ts";
import { PRESETS, parseSettings, SettingsError, type SettingsErrorData } from "./api.ts";
import type { RoutingConfig } from "./types.ts";

const fail = (data: SettingsErrorData): never => { throw new SettingsError(data); };

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
