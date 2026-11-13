export interface AskQuestionDetails {
    question: string;
    options: string[];
    answer: string | string[] | null;
    multiSelect: boolean;
    wasCustom?: boolean;
    customAnswers?: string[];
    /** No compatible interaction adapter was installed; this is not cancellation. */
    unavailable?: boolean;
}

export interface AskQuestionResult {
    answers: string[];
    wasCustom?: boolean;
    customAnswers?: string[];
}

export interface AskQuestionInput {
    question: string;
    options: string[];
    multiSelect?: boolean;
    label?: string;
}

export interface QuestionnaireDetails {
    questions: Array<AskQuestionDetails & { label?: string }>;
    cancelled?: boolean;
    unavailable?: boolean;
}

export type QuestionnaireAnswer = AskQuestionResult & { completed: boolean };

export const normalizeOptions = (options: string[]): string[] => {
    const seen = new Set<string>();
    const result: string[] = [];

    for (const option of options) {
        const label = option.replace(/[\r\n\t]+/g, ' ').trim();
        if (!label || seen.has(label)) continue;
        seen.add(label);
        result.push(label);
    }

    return result.slice(0, 8);
};

export const formatList = (values: string[]): string => values.join('、');

export const questionnaireDetails = (questions: AskQuestionInput[], answers: Array<QuestionnaireAnswer | undefined>): QuestionnaireDetails => ({
    questions: questions.map((question, index) => {
        const answer = answers[index];
        const options = normalizeOptions(question.options);
        const multiSelect = question.multiSelect === true;
        return {
            question: question.question,
            label: question.label,
            options,
            answer: answer?.completed ? (multiSelect ? answer.answers : (answer.answers[0] ?? '')) : null,
            multiSelect,
            wasCustom: answer?.wasCustom,
            customAnswers: answer?.customAnswers,
        };
    }),
});

