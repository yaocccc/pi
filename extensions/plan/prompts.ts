import type { CheckResult, PlanTodo } from './types/index.ts';

export const planPrompt = (task: string) =>
    `进入 /plan 工作流：请评估并拆分任务。\n\n用户原始需求：\n${task}\n\n要求：\n` +
    `1. 先判断目标是否清晰，如有缺失请列出假设。\n` +
    `2. 将任务拆分为可执行的小项，每个小项包含：标题、目标、执行步骤、风险、验收标准。\n` +
    `3. 展示完整计划后，最后必须追加「## 简短 Todos」，格式为「1. 标题」「2. 标题」「3. 标题」。\n` +
    `4. 简短 Todos 的标题必须与 plan_set_todos 提交的小项标题一致。\n` +
    `5. 展示完计划后，立即调用 plan_set_todos 工具提交所有小项，todos 内容必须与展示一致。\n` +
    `6. 暂时不要执行任务，只输出计划和提交 todos。`;

export const supplementPrompt = (feedback: string) =>
    `用户检查计划后提出补充意见：\n\n${feedback}\n\n请基于原始需求、上一版计划和补充意见，重新输出完整计划。\n\n` +
    `要求：\n1. 只输出补充后的计划，不要执行任务。\n` +
    `2. 展示完整计划后，最后必须追加「## 简短 Todos」。\n` +
    `3. 简短 Todos 的标题必须与 plan_set_todos 提交的小项标题一致。\n` +
    `4. 展示完计划后立即调用 plan_set_todos 重新提交所有小项。`;

export const executeTodoPrompt = (todo: PlanTodo, index: number, total: number) =>
    `进入 /plan 执行阶段：请执行第 ${index}/${total} 个 todo。\n\n` +
    `标题：${todo.title}\n目标：${todo.goal}\n步骤：\n${todo.steps.map((s, i) => `${i + 1}. ${s}`).join('\n')}\n` +
    `验收标准：\n${todo.acceptance.map((a, i) => `${i + 1}. ${a}`).join('\n')}\n` +
    `\n要求：\n1. 只执行第 ${index} 个 todo，不要执行后续 todo。\n` +
    `2. 完成后说明做了什么、改了哪些文件、是否满足验收标准。`;

export const reviewTodoPrompt = (todo: PlanTodo, index: number, total: number) =>
    `进入 /plan 检查阶段：请复查第 ${index}/${total} 个 todo 的执行结果。\n\n` +
    `原始 todo：\n标题：${todo.title}\n目标：${todo.goal}\n验收标准：\n${todo.acceptance.map((a, i) => `${i + 1}. ${a}`).join('\n')}\n\n` +
    `要求：\n1. 对照 todo 要求检查是否完成。\n2. 检查是否误做了其他 todo。\n` +
    `3. 如果通过，调用 plan_check_result 工具，status 设为 "pass"。\n` +
    `4. 如果未通过，调用 plan_check_result 工具，status 设为 "fail"，并填写 reason 和 improvements。\n` +
    `5. 必须调用 plan_check_result 工具，且只能调用一次。`;

export const failurePrompt = (todo: PlanTodo, index: number, total: number, check?: CheckResult) =>
    `/plan 在第 ${index}/${total} 个 todo 处停止——"${todo.title}"。\n\n` +
    `失败原因：${check?.reason ?? '检查未返回有效结果，可能是模型未调用 plan_check_result 工具。'}\n` +
    `改进建议：${check?.improvements?.join('；') ?? '请在下一次 /plan 中补充更具体的需求或验收标准。'}\n\n` +
    `要求：\n1. 总结本次 /plan 已完成和未完成的部分。\n2. 不要继续执行后续 todo。`;

export const finalReviewPrompt =
    '进入 /plan 最终复查阶段：所有 todo 已执行完毕。请做总复查。\n\n要求：\n' +
    '1. 对照原始需求和计划检查是否全部完成。\n2. 指出遗漏、风险、潜在问题。\n' +
    '3. 如有必要，给出后续建议；不要继续修改。\n4. 最后给出结论：通过 / 需补充 / 需返工。';
