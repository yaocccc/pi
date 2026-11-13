import assert from "node:assert/strict";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { acquireSlot, beginWorkerShutdown, createWorkerOwner, runPiWorker } from "./process.ts";
import { WorkerRuntime } from "./runtime.ts";
import { start, task, until } from "./fixtures/helpers.ts";
import { cloneUiDetails } from "./data.ts";
import { WORKER_SNAPSHOT_ENTRY, workerBranchSnapshots } from "./history.ts";
import type { ChildProgress, Route } from "./types.ts";

const route: Route = { requestedPreset: "fast", resolvedPreset: "fast", modelId: "fixture/local", provider: "fixture", thinking: "high", routeReason: [] };
const fixture = fileURLToPath(new URL("./fixtures/child.mjs", import.meta.url));

test("slot phase codes survive progress, runtime details, cloning and restored history", async () => {
	const owner = createWorkerOwner();
	const release = await acquireSlot(1, undefined, 5000, owner);
	const runtime = new WorkerRuntime(() => false);
	const snapshots: ChildProgress[] = [];
	const batch = start(runtime, [task("README.md")], async (item) => {
		await runPiWorker({ ...item.task, mode: "scout", sessionMode: "new" }, route, process.cwd(), "fixture", "fixture", undefined, 5000, 1, progress => {
			snapshots.push(progress);
			Object.assign(item.batch.ui.tasks[item.index], progress);
			runtime.changed();
		}, { owner, invocation: { command: process.execPath, args: [fixture, "tool-tokens"] } });
		return { status: "completed" };
	}, 1, 10000);
	try {
		await until(() => owner.waiters.length === 1);
		assert.equal(batch.ui.tasks[0].phaseCode, "waiting_slot");
		release();
		await runtime.wait(batch.id);
		assert.ok(snapshots.some(p => p.phaseCode === "slot_acquired"));
		assert.equal(snapshots.at(-1)!.phaseCode, "completed");
		assert.equal(batch.ui.tasks[0].phaseCode, "completed");
		const details = cloneUiDetails(batch.ui);
		const restored = workerBranchSnapshots([
			{ type: "message", message: { role: "assistant", content: [{ type: "toolCall", name: "worker", id: "call", arguments: { tasks: [] } }] } },
			{ type: "custom", customType: WORKER_SNAPSHOT_ENTRY, data: JSON.parse(JSON.stringify({ ...details, originToolCallId: "call" })) },
		]).get(batch.id)!;
		assert.equal(restored.tasks[0].phaseCode, "completed");
		assert.ok(restored.tasks[0].activities.some(a => a.phaseCode === "slot_acquired"));
	} finally { release(); beginWorkerShutdown(owner); await runtime.dispose(); }
});

test("slot timeout and cancellation have non-slot codes", async () => {
	for (const cancelled of [false, true]) {
		// Slot deadlines are unref'ed in production; keep this isolated test alive.
		const keepAlive = setInterval(() => {}, 1000);
		const owner = createWorkerOwner();
		const release = await acquireSlot(1, undefined, 5000, owner);
		const controller = new AbortController();
		const snapshots: ChildProgress[] = [];
		try {
			const pending = runPiWorker({ mode: "scout", preset: "fast", objective: "fixture", sessionMode: "new" }, route, process.cwd(), "fixture", "fixture", controller.signal, cancelled ? 5000 : 100, 1, progress => snapshots.push(progress), { owner });
			await until(() => owner.waiters.length === 1);
			if (cancelled) controller.abort();
			const result = await pending;
			const code = cancelled ? "cancelled" : "timed_out";
			assert.equal(snapshots.at(-1)!.phaseCode, code);
			assert.equal(result.activities.at(-1)!.phaseCode, code);
		} finally { release(); beginWorkerShutdown(owner); clearInterval(keepAlive); }
	}
});
