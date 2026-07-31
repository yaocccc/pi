import { AssistantMessageComponent, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Box, Text } from '@earendil-works/pi-tui';
import { foregroundFromRenderedBox, halfBlockLine, replaceVisibleContent } from './utils.ts';

let paddedBackgroundHalfBlockPatched = false;
let userMessageHalfBlockPatched = false;

const COLLAPSED_THINKING_PATCH = Symbol.for('pi.extensions.ui.single-thinking-preview.v5');
const COLLAPSED_THINKING_LINE_WIDTH = 120;
const ANSI_BOLD = '\x1b[1m';
const ANSI_NORMAL_INTENSITY = '\x1b[22m';

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

const thinkingPreview = (message: AssistantMessage): string => {
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

    return lines.at(-1) ?? '';
};

const escapeMarkdown = (value: string): string => value.replace(/([\\`*_{}\[\]()<>#+\-.!|])/g, '\\$1');

const compactThinkingMessage = (message: AssistantMessage, preview: string): AssistantMessage => {
    const content: AssistantMessage['content'] = [];
    let inserted = false;

    for (const part of message.content) {
        if (part.type !== 'thinking') {
            content.push(part);
            continue;
        }
        if (inserted) continue;
        inserted = true;
        content.push({ ...part, thinking: `Thinking: ${ANSI_BOLD}${escapeMarkdown(preview)}${ANSI_NORMAL_INTENSITY}` });
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
        if (!preview) {
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
        const lines = originalRender.call(this, width);
        if (lines.length === 0) return lines;

        const fg = foregroundFromRenderedBox(lines);
        const patched = [...lines];

        patched[0] = replaceVisibleContent(patched[0]!, halfBlockLine(width, 'top', fg));
        patched[patched.length - 1] = replaceVisibleContent(patched[patched.length - 1]!, halfBlockLine(width, 'bottom', fg));
        return patched;
    };
};
