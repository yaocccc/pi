import assert from "node:assert/strict";
import { test } from "node:test";
import { notifyFrontendEvent } from "./frontend.ts";

test("frontend facts route only to the selected UI adapter", () => {
	const previous = process.env.PI_GOUI_PROTOCOL;
	try {
		process.env.PI_GOUI_PROTOCOL = "1";
		const notices: unknown[][] = [];
		const ctx = { mode: "rpc", hasUI: true, ui: { notify: (...args: unknown[]) => notices.push(args) } };
		const event = { name: "context.ready", data: { generationMs: 1200 } } as const;
		notifyFrontendEvent(ctx as any, event);
		assert.equal(notices[0][0], `pi-goui:event:v1:${JSON.stringify(event)}`);
		ctx.mode = "tui";
		notifyFrontendEvent(ctx as any, event);
		assert.match(String(notices[1][0]), /生成耗时 1.2s/);
		delete process.env.PI_GOUI_PROTOCOL;
		ctx.mode = "rpc";
		notifyFrontendEvent(ctx as any, event);
		ctx.mode = "print";
		notifyFrontendEvent(ctx as any, event);
		assert.equal(notices.length, 2);
	} finally {
		if (previous === undefined) delete process.env.PI_GOUI_PROTOCOL;
		else process.env.PI_GOUI_PROTOCOL = previous;
	}
});
