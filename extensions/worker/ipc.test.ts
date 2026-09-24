import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
import { Duplex } from "node:stream";
import { ChildChannel, ParentChannel, type AskParent, type QuestionControl } from "./ipc.ts";
import { WorkerRuntime } from "./runtime.ts";
import { start, task, until } from "./fixtures/helpers.ts";
import { acquireSlot, activeChildren, activeSlots, beginWorkerShutdown, resetWorkerRuntime, runPiWorker, slotWaiters } from "./process.ts";
import type { Route } from "./types.ts";

const route: Route = { requestedPreset: "fast", resolvedPreset: "fast", modelId: "fixture/local", provider: "fixture", thinking: "high", routeReason: [] };
const fixture = fileURLToPath(new URL("./fixtures/child.mjs", import.meta.url));
const launch = (scenario: string, askParent?: AskParent, signal?: AbortSignal, timeout = 2_000, managedTimeout = false) => runPiWorker(
	{ mode: "scout", objective: "IPC fixture" }, route, process.cwd(), "fixture", "fixture", signal, timeout, 2, undefined,
	{ askParent, managedTimeout, invocation: { command: process.execPath, args: ["--experimental-transform-types", fixture, scenario] } },
);

// Streams deliberately split every UTF-8 character to test framing, not only ASCII happy paths.
function pair(): [Duplex, Duplex] {
	let left: Duplex;
	let right: Duplex;
	const build = (peer: () => Duplex) => new Duplex({ read() {}, write(chunk, _encoding, callback) { for (const byte of chunk) peer().push(Buffer.from([byte])); callback(); }, final(callback) { peer().push(null); callback(); } });
	left = build(() => right);
	right = build(() => left);
	return [left, right];
}

test("IPC supports fragmented Unicode, concurrent unique questions and out-of-order answers", async () => {
	const [left, right] = pair();
	const pending: Array<{ id: string; resolve: (value: string) => void }> = [];
	const parent = new ParentChannel(left, (question) => new Promise((resolve) => { pending.push({ id: question.id, resolve }); }));
	const child = new ChildChannel(right);
	const first = child.ask("中文问题一");
	const second = child.ask("中文问题二");
	await delay(5);
	assert.notEqual(pending[0].id, pending[1].id);
	pending[1].resolve("中文回答二");
	pending[0].resolve("中文回答一");
	assert.equal((await first).answer, "中文回答一");
	assert.equal((await second).answer, "中文回答二");
	child.close(); parent.close();
});

test("IPC timeout and abort notify parent, late answers do not revive calls, and close rejects pending requests", async () => {
	const [left, right] = pair();
	let aborted = 0;
	const resolvers: Array<(value: string) => void> = [];
	const parent = new ParentChannel(left, (_question, signal) => new Promise((resolve) => {
		resolvers.push(resolve);
		signal.addEventListener("abort", () => { aborted++; }, { once: true });
	}));
	const child = new ChildChannel(right);
	await assert.rejects(child.ask("timeout?", undefined, 15), /超时/);
	const controller = new AbortController();
	const cancelled = child.ask("abort?", controller.signal);
	await delay(5);
	controller.abort();
	await assert.rejects(cancelled, /取消/);
	await delay(5);
	assert.equal(aborted, 2);
	for (const resolve of resolvers) resolve("late");
	const pending = child.ask("close?");
	child.close();
	await assert.rejects(pending, /关闭/);
	parent.close();
});

test("invalid/oversized IPC frames close the channel without interpreting stdout as control", async () => {
	const [left, right] = pair();
	let asks = 0;
	const parent = new ParentChannel(left, async () => { asks++; return "no"; });
	right.write("x".repeat(65_537));
	await delay(5);
	assert.equal(left.destroyed, true);
	assert.equal(asks, 0);
	parent.close(); right.destroy();
});

test("real fd3 child calls the registered ask_parent tool; stdout result and usage remain intact", async () => {
	resetWorkerRuntime();
	let id = "";
	const result = await launch("ask", async (question) => {
		id = question.id;
		assert.equal(question.question, "选择中文方案 A 还是 B?");
		return "方案 B";
	});
	assert.equal(result.exitCode, 0, result.stderr);
	const parsed = JSON.parse(result.assistantText);
	assert.equal(parsed.result.details.questionId, id);
	assert.equal(parsed.result.content[0].text, "方案 B");
	assert.equal(result.usage.input, 20);
	assert.equal(result.usage.output, 10);
	assert.equal(result.actualModel, "local");
	assert.equal(activeSlots, 0);
	assert.equal(activeChildren.size, 0);
});

test("real fd3 child exit closes pending question, and task timeout releases process slots", async () => {
	let closed = false;
	const exiting = await launch("exit", (_q, signal) => new Promise((_resolve, reject) => {
		signal.addEventListener("abort", () => { closed = true; reject(new Error("closed")); });
	}));
	assert.equal(exiting.exitCode, 2);
	assert.equal(closed, true);
	const timedOut = await launch("hang", undefined, undefined, 120);
	assert.equal(timedOut.timedOut, true);
	assert.equal(activeSlots, 0);
	assert.equal(activeChildren.size, 0);
});

test("runtime + real child IPC returns questions while independent processes finish, then resumes by exact IDs", async () => {
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a.ts"), task("b.ts")], async (running) => {
		const result = await launch(running.index === 0 ? "ask" : "done", (q, signal) => runtime.ask(running, q, signal), running.controller.signal);
		return { ...JSON.parse(result.assistantText), usage: result.usage };
	});
	await runtime.wait(batch.id);
	assert.equal(batch.questions.length, 1);
	await until(() => batch.tasks[1].state === "finished");
	assert.equal(batch.tasks[0].state, "running");
	const question = batch.questions[0];
	runtime.reply(batch.id, question.taskId, question.id, "方案 A");
	await runtime.wait(batch.id);
	assert.equal(batch.finished, true);
	assert.equal(batch.tasks[0].result!.result.content[0].text, "方案 A");
	assert.equal(batch.tasks[0].result!.usage.input, 20);
	await runtime.dispose();
	assert.equal(activeSlots, 0);
	assert.equal(activeChildren.size, 0);
});

test("child-side answer timeout is retained as expired runtime history", async () => {
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("timeout.ts")], async (running) => {
		const result = await launch("question-timeout", (q, signal) => runtime.ask(running, q, signal), running.controller.signal);
		return JSON.parse(result.assistantText);
	});
	await until(() => batch.finished);
	assert.equal(batch.questions[0].status, "expired");
	assert.throws(() => runtime.reply(batch.id, batch.tasks[0].id, batch.questions[0].id, "late"), /过期/);
	await runtime.dispose();
});

test("spawn failure cleans IPC and releases its slot", async () => {
	const result = await runPiWorker({ mode: "scout", objective: "fixture" }, route, process.cwd(), "fixture", "fixture", undefined, 1_000, 1, undefined,
		{ invocation: { command: "/nonexistent/pi-worker-fixture", args: [] } });
	assert.equal(result.exitCode, 1);
	assert.match(result.errorMessage!, /ENOENT/);
	assert.equal(activeChildren.size, 0);
	assert.equal(activeSlots, 0);
});

test("slot waiters cancel/timeout without leaking capacity", async () => {
	const release = await acquireSlot(1, undefined, 1_000);
	const controller = new AbortController();
	const cancelled = acquireSlot(1, controller.signal, 1_000);
	controller.abort();
	await assert.rejects(cancelled, /取消/);
	await Promise.all([assert.rejects(acquireSlot(1, undefined, 20), /超时/), delay(30)]);
	assert.equal(slotWaiters.length, 0);
	assert.equal(activeSlots, 1);
	release();
	assert.equal(activeSlots, 0);
});

test("an old shutdown escalation cannot kill a replacement session's children", async () => {
	const oldWork = launch("hang", undefined, undefined, 5_000);
	await delay(150);
	beginWorkerShutdown();
	await oldWork;
	resetWorkerRuntime();
	const controller = new AbortController();
	const newWork = launch("hang", undefined, controller.signal, 5_000);
	await delay(3_150);
	assert.equal(activeChildren.size, 1, "old escalation timer must only target its original children");
	controller.abort();
	assert.equal((await newWork).aborted, true);
	assert.equal(activeSlots, 0);
});

test("process cancellation while asking cleans IPC and shutdown kills a SIGTERM-resistant fixture", async () => {
	const controller = new AbortController();
	let asked!: () => void;
	const ready = new Promise<void>((resolve) => { asked = resolve; });
	let cancelledQuestion = false;
	const work = launch("ask", (_q, signal) => new Promise((_resolve, reject) => {
		signal.addEventListener("abort", () => { cancelledQuestion = true; reject(new Error("closed")); });
		asked();
	}), controller.signal);
	await ready;
	controller.abort();
	assert.equal((await work).aborted, true);
	assert.equal(cancelledQuestion, true);
	const stubborn = launch("stubborn", undefined, undefined, 10_000);
	await delay(200);
	beginWorkerShutdown();
	const result = await stubborn;
	assert.notEqual(result.exitCode, 0);
	assert.equal(activeChildren.size, 0);
	assert.equal(activeSlots, 0);
	resetWorkerRuntime();
});

test("IPC pause/resume acknowledgements preserve remaining child budget across nested tokens; late replies cannot revive timeout", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const [left, right] = pair();
	let control!: QuestionControl; let answer!: (value: string) => void;
	const parent = new ParentChannel(left, (_q, _signal, handle) => new Promise((resolve) => { control = handle!; answer = resolve; }));
	const child = new ChildChannel(right);
	const result = child.ask("pause me", undefined, 100);
	const expired = assert.rejects(result, /超时/);
	await new Promise(setImmediate);
	try {
		t.mock.timers.tick(30);
		await control("pause", "outer", Date.now() + 10_000);
		await control("pause", "outer", Date.now() + 10_000);
		t.mock.timers.tick(1_000);
		await control("pause", "inner", Date.now() + 10_000);
		await control("resume", "outer", Date.now() + 10_000);
		t.mock.timers.tick(1_000);
		await control("resume", "inner", Date.now() + 10_000);
		t.mock.timers.tick(69);
		let settled = false; void result.catch(() => { settled = true; });
		await Promise.resolve(); assert.equal(settled, false);
		t.mock.timers.tick(1); await expired;
		await assert.rejects(control("pause", "late", Date.now() + 10_000), /过期/);
		answer("late answer"); await new Promise(setImmediate);
	} finally { child.close(); parent.close(); t.mock.timers.reset(); }
});

test("IPC human cap expires the child even if parent never resumes; missing or malformed acknowledgements fail closed", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const [left, right] = pair();
	let control!: QuestionControl;
	const parent = new ParentChannel(left, (_q, _signal, handle) => { control = handle!; return new Promise(() => {}); });
	const child = new ChildChannel(right);
	const expired = assert.rejects(child.ask("cap", undefined, 100), /上限/);
	await new Promise(setImmediate);
	await control("pause", "human", Date.now() + 500);
	t.mock.timers.tick(500); await expired;
	child.close(); parent.close();
	const [unacked, silent] = pair();
	let aborted = false;
	const noAck = new ParentChannel(unacked, (_q, signal, handle) => {
		control = handle!;
		signal.addEventListener("abort", () => { aborted = true; });
		return new Promise(() => {});
	});
	silent.write(JSON.stringify({ type: "question", id: "q", question: "Q", timeoutMs: 10_000 }) + "\n");
	await new Promise(setImmediate);
	const missing = assert.rejects(control("pause", "token", Date.now() + 500), /确认超时/);
	t.mock.timers.tick(3_000); await missing;
	assert.equal(aborted, true);
	await assert.rejects(control("resume", "token", Date.now() + 500), /关闭/);
	noAck.close(); silent.destroy();
	const [raw, client] = pair(); const malformed = new ChildChannel(client);
	const rejected = assert.rejects(malformed.ask("bad frame"), /关闭/);
	raw.write(JSON.stringify({ type: "pause", id: "q", token: "invalid/token", requestId: "r", deadline: Date.now() + 50 }) + "\n");
	await rejected; malformed.close(); raw.destroy(); t.mock.timers.reset();
});

test("real managed process + runtime + parent and child deadlines all survive human wait beyond ordinary budgets, then cancel cleanly", async () => {
	resetWorkerRuntime();
	const runtime = new WorkerRuntime(() => false);
	const execute = async (running: Parameters<Parameters<typeof start>[2]>[0]) => {
		const result = await launch("human-wait", (q, signal, control) => runtime.ask(running, q, signal, control), running.controller.signal, 50, true);
		return { ...JSON.parse(result.assistantText || "{}"), process: { aborted: result.aborted, timedOut: result.timedOut } };
	};
	try {
		const batch = start(runtime, [task("human.ts")], execute, 1, 2_000);
		await runtime.wait(batch.id);
		await runtime.beginInteraction("real-human", Date.now() + 10_000);
		await delay(2_100);
		assert.equal(batch.tasks[0].state, "running");
		assert.equal(batch.questions[0].status, "waiting");
		assert.equal(activeSlots, 1);
		await runtime.endInteraction("real-human");
		runtime.reply(batch.id, batch.tasks[0].id, batch.questions[0].id, "real parent decision");
		await runtime.wait(batch.id);
		assert.equal(batch.tasks[0].result!.result.content[0].text, "real parent decision");
		assert.deepEqual(batch.tasks[0].result!.process, { aborted: false, timedOut: false });
		const cancelled = start(runtime, [task("cancel-human.ts")], execute, 1, 2_000);
		await runtime.wait(cancelled.id);
		await runtime.beginInteraction("cancel-human", Date.now() + 10_000);
		runtime.cancel(cancelled.id);
		await runtime.wait(cancelled.id);
		await runtime.endInteraction("cancel-human");
		assert.equal(cancelled.questions[0].status, "cancelled");
		assert.throws(() => runtime.reply(cancelled.id, cancelled.tasks[0].id, cancelled.questions[0].id, "late"), /取消|过期/);
	} finally { await runtime.dispose(); }
	assert.equal(activeChildren.size, 0); assert.equal(activeSlots, 0);
});
