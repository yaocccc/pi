import assert from "node:assert/strict";
import { test } from "node:test";
import { createEventBus } from "@earendil-works/pi-coding-agent";
import { notifyFrontendEvent as publish } from "../../shared/frontend.ts";
import { registerFrontendEvents, supportsStructuredUI } from "./frontend.ts";

for (const protocol of ["PI_UI_PROTOCOL", "PI_GOUI_PROTOCOL"]) {
	test(`${protocol}: facts route to the opted-in RPC UI, never based on a frontend brand`, () => {
		const pi = { events: createEventBus() } as any;
		registerFrontendEvents(pi);
		const previous = { PI_UI_PROTOCOL: process.env.PI_UI_PROTOCOL, PI_GOUI_PROTOCOL: process.env.PI_GOUI_PROTOCOL };
		delete process.env.PI_UI_PROTOCOL;
		delete process.env.PI_GOUI_PROTOCOL;
		try {
			process.env[protocol] = "1";
			const notices: unknown[][] = [];
			const ctx = { mode: "rpc", hasUI: true, ui: { notify: (...args: unknown[]) => notices.push(args) } } as any;
			const event = { name: "context.ready", data: { generationMs: 1200 } } as const;
			assert.equal(supportsStructuredUI(ctx), true);
			publish(pi, ctx, event);
			assert.equal(notices[0][0], `pi-goui:event:v1:${JSON.stringify(event)}`);
			ctx.hasUI = false;
			publish(pi, ctx, event);
			assert.equal(notices.length, 1);
			ctx.hasUI = true;
			ctx.mode = "tui";
			assert.equal(supportsStructuredUI(ctx), false);
			publish(pi, ctx, event);
			assert.match(String(notices[1][0]), /生成耗时 1.2s/);
			delete process.env[protocol];
			ctx.mode = "rpc";
			publish(pi, ctx, event);
			ctx.mode = "print";
			publish(pi, ctx, event);
			assert.equal(notices.length, 2);
		} finally {
			for (const [name, value] of Object.entries(previous)) {
				if (value === undefined) delete process.env[name]; else process.env[name] = value;
			}
		}
	});
}
