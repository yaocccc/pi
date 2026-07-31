import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { Type } from 'typebox';
import type { CheckResult, PlanState } from './types/index.ts';

const PLAN_CHECK_RESULT_PARAMETERS = Type.Object({
    status: Type.Union([Type.Literal('pass'), Type.Literal('fail')], { description: 'pass 或 fail' }),
    reason: Type.Optional(Type.String({ description: '失败原因（fail 时必填）' })),
    improvements: Type.Optional(Type.Array(Type.String({ description: '改进措施' }))),
});

export const registerPlanTools = (pi: ExtensionAPI, state: PlanState) => {
    pi.registerTool({
        name: 'plan_check_result',
        label: '提交检查结果',
        description: '仅 /plan 最终检查阶段使用：提交整个 checklist 的检查结论。status 为 "pass" 表示通过，"fail" 表示仍有未解决问题。非 /plan 日常对话不要调用。',
        promptSnippet: '仅 /plan 最终检查阶段使用：提交整个 checklist 的检查结果；非 /plan 日常对话不要调用',
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
