import assert from "node:assert/strict";
import test from "node:test";
import { Theme } from "@earendil-works/pi-coding-agent";
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

test("truncated tool lines preserve an enclosing ANSI background", () => {
	const colors = { accent: "#00aaff", warning: "#ffaa00", success: "#00ff00", error: "#ff0000", dim: "#777777", muted: "#888888", text: "#ffffff", thinkingXhigh: "#aaaaaa" } as ConstructorParameters<typeof Theme>[0];
	const theme = new Theme(colors, { selectedBg: "#223344" } as ConstructorParameters<typeof Theme>[1], "truecolor");
	const line = uiActivityLine({ id: "tool:2", type: "tool", status: "completed", label: "read", detail: "中文摘要".repeat(25), at: 0 }, theme);
	assert.equal(visibleWidth(line), 70);
	assert.ok(stripTerminalSequences(line).endsWith("…"));
	assert.doesNotMatch(line, /\x1b\[0m/, "a full SGR reset would clear the enclosing background");
	const framed = theme.bg("selectedBg", line + " ".repeat(10));
	assert.match(framed, /…\x1b\[39m {10}\x1b\[49m$/);
});

test("questions and answers stay inside the matching worker block without duplication", () => {
	const batchId = "batch-long-id";
	const task = (index: number) => ({
		index, mode: "scout" as const, objective: `worker-${index + 1}`, status: "running" as const,
		requestedPreset: "fast" as const, attempt: 0, phase: "运行", activities: [], toolCalls: 0,
		usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 0 },
	});
	const details: WorkerUiDetails = {
		kind: "worker-ui", batchId, startedAt: 1, limit: 3, total: 3, completed: 0,
		tasks: [task(0), task(1), task(2)],
		questions: [
			{ id: "question-1", batchId, taskId: `${batchId}:1`, status: "answered", question: "first question", answer: "first answer", askedAt: 1, expiresAt: 10, timeoutMs: 9 },
			{ id: "question-2", batchId, taskId: `${batchId}:2`, status: "waiting", question: "second question", askedAt: 1, expiresAt: 10, timeoutMs: 9 },
			{ id: "question-3", batchId, taskId: `${batchId}:3`, status: "answered", question: "third question", answer: "third answer", askedAt: 1, expiresAt: 10, timeoutMs: 9 },
		],
	};
	const theme = { fg: (_: string, text: string) => text } as Theme;
	const rendered = renderWorkerDetails(details, theme).render(1_000).join("\n");
	const blocks = rendered.split("─".repeat(24));
	assert.equal(blocks.length, 3);
	assert.match(blocks[0]!, /  Q: first question[\s\S]*  A: first answer/);
	assert.doesNotMatch(blocks[0]!, /second question|third question/);
	assert.match(blocks[1]!, /  Q: second question/);
	assert.doesNotMatch(blocks[1]!, /  A:/, "unanswered questions do not get a fabricated answer");
	assert.match(blocks[2]!, /  Q: third question[\s\S]*  A: third answer/);
	assert.equal((rendered.match(/first answer/g) ?? []).length, 1);
	assert.equal((rendered.match(/third answer/g) ?? []).length, 1);
	assert.doesNotMatch(rendered, /question-1|question-2|question-3|Q[1-9]\s*·|已回答|等待回答|展开工具查看完整问答/);
	assert.match(rendered, /\n  Q: first question/);
	assert.match(rendered, /\n  A: first answer/);
});

test("worker details omit batch and scheduling rows while retaining task and timeout diagnostics", () => {
	const batchId = "batch-render-test";
	const details: WorkerUiDetails = {
		kind: "worker-ui", batchId, startedAt: 1, limit: 1, total: 1, completed: 0,
		controlErrors: [{ toolCallId: "call-1", message: "真正控制错误" }],
		tasks: [{
			index: 0, mode: "scout", objective: "保留目标", status: "running", requestedPreset: "fast", attempt: 0,
			phase: "等待执行槽位", activities: [
				{ id: "phase:slot", type: "phase", status: "running", label: "等待执行槽位", at: 1 },
				{ id: "phase:timeout", type: "phase", status: "failed", label: "等待执行槽位超时", at: 2 },
			], toolCalls: 0,
			usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 0 },
		}],
	};
	const theme = { fg: (_: string, text: string) => text } as Theme;
	for (const expanded of [false, true]) {
		const rendered = renderWorkerDetails(details, theme, { expanded }).render(1_000).join("\n");
		assert.doesNotMatch(rendered, /Batch |batch-render-test|等待主 Agent（占用槽位与路径锁）|等待执行槽位(?!超时)/);
		assert.match(rendered, /◌ scout/);
		assert.match(rendered, /保留目标/);
		assert.match(rendered, /真正控制错误/);
		assert.match(rendered, /等待执行槽位超时/);
	}
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
