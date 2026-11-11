import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { FrontendEvent } from "./frontend-events.ts";
import { nativeEventNotice } from "./native-events.ts";

/** Versioned data transport over the SDK's supported RPC UI channel. */
export const isGoUI = (ctx: ExtensionContext): boolean =>
	ctx.mode === "rpc" && process.env.PI_GOUI_PROTOCOL === "1";

/** Route facts to Go; native notification formatting is isolated in its adapter. */
export function notifyFrontendEvent(ctx: ExtensionContext, event: FrontendEvent): void {
	if (isGoUI(ctx)) {
		emitFrontendEvent(ctx, event.name, event.data);
	} else if (ctx.mode === "tui") {
		const { message, level } = nativeEventNotice(event);
		ctx.ui.notify(message, level);
	}
}

export function emitFrontendEvent(ctx: ExtensionContext, name: string, data: Record<string, unknown>): void {
	if (isGoUI(ctx)) ctx.ui.notify(`pi-goui:event:v1:${JSON.stringify({ name, data })}`, "info");
}
