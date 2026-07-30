import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { Type } from 'typebox';
import type { CheckResult, PlanState, PlanTodo } from './types/index.ts';

const PLAN_SET_TODOS_PARAMETERS = Type.Object({
    todos: Type.Array(Type.Object({
        title: Type.String({ description: '小项标题' }),
        goal: Type.String({ description: '小项目标' }),
        steps: Type.Array(Type.String({ description: '执行步骤列表' }), { minItems: 1 }),
        risks: Type.Optional(Type.Array(Type.String({ description: '风险列表' }))),
        acceptance: Type.Array(Type.String({ description: '验收标准列表' }), { minItems: 1 }),
    }), { minItems: 1 }),
});

const PLAN_CHECK_RESULT_PARAMETERS = Type.Object({
    status: Type.Union([Type.Literal('pass'), Type.Literal('fail')], { description: 'pass 或 fail' }),
    reason: Type.Optional(Type.String({ description: '失败原因（fail 时必填）' })),
    improvements: Type.Optional(Type.Array(Type.String({ description: '改进措施' }))),
});

export const registerPlanTools = (pi: ExtensionAPI, state: PlanState) => {
    pi.registerTool({
        name: 'plan_set_todos',
        label: '提交 Todos',
        description: '仅 /plan 模式使用：提交计划中的所有小项，供 plan extension 逐项执行。非 /plan 日常对话不要调用。',
        promptSnippet: '仅 /plan 模式使用：提交结构化 todos；非 /plan 日常对话不要调用',
        parameters: PLAN_SET_TODOS_PARAMETERS,
        async execute(_id, params) {
            state.todos = params.todos as PlanTodo[];
            return {
                content: [{ type: 'text', text: `已记录 ${state.todos.length} 个 todos。` }],
                details: { count: state.todos.length },
            };
        },
    });

    pi.registerTool({
        name: 'plan_check_result',
        label: '提交检查结果',
        description: '仅 /plan 模式使用：提交当前 todo 的检查结论。status 为 "pass" 表示通过，"fail" 表示未通过。非 /plan 日常对话不要调用。',
        promptSnippet: '仅 /plan 模式使用：提交 todo 检查结果；非 /plan 日常对话不要调用',
        parameters: PLAN_CHECK_RESULT_PARAMETERS,
        async execute(_id, params) {
            state.lastCheck = params as CheckResult;
            const label = state.lastCheck!.status === 'pass' ? '通过' : '未通过';
            return {
                content: [{ type: 'text', text: `检查结论：${label}。` }],
                details: state.lastCheck,
            };
        },
    });
};
