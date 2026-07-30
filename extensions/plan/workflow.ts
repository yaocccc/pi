import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import {
    executeTodoPrompt,
    failurePrompt,
    finalReviewPrompt,
    planPrompt,
    reviewTodoPrompt,
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
        description: 'Plan 模式：使用当前配置模型规划、逐项执行、检查并最终复查',
        handler: async (args, ctx) => {
            const task = (args as string).trim() || await readPlanTask(ctx);
            if (!task?.trim()) return ctx.ui.notify('已取消：无输入。', 'warning');

            state.todos = [];
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

                state.todos = [];
                ctx.ui.notify('正在补充计划……', 'info');
                await sendAndWait(pi, ctx, supplementPrompt(feedback));
            }

            if (state.todos.length === 0) {
                ctx.ui.notify('模型未提交 todos。', 'error');
                return;
            }

            for (const [i, todo] of state.todos.entries()) {
                const idx = i + 1;
                const total = state.todos.length;

                ctx.ui.notify(`正在执行 todo ${idx}/${total}：${todo.title}`, 'info');
                await sendAndWait(pi, ctx, executeTodoPrompt(todo, idx, total));

                state.lastCheck = undefined;
                ctx.ui.notify(`正在检查 todo ${idx}/${total}：${todo.title}`, 'info');
                await sendAndWait(pi, ctx, reviewTodoPrompt(todo, idx, total));

                // as 断言：TypeScript 无法追踪闭包内对象属性的 mutation
                if ((state.lastCheck as CheckResult | undefined)?.status !== 'pass') {
                    ctx.ui.notify(`❌ todo ${idx}/${total} 未通过，正在总结……`, 'warning');
                    await sendAndWait(pi, ctx, failurePrompt(todo, idx, total, state.lastCheck as CheckResult | undefined));
                    return;
                }

                ctx.ui.notify(`✅ todo ${idx}/${total} 通过。`, 'info');
            }

            ctx.ui.notify('正在做最终总复查……', 'info');
            await sendAndWait(pi, ctx, finalReviewPrompt);
        },
    });
};
