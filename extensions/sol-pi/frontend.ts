import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
/** Backend facts owned by this capability; UI owns their presentation. */
export type FrontendEvent =
	| { name: "context.ready"; data: { generationMs: number } }
	| { name: "context.applied"; data: { beforeTokens: number; afterTokens: number; generationMs: number; durationMs: number } }
	| { name: "context.failed"; data: { durationMs: number; reason: string } };

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
