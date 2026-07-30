import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import type { TokenUsage } from './types.ts';
import { formatElapsed, formatTokenCount } from './utils.ts';

export const workingMessage = (startedAt: number | undefined, usage: TokenUsage = {}, turn?: number): string => {
    const input = usage.input ?? 0;
    const output = usage.output ?? 0;
    const usageText = [
        ...(input > 0 ? [`↑${formatTokenCount(input)}`] : []),
        ...(output > 0 ? [`↓${formatTokenCount(output)}`] : []),
    ].join(' ');
    const parts = [
        `Turn ${turn ?? 1}`,
        ...(usageText ? [usageText] : []),
        formatElapsed(startedAt ? Date.now() - startedAt : 0),
    ];
    return parts.join(' · ');
};

export const applyWorkingMessage = (ctx: ExtensionContext, startedAt?: number, usage?: TokenUsage, turn?: number): void => ctx.ui.setWorkingMessage(workingMessage(startedAt, usage, turn));
