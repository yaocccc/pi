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
export interface ChannelClose { source: "local" | "eof" | "transport" | "protocol"; code: string }
export type ShutdownReason = "completed" | "cancelled" | "timeout" | "owner_shutdown" | "output" | "ipc_failure";
const shutdownReasons: readonly string[] = ["completed", "cancelled", "timeout", "owner_shutdown", "output", "ipc_failure"];

/** Private, inherited duplex pipe. Never multiplex control messages onto model stdout. */
class Frames {
	private buffer = "";
	private decoder = new StringDecoder("utf8");
	closed = false;
	constructor(readonly stream: Duplex, readonly receive: (message: any) => void, readonly ended: (reason: ChannelClose) => void) {
		stream.on("data", this.data);
		stream.on("error", this.error);
		stream.on("end", this.eof);
		stream.on("close", this.eof);
	}
	private error = (error: NodeJS.ErrnoException) => this.close({ source: "transport", code: /^[A-Z0-9_]{1,40}$/.test(error.code ?? "") ? error.code! : "SOCKET_ERROR" });
	private eof = () => {
		if (this.closed) return;
		this.buffer += this.decoder.end();
		this.close(this.buffer.length ? { source: "protocol", code: "INCOMPLETE_FRAME" } : { source: "eof", code: "EOF" });
	};
	private data = (chunk: Buffer) => {
		if (this.closed) return;
		this.buffer += this.decoder.write(chunk);
		let newline: number;
		while (!this.closed && (newline = this.buffer.indexOf("\n")) >= 0) {
			const line = this.buffer.slice(0, newline);
			this.buffer = this.buffer.slice(newline + 1);
			if (Buffer.byteLength(line) > FRAME_LIMIT) return this.close({ source: "protocol", code: "FRAME_LIMIT" });
			let message: unknown;
			try { message = JSON.parse(line); } catch { this.close({ source: "protocol", code: "INVALID_JSON" }); return; }
			this.receive(message);
		}
		if (Buffer.byteLength(this.buffer) > FRAME_LIMIT) this.close({ source: "protocol", code: "FRAME_LIMIT" });
	};
	send(message: unknown): boolean {
		if (this.closed) return false;
		if (this.stream.destroyed) { this.close({ source: "transport", code: "SOCKET_CLOSED" }); return false; }
		const frame = JSON.stringify(message) + "\n";
		if (Buffer.byteLength(frame) > FRAME_LIMIT || this.stream.writableLength > FRAME_LIMIT * 2) { this.close({ source: "protocol", code: "WRITE_LIMIT" }); return false; }
		try { this.stream.write(frame, (error) => { if (error) this.error(error); }); }
		catch (error) { this.error(error as NodeJS.ErrnoException); return false; }
		return true;
	}
	close = (reason: ChannelClose = { source: "local", code: "LOCAL_CLOSE" }) => {
		if (this.closed) return;
		this.closed = true;
		this.buffer = "";
		this.stream.removeListener("data", this.data);
		this.stream.destroy();
		this.ended(reason);
	};
}

function validId(id: unknown): id is string { return typeof id === "string" && /^[a-zA-Z0-9-]{1,80}$/.test(id); }
function textInBounds(value: unknown, max: number): value is string { return typeof value === "string" && !!value.trim() && value.length <= max; }

export class ParentChannel {
	private frames: Frames;
	private pending = new Map<string, AbortController>();
	// Keep bounded controllers for late child-side expiry after an answer was sent.
	private seen = new Map<string, AbortController>();
	constructor(stream: Duplex, ask: AskParent, onClose: (reason: ChannelClose & { pending: number }) => void = () => {}) {
		this.frames = new Frames(stream, (message) => {
			if (!message || !validId(message.id)) { this.invalid(); return; }
			if (message.type === "cancel") {
				if (message.reason !== "timeout" && message.reason !== "abort") { this.invalid(); return; }
				const error = new Error(message.reason === "timeout" ? "问题已过期" : "问题已取消");
				if (message.reason === "timeout") error.name = "TimeoutError";
				const controller = this.seen.get(message.id);
				this.pending.delete(message.id);
				controller?.abort(error);
				return;
			}
			if (message.type !== "question" || !textInBounds(message.question, QUESTION_LIMIT) || !Number.isInteger(message.timeoutMs) || message.timeoutMs < 1 || message.timeoutMs > 600_000) { this.invalid(); return; }
			if (this.seen.has(message.id) || this.seen.size >= MAX_QUESTIONS) {
				this.frames.send({ type: "error", id: message.id, error: "重复问题 ID 或问题数量超过限制" });
				return;
			}
			const controller = new AbortController();
			this.seen.set(message.id, controller);
			this.pending.set(message.id, controller);
			let answer: Promise<string>;
			try { answer = ask(message, controller.signal); } catch (error) { answer = Promise.reject(error); }
			void answer.then((value) => {
				// Retire before write (not in a later finally): answer + EOF can share a turn.
				this.pending.delete(message.id);
				if (!controller.signal.aborted) this.frames.send({ type: "answer", id: message.id, answer: value });
			}, (error) => {
				this.pending.delete(message.id);
				if (!controller.signal.aborted) this.frames.send({ type: "error", id: message.id, error: error instanceof Error ? error.message : String(error) });
			});
		}, (reason) => {
			const pending = this.pending.size;
			// Notify the process before rejecting questions: a child may catch and claim success.
			onClose({ ...reason, pending });
			for (const controller of this.pending.values()) controller.abort(new Error(`Worker IPC 已关闭 [source=${reason.source} code=${reason.code}]`));
			this.pending.clear();
			this.seen.clear();
		});
	}
	private invalid() { this.frames.close({ source: "protocol", code: "INVALID_MESSAGE" }); }
	close(reason: ShutdownReason = "completed") {
		// Best effort only. Do not await delivery or extend cancellation grace.
		this.frames.send({ type: "shutdown", reason });
		this.frames.close({ source: "local", code: reason });
	}
}

export class ChildChannel {
	private frames: Frames;
	private shutdownReason?: ShutdownReason;
	private pending = new Map<string, { resolve: (answer: string) => void; reject: (error: Error) => void; deadline: number }>();
	constructor(stream: Duplex) {
		this.frames = new Frames(stream, (message) => {
			if (message?.type === "shutdown" && shutdownReasons.includes(message.reason)) {
				this.shutdownReason = message.reason;
				this.frames.close({ source: "local", code: message.reason });
				return;
			}
			if (!message || !validId(message.id) || !["answer", "error"].includes(message.type)) { this.frames.close({ source: "protocol", code: "INVALID_MESSAGE" }); return; }
			const pending = this.pending.get(message.id);
			if (!pending) return; // Late answers never revive cancelled calls.
			if (Date.now() >= pending.deadline) {
				this.frames.send({ type: "cancel", id: message.id, reason: "timeout" });
				pending.reject(new Error("问题已过期")); return;
			}
			if (message.type === "answer" && textInBounds(message.answer, ANSWER_LIMIT)) pending.resolve(message.answer);
			else pending.reject(new Error(typeof message.error === "string" ? message.error : "无效的父进程回答"));
		}, (reason) => {
			const description = this.shutdownReason ? `主 Agent 已结束本 Worker 请求 [reason=${this.shutdownReason}]` : `父进程 IPC 已关闭 [source=${reason.source} code=${reason.code}]，原因未知（不代表父进程必然退出）`;
			for (const pending of this.pending.values()) pending.reject(new Error(`${description}；不得猜测授权或自动重试`));
		});
	}
	ask(question: string, signal?: AbortSignal, timeoutMs = QUESTION_TIMEOUT_MS): Promise<{ questionId: string; answer: string }> {
		if (!textInBounds(question, QUESTION_LIMIT)) return Promise.reject(new Error("问题为空或超过长度限制"));
		if (!Number.isInteger(timeoutMs) || timeoutMs < 1 || timeoutMs > 600_000) return Promise.reject(new Error("无效的问题超时"));
		if (signal?.aborted || this.frames.closed) return Promise.reject(new Error("ask_parent 已取消或连接已关闭；不得猜测授权"));
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
			if (!this.frames.send({ type: "question", id, question, timeoutMs })) finish(new Error("父进程 IPC 不可用；不得猜测授权"));
		});
	}
	close() { this.frames.close(); }
}
