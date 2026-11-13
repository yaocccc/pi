import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { PRESETS } from "../../worker/config.ts";
import type { WorkerCapability } from "../../worker/api.ts";
import type { SettingsErrorData } from "../../worker/settings.ts";

type GetModels = () => ReturnType<WorkerCapability["getModels"]>;
import type { WorkerPreset, RoutingConfig } from "../../worker/types.ts";

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

const configurePreset = async (ctx: ExtensionContext, draft: RoutingConfig, preset: WorkerPreset, getModels: GetModels): Promise<void> => {
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

		const available = getModels();
		if (key === "model") {
			const modelIds = [...new Set(available.map((model) => model.id))].sort((a, b) => a.localeCompare(b));
			if (!modelIds.includes(setting.model)) modelIds.unshift(setting.model);
			const availableIds = new Set(available.map((model) => model.id));
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
			const model = available.find((item) => item.id === modelId);
			if (model) {
				if (!model.thinking.includes(setting.thinking)) {
					ctx.ui.notify(`模型不支持当前 Thinking ${setting.thinking}；已保留原值，请显式选择支持的等级`, "warning");
				}
			}
		} else if (key === "thinking") {
			const model = available.find((item) => item.id === setting.model);
			const levels = model?.thinking ?? [];
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

const saveErrorMessage = (cause: unknown): string => {
	// Services can live in separate extension modules: use structured data,
	// not instanceof, and leave all validation to the capability.
	const data = cause && typeof cause === "object" && "data" in cause ? cause.data as SettingsErrorData : undefined;
	const preset = PRESET_LABELS[data?.field as WorkerPreset] ?? data?.field ?? "";
	if (data?.code === "model-unavailable") return `${preset} 模型当前不可用：${data.model}`;
	if (data?.code === "thinking-unsupported") return `${preset} 模型不支持 ${data.thinking}`;
	return cause instanceof Error ? cause.message : String(cause);
};

export const configureWorkerSettings = async (
	ctx: ExtensionContext,
	current: RoutingConfig,
	save: (value: unknown) => RoutingConfig,
	getModels: GetModels,
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
			try { return save(draft); }
			catch (cause) { ctx.ui.notify(saveErrorMessage(cause), "warning"); }
			continue;
		}
		if (key && PRESETS.includes(key as WorkerPreset)) {
			await configurePreset(ctx, draft, key as WorkerPreset, getModels);
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
