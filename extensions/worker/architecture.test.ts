import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { WorkerRenderRefresh } from "./presentation.ts";
import type { WorkerUiDetails } from "./types.ts";

const source = (name: string) => readFileSync(new URL(name, import.meta.url), "utf8");

test("backend data, history, callbacks and process modules do not import native presentation", () => {
	for (const name of ["data.ts", "history.ts", "callbacks.ts", "process.ts", "runtime.ts", "../shared/token-estimate.ts"]) {
		const text = source(name);
		assert.doesNotMatch(text, /from\s+[\x27\x22][^\x27\x22]*(?:pi-tui|\/ui\.ts|\/native-ui\.ts|\/presentation\.ts|\/tool-tokens\.ts)[\x27\x22]/, name);
		assert.doesNotMatch(text, /\b(?:Theme|renderCall|renderResult)\b/, name);
	}
});

test("tool registration delegates native rendering without owning layout", () => {
	const text = source("index.ts");
	assert.match(text, /\.\.\.nativeWorkerRenderers\(\(\) => session\)/);
	assert.doesNotMatch(text, /pi-tui|renderCall\(|renderResult\(|theme\.fg|new Text\(/);
});

test("RPC progress without native render callbacks never allocates refresh timers", () => {
	let timers = 0;
	const details: WorkerUiDetails = { kind: "worker-ui", batchId: "rpc", startedAt: 1, total: 1, completed: 0, limit: 1, tasks: [] };
	const refresh = new WorkerRenderRefresh(() => details, {
		setInterval: () => { timers++; }, clearInterval() {},
		setTimeout: () => { timers++; }, clearTimeout() {},
	}, () => details, true);
	refresh.bindHistory();
	refresh.changed();
	refresh.refreshBatch("rpc");
	refresh.dispose();
	assert.equal(timers, 0);
});
