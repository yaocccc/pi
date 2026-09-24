import assert from "node:assert/strict";
import test from "node:test";
import type { Theme } from "@earendil-works/pi-coding-agent";
import { stripTerminalSequences, visibleWidth } from "@earendil-works/pi-tui";
import type { WorkerUiActivity, WorkerUiDetails } from "./types.ts";
import { createThinkingActivityRecorder, renderWorkerDetails, uiActivityLine, workerUsageText } from "./ui.ts";

test("worker usage groups input and output without a dot separator", () => {
	assert.equal(
		workerUsageText({ input: 7_000, output: 669, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 3 }),
		"Turn 3 · ↑7k ↓669",
	);
});

test("thinking deltas retain spaces within one message and separate consecutive messages", () => {
	const activities: WorkerUiActivity[] = [];
	const record = createThinkingActivityRecorder(activities, () => {});
	const delta = (text: string) => record({ type: "message_update", assistantMessageEvent: { type: "thinking_delta", delta: text } });
	record({ type: "message_start", message: { role: "assistant" } });
	delta("first ");
	delta("thought");
	record({ type: "message_end", message: { role: "assistant", content: [] } });
	record({ type: "message_start", message: { role: "assistant" } });
	delta("second ");
	delta("thought");
	record({ type: "message_end", message: { role: "assistant", content: [{ type: "thinking", thinking: "second thought" }] } });
	assert.deepEqual(activities.map(({ id, detail, status }) => ({ id, detail, status })), [
		{ id: "thinking:1", detail: "first thought", status: "completed" },
		{ id: "thinking:2", detail: "second thought", status: "completed" },
	]);
	const theme = { fg: (_: string, text: string) => text } as Theme;
	assert.deepEqual(activities.map((item) => uiActivityLine(item, theme)), ["! first thought", "! second thought"]);
});

test("thinking snapshot and delta on the same update are not counted twice", () => {
	const activities: WorkerUiActivity[] = [];
	const record = createThinkingActivityRecorder(activities, () => {});
	record({ type: "message_start", message: { role: "assistant" } });
	record({ type: "message_update", message: { role: "assistant", content: [{ type: "thinking", thinking: "hello " }] }, assistantMessageEvent: { type: "thinking_delta", delta: "hello " } });
	record({ type: "message_update", message: { role: "assistant", content: [{ type: "thinking", thinking: "hello world" }] }, assistantMessageEvent: { type: "thinking_delta", delta: "world" } });
	record({ type: "message_end", message: { role: "assistant", content: [{ type: "thinking", thinking: "hello world" }] } });
	assert.equal(activities.length, 1);
	assert.equal(activities[0]!.detail, "hello world");
});

test("tool activity lines fit 70 visible columns including wide Chinese and ellipsis", () => {
	const theme = { fg: (_: string, text: string) => text } as Theme;
	const line = uiActivityLine({ id: "tool:1", type: "tool", status: "running", label: "中文工具", detail: "中文摘要".repeat(25), at: 0 }, theme);
	assert.equal(visibleWidth(line), 70);
	assert.ok(stripTerminalSequences(line).endsWith("…"));
	assert.ok(line.startsWith("→ 中文工具 · "));
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
