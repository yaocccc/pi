import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { isGoUI } from '../shared/frontend.ts';
import { QuestionInteractions } from './interaction.ts';
import { showQuestions } from './native-ui.ts';
import { normalizeOptions, type AskQuestionInput, type AskQuestionResult, type QuestionnaireAnswer } from './model.ts';

const invalidResponse = (): never => { throw new Error('Invalid Go UI questionnaire response'); };
const isRecord = (value: unknown): value is Record<string, unknown> =>
    typeof value === 'object' && value !== null && !Array.isArray(value);
const isAnswerList = (value: unknown): value is string[] =>
    Array.isArray(value) && value.every((answer) => typeof answer === 'string' && answer.trim().length > 0)
    && new Set(value).size === value.length;

/** Treat the frontend as a protocol peer, not as trusted tool-result data. */
export function validateGoResponse(value: unknown, questions: AskQuestionInput[]): { answers: QuestionnaireAnswer[]; cancelled: boolean } {
    if (!isRecord(value) || typeof value.cancelled !== 'boolean') return invalidResponse();
    // Cancellation discards even partially completed drafts.
    if (value.cancelled) return { answers: [], cancelled: true };
    if (!Array.isArray(value.answers) || value.answers.length !== questions.length) return invalidResponse();
    const answers = value.answers.map((answer: unknown, index: number): QuestionnaireAnswer => {
        if (!isRecord(answer) || answer.completed !== true || !isAnswerList(answer.answers)
            || answer.answers.length === 0 || (!questions[index]!.multiSelect && answer.answers.length !== 1)
            || (answer.wasCustom !== undefined && typeof answer.wasCustom !== 'boolean')
            || (answer.customAnswers !== undefined && !isAnswerList(answer.customAnswers))) return invalidResponse();
        const selected = answer.answers;
        const custom = (answer.customAnswers ?? []) as string[];
        const options = normalizeOptions(questions[index]!.options);
        if (custom.some((item) => !selected.includes(item))
            || selected.some((item) => !options.includes(item) && !custom.includes(item))) return invalidResponse();
        const wasCustom = custom.length > 0 && custom.length === selected.length;
        if (answer.wasCustom !== undefined && answer.wasCustom !== wasCustom) return invalidResponse();
        return { answers: [...selected], customAnswers: custom.length ? [...custom] : undefined, wasCustom, completed: true };
    });
    return { answers, cancelled: false };
}

const askRpcQuestion = async (question: AskQuestionInput, ctx: ExtensionContext, signal?: AbortSignal): Promise<AskQuestionResult | null> => {
    const customLabel = '自己输入…';
    const options = normalizeOptions(question.options);
    const dialogOptions = signal ? { signal } : undefined;

    if (!question.multiSelect) {
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
            `完成（已选 ${selected.size + customAnswers.length} 项）`,
        ];
        const choice = await ctx.ui.select(question.label || question.question, choices, dialogOptions);
        if (!choice) return null;
        if (choice === customLabel) {
            const custom = (await ctx.ui.input(question.question, '请输入你的答案', dialogOptions))?.trim();
            if (custom && !customAnswers.includes(custom)) customAnswers.push(custom);
            continue;
        }
        if (choice.startsWith('完成（')) {
            const answers = [...options.filter((option) => selected.has(option)), ...customAnswers];
            if (answers.length > 0) return {
                answers,
                wasCustom: selected.size === 0 && customAnswers.length > 0,
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

export const askQuestions = async (questions: AskQuestionInput[], review: boolean, ctx: ExtensionContext, interactions: QuestionInteractions, signal?: AbortSignal) => {
    if (ctx.mode === 'tui') return showQuestions(questions, review, ctx, interactions, signal);
    if (isGoUI(ctx) || process.env.PI_GOUI_ASK_QUESTION === '1') {
        const value = await ctx.ui.input(
            'pi-goui:ask-question:v1', JSON.stringify({ questions, review }), signal ? { signal } : undefined,
        );
        if (!value) return null;
        return validateGoResponse(JSON.parse(value), questions);
    }
    const answers: QuestionnaireAnswer[] = [];
    for (const question of questions) {
        const answer = await askRpcQuestion(question, ctx, signal);
        if (!answer) return null;
        answers.push({ ...answer, completed: true });
    }
    return { answers, cancelled: false };
};

