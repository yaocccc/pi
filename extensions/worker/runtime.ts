import { randomUUID } from "node:crypto";
import type { ParentQuestion } from "./ipc.ts";
import { ANSWER_LIMIT, MAX_QUESTIONS } from "./ipc.ts";
import type { WorkerAnswer, WorkerTask, WorkerUiDetails } from "./types.ts";

export interface QuestionRecord extends ParentQuestion {
	batchId: string;
	taskId: string;
	status: "waiting" | "answered" | "expired" | "cancelled";
	askedAt: number;
	expiresAt: number;
	answeredAt?: number;
	answer?: string;
}
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
	single: boolean;
	ui: WorkerUiDetails;
	tasks: RuntimeTask[];
	questions: QuestionRecord[];
	finished: boolean;
	cancelled: boolean;
	execute: (task: RuntimeTask) => Promise<Record<string, any>>;
}
interface PendingAnswer {
	record: QuestionRecord;
	resolve: (value: string) => void;
	reject: (error: Error) => void;
	cleanup: () => void;
	task: RuntimeTask;
}

/** Session-owned queue: slots and path locks span ALL batches, including Q&A waits. */
export class WorkerRuntime {
	readonly batches = new Map<string, Batch>();
	private queue: RuntimeTask[] = [];
	private active = new Set<RuntimeTask>();
	private pending = new Map<string, PendingAnswer>();
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
	start(input: Omit<Batch, "id" | "tasks" | "questions" | "finished" | "cancelled">, tasks: WorkerTask[], cwd: string): Batch {
		if (this.closed) throw new Error("Worker 会话已关闭");
		if (this.batches.size >= 64) throw new Error("本会话已达 64 个 Worker 批次上限；请在新会话继续");
		const batch: Batch = { ...input, id: randomUUID(), tasks: [], questions: [], finished: false, cancelled: false };
		batch.ui.batchId = batch.id;
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
				ui.startedAt = Date.now();
				this.active.add(task);
				this.changed();
				task.deadline = Date.now() + task.batch.timeoutMs;
				task.timer = setTimeout(() => { task.timedOut = true; this.cancelTask(task, "Worker 任务超时"); }, task.batch.timeoutMs);
				task.timer.unref();
				void Promise.resolve().then(() => task.batch.execute(task)).then((result) => {
					task.result = result;
				}, (error) => {
					task.result = this.failure(error instanceof Error ? error.message : String(error));
				}).finally(() => {
					// The event loop may resolve execute before dispatching an overdue timer.
					if (!task.controller.signal.aborted && Date.now() >= task.deadline!) {
						task.timedOut = true;
						this.cancelTask(task, "Worker 任务超时");
					}
					clearTimeout(task.timer);
					// Cancellation wins over a late successful result from a cooperative runner.
					if (task.controller.signal.aborted) {
						const reason = String(task.controller.signal.reason?.message ?? "Worker 已取消");
						task.result = { ...task.result, status: "failed", summary: [reason],
							failure: { category: task.timedOut ? "timeout" : "cancelled", reason, retryable: false, next_action: "由主 Agent 检查，不要自动重试。" },
							execution: { ...task.result?.execution, cancelled: !task.timedOut, timed_out: !!task.timedOut },
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
		for (const pending of [...this.pending.values()]) if (pending.record.taskId === task.id) this.endQuestion(pending, "expired", "Worker 已结束");
		const ui = task.batch.ui.tasks[task.index];
		ui.result = task.result;
		ui.status = task.result?.status === "completed" ? "completed" : task.result?.status === "blocked" ? "blocked" : "failed";
		ui.finishedAt = Date.now();
		ui.phase = ui.status === "completed" ? "已完成" : ui.status === "blocked" ? "已阻塞" : "执行失败";
		task.batch.ui.completed = task.batch.tasks.filter((item) => item.state === "finished").length;
		task.batch.finished = task.batch.tasks.every((item) => item.state === "finished");
		if (task.batch.finished) task.batch.ui.finishedAt = Date.now();
	}
	ask(task: RuntimeTask, question: ParentQuestion, signal: AbortSignal): Promise<string> {
		if (this.closed || task.state !== "running" || task.controller.signal.aborted || signal.aborted) return Promise.reject(new Error("Worker 已结束或取消"));
		if (task.deadline === undefined || Date.now() >= task.deadline) return Promise.reject(new Error("Worker 任务已过期"));
		if (task.batch.questions.some((item) => item.id === question.id) || task.batch.questions.filter((item) => item.taskId === task.id).length >= MAX_QUESTIONS) return Promise.reject(new Error("重复问题 ID 或问题数量超过限制"));
		const askedAt = Date.now();
		const record: QuestionRecord = { ...question, batchId: task.batch.id, taskId: task.id, askedAt, expiresAt: askedAt + question.timeoutMs, status: "waiting" };
		task.batch.questions.push(record);
		return new Promise((resolve, reject) => {
			const abort = () => this.endQuestion(pending, signal.reason?.name === "TimeoutError" ? "expired" : "cancelled", "问题已取消、过期或 IPC 已关闭");
			const timer = setTimeout(() => this.endQuestion(pending, "expired", "等待主 Agent 回答超时"), question.timeoutMs);
			timer.unref();
			const pending: PendingAnswer = { record, resolve, reject, task, cleanup: () => {
				clearTimeout(timer); signal.removeEventListener("abort", abort);
			} };
			this.pending.set(`${task.id}/${question.id}`, pending);
			signal.addEventListener("abort", abort, { once: true });
			this.changed();
		});
	}
	private endQuestion(pending: PendingAnswer, status: "expired" | "cancelled", reason: string) {
		if (pending.record.status !== "waiting") return;
		pending.record.status = status;
		pending.cleanup();
		this.pending.delete(`${pending.record.taskId}/${pending.record.id}`);
		pending.reject(new Error(reason));
		this.changed();
	}
	reply(batchId: string, taskId: string, questionId: string, answer: string): QuestionRecord {
		return this.replyMany(batchId, [{ taskId, questionId, answer }])[0];
	}
	/** Validate the entire submission before resolving ANY worker (no partial application). */
	replyMany(batchId: string, answers: WorkerAnswer[]): QuestionRecord[] {
		const batch = this.get(batchId);
		if (!Array.isArray(answers) || !answers.length || answers.length > batch.tasks.length * MAX_QUESTIONS) throw new Error("answers 必须为非空且不超过批次问题上限的数组");
		const seen = new Set<string>();
		const now = Date.now();
		const validated = answers.map((item) => {
			if (!item || typeof item.taskId !== "string" || typeof item.questionId !== "string" || typeof item.answer !== "string" || !item.answer.trim() || item.answer.length > ANSWER_LIMIT) throw new Error("回答标识无效、回答为空或超过长度限制");
			const key = `${item.taskId}/${item.questionId}`;
			if (seen.has(key)) throw new Error("answers 包含重复问题");
			seen.add(key);
			if (!batch.tasks.some((task) => task.id === item.taskId)) throw new Error("未知或跨批次的 taskId");
			const record = batch.questions.find((q) => q.id === item.questionId);
			if (!record) throw new Error("未知或跨批次的 questionId");
			if (record.taskId !== item.taskId) throw new Error("questionId 属于其他任务，不能跨任务回答");
			if (record.status === "answered") throw new Error("问题已回答，不能重复回答");
			if (record.status !== "waiting") throw new Error("问题已取消或过期");
			const pending = this.pending.get(key);
			if (!pending || now >= record.expiresAt || pending.record !== record) throw new Error("问题已过期");
			if (this.closed || batch.cancelled || batch.finished || pending.task.state !== "running" || pending.task.controller.signal.aborted) throw new Error("Worker 任务已结束或取消，问题已过期");
			if (pending.task.deadline === undefined || now >= pending.task.deadline) throw new Error("Worker 任务总期限已过期");
			return { pending, answer: item.answer };
		});
		for (const { pending, answer } of validated) {
			pending.record.status = "answered";
			pending.record.answer = answer;
			pending.record.answeredAt = now;
			pending.cleanup();
			this.pending.delete(`${pending.record.taskId}/${pending.record.id}`);
			pending.resolve(answer);
		}
		this.changed();
		return validated.map(({ pending }) => pending.record);
	}
	private cancelTask(task: RuntimeTask, reason: string) {
		if (task.state === "finished") return;
		clearTimeout(task.timer);
		task.controller.abort(new Error(reason));
		for (const pending of [...this.pending.values()]) if (pending.record.taskId === task.id) this.endQuestion(pending, "cancelled", reason);
		if (task.state === "queued") {
			this.queue = this.queue.filter((item) => item !== task);
			task.result = this.failure(reason);
			this.finish(task);
		}
	}
	cancel(batchId: string, reason = "Worker 批次已取消") {
		const batch = this.get(batchId);
		if (batch.finished) return;
		batch.cancelled = true;
		for (const task of batch.tasks) this.cancelTask(task, reason);
		this.changed();
		this.schedule();
	}
	/** No polling deadline. An active invocation's abort cancels its batch and waits for cleanup. */
	wait(batchId: string, signal?: AbortSignal): Promise<void> {
		const batch = this.get(batchId);
		return new Promise((resolve) => {
			const finish = () => { this.listeners.delete(check); signal?.removeEventListener("abort", abort); resolve(); };
			const check = () => {
				if (batch.finished || (!batch.cancelled && !this.closed && batch.questions.some((q) => q.status === "waiting"))) finish();
			};
			const abort = () => this.cancel(batch.id, "Worker 调用已取消");
			this.listeners.add(check);
			signal?.addEventListener("abort", abort, { once: true });
			if (signal?.aborted) abort();
			check();
		});
	}
	async dispose(): Promise<void> {
		this.closed = true;
		for (const batch of this.batches.values()) this.cancel(batch.id, "Worker 会话已关闭");
		this.changed();
		if (!this.active.size) return;
		await new Promise<void>((resolve) => {
			const check = () => { if (!this.active.size) { this.listeners.delete(check); resolve(); } };
			this.listeners.add(check);
			check();
		});
	}
}
