import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import * as fs from "node:fs";
import * as path from "node:path";
import { agentDir, isPathInside, WRITE_MODES } from "./config";
import type { CommandResult, WorkerTask, WorkspaceSnapshot } from "./types";

export const MAX_FS_SNAPSHOT_ENTRIES = 250_000;
export const MAX_IGNORED_HASH_FILE_BYTES = 1024 * 1024;
export const MAX_IGNORED_HASH_TOTAL_BYTES = 64 * 1024 * 1024;
export const AGENT_RUNTIME_SNAPSHOT_EXCLUDES = ["memory.md", "memory-index.md", "memories", "sessions"];

export function isTaskLocalPath(file: string): boolean {
	const normalized = file.replaceAll("\\", "/").replace(/^\.\//, "");
	return normalized !== ".." && !normalized.startsWith("../") && !path.isAbsolute(file);
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

export async function runCommand(command: string, args: string[], cwd: string, maxBytes = 4 * 1024 * 1024): Promise<CommandResult> {
	return await new Promise((resolve, reject) => {
		const child = spawn(command, args, { cwd, shell: false, stdio: ["ignore", "pipe", "pipe"] });
		const stdout: Buffer[] = [];
		const stderr: Buffer[] = [];
		let stdoutBytes = 0;
		let stderrBytes = 0;
		child.stdout.on("data", (chunk: Buffer) => { if (stdoutBytes < maxBytes) { stdout.push(chunk.subarray(0, maxBytes - stdoutBytes)); stdoutBytes += chunk.length; } });
		child.stderr.on("data", (chunk: Buffer) => { if (stderrBytes < maxBytes) { stderr.push(chunk.subarray(0, maxBytes - stderrBytes)); stderrBytes += chunk.length; } });
		(child as any).on("error", reject);
		(child as any).on("close", (code: number | null) => resolve({ code: code ?? 1, stdout: Buffer.concat(stdout), stderr: Buffer.concat(stderr) }));
	});
}

export function nulPaths(buffer: Buffer): string[] {
	return buffer.toString("utf8").split("\0").filter(Boolean);
}

export async function gitChangedPaths(gitRoot: string): Promise<Set<string>> {
	const commands: string[][] = [
		["diff", "--name-only", "-z"],
		["diff", "--cached", "--name-only", "-z"],
		["ls-files", "--others", "--exclude-standard", "-z"],
	];
	const results = await Promise.all(commands.map((args) => runCommand("git", args, gitRoot)));
	return new Set(results.flatMap((result) => nulPaths(result.stdout)));
}

export async function gitIgnoredPaths(gitRoot: string): Promise<Set<string>> {
	const result = await runCommand("git", ["ls-files", "--others", "--ignored", "--exclude-standard", "-z"], gitRoot, 32 * 1024 * 1024);
	return result.code === 0 ? new Set(nulPaths(result.stdout)) : new Set();
}

export function scanWorkspace(gitRoot: string, ignoredPaths: Set<string>, excludeAgentRuntime = false): { entries: Map<string, string>; externalSymlinks: string[] } {
	const entries = new Map<string, string>();
	const externalSymlinks: string[] = [];
	const pending = [gitRoot];
	let ignoredHashBytes = 0;
	while (pending.length) {
		const directory = pending.pop()!;
		for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
			const fullPath = path.join(directory, entry.name);
			const relativePath = path.relative(gitRoot, fullPath).replaceAll("\\", "/");
			if (relativePath === ".git" || relativePath.startsWith(".git/")) continue;
			if (excludeAgentRuntime && AGENT_RUNTIME_SNAPSHOT_EXCLUDES.some((excluded) => relativePath === excluded || relativePath.startsWith(`${excluded}/`))) continue;
			const stat = fs.lstatSync(fullPath, { bigint: true });
			if (stat.isDirectory()) {
				pending.push(fullPath);
				continue;
			}
			if (entries.size >= MAX_FS_SNAPSHOT_ENTRIES) throw new Error(`工作区文件超过安全快照上限 ${MAX_FS_SNAPSHOT_ENTRIES}`);
			// ctime/ino cannot be restored by an unprivileged Worker the way mtime can,
			// so large ignored files remain detectable even after hash budget fallback.
			let signature = `${stat.mode}:${stat.size}:${stat.mtimeNs}:${stat.ctimeNs}:${stat.ino}:${stat.dev}`;
			if (stat.isSymbolicLink()) {
				const link = fs.readlinkSync(fullPath);
				try {
					const target = fs.realpathSync(fullPath);
					if (!isPathInside(gitRoot, target)) externalSymlinks.push(relativePath);
					const targetStat = fs.statSync(target, { bigint: true });
					signature = `link:${link}:${targetStat.mode}:${targetStat.size}:${targetStat.mtimeNs}:${targetStat.ctimeNs}:${targetStat.ino}:${targetStat.dev}`;
				} catch {
					signature = `link:${link}:broken`;
				}
			} else if (stat.isFile() && ignoredPaths.has(relativePath) && stat.size <= BigInt(MAX_IGNORED_HASH_FILE_BYTES) && ignoredHashBytes + Number(stat.size) <= MAX_IGNORED_HASH_TOTAL_BYTES) {
				ignoredHashBytes += Number(stat.size);
				signature += `:${fileHash(fullPath) ?? "unreadable"}`;
			}
			entries.set(relativePath, signature);
		}
	}
	return { entries, externalSymlinks: [...new Set(externalSymlinks)].sort() };
}

export function fileHash(filePath: string): string | null {
	try {
		if (!fs.statSync(filePath).isFile()) return null;
		return createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
	} catch {
		return null;
	}
}

export async function indexHash(gitRoot: string, relativePath: string): Promise<string | null> {
	const result = await runCommand("git", ["rev-parse", `:${relativePath}`], gitRoot, 1024);
	return result.code === 0 ? result.stdout.toString("utf8").trim() : null;
}

export async function snapshotWorkspace(cwd: string, includePaths: Set<string> = new Set(), includeFilesystem = false, excludeAgentRuntime = false): Promise<WorkspaceSnapshot> {
	const rootResult = await runCommand("git", ["rev-parse", "--show-toplevel"], cwd, 4096);
	if (rootResult.code !== 0) throw new Error("写入 Worker 需要 Git 工作区以安全校验变更范围");
	const gitRoot = fs.realpathSync(rootResult.stdout.toString("utf8").trim());
	const statusPaths = await gitChangedPaths(gitRoot);
	const paths = new Set([...statusPaths, ...includePaths]);
	const files = new Map<string, { worktree: string | null; index: string | null }>();
	for (const relativePath of paths) {
		files.set(relativePath, { worktree: fileHash(path.join(gitRoot, relativePath)), index: await indexHash(gitRoot, relativePath) });
	}
	let fsEntries: Map<string, string> | undefined;
	let externalSymlinks: string[] = [];
	if (includeFilesystem) {
		const shouldExcludeAgentRuntime = excludeAgentRuntime && gitRoot === fs.realpathSync(agentDir());
		const scanned = scanWorkspace(gitRoot, await gitIgnoredPaths(gitRoot), shouldExcludeAgentRuntime);
		fsEntries = scanned.entries;
		externalSymlinks = scanned.externalSymlinks;
	}
	return { gitRoot, cwd, statusPaths, files, fsEntries, excludeAgentRuntime, externalSymlinks };
}

export async function changedSince(before: WorkspaceSnapshot): Promise<{ changed: string[]; after: WorkspaceSnapshot; externalSymlinks: string[] }> {
	const currentPaths = await gitChangedPaths(before.gitRoot);
	const union = new Set([...before.statusPaths, ...currentPaths]);
	const after = await snapshotWorkspace(before.cwd, union, Boolean(before.fsEntries), before.excludeAgentRuntime);
	const changed = new Set<string>();
	for (const relativePath of union) {
		const old = before.files.get(relativePath) ?? { worktree: fileHash(path.join(before.gitRoot, relativePath)), index: await indexHash(before.gitRoot, relativePath) };
		const now = after.files.get(relativePath)!;
		if (old.worktree !== now.worktree || old.index !== now.index || before.statusPaths.has(relativePath) !== currentPaths.has(relativePath)) {
			changed.add(path.relative(before.cwd, path.join(before.gitRoot, relativePath)).replaceAll("\\", "/"));
		}
	}
	if (before.fsEntries && after.fsEntries) {
		const fsPaths = new Set([...before.fsEntries.keys(), ...after.fsEntries.keys()]);
		for (const relativePath of fsPaths) {
			if (before.fsEntries.get(relativePath) !== after.fsEntries.get(relativePath)) {
				changed.add(path.relative(before.cwd, path.join(before.gitRoot, relativePath)).replaceAll("\\", "/"));
			}
		}
	}
	return { changed: [...changed].sort(), after, externalSymlinks: after.externalSymlinks };
}
