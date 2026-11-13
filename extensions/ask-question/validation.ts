import { normalizeOptions, type AskQuestionInput, type QuestionnaireAnswer } from './model.ts';
import type { QuestionSubmission } from './api.ts';

const invalidResponse = (): never => { throw new Error('Invalid questionnaire response'); };
const isRecord = (value: unknown): value is Record<string, unknown> =>
    typeof value === 'object' && value !== null && !Array.isArray(value);
const isAnswerList = (value: unknown): value is string[] =>
    Array.isArray(value) && value.every((answer) => typeof answer === 'string' && answer.trim().length > 0)
    && new Set(value).size === value.length;

/** Validate every adapter equally; cancellation discards all unsubmitted drafts. */
export function validateQuestionResponse(value: unknown, questions: AskQuestionInput[]): QuestionSubmission {
    if (!isRecord(value) || typeof value.cancelled !== 'boolean') return invalidResponse();
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
