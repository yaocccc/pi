import { readFileSync } from "node:fs";
import { join } from "node:path";
import { getAgentDir, SettingsManager, type ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { EffectiveCompactionSettings } from "./native-preparation.ts";

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
