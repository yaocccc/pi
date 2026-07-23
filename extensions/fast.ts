import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';

export default function (pi: ExtensionAPI) {
    pi.on('before_provider_request', (event, ctx) => {
        const model = ctx.model;
        if (
            model?.provider !== 'openai-codex' ||
            model.api !== 'openai-codex-responses' ||
            !/^gpt-5\.6-(sol|terra|luna)$/.test(model.id) ||
            !event.payload ||
            typeof event.payload !== 'object' ||
            Array.isArray(event.payload)
        ) {
            return;
        }

        return {
            ...(event.payload as Record<string, unknown>),
            service_tier: 'priority',
        };
    });
}
