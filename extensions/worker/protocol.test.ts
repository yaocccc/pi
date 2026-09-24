import assert from "node:assert/strict";
import test from "node:test";
import workerExtension, { validateWorkerInput } from "./index.ts";
import { DEFAULT_OPTIONS } from "./config.ts";
import type { executeTask } from "./process.ts";
import { until } from "./fixtures/helpers.ts";

const complete = { status: "completed", summary: ["done"] };
const task = { mode: "scout", objective: "fixture" };
const answer = { taskId: "task", questionId: "question", answer: "yes" };
const payload = (result: any) => JSON.parse(result.content[0].text);
const answersFor = (result: any) => result.questions.map((q: any) => ({ taskId: q.taskId, questionId: q.id, answer: `answer ${q.id}` }));
function deferred() {
	let resolve!: () => void;
	const promise = new Promise<void>((done) => { resolve = done; });
	return { promise, resolve };
}
function harness(execute: typeof executeTask, automaticDelegationEnabled = true, maxConcurrentWorkers = DEFAULT_OPTIONS.maxConcurrentWorkers) {
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
			loadRoutingConfig: () => { loads++; return { config: { ...DEFAULT_OPTIONS, automaticDelegationEnabled, maxConcurrentWorkers, fast: preset, normal: preset, deep: preset, max: preset }, warnings: [], path: "fixture" }; },
		});
	} finally {
		if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth;
	}
	const tool = tools.get("worker");
	return {
		tools, loads: () => loads,
		call: (input: any, signal?: AbortSignal) => tool.execute("fixture-call", input, signal, undefined, { cwd: process.cwd(), hasUI: false }),
		close: () => handlers.get("session_shutdown")({}),
	};
}

test("single-tool input modes strictly reject mixed, missing, unknown and malformed fields", () => {
	for (const input of [
		{ task }, { tasks: [task], manual: true }, { task, manual: false },
		{ batchId: "batch" }, { batchId: "batch", answers: [answer] },
		{ batchId: "batch", cancel: true }, { batchId: "batch", questionOffset: 0 },
		{ batchId: "batch", answers: [answer], questionOffset: 8 },
	]) assert.doesNotThrow(() => validateWorkerInput(input));
	for (const input of [
		undefined, null, [], {}, { task, tasks: [task] }, { task: null }, { tasks: [] },
		{ tasks: Array(13).fill(task) }, { task: { ...task, mode: "invalid" } },
		{ task: { ...task, objective: 1 } }, { task: { ...task, unknown: true } },
		{ task: { ...task, allowedPaths: [1] } }, { task, manual: "yes" },
		{ task, waitMs: 0 }, { task, batchId: "batch" }, { tasks: [task], batchId: "batch" },
		{ answers: [answer] }, { cancel: true }, { questionOffset: 0 },
		{ task, answers: [answer] }, { task, cancel: true }, { task, questionOffset: 0 },
		{ batchId: "" }, { batchId: " " }, { batchId: 1 }, { batchId: "batch", manual: false },
		{ batchId: "batch", cancel: false }, { batchId: "batch", cancel: true, answers: [answer] },
		{ batchId: "batch", cancel: true, questionOffset: 0 }, { batchId: "batch", waitMs: 0 },
		{ batchId: "batch", questionOffset: -1 }, { batchId: "batch", questionOffset: 1.5 },
		{ batchId: "batch", answers: [] }, { batchId: "batch", answers: Array(385).fill(answer) },
		{ batchId: "batch", answers: [null] }, { batchId: "batch", answers: [{ ...answer, answer: 1 }] },
		{ batchId: "batch", answers: [{ ...answer, answer: "" }] },
		{ batchId: "batch", answers: [{ ...answer, answer: "x".repeat(4001) }] },
		{ batchId: "batch", answers: [{ ...answer, taskId: "" }] },
		{ batchId: "batch", answers: [{ ...answer, extra: true }] },
	]) assert.throws(() => validateWorkerInput(input), /参数错误/, JSON.stringify(input));
});

test("invalid starts admit no work; continuations bypass routing/delegation settings and never create batches", async () => {
	let starts = 0;
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		starts++;
		await ask!({ id: "one", question: "Continue?", timeoutMs: 5_000 }, signal!);
		return complete;
	}, false);
	try {
		assert.deepEqual([...h.tools.keys()], ["worker"]);
		await assert.rejects(h.call({ task, tasks: [task] }), /参数错误/);
		assert.equal(h.loads(), 0);
		assert.equal(payload(await h.call({ task })).status, "blocked");
		assert.equal(starts, 0);
		const first = payload(await h.call({ task, manual: true }));
		const loads = h.loads();
		const continued = payload(await h.call({ batchId: first.batchId }));
		assert.equal(continued.batchId, first.batchId);
		assert.equal(continued.questions[0].id, "one");
		const done = payload(await h.call({ batchId: first.batchId, answers: answersFor(first) }));
		assert.equal(done.finished, true);
		assert.equal(done.batchId, first.batchId);
		assert.equal(starts, 1);
		assert.equal(h.loads(), loads);
		await assert.rejects(h.call({ batchId: "foreign" }), /未知/);
	} finally { await h.close(); }
});

test("batch answers are atomic, reject foreign/duplicate IDs and support parallel multi-round Q&A", async () => {
	let answered = 0;
	const h = harness(async (task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		for (let round = 1; round <= 2; round++) {
			const id = `${task.objective}-${round}`;
			assert.equal(await ask!({ id, question: `Round ${round}`, timeoutMs: 5_000 }, signal!), `answer ${id}`);
			answered++;
		}
		return complete;
	});
	try {
		const first = payload(await h.call({ tasks: [{ ...task, objective: "a" }, { ...task, objective: "b" }] }));
		assert.equal(first.questions.length, 2);
		const good = answersFor(first);
		for (const [invalid, pattern] of [
			[[good[0], good[0]], /重复/],
			[[good[0], { ...good[1], taskId: "foreign" }], /跨批次/],
			[[good[0], { ...good[1], questionId: "foreign" }], /未知/],
			[[good[0], { ...good[1], questionId: good[0].questionId }], /跨任务/],
			[[good[0], { ...good[1], answer: "  " }], /回答为空/],
		] as const) {
			await assert.rejects(h.call({ batchId: first.batchId, answers: invalid }), pattern);
			assert.equal(answered, 0);
			assert.equal(payload(await h.call({ batchId: first.batchId })).questions.length, 2);
		}
		const next = payload(await h.call({ batchId: first.batchId, answers: good }));
		assert.equal(answered, 2);
		assert.equal(next.finished, false);
		assert.deepEqual(next.questions.map((q: any) => q.id), ["a-2", "b-2"]);
		await assert.rejects(h.call({ batchId: first.batchId, answers: good }), /重复/);
		const done = payload(await h.call({ batchId: first.batchId, answers: answersFor(next) }));
		assert.equal(done.batchId, first.batchId);
		assert.equal(done.finished, true);
		assert.equal(answered, 4);
		assert.deepEqual(done.result.results.map((item: any) => item.status), ["completed", "completed"]);
		assert.ok(done.history.every((q: any) => q.status === "answered"));
	} finally { await h.close(); }
});

test("partial answers return unanswered questions immediately and actual foreign batch IDs are rejected", async () => {
	const h = harness(async (task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		await ask!({ id: task.objective, question: "Q", timeoutMs: 5_000 }, signal!);
		return complete;
	});
	try {
		const first = payload(await h.call({ tasks: [{ ...task, objective: "a" }, { ...task, objective: "b" }] }));
		assert.equal(payload(await h.call({ task: { ...task, objective: "foreign" } })).activeBatchId, first.batchId);
		assert.equal(payload(await h.call({ batchId: first.batchId })).questions.length, 2);
		const partial = payload(await h.call({ batchId: first.batchId, answers: [answersFor(first)[0]] }));
		assert.equal(partial.finished, false);
		assert.deepEqual(partial.questions.map((q: any) => q.id), ["b"]);
		assert.equal(payload(await h.call({ batchId: first.batchId, answers: answersFor(partial) })).finished, true);
		const foreign = payload(await h.call({ task: { ...task, objective: "foreign" } }));
		const again = payload(await h.call({ task }));
		assert.equal(again.activeBatchId, foreign.batchId);
		await assert.rejects(h.call({ batchId: foreign.batchId, answers: [{ ...answersFor(foreign)[0], taskId: first.tasks[0].taskId }] }), /跨批次/);
		assert.equal(payload(await h.call({ batchId: foreign.batchId, cancel: true })).finished, true);
	} finally { await h.close(); }
});

test("same-turn concurrent starts reject the second immediately while the first question remains answerable", async () => {
	let starts = 0;
	const h = harness(async (running, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		starts++;
		if (running.objective === "ask") await ask!({ id: "prompt", question: "Choose?", timeoutMs: 5_000 }, signal!);
		return complete;
	}, true, 1);
	try {
		let timer: ReturnType<typeof setTimeout> | undefined;
		const deadline = new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error("second start waited for the first question")), 1_000); });
		let first: any;
		let refused: any;
		try {
			[first, refused] = await Promise.race([Promise.all([
				h.call({ task: { ...task, objective: "ask" } }),
				h.call({ task: { ...task, objective: "other" } }),
			]), deadline]);
		} finally { clearTimeout(timer); }
		const pending = payload(first);
		const blocked = payload(refused);
		assert.equal(pending.finished, false);
		assert.equal(pending.questions[0].id, "prompt");
		assert.equal(blocked.status, "blocked");
		assert.equal(blocked.error, "active_batch");
		assert.equal(blocked.activeBatchId, pending.batchId);
		assert.match(blocked.next_action, /answers.*继续等待.*cancel/);
		assert.equal(starts, 1, "rejected invocation must not admit another task");
		const sequential = payload(await h.call({ task }));
		assert.equal(sequential.activeBatchId, pending.batchId);
		const done = payload(await h.call({ batchId: pending.batchId, answers: answersFor(pending) }));
		assert.equal(done.finished, true);
		assert.equal(done.history[0].status, "answered");
		assert.equal(payload(await h.call({ task })).finished, true);
		assert.equal(starts, 2);
	} finally { await h.close(); }
});

test("a second start is rejected while the first still waits without any question", async () => {
	const finish = deferred();
	let starts = 0;
	const h = harness(async () => { starts++; await finish.promise; return complete; }, true, 1);
	try {
		let firstReturned = false;
		const first = h.call({ task }).then((value: any) => { firstReturned = true; return value; });
		const secondCall = h.call({ task });
		assert.equal(starts, 0, "admission is synchronous even before execution microtasks");
		const second = payload(await secondCall);
		assert.equal(second.status, "blocked");
		assert.ok(second.activeBatchId);
		assert.equal(firstReturned, false);
		await until(() => starts === 1);
		assert.equal(firstReturned, false);
		finish.resolve();
		assert.equal(payload(await first).batchId, second.activeBatchId);
		assert.equal(payload(await h.call({ task })).finished, true);
	} finally { finish.resolve(); await h.close(); }
});

test("single-tool cancel waits for cleanup and refuses new batches until released", async () => {
	const cleanup = deferred();
	let starts = 0;
	let aborted = false;
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		if (++starts === 1) {
			try { await ask!({ id: "cancel", question: "Q", timeoutMs: 5_000 }, signal!); }
			finally { aborted = signal!.aborted; await cleanup.promise; }
		}
		return complete;
	});
	try {
		const write = { mode: "implement", objective: "same path", allowedPaths: ["same.ts"] };
		const first = payload(await h.call({ tasks: [write, write] }));
		let cancelledReturned = false;
		const cancellation = h.call({ batchId: first.batchId, cancel: true }).then((value: any) => { cancelledReturned = true; return value; });
		const second = payload(await h.call({ task: write }));
		assert.equal(second.status, "blocked");
		assert.equal(second.activeBatchId, first.batchId);
		await until(() => aborted);
		assert.equal(cancelledReturned, false);
		assert.equal(starts, 1);
		cleanup.resolve();
		const cancelled = payload(await cancellation);
		assert.equal(cancelled.finished, true);
		assert.ok(cancelled.tasks.every((t: any) => t.status === "failed"));
		assert.equal(cancelled.questions.length, 0);
		assert.equal(payload(await h.call({ task: write })).finished, true);
		assert.equal(starts, 2, "queued sibling was cancelled; only the next batch starts");
		assert.equal(payload(await h.call({ batchId: first.batchId, cancel: true })).finished, true);
	} finally { cleanup.resolve(); await h.close(); }
});

for (const mode of ["initial", "answer", "continue", "pre-answer", "pre-continue"] as const) {
	test(`${mode} invocation abort cancels its batch, skips pre-aborted answers and awaits cleanup`, async () => {
		const running = deferred();
		const cleanup = deferred();
		let aborted = false;
		let answered = false;
		const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
			running.resolve();
			try {
				if (mode !== "initial") {
					await ask!({ id: "abort", question: "Q", timeoutMs: 5_000 }, signal!);
					answered = true;
				}
				await new Promise<void>((resolve) => {
					if (signal!.aborted) resolve(); else signal!.addEventListener("abort", () => resolve(), { once: true });
				});
			} finally { aborted = signal!.aborted; await cleanup.promise; }
			return complete;
		});
		try {
			const controller = new AbortController();
			let invocation: Promise<any>;
			let answerWait: Promise<any> | undefined;
			if (mode === "initial") {
				invocation = h.call({ task }, controller.signal);
				await running.promise;
			} else {
				const first = payload(await h.call({ task }));
				if (mode.startsWith("pre-")) controller.abort();
				if (mode === "continue") {
					answerWait = h.call({ batchId: first.batchId, answers: answersFor(first) });
					await until(() => answered);
				}
				invocation = h.call({ batchId: first.batchId, ...(mode.endsWith("answer") ? { answers: answersFor(first) } : {}) }, controller.signal);
			}
			let returned = false;
			void invocation.then(() => { returned = true; });
			controller.abort();
			await until(() => aborted);
			assert.equal(returned, false);
			cleanup.resolve();
			const done = payload(await invocation);
			assert.equal(done.finished, true);
			assert.equal(done.result.status, "failed");
			assert.equal(done.result.execution.cancelled, true);
			if (mode.startsWith("pre-")) assert.equal(answered, false);
			if (answerWait) assert.equal(payload(await answerWait).finished, true);
		} finally { cleanup.resolve(); await h.close(); }
	});
}

test("pre-aborted start admits no worker; completion without questions waits beyond 30 seconds", async (t) => {
	t.mock.timers.enable({ apis: ["setTimeout"] });
	const finish = deferred();
	let starts = 0;
	const h = harness(async () => { starts++; await finish.promise; return complete; });
	try {
		const controller = new AbortController(); controller.abort();
		await h.call({ task }, controller.signal);
		assert.equal(starts, 0);
		let returned = false;
		const invocation = h.call({ task }).then((value: any) => { returned = true; return value; });
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
		const invocation = h.call({ task }).then((value: any) => { returned = true; return value; });
		await running.promise;
		const closing = h.close();
		assert.equal(aborted, true);
		await Promise.resolve();
		assert.equal(returned, false);
		cleanup.resolve();
		await closing;
		assert.equal(payload(await invocation).finished, true);
		await assert.rejects(h.call({ task }), /会话已关闭/);
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
		const result = h.call({ task });
		await running.promise;
		t.mock.timers.tick(DEFAULT_OPTIONS.defaultTimeoutMs);
		const done = payload(await result);
		assert.equal(done.finished, true);
		assert.equal(done.result.failure.category, "timeout");
		assert.equal(done.result.execution.timed_out, true);
	} finally { await h.close(); t.mock.timers.reset(); }
});
