import { setTimeout as delay } from "node:timers/promises";
import type { WorkerRuntime, Batch } from "../runtime.ts";
import { emptyWorkerUsage } from "../ui.ts";
import type { WorkerTask } from "../types.ts";

export const task = (path: string): WorkerTask => ({ mode: "implement", objective: path, allowedPaths: [path] });
export function start(runtime: WorkerRuntime, tasks: WorkerTask[], execute: Batch["execute"], limit = 2, timeoutMs = 2_000): Batch {
	return runtime.start({
		limit, timeoutMs, maxOutputBytes: 65536, single: tasks.length === 1, execute,
		ui: { kind: "worker-ui", startedAt: Date.now(), limit, total: tasks.length, completed: 0, tasks: tasks.map((t, index) => ({ index, mode: t.mode, objective: t.objective, status: "queued", requestedPreset: "auto", attempt: 0, phase: "等待执行", activities: [], toolCalls: 0, usage: emptyWorkerUsage() })) },
	}, tasks, process.cwd());
}
export async function until(check: () => boolean, ms = 2_000) {
	const end = Date.now() + ms;
	while (!check()) { if (Date.now() > end) throw new Error("condition timed out"); await delay(5); }
}
