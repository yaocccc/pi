import assert from "node:assert/strict";
import test from "node:test";
import workerExtension, { validateWorkerInput, batchResponse, batchUiSnapshot } from "./index.ts";
import { WorkerRuntime } from "./runtime.ts";
import { DEFAULT_OPTIONS } from "./config.ts";
import { compactWorkerResult } from "./data.ts";
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
			events: { emit() {} }, sendMessage() { throw new Error("no queued continuations"); },
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


test("title survives schema, execution and UI snapshots without replacing objective", async () => {
	const titled = { ...task, title: "Inspect task cards" };
	const h = harness(async (received) => {
		assert.equal(received.title, titled.title);
		assert.equal(received.objective, task.objective);
		return structuredClone(complete);
	});
	try {
		for (const title of [undefined, "", " \t ", titled.title]) {
			assert.doesNotThrow(() => validateWorkerInput({ tasks: [{ ...task, title }] }));
		}
		assert.throws(() => validateWorkerInput({ tasks: [{ ...task, title: 42 }] }));
		const response = await h.call({ tasks: [titled] });
		assert.equal(response.details.tasks[0].status, "completed");
		assert.equal(response.details.tasks[0].title, titled.title);
		assert.equal(response.details.tasks[0].objective, task.objective);
	} finally { await h.close(); }
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

test("continuations preserve completed results and do not reload settings or admit work", async () => {
	let starts = 0;
	const h = harness(async () => { starts++; return structuredClone(batchResultFixture); });
	const expected = { ...compactWorkerResult(batchResultFixture), observed_changed_files: batchResultFixture.observed_changed_files };
	try {
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
	} finally { await h.close(); }
});

test("blocked worker results return immediately without a waiting state or legacy response fields", async () => {
	const h = harness(async () => ({ status: "blocked", summary: ["Need a decision"], recommended_next_action: ["Provide context in a new contract"] }));
	try {
		const result = payload(await h.call({ tasks: [task] }));
		assert.equal(result.finished, true);
		assert.equal(result.status, "partial");
		assert.equal(result.tasks[0].status, "blocked");
		assert.equal(result.result.results[0].status, "blocked");
		assert.deepEqual(Object.keys(result).sort(), ["batchId", "finished", "next_action", "result", "status", "tasks"]);
		assert.equal(payload(await h.call({ tasks: [task] })).finished, true);
	} finally { await h.close(); }
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

test("one invalid directory scope rejects the entire batch before routing or subprocess execution", async () => {
	let starts = 0;
	const h = harness(async () => { starts++; return complete; });
	try {
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
	} finally { await h.close(); }
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
