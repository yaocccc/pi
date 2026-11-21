import assert from "node:assert/strict";
import test from "node:test";
import { createWorkerEventProgress } from "./event-progress.ts";

test("JSON message_update counts full deltas and cumulative usage until authoritative message_end", () => {
	const progress = createWorkerEventProgress([], () => {});
	const update = (type: string, delta: string, output: number) => progress.record({
		type: "message_update", usage: { input: 30, output, cacheRead: 7, cacheWrite: 0, totalTokens: 37 + output },
		assistantMessageEvent: { type, contentIndex: 0, delta },
	});
	update("thinking_delta", "x".repeat(4000), 0);
	update("text_delta", "y".repeat(2000), 0);
	assert.equal(progress.snapshot().usage.output, 1500);
	assert.equal(progress.snapshot().usage.input, 30);
	update("text_delta", "z", 1700);
	assert.equal(progress.snapshot().usage.output, 1700);
	progress.record({ type: "message_end", message: { role: "assistant", content: [], usage: { input: 28, output: 1234, cacheRead: 0, totalTokens: 1262 } } });
	assert.equal(progress.snapshot().usage.output, 1234);
	assert.equal(progress.snapshot().usage.cacheRead, 0);
	update("text_delta", "next", 0);
	assert.equal(progress.snapshot().usage.output, 1235);
});
