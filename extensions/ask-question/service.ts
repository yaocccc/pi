import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { getCapability } from './capabilities.ts';
import { ASK_QUESTION_INTERACTION, type AskQuestionService, type QuestionInteractionPort } from './api.ts';
import { QuestionInteractions } from './interaction.ts';
import { normalizeOptions } from './model.ts';
import { validateQuestionResponse } from './validation.ts';

export function createQuestionService(pi: ExtensionAPI, interactions: QuestionInteractions): AskQuestionService {
    return {
        pending: () => interactions.pending,
        async ask(request) {
            request.signal?.throwIfAborted();
            const port = getCapability<QuestionInteractionPort>(pi, ASK_QUESTION_INTERACTION);
            if (!port?.supports(request.context)) return undefined;
            const questions = request.questions.map((question) => ({
                question: question.question,
                label: question.label?.replace(/[\r\n\t]+/g, ' ').trim() || undefined,
                options: normalizeOptions(question.options),
                multiSelect: question.multiSelect === true,
            }));
            return interactions.run(request.signal, async (signal) => {
                // Do not let a port mutate the question set against which we validate.
                const value = await port.ask({ ...request, questions: structuredClone(questions), signal });
                return validateQuestionResponse(value, questions);
            });
        },
    };
}
