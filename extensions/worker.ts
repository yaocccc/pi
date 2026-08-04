import { spawn, type ChildProcess } from "node:child_process";
import { createHash } from "node:crypto";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import type { ExtensionAPI, ExtensionContext, Theme } from "@earendil-works/pi-coding-agent";
import { getAgentDir, parseFrontmatter } from "@earendil-works/pi-coding-agent";
import type { Model } from "@earendil-works/pi-ai";
import { StringEnum } from "@earendil-works/pi-ai";
import { Container, Spacer, Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";

type WorkerMode = "investigate" | "implement" | "test" | "review" | "fix";
type WorkerPreset = "auto" | "fast" | "standard" | "deep" | "critical";
type WorkerModel = "auto" | "luna" | "terra" | "sol";
type WorkerThinking = "auto" | "high" | "xhigh" | "max";
type ModelAlias = Exclude<WorkerModel, "auto">;
type Thinking = Exclude<WorkerThinking, "auto">;
type ResolvedPreset = Exclude<WorkerPreset, "auto">;

interface WorkerTask {
	mode: WorkerMode;
	objective: string;
	preset?: WorkerPreset;
	model?: WorkerModel;
	thinking?: WorkerThinking;
	/** Must be true when `thinking: max` reflects an explicit user request. */
	userExplicitMax?: boolean;
	context?: string;
	relevantFiles?: string[];
	allowedPaths?: string[];
	forbiddenPaths?: string[];
	acceptanceCriteria?: string[];
	verificationCommands?: string[];
	outputRequirements?: string[];
	cwd?: string;
	timeoutMs?: number;
}

interface WorkerToolInput {
	task?: WorkerTask;
	tasks?: WorkerTask[];
	/** Required for calls made while automatic delegation is disabled in routing config. */
	manual?: boolean;
}

interface ModelConfig {
	modelId: string | null;
	enabled: boolean;
	supportedThinking?: string[];
}

interface RoutingConfig {
	version: number;
	models: Record<ModelAlias, ModelConfig>;
	presets: Record<ResolvedPreset, { model: ModelAlias; thinking: Thinking }>;
	defaultPreset: ResolvedPreset;
	maxConcurrentWorkers: number;
	maxDelegationDepth: number;
	maxAutomaticRetries: number;
	allowAutomaticXhigh: boolean;
	allowAutomaticMax: boolean;
	automaticDelegationEnabled: boolean;
	defaultTimeoutMs: number;
	maxOutputBytes: number;
}

interface Route {
	requestedPreset: WorkerPreset;
	resolvedPreset: ResolvedPreset;
	requestedModel: WorkerModel;
	modelAlias: ModelAlias;
	modelId: string;
	provider: string;
	requestedThinking: WorkerThinking;
	thinking: Thinking;
	routeReason: string[];
	fallback?: { requested: string; actual: string; reason: string };
	degraded?: boolean;
}

interface CommandResult {
	code: number;
	stdout: Buffer;
	stderr: Buffer;
}

interface WorkspaceSnapshot {
	gitRoot: string;
	cwd: string;
	statusPaths: Set<string>;
	files: Map<string, { worktree: string | null; index: string | null }>;
}

type WorkerUiStatus = "queued" | "running" | "completed" | "blocked" | "failed";
type WorkerUiActivityStatus = "running" | "completed" | "failed";

interface WorkerUsage {
	input: number;
	output: number;
	cacheRead: number;
	cacheWrite: number;
	contextTokens: number;
	turns: number;
}

interface WorkerUiActivity {
	id: string;
	type: "phase" | "tool" | "thinking";
	status: WorkerUiActivityStatus;
	label: string;
	detail?: string;
	at: number;
}

interface ChildProgress {
	phase: string;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
	actualModel?: string;
}

interface WorkerUiTask {
	index: number;
	mode: WorkerMode;
	objective: string;
	status: WorkerUiStatus;
	requestedPreset: WorkerPreset;
	resolvedPreset?: ResolvedPreset;
	modelAlias?: ModelAlias;
	modelId?: string;
	thinking?: Thinking;
	attempt: number;
	phase: string;
	startedAt?: number;
	finishedAt?: number;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
	result?: Record<string, any>;
}

interface WorkerUiDetails {
	kind: "worker-ui";
	startedAt: number;
	finishedAt?: number;
	limit: number;
	total: number;
	completed: number;
	tasks: WorkerUiTask[];
	payload?: Record<string, any>;
}

interface ChildResult {
	exitCode: number;
	stderr: string;
	assistantText: string;
	actualProvider?: string;
	actualModel?: string;
	stopReason?: string;
	errorMessage?: string;
	aborted: boolean;
	timedOut: boolean;
	truncated: boolean;
	activities: WorkerUiActivity[];
	toolCalls: number;
	usage: WorkerUsage;
}

const MODES = ["investigate", "implement", "test", "review", "fix"] as const;
const PRESETS = ["auto", "fast", "standard", "deep", "critical"] as const;
const MODELS = ["auto", "luna", "terra", "sol"] as const;
const THINKING = ["auto", "high", "xhigh", "max"] as const;
const MODEL_ALIASES = ["luna", "terra", "sol"] as const;
const WRITE_MODES = new Set<WorkerMode>(["implement", "test", "fix"]);
const READ_ONLY_MODES = new Set<WorkerMode>(["investigate", "review"]);
const activeChildren = new Set<ChildProcess>();
let runtimeShuttingDown = false;
let activeSlots = 0;
const slotWaiters: Array<() => void> = [];
const UI_ACTIVITY_LIMIT = 20;
const UI_RECENT_ACTIVITY_LIMIT = 5;
const UI_OBJECTIVE_CAP = 180;
const UI_DETAIL_CAP = 160;
const UI_SENSITIVE_KEY = /(?:password|passwd|token|secret|api[-_]?key|private[-_]?key|mnemonic|authorization|cookie|credential|auth)/i;

const TOOL_DESCRIPTION = `Use this tool autonomously for bounded, independently verifiable coding subtasks. Do not ask the user before delegating low-risk tasks. Use Fast for clear local work, Standard for normal development, Deep for complex cross-module work, and Critical only for security, funds, signing, permissions, or similarly high-risk work. Read the worker-orchestration skill when decomposition, parallelization, routing, review, or acceptance strategy is non-trivial. Workers may not create other workers. The main agent remains responsible for reviewing the diff, validation, and acceptance evidence.`;

const DEFAULT_CONFIG: RoutingConfig = {
	version: 1,
	models: {
		luna: { modelId: null, enabled: true },
		terra: { modelId: null, enabled: true },
		sol: { modelId: null, enabled: true },
	},
	presets: {
		fast: { model: "luna", thinking: "high" },
		standard: { model: "terra", thinking: "high" },
		deep: { model: "sol", thinking: "high" },
		critical: { model: "sol", thinking: "xhigh" },
	},
	defaultPreset: "standard",
	maxConcurrentWorkers: 3,
	maxDelegationDepth: 1,
	maxAutomaticRetries: 1,
	allowAutomaticXhigh: true,
	allowAutomaticMax: false,
	automaticDelegationEnabled: true,
	defaultTimeoutMs: 900_000,
	maxOutputBytes: 65_536,
};

const TaskSchema = Type.Object({
	mode: StringEnum(MODES),
	objective: Type.String({ minLength: 1 }),
	preset: Type.Optional(StringEnum(PRESETS)),
	model: Type.Optional(StringEnum(MODELS)),
	thinking: Type.Optional(StringEnum(THINKING)),
	userExplicitMax: Type.Optional(Type.Boolean()),
	context: Type.Optional(Type.String()),
	relevantFiles: Type.Optional(Type.Array(Type.String())),
	allowedPaths: Type.Optional(Type.Array(Type.String())),
	forbiddenPaths: Type.Optional(Type.Array(Type.String())),
	acceptanceCriteria: Type.Optional(Type.Array(Type.String())),
	verificationCommands: Type.Optional(Type.Array(Type.String())),
	outputRequirements: Type.Optional(Type.Array(Type.String())),
	cwd: Type.Optional(Type.String()),
	timeoutMs: Type.Optional(Type.Integer({ minimum: 1_000, maximum: 3_600_000 })),
});

const InputSchema = Type.Object({
	task: Type.Optional(TaskSchema),
	tasks: Type.Optional(Type.Array(TaskSchema, { minItems: 1, maxItems: 12 })),
	manual: Type.Optional(Type.Boolean()),
});

function sanitizeUiText(value: string): string {
	return value
		.replace(/\x1b\[[0-?]*[ -/]*[@-~]/g, "")
		.replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f-\u009f]/g, " ")
		.replace(/\b((?:authorization|cookie|x-auth)\s*:\s*)[^'"\r\n]+/gi, "$1[已隐藏]")
		.replace(/\b((?:[a-z0-9]+[_-])*(?:token|secret|password|passwd|api[-_]?key|private[-_]?key|mnemonic|credential|auth|authorization|cookie)(?:[_-][a-z0-9]+)*\s*[:=]\s*)[^\s,;&]+/gi, "$1[已隐藏]");
}

function uiSnippet(value: string, maxLength: number): string {
	const compact = sanitizeUiText(value).replace(/\s+/g, " ").trim();
	return compact.length > maxLength ? `${compact.slice(0, Math.max(0, maxLength - 1))}…` : compact;
}

function compactUiValue(value: unknown, maxLength = UI_DETAIL_CAP): string | undefined {
	if (value === undefined || value === null) return undefined;
	if (typeof value === "string") return uiSnippet(value, maxLength) || undefined;
	if (typeof value === "number" || typeof value === "boolean") return String(value);
	try {
		return uiSnippet(JSON.stringify(value, (key, nested) => UI_SENSITIVE_KEY.test(key) ? "[已隐藏]" : nested), maxLength) || undefined;
	} catch {
		return "[无法显示]";
	}
}

function summarizeToolArgs(args: unknown): string | undefined {
	if (!args || typeof args !== "object" || Array.isArray(args)) return compactUiValue(args, 120);
	const record = args as Record<string, unknown>;
	const preferred = ["command", "path", "pattern", "query", "url", "task", "offset", "limit"];
	const keys = [...preferred.filter((key) => key in record), ...Object.keys(record).filter((key) => !preferred.includes(key) && !["content", "oldText", "newText", "edits"].includes(key))].slice(0, 3);
	const text = keys.map((key) => `${key}=${UI_SENSITIVE_KEY.test(key) ? "[已隐藏]" : compactUiValue(record[key], 80) ?? ""}`).join(" · ");
	return text ? uiSnippet(text, 140) : undefined;
}

function summarizeToolResult(value: unknown): string | undefined {
	if (value && typeof value === "object" && !Array.isArray(value)) {
		const content = (value as Record<string, unknown>).content;
		if (Array.isArray(content)) {
			const text = content.map((part) => part && typeof part === "object" && (part as Record<string, unknown>).type === "text" ? String((part as Record<string, unknown>).text ?? "") : "").filter(Boolean).join(" ");
			if (text) return uiSnippet(text, UI_DETAIL_CAP);
		}
	}
	return compactUiValue(value, UI_DETAIL_CAP);
}

function publicThinking(message: any): string | undefined {
	if (!message || !Array.isArray(message.content)) return undefined;
	const thinking = message.content.filter((part: any) => part?.type === "thinking" && typeof part.thinking === "string").map((part: any) => part.thinking.trim()).filter(Boolean).join("\n");
	return thinking ? uiSnippet(thinking, UI_DETAIL_CAP) : undefined;
}

function appendUiActivity(activities: WorkerUiActivity[], activity: WorkerUiActivity): void {
	const existing = activities.findIndex((item) => item.id === activity.id);
	if (existing >= 0) activities[existing] = activity;
	else activities.push(activity);
	if (activities.length > UI_ACTIVITY_LIMIT) activities.splice(0, activities.length - UI_ACTIVITY_LIMIT);
}

function emptyWorkerUsage(): WorkerUsage {
	return { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, contextTokens: 0, turns: 0 };
}

function addWorkerUsage(left: WorkerUsage, right: WorkerUsage): WorkerUsage {
	return {
		input: left.input + right.input,
		output: left.output + right.output,
		cacheRead: left.cacheRead + right.cacheRead,
		cacheWrite: left.cacheWrite + right.cacheWrite,
		contextTokens: Math.max(left.contextTokens, right.contextTokens),
		turns: left.turns + right.turns,
	};
}

function messageUsage(message: any, turns = 0): WorkerUsage {
	const usage = message?.usage ?? {};
	return {
		input: Number(usage.input) || 0,
		output: Number(usage.output) || 0,
		cacheRead: Number(usage.cacheRead) || 0,
		cacheWrite: Number(usage.cacheWrite) || 0,
		contextTokens: Number(usage.totalTokens) || 0,
		turns,
	};
}

function estimateMessageTokens(message: any): number {
	if (!Array.isArray(message?.content)) return 0;
	let chars = 0;
	for (const part of message.content) {
		if (part?.type === "text" && typeof part.text === "string") chars += part.text.length;
		else if (part?.type === "thinking" && typeof part.thinking === "string") chars += part.thinking.length;
		else if (part?.type === "toolCall") chars += JSON.stringify(part.arguments ?? {}).length;
	}
	return Math.ceil(chars / 4);
}

function formatTokenCount(value: number): string {
	if (!Number.isFinite(value) || value <= 0) return "0";
	if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
	if (value >= 1_000) return `${Math.round(value / 1_000)}k`;
	return String(Math.round(value));
}

function workerUsageText(usage: WorkerUsage, active = false): string {
	const turn = usage.turns || (active ? 1 : 0);
	const parts = [turn ? `Turn ${turn}` : undefined, usage.input ? `↑${formatTokenCount(usage.input)}` : undefined, usage.output ? `↓${formatTokenCount(usage.output)}` : undefined].filter(Boolean);
	return parts.join(" · ");
}

function cloneUiDetails(details: WorkerUiDetails): WorkerUiDetails {
	return {
		...details,
		tasks: details.tasks.map((task) => ({ ...task, activities: task.activities.map((activity) => ({ ...activity })), usage: { ...task.usage } })),
	};
}

function agentDir(): string {
	return path.resolve(process.env.PI_CODING_AGENT_DIR || getAgentDir());
}

function deepMergeMissing<T>(current: T, defaults: T): { value: T; changed: boolean } {
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

function atomicWriteJson(filePath: string, value: unknown): void {
	fs.mkdirSync(path.dirname(filePath), { recursive: true });
	const temp = `${filePath}.tmp-${process.pid}-${Date.now()}`;
	fs.writeFileSync(temp, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600 });
	fs.renameSync(temp, filePath);
}

function normalizeLegacyThinking(value: unknown, warnings: string[]): Thinking {
	if (value === "xhigh" || value === "max" || value === "high") return value;
	if (["off", "minimal", "low", "medium"].includes(String(value))) {
		warnings.push(`旧 thinking 值 ${String(value)} 已兼容提升为 high`);
		return "high";
	}
	return "high";
}

function validateConfig(raw: RoutingConfig): { config: RoutingConfig; warnings: string[] } {
	const warnings: string[] = [];
	const config = structuredClone(raw);
	config.maxConcurrentWorkers = Math.max(1, Math.min(16, Number(config.maxConcurrentWorkers) || 3));
	config.maxDelegationDepth = 1;
	config.maxAutomaticRetries = Math.max(0, Math.min(1, Number(config.maxAutomaticRetries) || 0));
	config.defaultTimeoutMs = Math.max(1_000, Math.min(3_600_000, Number(config.defaultTimeoutMs) || 900_000));
	config.maxOutputBytes = Math.max(8_192, Math.min(1_048_576, Number(config.maxOutputBytes) || 65_536));
	if (!["fast", "standard", "deep", "critical"].includes(config.defaultPreset)) config.defaultPreset = "standard";
	for (const preset of ["fast", "standard", "deep", "critical"] as const) {
		const item = config.presets[preset] ?? DEFAULT_CONFIG.presets[preset];
		if (!MODEL_ALIASES.includes(item.model)) item.model = DEFAULT_CONFIG.presets[preset].model;
		item.thinking = normalizeLegacyThinking(item.thinking, warnings);
		config.presets[preset] = item;
	}
	return { config, warnings };
}

function splitModelId(full: string): { provider: string; id: string } | null {
	const slash = full.indexOf("/");
	if (slash <= 0 || slash === full.length - 1) return null;
	return { provider: full.slice(0, slash), id: full.slice(slash + 1) };
}

function exactModelMatch(model: Model<any>, alias: ModelAlias): boolean {
	const expectedId = `gpt-5.6-${alias}`;
	const expectedName = `gpt-5.6 ${alias}`;
	return model.id.toLowerCase() === expectedId || model.name.toLowerCase().replace(/\s+/g, " ") === expectedName;
}

function supportedThinking(model: Model<any>): string[] {
	if (!model.reasoning) return ["off"];
	const map = (model as Model<any> & { thinkingLevelMap?: Record<string, string | null> }).thinkingLevelMap ?? {};
	const levels = ["off", "minimal", "low", "medium", "high"];
	if (typeof map.xhigh === "string") levels.push("xhigh");
	if (typeof map.max === "string") levels.push("max");
	return levels.filter((level) => map[level] !== null);
}

async function loadRoutingConfig(ctx: ExtensionContext): Promise<{ config: RoutingConfig; warnings: string[]; path: string }> {
	const configPath = path.join(agentDir(), "worker-routing.json");
	let current: unknown = {};
	if (fs.existsSync(configPath)) {
		try {
			current = JSON.parse(fs.readFileSync(configPath, "utf8"));
		} catch (error) {
			throw new Error(`worker-routing.json 无法解析，已保留原文件：${error instanceof Error ? error.message : String(error)}`);
		}
	}
	const merged = deepMergeMissing(current as RoutingConfig, DEFAULT_CONFIG);
	const validated = validateConfig(merged.value);
	const config = validated.config;
	// Keep a persistence copy so temporary availability decisions never overwrite
	// a user's model ID or enabled flag merely because a provider is offline.
	const persistConfig = structuredClone(config);
	const warnings = [...validated.warnings];

	await ctx.modelRegistry.refresh();
	const available = ctx.modelRegistry.getAvailable();
	const configuredProviders = MODEL_ALIASES
		.map((alias) => config.models[alias]?.modelId)
		.filter((id): id is string => Boolean(id))
		.map(splitModelId)
		.filter((entry): entry is { provider: string; id: string } => Boolean(entry))
		.map((entry) => entry.provider);
	const provider = configuredProviders[0] || ctx.model?.provider;
	if (configuredProviders.some((item) => item !== provider)) warnings.push("配置包含多个 Provider；不会跨 Provider 自动回退");

	let discovered = false;
	if (provider) {
		for (const alias of MODEL_ALIASES) {
			const entry = config.models[alias];
			const parsed = entry.modelId ? splitModelId(entry.modelId) : null;
			const existing = parsed ? available.find((model) => model.provider === parsed.provider && model.id === parsed.id) : undefined;
			if (existing) continue;
			const matches = available.filter((model) => model.provider === provider && exactModelMatch(model, alias));
			if (matches.length === 1) {
				entry.modelId = `${matches[0].provider}/${matches[0].id}`;
				entry.supportedThinking = supportedThinking(matches[0]);
				persistConfig.models[alias].modelId = entry.modelId;
				persistConfig.models[alias].supportedThinking = entry.supportedThinking;
				discovered = true;
			} else if (matches.length === 0) {
				entry.modelId = null;
				entry.enabled = false;
				warnings.push(`未在当前 Provider ${provider} 准确识别 ${alias}`);
			} else {
				entry.modelId = null;
				entry.enabled = false;
				warnings.push(`${alias} 存在多个精确候选，已禁用以避免误选`);
			}
		}
	}
	if (merged.changed || discovered || !fs.existsSync(configPath)) atomicWriteJson(configPath, persistConfig);
	return { config, warnings, path: configPath };
}

function validateTask(task: WorkerTask, baseCwd: string): string[] {
	const errors: string[] = [];
	if (!MODES.includes(task.mode)) errors.push(`无效 mode: ${String(task.mode)}`);
	if (!task.objective || !task.objective.trim()) errors.push("objective 不能为空");
	if (task.preset && !PRESETS.includes(task.preset)) errors.push(`无效 preset: ${String(task.preset)}`);
	if (task.model && !MODELS.includes(task.model)) errors.push(`无效 model: ${String(task.model)}`);
	if (task.thinking && !THINKING.includes(task.thinking)) errors.push(`无效 thinking: ${String(task.thinking)}`);
	if (task.thinking === "max" && !task.userExplicitMax) errors.push("max 仅允许响应用户明确要求；请设置 userExplicitMax: true");
	if (WRITE_MODES.has(task.mode) && (!task.allowedPaths || task.allowedPaths.length === 0)) errors.push(`${task.mode} 必须提供非空 allowedPaths`);
	for (const [label, patterns] of [["relevantFiles", task.relevantFiles], ["allowedPaths", task.allowedPaths], ["forbiddenPaths", task.forbiddenPaths]] as const) {
		for (const pattern of patterns ?? []) {
			if (!pattern.trim() || path.isAbsolute(pattern) || pattern.split(/[\\/]+/).includes("..")) errors.push(`${label} 只能包含 cwd 下的相对路径或 glob: ${pattern}`);
		}
	}
	const cwd = path.resolve(baseCwd, task.cwd || ".");
	if (!fs.existsSync(cwd) || !fs.statSync(cwd).isDirectory()) errors.push(`cwd 不是现有目录: ${cwd}`);
	return errors;
}

function inferPreset(task: WorkerTask, config: RoutingConfig): { preset: ResolvedPreset; reasons: string[] } {
	if (task.preset && task.preset !== "auto") return { preset: task.preset, reasons: [`任务显式指定 ${task.preset}`] };
	const text = [task.objective, task.context, ...(task.acceptanceCriteria ?? [])].filter(Boolean).join(" ").toLowerCase();
	const critical = /(私钥|助记词|资金|资产安全|钱包签名|签名重放|授权|权限边界|合约资金|不可恢复|不可逆数据|core data consistency|private key|mnemonic|wallet signing|replay|authorization|permission boundary|funds)/i.test(text);
	if (critical && config.allowAutomaticXhigh) return { preset: "critical", reasons: ["涉及资金、签名、授权或不可恢复风险"] };
	const deep = /(跨模块|跨服务|跨语言|并发|异步状态|缓存一致性|网络重试|资源生命周期|数据同步|根因不明|架构|cross.module|cross.service|concurren|async state|cache consistency|retry|resource lifecycle|architecture)/i.test(text);
	if (deep || (task.relevantFiles?.length ?? 0) >= 6) return { preset: "deep", reasons: [deep ? "包含复杂跨模块或状态一致性约束" : "相关文件范围较大"] };
	const fast = /(查找|搜索|文案|css|类型错误|运行已有测试|机械修改|find|locate|copy change|type error|existing test)/i.test(text);
	if (fast && (task.relevantFiles?.length ?? 0) <= 2) return { preset: "fast", reasons: ["目标明确、局部且易验证"] };
	return { preset: config.defaultPreset || "standard", reasons: ["普通开发任务，采用默认 Standard"] };
}

function findAvailableModel(ctx: ExtensionContext, config: RoutingConfig, alias: ModelAlias, provider: string): Model<any> | undefined {
	const modelId = config.models[alias]?.modelId;
	const parsed = modelId ? splitModelId(modelId) : null;
	if (!config.models[alias]?.enabled || !parsed || parsed.provider !== provider) return undefined;
	return ctx.modelRegistry.getAvailable().find((model) => model.provider === parsed.provider && model.id === parsed.id);
}

function fallbackAliases(alias: ModelAlias): ModelAlias[] {
	if (alias === "luna") return ["terra"];
	if (alias === "terra") return ["sol"];
	return ["terra"];
}

function resolveRoute(task: WorkerTask, config: RoutingConfig, ctx: ExtensionContext): Route {
	const inferred = inferPreset(task, config);
	const preset = inferred.preset;
	const requestedModel = task.model ?? "auto";
	const requestedThinking = task.thinking ?? "auto";
	let alias: ModelAlias = requestedModel === "auto" ? config.presets[preset].model : requestedModel;
	let thinking: Thinking = requestedThinking === "auto" ? config.presets[preset].thinking : requestedThinking;
	if (["off", "minimal", "low", "medium"].includes(String(thinking))) thinking = "high";
	if (thinking === "max" && !task.userExplicitMax) throw new Error("max 未获用户明确授权");
	if (thinking === "max" && config.allowAutomaticMax && !task.userExplicitMax) throw new Error("max 不得自动使用");
	const configured = config.models[alias]?.modelId;
	const parsed = configured ? splitModelId(configured) : null;
	const provider = parsed?.provider || ctx.model?.provider;
	if (!provider) throw new Error("无法确定当前 Provider");
	let model = findAvailableModel(ctx, config, alias, provider);
	let fallback: Route["fallback"];
	let degraded = false;
	if (!model) {
		if (preset === "critical") throw new Error(`Critical blocked：${alias} 在 Provider ${provider} 不可用，不能静默降级`);
		const replacement = fallbackAliases(alias).find((candidate) => findAvailableModel(ctx, config, candidate, provider));
		if (!replacement) throw new Error(`${alias} 在 Provider ${provider} 不可用，且无同 Provider 回退模型`);
		const previous = alias;
		alias = replacement;
		model = findAvailableModel(ctx, config, alias, provider)!;
		degraded = previous === "sol";
		fallback = { requested: `${previous} + ${thinking}`, actual: `${alias} + ${thinking}`, reason: `${previous} unavailable` };
	}
	const levels = supportedThinking(model);
	if (!levels.includes(thinking)) {
		if (thinking === "max") throw new Error(`unsupported：${model.provider}/${model.id} 不支持 max；当前最高为 ${levels.at(-1)}`);
		if (preset === "critical" || thinking === "xhigh") throw new Error(`Critical blocked：${model.provider}/${model.id} 不支持 xhigh；当前最高为 ${levels.at(-1)}`);
		throw new Error(`${model.provider}/${model.id} 不支持 ${thinking}`);
	}
	return {
		requestedPreset: task.preset ?? "auto",
		resolvedPreset: preset,
		requestedModel,
		modelAlias: alias,
		modelId: `${model.provider}/${model.id}`,
		provider: model.provider,
		requestedThinking,
		thinking,
		routeReason: inferred.reasons,
		fallback,
		degraded,
	};
}

function globToRegExp(pattern: string): RegExp {
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

function matchesAny(file: string, patterns: string[]): boolean {
	const normalized = file.replaceAll("\\", "/").replace(/^\.\//, "");
	return patterns.some((pattern) => globToRegExp(pattern).test(normalized));
}

function staticGlobPrefix(pattern: string): string {
	const normalized = pattern.replaceAll("\\", "/").replace(/^\.\//, "");
	const index = normalized.search(/[?*[]/);
	return (index < 0 ? normalized : normalized.slice(0, index)).replace(/\/$/, "");
}

function writeTasksMayOverlap(a: WorkerTask, b: WorkerTask): boolean {
	if (!WRITE_MODES.has(a.mode) || !WRITE_MODES.has(b.mode)) return false;
	if (!a.allowedPaths?.length || !b.allowedPaths?.length) return true;
	const aCwd = path.resolve(a.cwd || ".");
	const bCwd = path.resolve(b.cwd || ".");
	if (aCwd !== bCwd) return false;
	return a.allowedPaths.some((left) => b.allowedPaths!.some((right) => {
		const lp = staticGlobPrefix(left);
		const rp = staticGlobPrefix(right);
		return !lp || !rp || lp === rp || lp.startsWith(`${rp}/`) || rp.startsWith(`${lp}/`);
	}));
}

async function runCommand(command: string, args: string[], cwd: string, maxBytes = 4 * 1024 * 1024): Promise<CommandResult> {
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

function nulPaths(buffer: Buffer): string[] {
	return buffer.toString("utf8").split("\0").filter(Boolean);
}

async function gitChangedPaths(gitRoot: string): Promise<Set<string>> {
	const commands: string[][] = [
		["diff", "--name-only", "-z"],
		["diff", "--cached", "--name-only", "-z"],
		["ls-files", "--others", "--exclude-standard", "-z"],
	];
	const results = await Promise.all(commands.map((args) => runCommand("git", args, gitRoot)));
	return new Set(results.flatMap((result) => nulPaths(result.stdout)));
}

function fileHash(filePath: string): string | null {
	try {
		if (!fs.statSync(filePath).isFile()) return null;
		return createHash("sha256").update(fs.readFileSync(filePath)).digest("hex");
	} catch {
		return null;
	}
}

async function indexHash(gitRoot: string, relativePath: string): Promise<string | null> {
	const result = await runCommand("git", ["rev-parse", `:${relativePath}`], gitRoot, 1024);
	return result.code === 0 ? result.stdout.toString("utf8").trim() : null;
}

async function snapshotWorkspace(cwd: string, includePaths: Set<string> = new Set()): Promise<WorkspaceSnapshot> {
	const rootResult = await runCommand("git", ["rev-parse", "--show-toplevel"], cwd, 4096);
	if (rootResult.code !== 0) throw new Error("写入 Worker 需要 Git 工作区以安全校验变更范围");
	const gitRoot = fs.realpathSync(rootResult.stdout.toString("utf8").trim());
	const statusPaths = await gitChangedPaths(gitRoot);
	const paths = new Set([...statusPaths, ...includePaths]);
	const files = new Map<string, { worktree: string | null; index: string | null }>();
	for (const relativePath of paths) {
		files.set(relativePath, { worktree: fileHash(path.join(gitRoot, relativePath)), index: await indexHash(gitRoot, relativePath) });
	}
	return { gitRoot, cwd, statusPaths, files };
}

async function changedSince(before: WorkspaceSnapshot): Promise<{ changed: string[]; after: WorkspaceSnapshot }> {
	const currentPaths = await gitChangedPaths(before.gitRoot);
	const union = new Set([...before.statusPaths, ...currentPaths]);
	const after = await snapshotWorkspace(before.cwd, union);
	const changed: string[] = [];
	for (const relativePath of union) {
		const old = before.files.get(relativePath) ?? { worktree: fileHash(path.join(before.gitRoot, relativePath)), index: await indexHash(before.gitRoot, relativePath) };
		const now = after.files.get(relativePath)!;
		if (old.worktree !== now.worktree || old.index !== now.index || before.statusPaths.has(relativePath) !== currentPaths.has(relativePath)) {
			changed.push(path.relative(before.cwd, path.join(before.gitRoot, relativePath)).replaceAll("\\", "/"));
		}
	}
	return { changed: [...new Set(changed)].sort(), after };
}

function getPiInvocation(args: string[]): { command: string; args: string[] } {
	const currentScript = process.argv[1];
	if (currentScript && !currentScript.startsWith("/$bunfs/root/") && fs.existsSync(currentScript)) {
		const runtime = path.basename(process.execPath).toLowerCase();
		if (runtime === "node" || runtime === "node.exe" || runtime === "bun" || runtime === "bun.exe") return { command: process.execPath, args: [currentScript, ...args] };
		return { command: currentScript, args };
	}
	return { command: "pi", args };
}

function killProcessTree(child: ChildProcess, signal: NodeJS.Signals): void {
	if (!child.pid) return;
	try {
		if (process.platform !== "win32") process.kill(-child.pid, signal);
		else child.kill(signal);
	} catch {
		try { child.kill(signal); } catch { /* already exited */ }
	}
}

function killAllChildren(force = false): void {
	for (const child of activeChildren) killProcessTree(child, force ? "SIGKILL" : "SIGTERM");
	if (!force && activeChildren.size > 0) {
		setTimeout(() => {
			for (const child of activeChildren) killProcessTree(child, "SIGKILL");
		}, 3_000).unref();
	}
}

async function acquireSlot(limit: number): Promise<() => void> {
	while (activeSlots >= limit) await new Promise<void>((resolve) => slotWaiters.push(resolve));
	activeSlots++;
	return () => {
		activeSlots--;
		slotWaiters.shift()?.();
	};
}

function workerPromptBody(): string {
	const promptPath = path.join(agentDir(), "agents", "worker.md");
	if (!fs.existsSync(promptPath)) throw new Error(`缺少 Worker Prompt: ${promptPath}`);
	const parsed = parseFrontmatter<Record<string, string>>(fs.readFileSync(promptPath, "utf8"));
	return parsed.body.trim();
}

function buildTaskPrompt(task: WorkerTask, route: Route, before: WorkspaceSnapshot | null): string {
	const existing = before ? [...before.statusPaths].map((item) => path.relative(before.cwd, path.join(before.gitRoot, item)).replaceAll("\\", "/")) : [];
	const contract = {
		mode: task.mode,
		objective: task.objective.trim(),
		context: task.context ?? "",
		relevantFiles: task.relevantFiles ?? [],
		allowedPaths: task.allowedPaths ?? [],
		forbiddenPaths: task.forbiddenPaths ?? [],
		acceptanceCriteria: task.acceptanceCriteria ?? [],
		verificationCommands: task.verificationCommands ?? [],
		outputRequirements: task.outputRequirements ?? [],
		cwd: before?.cwd,
		preExistingUserChanges: existing,
		execution: { preset: route.resolvedPreset, model: route.modelId, thinking: route.thinking },
	};
	return `执行以下单一 Worker 任务。不要创建或调用其他 Worker。\n\n任务契约：\n${JSON.stringify(contract, null, 2)}\n\n最终只返回一个 JSON 对象，不要 Markdown 代码围栏，不要隐藏思考过程，不要完整日志。格式：\n${JSON.stringify({ status: "completed | blocked | failed", summary: ["完成的工作"], changed_files: ["path"], validation: [{ command: "command", result: "passed | failed | not_run", details: "简要证据" }], acceptance: [{ criterion: "验收条件", result: "passed | failed | uncertain", evidence: "证据" }], findings: [], risks: [], out_of_scope: [], recommended_next_action: [] }, null, 2)}`;
}

function extractText(message: any): string {
	if (!message || message.role !== "assistant" || !Array.isArray(message.content)) return "";
	return message.content.filter((item: any) => item?.type === "text" && typeof item.text === "string").map((item: any) => item.text).join("\n");
}

async function runPiWorker(
	task: WorkerTask,
	route: Route,
	cwd: string,
	systemPrompt: string,
	prompt: string,
	signal: AbortSignal | undefined,
	timeoutMs: number,
	maxOutputBytes: number,
	concurrencyLimit: number,
	onProgress?: (progress: ChildProgress) => void,
): Promise<ChildResult> {
	const tools = READ_ONLY_MODES.has(task.mode) ? ["read", "grep", "find", "ls"] : ["read", "grep", "find", "ls", "bash", "edit", "write"];
	const activities: WorkerUiActivity[] = [];
	const seenToolIds = new Set<string>();
	let phase = "等待执行槽位";
	let toolCalls = 0;
	let lastEmitAt = 0;
	let latestThinking = "";
	let settledUsage = emptyWorkerUsage();
	let streamUsage = emptyWorkerUsage();
	let currentTurn = 1;
	let activeTurn = true;
	const usageSnapshot = (): WorkerUsage => {
		const usage = addWorkerUsage(settledUsage, streamUsage);
		usage.turns = Math.max(settledUsage.turns, activeTurn ? currentTurn : 0);
		return usage;
	};
	const emit = (force = false) => {
		const now = Date.now();
		if (!force && now - lastEmitAt < 150) return;
		lastEmitAt = now;
		onProgress?.({ phase, activities: activities.map((item) => ({ ...item })), toolCalls, usage: usageSnapshot(), actualModel: undefined });
	};
	const setPhase = (value: string, status: WorkerUiActivityStatus = "running") => {
		phase = value;
		for (const activity of activities) {
			if (activity.type === "phase" && activity.status === "running") activity.status = "completed";
		}
		appendUiActivity(activities, { id: `phase:${value}`, type: "phase", status, label: value, at: Date.now() });
		emit(true);
	};
	const upsertTool = (id: string, name: string, status: WorkerUiActivityStatus, detail?: string) => {
		if (!seenToolIds.has(id)) {
			seenToolIds.add(id);
			toolCalls++;
		}
		const activityId = `tool:${id}`;
		const previous = activities.find((item) => item.id === activityId);
		appendUiActivity(activities, {
			id: activityId,
			type: "tool",
			status,
			label: uiSnippet(name, 32) || "未知工具",
			detail: detail || previous?.detail,
			at: Date.now(),
		});
		emit(status !== "running");
	};
	const recordThinking = (value: string) => {
		const text = uiSnippet(value, UI_DETAIL_CAP);
		if (!text) return;
		const existing = activities.findIndex((item) => item.id === "thinking:latest");
		if (existing >= 0) activities.splice(existing, 1);
		activities.push({ id: "thinking:latest", type: "thinking", status: "running", label: "思考", detail: text, at: Date.now() });
		if (activities.length > UI_ACTIVITY_LIMIT) activities.splice(0, activities.length - UI_ACTIVITY_LIMIT);
		emit();
	};

	emit(true);
	const tempDir = await fs.promises.mkdtemp(path.join(os.tmpdir(), "pi-worker-"));
	const systemPath = path.join(tempDir, "worker-system.md");
	await fs.promises.writeFile(systemPath, systemPrompt, { encoding: "utf8", mode: 0o600 });
	const args = ["--mode", "json", "--print", "--no-session", "--no-extensions", "--no-skills", "--no-context-files", "--model", route.modelId, "--thinking", route.thinking, "--tools", tools.join(","), "--append-system-prompt", systemPath, prompt];
	const invocation = getPiInvocation(args);
	const release = await acquireSlot(concurrencyLimit);
	try {
		return await new Promise<ChildResult>((resolve) => {
			let stdoutBuffer = "";
			let stderr = "";
			let assistantText = "";
			let actualProvider: string | undefined;
			let actualModel: string | undefined;
			let stopReason: string | undefined;
			let errorMessage: string | undefined;
			let truncated = false;
			let aborted = false;
			let timedOut = false;
			let settled = false;
			setPhase("启动独立 Worker");
			const child = spawn(invocation.command, invocation.args, {
				cwd,
				shell: false,
				detached: process.platform !== "win32",
				stdio: ["ignore", "pipe", "pipe"],
				env: { ...process.env, PI_WORKER_DEPTH: "1", PI_SKIP_VERSION_CHECK: "1" },
			});
			activeChildren.add(child);
			setPhase("Worker 执行中");
			const finish = (exitCode: number) => {
				if (settled) return;
				settled = true;
				clearTimeout(timeout);
				signal?.removeEventListener("abort", abortHandler);
				activeChildren.delete(child);
				phase = aborted ? "已取消" : timedOut ? "已超时" : exitCode === 0 ? "子进程已完成" : "子进程失败";
				for (const activity of activities) {
					if (activity.status !== "running") continue;
					activity.status = activity.type === "tool" && (exitCode !== 0 || aborted || timedOut) ? "failed" : "completed";
				}
				appendUiActivity(activities, { id: "phase:finished", type: "phase", status: exitCode === 0 && !aborted && !timedOut ? "completed" : "failed", label: phase, at: Date.now() });
				emit(true);
				const usage = usageSnapshot();
				usage.turns = Math.max(usage.turns, settledUsage.turns);
				resolve({ exitCode, stderr: stderr.slice(-16_384), assistantText, actualProvider, actualModel, stopReason, errorMessage, aborted, timedOut, truncated, activities: activities.map((item) => ({ ...item })), toolCalls, usage });
			};
			const terminate = (reason: "abort" | "timeout") => {
				if (settled) return;
				aborted = reason === "abort";
				timedOut = reason === "timeout";
				setPhase(reason === "abort" ? "正在取消" : "正在终止超时任务", "failed");
				killProcessTree(child, "SIGTERM");
				setTimeout(() => { if (!settled) killProcessTree(child, "SIGKILL"); }, 3_000).unref();
			};
			const abortHandler = () => terminate("abort");
			const timeout = setTimeout(() => terminate("timeout"), timeoutMs);
			if (signal?.aborted) abortHandler(); else signal?.addEventListener("abort", abortHandler, { once: true });
			const processEvent = (event: any) => {
				if (event.type === "turn_start") {
					currentTurn = Math.max(currentTurn, Number(event.turnIndex) + 1 || settledUsage.turns + 1);
					activeTurn = true;
					emit(true);
					return;
				}
				if (event.type === "message_update") {
					if (event.message?.role === "assistant") {
						streamUsage = messageUsage(event.message);
						streamUsage.output = Math.max(streamUsage.output, estimateMessageTokens(event.message));
						activeTurn = true;
					}
					const thinking = publicThinking(event.message);
					if (thinking) recordThinking(thinking);
					const delta = event.assistantMessageEvent;
					if (delta?.type === "thinking_delta" && typeof delta.delta === "string") {
						latestThinking = uiSnippet(`${latestThinking}${delta.delta}`, UI_DETAIL_CAP);
						streamUsage.output = Math.max(streamUsage.output, Math.ceil(latestThinking.length / 4));
						recordThinking(latestThinking);
					}
					emit();
					return;
				}
				if (event.type === "message_end" && event.message?.role === "assistant") {
					assistantText = extractText(event.message) || assistantText;
					if (Buffer.byteLength(assistantText, "utf8") > maxOutputBytes) truncated = true;
					actualProvider = event.message.provider || actualProvider;
					actualModel = event.message.model || actualModel;
					stopReason = event.message.stopReason || stopReason;
					errorMessage = event.message.errorMessage || errorMessage;
					const reported = messageUsage(event.message, 1);
					if (!reported.input) reported.input = streamUsage.input;
					if (!reported.output) reported.output = streamUsage.output || estimateMessageTokens(event.message);
					if (!reported.cacheRead) reported.cacheRead = streamUsage.cacheRead;
					if (!reported.cacheWrite) reported.cacheWrite = streamUsage.cacheWrite;
					if (!reported.contextTokens) reported.contextTokens = streamUsage.contextTokens;
					settledUsage = addWorkerUsage(settledUsage, reported);
					currentTurn = Math.max(currentTurn, settledUsage.turns);
					streamUsage = emptyWorkerUsage();
					activeTurn = false;
					const thinking = publicThinking(event.message);
					if (thinking) recordThinking(thinking);
					for (const part of event.message.content ?? []) {
						if (part?.type === "toolCall") upsertTool(part.id, part.name, "running", summarizeToolArgs(part.arguments));
					}
					return;
				}
				if (event.type === "tool_execution_start" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, "running", summarizeToolArgs(event.args));
					return;
				}
				if (event.type === "tool_execution_update" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, "running", summarizeToolResult(event.partialResult) ?? summarizeToolArgs(event.args));
					return;
				}
				if (event.type === "tool_execution_end" && event.toolCallId && event.toolName) {
					upsertTool(event.toolCallId, event.toolName, event.isError ? "failed" : "completed", summarizeToolResult(event.result));
					return;
				}
				if ((event.type === "message_end" || event.type === "tool_result_end") && event.message?.role === "toolResult") {
					upsertTool(event.message.toolCallId, event.message.toolName, event.message.isError ? "failed" : "completed", summarizeToolResult(event.message));
				}
			};
			child.stdout.on("data", (chunk: Buffer) => {
				stdoutBuffer += chunk.toString("utf8");
				const lines = stdoutBuffer.split("\n");
				stdoutBuffer = lines.pop() || "";
				for (const line of lines) {
					if (!line.trim()) continue;
					try { processEvent(JSON.parse(line)); }
					catch { /* ignore non-JSON diagnostics */ }
				}
			});
			child.stderr.on("data", (chunk: Buffer) => { stderr = `${stderr}${chunk.toString("utf8")}`.slice(-65_536); });
			(child as any).on("error", (error: Error) => { errorMessage = error.message; finish(1); });
			(child as any).on("close", (code: number | null) => {
				if (stdoutBuffer.trim()) {
					try { processEvent(JSON.parse(stdoutBuffer)); } catch { /* ignore trailing diagnostics */ }
				}
				finish(code ?? 1);
			});
		});
	} finally {
		release();
		await fs.promises.rm(tempDir, { recursive: true, force: true });
	}
}

function parseStructuredResult(text: string): Record<string, any> | null {
	const cleaned = text.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "");
	try {
		const parsed = JSON.parse(cleaned);
		return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? parsed : null;
	} catch {
		const start = cleaned.indexOf("{");
		const end = cleaned.lastIndexOf("}");
		if (start >= 0 && end > start) {
			try { return JSON.parse(cleaned.slice(start, end + 1)); } catch { return null; }
		}
		return null;
	}
}

function isBlockedFailure(child: ChildResult): boolean {
	const text = `${child.stderr}\n${child.errorMessage ?? ""}\n${child.assistantText}`;
	return /(permission denied|unauthorized|authentication|not found|no such file|command not found|missing dependency|external service|network is unreachable|allowedpaths|git 工作区|权限|认证|不存在|缺少依赖)/i.test(text);
}

function escalationRoute(route: Route, task: WorkerTask, config: RoutingConfig, ctx: ExtensionContext): Route | null {
	let next: ModelAlias | null = route.modelAlias === "luna" ? "terra" : route.modelAlias === "terra" ? "sol" : null;
	let thinking = route.thinking;
	if (!next && route.modelAlias === "sol" && route.thinking === "high" && inferPreset(task, config).preset === "critical") thinking = "xhigh";
	else if (!next) return null;
	const alias = next ?? "sol";
	const model = findAvailableModel(ctx, config, alias, route.provider);
	if (!model || !supportedThinking(model).includes(thinking)) return null;
	return { ...route, modelAlias: alias, modelId: `${model.provider}/${model.id}`, thinking, routeReason: [...route.routeReason, `首次失败后自动升级到 ${alias} + ${thinking}`] };
}

function baseExecution(task: WorkerTask, route: Route | null, attempt: number, escalatedFrom: string | null, warnings: string[]) {
	return {
		requested_preset: task.preset ?? "auto",
		resolved_preset: route?.resolvedPreset ?? null,
		requested_model: task.model ?? "auto",
		resolved_model_alias: route?.modelAlias ?? null,
		resolved_model_id: route?.modelId ?? null,
		requested_thinking: task.thinking ?? "auto",
		resolved_thinking: route?.thinking ?? null,
		attempt,
		escalated_from: escalatedFrom,
		route_reason: route?.routeReason ?? [],
		fallback: route?.fallback ?? null,
		degraded: route?.degraded ?? false,
		warnings,
	};
}

async function executeTask(task: WorkerTask, config: RoutingConfig, warnings: string[], ctx: ExtensionContext, signal: AbortSignal | undefined, onProgress?: (patch: Partial<WorkerUiTask>) => void): Promise<Record<string, any>> {
	const cwd = fs.realpathSync(path.resolve(ctx.cwd, task.cwd || "."));
	let route: Route;
	onProgress?.({ status: "running", phase: "解析模型路由" });
	try {
		route = resolveRoute(task, config, ctx);
		onProgress?.({ resolvedPreset: route.resolvedPreset, modelAlias: route.modelAlias, modelId: route.modelId, thinking: route.thinking, phase: "检查工作区" });
	}
	catch (error) {
		return { status: "blocked", execution: baseExecution(task, null, 0, null, warnings), summary: [error instanceof Error ? error.message : String(error)], changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: ["主 Agent 检查模型可用性或任务契约"] };
	}
	let before: WorkspaceSnapshot | null = null;
	try {
		if (WRITE_MODES.has(task.mode) || READ_ONLY_MODES.has(task.mode)) before = await snapshotWorkspace(cwd);
		onProgress?.({ phase: "工作区检查完成" });
	} catch (error) {
		if (WRITE_MODES.has(task.mode)) return { status: "blocked", execution: baseExecution(task, route, 0, null, warnings), summary: [error instanceof Error ? error.message : String(error)], changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: ["在 Git 工作区中运行写入任务"] };
	}
	const systemPrompt = workerPromptBody();
	let attempt = 1;
	let escalatedFrom: string | null = null;
	let cumulativeUsage = emptyWorkerUsage();
	let child: ChildResult;
	while (true) {
		onProgress?.({ status: "running", resolvedPreset: route.resolvedPreset, modelAlias: route.modelAlias, modelId: route.modelId, thinking: route.thinking, attempt, phase: attempt > 1 ? `升级后重试 ${attempt}` : "启动 Worker" });
		const previousUsage = { ...cumulativeUsage };
		child = await runPiWorker(
			task,
			route,
			cwd,
			systemPrompt,
			buildTaskPrompt(task, route, before),
			signal,
			task.timeoutMs ?? config.defaultTimeoutMs,
			config.maxOutputBytes,
			config.maxConcurrentWorkers,
			(progress) => onProgress?.({ phase: progress.phase, activities: progress.activities, toolCalls: progress.toolCalls, usage: addWorkerUsage(previousUsage, progress.usage) }),
		);
		cumulativeUsage = addWorkerUsage(cumulativeUsage, child.usage);
		const interim = before ? await changedSince(before) : { changed: [] as string[] };
		const canRetry = !runtimeShuttingDown && attempt <= config.maxAutomaticRetries && !child.aborted && !child.timedOut && !isBlockedFailure(child) && interim.changed.length === 0 && (child.exitCode !== 0 || Boolean(child.errorMessage));
		const upgraded = canRetry ? escalationRoute(route, task, config, ctx) : null;
		if (!upgraded) break;
		escalatedFrom = `${route.modelAlias} + ${route.thinking}`;
		route = upgraded;
		attempt++;
	}
	onProgress?.({ phase: "校验结果与修改范围", activities: child.activities, toolCalls: child.toolCalls, usage: cumulativeUsage });
	const parsed = parseStructuredResult(child.assistantText);
	const delta = before ? await changedSince(before) : { changed: [] as string[] };
	const changedFiles = delta.changed;
	const forbidden = changedFiles.filter((file) => matchesAny(file, task.forbiddenPaths ?? []));
	const outsideAllowed = WRITE_MODES.has(task.mode) ? changedFiles.filter((file) => !matchesAny(file, task.allowedPaths ?? [])) : changedFiles;
	const actualMismatch = Boolean(child.actualProvider && child.actualModel && `${child.actualProvider}/${child.actualModel}` !== route.modelId);
	let status: "completed" | "blocked" | "failed" = parsed?.status === "completed" || parsed?.status === "blocked" || parsed?.status === "failed" ? parsed.status : "failed";
	const risks = Array.isArray(parsed?.risks) ? parsed.risks : [];
	if (runtimeShuttingDown || child.aborted || child.timedOut || child.exitCode !== 0 || child.errorMessage || !parsed || actualMismatch || forbidden.length || outsideAllowed.length) status = isBlockedFailure(child) ? "blocked" : "failed";
	if (route.resolvedPreset === "critical" && route.degraded) status = "blocked";
	if (forbidden.length) risks.push(`修改了 forbiddenPaths: ${forbidden.join(", ")}`);
	if (outsideAllowed.length) risks.push(`${READ_ONLY_MODES.has(task.mode) ? "只读模式发生写入" : "修改超出 allowedPaths"}: ${outsideAllowed.join(", ")}`);
	if (actualMismatch) risks.push(`实际模型 ${child.actualProvider}/${child.actualModel} 与请求 ${route.modelId} 不一致`);
	if (child.truncated) risks.push("Worker 事件输出超过上限，已截断");
	const summary = Array.isArray(parsed?.summary) ? parsed.summary : [child.aborted ? "Worker 已取消" : child.timedOut ? "Worker 超时" : child.errorMessage || child.stderr || "Worker 未返回可解析 JSON"];
	return {
		status,
		execution: { ...baseExecution(task, route, attempt, escalatedFrom, warnings), actual_model_id: child.actualProvider && child.actualModel ? `${child.actualProvider}/${child.actualModel}` : route.modelId, actual_thinking: route.thinking, usage: cumulativeUsage, exit_code: child.exitCode, timed_out: child.timedOut, cancelled: child.aborted || runtimeShuttingDown },
		summary,
		changed_files: changedFiles,
		validation: Array.isArray(parsed?.validation) ? parsed.validation : [],
		acceptance: Array.isArray(parsed?.acceptance) ? parsed.acceptance : [],
		findings: Array.isArray(parsed?.findings) ? parsed.findings : [],
		risks,
		out_of_scope: Array.isArray(parsed?.out_of_scope) ? parsed.out_of_scope : [],
		recommended_next_action: Array.isArray(parsed?.recommended_next_action) ? parsed.recommended_next_action : ["主 Agent 检查实际 diff 和验证证据"],
	};
}

function uiStatusIcon(status: WorkerUiStatus, theme: Theme): string {
	if (status === "queued") return theme.fg("dim", "○");
	if (status === "running") return theme.fg("warning", "◌");
	if (status === "completed") return theme.fg("success", "✓");
	if (status === "blocked") return theme.fg("warning", "!");
	return theme.fg("error", "✗");
}

function uiActivityLine(activity: WorkerUiActivity, theme: Theme): string {
	if (activity.type === "thinking") return `${theme.fg("mdLink", "!")} ${theme.fg("dim", uiSnippet(activity.detail ?? activity.label, 150))}`;
	const icon = activity.status === "running" ? theme.fg("warning", "→") : activity.status === "completed" ? theme.fg("success", "✓") : theme.fg("error", "✗");
	const label = activity.type === "tool" ? theme.fg("accent", activity.label) : theme.fg("muted", activity.label);
	return `${icon} ${label}${activity.detail ? theme.fg("dim", ` · ${uiSnippet(activity.detail, 96)}`) : ""}`;
}

function uiDuration(task: WorkerUiTask): string | undefined {
	if (!task.startedAt) return undefined;
	const elapsed = (task.finishedAt ?? Date.now()) - task.startedAt;
	if (elapsed < 1000) return `${elapsed}ms`;
	if (elapsed < 60_000) return `${(elapsed / 1000).toFixed(elapsed < 10_000 ? 1 : 0)}s`;
	return `${Math.floor(elapsed / 60_000)}m${Math.floor((elapsed % 60_000) / 1000)}s`;
}

function resultArray(result: Record<string, any> | undefined, key: string): any[] {
	return result && Array.isArray(result[key]) ? result[key] : [];
}

function renderWorkerDetails(details: WorkerUiDetails, expanded: boolean, theme: Theme) {
	const failed = details.tasks.some((task) => task.status === "failed");
	const blocked = details.tasks.some((task) => task.status === "blocked");
	const running = details.tasks.some((task) => task.status === "running" || task.status === "queued");
	const icon = running ? theme.fg("warning", "◌") : failed ? theme.fg("error", "✗") : blocked ? theme.fg("warning", "!") : theme.fg("success", "✓");
	const title = details.total === 1 ? "Worker" : `Workers ×${details.total}`;
	const totalTools = details.tasks.reduce((sum, task) => sum + task.toolCalls, 0);
	const totalUsage = details.tasks.reduce((usage, task) => addWorkerUsage(usage, task.usage), emptyWorkerUsage());
	const changed = details.tasks.reduce((sum, task) => sum + resultArray(task.result, "changed_files").length, 0);
	const validation = details.tasks.flatMap((task) => resultArray(task.result, "validation"));
	const validationPassed = validation.filter((item) => item?.result === "passed").length;
	const footer = [
		`并发 ${details.limit}`,
		workerUsageText(totalUsage) || undefined,
		totalTools ? `${totalTools} 次工具` : undefined,
		changed ? `${changed} 个文件` : undefined,
		validation.length ? `验证 ${validationPassed}/${validation.length}` : undefined,
	].filter(Boolean).join(" · ");

	if (!expanded) {
		let text = "";
		for (const [position, task] of details.tasks.entries()) {
			if (position > 0) text += `\n${theme.fg("borderMuted", "─".repeat(24))}\n`;
			const duration = uiDuration(task);
			const usage = workerUsageText(task.usage, task.status === "running");
			const preset = task.resolvedPreset ?? task.requestedPreset;
			const runtime = [preset, usage, duration].filter(Boolean).join(" · ");
			text += `${uiStatusIcon(task.status, theme)} ${theme.fg("accent", `#${task.index + 1} ${task.mode}`)}${runtime ? theme.fg("muted", ` · ${runtime}`) : ""}`;
			text += `\n  ${theme.fg("dim", uiSnippet(task.objective, 110))}`;
			for (const activity of task.activities.slice(-3)) {
				text += `\n  ${theme.fg("dim", `#${task.index + 1}`)} ${uiActivityLine(activity, theme)}`;
			}
		}
		text += `\n\n${theme.fg("muted", "展开可查看路由、活动、验证和结果")}`;
		return new Text(text, 0, 0);
	}

	const container = new Container();
	container.addChild(new Text(`${icon} ${theme.fg("toolTitle", theme.bold(title))}`, 0, 0));
	if (footer) container.addChild(new Text(theme.fg("muted", footer), 0, 0));
	for (const task of details.tasks) {
		container.addChild(new Spacer(1));
		container.addChild(new Text(theme.fg("borderMuted", "─".repeat(12)), 0, 0));
		const duration = uiDuration(task);
		container.addChild(new Text(`${uiStatusIcon(task.status, theme)} ${theme.fg("accent", `#${task.index + 1} ${task.mode}`)}${duration ? theme.fg("dim", ` · ${duration}`) : ""}`, 0, 0));
		container.addChild(new Text(theme.fg("text", uiSnippet(task.objective, UI_OBJECTIVE_CAP)), 0, 0));
		const route = [
			task.resolvedPreset ? `档位 ${task.resolvedPreset}` : undefined,
			task.modelId ? `模型 ${task.modelId}` : task.modelAlias ? `模型 ${task.modelAlias}` : undefined,
			task.thinking ? `思考 ${task.thinking}` : undefined,
			task.attempt ? `尝试 ${task.attempt}` : undefined,
		].filter(Boolean).join(" · ");
		if (route) container.addChild(new Text(theme.fg("muted", route), 0, 0));
		const usage = workerUsageText(task.usage, task.status === "running");
		const usageDetails = [
			usage,
			task.usage.cacheRead ? `缓存读 ${formatTokenCount(task.usage.cacheRead)}` : undefined,
			task.usage.cacheWrite ? `缓存写 ${formatTokenCount(task.usage.cacheWrite)}` : undefined,
			task.usage.contextTokens ? `上下文 ${formatTokenCount(task.usage.contextTokens)}` : undefined,
		].filter(Boolean).join(" · ");
		if (usageDetails) container.addChild(new Text(theme.fg("muted", usageDetails), 0, 0));
		container.addChild(new Text(theme.fg("dim", `阶段：${task.phase}`), 0, 0));
		const activities = task.activities.slice(-UI_RECENT_ACTIVITY_LIMIT);
		if (activities.length) {
			container.addChild(new Text(theme.fg("muted", "最近动态："), 0, 0));
			container.addChild(new Text(activities.map((activity) => `  ${uiActivityLine(activity, theme)}`).join("\n"), 0, 0));
		}
		const summary = resultArray(task.result, "summary").map(String).filter(Boolean);
		if (summary.length) container.addChild(new Text(summary.map((item) => `${theme.fg("mdListBullet", "-")} ${sanitizeUiText(item)}`).join("\n"), 0, 0));
		const changedFiles = resultArray(task.result, "changed_files").map(String);
		if (changedFiles.length) container.addChild(new Text(theme.fg("muted", `修改：${changedFiles.map((file) => uiSnippet(file, 80)).join(", ")}`), 0, 0));
		const taskValidation = resultArray(task.result, "validation");
		if (taskValidation.length) {
			container.addChild(new Text(theme.fg("muted", "验证："), 0, 0));
			container.addChild(new Text(taskValidation.slice(0, 8).map((item) => {
				const ok = item?.result === "passed";
				const marker = ok ? theme.fg("success", "✓") : item?.result === "not_run" ? theme.fg("warning", "-") : theme.fg("error", "✗");
				return `  ${marker} ${uiSnippet(String(item?.command ?? "验证"), 100)}${item?.details ? theme.fg("dim", ` · ${uiSnippet(String(item.details), 100)}`) : ""}`;
			}).join("\n"), 0, 0));
		}
		const acceptance = resultArray(task.result, "acceptance");
		if (acceptance.length) {
			const passed = acceptance.filter((item) => item?.result === "passed").length;
			container.addChild(new Text(theme.fg(passed === acceptance.length ? "success" : "warning", `验收：${passed}/${acceptance.length}`), 0, 0));
		}
		const findings = resultArray(task.result, "findings");
		const risks = resultArray(task.result, "risks");
		if (findings.length || risks.length) container.addChild(new Text(theme.fg(risks.length ? "warning" : "muted", `发现 ${findings.length} · 风险 ${risks.length}`), 0, 0));
	}
	return container;
}

async function mapWithLimit<T, R>(items: T[], limit: number, fn: (item: T, index: number) => Promise<R>): Promise<R[]> {
	const results = new Array<R>(items.length);
	let cursor = 0;
	async function runner() {
		while (true) {
			const index = cursor++;
			if (index >= items.length) return;
			try { results[index] = await fn(items[index], index); }
			catch (error) { results[index] = { status: "failed", summary: [error instanceof Error ? error.message : String(error)] } as R; }
		}
	}
	await Promise.all(Array.from({ length: Math.min(limit, items.length) }, runner));
	return results;
}

export default function workerExtension(pi: ExtensionAPI) {
	if (Number(process.env.PI_WORKER_DEPTH || "0") >= 1) return;
	runtimeShuttingDown = false;
	pi.on("session_shutdown", async () => { runtimeShuttingDown = true; killAllChildren(); });
	pi.registerTool({
		name: "worker",
		label: "Worker",
		description: TOOL_DESCRIPTION,
		parameters: InputSchema,
		async execute(_toolCallId, input: WorkerToolInput, signal, onUpdate, ctx) {
			if (Boolean(input.task) === Boolean(input.tasks)) {
				return { content: [{ type: "text", text: "worker 参数错误：task 和 tasks 必须二选一" }], details: { status: "failed" } };
			}
			const tasks = input.task ? [input.task] : input.tasks!;
			const taskErrors = tasks.map((task) => validateTask(task, ctx.cwd));
			if (taskErrors.some((errors) => errors.length)) {
				const details = taskErrors.map((errors, index) => ({ index, errors }));
				return { content: [{ type: "text", text: JSON.stringify({ status: "failed", validation_errors: details }, null, 2) }], details };
			}
			let loaded: Awaited<ReturnType<typeof loadRoutingConfig>>;
			try { loaded = await loadRoutingConfig(ctx); }
			catch (error) {
				return { content: [{ type: "text", text: JSON.stringify({ status: "blocked", summary: [error instanceof Error ? error.message : String(error)] }, null, 2) }], details: { status: "blocked" } };
			}
			if (!loaded.config.automaticDelegationEnabled && !input.manual) {
				return { content: [{ type: "text", text: JSON.stringify({ status: "blocked", summary: ["自动委派已在 worker-routing.json 中关闭；手动调用请设置 manual: true"] }, null, 2) }], details: { status: "blocked" } };
			}
			let limit = loaded.config.maxConcurrentWorkers;
			for (let i = 0; i < tasks.length; i++) for (let j = i + 1; j < tasks.length; j++) if (writeTasksMayOverlap(tasks[i], tasks[j])) limit = 1;
			const uiDetails: WorkerUiDetails = {
				kind: "worker-ui",
				startedAt: Date.now(),
				limit,
				total: tasks.length,
				completed: 0,
				tasks: tasks.map((task, index) => ({
					index,
					mode: task.mode,
					objective: task.objective,
					status: "queued",
					requestedPreset: task.preset ?? "auto",
					attempt: 0,
					phase: "等待执行",
					activities: [],
					toolCalls: 0,
					usage: emptyWorkerUsage(),
				})),
			};
			const emitUi = (message: string) => onUpdate?.({
				content: [{ type: "text", text: message }],
				details: cloneUiDetails(uiDetails),
			});
			emitUi(`准备执行 ${tasks.length} 个 Worker 任务`);
			const results = await mapWithLimit(tasks, limit, async (task, index) => {
				const uiTask = uiDetails.tasks[index]!;
				uiTask.status = "running";
				uiTask.startedAt = Date.now();
				uiTask.phase = "准备任务";
				emitUi(`#${index + 1} ${task.mode} 开始`);
				let item: Record<string, any>;
				try {
					item = await executeTask(task, loaded.config, loaded.warnings, ctx, signal, (patch) => {
						Object.assign(uiTask, patch);
						emitUi(`#${index + 1} ${uiTask.phase}`);
					});
				} catch (error) {
					item = {
						status: "failed",
						summary: [error instanceof Error ? error.message : String(error)],
						changed_files: [], validation: [], acceptance: [], findings: [], risks: [], out_of_scope: [], recommended_next_action: ["主 Agent 检查异常"],
					};
				}
				uiTask.result = item;
				uiTask.status = item.status === "completed" || item.status === "blocked" || item.status === "failed" ? item.status : "failed";
				uiTask.phase = uiTask.status === "completed" ? "已完成" : uiTask.status === "blocked" ? "已阻塞" : "执行失败";
				uiTask.finishedAt = Date.now();
				const execution = item.execution;
				if (execution) {
					uiTask.resolvedPreset = execution.resolved_preset ?? uiTask.resolvedPreset;
					uiTask.modelAlias = execution.resolved_model_alias ?? uiTask.modelAlias;
					uiTask.modelId = execution.actual_model_id ?? execution.resolved_model_id ?? uiTask.modelId;
					uiTask.thinking = execution.actual_thinking ?? execution.resolved_thinking ?? uiTask.thinking;
					uiTask.attempt = execution.attempt ?? uiTask.attempt;
					if (execution.usage) uiTask.usage = { ...execution.usage };
				}
				uiDetails.completed++;
				emitUi(`#${index + 1} ${uiTask.phase}`);
				return item;
			});
			const payload: Record<string, any> = input.task ? results[0] : { status: results.every((item) => item.status === "completed") ? "completed" : "partial", results };
			uiDetails.payload = payload;
			uiDetails.finishedAt = Date.now();
			const text = JSON.stringify(payload, null, 2);
			return {
				content: [{ type: "text", text: text.length > loaded.config.maxOutputBytes ? `${text.slice(0, loaded.config.maxOutputBytes)}\n…[worker result truncated]` : text }],
				details: cloneUiDetails(uiDetails),
			};
		},
		renderCall(args, theme) {
			const count = args.task ? 1 : Array.isArray(args.tasks) ? args.tasks.length : 0;
			const title = count > 1 ? `Workers ×${count}` : "Worker";
			return new Text(theme.fg("toolTitle", theme.bold(title)), 0, 0);
		},
		renderResult(result, { expanded }, theme) {
			const details = result.details as WorkerUiDetails | undefined;
			if (details?.kind === "worker-ui" && details.tasks.length) return renderWorkerDetails(details, expanded, theme);
			const text = result.content.find((item) => item.type === "text");
			return new Text(text?.type === "text" ? text.text : "Worker 无输出", 0, 0);
		},
	});
}

if (!(globalThis as any).__piWorkerExitHookInstalled) {
	(globalThis as any).__piWorkerExitHookInstalled = true;
	process.once("exit", () => killAllChildren(true));
}
