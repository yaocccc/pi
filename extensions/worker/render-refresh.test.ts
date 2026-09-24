import assert from "node:assert/strict";
import test from "node:test";
import { updateRenderRefresh, type RenderRefreshContext, type RenderRefreshTimerApi } from "./render-refresh.ts";

type Timer = { callback: () => void; delayMs: number; active: boolean };

function createTimerApi(): { timerApi: RenderRefreshTimerApi; timers: Timer[]; cleared: Timer[] } {
	const timers: Timer[] = [];
	const cleared: Timer[] = [];
	return {
		timerApi: {
			setInterval(callback, delayMs) {
				const timer = { callback, delayMs, active: true };
				timers.push(timer);
				return timer;
			},
			clearInterval(handle) {
				const timer = handle as Timer;
				timer.active = false;
				cleared.push(timer);
			},
		},
		timers,
		cleared,
	};
}

function createContext(): { context: RenderRefreshContext; invalidations: { count: number } } {
	const invalidations = { count: 0 };
	return {
		context: { state: {}, invalidate: () => { invalidations.count++; } },
		invalidations,
	};
}

test("partial Worker result refreshes elapsed time once per second without duplicate timers", () => {
	const { timerApi, timers } = createTimerApi();
	const { context, invalidations } = createContext();

	updateRenderRefresh(context, true, timerApi);
	updateRenderRefresh(context, true, timerApi);

	assert.equal(timers.length, 1);
	assert.equal(timers[0]!.delayMs, 1_000);
	timers[0]!.callback();
	assert.equal(invalidations.count, 1);
});

test("final or errored Worker result stops elapsed refresh", () => {
	const { timerApi, timers, cleared } = createTimerApi();
	const { context } = createContext();

	updateRenderRefresh(context, true, timerApi);
	updateRenderRefresh(context, false, timerApi);
	updateRenderRefresh(context, false, timerApi);

	assert.equal(cleared.length, 1);
	assert.equal(cleared[0], timers[0]);
	assert.equal(timers[0]!.active, false);
});

test("different tool-call contexts keep independent elapsed refresh timers", () => {
	const { timerApi, timers, cleared } = createTimerApi();
	const first = createContext();
	const second = createContext();

	updateRenderRefresh(first.context, true, timerApi);
	updateRenderRefresh(second.context, true, timerApi);
	updateRenderRefresh(first.context, true, timerApi);
	updateRenderRefresh(first.context, false, timerApi);

	assert.equal(timers.length, 2);
	assert.equal(cleared.length, 1);
	assert.equal(cleared[0], timers[0]);
	assert.equal(timers[1]!.active, true);
	timers[1]!.callback();
	assert.equal(first.invalidations.count, 0);
	assert.equal(second.invalidations.count, 1);

	updateRenderRefresh(second.context, false, timerApi);
	assert.deepEqual(cleared, timers);
});

test("missing renderer context remains compatible", () => {
	const { timerApi, timers, cleared } = createTimerApi();

	updateRenderRefresh(undefined, true, timerApi);
	updateRenderRefresh(undefined, false, timerApi);

	assert.equal(timers.length, 0);
	assert.equal(cleared.length, 0);
});
