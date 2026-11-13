import assert from "node:assert/strict";
import test from "node:test";
import { createEventBus } from "@earendil-works/pi-coding-agent";
import { getCapability, registerCapability } from "./capabilities.ts";
import { FRONTEND_EVENT, notifyFrontendEvent } from "./frontend.ts";

test("discovery is optional, bus-local, load-order independent and removable", () => {
	const first = { events: createEventBus() }, second = { events: createEventBus() };
	assert.equal(getCapability(first, "example"), undefined);
	const service = { read: () => 42 };
	const remove = registerCapability(first, "example", service);
	assert.equal(getCapability(first, "example"), service);
	assert.equal(getCapability(second, "example"), undefined);
	remove();
	assert.equal(getCapability(first, "example"), undefined);
});

test("frontend facts are available without UI and observers cannot fail a capability", () => {
	const pi = { events: createEventBus() };
	const ctx = { hasUI: false, mode: "json" } as any;
	const event = { name: "context.ready", data: { generationMs: 1200 } } as const;
	let observed: unknown;
	notifyFrontendEvent(pi, ctx, event); // No subscriber is required.
	pi.events.on(FRONTEND_EVENT, data => { observed = data; });
	notifyFrontendEvent(pi, ctx, event);
	assert.deepEqual(observed, { context: ctx, event });
	assert.doesNotThrow(() => notifyFrontendEvent({ events: { emit() { throw new Error("observer failed"); } } } as any, ctx, event));
});
