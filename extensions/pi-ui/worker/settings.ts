import { parseSettings, SettingsError, type SettingsErrorData } from "../../worker/api.ts";
import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { WorkerCapability } from "../../worker/api.ts";
import type { RoutingConfig } from "../../worker/types.ts";
import { supportsStructuredUI } from "./support.ts";
import { configureWorkerSettings as configureNative } from "./settings-ui.ts";

type SettingsCapability = Pick<WorkerCapability, "getSettings" | "getModels" | "saveSettings">;

/** Menu/RPC transport only. All authoritative writes go through the capability. */
export async function configureWorkerSettings(ctx: ExtensionContext, service: SettingsCapability): Promise<RoutingConfig | undefined> {
	if (!ctx.hasUI) return undefined;
	const current = service.getSettings();
	if (!supportsStructuredUI(ctx)) return configureNative(ctx, current, (value) => service.saveSettings(ctx, value), () => service.getModels(ctx));
	let draft = structuredClone(current);
	let error: SettingsErrorData | undefined;
	while (true) {
		const response = await ctx.ui.input("pi-goui:worker-settings:v1", JSON.stringify({ config: draft, original: current, models: service.getModels(ctx), error }));
		if (response === undefined) return undefined;
		try {
			let parsed: unknown;
			try { parsed = JSON.parse(response); } catch { throw new SettingsError({ code: "invalid-config" }); }
			draft = parseSettings(parsed);
			return service.saveSettings(ctx, draft);
		} catch (cause) {
			// Providers may be loaded by another extension's module loader; the
			// structured error contract must not depend on shared class identity.
			const data = cause && typeof cause === "object" && "data" in cause ? cause.data : undefined;
			error = data && typeof data === "object" && "code" in data
				? data as SettingsErrorData
				: { code: "save-failed", message: cause instanceof Error ? cause.message : String(cause) };
		}
	}
}
