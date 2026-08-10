import * as fs from "node:fs";
import * as path from "node:path";
import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { getAgentDir } from "@earendil-works/pi-coding-agent";
import type { Model } from "@earendil-works/pi-ai";
import type { PresetConfig, ResolvedPreset, Route, RoutingConfig, Thinking, WorkerMode, WorkerTask } from "./types";


export const MODES = ["scout", "implement", "test", "review", "fix"] as const;
export const PRESETS = ["auto", "fast", "normal", "deep", "max"] as const;
export const WRITE_MODES = new Set<WorkerMode>(["implement", "test", "fix"]);
export const READ_ONLY_MODES = new Set<WorkerMode>(["scout", "review"]);
export const RESOLVED_PRESETS = ["fast", "normal", "deep", "max"] as const;

export const DEFAULT_OPTIONS = {
	version: 2,
	maxConcurrentWorkers: 3,
	automaticDelegationEnabled: true,
	defaultTimeoutMs: 900_000,
	maxOutputBytes: 65_536,
};

export function agentDir(): string {
	return path.resolve(process.env.PI_CODING_AGENT_DIR || getAgentDir());
}

export function deepMergeMissing<T>(current: T, defaults: T): { value: T; changed: boolean } {
	if (Array.isArray(defaults)) return { value: current === undefined ? defaults : current, changed: current === undefined };
	if (defaults && typeof defaults === "object") {
		const base = current && typeof current === "object" && !Array.isArray(current) ? { ...(current as object) } as Record<string, unknown> : {};
		let changed = !(current && typeof current === "object" && !Array.isArray(current));
		for (const [key, defaultValue] of Object.entries(defaults as Record<string, unknown>)) {
			if (base[key] === undefined) {
				base[key] = defaultValue;
				changed = true;
			} else {
				const merged = deepMergeMissing(base[key], defaultValue);
				base[key] = merged.value;
				changed ||= merged.changed;
			}
		}
		return { value: base as T, changed };
	}
	return { value: current === undefined ? defaults : current, changed: current === undefined };
}

export function atomicWriteJson(filePath: string, value: unknown): void {
	fs.mkdirSync(path.dirname(filePath), { recursive: true });
	const temp = `${filePath}.tmp-${process.pid}-${Date.now()}`;
	fs.writeFileSync(temp, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
	fs.renameSync(temp, filePath);
}

export function normalizeLegacyThinking(value: unknown, warnings: string[]): Thinking {
	if (value === "xhigh" || value === "max" || value === "high") return value;
	if (["off", "minimal", "low", "medium"].includes(String(value))) {
		warnings.push(`旧 thinking 值 ${String(value)} 已兼容提升为 high`);
		return "high";
	}
	return "high";
}

export function validateConfig(raw: RoutingConfig): { config: RoutingConfig; warnings: string[] } {
	const warnings: string[] = [];
	const config = structuredClone(raw);
	delete (config as RoutingConfig & { maxAutomaticRetries?: unknown }).maxAutomaticRetries;
	config.maxConcurrentWorkers = Math.max(1, Math.min(16, Number(config.maxConcurrentWorkers) || 3));
	config.defaultTimeoutMs = Math.max(1_000, Math.min(3_600_000, Number(config.defaultTimeoutMs) || 900_000));
	config.maxOutputBytes = Math.max(8_192, Math.min(1_048_576, Number(config.maxOutputBytes) || 65_536));
	for (const preset of RESOLVED_PRESETS) {
		const item = config[preset];
		if (!item || typeof item.model !== "string" || !splitModelId(item.model)) throw new Error(`worker-settings.json 缺少有效的 ${preset}.model`);
		item.thinking = normalizeLegacyThinking(item.thinking, warnings);
		delete (item as PresetConfig & { maxOutputBytes?: unknown }).maxOutputBytes;
	}
	return { config, warnings };
}

export function splitModelId(full: string): { provider: string; id: string } | null {
	const slash = full.indexOf("/");
	if (slash <= 0 || slash === full.length - 1) return null;
	return { provider: full.slice(0, slash), id: full.slice(slash + 1) };
}

export function supportedThinking(model: Model<any>): string[] {
	if (!model.reasoning) return ["off"];
	const map = (model as Model<any> & { thinkingLevelMap?: Record<string, string | null> }).thinkingLevelMap ?? {};
	const levels = ["off", "minimal", "low", "medium", "high"];
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
	const hadPresetOutputLimits = Boolean(current && typeof current === "object" && RESOLVED_PRESETS.some((preset) => {
		const setting = (current as Record<string, unknown>)[preset];
		return Boolean(setting && typeof setting === "object" && "maxOutputBytes" in setting);
	}));
	const hadLegacyRetrySetting = Boolean(current && typeof current === "object" && "maxAutomaticRetries" in current);
	const merged = deepMergeMissing(current as Record<string, unknown>, DEFAULT_OPTIONS);
	const validated = validateConfig(merged.value as RoutingConfig);
	if (merged.changed || hadPresetOutputLimits || hadLegacyRetrySetting) atomicWriteJson(configPath, validated.config);
	return { config: validated.config, warnings: validated.warnings, path: configPath };
}

export function isPathInside(base: string, candidate: string): boolean {
	const relative = path.relative(base, candidate);
	return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
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
	if (task.preset && !PRESETS.includes(task.preset)) errors.push(`无效 preset: ${String(task.preset)}`);
	if (task.preset === "max" && !task.userExplicitMax) errors.push("Max 仅允许响应用户明确要求；请设置 userExplicitMax: true");
	if (WRITE_MODES.has(task.mode) && (!task.allowedPaths || task.allowedPaths.length === 0)) errors.push(`${task.mode} 必须提供非空 allowedPaths`);
	for (const file of task.relevantFiles ?? []) {
		if (!file.trim()) errors.push("relevantFiles 不能包含空路径");
	}
	for (const [label, patterns] of [["allowedPaths", task.allowedPaths], ["forbiddenPaths", task.forbiddenPaths]] as const) {
		for (const pattern of patterns ?? []) {
			if (!pattern.trim() || path.isAbsolute(pattern) || pattern.split(/[\\/]+/).includes("..")) errors.push(`${label} 只能包含 cwd 下的相对路径或 glob: ${pattern}`);
		}
	}
	try {
		const cwd = resolveTaskCwd(baseCwd, task.cwd);
		if (!fs.statSync(cwd).isDirectory()) errors.push(`cwd 不是目录: ${cwd}`);
	} catch (error) {
		errors.push(error instanceof Error ? error.message : String(error));
	}
	return errors;
}

export function inferPreset(task: WorkerTask): { preset: ResolvedPreset; reasons: string[] } {
	if (task.preset && task.preset !== "auto") return { preset: task.preset, reasons: [`任务显式指定 ${task.preset}`] };
	const text = [task.objective, task.context, ...(task.acceptanceCriteria ?? [])].filter(Boolean).join(" ").toLowerCase();
	const deep = /(跨模块|跨服务|跨语言|并发|异步状态|缓存一致性|网络重试|资源生命周期|数据同步|根因不明|架构|cross.module|cross.service|concurren|async state|cache consistency|retry|resource lifecycle|architecture)/i.test(text);
	if (deep || (task.relevantFiles?.length ?? 0) >= 6) return { preset: "deep", reasons: [deep ? "包含复杂跨模块或状态一致性约束" : "相关文件范围较大"] };
	const fast = /(查找|搜索|文案|css|类型错误|运行已有测试|机械修改|find|locate|copy change|type error|existing test)/i.test(text);
	if (fast && (task.relevantFiles?.length ?? 0) <= 2) return { preset: "fast", reasons: ["目标明确、局部且易验证"] };
	return { preset: "normal", reasons: ["普通开发任务，使用 Normal"] };
}

export function findConfiguredModel(ctx: ExtensionContext, modelId: string): Model<any> | undefined {
	const parsed = splitModelId(modelId);
	if (!parsed) return undefined;
	return ctx.modelRegistry.getAvailable().find((model) => model.provider === parsed.provider && model.id === parsed.id);
}

export function resolveRoute(task: WorkerTask, config: RoutingConfig, ctx: ExtensionContext): Route {
	const inferred = inferPreset(task);
	const preset = inferred.preset;
	const setting = config[preset];
	const thinking = setting.thinking;
	if ((preset === "max" || thinking === "max") && !task.userExplicitMax) throw new Error("Max/xhigh 未获用户明确授权");
	const model = findConfiguredModel(ctx, setting.model);
	if (!model) throw new Error(`${preset}.model 配置的模型不可用：${setting.model}`);
	const levels = supportedThinking(model);
	if (!levels.includes(thinking)) {
		if (thinking === "max") throw new Error(`unsupported：${setting.model} 不支持 max；当前最高为 ${levels.at(-1)}`);
		if (preset === "max" || thinking === "xhigh") throw new Error(`Max blocked：${setting.model} 不支持 xhigh；当前最高为 ${levels.at(-1)}`);
		throw new Error(`${setting.model} 不支持 ${thinking}`);
	}
	return {
		requestedPreset: task.preset ?? "auto",
		resolvedPreset: preset,
		modelId: setting.model,
		provider: model.provider,
		thinking,
		routeReason: inferred.reasons,
	};
}
