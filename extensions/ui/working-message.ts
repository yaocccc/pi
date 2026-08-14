import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import type { TokenUsage } from './types.ts';
import { formatElapsed, formatTokenCount } from './utils.ts';

const WORKING_FRAMES = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
export const WORKING_FRAME_INTERVAL_MS = 120;

let workingMessageActive = false;
let workingAnimationStartedAt = 0;
let currentWorkingLine: string | undefined;

export const setWorkingMessageActive = (active: boolean): void => {
    if (active && !workingMessageActive) workingAnimationStartedAt = Date.now();
    workingMessageActive = active;
    if (!active) currentWorkingLine = undefined;
};

export const getWorkingMessageLine = (): string | undefined => currentWorkingLine;

export const workingMessage = (startedAt: number | undefined, usage: TokenUsage = {}, turn?: number, tps?: number, firstTokenLatencyMs?: number): string => {
    const input = usage.input ?? 0;
    const output = usage.output ?? 0;
    const usageText = [
        ...(input > 0 ? [`↑${formatTokenCount(input)}`] : []),
        ...(output > 0 ? [`↓${formatTokenCount(output)}`] : []),
    ].join(' ');
    const parts = [
        `Turn ${turn ?? 1}`,
        ...(usageText ? [usageText] : []),
        ...(typeof tps === 'number' && Number.isFinite(tps) ? [`${tps.toFixed(1)} TPS`] : []),
        ...(typeof firstTokenLatencyMs === 'number' && Number.isFinite(firstTokenLatencyMs)
            ? [`TTFT ${firstTokenLatencyMs < 1000 ? `${Math.round(firstTokenLatencyMs)}ms` : `${(firstTokenLatencyMs / 1000).toFixed(1)}s`}`]
            : []),
        formatElapsed(startedAt ? Date.now() - startedAt : 0),
    ];
    return parts.join(' · ');
};

export const applyWorkingMessage = (ctx: ExtensionContext, startedAt?: number, usage?: TokenUsage, turn?: number, tps?: number, firstTokenLatencyMs?: number): void => {
    // 内置 status 与 editor 之间固定经过 widget spacer；把 working 行并入 editor 才能消除其下方空行。
    ctx.ui.setWorkingVisible(false);
    if (!workingMessageActive) {
        currentWorkingLine = undefined;
        return;
    }

    const elapsed = Math.max(0, Date.now() - workingAnimationStartedAt);
    const frameIndex = Math.floor(elapsed / WORKING_FRAME_INTERVAL_MS) % WORKING_FRAMES.length;
    const frame = WORKING_FRAMES[frameIndex]!;
    const message = workingMessage(startedAt, usage, turn, tps, firstTokenLatencyMs);
    currentWorkingLine = `${ctx.ui.theme.fg('accent', frame)} ${ctx.ui.theme.fg('muted', message)}`;
};
