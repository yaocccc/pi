import assert from "node:assert/strict";
import test from "node:test";
import { WorkerRuntime } from "./runtime.ts";
import { start, task } from "./fixtures/helpers.ts";

const complete = { status: "completed" };
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

test("ordinary question expiry retains locks through cleanup while independent queue entries continue", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime((a, b) => a.task.objective === b.task.objective);
	let clean!: () => void;
	const cleanup = new Promise<void>((resolve) => { clean = resolve; });
	const batch = start(runtime, [task("a"), task("b"), task("a"), task("c")], async (running) => {
		if (running.index === 0) {
			await assert.rejects(runtime.ask(running, { id: "q", question: "Q", timeoutMs: 100 }, running.controller.signal), /超时/);
			await cleanup;
		}
		return complete;
	}, 2, 200);
	try {
		await runtime.wait(batch.id); await flush();
		assert.equal(batch.tasks[1].state, "finished");
		assert.equal(batch.tasks[3].state, "finished");
		assert.equal(batch.tasks[2].state, "queued");
		t.mock.timers.tick(100); await flush();
		assert.equal(batch.questions[0].status, "expired");
		assert.throws(() => runtime.reply(batch.id, batch.tasks[0].id, "q", "late"), /过期/);
		assert.equal(batch.tasks[2].state, "queued", "expiry alone cannot release the owner's path lock");
		clean(); await runtime.wait(batch.id);
		assert.ok(batch.tasks.every((running) => running.state === "finished"));
		assert.deepEqual(batch.tasks[0].task.allowedPaths, ["a"]);
	} finally { clean(); await runtime.dispose(); }
});

test("answering a question does not restart the task deadline", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a")], async (running) => {
		await runtime.ask(running, { id: "q", question: "Q", timeoutMs: 100 }, running.controller.signal);
		await new Promise<void>((resolve) => running.controller.signal.addEventListener("abort", () => resolve(), { once: true }));
		return complete;
	}, 1, 200);
	try {
		await runtime.wait(batch.id); t.mock.timers.tick(40);
		runtime.reply(batch.id, batch.tasks[0].id, "q", "yes"); await flush();
		assert.equal(batch.tasks[0].deadline, 1_200);
		t.mock.timers.tick(159); assert.equal(batch.tasks[0].controller.signal.aborted, false);
		t.mock.timers.tick(1); await flush();
		assert.equal(batch.tasks[0].result?.failure.category, "timeout");
		assert.equal(batch.questions[0].status, "answered");
	} finally { await runtime.dispose(); }
});

test("cancellation clears the task timer without releasing locks before delayed cleanup", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime(() => true);
	let clean!: () => void;
	const cleanup = new Promise<void>((resolve) => { clean = resolve; });
	const batch = start(runtime, [task("a"), task("a")], async (running) => {
		if (running.index === 0) {
			await assert.rejects(runtime.ask(running, { id: "q", question: "Q", timeoutMs: 100 }, running.controller.signal));
			await cleanup;
		}
		return complete;
	}, 1, 200);
	try {
		await runtime.wait(batch.id);
		runtime.cancel(batch.id);
		let collected = false;
		const waiting = runtime.wait(batch.id).then(() => { collected = true; });
		t.mock.timers.tick(1_000); await flush();
		assert.equal(batch.tasks[0].state, "running");
		assert.equal(batch.tasks[0].timedOut, undefined);
		assert.equal(batch.tasks[1].state, "finished");
		assert.equal(collected, false);
		clean(); await waiting;
		assert.equal(batch.tasks[0].result?.failure.category, "cancelled");
	} finally { clean(); await runtime.dispose(); }
});

test("an expired task cannot create a new question before its timer runs", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a")], async (running) => {
		t.mock.timers.setTime(1_100);
		await assert.rejects(runtime.ask(running, { id: "late", question: "Q", timeoutMs: 100 }, running.controller.signal), /过期/);
		return complete;
	}, 1, 100);
	await runtime.wait(batch.id);
	assert.equal(batch.questions.length, 0);
	assert.equal(batch.tasks[0].result?.failure.category, "timeout");
	await runtime.dispose();
});
