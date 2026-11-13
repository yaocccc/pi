import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
/** Backend facts owned by this capability; UI owns their presentation. */
export type FrontendEvent =
	| { name: "worker.settings-saved"; data: Record<string, never> }
	| { name: "worker.settings-error"; data: { message: string } };

export const FRONTEND_EVENT = "pi:frontend-event:v1";
type ReadonlyFact<T> = T extends object ? { readonly [K in keyof T]: ReadonlyFact<T[K]> } : T;
export interface FrontendEventMessage {
	readonly context: ExtensionContext;
	readonly event: ReadonlyFact<FrontendEvent>;
}

function freezeFact<T>(value: T): T {
	if (value && typeof value === "object") {
		for (const child of Object.values(value)) freezeFact(child);
		Object.freeze(value);
	}
	return value;
}

/** Publish facts only. Optional UI/transport adapters own formatting and delivery. */
export function notifyFrontendEvent(pi: Pick<ExtensionAPI, "events">, ctx: ExtensionContext, event: FrontendEvent): void {
	try {
		// Snapshot only the fact; the live SDK context and its lifecycle belong to the host.
		const snapshot = freezeFact(structuredClone(event));
		pi.events.emit(FRONTEND_EVENT, Object.freeze({ context: ctx, event: snapshot } satisfies FrontendEventMessage));
	}
	catch { /* An observer must never change the outcome of a capability. */ }
}
