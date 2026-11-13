// Bundled support from pi-ui/shared/portable.ts; update via npm run sync:portable.
// Self-contained at runtime: consumers do not need the source template.
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { getCapability, registerCapability } from "../../autoname/capabilities.ts";

/** Any RPC UI may opt in; legacy Go flags and wire names remain compatible. */
export const supportsStructuredUI = (ctx: ExtensionContext): boolean =>
	ctx.mode === "rpc" && (process.env.PI_UI_PROTOCOL === "1" || process.env.PI_GOUI_PROTOCOL === "1");

export function emitStructuredEvent(ctx: ExtensionContext, name: string, data: Record<string, unknown>): void {
	if (ctx.hasUI && supportsStructuredUI(ctx)) ctx.ui.notify(`pi-goui:event:v1:${JSON.stringify({ name, data })}`, "info");
}

type Fact = { name: string; data: Record<string, unknown> };
type Notice = { message: string; level: "info" | "warning" | "error" };

/** Subscribe only to this adapter's facts. Copies cooperate through the host bus,
 * so combining standalone adapters never duplicates a fact notification. */
export function registerFactUI<E extends Fact>(pi: ExtensionAPI, names: readonly E["name"][], format: (event: E) => Notice): void {
	for (const name of names) {
		const key = `pi-ui.fact:${name}:v1`;
		if (getCapability(pi, key)) continue;
		registerCapability(pi, key, true);
		pi.events.on("pi:frontend-event:v1", (data) => {
			const { context: ctx, event } = data as { context: ExtensionContext; event: E };
			if (event.name !== name || !ctx.hasUI) return;
			if (supportsStructuredUI(ctx)) emitStructuredEvent(ctx, event.name, event.data);
			else if (ctx.mode === "tui") {
				const notice = format(event);
				ctx.ui.notify(notice.message, notice.level);
			}
		});
	}
}
