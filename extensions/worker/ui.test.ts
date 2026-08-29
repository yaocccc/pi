import assert from "node:assert/strict";
import test from "node:test";
import { workerUsageText } from "./ui.ts";

test("worker usage groups input and output without a dot separator", () => {
	assert.equal(
		workerUsageText({ input: 7_000, output: 669, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 3 }),
		"Turn 3 · ↑7k ↓669",
	);
});
