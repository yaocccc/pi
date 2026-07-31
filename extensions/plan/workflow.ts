import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import {
    executeChecklistPrompt,
    finalReviewPrompt,
    planPrompt,
    supplementPrompt,
} from './prompts.ts';
import type { CheckResult, PlanState } from './types/index.ts';
import { readPlanDecision } from './ui/decision.ts';
import { readPlanSupplement, readPlanTask } from './ui/text-input.ts';

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

const sendAndWait = async (
    pi: ExtensionAPI,
    ctx: { isIdle(): boolean; waitForIdle(): Promise<void> },
    content: string,
) => {
    pi.sendUserMessage(content, ctx.isIdle() ? undefined : { deliverAs: 'followUp' });
    for (let i = 0; i < 100 && ctx.isIdle(); i++) await sleep(20);
    await ctx.waitForIdle();
};

export const registerPlanCommand = (pi: ExtensionAPI, state: PlanState) => {
    pi.registerCommand('plan', {
        description: 'Plan 模式：生成 checklist，确认后连续执行并做最终检查',
        handler: async (args, ctx) => {
            const task = (args as string).trim() || await readPlanTask(ctx);
            if (!task?.trim()) return ctx.ui.notify('已取消：无输入。', 'warning');

            state.lastCheck = undefined;
            ctx.ui.notify('正在规划……', 'info');
            await sendAndWait(pi, ctx, planPrompt(task));

            while (true) {
                const choice = await readPlanDecision(ctx);
                if (!choice || choice === 'cancel') return ctx.ui.notify('已停止 /plan。', 'warning');
                if (choice === 'execute') break;

                const feedback = await readPlanSupplement(ctx);
                if (!feedback?.trim()) {
                    ctx.ui.notify('未输入内容。', 'warning');
                    continue;
                }

                ctx.ui.notify('正在补充计划……', 'info');
                await sendAndWait(pi, ctx, supplementPrompt(feedback));
            }

            ctx.ui.notify('正在按 checklist 连续执行……', 'info');
            await sendAndWait(pi, ctx, executeChecklistPrompt(task));

            state.lastCheck = undefined;
            ctx.ui.notify('正在做最终检查……', 'info');
            await sendAndWait(pi, ctx, finalReviewPrompt(task));

            // as 断言：TypeScript 无法追踪闭包内对象属性的 mutation
            const check = state.lastCheck as CheckResult | undefined;
            if (!check) {
                ctx.ui.notify('最终检查未提交 plan_check_result。', 'error');
            } else if (check.status === 'pass') {
                ctx.ui.notify('✅ checklist 已完成并通过最终检查。', 'info');
            } else {
                ctx.ui.notify(`❌ 最终检查未通过：${check.reason ?? '存在未解决问题。'}`, 'warning');
            }
        },
    });
};
