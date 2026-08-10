import {
    estimateTokens,
    type ExtensionAPI,
    type ExtensionCommandContext,
} from '@earendil-works/pi-coding-agent';
import { Key, matchesKey, truncateToWidth, visibleWidth, wrapTextWithAnsi } from '@earendil-works/pi-tui';

type ContextPart = {
    label: string;
    tokens: number;
    color: 'accent' | 'success' | 'warning' | 'muted' | 'dim';
};

type PreviewKey = 'systemPrompt' | 'tools' | 'contextFiles' | 'skills';

type ContextPreview = {
    key: PreviewKey;
    label: string;
    title: string;
    content: string;
};

type SystemPromptOptions = {
    contextFiles?: Array<{ path: string; content: string }>;
    skills?: Array<{
        name: string;
        description?: string;
        filePath?: string;
        disableModelInvocation?: boolean;
    }>;
    selectedTools?: string[];
    toolSnippets?: Record<string, string>;
    promptGuidelines?: string[];
};

type ToolPreviewInfo = {
    name: string;
    description?: string;
    promptGuidelines?: string[];
};

type ContextBreakdown = {
    parts: ContextPart[];
    options: SystemPromptOptions;
    systemPrompt: string;
};

const estimate = (value: unknown): number => {
    if (!value) return 0;
    const text = typeof value === 'string' ? value : JSON.stringify(value);
    return Math.ceil(text.length / 4);
};

const formatTokens = (tokens: number): string => {
    if (tokens < 1_000) return String(tokens);
    if (tokens < 100_000) return `${(tokens / 1_000).toFixed(1)}k`;
    return `${Math.round(tokens / 1_000)}k`;
};

const normalizePreviewText = (text: string): string => text
    .replace(/\r\n?/g, '\n')
    .replace(/\t/g, '  ')
    .replace(/[\u0000-\u0008\u000B\u000C\u000E-\u001F\u007F]/g, '');

const scaleParts = (parts: ContextPart[], target: number): ContextPart[] => {
    const estimated = parts.reduce((sum, part) => sum + part.tokens, 0);
    if (estimated === 0 || target <= 0) return parts;

    const scaled = parts.map((part) => ({
        ...part,
        tokens: Math.round((part.tokens / estimated) * target),
    }));
    const delta = target - scaled.reduce((sum, part) => sum + part.tokens, 0);
    const largest = scaled.reduce(
        (best, part, index) => part.tokens > scaled[best]!.tokens ? index : best,
        0,
    );
    scaled[largest]!.tokens += delta;
    return scaled;
};

const collectBreakdown = (ctx: ExtensionCommandContext): ContextBreakdown => {
    const options = (ctx.getSystemPromptOptions() ?? {}) as SystemPromptOptions;
    const systemPrompt = ctx.getSystemPrompt();
    const contextFiles = (options.contextFiles ?? []).reduce(
        (sum, file) => sum + estimate(file.content),
        0,
    );
    const skills = (options.skills ?? []).reduce(
        (sum, skill) => sum + estimate(skill.name) + estimate(skill.description) + estimate(skill.filePath),
        0,
    );
    const tools = (options.selectedTools ?? []).reduce(
        (sum, name) => sum + estimate(name) + estimate(options.toolSnippets?.[name]),
        estimate(options.promptGuidelines),
    );

    let user = 0;
    let assistant = 0;
    let toolResults = 0;
    let summaries = 0;

    for (const entry of ctx.sessionManager.buildContextEntries()) {
        if (entry.type === 'message') {
            const tokens = estimateTokens(entry.message);
            if (entry.message.role === 'user') user += tokens;
            else if (entry.message.role === 'assistant') assistant += tokens;
            else toolResults += tokens;
        } else if (entry.type === 'compaction' || entry.type === 'branch_summary') {
            summaries += estimate(entry);
        }
    }

    const system = Math.max(0, estimate(systemPrompt) - contextFiles - skills - tools);
    return {
        options,
        systemPrompt,
        parts: [
            { label: 'System prompt', tokens: system, color: 'accent' },
            { label: 'Tools', tokens: tools, color: 'success' },
            { label: 'Context files', tokens: contextFiles, color: 'warning' },
            { label: 'Skills', tokens: skills, color: 'warning' },
            { label: 'User messages', tokens: user, color: 'muted' },
            { label: 'Assistant messages', tokens: assistant, color: 'accent' },
            { label: 'Tool results', tokens: toolResults, color: 'dim' },
            { label: 'Compaction summaries', tokens: summaries, color: 'success' },
        ].filter((part) => part.tokens > 0) as ContextPart[],
    };
};

const showTextPreview = async (
    ctx: ExtensionCommandContext,
    title: string,
    rawContent: string,
): Promise<void> => {
    const content = normalizePreviewText(rawContent);

    await ctx.ui.custom(
        (tui, theme, _keybindings, done) => {
            let scrollOffset = 0;
            let pageSize = 1;
            let totalLines = 1;
            let cachedWidth: number | undefined;
            let cachedLines: string[] | undefined;
            const wrappedLines = (width: number): string[] => {
                if (cachedLines && cachedWidth === width) return cachedLines;
                cachedWidth = width;
                cachedLines = wrapTextWithAnsi(content, width);
                return cachedLines;
            };
            const scrollTo = (offset: number): void => {
                const next = Math.max(0, Math.min(offset, Math.max(0, totalLines - pageSize)));
                if (next !== scrollOffset) {
                    scrollOffset = next;
                    tui.requestRender();
                }
            };

            return {
                invalidate() {
                    cachedWidth = undefined;
                    cachedLines = undefined;
                },
                handleInput(data: string) {
                    if (matchesKey(data, Key.escape) || matchesKey(data, Key.ctrl('c'))) {
                        done(undefined);
                    } else if (matchesKey(data, Key.up)) {
                        scrollTo(scrollOffset - 1);
                    } else if (matchesKey(data, Key.down)) {
                        scrollTo(scrollOffset + 1);
                    } else if (matchesKey(data, 'pageUp')) {
                        scrollTo(scrollOffset - pageSize);
                    } else if (matchesKey(data, 'pageDown') || matchesKey(data, Key.space)) {
                        scrollTo(scrollOffset + pageSize);
                    } else if (matchesKey(data, Key.home)) {
                        scrollTo(0);
                    } else if (matchesKey(data, Key.end)) {
                        scrollTo(totalLines - pageSize);
                    }
                },
                render(width: number) {
                    const inner = Math.max(1, width - 2);
                    const terminalHeight = Math.max(1, tui.terminal.rows);
                    const availableHeight = Math.max(1, terminalHeight - 4);
                    const overlayHeight = Math.min(30, Math.max(1, Math.floor(terminalHeight * 0.8)), availableHeight);
                    pageSize = Math.max(1, overlayHeight - 6);
                    const wrapped = wrappedLines(Math.max(1, inner - 2));
                    totalLines = wrapped.length;
                    scrollOffset = Math.min(scrollOffset, Math.max(0, totalLines - pageSize));
                    const visible = wrapped.slice(scrollOffset, scrollOffset + pageSize);
                    const border = (text: string) => theme.fg('muted', text);
                    const pad = (text: string): string => {
                        const truncated = truncateToWidth(text, inner, '…');
                        return truncated + ' '.repeat(Math.max(0, inner - visibleWidth(truncated)));
                    };
                    const start = totalLines === 0 ? 0 : scrollOffset + 1;
                    const end = Math.min(totalLines, scrollOffset + pageSize);

                    return [
                        border(`╭${'─'.repeat(inner)}╮`),
                        `${border('│')}${pad(` ${theme.bold(theme.fg('accent', title))}`)}${border('│')}`,
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        ...Array.from({ length: pageSize }, (_, index) =>
                            `${border('│')}${pad(` ${visible[index] ?? ''}`)}${border('│')}`,
                        ),
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        `${border('│')}${pad(theme.fg('dim', ` ${start}-${end} / ${totalLines} lines · Space next page · Esc back`))}${border('│')}`,
                        border(`╰${'─'.repeat(inner)}╯`),
                    ];
                },
            };
        },
        {
            overlay: true,
            overlayOptions: { anchor: 'center', width: '85%', minWidth: 50, maxHeight: '80%', margin: 2 },
        },
    );
};

const showContext = async (
    ctx: ExtensionCommandContext,
    parts: ContextPart[],
    previews: ContextPreview[],
    used: number,
    contextWindow: number,
): Promise<void> => {
    const total = Math.max(contextWindow, used, 1);
    const percent = contextWindow > 0 ? (used / contextWindow) * 100 : 0;
    const previewByKey = new Map(previews.map((preview) => [preview.key, preview]));
    const visiblePreviews = previews.filter((preview) => parts.some((part) => part.label === preview.label));
    let selectedPreviewIndex = 0;

    while (true) {
        const action = await ctx.ui.custom(
            (tui, theme, _keybindings, done) => ({
                invalidate() {},
                handleInput(data: string) {
                    if (matchesKey(data, Key.escape) || matchesKey(data, Key.ctrl('c'))) {
                        done(undefined);
                    } else if (matchesKey(data, Key.up) && visiblePreviews.length > 0) {
                        selectedPreviewIndex = (selectedPreviewIndex - 1 + visiblePreviews.length) % visiblePreviews.length;
                        tui.requestRender();
                    } else if (matchesKey(data, Key.down) && visiblePreviews.length > 0) {
                        selectedPreviewIndex = (selectedPreviewIndex + 1) % visiblePreviews.length;
                        tui.requestRender();
                    } else if (matchesKey(data, Key.enter) && visiblePreviews.length > 0) {
                        done(visiblePreviews[selectedPreviewIndex]!.key);
                    }
                },
                render(width: number) {
                    const inner = Math.max(1, width - 2);
                    const pad = (text: string): string => {
                        const truncated = truncateToWidth(text, inner, '…');
                        return truncated + ' '.repeat(Math.max(0, inner - visibleWidth(truncated)));
                    };
                    const border = (text: string) => theme.fg('muted', text);
                    const barWidth = Math.max(1, Math.min(58, inner - 2));
                    let remaining = barWidth;
                    const bar = parts.map((part, index) => {
                        const cells = index === parts.length - 1
                            ? remaining
                            : Math.min(remaining, Math.round((part.tokens / total) * barWidth));
                        remaining -= cells;
                        return theme.fg(part.color, '█'.repeat(Math.max(0, cells)));
                    }).join('');
                    const labelWidth = Math.max(...parts.map((part) => part.label.length));
                    const selectedLabel = visiblePreviews[selectedPreviewIndex]?.label;
                    const rows = parts.map((part) => {
                        const share = (part.tokens / total) * 100;
                        const label = part.label.padEnd(labelWidth);
                        const selected = part.label === selectedLabel;
                        const row = `${selected ? '›' : ' '} ${theme.fg(part.color, '■')} ${label} ${formatTokens(part.tokens).padStart(7)}  ${share.toFixed(1).padStart(5)}%`;
                        return selected ? theme.bg('selectedBg', pad(row)) : pad(row);
                    });
                    const windowText = contextWindow > 0 ? formatTokens(contextWindow) : '?';
                    const title = theme.bold(theme.fg('accent', 'Context Usage'));
                    const summary = theme.fg('muted', `${formatTokens(used)} / ${windowText} tokens (${percent.toFixed(1)}%)`);
                    const hint = visiblePreviews.length > 0
                        ? ' Enter preview · Esc close'
                        : ' Esc close';

                    return [
                        border(`╭${'─'.repeat(inner)}╮`),
                        `${border('│')}${pad(` ${title}  ${summary}`)}${border('│')}`,
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        `${border('│')}${pad(` ${bar}`)}${border('│')}`,
                        `${border('│')}${' '.repeat(inner)}${border('│')}`,
                        ...rows.map((row) => `${border('│')}${row}${border('│')}`),
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        `${border('│')}${pad(theme.fg('dim', hint))}${border('│')}`,
                        border(`╰${'─'.repeat(inner)}╯`),
                    ];
                },
            }),
            {
                overlay: true,
                overlayOptions: { anchor: 'center', width: 64, minWidth: 44, maxHeight: '90%', margin: 1 },
            },
        );

        if (!action) return;
        const preview = previewByKey.get(action as PreviewKey);
        if (preview) await showTextPreview(ctx, preview.title, preview.content);
    }
};

export default function (pi: ExtensionAPI) {
    pi.registerCommand('context', {
        description: '查看当前上下文窗口的使用分布',
        handler: async (_args, ctx) => {
            const usage = ctx.getContextUsage();
            const contextWindow = usage?.contextWindow ?? ctx.model?.contextWindow ?? 0;
            const breakdown = collectBreakdown(ctx);
            const used = usage?.tokens ?? breakdown.parts.reduce((sum, part) => sum + part.tokens, 0);
            const parts = scaleParts(breakdown.parts, used);
            parts.push({
                label: 'Free space',
                tokens: Math.max(0, contextWindow - used),
                color: 'dim',
            });

            if (ctx.mode !== 'tui') {
                ctx.ui.notify(
                    parts.map((part) => `${part.label}: ${formatTokens(part.tokens)} tokens`).join('\n'),
                    'info',
                );
                return;
            }

            const options = breakdown.options;
            const toolsByName = new Map<string, ToolPreviewInfo>(
                (pi.getAllTools() as unknown as ToolPreviewInfo[]).map((tool) => [tool.name, tool]),
            );
            const tools = (options.selectedTools ?? []).map((name) => {
                const tool = toolsByName.get(name);
                const lines = [`## ${name}`];
                if (tool?.description) lines.push(tool.description);
                if (options.toolSnippets?.[name]) lines.push(`Prompt: ${options.toolSnippets[name]}`);
                if (tool?.promptGuidelines?.length) {
                    lines.push('Prompt guidelines:', ...tool.promptGuidelines.map((guideline) => `- ${guideline}`));
                }
                return lines.join('\n');
            });
            if (options.promptGuidelines?.length) {
                tools.push(`## Shared prompt guidelines\n${options.promptGuidelines.map((guideline) => `- ${guideline}`).join('\n')}`);
            }

            const previews: ContextPreview[] = [
                {
                    key: 'systemPrompt',
                    label: 'System prompt',
                    title: 'System Prompt',
                    content: breakdown.systemPrompt,
                },
                {
                    key: 'tools',
                    label: 'Tools',
                    title: 'Tools',
                    content: tools.join('\n\n') || 'No active tools.',
                },
                {
                    key: 'contextFiles',
                    label: 'Context files',
                    title: 'Context Files',
                    content: (options.contextFiles ?? [])
                        .map((file) => `===== ${file.path} =====\n${file.content}`)
                        .join('\n\n') || 'No context files loaded.',
                },
                {
                    key: 'skills',
                    label: 'Skills',
                    title: 'Skills',
                    content: (options.skills ?? [])
                        .map((skill) => [
                            `## ${skill.name}`,
                            skill.description,
                            `Path: ${skill.filePath ?? 'unknown'}`,
                            `Model invocation: ${skill.disableModelInvocation ? 'disabled' : 'enabled'}`,
                        ].filter(Boolean).join('\n'))
                        .join('\n\n') || 'No skills loaded.',
                },
            ].map((preview): ContextPreview => ({
                ...preview,
                key: preview.key as PreviewKey,
                content: normalizePreviewText(preview.content),
            }));

            await showContext(ctx, parts, previews, used, contextWindow);
        },
    });
}
