import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import type { AskQuestionInput, QuestionnaireAnswer } from './model.ts';

export const ASK_QUESTION_SERVICE = 'ask-question:v1';
export const ASK_QUESTION_INTERACTION = 'ask-question.interaction:v1';

export interface QuestionRequest {
    questions: AskQuestionInput[];
    review: boolean;
    context: ExtensionContext;
    signal?: AbortSignal;
}
export interface QuestionSubmission {
    answers: QuestionnaireAnswer[];
    cancelled: boolean;
}

/** Optional frontend port. Implementations must dispose their interaction on abort.
 * Results remain untrusted: the capability validates them before returning answers.
 */
export interface QuestionInteractionPort {
    supports(context: ExtensionContext): boolean;
    ask(request: QuestionRequest): Promise<unknown>;
}

/** Session-scoped public capability. Undefined means no compatible interaction port,
 * not user cancellation. Pending includes both active and queued requests.
 */
export interface AskQuestionService {
    ask(request: QuestionRequest): Promise<QuestionSubmission | undefined>;
    pending(): number;
}
