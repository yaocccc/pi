import { estimateTokens, type ExtensionAPI, type ExtensionContext } from '@earendil-works/pi-coding-agent';
import type { AssistantMessage } from '@earendil-works/pi-ai';
import { stripTerminalSequences } from '@earendil-works/pi-tui';
import { patchToolContext } from './tool-context.ts';
import { patchSessionHistory } from './history.ts';
import { WORKER_USAGE_EVENT } from '../worker/events.ts';
import { TextAreaEditor } from './editor.ts';
import { NoCostFooter } from './footer.ts';
import { CodexUsageBadge } from './codex-usage.ts';
import { StartupHeader } from './header.ts';
import { patchToolDisplay, patchFinalResponseSeparator, patchFullscreenScrollbar, patchMergeConsecutiveTools, patchUserMessageFrame } from './patches.ts';
import type { TokenUsage } from './types.ts';
import { calculateTps, combineTokenUsage, WorkerUsageTracker } from './worker-usage.ts';
import { applyWorkingMessage, clearWorkingMessage, getWorkingMessageLine, setWorkingMessageActive, WORKING_FRAME_INTERVAL_MS } from './working-message.ts';
import { recordRoundSummary, restoreRoundSummaries, ROUND_SUMMARY_ENTRY } from './rounds.ts';
import { renderCacheBudget } from './render-cache.ts';

export { TextAreaEditor } from './editor.ts';
export { NoCostFooter } from './footer.ts';
export { StartupHeader } from './header.ts';
export { patchToolDisplay, patchFinalResponseSeparator, patchFullscreenScrollbar, patchMergeConsecutiveTools, patchUserMessageFrame } from './patches.ts';
export type { FooterData, TokenUsage } from './types.ts';
export { formatElapsed, formatTokenCount, padToWidth, replaceVisibleContent } from './utils.ts';
export { applyWorkingMessage, setWorkingMessageActive, workingMessage, WORKING_FRAME_INTERVAL_MS } from './working-message.ts';

export default function ui(pi: ExtensionAPI) {
    patchSessionHistory();
    patchFinalResponseSeparator();
    patchToolDisplay();
    patchMergeConsecutiveTools();
    patchFullscreenScrollbar();
    patchUserMessageFrame();
    patchToolContext();

    let startedAt: number | undefined;
    let timer: NodeJS.Timeout | undefined;
    let footer: NoCostFooter | undefined;
    let traceUsage: TokenUsage = {};
    let currentUsage: TokenUsage = {};
    let currentTurn: number | undefined;
    let lastAssistant: AssistantMessage | undefined;
    let completedLabel: string | undefined;
    const workerUsage = new WorkerUsageTracker();

    const clearTimer = () => {
        if (timer) clearInterval(timer);
        timer = undefined;
    };

    const getMainUsage = (): TokenUsage => ({
        input: (traceUsage.input ?? 0) + (currentUsage.input ?? 0),
        output: (traceUsage.output ?? 0) + (currentUsage.output ?? 0),
    });
    const refreshWorkingMessage = (ctx: ExtensionContext) => {
        const mainUsage = getMainUsage();
        const displayedUsage = combineTokenUsage(mainUsage, workerUsage.total());
        const elapsedSeconds = startedAt === undefined ? 0 : (Date.now() - startedAt) / 1000;
        const tps = calculateTps(displayedUsage, elapsedSeconds);
        applyWorkingMessage(ctx, startedAt, displayedUsage, currentTurn, tps);
    };

    pi.events.on(WORKER_USAGE_EVENT, (data) => workerUsage.update(data));

    const restoreHistory = (ctx: ExtensionContext) => {
        renderCacheBudget.clear();
        completedLabel = undefined;
        lastAssistant = undefined;
        clearWorkingMessage();
        restoreRoundSummaries(ctx.sessionManager.getBranch());
    };
    pi.on('session_tree', (_event, ctx) => { restoreHistory(ctx); ctx.ui.setWorkingVisible(false); });

    pi.on('session_start', (_event, ctx) => {
        workerUsage.reset();
        clearTimer();
        restoreHistory(ctx);
        patchFullscreenScrollbar();
        refreshWorkingMessage(ctx);
        ctx.ui.setHeader((_tui, theme) => new StartupHeader(theme));
        ctx.ui.setEditorComponent((tui, theme, keybindings) => new TextAreaEditor(tui, theme, keybindings));
        footer?.dispose();
        footer = undefined;
        ctx.ui.setFooter((tui, theme, footerData) => {
            footer?.dispose();
            const usage = new CodexUsageBadge(ctx, () => tui.requestRender());
            footer = new NoCostFooter(ctx, theme, footerData, usage);
            return footer;
        });
    });

    pi.on('model_select', (_event, ctx) => footer?.setContext(ctx));

    pi.on('agent_start', (_event, ctx) => {
        completedLabel = undefined;
        lastAssistant = undefined;
        workerUsage.reset();
        setWorkingMessageActive(true);
        clearTimer();
        startedAt = Date.now();
        traceUsage = {};
        currentUsage = {};
        currentTurn = 1;
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
        refreshWorkingMessage(ctx);
    });

    pi.on('message_end', (event, ctx) => {
        if (event.message.role !== 'assistant') return;
        lastAssistant = event.message;
        traceUsage = {
            input: (traceUsage.input ?? 0) + event.message.usage.input,
            output: (traceUsage.output ?? 0) + event.message.usage.output,
        };
        currentUsage = {};
        refreshWorkingMessage(ctx);
    });

    pi.on('agent_end', (_event, ctx) => {
        setWorkingMessageActive(false);
        clearTimer();
        refreshWorkingMessage(ctx);
        const line = getWorkingMessageLine();
        if (line) completedLabel = stripTerminalSequences(line).replace(/^✓ /, '');
    });

    pi.on('agent_settled', (event, ctx) => {
        const aborted = 'aborted' in event && event.aborted === true;
        if (!aborted && completedLabel && lastAssistant) {
            const data = recordRoundSummary(ctx.sessionManager.getBranch(), lastAssistant, completedLabel);
            if (data) {
                pi.appendEntry(ROUND_SUMMARY_ENTRY, data);
                clearWorkingMessage();
                ctx.ui.setWorkingVisible(false); // Redraw after recording the settled round boundary.
            }
        }
        completedLabel = undefined;
        lastAssistant = undefined;
    });

    pi.on('session_shutdown', () => {
        renderCacheBudget.clear();
        completedLabel = undefined;
        lastAssistant = undefined;
        clearWorkingMessage();
        restoreRoundSummaries([]);
        clearTimer();
        footer?.dispose();
        footer = undefined;
    });
}
