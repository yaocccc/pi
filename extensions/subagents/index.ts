import { StringEnum } from "@earendil-works/pi-ai";
import {
	type ExtensionAPI,
	type ExtensionCommandContext,
	getMarkdownTheme,
	type Theme,
} from "@earendil-works/pi-coding-agent";
import { Container, Markdown, Spacer, Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";
import { loadAgents } from "./agents.ts";
import {
	getFailureMessage,
	getFinalOutput,
	isFailedResult,
	makeDetails,
	runAgent,
	sanitizeDisplaySnippet,
	sanitizeDisplayText,
} from "./runner.ts";
import {
	AGENT_NAMES,
	type AgentName,
	type AgentRunResult,
	type SubagentDetails,
	type ToolActivity,
} from "./types/index.ts";
import { readSubagentText, selectAgent } from "./ui.ts";
import { buildSingleAgentPrompt, buildWorkflowPrompt, type WorkflowKind } from "./workflows.ts";

const MODEL_OUTPUT_CAP = 50 * 1024;
const PREVIOUS_OUTPUT_CAP = 100 * 1024;
const UNSUPPORTED_CHILD_TOOLS = new Set(["ask_question", "plan_set_todos", "plan_check_result", "subagent"]);

function resolveChildExtensionPaths(pi: ExtensionAPI, toolNames: string[]): string[] {
	const invalid = toolNames.filter((name) => UNSUPPORTED_CHILD_TOOLS.has(name));
	if (invalid.length) throw new Error(`子代理不允许使用工具：${invalid.join(", ")}`);

	const configured = new Map(pi.getAllTools().map((tool) => [tool.name, tool]));
	const missing = toolNames.filter((name) => !configured.has(name));
	if (missing.length) throw new Error(`子代理工具未安装：${missing.join(", ")}`);

	return [
		...new Set(
			toolNames
				.map((name) => configured.get(name)?.sourceInfo?.path)
				.filter((sourcePath): sourcePath is string => Boolean(sourcePath && !sourcePath.startsWith("<builtin:"))),
		),
	];
}
const EXPANDED_OUTPUT_CAP = 20 * 1024;
const EXPANDED_RAW_SCAN_CAP = 40 * 1024;
const EXPANDED_TASK_CAP = 1000;

const AgentNameSchema = StringEnum(AGENT_NAMES, {
	description: "子代理类型：scout、planner、worker 或 reviewer",
});

const ChainItemSchema = Type.Object({
	agent: AgentNameSchema,
	task: Type.String({ description: "当前步骤任务，可用 {previous} 引用上一步输出" }),
});

const SubagentParams = Type.Object({
	agent: Type.Optional(AgentNameSchema),
	task: Type.Optional(Type.String({ description: "单个子代理任务" })),
	chain: Type.Optional(Type.Array(ChainItemSchema, { minItems: 1, maxItems: 8 })),
});

function truncateUtf8(text: string, maxBytes: number, note: string): string {
	const bytes = Buffer.from(text, "utf8");
	if (bytes.length <= maxBytes) return text;
	let end = maxBytes;
	while (end > 0 && (bytes[end]! & 0xc0) === 0x80) end--;
	return `${bytes.subarray(0, end).toString("utf8")}\n\n[${note}，完整内容保留在工具详情中]`;
}

function formatTokens(count: number): string {
	if (count < 1000) return String(count);
	if (count < 1_000_000) return `${(count / 1000).toFixed(count < 10_000 ? 1 : 0)}k`;
	return `${(count / 1_000_000).toFixed(1)}M`;
}

function thinkingLabel(thinking?: string): string | undefined {
	if (!thinking) return undefined;
	const labels: Record<string, string> = {
		off: "关闭",
		minimal: "最低",
		low: "低",
		medium: "中",
		high: "高",
		xhigh: "极高",
		max: "最高",
	};
	return labels[thinking] ?? thinking;
}

function usageText(result: AgentRunResult): string {
	const parts: string[] = [];
	if (result.usage.turns) parts.push(`${result.usage.turns} 轮`);
	if (result.usage.input) parts.push(`↑${formatTokens(result.usage.input)}`);
	if (result.usage.output) parts.push(`↓${formatTokens(result.usage.output)}`);
	return parts.join(" · ");
}

function latestActivity(result: AgentRunResult): string {
	const final = getFinalOutput(result.messages);
	if (final) {
		const summary = sanitizeDisplaySnippet(final, 90);
		if (summary) return summary;
	}
	return result.status === "running" ? "执行中…" : "无文本输出";
}

function toolActivityLine(activity: ToolActivity, theme: Theme, expanded: boolean): string {
	const icon =
		activity.status === "running"
			? theme.fg("warning", "→")
			: activity.status === "failed"
				? theme.fg("error", "✗")
				: theme.fg("success", "✓");
	const name = sanitizeDisplaySnippet(activity.name, 24) || "未知工具";
	let text = `${icon} ${theme.fg("accent", name)}`;
	if (activity.input) text += theme.fg("dim", ` · ${sanitizeDisplaySnippet(activity.input, expanded ? 56 : 40)}`);
	if (expanded && activity.output) {
		text += theme.fg(
			activity.status === "failed" ? "error" : "dim",
			` — ${sanitizeDisplaySnippet(activity.output, 64)}`,
		);
	}
	return text;
}

type RecentActivity =
	| { type: "tool"; activity: ToolActivity }
	| { type: "thinking"; text: string };

function recentThinkingLines(value: string, expanded: boolean): string[] {
	const lineWidth = expanded ? 160 : 120;
	const segments: string[] = [];
	for (const rawLine of sanitizeDisplayText(value).split(/\r?\n/)) {
		const trimmed = rawLine.trim();
		if (!trimmed) continue;
		const tail = trimmed.length > lineWidth * 3 ? `…${trimmed.slice(-(lineWidth * 3 - 1))}` : trimmed;
		const safe = sanitizeDisplaySnippet(tail, lineWidth * 3);
		for (let offset = 0; offset < safe.length; offset += lineWidth) {
			segments.push(safe.slice(offset, offset + lineWidth));
		}
	}
	return segments.slice(-3);
}

function recentActivityLines(result: AgentRunResult, theme: Theme, expanded: boolean): string[] {
	const timeline: RecentActivity[] = [];
	const toolsById = new Map((result.toolActivities ?? []).map((activity) => [activity.id, activity]));
	const seenToolIds = new Set<string>();
	let lastRecordedThinking = "";

	for (const message of result.messages) {
		if (message.role !== "assistant") continue;
		const thinkingParts: string[] = [];
		for (const part of message.content) {
			if (part.type === "thinking") {
				if (part.thinking.trim()) thinkingParts.push(part.thinking.trim());
				for (const line of recentThinkingLines(part.thinking, expanded)) {
					timeline.push({ type: "thinking", text: line });
				}
			} else if (part.type === "toolCall") {
				seenToolIds.add(part.id);
				timeline.push({
					type: "tool",
					activity: toolsById.get(part.id) ?? { id: part.id, name: part.name, status: "running" },
				});
			}
		}
		if (thinkingParts.length) lastRecordedThinking = thinkingParts.join("\n\n");
	}

	for (const activity of result.toolActivities ?? []) {
		if (!seenToolIds.has(activity.id)) timeline.push({ type: "tool", activity });
	}
	if (result.latestThinking?.trim() && result.latestThinking.trim() !== lastRecordedThinking.trim()) {
		for (const line of recentThinkingLines(result.latestThinking, expanded)) {
			timeline.push({ type: "thinking", text: line });
		}
	}

	return timeline.slice(-5).map((activity) =>
		activity.type === "tool"
			? toolActivityLine(activity.activity, theme, expanded)
			: `${theme.fg("mdLink", "?")} ${theme.fg("dim", activity.text)}`,
	);
}

function statusIcon(result: AgentRunResult, theme: Theme): string {
	if (result.status === "running") return theme.fg("warning", "◌");
	if (isFailedResult(result)) return theme.fg("error", "✗");
	return theme.fg("success", "✓");
}

async function requestTask(
	kind: WorkflowKind,
	args: string,
	ctx: ExtensionCommandContext,
): Promise<string | undefined> {
	const provided = args.trim();
	if (provided) return provided;
	if (!ctx.hasUI) {
		ctx.ui.notify(`请使用 /subagents-${kind} <任务描述>`, "error");
		return undefined;
	}

	const titles: Record<WorkflowKind, string> = {
		feat: "输入功能需求",
		fix: "输入问题或报错",
		review: "输入审查目标",
	};
	const placeholders: Record<WorkflowKind, string> = {
		feat: "描述目标、范围和验收标准",
		fix: "描述现象、预期行为和复现方式",
		review: "例如：审查当前 git diff",
	};
	const input =
		ctx.mode === "tui"
			? await readSubagentText(ctx, `/subagents-${kind}：${titles[kind]}，Enter 提交，Esc 取消`)
			: await ctx.ui.input(titles[kind], placeholders[kind]);
	const task = input?.trim();
	if (!task) {
		ctx.ui.notify("已取消", "info");
		return undefined;
	}
	return task;
}

const AGENT_TASK_PLACEHOLDERS: Record<AgentName, string> = {
	scout: "例如：梳理认证模块的入口、调用链和风险",
	planner: "例如：为登录流程重构制定实施计划",
	worker: "例如：实现已确认的缓存方案并完成测试",
	reviewer: "例如：审查当前 git diff",
};

async function requestAgentTask(
	agent: AgentName,
	args: string,
	ctx: ExtensionCommandContext,
): Promise<string | undefined> {
	const provided = args.trim();
	if (provided) return provided;
	const input =
		ctx.mode === "tui"
			? await readSubagentText(ctx, `${agent}：输入任务，Enter 提交，Esc 取消`)
			: await ctx.ui.input(`输入 ${agent} 任务`, AGENT_TASK_PLACEHOLDERS[agent]);
	const task = input?.trim();
	if (!task) {
		ctx.ui.notify("已取消", "info");
		return undefined;
	}
	return task;
}

function sendWorkflowPrompt(pi: ExtensionAPI, ctx: ExtensionCommandContext, prompt: string): void {
	if (ctx.isIdle()) pi.sendUserMessage(prompt);
	else pi.sendUserMessage(prompt, { deliverAs: "followUp" });
}

function parseHeadlessAgentTask(args: string): { agent: AgentName; task: string } | undefined {
	const match = args.trim().match(/^(\S+)\s+([\s\S]+)$/);
	if (!match || !AGENT_NAMES.includes(match[1] as AgentName)) return undefined;
	return { agent: match[1] as AgentName, task: match[2]!.trim() };
}

export default function (pi: ExtensionAPI) {
	const agents = loadAgents();

	pi.registerTool({
		name: "subagent",
		label: "子代理",
		description: [
			"把任务交给隔离上下文中的中文子代理。",
			"仅提供四种角色：scout 负责侦察，planner 负责计划，worker 负责实现，reviewer 负责只读审查。",
			"单任务使用 agent + task；顺序工作流使用 chain，并以 {previous} 传递上一步结果。",
			"同一工作目录只允许 worker 写入；reviewer 不得修改文件。",
		].join(" "),
		promptSnippet: "使用 scout、planner、worker、reviewer 执行单任务或顺序子代理工作流",
		promptGuidelines: [
			"Use subagent only with scout, planner, worker, or reviewer; use exactly one of single mode (agent + task) or chain mode.",
			"Keep only worker as the writer in a subagent chain; reviewer is always review-only.",
		],
		parameters: SubagentParams,

		async execute(_toolCallId, params, signal, onUpdate, ctx) {
			const hasSingle = Boolean(params.agent && params.task?.trim());
			const hasChain = Boolean(params.chain?.length);
			if (Number(hasSingle) + Number(hasChain) !== 1) {
				throw new Error("参数无效：必须且只能提供 agent + task 或 chain");
			}

			const launchModel = ctx.model ? `${ctx.model.provider}/${ctx.model.id}` : undefined;
			const displayModel = ctx.model?.name ?? ctx.model?.id;
			const thinking = ctx.thinkingLevel;

			if (hasSingle && params.agent && params.task) {
				const agent = agents.get(params.agent as AgentName);
				if (!agent) throw new Error(`未知子代理: ${params.agent}`);
				const result = await runAgent({
					cwd: ctx.cwd,
					agent,
					task: params.task.trim(),
					launchModel,
					displayModel,
					thinking,
					extensionPaths: resolveChildExtensionPaths(pi, agent.tools),
					signal,
					onUpdate: (current) => {
						onUpdate?.({
							content: [{ type: "text", text: `${current.agent} ${current.status === "running" ? "执行中" : current.status}` }],
							details: makeDetails("single", [current], 1, displayModel, thinking),
						});
					},
				});
				const output = isFailedResult(result) ? `子代理 ${result.agent} 执行失败：${getFailureMessage(result)}` : getFinalOutput(result.messages) || "子代理未返回文本结果";
				return {
					content: [{ type: "text", text: truncateUtf8(output, MODEL_OUTPUT_CAP, "输出已截断") }],
					details: makeDetails("single", [result], 1, displayModel, thinking),
				};
			}

			const results: AgentRunResult[] = [];
			let previous = "";
			for (let index = 0; index < params.chain!.length; index++) {
				const step = params.chain![index]!;
				const agent = agents.get(step.agent as AgentName);
				if (!agent) throw new Error(`未知子代理: ${step.agent}`);
				const previousForPrompt = truncateUtf8(previous, PREVIOUS_OUTPUT_CAP, "上一步输出已截断");
				const task = step.task.replaceAll("{previous}", previousForPrompt).trim();
				const result = await runAgent({
					cwd: ctx.cwd,
					agent,
					task,
					step: index + 1,
					launchModel,
					displayModel,
					thinking,
					extensionPaths: resolveChildExtensionPaths(pi, agent.tools),
					signal,
					onUpdate: (current) => {
						onUpdate?.({
							content: [{ type: "text", text: `步骤 ${index + 1}/${params.chain!.length}：${current.agent} 执行中` }],
							details: makeDetails("chain", [...results, current], params.chain!.length, displayModel, thinking),
						});
					},
				});
				results.push(result);
				if (isFailedResult(result)) {
					const failure = `子代理链在步骤 ${index + 1}（${result.agent}）停止：${getFailureMessage(result)}`;
					return {
						content: [{ type: "text", text: truncateUtf8(failure, MODEL_OUTPUT_CAP, "错误输出已截断") }],
						details: makeDetails("chain", results, params.chain!.length, displayModel, thinking),
					};
				}
				previous = getFinalOutput(result.messages);
			}

			const finalOutput = previous || "子代理链已完成，但最后一步未返回文本结果";
			return {
				content: [{ type: "text", text: truncateUtf8(finalOutput, MODEL_OUTPUT_CAP, "输出已截断") }],
				details: makeDetails("chain", results, params.chain!.length, displayModel, thinking),
			};
		},

		renderCall(args, theme) {
			if (args.chain?.length) {
				let text = `${theme.fg("toolTitle", theme.bold("子代理链"))} ${theme.fg("accent", `${args.chain.length} 步`)}`;
				for (const [index, step] of args.chain.entries()) {
					text += `\n  ${theme.fg("muted", `${index + 1}.`)} ${theme.fg("accent", step.agent)}`;
				}
				return new Text(text, 0, 0);
			}
			return new Text(
				`${theme.fg("toolTitle", theme.bold("子代理"))} ${theme.fg("accent", args.agent ?? "未知")}`,
				0,
				0,
			);
		},

		renderResult(result, { expanded }, theme) {
			const details = result.details as SubagentDetails | undefined;
			if (!details?.results.length) {
				const text = result.content.find((item) => item.type === "text");
				return new Text(text?.type === "text" ? text.text : "无输出", 0, 0);
			}

			const completed = details.results.filter((item) => item.status !== "running").length;
			const failed = details.results.some(isFailedResult);
			const running = details.results.some((item) => item.status === "running");
			const icon = running ? theme.fg("warning", "◌") : failed ? theme.fg("error", "✗") : theme.fg("success", "✓");
			const totalSteps = details.totalSteps ?? details.results.length;
			const title = details.mode === "chain" ? `子代理链 ${completed}/${totalSteps}` : `子代理 ${details.results[0]!.agent}`;
			const modelParts = [
				details.model ? `模型：${details.model}` : undefined,
				thinkingLabel(details.thinking) ? `思考强度：${thinkingLabel(details.thinking)}` : undefined,
			].filter(Boolean);

			if (!expanded) {
				let text = `${icon} ${theme.fg("toolTitle", theme.bold(title))}`;
				for (const [index, item] of details.results.entries()) {
					text += `\n${statusIcon(item, theme)} ${theme.fg("accent", item.agent)} ${theme.fg("dim", latestActivity(item))}`;
					if (item.status === "running" || index === details.results.length - 1) {
						for (const line of recentActivityLines(item, theme, false)) text += `\n  ${line}`;
					}
					const usage = usageText(item);
					if (usage) text += `\n  ${theme.fg("dim", usage)}`;
				}
				if (modelParts.length) text += `\n${theme.fg("muted", modelParts.join(" · "))}`;
				text += `\n${theme.fg("muted", "展开可查看完整结果")}`;
				return new Text(text, 0, 0);
			}

			const container = new Container();
			container.addChild(new Text(`${icon} ${theme.fg("toolTitle", theme.bold(title))}`, 0, 0));
			if (modelParts.length) container.addChild(new Text(theme.fg("muted", modelParts.join(" · ")), 0, 0));
			for (const item of details.results) {
				container.addChild(new Spacer(1));
				container.addChild(new Text(`${statusIcon(item, theme)} ${theme.fg("accent", `${item.step ? `步骤 ${item.step} · ` : ""}${item.agent}`)}`, 0, 0));
				container.addChild(
					new Text(theme.fg("dim", `任务：${sanitizeDisplaySnippet(item.task, EXPANDED_TASK_CAP)}`), 0, 0),
				);
				const activityLines = recentActivityLines(item, theme, true);
				if (activityLines.length) {
					container.addChild(new Text(theme.fg("muted", "最近动态："), 0, 0));
					container.addChild(new Text(activityLines.map((line) => `  ${line}`).join("\n"), 0, 0));
				}
				const output = isFailedResult(item) ? getFailureMessage(item) : getFinalOutput(item.messages);
				if (output) {
					const displayOutput = truncateUtf8(
						sanitizeDisplayText(output.slice(0, EXPANDED_RAW_SCAN_CAP)),
						EXPANDED_OUTPUT_CAP,
						"展开内容已截断",
					);
					container.addChild(new Markdown(displayOutput, 0, 0, getMarkdownTheme()));
				}
				const usage = usageText(item);
				if (usage) container.addChild(new Text(theme.fg("dim", usage), 0, 0));
			}
			return container;
		},
	});

	pi.registerCommand("subagents", {
		description: "选择 scout、planner、worker 或 reviewer 并快速启动",
		handler: async (args, ctx) => {
			if (ctx.mode !== "tui") {
				const parsed = parseHeadlessAgentTask(args);
				if (!parsed) {
					ctx.ui.notify("非 TUI 模式请使用：/subagents <agent> <任务描述>", "error");
					return;
				}
				sendWorkflowPrompt(pi, ctx, buildSingleAgentPrompt(parsed.agent, parsed.task));
				return;
			}

			const selected = await selectAgent(ctx, [...agents.values()]);
			if (!selected) {
				ctx.ui.notify("已取消", "info");
				return;
			}
			const task = await requestAgentTask(selected, args, ctx);
			if (!task) return;
			sendWorkflowPrompt(pi, ctx, buildSingleAgentPrompt(selected, task));
		},
	});

	const commands: Array<{
		name: `subagents-${WorkflowKind}`;
		kind: WorkflowKind;
		description: string;
	}> = [
		{ name: "subagents-feat", kind: "feat", description: "功能开发：侦察 → 计划 → 实现 → 审查" },
		{ name: "subagents-fix", kind: "fix", description: "问题修复：侦察 → 计划 → 修复 → 审查" },
		{ name: "subagents-review", kind: "review", description: "只读代码审查" },
	];

	for (const command of commands) {
		pi.registerCommand(command.name, {
			description: command.description,
			handler: async (args, ctx) => {
				const task = await requestTask(command.kind, args, ctx);
				if (!task) return;
				const prompt = buildWorkflowPrompt(command.kind, task);
				sendWorkflowPrompt(pi, ctx, prompt);
			},
		});
	}
}
