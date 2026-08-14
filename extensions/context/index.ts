import {
    estimateTokens,
    type ExtensionAPI,
    type ExtensionCommandContext,
} from '@earendil-works/pi-coding-agent';
import { Key, matchesKey, truncateToWidth, visibleWidth, wrapTextWithAnsi } from '@earendil-works/pi-tui';
import { enablePopupMouseWheel, getMouseWheelDirection } from '../ui/mouse-wheel';

type ContextColor =
    | 'syntaxFunction'
    | 'success'
    | 'warning'
    | 'error'
    | 'mdCode'
    | 'thinkingHigh'
    | 'thinkingMedium'
    | 'bashMode'
    | 'dim';

const CONTEXT_COLORS = {
    systemPrompt: 'error',
    tools: 'syntaxFunction',
    contextFiles: 'thinkingMedium',
    skills: 'warning',
    userMessages: 'success',
    agentMessages: 'thinkingHigh',
    toolOutput: 'mdCode',
    compactionSummaries: 'bashMode',
    freeSpace: 'dim',
} as const satisfies Record<string, ContextColor>;

type ContextDetail = {
    label: string;
    tokens: number;
};

type ContextPart = {
    label: string;
    tokens: number;
    color: ContextColor;
    details?: ContextDetail[];
};

type ToolGroupKey = 'systemTools' | 'customTools' | 'packageTools' | 'mcpTools';

type PreviewKey =
    | 'systemPrompt'
    | 'tools'
    | 'contextFiles'
    | 'skills'
    | 'userMessages'
    | 'agentMessages'
    | 'agentTextMessages'
    | 'agentThinkingMessages'
    | 'agentToolCallMessages';

type ContextPreview = {
    key: PreviewKey;
    label: string;
    title: string;
    color: ContextColor;
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
    parameters?: unknown;
    promptGuidelines?: string[];
    sourceInfo?: {
        path: string;
        source: string;
        origin: string;
    };
};

type ToolGroup = {
    key: ToolGroupKey;
    label: string;
    title: string;
    tools: ToolPreviewInfo[];
};

type MessagePreviews = {
    userMessages: string;
    agentTextMessages: string;
    agentThinkingMessages: string;
    agentToolCallMessages: string;
};

type ContextBreakdown = {
    parts: ContextPart[];
    options: SystemPromptOptions;
    systemPrompt: string;
    toolGroups: ToolGroup[];
    messagePreviews: MessagePreviews;
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

const safeJson = (value: unknown): string => {
    try {
        return JSON.stringify(value, null, 2) ?? String(value);
    } catch {
        return '[Unserializable value]';
    }
};

const formatUserContent = (content: unknown): string => {
    if (typeof content === 'string') return content;
    if (!Array.isArray(content)) return safeJson(content);
    return content.map((item) => {
        const block = item && typeof item === 'object' ? item as Record<string, unknown> : undefined;
        if (block?.type === 'text' && typeof block.text === 'string') return block.text;
        if (block?.type === 'image') {
            const mimeType = typeof block.mimeType === 'string' ? block.mimeType : 'unknown';
            const bytes = typeof block.data === 'string' ? Math.round((block.data.length * 3) / 4) : 0;
            return `[Image: ${mimeType}, approximately ${bytes} bytes]`;
        }
        return safeJson(item);
    }).join('\n');
};

const TOOL_GROUPS: Array<Omit<ToolGroup, 'tools'>> = [
    { key: 'systemTools', label: 'System Tools', title: 'System Tools' },
    { key: 'customTools', label: 'Custom Tools', title: 'Custom Tools' },
    { key: 'packageTools', label: 'Package Tools', title: 'Package Tools' },
    { key: 'mcpTools', label: 'MCP Tools', title: 'MCP Tools' },
];

const classifyTool = (tool: ToolPreviewInfo): ToolGroup['key'] => {
    const source = tool.sourceInfo?.source.toLowerCase() ?? '';
    const path = tool.sourceInfo?.path.toLowerCase() ?? '';
    if (source === 'builtin') return 'systemTools';
    if (source === 'sdk' || /(^|[/:_.-])mcp([/:_.-]|$)/u.test(`${source} ${path}`)) return 'mcpTools';
    if (tool.sourceInfo?.origin === 'package') return 'packageTools';
    return 'customTools';
};

const buildToolGroups = (
    options: SystemPromptOptions,
    toolsByName: Map<string, ToolPreviewInfo>,
): ToolGroup[] => {
    const groups = TOOL_GROUPS.map((group) => ({ ...group, tools: [] as ToolPreviewInfo[] }));
    for (const name of options.selectedTools ?? []) {
        const tool = toolsByName.get(name) ?? { name };
        groups.find((group) => group.key === classifyTool(tool))!.tools.push(tool);
    }
    return groups.filter((group) => group.tools.length > 0);
};

const toolTokens = (tool: ToolPreviewInfo, options: SystemPromptOptions): number => estimate({
    name: tool.name,
    description: tool.description,
    parameters: tool.parameters,
    promptSnippet: options.toolSnippets?.[tool.name],
    promptGuidelines: tool.promptGuidelines,
});

const scaleDetails = (details: ContextDetail[] | undefined, target: number): ContextDetail[] | undefined => {
    if (!details?.length) return details;
    const total = details.reduce((sum, detail) => sum + detail.tokens, 0);
    if (total <= 0 || target <= 0) return details;
    const scaled = details.map((detail) => ({
        ...detail,
        tokens: Math.round((detail.tokens / total) * target),
    }));
    const delta = target - scaled.reduce((sum, detail) => sum + detail.tokens, 0);
    const largest = scaled.reduce(
        (best, detail, index) => detail.tokens > scaled[best]!.tokens ? index : best,
        0,
    );
    scaled[largest]!.tokens += delta;
    return scaled;
};

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
    return scaled.map((part) => ({
        ...part,
        details: scaleDetails(part.details, part.tokens),
    }));
};

const collapseDetails = (details: ContextDetail[], visibleLimit = 5): ContextDetail[] => {
    const sorted = [...details].sort((a, b) => b.tokens - a.tokens);
    if (sorted.length <= visibleLimit) return sorted;
    const visible = sorted.slice(0, visibleLimit);
    visible.push({
        label: `Other (${sorted.length - visible.length})`,
        tokens: sorted.slice(visibleLimit).reduce((sum, detail) => sum + detail.tokens, 0),
    });
    return visible;
};

const collectBreakdown = (
    ctx: ExtensionCommandContext,
    toolsByName: Map<string, ToolPreviewInfo>,
): ContextBreakdown => {
    const options = (ctx.getSystemPromptOptions() ?? {}) as SystemPromptOptions;
    const systemPrompt = ctx.getSystemPrompt();
    const contextFileDetails = (options.contextFiles ?? []).map((file) => ({
        label: file.path.split(/[\\/]/u).pop() ?? file.path,
        tokens: estimate(file.content),
    }));
    const contextFiles = contextFileDetails.reduce((sum, file) => sum + file.tokens, 0);
    const skills = (options.skills ?? []).reduce(
        (sum, skill) => sum + estimate(skill.name) + estimate(skill.description) + estimate(skill.filePath),
        0,
    );
    const promptToolTokens = (options.selectedTools ?? []).reduce(
        (sum, name) => sum + estimate(name) + estimate(options.toolSnippets?.[name]),
        estimate(options.promptGuidelines),
    );
    const toolGroups = buildToolGroups(options, toolsByName);

    let user = 0;
    let agentText = 0;
    let agentThinking = 0;
    let agentToolCalls = 0;
    let toolOutput = 0;
    let summaries = 0;
    const outputByTool = new Map<string, number>();
    const userPreview: string[] = [];
    const agentTextPreview: string[] = [];
    const agentThinkingPreview: string[] = [];
    const agentToolCallPreview: string[] = [];
    let userIndex = 0;
    let agentTextIndex = 0;
    let agentThinkingIndex = 0;
    let agentToolCallIndex = 0;

    for (const entry of ctx.sessionManager.buildContextEntries()) {
        if (entry.type === 'message') {
            if (entry.message.role === 'user') {
                user += estimateTokens(entry.message);
                userPreview.push(`## User Message ${++userIndex}\n${formatUserContent(entry.message.content)}`);
            } else if (entry.message.role === 'assistant') {
                const model = `${entry.message.provider}/${entry.message.model}`;
                for (const block of entry.message.content) {
                    if (block.type === 'text') {
                        agentText += estimate(block);
                        agentTextPreview.push(`## Agent Text ${++agentTextIndex} · ${model}\n${block.text}`);
                    } else if (block.type === 'thinking') {
                        agentThinking += estimate(block);
                        const thinking = block.redacted ? '[Redacted thinking]' : block.thinking;
                        agentThinkingPreview.push(`## Agent Thinking ${++agentThinkingIndex} · ${model}\n${thinking}`);
                    } else if (block.type === 'toolCall') {
                        agentToolCalls += estimate(block);
                        agentToolCallPreview.push([
                            `## Tool Call ${++agentToolCallIndex} · ${block.name}`,
                            `Model: ${model}`,
                            'Arguments:',
                            safeJson(block.arguments),
                        ].join('\n'));
                    }
                }
            } else if (entry.message.role === 'toolResult') {
                const tokens = estimateTokens(entry.message);
                toolOutput += tokens;
                outputByTool.set(entry.message.toolName, (outputByTool.get(entry.message.toolName) ?? 0) + tokens);
            }
        } else if (entry.type === 'compaction' || entry.type === 'branch_summary') {
            summaries += estimate(entry);
        }
    }

    const system = Math.max(0, estimate(systemPrompt) - contextFiles - skills - promptToolTokens);
    const toolDetails = toolGroups.map((group) => ({
        label: group.label,
        tokens: group.tools.reduce((sum, tool) => sum + toolTokens(tool, options), 0),
    }));
    const tools = toolDetails.reduce((sum, group) => sum + group.tokens, 0);
    const agentDetails = [
        { label: 'Text Messages', tokens: agentText },
        { label: 'Thinking Messages', tokens: agentThinking },
        { label: 'Tool Call Messages', tokens: agentToolCalls },
    ].filter((detail) => detail.tokens > 0);
    const agentMessages = agentDetails.reduce((sum, detail) => sum + detail.tokens, 0);
    const outputDetails = collapseDetails(
        Array.from(outputByTool, ([label, tokens]) => ({ label, tokens })),
    );

    return {
        options,
        systemPrompt,
        toolGroups,
        messagePreviews: {
            userMessages: userPreview.join('\n\n') || 'No user messages.',
            agentTextMessages: agentTextPreview.join('\n\n') || 'No agent text messages.',
            agentThinkingMessages: agentThinkingPreview.join('\n\n') || 'No agent thinking messages.',
            agentToolCallMessages: agentToolCallPreview.join('\n\n') || 'No agent tool call messages.',
        },
        parts: [
            { label: 'System Prompt', tokens: system, color: CONTEXT_COLORS.systemPrompt },
            { label: 'Tools', tokens: tools, color: CONTEXT_COLORS.tools, details: toolDetails },
            { label: 'Context Files', tokens: contextFiles, color: CONTEXT_COLORS.contextFiles, details: contextFileDetails },
            { label: 'Skills', tokens: skills, color: CONTEXT_COLORS.skills },
            { label: 'User Messages', tokens: user, color: CONTEXT_COLORS.userMessages },
            { label: 'Agent Messages', tokens: agentMessages, color: CONTEXT_COLORS.agentMessages, details: agentDetails },
            { label: 'Tool Output', tokens: toolOutput, color: CONTEXT_COLORS.toolOutput, details: outputDetails },
            { label: 'Compaction Summaries', tokens: summaries, color: CONTEXT_COLORS.compactionSummaries },
        ].filter((part) => part.tokens > 0) as ContextPart[],
    };
};

const MOUSE_WHEEL_SCROLL_LINES = 3;

const showTextPreview = async (
    ctx: ExtensionCommandContext,
    title: string,
    color: ContextColor,
    rawContent: string,
): Promise<void> => {
    const content = normalizePreviewText(rawContent);
    let restoreMouseWheel: (() => void) | undefined;

    try {
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
            const scrollWithWheel = (direction: -1 | 1): void => {
                scrollTo(scrollOffset + direction * MOUSE_WHEEL_SCROLL_LINES);
            };
            restoreMouseWheel = enablePopupMouseWheel(tui, scrollWithWheel);

            return {
                invalidate() {
                    cachedWidth = undefined;
                    cachedLines = undefined;
                },
                handleInput(data: string) {
                    const wheelDirection = getMouseWheelDirection(data);
                    if (wheelDirection !== undefined) {
                        scrollWithWheel(wheelDirection);
                    } else if (matchesKey(data, Key.escape) || matchesKey(data, Key.ctrl('c'))) {
                        done(undefined);
                    } else if (matchesKey(data, Key.up)) {
                        scrollTo(scrollOffset - 1);
                    } else if (matchesKey(data, Key.down)) {
                        scrollTo(scrollOffset + 1);
                    } else if (matchesKey(data, 'pageUp')) {
                        scrollTo(scrollOffset - pageSize);
                    } else if (matchesKey(data, 'pageDown') || matchesKey(data, Key.space)) {
                        scrollTo(scrollOffset + pageSize);
                    } else if (data === 'g' || matchesKey(data, Key.home)) {
                        scrollTo(0);
                    } else if (data === 'G' || matchesKey(data, Key.end)) {
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
                        `${border('│')}${pad(` ${theme.bold(theme.fg(color, title))}`)}${border('│')}`,
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        ...Array.from({ length: pageSize }, (_, index) =>
                            `${border('│')}${pad(` ${visible[index] ?? ''}`)}${border('│')}`,
                        ),
                        `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                        `${border('│')}${pad(theme.fg('dim', ` ${start}-${end} / ${totalLines} lines · Wheel / ↑↓ scroll · Space next page · Esc back`))}${border('│')}`,
                        border(`╰${'─'.repeat(inner)}╯`),
                    ];
                },
            };
        },
            {
                overlay: true,
                overlayOptions: { anchor: 'center', width: '78%', minWidth: 48, maxHeight: '80%', margin: 2 },
            },
        );
    } finally {
        restoreMouseWheel?.();
    }
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
    const visiblePreviews = previews.filter((preview) => parts.some(
        (part) => part.label === preview.label || part.details?.some((detail) => detail.label === preview.label),
    ));
    let selectedPreviewIndex = 0;

    while (true) {
        let restoreMouseWheel: (() => void) | undefined;
        const action = await (async () => {
            try {
                return await ctx.ui.custom(
                    (tui, theme, _keybindings, done) => {
                let rowOffset = 0;
                let pageSize = 1;
                let totalRows = 1;
                let followSelection = true;
                const scrollWithWheel = (direction: -1 | 1): void => {
                    followSelection = false;
                    const next = Math.max(
                        0,
                        Math.min(
                            rowOffset + direction * MOUSE_WHEEL_SCROLL_LINES,
                            Math.max(0, totalRows - pageSize),
                        ),
                    );
                    if (next !== rowOffset) {
                        rowOffset = next;
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
                        } else if (matchesKey(data, Key.escape) || matchesKey(data, Key.ctrl('c'))) {
                            done(undefined);
                        } else if (matchesKey(data, Key.up) && visiblePreviews.length > 0) {
                            selectedPreviewIndex = (selectedPreviewIndex - 1 + visiblePreviews.length) % visiblePreviews.length;
                            followSelection = true;
                            tui.requestRender();
                        } else if (matchesKey(data, Key.down) && visiblePreviews.length > 0) {
                            selectedPreviewIndex = (selectedPreviewIndex + 1) % visiblePreviews.length;
                            followSelection = true;
                            tui.requestRender();
                        } else if (matchesKey(data, Key.space)) {
                            followSelection = false;
                            rowOffset = Math.min(rowOffset + pageSize, Math.max(0, totalRows - pageSize));
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
                        const barWidth = Math.max(1, inner - 2);
                        let remaining = barWidth;
                        const bar = parts.map((part, index) => {
                            const cells = index === parts.length - 1
                                ? remaining
                                : Math.min(remaining, Math.round((part.tokens / total) * barWidth));
                            remaining -= cells;
                            return theme.fg(part.color, '█'.repeat(Math.max(0, cells)));
                        }).join('');
                        const formatUsageRow = (left: string, tokens: number, share: number): string => {
                            const right = `${formatTokens(tokens).padStart(7)}  ${`${share.toFixed(1)}%`.padStart(6)}  `;
                            const maxLeftWidth = Math.max(1, inner - visibleWidth(right) - 3);
                            const fittedLeft = truncateToWidth(left, maxLeftWidth, '…');
                            const dots = '.'.repeat(Math.max(
                                1,
                                inner - visibleWidth(fittedLeft) - visibleWidth(right) - 2,
                            ));
                            return `${fittedLeft} ${theme.fg('dim', dots)} ${right}`;
                        };
                        const selectedLabel = visiblePreviews[selectedPreviewIndex]?.label;
                        const rows: Array<{ text: string; selected: boolean }> = [];
                        for (const part of parts) {
                            const share = (part.tokens / total) * 100;
                            const selected = part.label === selectedLabel;
                            const left = `${selected ? '›' : ' '} ${theme.fg(part.color, '■')} ${part.label}`;
                            const row = formatUsageRow(left, part.tokens, share);
                            rows.push({ text: selected ? theme.bg('selectedBg', pad(row)) : pad(row), selected });
                            for (const detail of part.details ?? []) {
                                const detailShare = (detail.tokens / total) * 100;
                                const detailSelected = detail.label === selectedLabel;
                                const detailLeft = `${detailSelected ? '›' : ' '}   ${theme.fg(part.color, '·')} ${detail.label}`;
                                const detailRow = formatUsageRow(detailLeft, detail.tokens, detailShare);
                                rows.push({
                                    text: detailSelected ? theme.bg('selectedBg', pad(detailRow)) : pad(detailRow),
                                    selected: detailSelected,
                                });
                            }
                        }

                        totalRows = rows.length;
                        pageSize = Math.max(1, Math.min(totalRows, Math.max(1, tui.terminal.rows - 10)));
                        const selectedRow = rows.findIndex((row) => row.selected);
                        if (followSelection && selectedRow >= 0) {
                            if (selectedRow < rowOffset) rowOffset = selectedRow;
                            else if (selectedRow >= rowOffset + pageSize) rowOffset = selectedRow - pageSize + 1;
                        }
                        rowOffset = Math.min(rowOffset, Math.max(0, totalRows - pageSize));
                        const visibleRows = rows.slice(rowOffset, rowOffset + pageSize);
                        const windowText = contextWindow > 0 ? formatTokens(contextWindow) : '?';
                        const title = theme.bold(theme.fg('accent', 'Context Usage'));
                        const summary = theme.fg('muted', `${formatTokens(used)} / ${windowText} tokens (${percent.toFixed(1)}%)`);
                        const range = totalRows > pageSize
                            ? `${rowOffset + 1}-${Math.min(totalRows, rowOffset + pageSize)} / ${totalRows} · `
                            : '';
                        const hint = visiblePreviews.length > 0
                            ? `${range}Wheel scroll · Enter preview · Esc close`
                            : `${range}Wheel scroll · Esc close`;

                        return [
                            border(`╭${'─'.repeat(inner)}╮`),
                            `${border('│')}${pad(` ${title}  ${summary}`)}${border('│')}`,
                            `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                            `${border('│')}${pad(` ${bar}`)}${border('│')}`,
                            `${border('│')}${' '.repeat(inner)}${border('│')}`,
                            ...visibleRows.map((row) => `${border('│')}${row.text}${border('│')}`),
                            `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                            `${border('│')}${pad(theme.fg('dim', ` ${hint}`))}${border('│')}`,
                            border(`╰${'─'.repeat(inner)}╯`),
                        ];
                    },
                };
            },
                    {
                        overlay: true,
                        overlayOptions: { anchor: 'center', width: 66, minWidth: 48, maxHeight: '90%', margin: 1 },
                    },
                );
            } finally {
                restoreMouseWheel?.();
            }
        })();

        if (!action) return;
        const preview = previewByKey.get(action as PreviewKey);
        if (preview) await showTextPreview(ctx, preview.title, preview.color, preview.content);
    }
};

const formatToolPreview = (group: ToolGroup, options: SystemPromptOptions): string => group.tools
    .map((tool) => {
        const lines = [`## ${tool.name}`];
        if (tool.description) lines.push(tool.description);
        if (tool.sourceInfo) lines.push(`Source: ${tool.sourceInfo.source}\nPath: ${tool.sourceInfo.path}`);
        if (options.toolSnippets?.[tool.name]) lines.push(`Prompt: ${options.toolSnippets[tool.name]}`);
        if (tool.promptGuidelines?.length) {
            lines.push('Prompt guidelines:', ...tool.promptGuidelines.map((guideline) => `- ${guideline}`));
        }
        if (tool.parameters) lines.push(`Parameters:\n${safeJson(tool.parameters)}`);
        return lines.join('\n');
    })
    .join('\n\n');

const formatToolsPreview = (groups: ToolGroup[], options: SystemPromptOptions): string => groups
    .map((group) => `# ${group.label}\n\n${formatToolPreview(group, options)}`)
    .join('\n\n');

export default function (pi: ExtensionAPI) {
    pi.registerCommand('context', {
        description: '查看当前上下文窗口的使用分布',
        handler: async (_args, ctx) => {
            const usage = ctx.getContextUsage();
            const contextWindow = usage?.contextWindow ?? ctx.model?.contextWindow ?? 0;
            const toolsByName = new Map<string, ToolPreviewInfo>(
                (pi.getAllTools() as unknown as ToolPreviewInfo[]).map((tool) => [tool.name, tool]),
            );
            const breakdown = collectBreakdown(ctx, toolsByName);
            const used = usage?.tokens ?? breakdown.parts.reduce((sum, part) => sum + part.tokens, 0);
            const parts = scaleParts(breakdown.parts, used);
            parts.push({
                label: 'Free Space',
                tokens: Math.max(0, contextWindow - used),
                color: CONTEXT_COLORS.freeSpace,
            });

            if (ctx.mode !== 'tui') {
                ctx.ui.notify(
                    parts.flatMap((part) => [
                        `${part.label}: ${formatTokens(part.tokens)} tokens`,
                        ...(part.details ?? []).map((detail) => `  ${detail.label}: ${formatTokens(detail.tokens)} tokens`),
                    ]).join('\n'),
                    'info',
                );
                return;
            }

            const options = breakdown.options;
            const previews: ContextPreview[] = [
                {
                    key: 'systemPrompt',
                    label: 'System Prompt',
                    title: 'System Prompt',
                    color: CONTEXT_COLORS.systemPrompt,
                    content: breakdown.systemPrompt,
                },
                {
                    key: 'tools',
                    label: 'Tools',
                    title: 'Tools',
                    color: CONTEXT_COLORS.tools,
                    content: formatToolsPreview(breakdown.toolGroups, options) || 'No active tools.',
                },
                {
                    key: 'contextFiles',
                    label: 'Context Files',
                    title: 'Context Files',
                    color: CONTEXT_COLORS.contextFiles,
                    content: (options.contextFiles ?? [])
                        .map((file) => `===== ${file.path} =====\n${file.content}`)
                        .join('\n\n') || 'No context files loaded.',
                },
                {
                    key: 'skills',
                    label: 'Skills',
                    title: 'Skills',
                    color: CONTEXT_COLORS.skills,
                    content: (options.skills ?? [])
                        .map((skill) => [
                            `## ${skill.name}`,
                            skill.description,
                            `Path: ${skill.filePath ?? 'unknown'}`,
                            `Model invocation: ${skill.disableModelInvocation ? 'disabled' : 'enabled'}`,
                        ].filter(Boolean).join('\n'))
                        .join('\n\n') || 'No skills loaded.',
                },
                {
                    key: 'userMessages',
                    label: 'User Messages',
                    title: 'User Messages',
                    color: CONTEXT_COLORS.userMessages,
                    content: breakdown.messagePreviews.userMessages,
                },
                {
                    key: 'agentMessages',
                    label: 'Agent Messages',
                    title: 'Agent Messages',
                    color: CONTEXT_COLORS.agentMessages,
                    content: [
                        `# Agent Text Messages\n\n${breakdown.messagePreviews.agentTextMessages}`,
                        `# Agent Thinking Messages\n\n${breakdown.messagePreviews.agentThinkingMessages}`,
                        `# Agent Tool Call Messages\n\n${breakdown.messagePreviews.agentToolCallMessages}`,
                    ].join('\n\n'),
                },
                {
                    key: 'agentTextMessages',
                    label: 'Text Messages',
                    title: 'Agent Text Messages',
                    color: CONTEXT_COLORS.agentMessages,
                    content: breakdown.messagePreviews.agentTextMessages,
                },
                {
                    key: 'agentThinkingMessages',
                    label: 'Thinking Messages',
                    title: 'Agent Thinking Messages',
                    color: CONTEXT_COLORS.agentMessages,
                    content: breakdown.messagePreviews.agentThinkingMessages,
                },
                {
                    key: 'agentToolCallMessages',
                    label: 'Tool Call Messages',
                    title: 'Agent Tool Call Messages',
                    color: CONTEXT_COLORS.agentMessages,
                    content: breakdown.messagePreviews.agentToolCallMessages,
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
