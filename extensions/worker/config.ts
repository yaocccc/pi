import * as fs from "node:fs";
import * as path from "node:path";
import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { getAgentDir } from "@earendil-works/pi-coding-agent";
import type { Model } from "@earendil-works/pi-ai";
import { PRESETS, THINKING_LEVELS } from "./api.ts";
import type { PresetConfig, Route, RoutingConfig, Thinking, WorkerMode, WorkerTask } from "./types";


export const MODES = ["scout", "implement", "test", "review", "fix"] as const;
export const SESSION_MODES = ["new", "fork"] as const;
export const WRITE_MODES = new Set<WorkerMode>(["implement", "test", "fix"]);

export const DEFAULT_OPTIONS = {
	version: 2,
	maxConcurrentWorkers: 3,
	defaultTimeoutMs: 900_000,
	maxOutputBytes: 65_536,
};

export function agentDir(): string {
	return path.resolve(process.env.PI_CODING_AGENT_DIR || getAgentDir());
}

export function atomicWriteJson(filePath: string, value: unknown): void {
	fs.mkdirSync(path.dirname(filePath), { recursive: true });
	const temp = `${filePath}.tmp-${process.pid}-${Date.now()}`;
	fs.writeFileSync(temp, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
	fs.renameSync(temp, filePath);
}

export function validateConfig(raw: RoutingConfig): { config: RoutingConfig; warnings: string[] } {
	const warnings: string[] = [];
	const config = structuredClone(raw);
	delete (config as RoutingConfig & { maxAutomaticRetries?: unknown }).maxAutomaticRetries;
	delete (config as RoutingConfig & { automaticDelegationEnabled?: unknown }).automaticDelegationEnabled;
	delete (config as RoutingConfig & { max?: unknown }).max;
	config.maxConcurrentWorkers = Math.max(1, Math.min(16, Number(config.maxConcurrentWorkers) || 3));
	config.defaultTimeoutMs = Math.max(1_000, Math.min(3_600_000, Number(config.defaultTimeoutMs) || 900_000));
	config.maxOutputBytes = Math.max(8_192, Math.min(1_048_576, Number(config.maxOutputBytes) || 65_536));
	for (const preset of PRESETS) {
		const item = config[preset];
		if (!item || typeof item.model !== "string" || !splitModelId(item.model)) throw new Error(`worker-settings.json 缺少有效的 ${preset}.model`);
		if (!THINKING_LEVELS.includes(item.thinking)) throw new Error(`worker-settings.json 无效的 ${preset}.thinking: ${String(item.thinking)}`);
		delete (item as PresetConfig & { maxOutputBytes?: unknown }).maxOutputBytes;
	}
	return { config, warnings };
}

export function splitModelId(full: string): { provider: string; id: string } | null {
	const slash = full.indexOf("/");
	if (slash <= 0 || slash === full.length - 1) return null;
	return { provider: full.slice(0, slash), id: full.slice(slash + 1) };
}

export function supportedThinking(model: Model<any>): Thinking[] {
	if (!model.reasoning) return ["off"];
	const map = (model as Model<any> & { thinkingLevelMap?: Record<string, string | null> }).thinkingLevelMap ?? {};
	const levels: Thinking[] = ["off", "minimal", "low", "medium", "high"];
	if (typeof map.xhigh === "string") levels.push("xhigh");
	if (typeof map.max === "string") levels.push("max");
	return levels.filter((level) => map[level] !== null);
}

export function loadRoutingConfig(): { config: RoutingConfig; warnings: string[]; path: string } {
	const configPath = path.join(agentDir(), "worker-settings.json");
	if (!fs.existsSync(configPath)) throw new Error(`缺少 Worker 路由配置：${configPath}`);
	let current: unknown;
	try {
		current = JSON.parse(fs.readFileSync(configPath, "utf8"));
	} catch (error) {
		throw new Error(`worker-settings.json 无法解析，已保留原文件：${error instanceof Error ? error.message : String(error)}`);
	}
	const hadPresetOutputLimits = Boolean(current && typeof current === "object" && PRESETS.some((preset) => {
		const setting = (current as Record<string, unknown>)[preset];
		return Boolean(setting && typeof setting === "object" && "maxOutputBytes" in setting);
	}));
	const hadLegacyRetrySetting = Boolean(current && typeof current === "object" && "maxAutomaticRetries" in current);
	const hadLegacyDelegationSetting = Boolean(current && typeof current === "object" && "automaticDelegationEnabled" in current);
	const hadLegacyMaxPreset = Boolean(current && typeof current === "object" && "max" in current);
	// Defaults are flat and parsed JSON cannot contain undefined values.
	const missingDefaults = Object.keys(DEFAULT_OPTIONS).some((key) => !Object.hasOwn(current ?? {}, key));
	const validated = validateConfig({ ...DEFAULT_OPTIONS, ...current as RoutingConfig });
	if (missingDefaults || hadPresetOutputLimits || hadLegacyRetrySetting || hadLegacyDelegationSetting || hadLegacyMaxPreset) atomicWriteJson(configPath, validated.config);
	return { config: validated.config, warnings: validated.warnings, path: configPath };
}

export function isPathInside(base: string, candidate: string): boolean {
	const relative = path.relative(base, candidate);
	return relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

export function resolveTaskCwd(baseCwd: string, requested?: string): string {
	const base = fs.realpathSync(baseCwd);
	if (requested && path.isAbsolute(requested)) throw new Error(`cwd 必须是当前工作区内的相对路径: ${requested}`);
	const candidate = fs.realpathSync(path.resolve(base, requested || "."));
	if (!isPathInside(base, candidate)) throw new Error(`cwd 解析后越出当前工作区: ${requested ?? "."}`);
	return candidate;
}

export function validateTask(task: WorkerTask, baseCwd: string): string[] {
	const errors: string[] = [];
	if (!MODES.includes(task.mode)) errors.push(`无效 mode: ${String(task.mode)}`);
	if (!task.objective || !task.objective.trim()) errors.push("objective 不能为空");
	if (!PRESETS.includes(task.preset)) errors.push(`无效 preset: ${String(task.preset)}`);
	if (!SESSION_MODES.includes(task.sessionMode)) errors.push(`无效 sessionMode: ${String(task.sessionMode)}`);
	if (WRITE_MODES.has(task.mode) && (!task.allowedPaths || task.allowedPaths.length === 0)) errors.push(`${task.mode} 必须提供非空 allowedPaths`);
	for (const file of task.relevantFiles ?? []) {
		if (!file.trim()) errors.push("relevantFiles 不能包含空路径");
	}
	let cwd: string | undefined;
	try {
		cwd = resolveTaskCwd(baseCwd, task.cwd);
		if (!fs.statSync(cwd).isDirectory()) errors.push(`cwd 不是目录: ${cwd}`);
	} catch (error) {
		errors.push(error instanceof Error ? error.message : String(error));
	}
	for (const [label, patterns] of [["allowedPaths", task.allowedPaths], ["forbiddenPaths", task.forbiddenPaths]] as const) {
		for (const pattern of patterns ?? []) {
			if (!pattern.trim() || path.isAbsolute(pattern) || pattern.split(/[\\/]+/).includes("..")) {
				errors.push(`${label} 只能包含 cwd 下的相对路径或 glob: ${pattern}`);
				continue;
			}
			// Literals match one path in the write guard, never the directory's children.
			// An explicit trailing slash communicates directory intent even before it exists.
			const normalized = pattern.replaceAll("\\", "/").replace(/^\.\/+/, "");
			if (/[*?]/.test(normalized)) continue;
			const literal = normalized.replace(/\/+$/, "") || ".";
			let existingDirectory = false;
			if (cwd) {
				try { existingDirectory = fs.statSync(path.resolve(cwd, literal)).isDirectory(); }
				catch { /* An absent bare path may be an exact new file. */ }
			}
			if (normalized.endsWith("/") || existingDirectory) {
				errors.push(`${label} 目录声明 ${pattern} 只匹配精确路径，不匹配子文件；如需目录后代，请改为 ${literal}/**（相对 task.cwd）`);
			}
		}
	}
	return errors;
}

export function findConfiguredModel(ctx: ExtensionContext, modelId: string): Model<any> | undefined {
	const parsed = splitModelId(modelId);
	if (!parsed) return undefined;
	return ctx.modelRegistry.getAvailable().find((model) => model.provider === parsed.provider && model.id === parsed.id);
}

export function resolveRoute(task: WorkerTask, config: RoutingConfig, ctx: ExtensionContext): Route {
	const preset = task.preset;
	if (!PRESETS.includes(preset)) throw new Error(`无效 preset: ${String(preset)}`);
	const setting = config[preset];
	const thinking = setting.thinking;
	const model = findConfiguredModel(ctx, setting.model);
	if (!model) throw new Error(`${preset}.model 配置的模型不可用：${setting.model}`);
	const levels = supportedThinking(model);
	if (!levels.includes(thinking)) throw new Error(`${setting.model} 不支持 thinking ${thinking}；当前最高为 ${levels.at(-1)}`);
	return {
		requestedPreset: preset,
		resolvedPreset: preset,
		modelId: setting.model,
		provider: model.provider,
		thinking,
		routeReason: [`任务显式指定 ${preset}`],
	};
}
