import assert from "node:assert/strict";
import test from "node:test";
import { Duplex } from "node:stream";
import { fileURLToPath } from "node:url";
import { ChildChannel, ParentChannel, type AskParent, type ChannelClose } from "./ipc.ts";
import { acquireSlot, beginWorkerShutdown, createWorkerOwner, runPiWorker, type WorkerOwner } from "./process.ts";
import { WorkerRuntime } from "./runtime.ts";
import { start, task, until } from "./fixtures/helpers.ts";
import type { ChildProgress, Route } from "./types.ts";

const route: Route = { requestedPreset: "fast", resolvedPreset: "fast", modelId: "fixture/local", provider: "fixture", thinking: "high", routeReason: [] };
const fixture = fileURLToPath(new URL("./fixtures/child.mjs", import.meta.url));
const launch = (owner: WorkerOwner, scenario: string, askParent?: AskParent, signal?: AbortSignal, timeout = 10_000, progress?: (progress: ChildProgress) => void) => runPiWorker(
	{ mode: "scout", objective: "fixture" }, route, process.cwd(), "fixture", "fixture", signal, timeout, 1, progress,
	{ owner, askParent, invocation: { command: process.execPath, args: ["--experimental-transform-types", fixture, scenario] } },
);
function pair(): [Duplex, Duplex] {
	let left: Duplex, right: Duplex;
	const build = (peer: () => Duplex) => new Duplex({ read() {}, write(chunk, _encoding, callback) { peer().push(chunk); callback(); } });
	left = build(() => right); right = build(() => left);
	return [left, right];
}

test("answer + EOF in the same reaction has no pending question; normal local close is idempotent", async () => {
	const [left, right] = pair();
	const closed: Array<ChannelClose & { pending: number }> = [];
	let answer!: (value: string) => void;
	const parent = new ParentChannel(left, () => new Promise((resolve) => { answer = resolve; }), (reason) => closed.push(reason));
	const child = new ChildChannel(right);
	try {
		const response = child.ask("fixture");
		await new Promise(setImmediate);
		answer("answer");
		queueMicrotask(() => left.emit("end"));
		assert.equal((await response).answer, "answer");
		assert.deepEqual(closed, [{ source: "eof", code: "EOF", pending: 0 }]);
		parent.close(); parent.close();
		assert.equal(closed.length, 1);
	} finally { child.close(); parent.close(); }
});

test("bounded shutdown frames distinguish cancellation from unknown parent loss without claiming authorization", async () => {
	const [left, right] = pair();
	let asked = false;
	const parent = new ParentChannel(left, (_q, signal) => new Promise((_resolve, reject) => { asked = true; signal.addEventListener("abort", () => reject(signal.reason)); }));
	const child = new ChildChannel(right);
	try {
		const response = assert.rejects(child.ask("fixture"), /reason=cancelled.*不得猜测授权/);
		await until(() => asked);
		parent.close("cancelled");
		await response;
	} finally { child.close(); parent.close(); }
});

for (const scenario of ["broken-eof", "broken-protocol"] as const) {
	test(`real fd3 ${scenario} fails even when the child catches IPC error and reports completed`, async () => {
		const owner = createWorkerOwner();
		let cancelled = false;
		const result = await launch(owner, scenario, (_q, signal) => new Promise((_resolve, reject) => {
			signal.addEventListener("abort", () => { cancelled = true; reject(signal.reason); });
		}));
		assert.equal(cancelled, true);
		assert.equal(JSON.parse(result.assistantText).status, "completed", "fixture intentionally swallows the rejected ask");
		assert.equal(result.exitCode, 1, "parent result wins over a successful child exit");
		assert.equal(result.aborted, false);
		assert.equal(result.timedOut, false);
		assert.equal(result.termination?.source, scenario === "broken-eof" ? "eof" : "protocol");
		assert.match(result.errorMessage!, /stage=ipc.*code=.*source=/);
		assert.doesNotMatch(result.errorMessage!, /Do not guess authorization/, "diagnostics contain no question payload");
		assert.equal(owner.children.size, 0); assert.equal(owner.slots, 0);
		assert.equal((await launch(owner, "done")).exitCode, 0, "normal no-pending EOF is not a failure");
	});
}

for (const first of ["abort", "error"] as const) {
	test(`same-turn ${first} then the other termination settles once and retains the first source`, async () => {
		const owner = createWorkerOwner();
		const controller = new AbortController();
		let ready!: () => void;
		const asked = new Promise<void>((resolve) => { ready = resolve; });
		const work = launch(owner, "catch", (_q, signal) => new Promise((_resolve, reject) => { signal.addEventListener("abort", () => reject(signal.reason)); ready(); }), controller.signal);
		await asked;
		const socket = [...owner.children][0].stdio[3]!;
		const abort = () => controller.abort(Object.assign(new Error("fixture cancellation"), { source: "invocation_abort" }));
		const error = () => socket.emit("error", Object.assign(new Error("secret payload not for diagnostics"), { code: "ECONNRESET" }));
		if (first === "abort") { abort(); error(); } else { error(); abort(); }
		const result = await work;
		assert.equal(result.termination?.source, first === "abort" ? "invocation_abort" : "transport");
		assert.equal(result.aborted, first === "abort");
		assert.doesNotMatch(result.errorMessage!, /secret payload/);
		assert.equal(owner.children.size, 0); assert.equal(owner.slots, 0);
	});
}

test("IPC failure releases runtime path locks only after the worker is cleaned; question timeout does not cancel the task", async () => {
	const owner = createWorkerOwner();
	const runtime = new WorkerRuntime(() => true);
	try {
		const batch = start(runtime, [task("same.ts"), task("same.ts")], async (running) => {
			if (running.index === 1) assert.equal(owner.children.size, 0);
			const result = await launch(owner, running.index === 0 ? "broken-eof" : "done", (q, signal) => runtime.ask(running, q, signal), running.controller.signal);
			return { status: result.errorMessage ? "failed" : "completed", execution: { termination: result.termination } };
		}, 2, 10_000);
		await until(() => batch.finished, 5_000);
		assert.equal(batch.tasks[0].result!.status, "failed");
		assert.equal(batch.tasks[1].result!.status, "completed");
		assert.equal(batch.questions[0].status, "cancelled");
		const timedQuestion = await launch(owner, "question-timeout", (_q, signal) => new Promise((_resolve, reject) => signal.addEventListener("abort", () => reject(signal.reason))));
		assert.equal(timedQuestion.exitCode, 0);
		assert.equal(timedQuestion.termination, undefined);
		assert.equal(timedQuestion.aborted, false); assert.equal(timedQuestion.timedOut, false);
	} finally { await runtime.dispose(); beginWorkerShutdown(owner); }
});

test("owner shutdown rejects only its own waiters and slot releases are idempotent", async () => {
	const a = createWorkerOwner(), b = createWorkerOwner();
	const releaseA = await acquireSlot(1, undefined, 1_000, a);
	const releaseB = await acquireSlot(1, undefined, 1_000, b);
	const waitA = assert.rejects(acquireSlot(1, undefined, 1_000, a), /取消/);
	beginWorkerShutdown(a); beginWorkerShutdown(a);
	await waitA;
	assert.equal(a.waiters.length, 0); assert.equal(b.closed, false); assert.equal(b.slots, 1);
	releaseA(); releaseA(); releaseB(); releaseB();
	assert.equal(a.slots, 0); assert.equal(b.slots, 0);
});

test("standalone timeout and pre-spawn task timeout retain explicit sources without claiming parent death", async () => {
	const owner = createWorkerOwner();
	const timeout = await launch(owner, "hang", undefined, undefined, 200);
	assert.equal(timeout.timedOut, true); assert.equal(timeout.aborted, false);
	assert.equal(timeout.termination?.source, "timeout");
	const controller = new AbortController();
	controller.abort(Object.assign(new Error("Worker 任务超时"), { source: "task_timeout" }));
	const early = await launch(owner, "done", undefined, controller.signal);
	assert.equal(early.timedOut, true); assert.equal(early.aborted, false);
	assert.equal(early.termination?.source, "task_timeout");
	assert.equal(owner.children.size, 0); assert.equal(owner.slots, 0);
});

test("transport error without a pending question is still failure, unlike ordinary no-pending EOF", async () => {
	const owner = createWorkerOwner();
	const work = launch(owner, "hang");
	await until(() => owner.children.size === 1);
	[...owner.children][0].stdio[3]!.destroy(Object.assign(new Error("fixture"), { code: "ECONNRESET" }));
	const result = await work;
	assert.equal(result.termination?.source, "transport");
	assert.match(result.errorMessage!, /pending=0/);
	assert.notEqual(result.exitCode, 0);
	assert.equal(owner.children.size, 0); assert.equal(owner.slots, 0);
});

test("raw onProgress throws throughout cancellation cannot suppress SIGKILL escalation or settlement", { timeout: 8_000 }, async () => {
	const owner = createWorkerOwner();
	const controller = new AbortController();
	const phases: string[] = [];
	let ready = false;
	const work = launch(owner, "stubborn", undefined, controller.signal, 10_000, (progress) => {
		ready ||= progress.activities.some((activity) => activity.id === "tool:fixture-ready");
		phases.push(progress.phase);
		throw new Error("fixture raw progress failure");
	});
	try {
		await until(() => ready);
		controller.abort();
		const result = await work;
		assert.equal(result.aborted, true);
		assert.ok(phases.includes("正在取消"));
		assert.ok(phases.includes("已取消"));
		assert.equal(owner.children.size, 0);
		assert.equal(owner.slots, 0);
		assert.doesNotMatch(result.errorMessage!, /FORCE_SETTLE/);
	} finally { beginWorkerShutdown(owner); }
});

test("SIGTERM resistance and suppressed close reach bounded strong settlement; delayed close cannot settle twice", { timeout: 10_000 }, async () => {
	const owner = createWorkerOwner();
	const controller = new AbortController();
	let settled = 0;
	let ready = false;
	const work = launch(owner, "stubborn", undefined, controller.signal, 10_000, (progress) => { ready ||= progress.activities.some((activity) => activity.id === "tool:fixture-ready"); }).then((result) => { settled++; return result; });
	await until(() => owner.children.size === 1);
	const child = [...owner.children][0];
	const originalEmit = child.emit;
	let delayedClose: any[] | undefined;
	child.emit = function (event: string | symbol, ...args: any[]) {
		if (event === "close") { delayedClose = args; return true; }
		return originalEmit.call(this, event, ...args);
	};
	try {
		await until(() => ready);
		controller.abort();
		const result = await work;
		assert.equal(result.exitCode, 1); assert.equal(result.aborted, true);
		assert.equal(result.termination?.source, "abort_signal");
		assert.match(result.errorMessage!, /cleanup=FORCE_SETTLE_NO_CLOSE/);
		assert.equal(settled, 1); assert.equal(owner.slots, 0);
		assert.equal(delayedClose?.[1], "SIGKILL", "SIGKILL closed the resistant child even when the event was hidden");
		assert.equal(owner.children.size, 1, "unconfirmed close remains tracked for the exit fallback");
		originalEmit.call(child, "close", ...delayedClose!);
		assert.equal(owner.children.size, 0);
		assert.equal(owner.stops.size, 0);
		assert.equal(settled, 1);
	} finally {
		child.emit = originalEmit;
		if (delayedClose) originalEmit.call(child, "close", ...delayedClose);
		beginWorkerShutdown(owner);
	}
});
