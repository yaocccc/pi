import { CustomEditor, type ExtensionAPI, type ExtensionContext } from '@earendil-works/pi-coding-agent';
import { Box, Container, Key, matchesKey, Spacer, Text, truncateToWidth, visibleWidth, wrapTextWithAnsi } from '@earendil-works/pi-tui';
import { basename } from 'node:path';
import { commitIndexedMemory } from './commit';
import { MemoryGetParams, MEMORY_INDEX_PATH, MemorySearchParams, MemorySummarizeParams } from './constants';
import {
    compactSearchDisplay,
    duplicateSearchResult,
    ensureIndexedMemory,
    getIndexedMemory,
    memoryVersion,
    projectName,
    searchCacheKey,
    searchIndexedMemory,
} from './indexed';
import { readMemorySettings } from './settings';
import { configureMemorySettings } from './settings-ui';
import { enablePopupMouseWheel, getMouseWheelDirection } from '../ui/mouse-wheel';
import { claimSummaryRequest, clearSummaryRequest, finishSummaryRequest, queueSummaryRequest } from './summary-request';
import type { ProgressInfo, SearchCacheEntry } from './types';
import { asObj, textOf } from './utils';

type EditorFactory = NonNullable<ReturnType<ExtensionContext['ui']['getEditorComponent']>>;
type SummaryResultMetadata = {
    model: string;
    thinking: string;
    input: string;
    output: string;
    elapsed: string;
};

const SUMMARY_FRAME_INTERVAL_MS = 120;
const MOUSE_WHEEL_SCROLL_LINES = 3;

const memoryExtension = async (pi: ExtensionAPI) => {
    let settings = await readMemorySettings();
    let summaryQueue = Promise.resolve();
    let settledQueue = Promise.resolve();
    const searchCaches = new Map<string, Map<string, SearchCacheEntry>>();

    void ensureIndexedMemory().catch(() => undefined);

    pi.registerMessageRenderer('text', (message) => new Text(typeof message.content === 'string' ? message.content : textOf(message.content), 0, 0));
    pi.registerMessageRenderer('indexed-memory', (message) => {
        const box = new Box(1, 1, (text) => `\x1b[48;2;37;37;37m${text}\x1b[49m`);
        box.addChild(new Text(typeof message.content === 'string' ? message.content : textOf(message.content), 0, 0));
        return box;
    });

    const sessionSearchCache = (ctx: ExtensionContext): Map<string, SearchCacheEntry> => {
        const sessionId = ctx.sessionManager.getSessionId();
        let cache = searchCaches.get(sessionId);
        if (!cache) {
            cache = new Map();
            searchCaches.set(sessionId, cache);
        }
        return cache;
    };

    const normalizeLoadedMemoryName = (name: string): string => name
        .replace(/^\d{4}\s+/, '')
        .trim()
        .toLowerCase()
        .replace(/\s+/g, ' ');

    const latestMemoryRead = (ctx: ExtensionContext, name: string): { name: string; version: string } | undefined => {
        const leaf = ctx.sessionManager.getLeafId();
        const entries = leaf ? ctx.sessionManager.getBranch(leaf) : ctx.sessionManager.getEntries();
        const normalizedName = normalizeLoadedMemoryName(name);
        for (let i = entries.length - 1; i >= 0; i--) {
            const entry = entries[i];
            if (entry.type !== 'message') continue;
            const message = entry.message as any;
            if (message.role === 'compactionSummary' || message.role === 'branchSummary') return undefined;
            if (message.role !== 'toolResult' || message.toolName !== 'memory_get' || message.isError) continue;
            const details = asObj(message.details);
            if (details?.found !== true || typeof details.name !== 'string') continue;
            const previousName = details.name.trim();
            if (!previousName || normalizeLoadedMemoryName(previousName) !== normalizedName) continue;
            if (typeof details.version === 'string' && details.version) return { name: previousName, version: details.version };
            if (details.duplicate === true) continue;
            const previousText = textOf(message.content);
            if (previousText) return { name: previousName, version: memoryVersion(previousText) };
        }
        return undefined;
    };

    const showSummaryResult = async (ctx: ExtensionContext, result: string, metadata?: SummaryResultMetadata): Promise<void> => {
        if (ctx.mode !== 'tui') {
            pi.sendMessage({ customType: 'indexed-memory', content: result, display: true });
            return;
        }

        const source = result.replace(/^## Indexed Memory Commit\s*/, '').trim() || '本轮无 indexed memory 写入。';
        let scrollOffset = 0;
        let maxOffset = 0;
        let restoreMouseWheel: (() => void) | undefined;
        try {
            await ctx.ui.custom(
                (tui, theme, _keybindings, done) => {
                    const scrollWithWheel = (direction: -1 | 1): void => {
                        const next = Math.max(
                            0,
                            Math.min(maxOffset, scrollOffset + direction * MOUSE_WHEEL_SCROLL_LINES),
                        );
                        if (next !== scrollOffset) {
                            scrollOffset = next;
                            tui.requestRender();
                        }
                    };
                    restoreMouseWheel = enablePopupMouseWheel(tui, scrollWithWheel);

                    return {
                invalidate() {},
                handleInput(data: string) {
                    const wheelDirection = getMouseWheelDirection(data);
                    if (wheelDirection !== undefined) {
                        scrollWithWheel(wheelDirection);
                        return;
                    }
                    if (matchesKey(data, Key.escape) || matchesKey(data, Key.ctrl('c')) || matchesKey(data, Key.enter)) {
                        done(undefined);
                        return;
                    }
                    const previousOffset = scrollOffset;
                    if (matchesKey(data, Key.up)) scrollOffset = Math.max(0, scrollOffset - 1);
                    else if (matchesKey(data, Key.down)) scrollOffset = Math.min(maxOffset, scrollOffset + 1);
                    else if (matchesKey(data, Key.pageUp)) scrollOffset = Math.max(0, scrollOffset - 10);
                    else if (matchesKey(data, Key.pageDown)) scrollOffset = Math.min(maxOffset, scrollOffset + 10);
                    if (scrollOffset !== previousOffset) tui.requestRender();
                },
                render(width: number) {
                    const inner = Math.max(1, width - 2);
                    const contentWidth = Math.max(1, inner - 2);
                    const styleResultLine = (line: string): string => {
                        const trimmed = line.trim();
                        if (!trimmed) return '';
                        if (/^###\s+Warnings/i.test(trimmed)) return theme.bold(theme.fg('warning', 'Warnings'));
                        if (/总结已取消|失败|warning/i.test(trimmed)) return theme.fg('warning', line);
                        if (/^已处理|^本轮无 indexed memory 写入|^已压缩 indexed memory/.test(trimmed)) {
                            return theme.bold(theme.fg('success', line));
                        }
                        if (/^\d+\.\s+(新增|更新)：/.test(trimmed)) return theme.bold(theme.fg('accent', line));
                        const field = line.match(/^(\s*-\s+)(file|summary|when_to_use|content):\s*(.*)$/);
                        if (field) {
                            const [, prefix, label, value] = field;
                            return `${prefix}${theme.bold(theme.fg('accent', `${label}:`))}${value ? ` ${value}` : ''}`;
                        }
                        const bullet = line.match(/^(\s*)-\s+(.*)$/);
                        if (bullet) return `${bullet[1]}${theme.fg('accent', '•')} ${bullet[2]}`;
                        return line;
                    };
                    const wrapped = source.split('\n').flatMap((line) => {
                        const styled = styleResultLine(line);
                        return styled ? wrapTextWithAnsi(styled, contentWidth) : [''];
                    });
                    const visibleRows = 16;
                    maxOffset = Math.max(0, wrapped.length - visibleRows);
                    scrollOffset = Math.min(scrollOffset, maxOffset);
                    const rows = wrapped.slice(scrollOffset, scrollOffset + visibleRows);
                    const pad = (text: string): string => {
                        const truncated = truncateToWidth(text, inner, '…');
                        return truncated + ' '.repeat(Math.max(0, inner - visibleWidth(truncated)));
                    };
                    const border = (text: string): string => theme.fg('border', text);
                    const position = maxOffset > 0 ? theme.fg('muted', ` ${scrollOffset + 1}-${scrollOffset + rows.length}/${wrapped.length}`) : '';
                    const title = theme.bold(theme.fg('accent', 'Indexed Memory Commit'));
                    const metadataRows = metadata ? [
                        ` ${theme.fg('accent', '◆')} ${theme.bold(metadata.model)} ${theme.fg('muted', `thinking ${metadata.thinking}`)}`,
                        ` ${theme.fg('muted', '上下文')} ${theme.bold(theme.fg('accent', `↑${metadata.input}`))}  ${theme.fg('muted', '输出')} ${theme.bold(theme.fg('success', `↓${metadata.output}`))}  ${theme.fg('muted', '耗时')} ${theme.bold(theme.fg('warning', metadata.elapsed))}`,
                    ] : [];
                    return [
                        border(`╭${'─'.repeat(inner)}╮`),
                        `${border('│')}${pad(` ${title}${position}`)}${border('│')}`,
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        ...metadataRows.map((row) => `${border('│')}${pad(row)}${border('│')}`),
                        ...(metadataRows.length ? [`${border('├')}${border('─'.repeat(inner))}${border('┤')}`] : []),
                        ...rows.map((row) => `${border('│')}${pad(` ${row}`)}${border('│')}`),
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        `${border('│')}${pad(theme.fg('dim', ' ↑/↓ / 鼠标滚轮 滚动 · Enter / Esc 关闭'))}${border('│')}`,
                        border(`╰${'─'.repeat(inner)}╯`),
                    ];
                },
                    };
                },
                {
                    overlay: true,
                    overlayOptions: { anchor: 'center', width: 92, minWidth: 56, maxHeight: '90%', margin: 1 },
                },
            );
        } finally {
            restoreMouseWheel?.();
        }
    };

    const runSummary = async (ctx: ExtensionContext) => {
        type SummaryOutcome = { result?: string; error?: string };
        const sendText = (content: string) => pi.sendMessage({ customType: 'text', content, display: true });
        const frames = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];
        const startedAt = Date.now();
        const controller = new AbortController();
        let progressText = '开始……';
        let summaryContextTokens: number | undefined;
        let summaryOutputTokens: number | undefined;
        let requestModel: string | undefined;
        let requestThinking: string | undefined;
        let cancellable = true;
        let cancelRequested = false;
        const formatTokens = (tokens: number | null | undefined): string => {
            if (tokens === null || tokens === undefined || !Number.isFinite(tokens)) return '?';
            if (tokens < 1_000) return String(Math.round(tokens));
            if (tokens < 1_000_000) return `${Math.round(tokens / 1_000)}k`;
            return `${(tokens / 1_000_000).toFixed(1)}m`;
        };
        const formatElapsed = (): string => {
            const elapsedSeconds = Math.floor((Date.now() - startedAt) / 1_000);
            const minutes = Math.floor(elapsedSeconds / 60);
            const seconds = elapsedSeconds % 60;
            return minutes > 0 ? `${minutes}m${String(seconds).padStart(2, '0')}s` : `${seconds}s`;
        };
        const progressLine = (): string => {
            const elapsed = Math.max(0, Date.now() - startedAt);
            const frameIndex = Math.floor(elapsed / SUMMARY_FRAME_INTERVAL_MS) % frames.length;
            const spinner = ctx.ui.theme.fg('accent', frames[frameIndex]!);
            const requestInfo = requestModel ? ` · ${requestModel} ${requestThinking ?? 'off'}` : '';
            return `${spinner} memory: ${progressText}${requestInfo} · ↑${formatTokens(summaryContextTokens)} ↓${formatTokens(summaryOutputTokens)} · ${formatElapsed()}`;
        };
        const renderProgress = () => {
            if (!ctx.hasUI) return;
            const lines = [progressLine()];
            if (cancellable && !cancelRequested) {
                lines.push('\x1b[2m  Esc 取消总结 · 后台运行，可继续对话\x1b[22m');
            }
            ctx.ui.setWidget('memory-progress', lines);
        };
        const showProgress = (message: string, info?: ProgressInfo) => {
            progressText = message;
            if (info?.contextTokens !== undefined) summaryContextTokens = info.contextTokens;
            if (info?.outputTokens !== undefined) summaryOutputTokens = info.outputTokens;
            if (info?.model !== undefined) requestModel = info.model;
            if (info?.thinking !== undefined) requestThinking = info.thinking;
            if (!cancelRequested && info?.cancellable !== undefined) cancellable = info.cancellable;
            renderProgress();
        };
        const execute = async (): Promise<SummaryOutcome> => {
            try {
                const result = await commitIndexedMemory(ctx, showProgress, settings, controller.signal);
                searchCaches.delete(ctx.sessionManager.getSessionId());
                return { result };
            } catch (e) {
                if (controller.signal.aborted) return { result: '## Indexed Memory Commit\n\n总结已取消，未写入 indexed memory。' };
                const message = e instanceof Error ? e.message : String(e);
                return { error: `indexed memory 写入失败：${message}` };
            }
        };

        const previousEditorFactory = ctx.mode === 'tui' ? ctx.ui.getEditorComponent() : undefined;
        let cancelEditorFactory: EditorFactory | undefined;
        if (ctx.mode === 'tui') {
            cancelEditorFactory = (tui, theme, keybindings) => {
                const editor = previousEditorFactory
                    ? previousEditorFactory(tui, theme, keybindings)
                    : new CustomEditor(tui, theme, keybindings);
                const handleInput = editor.handleInput.bind(editor);
                editor.handleInput = (data: string) => {
                    if (cancellable && !cancelRequested && matchesKey(data, Key.escape)) {
                        cancelRequested = true;
                        cancellable = false;
                        progressText = '正在取消……';
                        controller.abort();
                        renderProgress();
                        return;
                    }
                    handleInput(data);
                };
                return editor;
            };
            ctx.ui.setEditorComponent(cancelEditorFactory);
        }

        const progressTimer = ctx.hasUI ? setInterval(renderProgress, SUMMARY_FRAME_INTERVAL_MS) : undefined;
        showProgress('开始……');
        let outcome: SummaryOutcome;
        try {
            outcome = await execute();
        } finally {
            if (progressTimer) clearInterval(progressTimer);
            if (ctx.hasUI) ctx.ui.setWidget('memory-progress', undefined);
            if (cancelEditorFactory && ctx.ui.getEditorComponent() === cancelEditorFactory) {
                ctx.ui.setEditorComponent(previousEditorFactory);
            }
        }

        if (outcome.result) {
            const metadata: SummaryResultMetadata = {
                model: requestModel ?? 'model ?',
                thinking: requestThinking ?? 'off',
                input: formatTokens(summaryContextTokens),
                output: formatTokens(summaryOutputTokens),
                elapsed: formatElapsed(),
            };
            if (settings.summarize.resultDisplay === 'message') {
                pi.sendMessage({ customType: 'indexed-memory', content: outcome.result, display: true });
            } else if (settings.summarize.resultDisplay === 'popup') {
                await showSummaryResult(ctx, outcome.result, metadata);
            }
        } else if (outcome.error) sendText(outcome.error);
    };

    const enqueueSummary = (ctx: ExtensionContext): Promise<void> => {
        summaryQueue = summaryQueue.catch(() => undefined).then(() => runSummary(ctx));
        return summaryQueue;
    };

    pi.on('agent_settled', (_event, ctx) => {
        settledQueue = settledQueue.catch(() => undefined).then(async () => {
            if (!settings.summarize.auto) {
                await clearSummaryRequest(ctx);
                return;
            }
            const claimed = await claimSummaryRequest(ctx);
            if (!claimed) return;
            try {
                await enqueueSummary(ctx);
            } finally {
                await finishSummaryRequest(claimed);
            }
        });
    });

    pi.on('session_shutdown', async (_event, ctx) => {
        await clearSummaryRequest(ctx);
        await settledQueue.catch(() => undefined);
        await summaryQueue.catch(() => undefined);
    });

    pi.registerTool({
        name: 'memory_search',
        label: '搜索记忆',
        description: '搜索 memory index，返回匹配的 index 项。',
        parameters: MemorySearchParams,
        async execute(_id, params, _signal, _onUpdate, ctx) {
            const project = projectName(ctx, params.project);
            const cache = sessionSearchCache(ctx);
            const key = searchCacheKey(params.query, project);
            const cached = cache.get(key);
            if (cached) {
                return { content: [{ type: 'text', text: duplicateSearchResult(cached) }], details: { index: MEMORY_INDEX_PATH, duplicate: true } };
            }

            const result = await searchIndexedMemory(params.query, ctx, project);
            cache.set(key, { query: params.query, project });
            return { content: [{ type: 'text', text: result }], details: { index: MEMORY_INDEX_PATH, duplicate: false } };
        },
        renderCall(args, theme, context) {
            const project = (args.project?.trim() || basename(context.cwd || '') || 'global').toLowerCase();
            const text = theme.fg('toolTitle', theme.bold('memory_search '))
                + theme.fg('dim', `project=${JSON.stringify(project)} `)
                + theme.fg('muted', `query=${JSON.stringify(args.query)}`);
            const container = new Container();
            container.addChild(new Text(text, 0, 0));
            container.addChild(new Spacer(1));
            return container;
        },
        renderResult(result) {
            const first = result.content[0];
            return new Text(first?.type === 'text' ? compactSearchDisplay(first.text) : '', 0, 0);
        },
    });

    pi.registerTool({
        name: 'memory_get',
        label: '获取记忆',
        description: '按名称读取记忆；与当前分支最近一次读取版本相同则提示复用，有更新才返回最新详情。',
        parameters: MemoryGetParams,
        async execute(_id, params, _signal, _onUpdate, ctx) {
            const result = await getIndexedMemory(params.name);
            const previous = result.found ? latestMemoryRead(ctx, result.name) : undefined;
            if (previous && result.version === previous.version) {
                return {
                    content: [{ type: 'text', text: `## Get Memory Result\n\n记忆“${result.name}”与当前会话中最近一次读取的版本相同。请复用此前的 memory_get 结果。` }],
                    details: { requested: params.name, name: result.name, file: result.file, found: true, version: result.version, duplicate: true, updated: false },
                };
            }
            const updatedText = previous
                ? `## Get Memory Result\n\n检测到记忆“${result.name}”已更新，以下为最新内容。\n\n${result.text.replace(/^## Get Memory Result\n\n/, '')}`
                : result.text;
            return {
                content: [{ type: 'text', text: updatedText }],
                details: { requested: params.name, name: result.name, file: result.file, found: result.found, version: result.version, duplicate: false, updated: !!previous },
            };
        },
        renderCall(args, theme) {
            return new Text(
                theme.fg('toolTitle', theme.bold('memory_get ')) + theme.fg('muted', `name=${JSON.stringify(args.name)}`),
                0,
                0,
            );
        },
        renderResult() {
            return new Container();
        },
    });

    if (settings.summarize.auto) pi.registerTool({
        name: 'memory_summarize',
        label: '请求总结记忆',
        description: '请求在本轮消息结束后总结并沉淀 indexed memory。',
        parameters: MemorySummarizeParams,
        async execute(_id, _params, _signal, _onUpdate, ctx) {
            if (!settings.summarize.auto) return {
                content: [{ type: 'text', text: '自动记忆总结已关闭；可使用 /summarize 手动总结。' }],
                details: { queued: false, sessionId: ctx.sessionManager.getSessionId() },
            };
            await queueSummaryRequest(ctx);
            return {
                content: [{ type: 'text', text: '已请求在本轮消息结束后总结 indexed memory。' }],
                details: { queued: true, sessionId: ctx.sessionManager.getSessionId() },
            };
        },
        renderCall(_args, theme) {
            return new Text(theme.fg('toolTitle', theme.bold('memory_summarize')), 0, 0);
        },
        renderResult() {
            return new Container();
        },
    });

    pi.registerCommand('memory_settings', {
        description: '交互式配置 Memory',
        handler: async (_args, ctx) => {
            if (!ctx.hasUI) {
                pi.sendMessage({ customType: 'text', content: '/memory_settings 仅支持交互式 UI。', display: true });
                return;
            }
            await ctx.waitForIdle();
            const previousAuto = settings.summarize.auto;
            try {
                const updated = await configureMemorySettings(ctx, settings);
                if (!updated) return;
                settings = updated;
                const reloadHint = previousAuto !== updated.summarize.auto
                    ? '；自动总结工具状态将在 /reload 后同步'
                    : '';
                ctx.ui.notify(`Memory 设置已保存${reloadHint}`, 'info');
            } catch (error) {
                const message = error instanceof Error ? error.message : String(error);
                ctx.ui.notify(`Memory 设置保存失败：${message}`, 'error');
            }
        },
    });

    pi.registerCommand('summarize', {
        description: '总结当前任务并沉淀核心 indexed memory',
        handler: async (_args, ctx) => {
            await ctx.waitForIdle();
            await clearSummaryRequest(ctx);
            void enqueueSummary(ctx);
        },
    });
};

export default memoryExtension;
