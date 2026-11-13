import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { registerCapability } from '../../ask-question/capabilities.ts';
import { ASK_QUESTION_INTERACTION, type QuestionInteractionPort } from '../../ask-question/api.ts';
import { nativeRenderers } from './native-ui.ts';
import { askQuestions } from './rpc.ts';

export default registerAskQuestionUI;

/** Optional native/RPC compatibility adapter. Loading this never replaces the tool
 * or its authority to validate answers and own cancellation/queue state.
 */
export function registerAskQuestionUI(pi: ExtensionAPI): void {
    registerCapability<QuestionInteractionPort>(pi, ASK_QUESTION_INTERACTION, {
        supports: (context) => context.hasUI,
        async ask({ questions, review, context, signal }) {
            return askQuestions(questions, review, context, signal);
        },
    });
    pi.registerToolRenderer((toolName, next) => toolName === 'ask_question' ? nativeRenderers : next());
}
