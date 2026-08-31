import assert from "node:assert/strict";
import test from "node:test";
import type { Theme } from "@earendil-works/pi-coding-agent";
import type { WorkerUiDetails } from "./types.ts";
import { renderWorkerDetails, workerUsageText } from "./ui.ts";

test("worker usage groups input and output without a dot separator", () => {
	assert.equal(
		workerUsageText({ input: 7_000, output: 669, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 3 }),
		"Turn 3 · ↑7k ↓669",
	);
});

test("worker details display the complete objective", () => {
	const objective = `检查并修复 Worker 工具目标显示。${"完整目标内容".repeat(30)}目标结束`;
	const details: WorkerUiDetails = {
		kind: "worker-ui",
		startedAt: Date.now(),
		limit: 1,
		total: 1,
		completed: 0,
		tasks: [{
			index: 0,
			mode: "scout",
			objective,
			status: "queued",
			requestedPreset: "fast",
			attempt: 0,
			phase: "等待执行",
			activities: [],
			toolCalls: 0,
			usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 0 },
		}],
	};
	const theme = { fg: (color: string, text: string) => color === "toolOutput" ? `<toolOutput>${text}</toolOutput>` : text } as unknown as Theme;
	const rendered = renderWorkerDetails(details, theme).render(1_000).join("\n");

	assert.match(rendered, /目标结束/);
	assert.ok(rendered.includes(`<toolOutput>${objective}</toolOutput>`));
});
