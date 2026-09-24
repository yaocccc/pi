import assert from "node:assert/strict";
import test from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { WorkerRuntime, type RuntimeTask } from "./runtime.ts";
import { workerTaskScopesConflict } from "./security.ts";
import { start, task, until } from "./fixtures/helpers.ts";

const conflicts = (a: RuntimeTask, b: RuntimeTask) => workerTaskScopesConflict(a.task, a.cwd, b.task, b.cwd);
const completed = { status: "completed", summary: ["done"] };

test("a question returns wait promptly, independent tasks continue, and cross-batch locks persist until finish", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const starts: string[] = [];
	let active = 0;
	let maximum = 0;
	let continueAfterAnswer!: () => void;
	const hold = new Promise<void>((resolve) => { continueAfterAnswer = resolve; });
	const run = async (t: RuntimeTask) => {
		starts.push(t.task.objective);
		maximum = Math.max(maximum, ++active);
		if (t.task.objective === "a.ts") {
			assert.equal(await runtime.ask(t, { id: "q-one", question: "选哪个？", timeoutMs: 1_000 }, t.controller.signal), "选择 A");
			await hold;
		} else await delay(10);
		active--;
		return completed;
	};
	const first = start(runtime, [task("a.ts"), task("b.ts")], run);
	await runtime.wait(first.id);
	assert.equal(first.finished, false);
	assert.deepEqual(starts, ["a.ts", "b.ts"]);
	await until(() => first.tasks[1].state === "finished");
	const second = start(runtime, [{ ...task("a.ts"), objective: "conflicting" }, task("c.ts")], run);
	await until(() => second.tasks[1].state === "finished");
	assert.equal(second.tasks[0].state, "queued");
	assert.ok(first.tasks.every((t) => t.overlappedWriter));
	runtime.reply(first.id, first.tasks[0].id, "q-one", "选择 A");
	await delay(10);
	assert.equal(second.tasks[0].state, "queued", "answering does not release the path lock");
	continueAfterAnswer();
	await runtime.wait(first.id);
	await runtime.wait(second.id);
	assert.equal(maximum, 2);
	assert.equal(first.questions[0].status, "answered");
	assert.equal(second.finished, true);
	assert.deepEqual(first.tasks.map((t) => t.result?.status), ["completed", "completed"]);
	await runtime.dispose();
});

test("foreign, duplicate and expired answers are rejected; timeout history is retained", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const batch = start(runtime, [task("a.ts")], async (t) => {
		await assert.rejects(runtime.ask(t, { id: "expires", question: "等待", timeoutMs: 30 }, t.controller.signal), /超时/);
		await runtime.ask(t, { id: "answer", question: "继续?", timeoutMs: 500 }, t.controller.signal);
		await assert.rejects(runtime.ask(t, { id: "answer", question: "再次?", timeoutMs: 500 }, t.controller.signal), /重复/);
		return completed;
	});
	await runtime.wait(batch.id);
	assert.throws(() => runtime.reply(batch.id, "other-task", "expires", "ok"), /未知/);
	await until(() => batch.questions.length === 2);
	assert.equal(batch.questions[0].status, "expired");
	assert.throws(() => runtime.reply(batch.id, batch.tasks[0].id, "expires", "late"), /过期/);
	runtime.reply(batch.id, batch.tasks[0].id, "answer", "ok");
	assert.throws(() => runtime.reply(batch.id, batch.tasks[0].id, "answer", "duplicate"), /重复/);
	await runtime.wait(batch.id);
	assert.equal(batch.finished, true);
	await runtime.dispose();
});

test("expired answer in a bulk submission cannot partially apply a still-valid answer", async () => {
	const runtime = new WorkerRuntime(conflicts);
	let answers = 0;
	const batch = start(runtime, [task("a.ts"), task("b.ts")], async (running) => {
		await runtime.ask(running, { id: `q-${running.index}`, question: "Q", timeoutMs: 1_000 }, running.controller.signal);
		answers++;
		return completed;
	});
	try {
		await runtime.wait(batch.id);
		// Simulate the deadline race before the timeout callback runs.
		batch.questions[1].expiresAt = Date.now() - 1;
		assert.throws(() => runtime.replyMany(batch.id, batch.questions.map((q) => ({ taskId: q.taskId, questionId: q.id, answer: "yes" }))), /过期/);
		assert.equal(batch.questions[0].status, "waiting");
		assert.equal(batch.questions[0].answer, undefined);
		await Promise.resolve();
		assert.equal(answers, 0);
	} finally { await runtime.dispose(); }
});

test("bulk replies reject an expired task deadline atomically before its timer callback runs", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const runtime = new WorkerRuntime(conflicts);
	let received = 0;
	const batch = start(runtime, [task("a.ts"), task("b.ts")], async (running) => {
		await runtime.ask(running, { id: `deadline-${running.index}`, question: "Q", timeoutMs: 1_000 }, running.controller.signal);
		received++; return completed;
	}, 2, 100);
	try {
		await runtime.wait(batch.id);
		// Keep the first submitted answer valid to catch partial-application bugs.
		batch.tasks[0].deadline = 1_200;
		t.mock.timers.setTime(1_100);
		assert.equal(batch.tasks[1].controller.signal.aborted, false, "timer has not fired");
		assert.ok(batch.questions.every((q) => q.expiresAt > Date.now()));
		assert.throws(() => runtime.replyMany(batch.id, batch.questions.map((q) => ({ taskId: q.taskId, questionId: q.id, answer: "late approval" }))), /总期限已过期/);
		await Promise.resolve(); assert.equal(received, 0);
		assert.ok(batch.questions.every((q) => q.status === "waiting" && q.answer === undefined));
	} finally { await runtime.dispose(); }
});

test("late successful runner result cannot beat a task deadline when timers have not fired", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const runtime = new WorkerRuntime(conflicts);
	let release!: () => void;
	const gate = new Promise<void>((resolve) => { release = resolve; });
	const batch = start(runtime, [task("a.ts")], async () => { await gate; return completed; }, 1, 100);
	t.mock.timers.setTime(1_100);
	assert.equal(batch.tasks[0].controller.signal.aborted, false);
	release(); await runtime.wait(batch.id);
	assert.equal(batch.tasks[0].result?.status, "failed");
	assert.equal(batch.tasks[0].result?.failure.category, "timeout");
	assert.equal(batch.tasks[0].result?.execution.timed_out, true);
	await runtime.dispose();
});

test("bulk replies validate task state and controller independently of the question signal", async () => {
	for (const invalid of ["aborted", "finished"] as const) {
		const runtime = new WorkerRuntime(conflicts);
		const batch = start(runtime, [task("a.ts"), task("b.ts")], async (running) => {
			await runtime.ask(running, { id: `state-${running.index}`, question: "Q", timeoutMs: 1_000 }, new AbortController().signal);
			return completed;
		});
		try {
			await runtime.wait(batch.id);
			if (invalid === "aborted") batch.tasks[1].controller.abort();
			else batch.tasks[1].state = "finished";
			assert.throws(() => runtime.replyMany(batch.id, batch.questions.map((q) => ({ taskId: q.taskId, questionId: q.id, answer: "no" }))), /已结束或取消/);
			assert.ok(batch.questions.every((q) => q.status === "waiting" && q.answer === undefined));
		} finally { batch.tasks[1].state = "running"; await runtime.dispose(); }
	}
});

test("cancellation rejects questions and queued tasks but retains slots/locks through cleanup", async () => {
	const runtime = new WorkerRuntime(conflicts);
	let clean!: () => void;
	const cleaned = new Promise<void>((resolve) => { clean = resolve; });
	let starts = 0;
	const first = start(runtime, [task("a.ts"), task("a.ts")], async (t) => {
		starts++;
		await assert.rejects(runtime.ask(t, { id: "cancel-me", question: "Q", timeoutMs: 1_000 }, t.controller.signal));
		await cleaned;
		return completed;
	}, 1);
	await runtime.wait(first.id);
	const second = start(runtime, [task("c.ts")], async () => { starts++; return completed; }, 1);
	runtime.cancel(first.id);
	assert.equal(first.questions[0].status, "cancelled");
	assert.equal(first.tasks[1].state, "finished");
	await delay(10);
	assert.equal(starts, 1);
	assert.equal(second.tasks[0].state, "queued");
	assert.throws(() => runtime.reply(first.id, first.tasks[0].id, "cancel-me", "late"), /过期/);
	clean();
	await runtime.wait(first.id);
	await runtime.wait(second.id);
	assert.equal(first.tasks[0].result?.status, "failed");
	assert.equal(starts, 2);
	await runtime.dispose();
});

test("task timeout aborts ask and retains timeout classification", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const first = start(runtime, [task("a.ts")], async (t) => {
		await delay(15);
		await runtime.ask(t, { id: "timeout", question: "Q", timeoutMs: 1_000 }, t.controller.signal);
		return completed;
	}, 1, 60);
	await runtime.wait(first.id);
	assert.equal(first.finished, false);
	await until(() => first.finished);
	assert.equal(first.questions[0].status, "cancelled");
	assert.match(first.tasks[0].result!.summary[0], /超时/);
	assert.equal(first.tasks[0].result!.execution.timed_out, true);
	assert.equal(first.tasks[0].result!.failure.category, "timeout");
	await runtime.dispose();
});

test("dispose is idempotent, clears active/queued work and wakes waits; closed runtimes reject admission", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const batch = start(runtime, [task("a.ts"), task("a.ts")], async (t) => {
		await runtime.ask(t, { id: "shutdown", question: "Q", timeoutMs: 10_000 }, t.controller.signal);
		return completed;
	}, 1);
	await runtime.wait(batch.id);
	await Promise.all([runtime.dispose(), runtime.dispose()]);
	assert.equal(batch.finished, true);
	assert.equal(batch.questions[0].status, "cancelled");
	assert.ok(batch.tasks.every((t) => t.controller.signal.aborted));
	assert.throws(() => start(runtime, [task("b.ts")], async () => completed), /关闭/);
});

test("lower limits across batches never start extra work and cross-cwd locks resolve to the same path", async () => {
	const runtime = new WorkerRuntime(conflicts);
	let release!: () => void;
	const hold = new Promise<void>((resolve) => { release = resolve; });
	const first = start(runtime, [task("a.ts"), task("b.ts")], async () => { await hold; return completed; }, 2);
	const second = start(runtime, [task("c.ts")], async () => completed, 1);
	await delay(5);
	assert.ok(first.tasks.every((t) => t.state === "running"));
	assert.equal(second.tasks[0].state, "queued");
	release();
	await runtime.wait(first.id);
	await runtime.wait(second.id);
	assert.equal(second.finished, true);
	assert.equal(workerTaskScopesConflict(task("extensions/worker/**"), process.cwd(), task("worker/**"), `${process.cwd()}/extensions`), true);
	await runtime.dispose();
});

test("a failing task is isolated and wait collects the whole batch", async () => {
	const runtime = new WorkerRuntime(conflicts, () => { throw new Error("disposed UI"); });
	const batch = start(runtime, [task("a.ts"), task("b.ts")], async (t) => {
		if (t.index === 0) throw new Error("failure");
		await delay(40);
		return completed;
	});
	await runtime.wait(batch.id);
	assert.equal(batch.finished, true);
	assert.deepEqual(batch.tasks.map((t) => t.result?.status), ["failed", "completed"]);
	await runtime.dispose();
});
