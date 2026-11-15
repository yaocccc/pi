import assert from "node:assert/strict";
import test from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { WorkerRuntime, type RuntimeTask } from "./runtime.ts";
import { workerTaskScopesConflict } from "./security.ts";
import { start, task, until } from "./fixtures/helpers.ts";

const conflicts = (a: RuntimeTask, b: RuntimeTask) => workerTaskScopesConflict(a.task, a.cwd, b.task, b.cwd);
const completed = { status: "completed", summary: ["done"] };
function gate() {
	let resolve!: () => void;
	const promise = new Promise<void>((done) => { resolve = done; });
	return { promise, resolve };
}

test("independent tasks bypass blocked queue entries; conflicts retain ordering and locks across batches", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const first = gate();
	const batch = start(runtime, [task("a.ts"), task("a.ts"), task("b.ts")], async (running) => {
		if (running.index === 0) await first.promise;
		return completed;
	});
	const next = start(runtime, [task("a.ts")], async () => completed);
	try {
		await until(() => batch.tasks[2].state === "finished");
		assert.equal(batch.tasks[1].state, "queued");
		assert.equal(next.tasks[0].state, "queued");
		let returned = false;
		const waiting = runtime.wait(batch.id).then(() => { returned = true; });
		await delay(10); assert.equal(returned, false);
		first.resolve(); await waiting; await runtime.wait(next.id);
		assert.ok(batch.tasks.every((item) => item.state === "finished"));
		assert.ok(batch.tasks[0].overlappedWriter && batch.tasks[2].overlappedWriter);
	} finally { first.resolve(); await runtime.dispose(); }
});

test("queued conflicts cannot be overtaken through an overlapping bridge scope", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const first = gate();
	const starts: number[] = [];
	const batch = start(runtime, [task("x.ts"), { ...task("x.ts"), allowedPaths: ["x.ts", "a.ts"] }, task("a.ts")], async (running) => {
		starts.push(running.index);
		if (running.index === 0) await first.promise;
		return { status: "completed", summary: [String(running.index)] };
	});
	try {
		await until(() => starts.length > 0);
		assert.deepEqual(batch.tasks.map((item) => item.state), ["running", "queued", "queued"]);
		first.resolve();
		await runtime.wait(batch.id);
		assert.deepEqual(starts, [0, 1, 2]);
		assert.deepEqual(batch.tasks.map((item) => item.result?.summary), [["0"], ["1"], ["2"]]);
	} finally { first.resolve(); await runtime.dispose(); }
});

test("later-started writers mark overlap on both active tasks, but readers do not", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const first = gate(), later = gate();
	const batch = start(runtime, [task("x.ts"), task("a.ts"), task("a.ts")], async (running) => {
		if (running.index === 0) await first.promise;
		if (running.index === 2) await later.promise;
		return completed;
	});
	try {
		await until(() => batch.tasks[2].state === "running");
		assert.ok(batch.tasks.every((item) => item.overlappedWriter));
		first.resolve(); later.resolve(); await runtime.wait(batch.id);
		const readers = start(runtime, [{ mode: "review", preset: "normal", sessionMode: "fork", objective: "read" }, { mode: "scout", preset: "normal", sessionMode: "fork", objective: "read" }], async () => completed);
		await runtime.wait(readers.id);
		assert.ok(readers.tasks.every((item) => !item.overlappedWriter));
	} finally { first.resolve(); later.resolve(); await runtime.dispose(); }
});

test("blocking and failed tasks finish directly, release locks and preserve sibling results", async () => {
	const runtime = new WorkerRuntime(conflicts);
	const batch = start(runtime, [task("a.ts"), task("a.ts"), task("b.ts")], async (running) => {
		if (running.index === 0) return { status: "blocked", summary: ["Missing authorization"] };
		if (running.index === 2) throw new Error("fixture failure");
		return completed;
	});
	await runtime.wait(batch.id);
	assert.equal(batch.finished, true);
	assert.deepEqual(batch.tasks.map((item) => item.result?.status), ["blocked", "completed", "failed"]);
	assert.deepEqual(batch.ui.tasks.map((item) => item.status), ["blocked", "completed", "failed"]);
	assert.equal(batch.ui.completed, 3);
	await runtime.dispose();
});

test("concurrency cap and writer overlap are based on actual execution", async () => {
	const runtime = new WorkerRuntime(conflicts);
	let running = 0, maximum = 0;
	const batch = start(runtime, Array.from({ length: 5 }, (_, i) => task(`${i}.ts`)), async () => {
		maximum = Math.max(maximum, ++running); await delay(10); running--; return completed;
	}, 2);
	await runtime.wait(batch.id);
	assert.equal(maximum, 2);
	const serial = start(runtime, [task("a.ts"), task("b.ts")], async () => completed, 1);
	await runtime.wait(serial.id);
	assert.ok(serial.tasks.every((item) => !item.overlappedWriter));
	await runtime.dispose();
});

test("invocation abort waits for cleanup, cancellation wins over late success, and old signals detach", async () => {
	const runtime = new WorkerRuntime(conflicts);
	assert.equal(runtime.busy, false);
	const cleanup = gate();
	const batch = start(runtime, [task("a.ts"), task("a.ts")], async (running) => {
		await new Promise<void>((resolve) => running.controller.signal.addEventListener("abort", () => resolve(), { once: true }));
		await cleanup.promise; return completed;
	}, 1);
	await until(() => batch.tasks[0].state === "running");
	await delay(0);
	const controller = new AbortController();
	let returned = false;
	const waiting = runtime.wait(batch.id, controller.signal).then(() => { returned = true; });
	controller.abort();
	await delay(10);
	assert.equal(returned, false);
	assert.equal(batch.tasks[0].state, "running");
	assert.equal(batch.tasks[1].state, "finished");
	assert.equal(runtime.busy, true, "abort does not release the session while cleanup still owns its paths");
	cleanup.resolve(); await waiting;
	assert.equal(runtime.busy, false);
	assert.equal(batch.tasks[0].result?.failure.category, "cancelled");
	assert.equal(batch.tasks[0].result?.execution.termination_source, "invocation_abort");
	runtime.cancel(batch.id);
	const next = start(runtime, [task("a.ts")], async () => completed);
	await runtime.wait(next.id);
	assert.equal(next.tasks[0].result?.status, "completed");
	await runtime.dispose();
});

test("disposing one runtime cannot cancel a different runtime; closed runtime rejects admission", async () => {
	const a = new WorkerRuntime(conflicts), b = new WorkerRuntime(conflicts);
	const finish = gate();
	const batch = start(b, [task("a.ts")], async () => { await finish.promise; return completed; });
	await a.dispose();
	assert.equal(batch.tasks[0].controller.signal.aborted, false);
	assert.throws(() => start(a, [task("a.ts")], async () => completed), /关闭/);
	finish.resolve(); await b.wait(batch.id); await b.dispose();
});

test("presentation callback errors do not strand tasks or turn one failure into sibling cancellation", async () => {
	const runtime = new WorkerRuntime(conflicts, () => { throw new Error("disposed UI"); });
	const batch = start(runtime, [task("a.ts"), task("b.ts")], async (running) => {
		if (running.index === 0) throw new Error("failure");
		await delay(10); return completed;
	});
	await runtime.wait(batch.id);
	assert.deepEqual(batch.tasks.map((item) => item.result?.status), ["failed", "completed"]);
	await runtime.dispose();
});

const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

test("fixed task timeout retains locks through cleanup while independent work completes", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime((a, b) => a.task.objective === b.task.objective);
	let clean!: () => void;
	const cleanup = new Promise<void>((resolve) => { clean = resolve; });
	const batch = start(runtime, [task("a"), task("a"), task("b")], async (running) => {
		if (running.index === 0) {
			await new Promise<void>((resolve) => running.controller.signal.addEventListener("abort", () => resolve(), { once: true }));
			await cleanup;
		}
		return completed;
	}, 2, 100);
	try {
		await flush();
		assert.equal(batch.tasks[2].state, "finished");
		t.mock.timers.tick(100); await flush();
		assert.equal(batch.tasks[0].timedOut, true);
		assert.equal(batch.tasks[0].state, "running");
		assert.equal(batch.tasks[1].state, "queued");
		clean(); await runtime.wait(batch.id);
		assert.equal(batch.tasks[0].result?.failure.category, "timeout");
		assert.equal(batch.tasks[1].result?.status, "completed");
		assert.equal(batch.tasks[1].deadline, 1_200);
	} finally { clean(); await runtime.dispose(); }
});

test("cancellation clears the task timer without releasing locks before delayed cleanup", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime(() => true);
	let clean!: () => void;
	const cleanup = new Promise<void>((resolve) => { clean = resolve; });
	const batch = start(runtime, [task("a"), task("a")], async (running) => {
		await new Promise<void>((resolve) => running.controller.signal.addEventListener("abort", () => resolve(), { once: true }));
		await cleanup; return completed;
	}, 1, 200);
	try {
		await flush(); runtime.cancel(batch.id);
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

test("overdue completion is timeout even before the timer callback runs", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a")], async () => {
		t.mock.timers.setTime(1_100); return completed;
	}, 1, 100);
	await runtime.wait(batch.id);
	assert.equal(batch.tasks[0].result?.failure.category, "timeout");
	await runtime.dispose();
});
