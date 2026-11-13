import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { FrontendEvent } from "./frontend-events.ts";

export const FRONTEND_EVENT = "pi:frontend-event:v1";
export interface FrontendEventMessage {
	context: ExtensionContext;
	event: FrontendEvent;
}

/** Publish facts only. Optional UI/transport adapters own formatting and delivery. */
export function notifyFrontendEvent(pi: Pick<ExtensionAPI, "events">, ctx: ExtensionContext, event: FrontendEvent): void {
	try { pi.events.emit(FRONTEND_EVENT, { context: ctx, event } satisfies FrontendEventMessage); }
	catch { /* An observer must never change the outcome of a capability. */ }
}
