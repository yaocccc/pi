import assert from "node:assert/strict";
import test from "node:test";
import { WorkerRuntime } from "./runtime.ts";
import { start, task } from "./fixtures/helpers.ts";
import { HUMAN_WAIT_LIMIT_MS, withHumanInteraction, type InteractionEvent } from "../shared/interaction-lifecycle.ts";

const complete = { status: "completed" };
const flush = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };

test("human wait pauses remaining task/question budgets, new questions join, independent tasks and queue keep moving", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 10_000 });
	const runtime = new WorkerRuntime((a, b) => a.task.objective === b.task.objective);
	let askLater!: () => void;
	const gate = new Promise<void>((resolve) => { askLater = resolve; });
	let starts = 0;
	const batch = start(runtime, [task("a"), task("b"), task("c"), task("a"), task("d")], async (running) => {
		starts++;
		if (running.index === 1) { await gate; return complete; }
		if (running.index === 2) await gate;
		if (running.index === 0 || running.index === 2) await runtime.ask(running, { id: `q-${running.index}`, question: "Q", timeoutMs: 100 }, running.controller.signal);
		return complete;
	}, 3, 200);
	try {
		await runtime.wait(batch.id);
		t.mock.timers.tick(40);
		await runtime.beginInteraction("human-1", Date.now() + 10_000);
		await runtime.beginInteraction("human-1", Date.now() + 10_000); // Idempotent, no extra pause.
		askLater(); await flush();
		assert.equal(batch.tasks[1].state, "finished");
		assert.equal(batch.tasks[4].state, "finished", "independent queued work is admitted during human wait");
		assert.equal(batch.questions.length, 2);
		assert.ok(batch.questions.every((q) => q.pausedUntil));
		assert.equal(batch.tasks[3].state, "queued", "waiting owner keeps its path lock even with a free slot");
		t.mock.timers.tick(1_000); await flush();
		assert.equal(batch.questions[0].status, "waiting");
		assert.equal(batch.tasks[0].controller.signal.aborted, false);
		await runtime.beginInteraction("human-2", Date.now() + 10_000);
		await runtime.endInteraction("human-1");
		t.mock.timers.tick(1_000); await flush();
		assert.ok(batch.questions.every((q) => q.status === "waiting"));
		await runtime.endInteraction("human-2");
		await runtime.endInteraction("human-2");
		assert.equal(batch.questions[0].expiresAt - Date.now(), 60, "question resumes remaining, not original budget");
		assert.equal(batch.tasks[0].budget!.expiresAt - Date.now(), 160, "task budget resumes remaining active time");
		t.mock.timers.tick(50);
		runtime.replyMany(batch.id, batch.questions.map((q) => ({ taskId: q.taskId, questionId: q.id, answer: "approved within original paths" })));
		await runtime.wait(batch.id);
		assert.equal(starts, 5);
		assert.ok(batch.tasks.every((running) => running.result?.status === "completed"));
		assert.deepEqual(batch.tasks[0].task.allowedPaths, ["a"], "answers never change permissions");
	} finally { await runtime.dispose(); t.mock.timers.reset(); }
});

test("remaining task timeout resumes after answer; human cap cancels and cannot be renewed or revived", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a")], async (running) => {
		await runtime.ask(running, { id: "q", question: "Q", timeoutMs: 100 }, running.controller.signal);
		await new Promise<void>((resolve) => running.controller.signal.addEventListener("abort", () => resolve(), { once: true }));
		return complete;
	}, 1, 200);
	try {
		await runtime.wait(batch.id); t.mock.timers.tick(40);
		await runtime.beginInteraction("pause", Date.now() + 10_000);
		t.mock.timers.tick(1_000);
		await runtime.endInteraction("pause");
		runtime.reply(batch.id, batch.tasks[0].id, "q", "yes"); await flush();
		t.mock.timers.tick(159); assert.equal(batch.tasks[0].controller.signal.aborted, false);
		t.mock.timers.tick(1); await flush();
		assert.equal(batch.tasks[0].result?.failure.category, "timeout");
		const capped = start(runtime, [task("b")], async (running) => {
			await runtime.ask(running, { id: "cap", question: "Q", timeoutMs: 100 }, running.controller.signal);
			return complete;
		});
		await runtime.wait(capped.id);
		await runtime.beginInteraction("cap-one", Date.now() + HUMAN_WAIT_LIMIT_MS * 2);
		t.mock.timers.tick(HUMAN_WAIT_LIMIT_MS - 1);
		await runtime.beginInteraction("cap-two", Date.now() + HUMAN_WAIT_LIMIT_MS);
		t.mock.timers.tick(1); await flush();
		assert.equal(capped.finished, true);
		assert.equal(capped.questions[0].status, "cancelled");
		assert.match(capped.tasks[0].result!.summary[0], /人工确认等待已达上限/);
		await runtime.endInteraction("cap-two");
		assert.throws(() => runtime.reply(capped.id, capped.tasks[0].id, "cap", "late"), /取消|过期/);
	} finally { await runtime.dispose(); t.mock.timers.reset(); }
});

test("pause preparation is acknowledged before UI; cancellation during preparation cleans up without reviving worker", async () => {
	const runtime = new WorkerRuntime(() => false);
	let acknowledge!: () => void;
	const ack = new Promise<void>((resolve) => { acknowledge = resolve; });
	const controls: string[] = [];
	const batch = start(runtime, [task("a")], async (running) => {
		await runtime.ask(running, { id: "q", question: "Q", timeoutMs: 1_000 }, running.controller.signal, async (phase) => { controls.push(phase); if (phase === "pause") await ack; });
		return complete;
	});
	await runtime.wait(batch.id);
	let ui = false;
	const abort = new AbortController();
	const interaction = withHumanInteraction({ emit(_name, data) {
		const event = data as InteractionEvent;
		event.waitUntil(event.phase === "begin" ? runtime.beginInteraction(event.token, event.deadline) : runtime.endInteraction(event.token));
	} }, abort.signal, async () => { ui = true; return "yes"; });
	const rejected = assert.rejects(interaction);
	await flush(); assert.equal(ui, false); assert.deepEqual(controls, ["pause"]);
	runtime.cancel(batch.id); abort.abort(); acknowledge();
	await rejected; await runtime.wait(batch.id);
	assert.equal(ui, false); assert.equal(batch.questions[0].status, "cancelled");
	assert.deepEqual(controls, ["pause"], "cancelled questions must not receive resume");
	await runtime.dispose();
});

test("lifecycle cap cancels waiting task even when UI finally runs before runtime cap callback", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a")], async (running) => {
		await runtime.ask(running, { id: "q", question: "Q", timeoutMs: 100 }, running.controller.signal);
		return complete;
	});
	try {
		await runtime.wait(batch.id);
		let interactionToken = "";
		const interaction = withHumanInteraction({ emit(_name, value) {
			const event = value as InteractionEvent;
			interactionToken = event.token;
			event.waitUntil(event.phase === "begin" ? runtime.beginInteraction(event.token, event.deadline) : runtime.endInteraction(event.token));
		} }, undefined, async () => new Promise(() => {}), 500);
		const rejected = assert.rejects(interaction, /上限/);
		await flush();
		// Advance wall time before firing timers: simulate cleanup racing the cap callback.
		t.mock.timers.setTime(1_500);
		await runtime.endInteraction(interactionToken);
		assert.equal(batch.questions[0].status, "cancelled");
		t.mock.timers.tick(500); await rejected; await flush();
		assert.equal(batch.finished, true);
	} finally { await runtime.dispose(); t.mock.timers.reset(); }
});

test("begin preparation failure resumes unaffected tasks and rejects the UI, while late pause cannot revive expiry", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a"), task("b")], async (running) => {
		await runtime.ask(running, { id: `q-${running.index}`, question: "Q", timeoutMs: 100 }, running.controller.signal,
			async () => { if (running.index === 0) throw new Error("no ack"); });
		return complete;
	});
	try {
		await runtime.wait(batch.id);
		await assert.rejects(withHumanInteraction({ emit(_name, value) {
			const event = value as InteractionEvent;
			event.waitUntil(event.phase === "begin" ? runtime.beginInteraction(event.token, event.deadline) : runtime.endInteraction(event.token));
		} }, undefined, async () => { throw new Error("UI must not open"); }), /no ack/);
		assert.equal(batch.questions[0].status, "cancelled");
		assert.equal(batch.questions[1].pausedUntil, undefined);
		t.mock.timers.tick(100); await flush();
		await runtime.beginInteraction("late", Date.now() + 100);
		assert.equal(batch.questions[1].status, "expired");
		assert.throws(() => runtime.reply(batch.id, batch.tasks[1].id, "q-1", "late"), /过期/);
	} finally { await runtime.dispose(); t.mock.timers.reset(); }
});
