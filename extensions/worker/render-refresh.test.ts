import assert from "node:assert/strict";
import test from "node:test";
import { WorkerRenderRefresh, type RenderRefreshContext, type RenderRefreshTimerApi } from "./render-refresh.ts";
import type { WorkerUiDetails } from "./types.ts";

type Timer = { callback: () => void; ms: number; active: boolean };
function clock() {
	const timers: Timer[] = [];
	const add = (callback: () => void, ms: number) => { const timer = { callback, ms, active: true }; timers.push(timer); return timer; };
	const clear = (timer: unknown) => { (timer as Timer).active = false; };
	const api: RenderRefreshTimerApi = { setInterval: add, clearInterval: clear, setTimeout: add, clearTimeout: clear };
	const tick = (ms: number) => {
		for (const timer of [...timers]) if (timer.active && timer.ms === ms) {
			if (ms === 50) timer.active = false;
			timer.callback();
		}
	};
	return { api, timers, tick, active: () => timers.filter((timer) => timer.active) };
}
const details = (id = "batch"): WorkerUiDetails => ({ kind: "worker-ui", batchId: id, startedAt: 1, total: 1, completed: 0, limit: 1, tasks: [] });

test("live final-result rows share one session clock; changes coalesce and completion clears all timers", () => {
	const time = clock();
	let live = details();
	const refresh = new WorkerRenderRefresh(() => live, time.api);
	let paints = 0;
	const context: RenderRefreshContext = { state: {}, invalidate: () => { paints++; refresh.resolve(live, context); } };
	refresh.resolve(live, context);
	refresh.resolve(live, context);
	assert.equal(time.active().length, 1);
	time.tick(1_000);
	assert.equal(paints, 1);
	refresh.changed(); refresh.changed();
	assert.equal(time.active().length, 2);
	time.tick(50);
	assert.equal(paints, 2);
	live = { ...live, completed: 1, finishedAt: 10 };
	refresh.changed(); time.tick(50);
	assert.equal(paints, 3);
	assert.equal(time.active().length, 0);
	refresh.changed();
	assert.equal(time.active().length, 0);
	refresh.dispose(); refresh.dispose();
});

test("only active contexts are retained; no UI, unknown historical IDs and missing contexts start no timers", () => {
	const time = clock();
	const refresh = new WorkerRenderRefresh(() => undefined, time.api);
	refresh.changed();
	assert.equal(refresh.resolve(details()).snapshot, true);
	assert.equal(refresh.resolve(details(), { state: {}, invalidate() {} }).snapshot, true);
	assert.equal(time.active().length, 0);
	refresh.dispose();
});

test("shutdown freezes snapshots, clears pending/elapsed timers and cannot rebind old rows to a new runtime", () => {
	const time = clock();
	let live = details();
	const refresh = new WorkerRenderRefresh(() => live, time.api);
	const context = { state: {}, invalidate() {} };
	refresh.resolve(live, context);
	refresh.changed();
	live = { ...live, completed: 1, finishedAt: 15 };
	refresh.dispose();
	assert.equal(time.active().length, 0);
	const next = new WorkerRenderRefresh(() => ({ ...details(), completed: 999 }), time.api);
	const frozen = next.resolve(details(), context);
	assert.equal(frozen.details.completed, 1);
	assert.equal(frozen.snapshot, true);
	assert.equal(time.active().length, 0);
	refresh.changed(); refresh.dispose(); next.dispose();
	assert.equal(time.active().length, 0);
});

test("multiple rows refresh independently and a detached failing row cannot leak a timer", () => {
	const time = clock();
	const refresh = new WorkerRenderRefresh((id) => details(id), time.api);
	let paints = 0;
	refresh.resolve(details("a"), { state: {}, invalidate() { throw new Error("detached"); } });
	refresh.resolve(details("b"), { state: {}, invalidate() { paints++; } });
	assert.equal(time.active().length, 1);
	time.tick(1_000);
	assert.equal(paints, 1);
	assert.equal(time.active().length, 1);
	refresh.dispose();
	assert.equal(time.active().length, 0);
});
