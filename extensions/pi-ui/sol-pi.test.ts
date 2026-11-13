import assert from "node:assert/strict";
import test from "node:test";
import { createEventBus } from "@earendil-works/pi-coding-agent";
import { registerSolPiUI } from "./sol-pi.ts";
import { nativeEventNotice } from "./shared/native-events.ts";

test("background tool cards are hidden independently of tool execution", () => {
	let resolve: any;
	registerSolPiUI({ events: createEventBus(), registerToolRenderer(fn: unknown) { resolve = fn; } } as any);
	const next = { marker: true };
	assert.equal(resolve("other", () => next), next);
	for (const name of ["obs_recall", "update_plan"]) {
		const renderers = resolve(name, () => undefined);
		assert.equal(renderers.renderShell, "self");
		for (const render of [renderers.renderCall, renderers.renderResult]) {
			for (const isPartial of [true, false]) for (const isError of [true, false]) {
				const component = render({ isError }, { isPartial });
				for (const width of [1, 80, 120]) assert.deepEqual(component.render(width), []);
				component.invalidate();
				assert.deepEqual(component.render(80), []);
			}
		}
	}
});

test("context facts are formatted only by the native adapter", () => {
	const ready = nativeEventNotice({ name: "context.ready", data: { generationMs: 2500 } });
	assert.match(ready.message, /生成耗时 2\.5s/);
	for (const afterTokens of [500, 1500]) {
		const notice = nativeEventNotice({ name: "context.applied", data: { beforeTokens: 1000, afterTokens, generationMs: 2500, durationMs: 8000 } });
		assert.match(notice.message, afterTokens > 1000 ? /增加 500/ : /减少 500/);
		assert.match(notice.message, /生成 2\.5s，总耗时 8\.0s/);
	}
	const failure = nativeEventNotice({ name: "context.failed", data: { durationMs: 2500, reason: "bad config" } });
	assert.equal(failure.level, "warning");
	assert.match(failure.message, /原因：bad config/);
});
