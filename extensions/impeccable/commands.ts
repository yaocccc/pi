export const IMPECCABLE_SKILL_COMMAND = "/skill:impeccable";

export interface ImpeccableCommand {
	/** Text following `/skill:impeccable`; also the selector's stable value. */
	command: string;
	/** Human-readable invocation shown in the selector. */
	invocation: string;
	description: string;
	needsTarget: boolean;
	goal: string;
}

const target = (command: string, description: string, goal: string): ImpeccableCommand => ({
	command,
	invocation: `${command} [目标]`,
	description,
	needsTarget: true,
	goal,
});

const noTarget = (command: string, description: string, goal: string): ImpeccableCommand => ({
	command,
	invocation: command,
	description,
	needsTarget: false,
	goal,
});

/**
 * Self-contained catalogue for the Impeccable skill. Keep this list in sync
 * with the skill's public command surface without loading the skill at runtime.
 */
export const IMPECCABLE_COMMANDS: readonly ImpeccableCommand[] = [
	target("shape", "在写代码前规划 UX/UI 方案与关键流程。", "先形成清晰的体验与界面方案。"),
	noTarget("init", "记录可长期复用的产品背景到 PRODUCT.md。", "梳理并记录可持续使用的产品上下文。"),
	noTarget("document", "从现有项目代码生成 DESIGN.md 设计文档。", "提炼当前项目的设计系统与视觉规则。"),
	target("extract", "把可复用令牌与组件抽取为设计系统。", "识别并沉淀可复用的设计资产。"),
	target("critique", "进行带启发式评分的 UX 设计评审。", "发现体验问题并给出有优先级的改进建议。"),
	target("audit", "检查无障碍、性能与响应式等技术质量。", "找出并修复 UI 技术质量风险。"),
	target("polish", "在发布前完成最终视觉与交互质量打磨。", "消除细节缺陷，使界面达到可交付标准。"),
	target("bolder", "放大过于安全或平淡的设计表现力。", "在保留产品事实的前提下增强视觉主张。"),
	target("quieter", "降低过强、拥挤或刺激的设计噪声。", "让界面更克制、清晰且易用。"),
	target("distill", "提炼核心，移除不必要的复杂度。", "聚焦最重要的信息、行为与视觉层级。"),
	target("harden", "补齐错误态、国际化与边界情况以便生产使用。", "提升界面在真实场景中的可靠性。"),
	target("onboard", "设计首次使用、空状态与激活流程。", "帮助新用户理解价值并完成首次关键动作。"),
	target("animate", "加入有目的的动效与过渡。", "用运动反馈强化层级、状态和操作结果。"),
	target("colorize", "为单色界面加入有策略的色彩。", "建立更清晰、可访问的色彩层级。"),
	target("typeset", "改善字体、排版与文字层级。", "提升阅读节奏、可读性和品牌表达。"),
	target("layout", "修正间距、节奏、对齐与视觉层级。", "让信息组织更清晰、稳定且易扫读。"),
	target("delight", "加入个性与令人记住的微妙体验。", "在不妨碍任务的前提下提升愉悦感。"),
	target("overdrive", "突破常规限制，探索更大胆的设计方向。", "实现技术上出众且服务于目标的视觉体验。"),
	target("clarify", "优化 UX 文案、标签与错误消息。", "让用户更容易理解下一步与系统状态。"),
	target("adapt", "适配不同设备与屏幕尺寸。", "保证各类屏幕上的内容、操作和层级都可靠。"),
	target("optimize", "诊断并修复 UI 性能问题。", "改善加载、渲染与交互响应。"),
	noTarget("live", "在浏览器中选择元素并生成视觉变体。", "进入可视化迭代模式，比较并选择更好的方案。"),
	noTarget("hooks on", "开启项目设计检测钩子：UI 文件修改后自动提示问题。", "启用本项目的 Impeccable 设计检测。"),
	noTarget("hooks off", "关闭项目设计检测钩子。", "停用本项目的 Impeccable 设计检测。"),
	noTarget("hooks status", "查看项目设计检测钩子的当前状态。", "检查设计检测钩子是否已正确配置。"),
	target("hooks ignore-rule", "忽略一条设计检测规则。", "为特定规则配置项目级忽略。"),
	target("hooks ignore-file", "忽略一个文件或路径的设计检测。", "将指定文件排除在设计检测之外。"),
	target("hooks ignore-value", "忽略某条规则的特定检测值。", "为特定检测值添加例外。"),
	noTarget("hooks reset", "重置项目的设计检测钩子配置。", "清除检测钩子的自定义配置并恢复默认状态。"),
	noTarget("doctor", "检查并修复 PRODUCT.md、DESIGN.md、配置与钩子的漂移。", "诊断 Impeccable 项目工件是否过期或不一致。"),
	target("pin", "创建独立的 $<指令> 快捷方式。", "为常用 Impeccable 指令创建独立快捷方式。"),
	target("unpin", "移除独立的 $<指令> 快捷方式。", "移除不再需要的独立快捷方式。"),
];

/** Match a direct `/impeccable <command>` argument, ignoring extra whitespace. */
export function findImpeccableCommand(input: string): ImpeccableCommand | undefined {
	const normalized = input.trim().replace(/\s+/g, " ").toLowerCase();
	return IMPECCABLE_COMMANDS.find((item) => item.command === normalized);
}

/** Build the editable prompt placed into Pi's main editor; this function has no UI dependencies. */
export function buildImpeccablePrompt(command: ImpeccableCommand): string {
	return command.needsTarget
		? `${IMPECCABLE_SKILL_COMMAND} ${command.command} [请填写：目标]`
		: `${IMPECCABLE_SKILL_COMMAND} ${command.command} `;
}

export function availableCommandNames(): string {
	return IMPECCABLE_COMMANDS.map((item) => item.command).join("、");
}
