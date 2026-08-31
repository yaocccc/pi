import { estimateTokens, type ExtensionAPI, type ExtensionContext } from '@earendil-works/pi-coding-agent';
import { WORKER_USAGE_EVENT } from '../worker/events.ts';
import { TextAreaEditor } from './editor.ts';
import { NoCostFooter } from './footer.ts';
import { StartupHeader } from './header.ts';
import { patchCollapsedThinkingPreview, patchCompactToolDisplay, patchFinalResponseSeparator, patchFullscreenScrollbar, patchMergeConsecutiveTools, patchPaddedBackgroundHalfBlocks, patchThinkingSpacing, patchUserMessageHalfBlocks } from './patches.ts';
import type { TokenUsage } from './types.ts';
import { calculateTps, combineTokenUsage, WorkerUsageTracker } from './worker-usage.ts';
import { applyWorkingMessage, setWorkingMessageActive, WORKING_FRAME_INTERVAL_MS } from './working-message.ts';

export { TextAreaEditor } from './editor.ts';
export { NoCostFooter } from './footer.ts';
export { StartupHeader } from './header.ts';
export { patchCollapsedThinkingPreview, patchCompactToolDisplay, patchFinalResponseSeparator, patchFullscreenScrollbar, patchMergeConsecutiveTools, patchPaddedBackgroundHalfBlocks, patchThinkingSpacing, patchUserMessageHalfBlocks } from './patches.ts';
export type { FooterData, TokenUsage } from './types.ts';
export { formatElapsed, formatTokenCount, foregroundFromRenderedBox, halfBlockLine, padToWidth, replaceVisibleContent, textAreaBg } from './utils.ts';
export { applyWorkingMessage, setWorkingMessageActive, workingMessage, WORKING_FRAME_INTERVAL_MS } from './working-message.ts';

export default function ui(pi: ExtensionAPI) {
    patchCollapsedThinkingPreview();
    patchThinkingSpacing();
    patchFinalResponseSeparator();
    patchCompactToolDisplay();
    patchMergeConsecutiveTools();
    patchFullscreenScrollbar();
    patchPaddedBackgroundHalfBlocks();
    patchUserMessageHalfBlocks();

    let startedAt: number | undefined;
    let timer: NodeJS.Timeout | undefined;
    let traceUsage: TokenUsage = {};
    let currentUsage: TokenUsage = {};
    let currentTurn: number | undefined;
    let firstTokenLatencyMs: number | undefined;
    const workerUsage = new WorkerUsageTracker();

    const getMainUsage = (): TokenUsage => ({
        input: (traceUsage.input ?? 0) + (currentUsage.input ?? 0),
        output: (traceUsage.output ?? 0) + (currentUsage.output ?? 0),
    });
    const refreshWorkingMessage = (ctx: ExtensionContext) => {
        const mainUsage = getMainUsage();
        const displayedUsage = combineTokenUsage(mainUsage, workerUsage.total());
        const elapsedSeconds = startedAt === undefined ? 0 : (Date.now() - startedAt) / 1000;
        const tps = calculateTps(displayedUsage, elapsedSeconds);
        applyWorkingMessage(ctx, startedAt, displayedUsage, currentTurn, tps, firstTokenLatencyMs);
    };

    pi.events.on(WORKER_USAGE_EVENT, (data) => workerUsage.update(data));

    pi.on('session_start', (_event, ctx) => {
        workerUsage.reset();
        setWorkingMessageActive(false);
        patchFullscreenScrollbar();
        refreshWorkingMessage(ctx);
        ctx.ui.setHeader((_tui, theme) => new StartupHeader(theme));
        ctx.ui.setEditorComponent((tui, theme, keybindings) => new TextAreaEditor(tui, theme, keybindings));
        ctx.ui.setFooter((_tui, theme, footerData) => new NoCostFooter(ctx, theme, footerData));
    });

    pi.on('agent_start', (_event, ctx) => {
        workerUsage.reset();
        setWorkingMessageActive(true);
        if (timer) clearInterval(timer);
        startedAt = Date.now();
        traceUsage = {};
        currentUsage = {};
        currentTurn = 1;
        firstTokenLatencyMs = undefined;
        refreshWorkingMessage(ctx);
        timer = setInterval(() => refreshWorkingMessage(ctx), WORKING_FRAME_INTERVAL_MS);
    });

    pi.on('turn_start', (event, ctx) => {
        currentUsage = {};
        currentTurn = event.turnIndex + 1;
        refreshWorkingMessage(ctx);
    });

    pi.on('message_update', (event, ctx) => {
        if (event.message.role !== 'assistant') return;
        const reportedInput = event.message.usage.input;
        const reportedOutput = event.message.usage.output;
        const currentOutput = Math.max(currentUsage.output ?? 0, reportedOutput, estimateTokens(event.message));
        currentUsage = {
            input: reportedInput > 0 ? reportedInput : currentUsage.input,
            output: currentOutput,
        };
        if (firstTokenLatencyMs === undefined && currentOutput > 0 && startedAt !== undefined) {
            firstTokenLatencyMs = Date.now() - startedAt;
        }
        refreshWorkingMessage(ctx);
    });

    pi.on('message_end', (event, ctx) => {
        if (event.message.role !== 'assistant') return;
        traceUsage = {
            input: (traceUsage.input ?? 0) + event.message.usage.input,
            output: (traceUsage.output ?? 0) + event.message.usage.output,
        };
        if (firstTokenLatencyMs === undefined && event.message.usage.output > 0 && startedAt !== undefined) {
            firstTokenLatencyMs = Date.now() - startedAt;
        }
        currentUsage = {};
        refreshWorkingMessage(ctx);
    });

    pi.on('agent_end', (_event, ctx) => {
        setWorkingMessageActive(false);
        if (timer) clearInterval(timer);
        timer = undefined;
        refreshWorkingMessage(ctx);
    });

    pi.on('session_shutdown', () => {
        setWorkingMessageActive(false);
        if (timer) clearInterval(timer);
        timer = undefined;
    });
}
