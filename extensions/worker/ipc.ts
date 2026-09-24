import { randomUUID } from "node:crypto";
import type { Duplex } from "node:stream";
import { StringDecoder } from "node:string_decoder";

export const QUESTION_LIMIT = 2_000;
export const ANSWER_LIMIT = 4_000;
export const QUESTION_TIMEOUT_MS = 120_000;
export const MAX_QUESTIONS = 32;
const FRAME_LIMIT = 64 * 1024;
export const PARENT_FD_ENV = "PI_WORKER_PARENT_FD";

export interface ParentQuestion { id: string; question: string; timeoutMs: number }
export type AskParent = (question: ParentQuestion, signal: AbortSignal) => Promise<string>;

/** Private, inherited duplex pipe. Never multiplex control messages onto model stdout. */
class Frames {
	private buffer = "";
	private decoder = new StringDecoder("utf8");
	closed = false;
	constructor(readonly stream: Duplex, readonly receive: (message: any) => void, readonly ended: () => void) {
		stream.on("data", this.data);
		stream.on("error", this.close);
		stream.on("end", this.close);
		stream.on("close", this.close);
	}
	private data = (chunk: Buffer) => {
		this.buffer += this.decoder.write(chunk);
		let newline: number;
		while ((newline = this.buffer.indexOf("\n")) >= 0) {
			const line = this.buffer.slice(0, newline);
			this.buffer = this.buffer.slice(newline + 1);
			if (Buffer.byteLength(line) > FRAME_LIMIT) return this.close();
			try { this.receive(JSON.parse(line)); } catch { this.close(); return; }
		}
		if (Buffer.byteLength(this.buffer) > FRAME_LIMIT) this.close();
	};
	send(message: unknown): boolean {
		if (this.closed || this.stream.destroyed) return false;
		const frame = JSON.stringify(message) + "\n";
		if (Buffer.byteLength(frame) > FRAME_LIMIT || this.stream.writableLength > FRAME_LIMIT * 2) { this.close(); return false; }
		this.stream.write(frame, (error) => { if (error) this.close(); });
		return true;
	}
	close = () => {
		if (this.closed) return;
		this.closed = true;
		this.buffer = "";
		this.stream.removeListener("data", this.data);
		this.stream.destroy();
		this.ended();
	};
}

function validId(id: unknown): id is string { return typeof id === "string" && /^[a-zA-Z0-9-]{1,80}$/.test(id); }
function textInBounds(value: unknown, max: number): value is string { return typeof value === "string" && !!value.trim() && value.length <= max; }

export class ParentChannel {
	private frames: Frames;
	private pending = new Map<string, AbortController>();
	private seen = new Set<string>();
	constructor(stream: Duplex, ask: AskParent) {
		this.frames = new Frames(stream, (message) => {
			if (!message || !validId(message.id)) { this.close(); return; }
			if (message.type === "cancel") {
				const error = new Error(message.reason === "timeout" ? "问题已过期" : "问题已取消");
				if (message.reason === "timeout") error.name = "TimeoutError";
				this.pending.get(message.id)?.abort(error);
				return;
			}
			if (message.type !== "question" || !textInBounds(message.question, QUESTION_LIMIT) || !Number.isInteger(message.timeoutMs) || message.timeoutMs < 1 || message.timeoutMs > 600_000) { this.close(); return; }
			if (this.seen.has(message.id) || this.seen.size >= MAX_QUESTIONS) {
				this.frames.send({ type: "error", id: message.id, error: "重复问题 ID 或问题数量超过限制" });
				return;
			}
			this.seen.add(message.id);
			const controller = new AbortController();
			this.pending.set(message.id, controller);
			// Register synchronously: the parent may receive a reply on the next turn.
			let answer: Promise<string>;
			try { answer = ask(message, controller.signal); } catch (error) { answer = Promise.reject(error); }
			void answer.then((value) => {
				if (!controller.signal.aborted) this.frames.send({ type: "answer", id: message.id, answer: value });
			}, (error) => {
				this.frames.send({ type: "error", id: message.id, error: error instanceof Error ? error.message : String(error) });
			}).finally(() => this.pending.delete(message.id));
		}, () => {
			for (const controller of this.pending.values()) controller.abort(new Error("Worker IPC 已关闭"));
			this.pending.clear();
		});
	}
	close() { this.frames.close(); }
}

export class ChildChannel {
	private frames: Frames;
	private pending = new Map<string, { resolve: (answer: string) => void; reject: (error: Error) => void; deadline: number }>();
	constructor(stream: Duplex) {
		this.frames = new Frames(stream, (message) => {
			if (!message || !validId(message.id)) { this.close(); return; }
			const pending = this.pending.get(message.id);
			if (!pending) return; // Late answers never revive cancelled calls.
			if (Date.now() >= pending.deadline) {
				this.frames.send({ type: "cancel", id: message.id, reason: "timeout" });
				pending.reject(new Error("问题已过期")); return;
			}
			if (message.type === "answer" && textInBounds(message.answer, ANSWER_LIMIT)) pending.resolve(message.answer);
			else pending.reject(new Error(typeof message.error === "string" ? message.error : "无效的父进程回答"));
		}, () => {
			for (const pending of this.pending.values()) pending.reject(new Error("父进程 IPC 已关闭"));
		});
	}
	ask(question: string, signal?: AbortSignal, timeoutMs = QUESTION_TIMEOUT_MS): Promise<{ questionId: string; answer: string }> {
		if (!textInBounds(question, QUESTION_LIMIT)) return Promise.reject(new Error("问题为空或超过长度限制"));
		if (!Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 600_000) return Promise.reject(new Error("无效的问题超时"));
		if (signal?.aborted || this.frames.closed) return Promise.reject(new Error("ask_parent 已取消或连接已关闭"));
		const id = randomUUID();
		return new Promise((resolve, reject) => {
			const finish = (error?: Error, answer?: string) => {
				if (!this.pending.delete(id)) return;
				clearTimeout(timer);
				signal?.removeEventListener("abort", abort);
				if (!this.pending.size) (this.frames.stream as any).unref?.();
				if (error) reject(error); else resolve({ questionId: id, answer: answer! });
			};
			const cancel = (reason: "abort" | "timeout") => {
				this.frames.send({ type: "cancel", id, reason });
				finish(new Error(reason === "timeout" ? "等待主 Agent 回答超时" : "ask_parent 已取消"));
			};
			const abort = () => cancel("abort");
			const deadline = Date.now() + timeoutMs;
			const timer = setTimeout(() => cancel("timeout"), timeoutMs);
			this.pending.set(id, { resolve: (answer) => finish(undefined, answer), reject: (error) => finish(error), deadline });
			(this.frames.stream as any).ref?.();
			signal?.addEventListener("abort", abort, { once: true });
			if (!this.frames.send({ type: "question", id, question, timeoutMs })) finish(new Error("父进程 IPC 不可用"));
		});
	}
	close() { this.frames.close(); }
}
