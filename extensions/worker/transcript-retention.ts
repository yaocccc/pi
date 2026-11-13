import * as fs from "node:fs";
import * as path from "node:path";

export const WORKER_TRANSCRIPT_BUDGET = 10 * 1024 ** 3;
export const WORKER_TRANSCRIPT_CHECK_INTERVAL = 24 * 60 * 60 * 1000;
const UUID = "[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}";
const LOG = new RegExp(`^(${UUID})\\.jsonl$`);
const LEASE = new RegExp(`^(${UUID})\\.active$`);
const CHECK = ".retention.lock";
const NOFOLLOW = fs.constants.O_NOFOLLOW ?? 0;
type Owner = { version: 1; pid: number; start?: string; finished?: boolean };

// PID alone is insufficient after reuse or reboot. On Linux bind it to boot ID
// and process start ticks. Elsewhere a live/reused PID is conservatively protected.
function processStart(pid: number): string | undefined {
	if (process.platform !== "linux") return undefined;
	try {
		const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
		const fields = stat.slice(stat.lastIndexOf(")") + 2).split(" ");
		const boot = fs.readFileSync("/proc/sys/kernel/random/boot_id", "utf8").trim();
		return `${boot}:${fields[19]}`;
	} catch { return undefined; }
}
function ownerAlive(owner: Owner): boolean {
	try { process.kill(owner.pid, 0); }
	catch (error) { return (error as NodeJS.ErrnoException).code !== "ESRCH"; }
	const start = processStart(owner.pid);
	return !owner.start || !start || owner.start === start;
}
function sameFile(a: fs.Stats, b: fs.Stats): boolean { return a.dev === b.dev && a.ino === b.ino; }
function safeRoot(root: string, expected?: fs.Stats): fs.Stats {
	const stat = fs.lstatSync(root);
	if (!stat.isDirectory() || stat.isSymbolicLink() || (expected && !sameFile(stat, expected))) throw new Error("unsafe transcript root");
	return stat;
}
function readSmall(file: string): string {
	const fd = fs.openSync(file, fs.constants.O_RDONLY | NOFOLLOW);
	try {
		const stat = fs.fstatSync(fd);
		if (!stat.isFile() || stat.size > 4096) throw new Error("invalid retention metadata");
		return fs.readFileSync(fd, "utf8");
	} finally { fs.closeSync(fd); }
}

/** Publish before creating the log, remove only when execution finishes (not
 * when recording stops at its size cap). A dead owner's lease can be reclaimed.
 */
export function holdTranscript(root: string, id: string): () => void {
	if (!new RegExp(`^${UUID}$`).test(id)) throw new Error("invalid transcript ID");
	const directory = safeRoot(root);
	const file = path.join(root, `${id}.active`);
	const owner: Owner = { version: 1, pid: process.pid, start: processStart(process.pid) };
	const fd = fs.openSync(file, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | NOFOLLOW, 0o600);
	let stat: fs.Stats;
	try { fs.writeFileSync(fd, JSON.stringify(owner)); stat = fs.fstatSync(fd); }
	finally { fs.closeSync(fd); }
	return () => {
		try {
			safeRoot(root, directory);
			const current = fs.lstatSync(file);
			if (!current.isFile() || !sameFile(current, stat)) return;
			if (terminalLog(path.join(root, `${id}.jsonl`))) fs.unlinkSync(file);
			else {
				// A final disk error may prevent the terminal record. Preserve an
				// explicit finished lease so GC need not confuse this with a legacy
				// unmarked live log, even while the hosting Pi process stays alive.
				const fd = fs.openSync(file, fs.constants.O_WRONLY | NOFOLLOW);
				try {
					if (!sameFile(fs.fstatSync(fd), stat)) return;
					fs.ftruncateSync(fd, 0);
					fs.writeFileSync(fd, JSON.stringify({ ...owner, finished: true }));
				} finally { fs.closeSync(fd); }
			}
		} catch { /* best effort; process identity still permits crash recovery */ }
	};
}

function leaseState(file: string): "active" | "dead" | "absent" {
	try {
		const stat = fs.lstatSync(file);
		if (!stat.isFile()) return "active"; // Do not follow links or uncertain metadata.
		const owner = JSON.parse(readSmall(file)) as Owner;
		if (owner.version !== 1 || !Number.isSafeInteger(owner.pid) || owner.pid <= 0 || (owner.start !== undefined && typeof owner.start !== "string")) return "active";
		return owner.finished === true || !ownerAlive(owner) ? "dead" : "active";
	} catch (error) { return (error as NodeJS.ErrnoException).code === "ENOENT" ? "absent" : "active"; }
}

// Older writers have no active marker. Only reclaim their terminal logs; a
// missing/partial terminal is ambiguous and must not delete a legacy live task.
function terminalLog(file: string): boolean {
	let fd: number | undefined;
	try {
		fd = fs.openSync(file, fs.constants.O_RDONLY | NOFOLLOW);
		const stat = fs.fstatSync(fd);
		if (!stat.isFile()) return false;
		const tail = Buffer.alloc(Math.min(stat.size, 4096));
		fs.readSync(fd, tail, 0, tail.length, stat.size - tail.length);
		const text = tail.toString("utf8");
		if (!text.endsWith("\n")) return false;
		const lines = text.trimEnd().split("\n");
		const last = JSON.parse(lines.at(-1) ?? "{}");
		// Legacy size-limit markers can precede execution completion. Without a
		// lease we cannot distinguish those from still-running older writers.
		return last.type === "worker_transcript_end" && last.complete === true;
	} catch { return false; }
	finally { if (fd !== undefined) fs.closeSync(fd); }
}

/** A single persistent lock's mtime records the last attempted scan. Keep it
 * even after failure; a later lifecycle event may replace it after 24 hours.
 * Expired-lock replacement is best effort, not a distributed locking protocol.
 * Recheck its identity before deleting logs if another collector took over.
 */
export async function cleanupTranscripts(root: string, options: { now?: number; maxBytes?: number; intervalMs?: number } = {}): Promise<void> {
	try {
		const directory = safeRoot(root);
		const now = options.now ?? Date.now();
		const interval = options.intervalMs ?? WORKER_TRANSCRIPT_CHECK_INTERVAL;
		const budget = options.maxBytes ?? WORKER_TRANSCRIPT_BUDGET;
		if (!Number.isFinite(now) || now < 0 || !Number.isFinite(interval) || interval <= 0 || !Number.isFinite(budget) || budget < 0) return;
		const claim = path.join(root, CHECK);
		try {
			const previous = fs.lstatSync(claim);
			if (!previous.isFile() || now - previous.mtimeMs < interval) return;
			const current = fs.lstatSync(claim);
			if (!sameFile(previous, current) || current.mtimeMs !== previous.mtimeMs) return;
			fs.unlinkSync(claim);
		} catch (error) { if ((error as NodeJS.ErrnoException).code !== "ENOENT") return; }
		const fd = fs.openSync(claim, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_EXCL | NOFOLLOW, 0o600);
		let claimed: fs.Stats;
		try {
			fs.futimesSync(fd, now / 1000, now / 1000);
			claimed = fs.fstatSync(fd);
		} finally { fs.closeSync(fd); }
		const names = await fs.promises.readdir(root);
		const logs: { file: string; id: string; stat: fs.Stats }[] = [];
		let total = 0;
		for (const name of names) {
			safeRoot(root, directory);
			const match = LOG.exec(name);
			if (match) {
				try {
					const file = path.join(root, name);
					const stat = await fs.promises.lstat(file);
					if (stat.isFile()) { logs.push({ file, id: match[1], stat }); total += stat.size; }
				} catch { /* a concurrent cleanup/manual removal may win */ }
			} else {
				const orphan = LEASE.exec(name);
				if (orphan && leaseState(path.join(root, name)) === "dead") {
					try { fs.lstatSync(path.join(root, `${orphan[1]}.jsonl`)); }
					catch (error) {
						if ((error as NodeJS.ErrnoException).code === "ENOENT") {
						try { fs.unlinkSync(path.join(root, name)); } catch { /* orphan lease */ }
						}
					}
				}

			}
		}
		if (total <= budget) return;
		logs.sort((a, b) => a.stat.mtimeMs - b.stat.mtimeMs || a.id.localeCompare(b.id));
		for (const [index, log] of logs.entries()) {
			if (total <= budget) break;
			if (index % 32 === 0) await new Promise<void>((resolve) => setImmediate(resolve));
			safeRoot(root, directory);
			if (!sameFile(fs.lstatSync(claim), claimed)) return;
			const lease = path.join(root, `${log.id}.active`);
			const state = leaseState(lease);
			if (state === "active" || (state === "absent" && !terminalLog(log.file))) continue;
			try {
				// Recheck before unlink: never delete a file changed since scanning.
				const current = fs.lstatSync(log.file);
				if (!current.isFile() || !sameFile(log.stat, current) || current.size !== log.stat.size || current.mtimeMs !== log.stat.mtimeMs || leaseState(lease) === "active") continue;
				fs.unlinkSync(log.file);
				total -= log.stat.size;
				if (state === "dead") { try { if (fs.lstatSync(lease).isFile()) fs.unlinkSync(lease); } catch { /* best effort */ } }
			} catch { /* no retention failure may fail a task */ }
		}
	} catch { /* unsafe/unavailable storage, claimed interval, or IO failure: retry later */ }
}

// No per-token work, recurring timers, or unbounded cleanup queue. Filesystem
// enumeration runs asynchronously outside the stdout handler, at lifecycle edges.
const scheduled = new Map<string, number>();
const running = new Set<string>();
export function scheduleTranscriptCleanup(root: string): void {
	const now = Date.now();
	if (running.has(root) || now < (scheduled.get(root) ?? 0)) return;
	scheduled.set(root, now + WORKER_TRANSCRIPT_CHECK_INTERVAL);
	running.add(root);
	setImmediate(() => { void cleanupTranscripts(root).finally(() => running.delete(root)); });
}
