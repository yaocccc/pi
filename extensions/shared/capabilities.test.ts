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

for (const [name, event] of [
	["shared", { name: "context.ready", data: { generationMs: 1200 } }],
	["sol-pi", { name: "context.ready", data: { generationMs: 1200 } }],
	["autoname", { name: "autoname.renamed", data: { name: "original", contextTokens: 20, durationMs: 10 } }],
	["filter-output", { name: "filter.redacted", data: { path: ".env" } }],
	["worker", { name: "worker.settings-error", data: { message: "original" } }],
] as const) {
	test(`${name}: frontend facts detach and freeze data without freezing the host`, async () => {
		const { notifyFrontendEvent: publish } = await import(new URL(`../${name}/frontend.ts`, import.meta.url).href);
		const pi = { events: createEventBus() };
		const context = { mode: "rpc", hasUI: true } as any;
		const expected = structuredClone(event);
		const original = structuredClone(event);
		let received: any;
		pi.events.on(FRONTEND_EVENT, (value: any) => {
			received = value;
			Reflect.set(value, "context", {});
			Reflect.set(value.event, "name", "tampered");
			Reflect.set(value.event.data, Object.keys(original.data)[0], "tampered");
		});
		let later: any;
		pi.events.on(FRONTEND_EVENT, (value) => { later = value; });
		publish(pi, context, original);
		assert.deepEqual(original, expected, "observers must not mutate business data");
		assert.equal(received.context, context, "ownership envelope must remain stable");
		assert.notEqual(received.event, original);
		assert.notEqual(received.event.data, original.data);
		assert.deepEqual(later.event, expected, "earlier observers must not alter later facts");
		assert.ok(Object.isFrozen(received) && Object.isFrozen(received.event) && Object.isFrozen(received.event.data));
		Reflect.set(original.data, Object.keys(original.data)[0], "later business change");
		assert.deepEqual(received.event, expected, "later business mutations must not change the published snapshot");
		assert.equal(Object.isFrozen(context), false, "SDK context lifecycle remains host-owned");
	});
}
