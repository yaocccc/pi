import assert from "node:assert/strict";
import test from "node:test";
import { Theme } from "@earendil-works/pi-coding-agent";
import { stripTerminalSequences, visibleWidth } from "@earendil-works/pi-tui";
import type { WorkerUiActivity, WorkerUiDetails } from "./types.ts";
import { compactWorkerResult, serializePayload, createThinkingActivityRecorder, renderWorkerDetails, sanitizeStructuredValue, uiActivityLine, workerUsageText } from "./ui.ts";

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

test("completed tool estimates appear between name and preview, without tok or background resets", () => {
	const theme = { fg: (_: string, text: string) => text } as Theme;
	const activity: WorkerUiActivity = { id: "tool:tokens", type: "tool", status: "completed", label: "read", contextSizeEstimate: 775, detail: "func (s *Store) Kline(ctx context.Context, key string) (*model.Kline, error)", at: 0 };
	const sanitized = sanitizeStructuredValue({ ...activity, apiToken: 'secret-value' }) as WorkerUiActivity & { apiToken: string };
	assert.equal(sanitized.contextSizeEstimate, 775, 'estimate survives the actual snapshot sanitization path');
	assert.equal(sanitized.apiToken, '[已隐藏]', 'credential redaction remains enabled');
	const line = uiActivityLine(sanitized, theme);
	assert.ok(line.startsWith("✓ read · ≈775 · func"));
	assert.equal(visibleWidth(line), 70);
	assert.ok(line.endsWith("…"));
	assert.doesNotMatch(line, /tok|\x1b\[0m/);
	assert.doesNotMatch(uiActivityLine({ ...activity, status: "running" }, theme), /≈/);
	assert.doesNotMatch(uiActivityLine({ ...activity, contextSizeEstimate: undefined }, theme), /≈/);
	assert.match(uiActivityLine({ ...activity, status: "failed", detail: undefined }, theme), /^✗ read · ≈775$/);
});

test("worker tool estimates use orange from 5k and red from 10k", () => {
	const theme = { fg: (color: string, text: string) => `<${color}>${text}</${color}>` } as Theme;
	const activity: WorkerUiActivity = { id: "colors", type: "tool", status: "completed", label: "read", at: 0 };
	assert.ok(uiActivityLine({ ...activity, contextSizeEstimate: 5000 }, theme).includes("\x1b[38;2;255;165;0m≈5.0k\x1b[39m"));
	assert.ok(uiActivityLine({ ...activity, contextSizeEstimate: 10000 }, theme).includes("<error>≈10.0k</error>"));
});

test("truncated tool lines preserve an enclosing ANSI background", () => {
	const colors = { accent: "#00aaff", warning: "#ffaa00", success: "#00ff00", error: "#ff0000", dim: "#777777", muted: "#888888", text: "#ffffff", thinkingXhigh: "#aaaaaa" } as ConstructorParameters<typeof Theme>[0];
	const theme = new Theme(colors, { selectedBg: "#223344" } as ConstructorParameters<typeof Theme>[1], "truecolor");
	const line = uiActivityLine({ id: "tool:2", type: "tool", status: "completed", label: "read", contextSizeEstimate: 775, detail: "中文摘要".repeat(25), at: 0 }, theme);
	assert.ok(visibleWidth(line) <= 70);
	assert.ok(stripTerminalSequences(line).endsWith("…"));
	assert.doesNotMatch(line, /\x1b\[0m/, "a full SGR reset would clear the enclosing background");
	const framed = theme.bg("selectedBg", line + " ".repeat(10));
	assert.match(framed, /…\x1b\[39m {10}\x1b\[49m$/);
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

test("worker details retain status icons and render session icons in the preset color", () => {
	const colors: Array<[string, string]> = [];
	const theme = { fg: (color: string, text: string) => { colors.push([color, text]); return text; } } as Theme;
	for (const sessionMode of ["new", "fork"] as const) {
		for (const status of ["queued", "running", "completed", "blocked", "failed"] as const) {
			const details: WorkerUiDetails = {
				kind: "worker-ui", startedAt: 1, limit: 1, total: 1, completed: 0,
				tasks: [{
					index: 0, mode: "scout", objective: "inspect", status, sessionMode,
					requestedPreset: "fast", attempt: 0, phase: "fixture", activities: [], toolCalls: 0,
					usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 0 },
				}],
			};
			for (const expanded of [false, true]) {
				for (const snapshot of [false, true]) {
					colors.length = 0;
					const header = renderWorkerDetails(details, theme, { expanded, snapshot }).render(120)[0];
					const icon = sessionMode === "fork" ? "" : "󰐖";
					const statusIcon = { queued: "○", running: "◌", completed: "✓", blocked: "!", failed: "✗" }[status];
					assert.ok(header.startsWith(`${statusIcon} scout ${icon} · fast`), `${sessionMode}/${status}/${expanded}/${snapshot}: ${header}`);
					assert.ok(!header.includes(` · ${icon}`), "session icon must not be a dot-separated runtime field");
					assert.equal(colors.find(([, text]) => text === icon)?.[0], "muted");
					assert.equal(colors.find(([, text]) => text.startsWith(" · fast"))?.[0], "muted");
				}
			}
		}
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

test("result compaction preserves every field's standard and minimal limits", () => {
	const limits = {
		summary: [4, 2], changed_files: [50, 20], validation: [8, 3], acceptance: [8, 3],
		findings: [10, 4], risks: [6, 3], out_of_scope: [4, 2], recommended_next_action: [4, 2],
	};
	const source = {
		status: "completed", extra: "omitted",
		...Object.fromEntries(Object.keys(limits).map((key) => [key, Array.from({ length: 60 }, (_, i) => `${key}:${i}`)])),
	};
	const original = structuredClone(source);
	for (const [i, level] of (["standard", "minimal"] as const).entries()) {
		const result = compactWorkerResult(source, level);
		assert.deepEqual(Object.keys(result), ["status", "execution", "failure", ...Object.keys(limits)]);
		assert.equal(result.status, "completed");
		for (const [key, counts] of Object.entries(limits)) {
			assert.deepEqual(result[key], (source as any)[key].slice(0, counts[i]), `${level}.${key}`);
		}
		for (const malformed of [undefined, null, "text", {}, 42]) {
			const compact = compactWorkerResult(Object.fromEntries(Object.keys(limits).map((key) => [key, malformed])), level);
			for (const key of Object.keys(limits)) assert.deepEqual(compact[key], []);
		}
	}
	assert.deepEqual(source, original);
});

test("both compaction levels preserve fixed execution diagnostics through serialization", () => {
	const execution = {
		resolved_preset: "fast", actual_model_id: "fixture/local", actual_thinking: "high", attempt: 1,
		usage: { input: 42, output: 17, cacheRead: 0, cacheWrite: 0, turns: 1 }, exit_code: 1,
		timed_out: false, cancelled: true,
		termination: { stage: "process", code: "session_shutdown", source: "session_shutdown", parentPid: 1, workerPid: 2, ownerId: "fixture" },
		termination_source: "session_shutdown", warnings: ["configuration migrated"],
	};
	const source = { status: "failed", execution, summary: Array(20).fill("诊断内容".repeat(100)) };
	const before = structuredClone(source);
	for (const level of ["standard", "minimal"] as const) {
		const compact = compactWorkerResult(source, level);
		assert.deepEqual(compact.execution, execution, `${level}: fixed keys must not be truncated`);
		const expected = { status: "failed", ...(level === "minimal" ? { truncated: true } : {}), results: [compact] };
		const budget = Buffer.byteLength(JSON.stringify(expected));
		const serialized = serializePayload({ status: "failed", results: [compactWorkerResult(source)] }, budget);
		assert.deepEqual(JSON.parse(serialized.text), JSON.parse(JSON.stringify(expected)), `${level}: preserve diagnostics at the output boundary`);
		assert.equal(Buffer.byteLength(serialized.text), budget);
		const bounded = compactWorkerResult({ execution: {
			warnings: Array(20).fill("长".repeat(500)),
			termination: Object.fromEntries(Array.from({ length: 20 }, (_, i) => [`key${i}`, i])),
		} }, level).execution;
		assert.equal(bounded.warnings.length, level === "standard" ? 10 : 5);
		assert.ok(bounded.warnings.every((warning: string) => Buffer.byteLength(warning) <= (level === "standard" ? 768 : 256)));
		assert.equal(Object.keys(bounded.termination).length, level === "standard" ? 10 : 8);
	}
	assert.deepEqual(source, before, "compaction must not mutate the source");
});

test("serialization measures compact UTF-8 JSON, not indentation overhead", () => {
	const payload = { status: "completed", summary: ["保留空格  和换行\n引号：\"fixture\""], extra: { nested: Array(40).fill("保留") } };
	const text = JSON.stringify(payload);
	const budget = Buffer.byteLength(text);
	assert.ok(Buffer.byteLength(JSON.stringify(payload, null, 2)) > budget);
	const result = serializePayload(payload, budget);
	assert.equal(result.payload, payload, "a fitting payload must not be compacted or lose unknown fields");
	assert.equal(result.text, text);
});
