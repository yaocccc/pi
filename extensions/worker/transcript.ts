import * as fs from "node:fs";
import * as path from "node:path";
import { randomUUID } from "node:crypto";
import { agentDir } from "./config.ts";
import type { WorkerSessionSnapshot } from "./session.ts";
import type { WorkerTranscript } from "./types.ts";

/** Independent of ordinary Pi sessions: these logs are never resumable agents. */
export const WORKER_TRANSCRIPT_LIMIT = 64 * 1024 * 1024;
const END_RESERVE = 1024;
const EVENTS = new Set([
	"agent_start", "agent_end", "agent_settled", "turn_start", "turn_end",
	"message_start", "message_update", "message_end", "tool_execution_start", "tool_execution_update", "tool_execution_end",
	"queue_update", "entry_appended", "session_info_changed", "thinking_level_changed",
	"compaction_start", "compaction_end", "auto_retry_start", "auto_retry_end",
	"summarization_retry_scheduled", "summarization_retry_attempt_start", "summarization_retry_finished", "extension_error",
]);

/** Keep the captured active branch's display data, not its system prompt/tool declarations. */
function displayEntries(snapshot?: WorkerSessionSnapshot): any[] {
	const entries = snapshot?.branchJsonl ? snapshot.branchJsonl.split("\n").filter(Boolean).map((line) => JSON.parse(line)) : [];
	const removed = new Map<string, string | null>();
	let previous: string | null = null;
	for (const entry of entries) {
		if (entry.type === "message" && entry.message?.role === "system") removed.set(entry.id, previous);
		else previous = entry.id;
	}
	return entries.filter((entry) => !removed.has(entry.id)).map((entry) => {
		if (removed.has(entry.parentId)) entry.parentId = removed.get(entry.parentId);
		delete entry.systemMessage;
		if (removed.has(entry.firstKeptEntryId)) {
			const index = entries.findIndex((candidate) => candidate.id === entry.firstKeptEntryId);
			entry.firstKeptEntryId = entries.slice(index + 1).find((candidate) => !removed.has(candidate.id))?.id ?? entry.id;
		}
		return entry;
	});
}

/** Bounded synchronous appends keep stdout order without an unbounded promise queue.
 * No disk exceptions escape into Worker execution. Missing end => interrupted/incomplete.
 */
export class WorkerTranscriptWriter {
	private fd: number | undefined;
	private bytes = 0;
	private ended = false;
	private sawUser = false;
	private readonly timestamp = Date.now();
	readonly descriptor: WorkerTranscript;

	constructor(private readonly options: { cwd: string; prompt: string; snapshot?: WorkerSessionSnapshot; root?: string; maxBytes?: number }) {
		this.descriptor = { version: 1, id: randomUUID() };
		try {
			const root = path.join(options.root ?? path.dirname(agentDir()), "worker-transcripts");
			fs.mkdirSync(root, { recursive: true, mode: 0o700 });
			const stat = fs.lstatSync(root);
			if (!stat.isDirectory() || stat.isSymbolicLink()) throw new Error("unsafe transcript directory");
			fs.chmodSync(root, 0o700);
			this.fd = fs.openSync(path.join(root, `${this.descriptor.id}.jsonl`), fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | fs.constants.O_NOFOLLOW, 0o600);
			const header = { type: "worker_transcript_header", version: 1, id: this.descriptor.id, cwd: options.cwd, entries: displayEntries(options.snapshot) };
			if (!this.append(header)) {
				// An oversized inherited branch must not silently become an empty conversation.
				this.writeRaw({ type: "worker_transcript_header", version: 1, id: this.descriptor.id, cwd: options.cwd, entries: [] });
				this.incomplete("size_limit");
			}
		} catch {
			this.incomplete("io_error");
		}
	}

	private get limit(): number { return Math.max(4096, this.options.maxBytes ?? WORKER_TRANSCRIPT_LIMIT); }
	private writeRaw(value: unknown): void {
		if (this.fd === undefined) throw new Error("transcript unavailable");
		const buffer = Buffer.from(`${JSON.stringify(value)}\n`);
		let offset = 0;
		while (offset < buffer.length) {
			const wrote = fs.writeSync(this.fd, buffer, offset, buffer.length - offset);
			if (!wrote) throw new Error("short transcript write");
			offset += wrote;
		}
		this.bytes += buffer.length;
	}
	private append(value: unknown): boolean {
		if (this.bytes + Buffer.byteLength(JSON.stringify(value)) + 1 > this.limit - END_RESERVE) return false;
		this.writeRaw(value);
		return true;
	}
	private close(): void {
		if (this.fd === undefined) return;
		const fd = this.fd;
		this.fd = undefined;
		try { fs.closeSync(fd); } catch { this.descriptor.incomplete = true; this.descriptor.error = "io_error"; }
	}
	incomplete(reason: "size_limit" | "io_error" | "protocol_error"): void {
		this.descriptor.incomplete = true;
		this.descriptor.error = reason;
		if (this.ended) return;
		this.ended = true;
		try { this.writeRaw({ type: "worker_transcript_end", status: "incomplete", complete: false, error: reason }); } catch { /* descriptor remains authoritative when disk is unavailable */ }
		this.close();
	}
	private ensureUser(): void {
		if (this.sawUser || this.ended) return;
		this.sawUser = true;
		// Standard SDK emits user events; fallback covers older/aborted launchers.
		const message = { role: "user", content: this.options.prompt, timestamp: this.timestamp };
		for (const type of ["message_start", "message_end"]) if (!this.append({ type, message })) { this.incomplete("size_limit"); break; }
	}

	record(event: Record<string, any>): void {
		if (this.ended || !EVENTS.has(event.type)) return;
		try {
			if (event.message?.role === "system" || event.entry?.message?.role === "system") return;
			if (event.message?.role === "user") this.sawUser = true;
			if (event.type.startsWith("message_") || event.type.startsWith("tool_execution_")) this.ensureUser();
			let value = event;
			if (Array.isArray(event.messages)) value = { ...value, messages: event.messages.filter((message: any) => message?.role !== "system") };
			if (event.result?.systemMessage) { const { systemMessage: _omit, ...result } = event.result; value = { ...value, result }; }
			if (event.entry?.systemMessage) { const { systemMessage: _omit, ...entry } = event.entry; value = { ...value, entry }; }
			if (!this.append(value)) this.incomplete("size_limit");
		} catch { this.incomplete("io_error"); }
	}

	finish(status: "completed" | "failed" | "cancelled" | "timed_out"): void {
		if (this.ended) return;
		try {
			this.ensureUser();
			if (this.ended) return;
			this.writeRaw({ type: "worker_transcript_end", status, complete: true });
			fs.fsyncSync(this.fd!);
			this.ended = true;
			this.close();
		} catch { this.incomplete("io_error"); }
	}
}
