import type { ExtensionAPI, ExtensionContext } from '@earendil-works/pi-coding-agent';
import { AskQuestionParams } from './schema.ts';
import { QuestionInteractions } from './interaction.ts';
import { nativeRenderers } from './native-ui.ts';
import { askQuestions } from './rpc.ts';
import { normalizeOptions, formatList, questionnaireDetails, type AskQuestionInput, type AskQuestionDetails } from './model.ts';

const executeQuestionnaire = async (params: { questions: AskQuestionInput[] }, ctx: ExtensionContext, interactions: QuestionInteractions, signal?: AbortSignal) => {
    const questions = params.questions.map((question) => ({
        question: question.question,
        label: question.label?.replace(/[\r\n\t]+/g, ' ').trim() || undefined,
        options: normalizeOptions(question.options),
        multiSelect: question.multiSelect === true,
    }));

    if (!ctx.hasUI) {
        const details = questionnaireDetails(questions, []);
        return {
            content: [{ type: 'text' as const, text: `需要询问用户（${questions.length} 个问题）：\n${questions.map((q, i) => `${q.label || `问题 ${i + 1}`}：${q.question}\n${q.multiSelect ? '可多选：' : '选项：'}${q.options.join(' / ')}`).join('\n')}` }],
            details,
        };
    }

    const result = await askQuestions(questions, true, ctx, interactions, signal);
    // Esc cancels the submission, including any unsubmitted draft choices.
    const indexedAnswers = questions.map((_question, index) => result && !result.cancelled ? result.answers[index] : undefined);
    const details = questionnaireDetails(questions, indexedAnswers);
    details.cancelled = !result || result.cancelled;
    if (!result || result.cancelled) {
        return { content: [{ type: 'text' as const, text: '用户取消了问卷。' }], details };
    }
    const summary = details.questions.map((question, index) => `${question.label || `问题 ${index + 1}`}（${question.question}）：${Array.isArray(question.answer) ? formatList(question.answer) : question.answer}`).join('\n');
    return { content: [{ type: 'text' as const, text: `用户回答了 ${questions.length} 个问题：\n${summary}` }], details };
};

const askQuestion = (pi: ExtensionAPI) => {
    const interactions = new QuestionInteractions((pending) => {
        pi.events?.emit("ask-question:pending", { pending });
    });
    pi.on('session_shutdown', () => interactions.shutdown());
    pi.on('session_tree', () => interactions.shutdown());
    pi.on('session_start', () => interactions.shutdown());
    pi.registerTool({
        name: 'ask_question',
        label: '提问用户',
        description: '向用户提问，支持单选、多选及自动追加的自定义输入。并发调用依次展示；排队和等待不延长 Worker 任务或问题超时。',
        promptSnippet: '向用户提问',
        promptGuidelines: [
            '需用户决策、确认或补充信息时必须用 ask_question，不用普通文本反问；能继续则不问。',
            '提供 2-6 个清晰选项；偏好不明可加“由 pi 自行判断”。允许多选须设 multiSelect: true；相关问题合并到 questions。',
        ],
        parameters: AskQuestionParams,

        async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
            if (params.questions && params.questions.length > 0) {
                return executeQuestionnaire({ questions: params.questions as AskQuestionInput[] }, ctx, interactions, _signal);
            }

            if (typeof params.question !== 'string' || !Array.isArray(params.options)) {
                throw new Error('ask_question 需要提供 question 和 options，或提供非空的 questions 数组。');
            }

            const options = normalizeOptions(params.options);
            const multiSelect = params.multiSelect === true;

            if (!ctx.hasUI) {
                return {
                    content: [{ type: 'text', text: `需要询问用户：${params.question}\n${multiSelect ? '可多选：' : '选项：'}${options.join(' / ')}` }],
                    details: { question: params.question, options, answer: null, multiSelect } as AskQuestionDetails,
                };
            }

            const submission = await askQuestions([{ question: params.question, options, multiSelect }], false, ctx, interactions, _signal);
            const result = submission && !submission.cancelled ? submission.answers[0] : null;

            if (!result) {
                return {
                    content: [{ type: 'text', text: '用户取消了选择。' }],
                    details: { question: params.question, options, answer: null, multiSelect } as AskQuestionDetails,
                };
            }

            if (multiSelect) {
                return {
                    content: [{ type: 'text', text: `用户选择了 ${result.answers.length} 项：\n${formatList(result.answers)}` }],
                    details: {
                        question: params.question,
                        options,
                        answer: result.answers,
                        multiSelect: true,
                        wasCustom: result.wasCustom,
                        customAnswers: result.customAnswers,
                    } as AskQuestionDetails,
                };
            }

            const answer = result.answers[0] ?? '';
            const wasCustom = Boolean(result.wasCustom);
            return {
                content: [{ type: 'text', text: `${wasCustom ? '用户输入' : '用户选择'}：${answer}` }],
                details: {
                    question: params.question,
                    options,
                    answer,
                    multiSelect: false,
                    wasCustom,
                } as AskQuestionDetails,
            };
        },

        ...nativeRenderers,
    });
};

export default askQuestion;
