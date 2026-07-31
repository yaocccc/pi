export const planPrompt = (task: string) =>
    `进入 /plan 规划阶段。请结合当前仓库，为下面的需求生成一份可直接执行的 checklist。\n\n用户原始需求：\n${task}\n\n要求：\n` +
    `1. 先判断目标是否清晰，并按需检查当前仓库；信息不足时列出最小必要假设，不要擅自扩大范围。\n` +
    `2. 不要重复输出“详细计划”和“简短计划”，只保留一份 checklist 作为后续执行依据。\n` +
    `3. checklist 使用「## Checklist」标题和「- [ ] 步骤」格式，按依赖顺序列出可执行步骤；每一步包含明确结果，避免拆成琐碎操作。\n` +
    `4. checklist 之后列出风险和整体验收标准。\n` +
    `5. 暂时不要修改文件，不要调用 plan_check_result；只输出规划结果。`;

export const supplementPrompt = (feedback: string) =>
    `用户检查计划后提出补充意见：\n\n${feedback}\n\n请基于原始需求、最近一版 checklist 和补充意见，重新输出一份完整的替代版本。\n\n` +
    `要求：\n1. 只输出更新后的规划结果，不要执行任务。\n` +
    `2. 仍然只保留一份「## Checklist」，使用「- [ ] 步骤」格式并按依赖顺序排列。\n` +
    `3. checklist 之后列出更新后的风险和整体验收标准。\n` +
    `4. 不要调用 plan_check_result。`;

export const executeChecklistPrompt = (task: string) =>
    `进入 /plan 执行阶段。用户已经确认最近一版 checklist，请在当前会话中按顺序连续完成全部步骤。\n\n用户原始需求：\n${task}\n\n要求：\n` +
    `1. 以最近一版「## Checklist」为唯一执行清单；如果有多个版本，只使用最后确认的版本。\n` +
    `2. 按依赖顺序逐步实施，每完成一步就自行核对结果，然后继续下一步；不要在步骤之间等待用户确认。\n` +
    `3. 严格控制范围，遇到计划与当前代码不一致时选择最小且安全的调整，并在结果中说明。\n` +
    `4. 完成实现后运行最小充分的测试、类型检查或构建；不要伪造验证结果。\n` +
    `5. 只有遇到必须由用户决定的产品、架构或破坏性选择时才停止。\n` +
    `6. 暂时不要调用 plan_check_result。最终输出一份执行后的 checklist：已完成项使用「- [x]」，未完成项保留「- [ ]」并说明原因，同时列出修改文件和验证结果。`;

export const finalReviewPrompt = (task: string) =>
    `进入 /plan 最终检查阶段。请对照用户原始需求、最近一版已确认 checklist、当前仓库改动和验证结果做总检查。\n\n用户原始需求：\n${task}\n\n要求：\n` +
    `1. 检查 checklist 是否全部完成，是否存在遗漏、越界修改、逻辑错误、回归风险或验证不足。\n` +
    `2. 运行必要且安全的补充验证；如果发现属于原范围且可以安全修复的问题，立即修复并重新验证。\n` +
    `3. 检查完成后必须且只能调用一次 plan_check_result：全部满足时 status="pass"；仍有未解决问题时 status="fail"，并填写 reason 和 improvements。\n` +
    `4. 最终简要汇总完成内容、修改文件、验证结果和结论。`;
