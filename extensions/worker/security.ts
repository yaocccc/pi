import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import * as fs from "node:fs";
import * as path from "node:path";
import { WRITE_MODES } from "./config";
import type { CommandResult, WorkerTask, WorkspaceSnapshot } from "./types";

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

export function batchRequiresSerial(tasks: WorkerTask[]): boolean {
	// All tasks observe the same Git worktree. A concurrent writer can otherwise
	// be attributed to a read-only sibling or race another writer's snapshot.
	return tasks.length > 1 && tasks.some((task) => WRITE_MODES.has(task.mode));
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
