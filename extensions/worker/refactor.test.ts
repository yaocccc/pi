import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { DEFAULT_OPTIONS } from "./config.ts";
import workerExtension, { batchResponse, batchUiSnapshot, InputSchema, TOOL_DESCRIPTION } from "./index.ts";
import { start, task } from "./fixtures/helpers.ts";
import { WorkerRuntime } from "./runtime.ts";
import { executeTask, workerPromptBody } from "./process.ts";
import type { RoutingConfig, WorkerTask } from "./types.ts";
import { compactWorkerResult, serializePayload } from "./ui.ts";

test("always-loaded tool metadata and worker system prompt stay within compact budgets", (t) => {
	assert.ok(Buffer.byteLength(TOOL_DESCRIPTION) <= 1850);
	assert.ok(Buffer.byteLength(JSON.stringify(InputSchema)) <= 3300);
	const previous = process.env.PI_CODING_AGENT_DIR;
	process.env.PI_CODING_AGENT_DIR = fileURLToPath(new URL("../../", import.meta.url));
	t.after(() => {
		if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
		else process.env.PI_CODING_AGENT_DIR = previous;
	});
	const body = workerPromptBody();
	assert.ok(Buffer.byteLength(body) <= 2850);
	assert.ok(!body.startsWith("---"));
	for (const instruction of ["allowedPaths", "forbiddenPaths", "dir/**", "ask_parent", "questionId", "blocked", "不覆盖或撤销已有修改", "不可逆命令"]) {
		assert.ok(body.includes(instruction), instruction);
	}
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

const batchResultFixture = {
	status: "completed",
	execution: { requested_preset: "fast", resolved_preset: "fast", actual_model_id: "fixture/local", actual_thinking: "high", attempt: 1, warnings: [], usage: { input: 42, output: 17, cacheRead: 0, cacheWrite: 0, contextTokens: 59, turns: 1 } },
	failure: null,
	summary: ["保留中文、空格  和换行\n第二行", 'Quotes: "fixture", path: C:\\fixture'],
	changed_files: ["src/fixture.ts"],
	observed_changed_files: ["src/fixture.ts", "tests/fixture.ts"],
	validation: [{ command: "fixture test", result: "passed", details: "2 checks" }],
	acceptance: [{ criterion: "preserve data", result: "passed", evidence: "fixture" }],
	findings: [{ severity: "info", file: "src/fixture.ts", location: "1", problem: "fixture only", evidence: "none", impact: "none", recommendation: "none", confidence: 1 }],
	risks: ["fixture risk"],
	out_of_scope: ["fixture exclusion"],
	recommended_next_action: ["Review actual diff."],
};

test("both compaction levels preserve fixed execution diagnostics through serialization", () => {
	const execution = {
		resolved_preset: "fast", actual_model_id: "fixture/local", actual_thinking: "high", attempt: 1,
		usage: { input: 42, output: 17, cacheRead: 0, cacheWrite: 0, turns: 1 }, exit_code: 1,
		timed_out: false, cancelled: true,
		termination: { stage: "ipc", code: "EOF", source: "session_shutdown", parentPid: 1, workerPid: 2, ownerId: "fixture" },
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

function assertCompactBatch(result: any, expected: any) {
	const text = result.content[0].text;
	const legacyText = JSON.stringify(expected, null, 2);
	assert.deepEqual(result.content, [{ type: "text", text }]);
	assert.deepEqual(JSON.parse(text), JSON.parse(legacyText));
	// Reformatting also checks every nested property's insertion order and string whitespace.
	assert.equal(JSON.stringify(JSON.parse(text), null, 2), legacyText);
	assert.equal(text, JSON.stringify(expected));
	assert.ok(Buffer.byteLength(text, "utf8") < Buffer.byteLength(legacyText, "utf8"));
	return { pretty: Buffer.byteLength(legacyText, "utf8"), compact: Buffer.byteLength(text, "utf8") };
}

test("batch JSON removes only formatting whitespace, preserving all fields and UI details", async (t) => {
	t.mock.method(Date, "now", () => 1_700_000_000_000);
	const runtime = new WorkerRuntime(() => false);
	t.after(() => runtime.dispose());
	const batch = start(runtime, [task("src/fixture.ts"), task("tests/fixture.ts")], async () => structuredClone(batchResultFixture));
	await runtime.wait(batch.id);
	batch.questions = [{ id: "fixture-question", batchId: batch.id, taskId: batch.tasks[0].id, question: "保留这个字段？", timeoutMs: 1000, askedAt: Date.now(), expiresAt: Date.now() + 1000, status: "answered", answer: "是，保留。", answeredAt: Date.now() }];
	const expectedPayload = {
		batchId: batch.id,
		status: "completed",
		finished: true,
		tasks: batch.tasks.map((item) => ({ taskId: item.id, status: "completed" })),
		questions: [],
		history: structuredClone(batch.questions),
		historyOffset: 0,
		historyTotal: 1,
		nextHistoryOffset: null,
		result: { status: "completed", results: [batchResultFixture, batchResultFixture] },
		next_action: "Review results and actual diff.",
	};
	const expectedDetails = { ...batchUiSnapshot(batch), payload: expectedPayload };
	const beforeUi = structuredClone(batch.ui);
	const beforeQuestions = structuredClone(batch.questions);
	const result = batchResponse(batch);
	const bytes = assertCompactBatch(result, expectedPayload);
	assert.deepEqual(result.details, expectedDetails);
	assert.deepEqual(batch.ui, beforeUi);
	assert.deepEqual(batch.questions, beforeQuestions);
	t.diagnostic(`Typical two-task batch UTF-8 bytes: ${bytes.pretty} -> ${bytes.compact} (not a token count)`);
});

test("mocked start/continue return compact single and multi results; renderer uses details, not JSON text", async (t) => {
	let tool: any;
	let calls = 0;
	const handlers = new Map<string, any>();
	const preset = { model: "fixture/local", thinking: "high" as const };
	const depth = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	try {
		workerExtension({
			on: (name: string, handler: any) => handlers.set(name, handler),
			registerTool: (definition: any) => { tool = definition; },
			registerCommand() {}, events: { emit() {} },
		} as any, {
			loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, fast: preset, normal: preset, deep: preset }, warnings: [], path: "fixture" }),
			executeTask: async () => { calls++; return structuredClone(batchResultFixture); },
		});
	} finally {
		if (depth === undefined) delete process.env.PI_WORKER_DEPTH;
		else process.env.PI_WORKER_DEPTH = depth;
	}
	t.after(() => handlers.get("session_shutdown")({}));
	const ctx = { cwd: process.cwd(), hasUI: false };
	const theme = { fg: (_: string, text: string) => text, bold: (text: string) => text };
	const fixtureTask = { mode: "scout", objective: "inspect fixture" };
	const expectedResult = { ...compactWorkerResult(batchResultFixture), observed_changed_files: batchResultFixture.observed_changed_files };
	for (const single of [true, false]) {
		const input = single ? { task: fixtureTask } : { tasks: [fixtureTask, fixtureTask] };
		const first = await tool.execute(`fixture-${single}`, input, undefined, undefined, ctx);
		assertCompactBatch(first, first.details.payload);
		assert.equal(first.details.payload.finished, true);
		assert.deepEqual(first.details.payload.result, single ? expectedResult : { status: "completed", results: [expectedResult, expectedResult] });
		const previousCalls = calls;
		const continued = await tool.execute("continue", { batchId: first.details.batchId }, undefined, undefined, ctx);
		assertCompactBatch(continued, first.details.payload);
		assert.equal(calls, previousCalls, "continue must not schedule more tasks");
		assert.deepEqual(continued.details, { ...first.details, snapshotAt: continued.details.snapshotAt });
		const before = structuredClone(first.details);
		for (const expanded of [false, true]) {
			const render = (result: any) => tool.renderResult(result, { expanded, isPartial: false }, theme).render(160);
			const normal = render(first);
			assert.ok(normal.length > 0);
			assert.deepEqual(render({ details: first.details, get content() { throw new Error("worker UI must not read JSON content"); } }), normal);
		}
		assert.deepEqual(first.details, before, "rendering must not alter details");
	}
	assert.equal(calls, 3, "only fixture executors were used");
});

test("preflight failures preserve category, routing metadata and result shape without launching a worker", async (t) => {
	const cwd = fs.mkdtempSync(path.join(os.tmpdir(), "worker-preflight-"));
	t.after(() => fs.rmSync(cwd, { recursive: true, force: true }));
	const task: WorkerTask = { mode: "scout", objective: "inspect", preset: "fast" };
	const preset = { model: "fixture/model", thinking: "high" as const };
	const config: RoutingConfig = { ...DEFAULT_OPTIONS, fast: preset, normal: preset, deep: preset };
	for (const available of [false, true]) {
		const ctx = { cwd, modelRegistry: { getAvailable: () => available ? [{ provider: "fixture", id: "model", reasoning: true }] : [] } } as any;
		const result = await executeTask(task, config, ["warning"], ctx, undefined);
		assert.equal(result.status, "blocked");
		assert.equal(result.failure.category, available ? "workspace_snapshot" : "route_or_contract");
		assert.equal(result.failure.retryable, false);
		assert.deepEqual(result.summary, [result.failure.reason]);
		assert.deepEqual(result.recommended_next_action, [result.failure.next_action]);
		assert.equal(result.execution.attempt, 0);
		assert.equal(result.execution.requested_preset, "fast");
		assert.equal(result.execution.resolved_preset, available ? "fast" : null);
		assert.equal(result.execution.resolved_model_id, available ? "fixture/model" : null);
		assert.deepEqual(result.execution.warnings, ["warning"]);
		for (const key of ["changed_files", "observed_changed_files", "validation", "acceptance", "findings", "risks", "out_of_scope"]) {
			assert.deepEqual(result[key], [], key);
		}
	}
});
