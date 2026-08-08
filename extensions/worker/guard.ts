import * as fs from "node:fs";
import * as path from "node:path";
import { isToolCallEventType, type ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { isPathInside } from "./config";
import { matchesAny } from "./security";
import type { WorkerMode } from "./types";

export const WORKER_MODE_ENV = "PI_WORKER_MODE";
export const WORKER_CWD_ENV = "PI_WORKER_CWD";
export const WORKER_ALLOWED_PATHS_ENV = "PI_WORKER_ALLOWED_PATHS";
export const WORKER_FORBIDDEN_PATHS_ENV = "PI_WORKER_FORBIDDEN_PATHS";

const MODES = new Set<WorkerMode>(["scout", "implement", "test", "review", "fix"]);
const WRITE_MODES = new Set<WorkerMode>(["implement", "test", "fix"]);

interface WorkerWritePolicy {
	mode: WorkerMode;
	cwd: string;
	allowedPaths: string[];
	forbiddenPaths: string[];
}

function parsePatterns(name: string): string[] {
	const raw = process.env[name];
	if (!raw) return [];
	const value = JSON.parse(raw);
	if (!Array.isArray(value) || value.some((item) => typeof item !== "string")) throw new Error(`${name} 格式无效`);
	return value;
}

export function loadWorkerWritePolicy(): WorkerWritePolicy {
	const mode = process.env[WORKER_MODE_ENV] as WorkerMode | undefined;
	if (!mode || !MODES.has(mode)) throw new Error("缺少有效的 Worker mode");
	const configuredCwd = process.env[WORKER_CWD_ENV];
	if (!configuredCwd) throw new Error("缺少 Worker cwd");
	const cwd = fs.realpathSync(configuredCwd);
	const allowedPaths = parsePatterns(WORKER_ALLOWED_PATHS_ENV);
	const forbiddenPaths = parsePatterns(WORKER_FORBIDDEN_PATHS_ENV);
	if (WRITE_MODES.has(mode) && allowedPaths.length === 0) throw new Error(`${mode} 缺少 allowedPaths`);
	return { mode, cwd, allowedPaths, forbiddenPaths };
}

function pathEntryExists(filePath: string): boolean {
	try {
		fs.lstatSync(filePath);
		return true;
	} catch {
		return false;
	}
}

export function guardedRelativePath(baseCwd: string, requestedPath: string): string {
	if (!requestedPath.trim()) throw new Error("path 不能为空");
	const candidate = path.resolve(baseCwd, requestedPath);
	if (!isPathInside(baseCwd, candidate)) throw new Error(`path 越出 Worker cwd: ${requestedPath}`);

	let ancestor = candidate;
	while (!pathEntryExists(ancestor)) {
		const parent = path.dirname(ancestor);
		if (parent === ancestor) throw new Error(`无法解析 path: ${requestedPath}`);
		ancestor = parent;
	}
	const realAncestor = fs.realpathSync(ancestor);
	if (!isPathInside(baseCwd, realAncestor)) throw new Error(`path 通过符号链接越出 Worker cwd: ${requestedPath}`);
	if (ancestor === candidate) {
		const realTarget = fs.realpathSync(candidate);
		if (!isPathInside(baseCwd, realTarget)) throw new Error(`path 通过符号链接越出 Worker cwd: ${requestedPath}`);
	}

	return path.relative(baseCwd, candidate).replaceAll("\\", "/") || ".";
}

export function validateWorkerWritePath(policy: WorkerWritePolicy, requestedPath: string): string | undefined {
	if (!WRITE_MODES.has(policy.mode)) return `${policy.mode} 模式禁止调用 edit/write`;
	let relativePath: string;
	try {
		relativePath = guardedRelativePath(policy.cwd, requestedPath);
	} catch (error) {
		return error instanceof Error ? error.message : String(error);
	}
	if (matchesAny(relativePath, policy.forbiddenPaths)) return `edit/write path 命中 forbiddenPaths: ${relativePath}`;
	if (!matchesAny(relativePath, policy.allowedPaths)) return `edit/write path 超出 allowedPaths: ${relativePath}`;
	return undefined;
}

export function registerWorkerWriteGuard(pi: ExtensionAPI): void {
	let policy: WorkerWritePolicy | undefined;
	let policyError: string | undefined;
	try {
		policy = loadWorkerWritePolicy();
	} catch (error) {
		policyError = error instanceof Error ? error.message : String(error);
	}
	const guard = (requestedPath: string) => {
		const reason = policyError ?? (policy ? validateWorkerWritePath(policy, requestedPath) : "Worker 写入策略不可用");
		return reason ? { block: true as const, reason } : undefined;
	};
	pi.on("tool_call", (event) => {
		if (isToolCallEventType("write", event)) return guard(event.input.path);
		if (isToolCallEventType("edit", event)) return guard(event.input.path);
		return undefined;
	});
}
