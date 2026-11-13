import { randomUUID } from "node:crypto";
import type { WorkerTask, WorkerUiDetails } from "./types.ts";

export interface RuntimeTask {
	id: string;
	index: number;
	batch: Batch;
	task: WorkerTask;
	cwd: string;
	state: "queued" | "running" | "finished";
	controller: AbortController;
	overlappedWriter: boolean;
	timedOut?: boolean;
	deadline?: number;
	timer?: NodeJS.Timeout;
	result?: Record<string, any>;
}
export interface Batch {
	id: string;
	limit: number;
	timeoutMs: number;
	maxOutputBytes: number;
	ui: WorkerUiDetails;
	tasks: RuntimeTask[];
	finished: boolean;
	cancelled: boolean;
	execute: (task: RuntimeTask) => Promise<Record<string, any>>;
}
/** Session-owned queue: slots and path locks span all batches until task cleanup. */
export class WorkerRuntime {
	readonly batches = new Map<string, Batch>();
	private queue: RuntimeTask[] = [];
	private active = new Set<RuntimeTask>();
	private listeners = new Set<() => void>();
	private closed = false;
	private scheduling = false;
	constructor(private conflicts: (left: RuntimeTask, right: RuntimeTask) => boolean, private onChange: () => void = () => {}) {}

	changed() {
		for (const batch of this.batches.values()) batch.ui.revision = (batch.ui.revision ?? 0) + 1;
		// Presentation errors must never strand task promises or path locks.
		try { this.onChange(); } catch { /* UI may already be disposed */ }
		for (const listener of [...this.listeners]) listener();
	}
	start(input: Omit<Batch, "id" | "tasks" | "finished" | "cancelled">, tasks: WorkerTask[], cwd: string): Batch {
		if (this.closed) throw new Error("Worker 会话已关闭");
		if (this.batches.size >= 64) throw new Error("本会话已达 64 个 Worker 批次上限；请在新会话继续");
		const batch: Batch = { ...input, id: randomUUID(), tasks: [], finished: false, cancelled: false };
		batch.ui.batchId = batch.id;
		for (const ui of batch.ui.tasks) ui.phaseCode = "queued";
		batch.tasks = tasks.map((task, index) => ({ id: `${batch.id}:${index + 1}`, index, batch, task, cwd, state: "queued", controller: new AbortController(), overlappedWriter: false }));
		this.batches.set(batch.id, batch);
		this.queue.push(...batch.tasks);
		this.changed();
		this.schedule();
		return batch;
	}
	get(id: string): Batch {
		const batch = this.batches.get(id);
		if (!batch) throw new Error("未知或已过期的 Worker batchId（批次仅属于当前会话）");
		return batch;
	}
	private schedule() {
		if (this.scheduling || this.closed) return;
		this.scheduling = true;
		try {
			const limits = [...this.batches.values()].filter((batch) => !batch.finished).map((batch) => batch.limit);
			const limit = Math.max(1, Math.min(...limits));
			while (this.active.size < limit) {
				const index = this.queue.findIndex((task, i) => [...this.active].every((running) => !this.conflicts(task, running)) && this.queue.slice(0, i).every((earlier) => !this.conflicts(task, earlier)));
				if (index < 0) break;
				const [task] = this.queue.splice(index, 1);
				for (const other of this.active) {
					if (["implement", "test", "fix"].includes(task.task.mode) && ["implement", "test", "fix"].includes(other.task.mode)) task.overlappedWriter = other.overlappedWriter = true;
				}
				task.state = "running";
				const ui = task.batch.ui.tasks[task.index];
				ui.status = "running";
				ui.phase = "执行中";
				ui.phaseCode = "running";
				ui.startedAt = Date.now();
				this.active.add(task);
				this.changed();
				task.deadline = Date.now() + task.batch.timeoutMs;
				task.timer = setTimeout(() => { task.timedOut = true; this.cancelTask(task, "Worker 任务超时", "task_timeout"); }, task.batch.timeoutMs);
				task.timer.unref();
				void Promise.resolve().then(() => task.batch.execute(task)).then((result) => {
					task.result = result;
				}, (error) => {
					task.result = this.failure(error instanceof Error ? error.message : String(error));
				}).finally(() => {
					// The event loop may resolve execute before dispatching an overdue timer.
					if (!task.controller.signal.aborted && Date.now() >= task.deadline!) {
						task.timedOut = true;
						this.cancelTask(task, "Worker 任务超时", "task_timeout");
					}
					clearTimeout(task.timer);
					// Cancellation wins over a late successful result from a cooperative runner.
					if (task.controller.signal.aborted) {
						const reason = String(task.controller.signal.reason?.message ?? "Worker 已取消");
						task.result = { ...task.result, status: "failed", summary: [reason],
							failure: { category: task.timedOut ? "timeout" : "cancelled", reason, retryable: false, next_action: "由主 Agent 检查，不要自动重试。" },
							execution: { ...task.result?.execution, cancelled: !task.timedOut, timed_out: !!task.timedOut, termination_source: task.controller.signal.reason?.source },
						};
					}
					this.finish(task);
					this.active.delete(task);
					this.changed();
					this.schedule();
				});
			}
		} finally { this.scheduling = false; }
	}
	private failure(reason: string) {
		return { status: "failed", summary: [reason], changed_files: [], observed_changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: ["主 Agent 检查失败原因"] };
	}
	private finish(task: RuntimeTask) {
		task.state = "finished";
		const ui = task.batch.ui.tasks[task.index];
		ui.result = task.result;
		ui.status = task.result?.status === "completed" ? "completed" : task.result?.status === "blocked" ? "blocked" : "failed";
		ui.finishedAt = Date.now();
		ui.phase = ui.status === "completed" ? "已完成" : ui.status === "blocked" ? "已阻塞" : "执行失败";
		ui.phaseCode = task.result?.execution?.timed_out ? "timed_out" : task.result?.execution?.cancelled ? "cancelled" : ui.status;
		task.batch.ui.completed = task.batch.tasks.filter((item) => item.state === "finished").length;
		task.batch.finished = task.batch.tasks.every((item) => item.state === "finished");
		if (task.batch.finished) task.batch.ui.finishedAt = Date.now();
	}
	private cancelTask(task: RuntimeTask, reason: string, source: string) {
		if (task.state === "finished") return;
		clearTimeout(task.timer);
		task.controller.abort(Object.assign(new Error(reason), { source }));
		if (task.state === "queued") {
			this.queue = this.queue.filter((item) => item !== task);
			task.result = { ...this.failure(reason),
				failure: { category: task.timedOut ? "timeout" : "cancelled", reason, retryable: false, next_action: "由主 Agent 检查，不要自动重试。" },
				execution: { cancelled: !task.timedOut, timed_out: !!task.timedOut, termination_source: task.controller.signal.reason?.source },
			};
			this.finish(task);
		}
	}
	cancel(batchId: string, reason = "Worker 批次已取消", source = "batch_cancel") {
		const batch = this.get(batchId);
		if (batch.finished) return;
		batch.cancelled = true;
		for (const task of batch.tasks) this.cancelTask(task, reason, source);
		this.changed();
		this.schedule();
	}
	/** No polling deadline. An active invocation's abort cancels its batch and waits for cleanup. */
	wait(batchId: string, signal?: AbortSignal): Promise<void> {
		const batch = this.get(batchId);
		return new Promise((resolve) => {
			const finish = () => { this.listeners.delete(check); signal?.removeEventListener("abort", abort); resolve(); };
			const check = () => {
				if (batch.finished) finish();
			};
			const abort = () => this.cancel(batch.id, "Worker 调用已取消", "invocation_abort");
			this.listeners.add(check);
			signal?.addEventListener("abort", abort, { once: true });
			if (signal?.aborted) abort();
			check();
		});
	}
	async dispose(): Promise<void> {
		this.closed = true;
		for (const batch of this.batches.values()) this.cancel(batch.id, "Worker 会话已关闭", "session_shutdown");
		this.changed();
		if (!this.active.size) return;
		await new Promise<void>((resolve) => {
			const check = () => { if (!this.active.size) { this.listeners.delete(check); resolve(); } };
			this.listeners.add(check);
			check();
		});
	}
}
