import assert from "node:assert/strict";
import test, { type TestContext } from "node:test";
import workerExtension, { validateWorkerInput, batchResponse, batchUiSnapshot } from "./index.ts";
import { WorkerRuntime } from "./runtime.ts";
import { DEFAULT_OPTIONS } from "./config.ts";
import { compactWorkerResult, serializePayload, sanitizeStructuredValue } from "./data.ts";
import type { executeTask } from "./process.ts";
import { until, start, task as writeTask } from "./fixtures/helpers.ts";

const complete = { status: "completed", summary: ["done"] };
const task = { mode: "scout", preset: "normal", sessionMode: "fork", objective: "fixture" };
const payload = (result: any) => JSON.parse(result.content[0].text);
function deferred() {
	let resolve!: () => void;
	const promise = new Promise<void>((done) => { resolve = done; });
	return { promise, resolve };
}
function harness(execute: typeof executeTask, maxConcurrentWorkers = DEFAULT_OPTIONS.maxConcurrentWorkers) {
	const tools = new Map<string, any>();
	const handlers = new Map<string, any>();
	const oldDepth = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	let loads = 0;
	try {
		const preset = { model: "fixture/local", thinking: "high" as const };
		workerExtension({
			on: (event: string, fn: any) => handlers.set(event, fn),
			registerTool: (tool: any) => tools.set(tool.name, tool), registerCommand() {},
			events: { emit() {}, on() { return () => {}; } }, sendMessage() { throw new Error("no queued continuations"); },
		} as any, {
			executeTask: execute,
			loadRoutingConfig: () => { loads++; return { config: { ...DEFAULT_OPTIONS, maxConcurrentWorkers, fast: preset, normal: preset, deep: preset }, warnings: [], path: "fixture" }; },
		});
	} finally {
		if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth;
	}
	const tool = tools.get("worker");
	return {
		tools, loads: () => loads,
		call: (input: any, signal?: AbortSignal) => tool.execute("fixture-call", input, signal, undefined, { cwd: process.cwd(), hasUI: false, sessionManager: { getBranch: () => [] } }),
		close: () => handlers.get("session_shutdown")({}),
	};
}


function completingHarness(t: TestContext, execute: typeof executeTask) {
	const h = harness(execute);
	t.after(h.close);
	return h;
}

test("field clipping is visible, keeps explicit failures, and survives further byte compression", async (t) => {
	const result = {
		status: "completed",
		findings: [...Array.from({ length: 10 }, () => ({ severity: "low" })), { severity: "critical" }],
		validation: [...Array.from({ length: 8 }, () => ({ result: "passed" })), { result: "failed" }],
		acceptance: [...Array.from({ length: 8 }, () => ({ result: "passed" })), { result: "failed" }],
		changed_files: Array.from({ length: 51 }, (_, i) => String(i)),
	};
	assert.ok(Buffer.byteLength(JSON.stringify(result)) < 1024);
	const original = structuredClone(result);
	const compact = compactWorkerResult(result);
	assert.equal(compact.truncated, true);
	assert.deepEqual(compact.omitted, { findings: 1, validation: 1, acceptance: 1, changed_files: 1 });
	assert.equal(compact.findings[0].severity, "critical");
	assert.equal(compact.validation[0].result, "failed");
	assert.equal(compact.acceptance[0].result, "failed");
	const minimal = compactWorkerResult(compact, "minimal");
	assert.equal(minimal.omitted.findings, 7);
	assert.equal(minimal.omitted.validation, 6);
	for (const budget of [8192, 800, 300, 20]) {
		const serialized = serializePayload({ status: "completed", results: [compact] }, budget);
		assert.ok(Buffer.byteLength(serialized.text) <= budget);
		assert.equal(JSON.parse(serialized.text).truncated, true);
	}
	assert.equal(compactWorkerResult(complete).truncated, undefined);
	assert.equal(serializePayload({ results: [compactWorkerResult(complete)] }, 8192).payload.truncated, undefined);
	assert.deepEqual(result, original);
	const h = completingHarness(t, async () => structuredClone(result));
	const response = await h.call({ tasks: [task] });
	assert.equal(payload(response).result.truncated, true);
	assert.equal(payload(response).result.results[0].omitted.findings, 1);
	assert.equal(payload(response).result.results[0].validation[0].result, "failed");
});

test("real UI snapshots preserve numeric protocol usage and detach sanitized user content", async (t) => {
	const usage = { input: 20, output: 10, cacheRead: 3, cacheWrite: 4, contextTokens: 1200, turns: 2 };
	const result = { ...complete, execution: { usage }, failure: { api_key: "credential", accessToken: "credential", active: false, contextTokens: 1200 } };
	const original = structuredClone(result);
	const h = completingHarness(t, async () => result);
	const response = await h.call({ tasks: [task] });
	const snapshot = response.details;
	assert.deepEqual(snapshot.tasks[0].usage, usage);
	assert.deepEqual(JSON.parse(JSON.stringify(snapshot)).tasks[0].usage, usage);
	assert.deepEqual(sanitizeStructuredValue(result.failure), { api_key: "[已隐藏]", accessToken: "[已隐藏]", active: false, contextTokens: 1200 });
	snapshot.tasks[0].usage.contextTokens = -1;
	snapshot.tasks[0].activities.push({ label: "tampered" });
	const next = await h.call({ batchId: snapshot.batchId });
	assert.equal(next.details.tasks[0].usage.contextTokens, 1200);
	assert.equal(next.details.tasks[0].activities.some((item: any) => item.label === "tampered"), false);
	assert.deepEqual(result, original);
});

test("registered Worker redacts token credentials while preserving numeric counters and independent snapshots", async () => {
	const credentials = {
		accessTokenValue: "fixture-login-7f3b9c2d5e8a",
		tokenValue: "fixture-session-9b4e2d8c",
		password: "fixture-password-short",
		secret: "fixture-secret-short",
		authorization: "fixture-authorization-short",
		credentials: "fixture-credentials-short",
		secretAccessKey: "fixture-access-key-short",
	};
	const credentialCounters = { contextTokens: "fixture-counter-credential", tokenCount: "fixture-count-credential" };
	const findings = [{ ...credentials, contextTokens: 1200, tokenCount: 7 }, credentialCounters];
	const usage = { input: 20, output: 10, cacheRead: 3, cacheWrite: 4, contextTokens: 1200, turns: 2 };
	const original = { ...complete, findings, execution: { usage } };
	const before = structuredClone(original);
	const h = harness(async () => original);
	try {
		const response = await h.call({ tasks: [task] });
		const expected = [
			{ ...Object.fromEntries(Object.keys(credentials).map((key) => [key, "[已隐藏]"])), contextTokens: 1200, tokenCount: 7 },
			{ contextTokens: "[已隐藏]", tokenCount: "[已隐藏]" },
		];
		assert.deepEqual(payload(response).result.results[0].findings, expected);
		assert.deepEqual(response.details.tasks[0].result.findings, expected);
		assert.deepEqual(response.details.payload.result.results[0].findings, expected);
		assert.deepEqual(response.details.tasks[0].usage, usage);
		assert.deepEqual(payload(response).result.results[0].execution.usage, { input: 20, output: 10, cacheRead: 3, cacheWrite: 4, turns: 2 });
		for (const secret of [...Object.values(credentials), ...Object.values(credentialCounters)]) {
			assert.equal(response.content[0].text.includes(secret), false);
			assert.equal(JSON.stringify(response.details).includes(secret), false);
		}
		assert.deepEqual(original, before);
		response.details.tasks[0].usage.contextTokens = -1;
		response.details.tasks[0].result.findings[0].contextTokens = -1;
		response.details.payload.result.results[0].findings[0].tokenCount = -1;
		const next = await h.call({ batchId: response.details.batchId });
		assert.deepEqual(next.details.tasks[0].usage, usage);
		assert.deepEqual(next.details.tasks[0].result.findings, expected);
		assert.deepEqual(next.details.payload.result.results[0].findings, expected);
		assert.deepEqual(original, before);
	} finally { await h.close(); }
});

test("title survives schema, execution and UI snapshots without replacing objective", async (t) => {
	const titled = { ...task, title: "Inspect task cards" };
	const h = completingHarness(t, async (received) => {
		assert.equal(received.title, titled.title);
		assert.equal(received.objective, task.objective);
		return structuredClone(complete);
	});
	for (const title of [undefined, "", " \t ", titled.title]) {
		assert.doesNotThrow(() => validateWorkerInput({ tasks: [{ ...task, title }] }));
	}
	assert.throws(() => validateWorkerInput({ tasks: [{ ...task, title: 42 }] }));
	const response = await h.call({ tasks: [titled] });
	assert.equal(response.details.tasks[0].status, "completed");
	assert.equal(response.details.tasks[0].title, titled.title);
	assert.equal(response.details.tasks[0].objective, task.objective);
});

test("host transcript locator survives completion without progress and result compaction", async (t) => {
	const transcript = { version: 1 as const, id: "12345678-1234-4234-8234-123456789abc" };
	const h = completingHarness(t, async () => ({ ...complete, transcript }));
	const response = await h.call({ tasks: [task] });
	assert.deepEqual(response.details.tasks[0].transcript, transcript);
	assert.notEqual(response.details.tasks[0].transcript, transcript);
	const continued = await h.call({ batchId: payload(response).batchId });
	assert.deepEqual(continued.details.tasks[0].transcript, transcript);
});

test("only tasks-array starts, continuations and cancellations are accepted", () => {
	for (const input of [{ tasks: [task] }, { tasks: [task, task] }, { tasks: Array(12).fill(task) }, { batchId: "batch" }, { batchId: "batch", cancel: true }]) {
		assert.doesNotThrow(() => validateWorkerInput(input));
	}
	for (const input of [
		undefined, null, [], {}, { task }, { task, tasks: [task] }, { tasks: task },
		{ tasks: undefined }, { tasks: null }, { tasks: [] }, { tasks: Array(13).fill(task) },
		{ tasks: [{ ...task, mode: "invalid" }] }, { tasks: [{ ...task, preset: "max" }] },
		...["preset", "sessionMode"].flatMap((field) => [
			{ tasks: [Object.fromEntries(Object.entries(task).filter(([key]) => key !== field))] },
			{ tasks: [{ ...task, [field]: undefined }] },
		]),
		{ tasks: [{ ...task, preset: "auto" }] },
		{ tasks: [{ ...task, unknown: true }] }, ...[true, false, "yes"].map((manual) => ({ tasks: [task], manual })), { tasks: [task], waitMs: 0 },
		{ tasks: [task], batchId: "batch" }, { tasks: [task], cancel: true }, { batchId: "" }, { batchId: " " },
		{ batchId: "batch", task }, { batchId: "batch", manual: true }, { batchId: "batch", cancel: false },
		{ batchId: "batch", answers: [{ taskId: "t", questionId: "q", answer: "yes" }] },
		{ batchId: "batch", questionOffset: 0 },
	]) assert.throws(() => validateWorkerInput(input), /参数错误/);
});

test("continuations preserve completed results and do not reload settings or admit work", async (t) => {
	let starts = 0;
	const h = completingHarness(t, async () => { starts++; return structuredClone(batchResultFixture); });
	const expected = { ...compactWorkerResult(batchResultFixture), observed_changed_files: batchResultFixture.observed_changed_files };
	assert.deepEqual([...h.tools.keys()], ["worker"]);
	await assert.rejects(h.call({ task }), /参数错误/);
	await assert.rejects(h.call({ task, tasks: [task] }), /参数错误/);
	await assert.rejects(h.call({ tasks: [task], manual: true }), /参数错误/);
	assert.equal(h.loads(), 0);
	assert.equal(starts, 0);
	for (const input of [{ tasks: [task] }, { tasks: [task, task] }]) {
		const first = await h.call(input);
		assert.deepEqual(first.details.payload.result, { status: "completed", results: input.tasks.map(() => expected) });
		const loads = h.loads();
		const calls: number = starts;
		const continued = await h.call({ batchId: first.details.batchId });
		assert.deepEqual(continued.content, first.content);
		assert.equal(first.content[0].text, JSON.stringify(payload(first)));
		assert.deepEqual(continued.details, { ...first.details, snapshotAt: continued.details.snapshotAt });
		assert.equal(payload(await h.call({ batchId: first.details.batchId, cancel: true })).finished, true);
		assert.equal(h.loads(), loads);
		assert.equal(starts, calls);
	}
	assert.equal(starts, 3);
	await assert.rejects(h.call({ batchId: "foreign" }), /未知/);
});

test("blocked worker results return immediately without a waiting state or legacy response fields", async (t) => {
	const h = completingHarness(t, async () => ({ status: "blocked", summary: ["Need a decision"], recommended_next_action: ["Provide context in a new contract"] }));
	const result = payload(await h.call({ tasks: [task] }));
	assert.equal(result.finished, true);
	assert.equal(result.status, "partial");
	assert.equal(result.tasks[0].status, "blocked");
	assert.equal(result.result.results[0].status, "blocked");
	assert.deepEqual(Object.keys(result).sort(), ["batchId", "finished", "next_action", "result", "status", "tasks"]);
	assert.equal(payload(await h.call({ tasks: [task] })).finished, true);
});

test("cancel waits for cleanup, cancels queued siblings and prevents new admission until released", async () => {
	const cleanup = deferred(), running = deferred();
	let starts = 0, aborted = false;
	const h = harness(async (_task, _config, _warnings, _ctx, signal) => {
		if (++starts === 1) {
			running.resolve();
			await new Promise<void>((resolve) => signal!.addEventListener("abort", () => { aborted = true; resolve(); }, { once: true }));
			await cleanup.promise;
		}
		return complete;
	}, 1);
	try {
		const write = { mode: "implement", preset: "normal", sessionMode: "fork", objective: "same", allowedPaths: ["same.ts"] };
		const first = h.call({ tasks: [write, write] });
		await running.promise;
		const blocked = payload(await h.call({ tasks: [write] }));
		let returned = false;
		const cancellation = h.call({ batchId: blocked.activeBatchId, cancel: true }).then((value: any) => { returned = true; return value; });
		await until(() => aborted);
		assert.equal(returned, false);
		assert.equal(payload(await h.call({ tasks: [write] })).activeBatchId, blocked.activeBatchId);
		cleanup.resolve();
		const done = payload(await cancellation);
		assert.equal(done.finished, true);
		assert.ok(done.tasks.every((item: any) => item.status === "failed"));
		assert.equal(payload(await first).finished, true);
		assert.equal(payload(await h.call({ tasks: [write] })).finished, true);
		assert.equal(starts, 2);
	} finally { cleanup.resolve(); await h.close(); }
});

for (const mode of ["initial", "continue", "pre-continue"] as const) {
	test(`${mode} invocation abort cancels and awaits cleanup`, async () => {
		const running = deferred(), cleanup = deferred();
		let aborted = false;
		const h = harness(async (_task, _config, _warnings, _ctx, signal) => {
			running.resolve();
			await new Promise<void>((resolve) => signal!.addEventListener("abort", () => { aborted = true; resolve(); }, { once: true }));
			await cleanup.promise; return complete;
		});
		try {
			const controller = new AbortController();
			const first = h.call({ tasks: [task] }, mode === "initial" ? controller.signal : undefined);
			await running.promise;
			let invocation = first;
			if (mode !== "initial") {
				const blocked = payload(await h.call({ tasks: [task] }));
				if (mode === "pre-continue") controller.abort();
				invocation = h.call({ batchId: blocked.activeBatchId }, controller.signal);
			}
			let returned = false;
			void invocation.then(() => { returned = true; });
			controller.abort(); await until(() => aborted);
			assert.equal(returned, false);
			cleanup.resolve();
			const done = payload(await invocation);
			assert.equal(done.finished, true);
			assert.equal(done.result.results[0].execution.cancelled, true);
			await first;
		} finally { cleanup.resolve(); await h.close(); }
	});
}

test("one invalid directory scope rejects the entire batch before routing or subprocess execution", async (t) => {
	let starts = 0;
	const h = completingHarness(t, async () => { starts++; return complete; });
	const result = payload(await h.call({ tasks: [
		{ mode: "implement", preset: "normal", sessionMode: "fork", objective: "backend", allowedPaths: ["extensions/worker"] },
		{ mode: "implement", preset: "normal", sessionMode: "fork", objective: "frontend", allowedPaths: ["README.md"] },
	] }));
	assert.equal(result.status, "failed");
	assert.match(result.validation_errors[0].errors[0], /extensions\/worker\/\*\*/);
	assert.deepEqual(result.validation_errors[1].errors, []);
	assert.equal(h.loads(), 0);
	assert.equal(starts, 0);
	assert.equal(payload(await h.call({ tasks: [{ mode: "implement", preset: "normal", sessionMode: "fork", objective: "valid", allowedPaths: ["README.md"] }] })).finished, true);
	assert.equal(starts, 1);
});


test("a second start is rejected while the first still waits before completion", async () => {
	const finish = deferred();
	let starts = 0;
	const h = harness(async () => { starts++; await finish.promise; return complete; }, 1);
	try {
		let firstReturned = false;
		const first = h.call({ tasks: [task] }).then((value: any) => { firstReturned = true; return value; });
		const secondCall = h.call({ tasks: [task] });
		assert.equal(starts, 0, "admission is synchronous even before execution microtasks");
		const second = payload(await secondCall);
		assert.equal(second.status, "blocked");
		assert.ok(second.activeBatchId);
		assert.equal(firstReturned, false);
		await until(() => starts === 1);
		assert.equal(firstReturned, false);
		finish.resolve();
		assert.equal(payload(await first).batchId, second.activeBatchId);
		assert.equal(payload(await h.call({ tasks: [task] })).finished, true);
	} finally { finish.resolve(); await h.close(); }
});


test("pre-aborted start admits no worker; completion  waits beyond 30 seconds", async (t) => {
	t.mock.timers.enable({ apis: ["setTimeout"] });
	const finish = deferred();
	let starts = 0;
	const h = harness(async () => { starts++; await finish.promise; return complete; });
	try {
		const controller = new AbortController(); controller.abort();
		await h.call({ tasks: [task] }, controller.signal);
		assert.equal(starts, 0);
		let returned = false;
		const invocation = h.call({ tasks: [task] }).then((value: any) => { returned = true; return value; });
		await Promise.resolve();
		t.mock.timers.tick(60_000);
		await Promise.resolve();
		assert.equal(returned, false);
		finish.resolve();
		assert.equal(payload(await invocation).finished, true);
		assert.equal(starts, 1);
	} finally { finish.resolve(); await h.close(); t.mock.timers.reset(); }
});


test("session shutdown wakes an indefinite invocation only after cleanup completes", async () => {
	const running = deferred();
	const cleanup = deferred();
	let aborted = false;
	const h = harness(async (_task, _config, _warnings, _ctx, signal) => {
		running.resolve();
		await new Promise<void>((resolve) => signal!.addEventListener("abort", () => { aborted = true; resolve(); }, { once: true }));
		await cleanup.promise;
		return complete;
	});
	try {
		let returned = false;
		const invocation = h.call({ tasks: [task] }).then((value: any) => { returned = true; return value; });
		await running.promise;
		const closing = h.close();
		assert.equal(aborted, true);
		await Promise.resolve();
		assert.equal(returned, false);
		cleanup.resolve();
		await closing;
		assert.equal(payload(await invocation).finished, true);
		await assert.rejects(h.call({ tasks: [task] }), /会话已关闭/);
	} finally { cleanup.resolve(); await h.close(); }
});


test("task timeout wakes an indefinite tool wait and returns cleanup-complete failure", async (t) => {
	t.mock.timers.enable({ apis: ["setTimeout"] });
	const running = deferred();
	const h = harness(async (_task, _config, _warnings, _ctx, signal) => {
		running.resolve();
		await new Promise<void>((resolve) => signal!.addEventListener("abort", () => resolve(), { once: true }));
		return complete;
	});
	try {
		const result = h.call({ tasks: [task] });
		await running.promise;
		t.mock.timers.tick(DEFAULT_OPTIONS.defaultTimeoutMs);
		const done = payload(await result);
		assert.equal(done.finished, true);
		assert.equal(done.result.results[0].failure.category, "timeout");
		assert.equal(done.result.results[0].execution.timed_out, true);
	} finally { await h.close(); t.mock.timers.reset(); }
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

test("final tool text includes control metadata within its byte budget", async (t) => {
	const runtime = new WorkerRuntime(() => false);
	t.after(() => runtime.dispose());
	const batch = start(runtime, [writeTask("budget.ts")], async () => ({ status: "completed", summary: ["x".repeat(8000)] }));
	await runtime.wait(batch.id);
	batch.maxOutputBytes = 8192;
	const response = batchResponse(batch);
	assert.ok(Buffer.byteLength(response.content[0].text) <= batch.maxOutputBytes);
	assert.equal(payload(response).batchId, batch.id);
	assert.equal(payload(response).result.truncated, true);
});

test("single and multi batch JSON is compact and preserves fields and detached UI details", async (t) => {
	t.mock.method(Date, "now", () => 1_700_000_000_000);
	const runtime = new WorkerRuntime(() => false);
	t.after(() => runtime.dispose());
	for (const count of [1, 2]) {
		const batch = start(runtime, Array.from({ length: count }, (_, i) => writeTask(`fixture${i}.ts`)), async () => structuredClone(batchResultFixture));
		await runtime.wait(batch.id);
		const expectedPayload = {
			batchId: batch.id, status: "completed", finished: true,
			tasks: batch.tasks.map((item) => ({ taskId: item.id, status: "completed" })),
			result: { status: "completed", results: Array(count).fill(batchResultFixture) },
			next_action: "Review results and actual diff.",
		};
		const expectedDetails = { ...batchUiSnapshot(batch), payload: expectedPayload };
		const beforeUi = structuredClone(batch.ui);
		const result = batchResponse(batch);
		assert.deepEqual(result.content, [{ type: "text", text: JSON.stringify(expectedPayload) }]);
		assert.ok(Buffer.byteLength(result.content[0].text) < Buffer.byteLength(JSON.stringify(expectedPayload, null, 2)));
		assert.deepEqual(result.details, expectedDetails);
		assert.deepEqual(batch.ui, beforeUi);
	}
});
