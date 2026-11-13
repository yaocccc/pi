import * as fs from "node:fs";
import * as path from "node:path";
import { randomUUID } from "node:crypto";

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const LIMIT = 65 << 20;
const CHUNK = 4 << 20;
const MAX_CURSORS = 32;
const MAX_AGE = 5 * 60_000;

export interface TranscriptReadRequest {
	descriptor: { version: 1; id: string };
	/** Opaque, backend-owned continuation token. No paths or byte offsets. */
	cursor?: string;
}
export interface TranscriptReadResult {
	cursor: string;
	reset: boolean;
	records: Record<string, unknown>[];
	more: boolean;
	partial: boolean;
}
interface Position {
	id: string; root: string; device: number; inode: number;
	offset: number; tail: Buffer; anchor: Buffer; header: boolean; primary: boolean; touched: number;
}
export class TranscriptReadError extends Error {
	constructor(readonly code: string, message: string) { super(message); this.name = "TranscriptReadError"; }
}
const invalid = (message = "Invalid Worker transcript request") => new TranscriptReadError("invalid-transcript", message);
const sameFile = (left: fs.Stats, right: fs.Stats) => left.dev === right.dev && left.ino === right.ino;

/** Owns filesystem knowledge and bounded continuations. A frontend sees only
 * records and opaque tokens. Tokens are retryable, expire, and reset after reload.
 * No file descriptors or asynchronous jobs survive a read. */
export class WorkerTranscriptReader {
	private readonly positions = new Map<string, Position>();
	constructor(private readonly agentDirectory: () => string) {}
	clear(): void { this.positions.clear(); }

	read(request: unknown): TranscriptReadResult {
		const value = request as TranscriptReadRequest;
		if (!value || typeof value !== "object" || value.descriptor?.version !== 1 || typeof value.descriptor.id !== "string" || !ID.test(value.descriptor.id)
			|| (value.cursor !== undefined && (typeof value.cursor !== "string" || !ID.test(value.cursor)))) throw invalid();
		const now = Date.now();
		for (const [key, position] of this.positions) if (now - position.touched > MAX_AGE) this.positions.delete(key);
		const previous = value.cursor ? this.positions.get(value.cursor) : undefined;
		if (previous && previous.id !== value.descriptor.id) throw invalid("Cursor belongs to another Worker transcript");
		const agent = path.resolve(this.agentDirectory());
		const primaryRoot = path.join(path.dirname(agent), "worker-transcripts");
		let opened: ReturnType<typeof openTranscript>;
		let primary = true;
		try {
			try { opened = openTranscript(primaryRoot, value.descriptor.id); }
			catch (error) {
				// Only absence permits legacy lookup. Never bypass an unsafe new file,
				// or return to an older copy after this cursor reached the new location.
				if ((error as NodeJS.ErrnoException).code !== "ENOENT" || previous?.primary || (value.cursor && !previous)) throw error;
				primary = false;
				opened = openTranscript(path.join(agent, "worker-transcripts"), value.descriptor.id);
			}
		} catch (error) { throw readError(error); }
		try {
			const { fd, stat, root } = opened;
			if (stat.size > LIMIT) throw invalid("Worker transcript exceeds recording limit");
			let reset = !previous || previous.root !== root || previous.device !== stat.dev || previous.inode !== stat.ino || stat.size < previous.offset;
			if (!reset && previous!.anchor.length) {
				const anchor = Buffer.alloc(previous!.anchor.length);
				const count = fs.readSync(fd, anchor, 0, anchor.length, previous!.offset - anchor.length);
				reset = count !== anchor.length || !anchor.equals(previous!.anchor);
			}
			let position: Position = reset
				? { id: value.descriptor.id, root, device: stat.dev, inode: stat.ino, offset: 0, tail: Buffer.alloc(0), anchor: Buffer.alloc(0), header: false, primary, touched: now }
				: { ...previous!, primary, touched: now };
			const data = Buffer.alloc(Math.min(CHUNK, Math.max(0, stat.size - position.offset)));
			const count = fs.readSync(fd, data, 0, data.length, position.offset);
			const bytes = data.subarray(0, count);
			position.offset += count;
			if (count) position.anchor = Buffer.from(Buffer.concat([position.anchor, bytes]).subarray(-64));
			const pending = Buffer.concat([position.tail, bytes]);
			if (pending.length > LIMIT) throw invalid("Worker transcript record exceeds limit");
			const records: Record<string, unknown>[] = [];
			let start = 0;
			while (true) {
				const end = pending.indexOf(10, start);
				if (end < 0) break;
				let record: Record<string, unknown>;
				try { record = JSON.parse(pending.subarray(start, end).toString("utf8")); }
				catch { throw invalid("Invalid Worker transcript record"); }
				if (!record || typeof record !== "object" || typeof record.type !== "string" || !record.type) throw invalid("Invalid Worker transcript record");
				if (!position.header) {
					if (record.type !== "worker_transcript_header" || record.version !== 1 || record.id !== value.descriptor.id || !Array.isArray(record.entries)) throw invalid("Invalid Worker transcript header");
					position.header = true;
				} else if (record.type === "worker_transcript_header") throw invalid("Unexpected Worker transcript header");
				records.push(record);
				start = end + 1;
			}
			// Leave room for the standard RPC envelope below its 64 MiB JSONL cap.
			if (Buffer.byteLength(JSON.stringify(records), "utf8") > (64 << 20) - 1024) throw invalid("Worker transcript response exceeds transport limit");
			position.tail = Buffer.from(pending.subarray(start));
			const cursor = randomUUID();
			this.positions.set(cursor, position);
			// Bound both count and retained partial-line storage. Evicted cursors
			// safely restart in primary storage only: lost provenance must never
			// permit falling back from a removed primary to an obsolete legacy log.
			let retained = [...this.positions.values()].reduce((sum, p) => sum + p.tail.length, 0);
			for (const [key, p] of this.positions) {
				if (this.positions.size <= MAX_CURSORS && retained <= LIMIT) break;
				this.positions.delete(key); retained -= p.tail.length;
			}
			return { cursor, reset, records, more: position.offset < stat.size, partial: position.tail.length > 0 };
		} catch (error) { throw readError(error); }
		finally { fs.closeSync(opened.fd); }
	}
}

function openTranscript(root: string, id: string) {
	const directory = fs.lstatSync(root);
	if (!directory.isDirectory() || directory.isSymbolicLink()) throw invalid("Unsafe Worker transcript directory");
	const name = path.join(root, `${id}.jsonl`);
	const before = fs.lstatSync(name);
	if (!before.isFile() || before.isSymbolicLink() || before.nlink !== 1) throw invalid("Worker transcript is not a private regular file");
	const fd = fs.openSync(name, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW | fs.constants.O_NONBLOCK);
	try {
		const stat = fs.fstatSync(fd);
		const after = fs.lstatSync(root);
		if (!sameFile(before, stat) || !stat.isFile() || stat.nlink !== 1 || !sameFile(directory, after) || !after.isDirectory() || after.isSymbolicLink()) throw invalid("Worker transcript location changed");
		return { fd, stat, root };
	} catch (error) { fs.closeSync(fd); throw error; }
}
function readError(error: unknown): TranscriptReadError {
	if (error instanceof TranscriptReadError) return error;
	const code = (error as NodeJS.ErrnoException)?.code;
	if (code === "ENOENT") return new TranscriptReadError("transcript-not-found", "Worker transcript is unavailable or expired");
	if (code === "EACCES" || code === "EPERM") return new TranscriptReadError("transcript-permission", "Worker transcript access denied");
	return new TranscriptReadError("transcript-read-failed", "Worker transcript read failed");
}
