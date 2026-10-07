import { readFileSync } from "node:fs";
import { join } from "node:path";
import { getAgentDir, SettingsManager, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { EffectiveCompactionSettings } from "./native-preparation.ts";
import type { ThinkingLevel } from "@earendil-works/pi-agent-core";

export interface SummarySettings {
	model: NonNullable<ExtensionContext["model"]>;
	thinking: ThinkingLevel | undefined;
	/** Optional for SDK resolvers; omission behaves like auto. */
	service_tier?: "auto" | "fast" | "ultrafast";
}

/** Read afresh when starting a job; never read an untrusted project's configuration. */
export function resolveSummarySettings(ctx: ExtensionContext, path = join(getAgentDir(), "online-compact-settings.json")): SummarySettings {
	let config: { model?: unknown; thinking?: unknown; service_tier?: unknown } = {};
	try { config = JSON.parse(readFileSync(path, "utf8")); }
	catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
	if (!config || typeof config !== "object" || Array.isArray(config)) throw new Error("Invalid online compaction settings");
	const modelName = config.model ?? "auto";
	const thinking = config.thinking ?? "auto";
	const serviceTier = config.service_tier ?? "auto";
	if (serviceTier !== "auto" && serviceTier !== "fast" && serviceTier !== "ultrafast") {
		throw new Error("Invalid online compaction service_tier");
	}
	if (typeof modelName !== "string" || typeof thinking !== "string" ||
		!["auto", "off", "minimal", "low", "medium", "high", "xhigh", "max"].includes(thinking)) {
		throw new Error("Invalid online compaction model or thinking");
	}
	const slash = modelName.indexOf("/");
	const model = modelName === "auto" ? ctx.model : slash > 0
		? ctx.modelRegistry.find(modelName.slice(0, slash), modelName.slice(slash + 1)) : undefined;
	if (!model) throw new Error(`Unknown online compaction model: ${modelName}`);
	return { model: structuredClone(model), thinking: thinking === "auto" ? ctx.thinkingLevel : thinking as ThinkingLevel,
		service_tier: serviceTier };
}

export type OnlineSettings = {
	compaction: EffectiveCompactionSettings;
	retry: ReturnType<SettingsManager["getRetrySettings"]>;
};
export type SettingsResolver = (ctx: ExtensionContext) => OnlineSettings;

/** No setters, locks, directory creation, credentials, or project reads without trust. */
export const resolveOnlineSettings: SettingsResolver = (ctx) => {
	const trusted = ctx.isProjectTrusted();
	const manager = SettingsManager.fromStorage({
		withLock(scope, read) {
			if (scope === "project" && !trusted) { read(undefined); return; }
			const path = scope === "global" ? join(getAgentDir(), "settings.json") : join(ctx.cwd, ".pi", "settings.json");
			let text: string | undefined;
			try { text = readFileSync(path, "utf8"); }
			catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error; }
			// SettingsManager reads/migrates in memory. Never persist its return value.
			read(text);
		},
	}, { projectTrusted: trusted });
	const errors = manager.drainErrors();
	if (errors.length) throw errors[0].error;
	return { compaction: manager.getCompactionSettings(ctx.model), retry: manager.getRetrySettings() };
};
