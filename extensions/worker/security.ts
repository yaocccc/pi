import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import * as fs from "node:fs";
import * as path from "node:path";
import { resolveTaskCwd, WRITE_MODES } from "./config.ts";
import type { CommandResult, WorkerTask, WorkspaceSnapshot } from "./types.ts";

export const DEFAULT_GIT_COMMAND_TIMEOUT_MS = 30_000;
export const GIT_COMMAND_KILL_GRACE_MS = 1_000;

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

function isSameOrAncestor(ancestor: string, candidate: string): boolean {
	const relative = path.relative(ancestor, candidate);
	return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
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
		const pattern = rawPattern.trim().replaceAll("\\", "/").replace(/^\.\/+/, "").replace(/\/+$/, "") || ".";
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
	return isSameOrAncestor(left.root, right.root) || isSameOrAncestor(right.root, left.root);
}

export function workerTasksConflict(left: WorkerTask, right: WorkerTask, baseCwd: string): boolean {
	const leftWrites = WRITE_MODES.has(left.mode);
	const rightWrites = WRITE_MODES.has(right.mode);
	if (!leftWrites && !rightWrites) return false;
	// Read tasks do not declare a complete read set, so no read/write independence
	// can be proven within a shared worktree.
	if (leftWrites !== rightWrites) return true;
	const leftBoundary = declaredWriteBoundary(left, baseCwd);
	const rightBoundary = declaredWriteBoundary(right, baseCwd);
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

export interface ConflictExecutionContext {
	/** Mutable for the lifetime of the task so earlier starters see later overlap. */
	readonly overlappingIndices: ReadonlySet<number>;
}

/** Ordered, failure-isolated scheduler that scans past a conflicting queue head. */
export async function mapWithConflicts<T, R>(
	items: T[],
	limit: number,
	conflicts: (left: T, right: T) => boolean,
	fn: (item: T, index: number, execution: ConflictExecutionContext) => Promise<R>,
): Promise<R[]> {
	if (items.length === 0) return [];
	const results = new Array<R>(items.length);
	const pending = items.map(() => true);
	const active = new Set<number>();
	const contexts = items.map(() => ({ overlappingIndices: new Set<number>() }));
	let remaining = items.length;
	const concurrency = Math.max(1, Math.min(Math.floor(limit) || 1, items.length));
	return await new Promise<R[]>((resolve) => {
		const schedule = () => {
			if (remaining === 0) { resolve(results); return; }
			while (active.size < concurrency) {
				let selected = -1;
				for (let index = 0; index < items.length; index++) {
					if (!pending[index]) continue;
					const compatibleWithActive = [...active].every((running) => !conflicts(items[index], items[running]));
					const preservesConflictOrder = pending.slice(0, index).every((isPending, earlier) =>
						!isPending || !conflicts(items[index], items[earlier]));
					if (compatibleWithActive && preservesConflictOrder) { selected = index; break; }
				}
				if (selected < 0) return;
				pending[selected] = false;
				for (const running of active) {
					contexts[selected].overlappingIndices.add(running);
					contexts[running].overlappingIndices.add(selected);
				}
				active.add(selected);
				let execution: Promise<R>;
				try { execution = Promise.resolve(fn(items[selected], selected, contexts[selected])); }
				catch (error) { execution = Promise.reject(error); }
				execution
					.then((result) => { results[selected] = result; })
					.catch((error) => {
						results[selected] = { status: "failed", summary: [error instanceof Error ? error.message : String(error)] } as R;
					})
					.finally(() => {
						active.delete(selected);
						remaining--;
						schedule();
					});
			}
		};
		schedule();
	});
}

export async function runCommand(command: string, args: string[], cwd: string, options: RunCommandOptions = {}): Promise<CommandResult> {
	const maxBytes = Math.max(1, options.maxBytes ?? 4 * 1024 * 1024);
	const timeoutMs = Math.max(1, options.timeoutMs ?? DEFAULT_GIT_COMMAND_TIMEOUT_MS);
	throwIfAborted(options.signal, `命令 ${command} `);
	return await new Promise((resolve, reject) => {
		const child = spawn(command, args, { cwd, shell: false, stdio: ["ignore", "pipe", "pipe"] });
		const stdout: Buffer[] = [];
		const stderr: Buffer[] = [];
		let stdoutBytes = 0;
		let stderrBytes = 0;
		let settled = false;
		let killTimer: NodeJS.Timeout | undefined;
		let timeout: NodeJS.Timeout | undefined;
		const cleanup = () => {
			if (timeout) clearTimeout(timeout);
			options.signal?.removeEventListener("abort", abortHandler);
		};
		const fail = (error: Error) => {
			if (settled) return;
			settled = true;
			cleanup();
			reject(error);
		};
		const terminate = (error: Error) => {
			if (settled) return;
			try { child.kill("SIGTERM"); } catch { /* process already exited */ }
			killTimer = setTimeout(() => {
				try { child.kill("SIGKILL"); } catch { /* process already exited */ }
			}, GIT_COMMAND_KILL_GRACE_MS);
			killTimer.unref();
			fail(error);
		};
		const abortHandler = () => terminate(new Error(`命令 ${command} 已取消`));
		child.stdout.on("data", (chunk: Buffer) => {
			const length = Math.min(chunk.length, Math.max(0, maxBytes - stdoutBytes));
			if (length > 0) stdout.push(chunk.subarray(0, length));
			stdoutBytes += length;
		});
		child.stderr.on("data", (chunk: Buffer) => {
			const length = Math.min(chunk.length, Math.max(0, maxBytes - stderrBytes));
			if (length > 0) stderr.push(chunk.subarray(0, length));
			stderrBytes += length;
		});
		(child as any).on("error", (error: Error) => {
			if (killTimer) clearTimeout(killTimer);
			fail(error);
		});
		(child as any).on("close", (code: number | null) => {
			if (killTimer) clearTimeout(killTimer);
			if (settled) return;
			settled = true;
			cleanup();
			resolve({ code: code ?? 1, stdout: Buffer.concat(stdout), stderr: Buffer.concat(stderr) });
		});
		options.signal?.addEventListener("abort", abortHandler, { once: true });
		timeout = setTimeout(() => terminate(new Error(`命令 ${command} 超时（${timeoutMs}ms）`)), timeoutMs);
		timeout.unref();
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
	const results = await Promise.all(commands.map((args) => runCommand("git", args, gitRoot, { signal })));
	return new Set(results.flatMap((result) => nulPaths(result.stdout)));
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
