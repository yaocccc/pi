import type { AgentName, ChainTask } from "./types/index.ts";

export type WorkflowKind = "feat" | "fix" | "review";

export interface WorkflowOptions {
	includeScout?: boolean;
}

function originalRequest(task: string): string {
	return `原始需求：\n${task}`;
}

function chainPrompt(kind: "feat" | "fix", task: string, includeScout: boolean): string {
	const original = originalRequest(task);
	const focus =
		kind === "feat"
			? {
				scout: "定位功能涉及的入口、数据流、约束、现有测试和潜在风险，只做侦察。",
				planner: includeScout
					? "核对侦察结果与当前仓库，制定具体、可执行、可验收的实现计划，只规划不修改文件。"
					: "先完成任务所需的最小代码侦察，再制定具体、可执行、可验收的实现计划；避免大范围重复扫描，只规划不修改文件。",
				worker: "严格依据计划实施功能，保持改动最小，完成必要验证。",
				reviewer: "对照原始需求审查最终仓库改动，检查正确性、遗漏、回归、测试和复杂度；只审查，不修改文件。",
			}
			: {
				scout: "定位问题现象、相关调用链、可能根因、现有测试和风险，只做侦察。",
				planner: includeScout
					? "核对侦察结果与当前仓库，制定最小修复计划，包含根因、修改点、回归风险和验证方式，只规划不修改文件。"
					: "先完成定位根因所需的最小代码侦察，再制定最小修复计划，包含根因、修改点、回归风险和验证方式；避免大范围重复扫描，只规划不修改文件。",
				worker: "严格依据计划修复根因，避免无关重构，并完成针对性验证。",
				reviewer: "对照原始问题审查修复是否命中根因、是否引入回归、测试是否充分；只审查，不修改文件。",
			};

	const chain: ChainTask[] = [];
	if (includeScout) {
		chain.push({ agent: "scout", task: `${original}\n\n当前职责：${focus.scout}` });
	}
	chain.push(
		{
			agent: "planner",
			task: includeScout
				? `${original}\n\n当前职责：${focus.planner}\n\n上一步侦察结果：\n{previous}`
				: `${original}\n\n当前职责：${focus.planner}`,
		},
		{
			agent: "worker",
			task: `${original}\n\n当前职责：${focus.worker}\n\n上一步结果：\n{previous}`,
		},
		{
			agent: "reviewer",
			task: `${original}\n\n当前职责：${focus.reviewer}\n\n上一步结果：\n{previous}`,
		},
	);

	return [
		`这是 /subagents-${kind} 发起的${includeScout ? "含独立侦察的四阶段" : "默认三阶段"}子代理工作流。`,
		"立即调用 subagent 工具一次，并原样使用下面的参数。不要只描述流程，不要二次确认，也不要改用其他 agent。等待整个链执行完成后，再用中文简要汇总结果。",
		"",
		"```json",
		JSON.stringify({ chain }, null, 2),
		"```",
	].join("\n");
}

function reviewPrompt(task: string): string {
	const agent: AgentName = "reviewer";
	const params = {
		agent,
		task: `${originalRequest(task)}\n\n只做审查，不修改任何文件。检查需求符合度、正确性、安全性、边界情况、回归风险、测试覆盖和不必要复杂度；结论必须提供文件路径与行号证据。`,
	};
	return [
		"这是 /subagents-review 发起的只读审查。",
		"立即调用 subagent 工具一次，并原样使用下面的参数。不要只描述流程，不要二次确认。等待 reviewer 完成后，再用中文简要汇总结论。",
		"",
		"```json",
		JSON.stringify(params, null, 2),
		"```",
	].join("\n");
}

export function buildSingleAgentPrompt(agent: AgentName, task: string): string {
	const params = {
		agent,
		task: originalRequest(task),
	};
	return [
		"这是 /subagents 发起的单个子代理任务。",
		"立即调用 subagent 工具一次，并原样使用下面的参数。不要只描述流程，不要二次确认。等待子代理完成后，再用中文简要汇总结果。",
		"",
		"```json",
		JSON.stringify(params, null, 2),
		"```",
	].join("\n");
}

export function buildWorkflowPrompt(kind: WorkflowKind, task: string, options: WorkflowOptions = {}): string {
	return kind === "review" ? reviewPrompt(task) : chainPrompt(kind, task, options.includeScout === true);
}
