import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { registerPlanTools } from './tools.ts';
import type { PlanState } from './types/index.ts';
import { registerPlanCommand } from './workflow.ts';

export default (pi: ExtensionAPI) => {
    const state: PlanState = {
        lastCheck: undefined,
    };

    registerPlanTools(pi, state);
    registerPlanCommand(pi, state);
};
