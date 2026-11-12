import { spawn, type ChildProcess } from "node:child_process";
import { createHash } from "node:crypto";
import * as fs from "node:fs";
import * as path from "node:path";
import { isPathInside, resolveTaskCwd, WRITE_MODES } from "./config.ts";
import type { CommandResult, WorkerTask, WorkspaceSnapshot } from "./types.ts";

export const DEFAULT_GIT_COMMAND_TIMEOUT_MS = 30_000;
export const GIT_COMMAND_KILL_GRACE_MS = 1_000;

// Auxiliary commands also use detached groups. Retain an exit fallback across
// extension reloads, without tying commands from one session to another owner.
const commandsKey = Symbol.for("pi.worker.commandChildren.v1");
const commandGlobals = globalThis as typeof globalThis & { [commandsKey]?: Set<ChildProcess> };
if (!commandGlobals[commandsKey]) {
	const children = commandGlobals[commandsKey] = new Set<ChildProcess>();
	process.once("exit", () => { for (const child of children) killProcessTree(child, "SIGKILL"); });
}
const commandChildren = commandGlobals[commandsKey];

export interface RunCommandOptions {
	maxBytes?: number;
	signal?: AbortSignal;
	timeoutMs?: number;
}

function throwIfAborted(signal: AbortSignal | undefined, phase: string): void {
	if (signal?.aborted) throw new Error(`${phase}已取消`);
}

export function globToRegExp(pattern: string): RegExp {
	let result = "^";
	const normalized = pattern.replaceAll("\\", "/").replace(/^\.\//, "");
	for (let i = 0; i < normalized.length; i++) {
		const char = normalized[i];
		if (char === "*") {
			if (normalized[i + 1] === "*") {
				i++;
				if (normalized[i + 1] === "/") { i++; result += "(?:.*/)?"; }
				else result += ".*";
			} else result += "[^/]*";
		} else if (char === "?") result += "[^/]";
		else result += char.replace(/[.+^${}()|[\]\\]/g, "\\$&");
	}
	return new RegExp(`${result}$`);
}

export function matchesAny(file: string, patterns: string[]): boolean {
	const normalized = file.replaceAll("\\", "/").replace(/^\.\//, "");
	return patterns.some((pattern) => globToRegExp(pattern).test(normalized));
}

export interface WriteBoundary {
	unknown: boolean;
	ranges: Array<{ root: string; kind: "exact" | "subtree"; relativePattern?: string }>;
}

function pathExists(filePath: string): boolean {
	try { fs.lstatSync(filePath); return true; }
	catch { return false; }
}

/** Resolve symlinks in the longest existing prefix while retaining a not-yet-created suffix. */
export function canonicalizePotentialPath(filePath: string): string | null {
	const absolute = path.resolve(filePath);
	let ancestor = absolute;
	while (!pathExists(ancestor)) {
		const parent = path.dirname(ancestor);
		if (parent === ancestor) return null;
		ancestor = parent;
	}
	try {
		return path.resolve(fs.realpathSync(ancestor), path.relative(ancestor, absolute));
	} catch {
		// A dangling symlink or any other realpath failure makes independence
		// unprovable. Do not fall back to a lexical path and risk a false negative.
		return null;
	}
}

/**
 * Convert allowedPaths to a conservative workspace-coordinate boundary.
 * A root-level or unsupported glob is deliberately unknown and conflicts with every writer.
 */
export function declaredWriteBoundary(task: WorkerTask, baseCwd: string): WriteBoundary {
	if (!WRITE_MODES.has(task.mode) || !task.allowedPaths?.length) return { unknown: true, ranges: [] };
	let cwd: string;
	try { cwd = resolveTaskCwd(baseCwd, task.cwd); }
	catch { return { unknown: true, ranges: [] }; }
	const ranges: WriteBoundary["ranges"] = [];
	for (const rawPattern of task.allowedPaths) {
		const pattern = rawPattern.replaceAll("\\", "/").replace(/^\.\/+/, "").replace(/\/+$/, "") || ".";
		// globToRegExp only has defined semantics for * and ?. Treat other common
		// glob syntax as unknown rather than claiming two tasks are independent.
		if (/[\[\]{}]/.test(pattern)) return { unknown: true, ranges: [] };
		const segments = pattern.split("/");
		const wildcardIndex = segments.findIndex((segment) => /[*?]/.test(segment));
		if (wildcardIndex === 0) return { unknown: true, ranges: [] };
		if (wildcardIndex > 0) {
			const root = canonicalizePotentialPath(path.resolve(cwd, ...segments.slice(0, wildcardIndex)));
			if (!root) return { unknown: true, ranges: [] };
			ranges.push({ root, kind: "subtree", relativePattern: segments.slice(wildcardIndex).join("/") });
			continue;
		}
		const root = canonicalizePotentialPath(path.resolve(cwd, pattern));
		if (!root) return { unknown: true, ranges: [] };
		// This deliberately matches the write guard: a literal such as "src" only
		// matches that exact path. Callers must declare "src/**" for its subtree.
		ranges.push({ root, kind: "exact" });
	}
	return { unknown: false, ranges };
}

function rangesOverlap(left: WriteBoundary["ranges"][number], right: WriteBoundary["ranges"][number]): boolean {
	// Scheduling is deliberately stricter than exact guard matching: changing an
	// ancestor node can affect a declared descendant even when the literal path
	// itself does not grant write access to the descendant.
	return isPathInside(left.root, right.root) || isPathInside(right.root, left.root);
}

/** Cross-batch tasks may have different session cwd snapshots. */
export function workerTaskScopesConflict(left: WorkerTask, leftCwd: string, right: WorkerTask, rightCwd: string): boolean {
	const leftWrites = WRITE_MODES.has(left.mode);
	const rightWrites = WRITE_MODES.has(right.mode);
	if (!leftWrites && !rightWrites) return false;
	// Read tasks do not declare a complete read set, so no read/write independence
	// can be proven within a shared worktree.
	if (leftWrites !== rightWrites) return true;
	const leftBoundary = declaredWriteBoundary(left, leftCwd);
	const rightBoundary = declaredWriteBoundary(right, rightCwd);
	if (leftBoundary.unknown || rightBoundary.unknown) return true;
	return leftBoundary.ranges.some((leftRange) => rightBoundary.ranges.some((rightRange) => rangesOverlap(leftRange, rightRange)));
}

export function filterChangedFilesToBoundary(task: WorkerTask, baseCwd: string, changedFiles: string[]): string[] {
	const boundary = declaredWriteBoundary(task, baseCwd);
	if (boundary.unknown) return [];
	let cwd: string;
	try { cwd = resolveTaskCwd(baseCwd, task.cwd); }
	catch { return []; }
	return changedFiles.filter((file) => {
		const candidate = canonicalizePotentialPath(path.resolve(cwd, file));
		if (!candidate) return false;
		return boundary.ranges.some((range) => {
			if (range.kind === "exact") return range.root === candidate;
			const relative = path.relative(range.root, candidate).replaceAll("\\", "/");
			if (!relative || relative.startsWith("../") || path.isAbsolute(relative)) return false;
			return matchesAny(relative, [range.relativePattern!]);
		});
	});
}

export interface ChangedFilesAttribution {
	changedFiles: string[];
	observedChangedFiles: string[];
	attributedToDeclaredPaths: boolean;
}

/** Keep the raw snapshot observation even when concurrent attribution is needed. */
export function attributeChangedFiles(
	task: WorkerTask,
	baseCwd: string,
	observedChangedFiles: string[],
	hadConcurrentWriter: boolean,
): ChangedFilesAttribution {
	const observed = [...observedChangedFiles];
	return {
		changedFiles: hadConcurrentWriter ? filterChangedFilesToBoundary(task, baseCwd, observed) : [...observed],
		observedChangedFiles: observed,
		attributedToDeclaredPaths: hadConcurrentWriter,
	};
}

export function killProcessTree(child: ChildProcess, signal: NodeJS.Signals): void {
	if (!child.pid) return;
	try {
		if (process.platform !== "win32") process.kill(-child.pid, signal);
		else child.kill(signal);
	} catch {
		try { child.kill(signal); } catch { /* already exited */ }
	}
}

/** false means no runnable members; unknown must retain the original kill deadline. */
export function processGroupActive(pid: number): boolean | undefined {
	try { process.kill(-pid, 0); }
	catch (error) { return (error as NodeJS.ErrnoException).code === "ESRCH" ? false : undefined; }
	if (process.platform !== "linux") return undefined;
	// kill(0) also sees orphan zombies. They cannot execute and may never be reaped
	// by a container's PID 1, so do not wait for them indefinitely.
	try {
		let sawZombie = false;
		for (const entry of fs.readdirSync("/proc")) {
			if (!/^\d+$/.test(entry)) continue;
			let stat: string;
			try { stat = fs.readFileSync(`/proc/${entry}/stat`, "utf8"); }
			catch (error) {
				if ((error as NodeJS.ErrnoException).code === "ENOENT") continue;
				return undefined;
			}
			const end = stat.lastIndexOf(") ");
			const fields = stat.slice(end + 2).split(" ");
			if (end < 0 || !/^\d+$/.test(fields[2] ?? "")) return undefined;
			if (Number(fields[2]) !== pid) continue;
			if (!["Z", "X"].includes(fields[0])) return true;
			sawZombie = true;
		}
		// A group seen by kill(0) but absent from this scan is not confirmed empty.
		return sawZombie ? false : undefined;
	} catch { return undefined; }
}

export async function runCommand(command: string, args: string[], cwd: string, options: RunCommandOptions = {}): Promise<CommandResult> {
	const maxBytes = Math.max(1, options.maxBytes ?? 4 * 1024 * 1024);
	const timeoutMs = Math.max(1, options.timeoutMs ?? DEFAULT_GIT_COMMAND_TIMEOUT_MS);
	throwIfAborted(options.signal, `命令 ${command} `);
	return await new Promise((resolve, reject) => {
		const child = spawn(command, args, { cwd, shell: false, detached: process.platform !== "win32", stdio: ["ignore", "pipe", "pipe"] });
		commandChildren.add(child);
		const stdout: Buffer[] = [];
		const stderr: Buffer[] = [];
		let stdoutBytes = 0;
		let stderrBytes = 0;
		let settled = false;
		let terminating = false;
		let terminationError: Error | undefined;
		let closedCode: number | undefined;
		let killTimer: NodeJS.Timeout | undefined;
		let forceTimer: NodeJS.Timeout | undefined;
		let groupTimer: NodeJS.Timeout | undefined;
		let timeout: NodeJS.Timeout | undefined;
		const finish = () => {
			if (settled) return;
			settled = true;
			clearTimeout(timeout);
			clearTimeout(killTimer);
			clearTimeout(forceTimer);
			clearTimeout(groupTimer);
			options.signal?.removeEventListener("abort", abortHandler);
			if (closedCode !== undefined || !child.pid) commandChildren.delete(child);
			if (terminationError) reject(terminationError);
			else resolve({ code: closedCode ?? 1, stdout: Buffer.concat(stdout), stderr: Buffer.concat(stderr) });
		};
		const checkClosedGroup = () => {
			if (settled || closedCode === undefined) return;
			if (process.platform === "win32" || !child.pid || processGroupActive(child.pid) === false) { finish(); return; }
			groupTimer = setTimeout(checkClosedGroup, 100);
		};
		const terminate = (error?: Error) => {
			if (settled) return;
			terminationError ??= error;
			if (terminating) return;
			terminating = true;
			killProcessTree(child, "SIGTERM");
			killTimer = setTimeout(() => {
				killProcessTree(child, "SIGKILL");
				forceTimer = setTimeout(() => {
					terminationError = new Error(`${terminationError?.message ?? `命令 ${command} 清理失败`} [cleanup=FORCE_SETTLE]`);
					child.stdout.destroy();
					child.stderr.destroy();
					child.unref();
					finish();
				}, GIT_COMMAND_KILL_GRACE_MS);
			}, GIT_COMMAND_KILL_GRACE_MS);
		};
		const abortHandler = () => terminate(new Error(`命令 ${command} 已取消`));
		const reapDescendants = () => {
			if (!settled && !terminating && process.platform !== "win32" && child.pid && processGroupActive(child.pid) !== false) terminate();
		};
		child.stdout.on("data", (chunk: Buffer) => {
			if (terminationError) return;
			// Callers parse stdout as complete evidence (including NUL-delimited
			// Git paths), so an incomplete result must never resolve successfully.
			if (chunk.length > maxBytes - stdoutBytes) {
				terminate(new Error(`命令 ${command} stdout 超出上限（${maxBytes} bytes）`));
				return;
			}
			stdout.push(chunk);
			stdoutBytes += chunk.length;
		});
		child.stderr.on("data", (chunk: Buffer) => {
			const length = Math.min(chunk.length, Math.max(0, maxBytes - stderrBytes));
			if (length > 0) stderr.push(chunk.subarray(0, length));
			stderrBytes += length;
		});
		child.on("error", (error: Error) => {
			if (child.pid) terminate(error);
			else { terminationError = error; finish(); }
		});
		child.on("exit", reapDescendants);
		child.on("close", (code: number | null) => {
			if (settled) { commandChildren.delete(child); return; }
			closedCode = code ?? 1;
			reapDescendants();
			checkClosedGroup();
		});
		options.signal?.addEventListener("abort", abortHandler, { once: true });
		timeout = setTimeout(() => terminate(new Error(`命令 ${command} 超时（${timeoutMs}ms）`)), timeoutMs);
		if (options.signal?.aborted) abortHandler();
	});
}

export function nulPaths(buffer: Buffer): string[] {
	return buffer.toString("utf8").split("\0").filter(Boolean);
}

export async function gitChangedPaths(gitRoot: string, signal?: AbortSignal): Promise<Set<string>> {
	const commands: string[][] = [
		["diff", "--name-only", "-z"],
		["diff", "--cached", "--name-only", "-z"],
		["ls-files", "--others", "--exclude-standard", "-z"],
	];
	// Do not release the task while a sibling command is still cleaning up.
	const results = await Promise.allSettled(commands.map((args) => runCommand("git", args, gitRoot, { signal })));
	return new Set(results.flatMap((result, index) => {
		if (result.status === "rejected") throw result.reason;
		if (result.value.code !== 0) throw new Error(`git ${commands[index].join(" ")} 失败（${result.value.code}）：${result.value.stderr.toString("utf8").slice(-2048)}`);
		return nulPaths(result.value.stdout);
	}));
}

export function fileHash(filePath: string): string | null {
	try {
		if (!fs.statSync(filePath).isFile()) return null;
		return createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
	} catch {
		return null;
	}
}

export async function indexHash(gitRoot: string, relativePath: string, signal?: AbortSignal): Promise<string | null> {
	const result = await runCommand("git", ["rev-parse", `:${relativePath}`], gitRoot, { maxBytes: 1024, signal });
	return result.code === 0 ? result.stdout.toString("utf8").trim() : null;
}

export async function snapshotWorkspace(cwd: string, includePaths: Set<string> = new Set(), signal?: AbortSignal): Promise<WorkspaceSnapshot> {
	throwIfAborted(signal, "工作区快照");
	const rootResult = await runCommand("git", ["rev-parse", "--show-toplevel"], cwd, { maxBytes: 4096, signal });
	if (rootResult.code !== 0) throw new Error("Worker 需要 Git 工作区以记录变更");
	const gitRoot = fs.realpathSync(rootResult.stdout.toString("utf8").trim());
	const statusPaths = await gitChangedPaths(gitRoot, signal);
	const paths = new Set([...statusPaths, ...includePaths]);
	const files = new Map<string, { worktree: string | null; index: string | null }>();
	for (const relativePath of paths) {
		throwIfAborted(signal, "工作区快照");
		files.set(relativePath, { worktree: fileHash(path.join(gitRoot, relativePath)), index: await indexHash(gitRoot, relativePath, signal) });
	}
	return { gitRoot, cwd, statusPaths, files };
}

export async function changedSince(before: WorkspaceSnapshot, signal?: AbortSignal): Promise<{ changed: string[]; after: WorkspaceSnapshot }> {
	throwIfAborted(signal, "工作区变更校验");
	const currentPaths = await gitChangedPaths(before.gitRoot, signal);
	const union = new Set([...before.statusPaths, ...currentPaths]);
	const after = await snapshotWorkspace(before.cwd, union, signal);
	const changed = new Set<string>();
	for (const relativePath of union) {
		throwIfAborted(signal, "工作区变更校验");
		const old = before.files.get(relativePath) ?? { worktree: fileHash(path.join(before.gitRoot, relativePath)), index: await indexHash(before.gitRoot, relativePath, signal) };
		const now = after.files.get(relativePath)!;
		if (old.worktree !== now.worktree || old.index !== now.index || before.statusPaths.has(relativePath) !== currentPaths.has(relativePath)) {
			changed.add(path.relative(before.cwd, path.join(before.gitRoot, relativePath)).replaceAll("\\", "/"));
		}
	}
	return { changed: [...changed].sort(), after };
}
