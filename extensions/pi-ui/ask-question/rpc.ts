import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { supportsStructuredUI } from './support.ts';
import { showQuestions } from './native-ui.ts';
import { normalizeOptions, type AskQuestionInput, type AskQuestionResult, type QuestionnaireAnswer } from '../../ask-question/model.ts';

const askRpcQuestion = async (question: AskQuestionInput, ctx: ExtensionContext, signal?: AbortSignal): Promise<AskQuestionResult | null> => {
    let customLabel = '自己输入…';
    const options = normalizeOptions(question.options);
    const dialogOptions = signal ? { signal } : undefined;

    if (!question.multiSelect) {
        // RPC select returns display text, so reserve an unambiguous action label
        // without changing or restricting any caller-provided option.
        for (let suffix = 2; options.includes(customLabel); suffix++) customLabel = `自己输入…（${suffix}）`;
        const selected = await ctx.ui.select(question.label || question.question, [...options, customLabel], dialogOptions);
        if (!selected) return null;
        if (selected !== customLabel) return { answers: [selected], wasCustom: false };
        const custom = (await ctx.ui.input(question.question, '请输入你的答案', dialogOptions))?.trim();
        return custom ? { answers: [custom], wasCustom: true, customAnswers: [custom] } : null;
    }

    const selected = new Set<string>();
    const customAnswers: string[] = [];
    while (true) {
        const choices = [
            ...options.map((option) => `${selected.has(option) ? '[✓]' : '[ ]'} ${option}`),
            customLabel,
            `完成（已选 ${new Set([...selected, ...customAnswers]).size} 项）`,
        ];
        const choice = await ctx.ui.select(question.label || question.question, choices, dialogOptions);
        if (!choice) return null;
        if (choice === customLabel) {
            const custom = (await ctx.ui.input(question.question, '请输入你的答案', dialogOptions))?.trim();
            if (custom && !customAnswers.includes(custom)) customAnswers.push(custom);
            continue;
        }
        if (choice.startsWith('完成（')) {
            const answers = [...new Set([...options.filter((option) => selected.has(option)), ...customAnswers])];
            if (answers.length > 0) return {
                answers,
                wasCustom: customAnswers.length > 0 && customAnswers.length === answers.length,
                customAnswers: customAnswers.length > 0 ? customAnswers : undefined,
            };
            ctx.ui.notify('请至少选择或输入一项。', 'warning');
            continue;
        }
        const option = choice.slice(4);
        if (selected.has(option)) selected.delete(option);
        else selected.add(option);
    }
};

export const askQuestions = async (questions: AskQuestionInput[], review: boolean, ctx: ExtensionContext, signal?: AbortSignal) => {
    if (ctx.mode === 'tui') return await showQuestions(questions, review, ctx, signal) ?? { answers: [], cancelled: true };
    if (supportsStructuredUI(ctx) || process.env.PI_GOUI_ASK_QUESTION === '1') {
        const value = await ctx.ui.input(
            'pi-goui:ask-question:v1', JSON.stringify({ questions, review }), signal ? { signal } : undefined,
        );
        if (!value) return { answers: [], cancelled: true };
        return JSON.parse(value) as unknown; // The capability validates every adapter's result.
    }
    const answers: QuestionnaireAnswer[] = [];
    for (const question of questions) {
        const answer = await askRpcQuestion(question, ctx, signal);
        if (!answer) return { answers: [], cancelled: true };
        answers.push({ ...answer, completed: true });
    }
    return { answers, cancelled: false };
};
