import { AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Box, Spacer, Text } from '@earendil-works/pi-tui';
import { foregroundFromRenderedBox, halfBlockLine, padToWidth, replaceVisibleContent } from './utils.ts';

let paddedBackgroundHalfBlockPatched = false;
let userMessageHalfBlockPatched = false;

const COLLAPSED_THINKING_PATCH = Symbol.for('pi.extensions.ui.single-thinking-preview.v7');
const THINKING_SPACING_PATCH = Symbol.for('pi.extensions.ui.thinking-spacing.v3');
const LEGACY_FINAL_RESPONSE_SEPARATOR_PATCH = Symbol.for('pi.extensions.ui.final-response-separator.v1');
const FINAL_RESPONSE_SEPARATOR_PATCH = Symbol.for('pi.extensions.ui.final-response-separator.v2');
const FULLSCREEN_SCROLLBAR_PATCH = Symbol.for('pi.extensions.ui.fullscreen-scrollbar.v1');
const COMPACT_TOOL_DISPLAY_PATCH = Symbol.for('pi.extensions.ui.compact-tool-display.v9');
const MERGE_CONSECUTIVE_TOOLS_PATCH = Symbol.for('pi.extensions.ui.merge-consecutive-tools.v5');
const RUNTIME_THEME_KEY = Symbol.for('@earendil-works/pi-coding-agent:theme');
const COLLAPSED_THINKING_LINE_WIDTH = 120;

let renderedActivitySinceUserMessage = false;

type AssistantMessage = Parameters<AssistantMessageComponent['updateContent']>[0];
type AssistantMessagePrototype = {
    hideThinkingBlock: boolean;
    lastMessage?: AssistantMessage;
    updateContent(message: AssistantMessage): void;
    [key: symbol]: unknown;
};

type RenderablePrototype = {
    render(width: number): string[];
};

type RuntimeTheme = {
    fg(color: string, text: string): string;
    bg(color: string, text: string): string;
    bold(text: string): string;
};

type RuntimeThemePrototype = RuntimeTheme & {
    [key: symbol]: unknown;
};

export const patchFullscreenScrollbar = (): void => {
    // Pi 用全局 symbol 共享当前 Theme；从实例原型打补丁，主题热切换后仍然生效。
    const runtimeTheme = (globalThis as any)[RUNTIME_THEME_KEY] as RuntimeTheme | undefined;
    if (!runtimeTheme) return;

    const prototype = Object.getPrototypeOf(runtimeTheme) as RuntimeThemePrototype;
    if (prototype[FULLSCREEN_SCROLLBAR_PATCH]) return;

    const originalBg = prototype.bg;
    prototype.bg = function patchedThemeBackground(this: RuntimeTheme, color: string, text: string): string {
        if (color === 'scrollbarThumb') return this.fg('dim', '▐');
        return originalBg.call(this, color, text);
    };
    prototype[FULLSCREEN_SCROLLBAR_PATCH] = true;
};

type CompactToolExecution = {
    toolName?: unknown;
    args?: unknown;
    isPartial?: boolean;
    result?: { isError?: boolean };
};

type ToolExecutionPrototype = {
    render(width: number): string[];
    [key: symbol]: unknown;
};

const toolBackgroundColor = (tool: CompactToolExecution): string => tool.result?.isError === true
    ? 'toolErrorBg'
    : tool.isPartial ? 'toolPendingBg' : 'toolSuccessBg';

const toolBackgroundBlankLine = (tool: CompactToolExecution, width: number): string => {
    const blank = ' '.repeat(Math.max(1, width));
    const runtimeTheme = (globalThis as any)[RUNTIME_THEME_KEY] as RuntimeTheme | undefined;
    return runtimeTheme ? runtimeTheme.bg(toolBackgroundColor(tool), blank) : blank;
};

const usesCompactToolDisplay = (toolName: string): boolean => toolName.startsWith('ctx_')
    || toolName.startsWith('ff')
    || toolName === 'read'
    || toolName === 'find'
    || toolName === 'grep';

const SENSITIVE_TOOL_ARG = /(?:api.?key|authorization|cookie|password|private.?key|secret|token)/i;
const MAX_COMPACT_TOOL_ARGS = 4;
const MAX_COMPACT_ARG_LENGTH = 40;

const compactToolArgValue = (key: string, value: unknown): string => {
    if (SENSITIVE_TOOL_ARG.test(key)) return '[已隐藏]';
    if (typeof value === 'string') {
        const compact = value.replace(/\s+/g, ' ').trim();
        const truncated = compact.length > MAX_COMPACT_ARG_LENGTH ? `${compact.slice(0, MAX_COMPACT_ARG_LENGTH - 1)}…` : compact;
        return /\s/.test(truncated) ? JSON.stringify(truncated) : truncated;
    }
    if (typeof value === 'number' || typeof value === 'boolean' || value === null) return String(value);
    if (Array.isArray(value)) {
        if (value.every((item) => typeof item === 'string' || typeof item === 'number' || typeof item === 'boolean')) {
            const preview = value.slice(0, 2).map((item) => compactToolArgValue(key, item));
            return `[${preview.join(', ')}${value.length > preview.length ? ', …' : ''}]`;
        }
        return `[${value.length}项]`;
    }
    if (value && typeof value === 'object') return `{${Object.keys(value).length}项}`;
    return String(value);
};

const compactToolArgs = (args: unknown): string => {
    if (!args || typeof args !== 'object' || Array.isArray(args)) return '';
    return Object.entries(args)
        .slice(0, MAX_COMPACT_TOOL_ARGS)
        .map(([key, value]) => `${key}=${compactToolArgValue(key, value)}`)
        .join(' · ');
};

const isVisuallyBlankLine = (line: string): boolean => line
    .replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|.)/g, '')
    .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, '')
    .trim().length === 0;

const removeLeadingBlankLine = (lines: string[]): string[] => lines.length > 0 && isVisuallyBlankLine(lines[0]!)
    ? lines.slice(1)
    : lines;

export const patchCompactToolDisplay = (): void => {
    const prototype = ToolExecutionComponent.prototype as unknown as ToolExecutionPrototype;
    if (prototype[COMPACT_TOOL_DISPLAY_PATCH]) return;

    const originalRender = prototype.render;
    prototype.render = function patchedCompactToolRender(this: CompactToolExecution, width: number): string[] {
        renderedActivitySinceUserMessage = true;
        if (typeof this.toolName !== 'string' || !usesCompactToolDisplay(this.toolName)) {
            return removeLeadingBlankLine(originalRender.call(this, width));
        }

        const runtimeTheme = (globalThis as any)[RUNTIME_THEME_KEY] as RuntimeTheme | undefined;
        const isError = this.result?.isError === true;
        const args = compactToolArgs(this.args);
        const errorPrefix = isError ? `${runtimeTheme ? runtimeTheme.fg('error', '✗') : '✗'} ` : '';
        const line = runtimeTheme
            ? `${errorPrefix}${runtimeTheme.fg('toolTitle', runtimeTheme.bold(this.toolName))}${args ? `  ${runtimeTheme.fg('dim', args)}` : ''}`
            : `${errorPrefix}${this.toolName}${args ? `  ${args}` : ''}`;
        const safeWidth = Math.max(1, width);
        if (!runtimeTheme) return [padToWidth(line, safeWidth)];

        const content = runtimeTheme.bg(toolBackgroundColor(this), padToWidth(` ${line}`, safeWidth));
        const foreground = foregroundFromRenderedBox([content]);
        return [
            halfBlockLine(safeWidth, 'top', foreground),
            content,
            halfBlockLine(safeWidth, 'bottom', foreground),
        ];
    };
    prototype[COMPACT_TOOL_DISPLAY_PATCH] = true;
};

type ToolContainer = {
    children: Array<{ render(width: number): string[] }>;
    render(width: number): string[];
    [key: symbol]: unknown;
};

const isHalfBlockBoundary = (line: string, glyph: '▄' | '▀'): boolean => {
    const visible = line
        .replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|.)/g, '')
        .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, '')
        .trim();
    return visible.length > 0 && [...visible].every((character) => character === glyph);
};

const visibleToolContentLineCount = (lines: string[]): number => {
    const start = lines.length > 0 && isHalfBlockBoundary(lines[0]!, '▄') ? 1 : 0;
    const end = lines.length > start && isHalfBlockBoundary(lines[lines.length - 1]!, '▀') ? lines.length - 1 : lines.length;
    return lines.slice(start, end).filter((line) => !isVisuallyBlankLine(line)).length;
};

export const patchMergeConsecutiveTools = (): void => {
    // 从 ToolExecutionComponent 的继承链取得 Pi 实际使用的 Container，避免重复依赖实例导致补丁失效。
    const prototype = Object.getPrototypeOf(ToolExecutionComponent.prototype) as ToolContainer;
    if (prototype[MERGE_CONSECUTIVE_TOOLS_PATCH]) return;

    const originalRender = prototype.render;
    prototype.render = function patchedConsecutiveTools(this: ToolContainer, width: number): string[] {
        const hasTool = this.children.some((child) => child instanceof ToolExecutionComponent);
        const hasUserMessage = this.children.some((child) => child instanceof UserMessageComponent);
        if (!hasTool && !hasUserMessage) return originalRender.call(this, width);

        const rendered = this.children.map((child) => ({ child, lines: child.render(width) }));
        // 历史用户输入不保留组件外的上下空行，但保留消息自身的半高边框。
        for (let index = 0; index < rendered.length; index++) {
            if (!(rendered[index]!.child instanceof UserMessageComponent)) continue;

            for (let previous = index - 1; previous >= 0; previous--) {
                const previousLines = rendered[previous]!.lines;
                if (previousLines.length === 0) continue;
                if (!previousLines.every(isVisuallyBlankLine)) break;
                rendered[previous]!.lines = [];
            }

            for (let next = index + 1; next < rendered.length; next++) {
                const nextLines = rendered[next]!.lines;
                if (nextLines.length === 0) continue;
                if (nextLines.every(isVisuallyBlankLine)) {
                    rendered[next]!.lines = [];
                    continue;
                }
                if (isVisuallyBlankLine(nextLines[0]!)) {
                    if (nextLines.length > 1) nextLines[1] = replaceVisibleContent(nextLines[0]!, nextLines[1]!);
                    nextLines.shift();
                }
                break;
            }
        }

        // 仅按实际可见行判断连续性；tool 之间常夹着不渲染任何行的 AssistantMessageComponent。
        const visibleIndexes = rendered
            .map((entry, index) => entry.lines.length > 0 ? index : -1)
            .filter((index) => index >= 0);
        const removeTopAt = new Set<number>();
        const removeBottomAt = new Set<number>();
        const addBlankAfter = new Map<number, CompactToolExecution>();
        for (let position = 0; position < visibleIndexes.length - 1; position++) {
            const currentIndex = visibleIndexes[position]!;
            const nextIndex = visibleIndexes[position + 1]!;
            const current = rendered[currentIndex]!;
            const next = rendered[nextIndex]!;
            if (current.child instanceof ToolExecutionComponent
                && next.child instanceof ToolExecutionComponent
                && isHalfBlockBoundary(current.lines[current.lines.length - 1]!, '▀')
                && isHalfBlockBoundary(next.lines[0]!, '▄')) {
                removeBottomAt.add(currentIndex);
                removeTopAt.add(nextIndex);

                const currentIsMultiline = visibleToolContentLineCount(current.lines) > 1;
                const nextIsMultiline = visibleToolContentLineCount(next.lines) > 1;
                const currentEdgeIndex = current.lines.length - 2;
                const currentEdge = current.lines[currentEdgeIndex];
                const nextEdge = next.lines[1];
                if (currentIsMultiline || nextIsMultiline) {
                    const spacingTool = (currentIsMultiline ? current.child : next.child) as unknown as CompactToolExecution;
                    const styledBlank = toolBackgroundBlankLine(spacingTool, width);
                    if (currentEdge !== undefined && isVisuallyBlankLine(currentEdge)) current.lines[currentEdgeIndex] = styledBlank;
                    else if (nextEdge !== undefined && isVisuallyBlankLine(nextEdge)) next.lines[1] = styledBlank;
                    else addBlankAfter.set(currentIndex, spacingTool);
                }
            }
        }

        const lines: string[] = [];
        for (let index = 0; index < rendered.length; index++) {
            const childLines = rendered[index]!.lines;
            const start = removeTopAt.has(index) ? 1 : 0;
            const end = removeBottomAt.has(index) ? childLines.length - 1 : childLines.length;
            lines.push(...childLines.slice(start, end));
            const spacingTool = addBlankAfter.get(index);
            if (spacingTool) lines.push(toolBackgroundBlankLine(spacingTool, width));
        }
        return lines;
    };
    prototype[MERGE_CONSECUTIVE_TOOLS_PATCH] = true;
};

const thinkingPreview = (message: AssistantMessage): string[] => {
    const lines: string[] = [];

    for (const part of message.content) {
        if (part.type !== 'thinking') continue;
        for (const rawLine of part.thinking.split(/\r?\n/)) {
            const line = rawLine
                .replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|.)/g, '')
                .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, ' ')
                .replace(/\*\*/g, '')
                .replace(/\s+/g, ' ')
                .trim();
            if (!line) continue;

            lines.push(line.length > COLLAPSED_THINKING_LINE_WIDTH
                ? `…${line.slice(-(COLLAPSED_THINKING_LINE_WIDTH - 1))}`
                : line);
        }
    }

    return lines;
};

const escapeMarkdown = (value: string): string => value.replace(/([\\`*_{}\[\]()<>#+\-.!|])/g, '\\$1');

const compactThinkingMessage = (message: AssistantMessage, preview: string[]): AssistantMessage => {
    const content: AssistantMessage['content'] = [];
    let inserted = false;

    for (const part of message.content) {
        if (part.type !== 'thinking') {
            content.push(part);
            continue;
        }
        if (inserted) continue;
        inserted = true;
        const previewLines = preview.map(escapeMarkdown).join('  \n');
        content.push({ ...part, thinking: previewLines });
    }

    return { ...message, content };
};

export const patchCollapsedThinkingPreview = (): void => {
    const prototype = AssistantMessageComponent.prototype as unknown as AssistantMessagePrototype;
    if (prototype[COLLAPSED_THINKING_PATCH]) return;

    const originalUpdateContent = prototype.updateContent;

    prototype.updateContent = function patchedAssistantMessageContent(this: AssistantMessagePrototype, message: AssistantMessage): void {
        if (!this.hideThinkingBlock) {
            originalUpdateContent.call(this, message);
            return;
        }

        const preview = thinkingPreview(message);
        if (preview.length === 0) {
            originalUpdateContent.call(this, message);
            return;
        }

        const hideThinkingBlock = this.hideThinkingBlock;
        this.hideThinkingBlock = false;
        try {
            originalUpdateContent.call(this, compactThinkingMessage(message, preview));
        } finally {
            this.hideThinkingBlock = hideThinkingBlock;
            this.lastMessage = message;
        }
    };

    prototype[COLLAPSED_THINKING_PATCH] = true;
};

type ThinkingSpacingComponent = AssistantMessagePrototype & {
    contentContainer: {
        children: unknown[];
        removeChild(child: unknown): void;
    };
};

const hasVisibleContentAfterThinking = (message: AssistantMessage): boolean => {
    let lastThinkingIndex = -1;
    for (let index = message.content.length - 1; index >= 0; index--) {
        const part = message.content[index]!;
        if (part.type === 'thinking' && part.thinking.trim()) {
            lastThinkingIndex = index;
            break;
        }
    }
    return lastThinkingIndex >= 0 && message.content.slice(lastThinkingIndex + 1)
        .some((part) => (part.type === 'text' && part.text.trim()) || (part.type === 'thinking' && part.thinking.trim()));
};

export const patchThinkingSpacing = (): void => {
    const prototype = AssistantMessageComponent.prototype as unknown as ThinkingSpacingComponent;
    if (prototype[THINKING_SPACING_PATCH]) return;

    const originalUpdateContent = prototype.updateContent;
    prototype.updateContent = function patchedThinkingSpacing(this: ThinkingSpacingComponent, message: AssistantMessage): void {
        originalUpdateContent.call(this, message);
        if (!this.hideThinkingBlock || !message.content.some((part) => part.type === 'thinking' && part.thinking.trim())) return;

        const first = this.contentContainer.children[0];
        if (first instanceof Spacer) this.contentContainer.removeChild(first);

        if (hasVisibleContentAfterThinking(message)) {
            const spacerAfterThinking = this.contentContainer.children.find((child) => child instanceof Spacer);
            if (spacerAfterThinking) this.contentContainer.removeChild(spacerAfterThinking);
        }
    };
    prototype[THINKING_SPACING_PATCH] = true;
};

class FinalResponseGap {
    render(width: number): string[] {
        return [' '.repeat(Math.max(1, width))];
    }

    invalidate() {}
}

const finalTextBlockIndex = (message: AssistantMessage): number | undefined => {
    if (message.content.some((part) => part.type === 'toolCall')) return undefined;

    let renderedBlocks = 0;
    let hasPriorThinking = false;
    for (let index = 0; index < message.content.length; index++) {
        const part = message.content[index]!;
        if (part.type === 'text' && part.text.trim()) {
            if (hasPriorThinking) return renderedBlocks;
            renderedBlocks++;
            continue;
        }
        if (part.type !== 'thinking') continue;

        let hasVisibleThinking = false;
        for (; index < message.content.length; index++) {
            const thinking = message.content[index]!;
            if (thinking.type !== 'thinking') break;
            if (thinking.thinking.trim()) hasVisibleThinking = true;
        }
        index--;
        if (hasVisibleThinking) {
            renderedBlocks++;
            hasPriorThinking = true;
        }
    }
    return undefined;
};

type FinalResponseComponent = ThinkingSpacingComponent & {
    outputPad: number;
    render(width: number): string[];
};

const isLegacySeparatorRuleLine = (line: string): boolean => {
    const visible = line
        .replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|.)/g, '')
        .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, '')
        .trim();
    return visible.length >= 3 && /^─+$/.test(visible);
};

const collapseLegacyFinalSeparator = (lines: string[], width: number): string[] => {
    const separatorIndex = lines.findIndex(isLegacySeparatorRuleLine);
    if (separatorIndex < 0) return lines;

    let start = separatorIndex;
    let end = separatorIndex + 1;
    while (start > 0 && isVisuallyBlankLine(lines[start - 1]!)) start--;
    while (end < lines.length && isVisuallyBlankLine(lines[end]!)) end++;
    const blank = replaceVisibleContent(lines[start]!, ' '.repeat(Math.max(1, width)));
    return [...lines.slice(0, start), blank, ...lines.slice(end)];
};

export const patchFinalResponseSeparator = (): void => {
    const prototype = AssistantMessageComponent.prototype as unknown as FinalResponseComponent;
    if (prototype[FINAL_RESPONSE_SEPARATOR_PATCH]) return;

    const hasLegacySeparatorPatch = Boolean(prototype[LEGACY_FINAL_RESPONSE_SEPARATOR_PATCH]);
    const originalUpdateContent = prototype.updateContent;
    prototype.updateContent = function patchedFinalResponseContent(this: FinalResponseComponent, message: AssistantMessage): void {
        originalUpdateContent.call(this, message);
        for (const child of [...this.contentContainer.children]) {
            if ((child as { constructor?: { name?: string } }).constructor?.name === 'FinalResponseSeparator') {
                this.contentContainer.removeChild(child);
            }
        }

        const blockIndex = finalTextBlockIndex(message);
        if (blockIndex === undefined) return;
        const leadingSpacer = this.contentContainer.children[0] instanceof Spacer ? 1 : 0;
        this.contentContainer.children.splice(leadingSpacer + blockIndex, 0, new FinalResponseGap());
    };

    const originalRender = prototype.render;
    prototype.render = function patchedFinalResponseRender(this: FinalResponseComponent, width: number): string[] {
        let lines = originalRender.call(this, width);
        if (hasLegacySeparatorPatch) lines = collapseLegacyFinalSeparator(lines, width);

        const message = this.lastMessage;
        if (!message) return lines;
        const hasText = message.content.some((part) => part.type === 'text' && part.text.trim());
        const hasThinking = message.content.some((part) => part.type === 'thinking' && part.thinking.trim());
        const hasToolCalls = message.content.some((part) => part.type === 'toolCall');
        if (!hasText || hasToolCalls) {
            if (hasThinking || hasToolCalls) renderedActivitySinceUserMessage = true;
            return lines;
        }

        if (renderedActivitySinceUserMessage && !hasThinking) {
            renderedActivitySinceUserMessage = false;
            if (lines.length > 0 && isVisuallyBlankLine(lines[0]!)) return lines;
            return [...new FinalResponseGap().render(width), ...lines];
        }

        renderedActivitySinceUserMessage = false;
        return lines;
    };
    prototype[FINAL_RESPONSE_SEPARATOR_PATCH] = true;
};

const patchPaddedBackgroundComponent = (prototype: RenderablePrototype): void => {
    const originalRender = prototype.render;

    prototype.render = function patchedPaddedBackgroundRender(this: { paddingY?: number; bgFn?: unknown; customBgFn?: unknown }, width: number): string[] {
        const lines = originalRender.call(this, width);
        const hasComponentBg = typeof this.bgFn === 'function' || typeof this.customBgFn === 'function';
        if (!hasComponentBg || lines.length < 2 || (typeof this.paddingY === 'number' && this.paddingY <= 0)) return lines;

        const fg = foregroundFromRenderedBox(lines);
        if (!fg) return lines;

        const patched = [...lines];
        patched[0] = replaceVisibleContent(patched[0]!, halfBlockLine(width, 'top', fg));
        patched[patched.length - 1] = replaceVisibleContent(patched[patched.length - 1]!, halfBlockLine(width, 'bottom', fg));
        return patched;
    };
};

export const patchPaddedBackgroundHalfBlocks = (): void => {
    if (paddedBackgroundHalfBlockPatched) return;
    paddedBackgroundHalfBlockPatched = true;

    // Box 覆盖工具调用、custom message、compact/branch/skill 等带背景色块的组件。
    patchPaddedBackgroundComponent(Box.prototype as unknown as RenderablePrototype);
    // Text 覆盖少量直接用 Text + customBgFn 渲染背景的组件兜底。
    patchPaddedBackgroundComponent(Text.prototype as unknown as RenderablePrototype);
};

export const patchUserMessageHalfBlocks = (): void => {
    if (userMessageHalfBlockPatched) return;
    userMessageHalfBlockPatched = true;

    const prototype = UserMessageComponent.prototype as UserMessageComponent & { render(width: number): string[] };
    const originalRender = prototype.render;

    prototype.render = function patchedUserMessageRender(this: UserMessageComponent, width: number): string[] {
        renderedActivitySinceUserMessage = false;
        const lines = originalRender.call(this, width);
        if (lines.length === 0) return lines;

        const fg = foregroundFromRenderedBox(lines);
        const patched = [...lines];

        patched[0] = replaceVisibleContent(patched[0]!, halfBlockLine(width, 'top', fg));
        patched[patched.length - 1] = replaceVisibleContent(patched[patched.length - 1]!, halfBlockLine(width, 'bottom', fg));
        return patched;
    };
};
