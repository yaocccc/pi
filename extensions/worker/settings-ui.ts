import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { PRESETS, THINKING_LEVELS, supportedThinking, validateConfig } from "./config.ts";
import { saveWorkerSettings } from "./settings.ts";
import type { WorkerPreset, RoutingConfig } from "./types";

type MenuKey = WorkerPreset | "maxConcurrentWorkers" | "defaultTimeoutMs" | "maxOutputBytes" | "save" | "cancel";

const PRESET_LABELS: Record<WorkerPreset, string> = {
	fast: "Fast",
	normal: "Normal",
	deep: "Deep",
};

const formatDuration = (milliseconds: number): string => {
	const seconds = milliseconds / 1_000;
	return seconds >= 60 && seconds % 60 === 0 ? `${seconds / 60}m` : `${seconds}s`;
};
const formatOutput = (bytes: number): string => `${Math.round(bytes / 1_024)} KiB`;

const configurePreset = async (ctx: ExtensionContext, draft: RoutingConfig, preset: WorkerPreset): Promise<void> => {
	while (true) {
		const setting = draft[preset];
		const items = [
			{ key: "model", label: `模型 · ${setting.model}` },
			{ key: "thinking", label: `Thinking · ${setting.thinking}` },
			{ key: "back", label: "返回" },
		] as const;
		const selected = await ctx.ui.select(`${PRESET_LABELS[preset]} 设置`, items.map((item) => item.label));
		if (!selected) return;
		const key = items.find((item) => item.label === selected)?.key;
		if (key === "back") return;

		const available = ctx.modelRegistry.getAvailable();
		if (key === "model") {
			const modelIds = [...new Set(available.map((model) => `${model.provider}/${model.id}`))].sort((a, b) => a.localeCompare(b));
			if (!modelIds.includes(setting.model)) modelIds.unshift(setting.model);
			const availableIds = new Set(available.map((model) => `${model.provider}/${model.id}`));
			const options = modelIds.map((modelId) => [
				modelId === setting.model ? "●" : "○",
				modelId,
				availableIds.has(modelId) ? "" : " · 当前不可用",
			].join(" ").trimEnd());
			const chosen = await ctx.ui.select(`${PRESET_LABELS[preset]} 模型`, options);
			if (!chosen) continue;
			const modelId = modelIds[options.indexOf(chosen)];
			if (!modelId) continue;
			setting.model = modelId;
			const model = available.find((item) => `${item.provider}/${item.id}` === modelId);
			if (model) {
				if (!supportedThinking(model).includes(setting.thinking)) {
					ctx.ui.notify(`模型不支持当前 Thinking ${setting.thinking}；已保留原值，请显式选择支持的等级`, "warning");
				}
			}
		} else if (key === "thinking") {
			const model = available.find((item) => `${item.provider}/${item.id}` === setting.model);
			const levels = model ? supportedThinking(model) : [...THINKING_LEVELS];
			if (!levels.length) {
				ctx.ui.notify("当前模型不支持 Worker 所需的 Thinking", "warning");
				continue;
			}
			const options = levels.map((level) => `${level === setting.thinking ? "●" : "○"} ${level}`);
			const chosen = await ctx.ui.select(`${PRESET_LABELS[preset]} Thinking`, options);
			if (chosen) setting.thinking = levels[options.indexOf(chosen)] ?? setting.thinking;
		}
	}
};

const inputInteger = async (
	ctx: ExtensionContext,
	title: string,
	placeholder: string,
	current: number,
	minimum: number,
	maximum: number,
): Promise<number> => {
	const entered = (await ctx.ui.input(title, placeholder))?.trim();
	if (!entered) return current;
	const value = Number(entered);
	if (!Number.isInteger(value) || value < minimum || value > maximum) {
		ctx.ui.notify(`请输入 ${minimum}-${maximum} 之间的整数`, "warning");
		return current;
	}
	return value;
};

const menuItems = (config: RoutingConfig): Array<{ key: MenuKey; label: string }> => [
	...PRESETS.map((preset) => ({
		key: preset,
		label: `${PRESET_LABELS[preset]} · ${config[preset].model} · ${config[preset].thinking}`,
	})),
	{ key: "maxConcurrentWorkers", label: `并发上限 · ${config.maxConcurrentWorkers}` },
	{ key: "defaultTimeoutMs", label: `默认超时 · ${formatDuration(config.defaultTimeoutMs)}` },
	{ key: "maxOutputBytes", label: `最大输出 · ${formatOutput(config.maxOutputBytes)}` },
	{ key: "save", label: "保存并退出" },
	{ key: "cancel", label: "取消" },
];

const validateModels = (ctx: ExtensionContext, config: RoutingConfig): string | undefined => {
	const available = ctx.modelRegistry.getAvailable();
	for (const preset of PRESETS) {
		const setting = config[preset];
		const model = available.find((item) => `${item.provider}/${item.id}` === setting.model);
		if (!model) return `${PRESET_LABELS[preset]} 模型当前不可用：${setting.model}`;
		if (!supportedThinking(model).includes(setting.thinking)) {
			return `${PRESET_LABELS[preset]} 模型不支持 ${setting.thinking}`;
		}
	}
	return undefined;
};

export const configureWorkerSettings = async (
	ctx: ExtensionContext,
	current: RoutingConfig,
	configPath: string,
): Promise<RoutingConfig | undefined> => {
	const draft = structuredClone(current);
	const original = JSON.stringify(current);

	while (true) {
		const dirty = JSON.stringify(draft) !== original;
		const items = menuItems(draft);
		const selected = await ctx.ui.select(`Worker 设置${dirty ? "  ·  未保存" : ""}`, items.map((item) => item.label));
		if (!selected) {
			if (!dirty || await ctx.ui.confirm("放弃更改？", "当前修改尚未保存。")) return undefined;
			continue;
		}
		const key = items.find((item) => item.label === selected)?.key;
		if (key === "cancel") {
			if (!dirty || await ctx.ui.confirm("放弃更改？", "当前修改尚未保存。")) return undefined;
			continue;
		}
		if (key === "save") {
			const validated = validateConfig(draft).config;
			const modelError = validateModels(ctx, validated);
			if (modelError) {
				ctx.ui.notify(modelError, "warning");
				continue;
			}
			return saveWorkerSettings(ctx, validated, configPath);
		}
		if (key && PRESETS.includes(key as WorkerPreset)) {
			await configurePreset(ctx, draft, key as WorkerPreset);
		} else if (key === "maxConcurrentWorkers") {
			draft.maxConcurrentWorkers = await inputInteger(ctx, "并发上限", `1-16，当前：${draft.maxConcurrentWorkers}`, draft.maxConcurrentWorkers, 1, 16);
		} else if (key === "defaultTimeoutMs") {
			const seconds = await inputInteger(ctx, "默认超时（秒）", `1-3600，当前：${draft.defaultTimeoutMs / 1_000}`, draft.defaultTimeoutMs / 1_000, 1, 3_600);
			draft.defaultTimeoutMs = seconds * 1_000;
		} else if (key === "maxOutputBytes") {
			const kibibytes = await inputInteger(ctx, "最大输出（KiB）", `8-1024，当前：${draft.maxOutputBytes / 1_024}`, draft.maxOutputBytes / 1_024, 8, 1_024);
			draft.maxOutputBytes = kibibytes * 1_024;
		}
	}
};
