import { AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Box, stripTerminalSequences, visibleWidth, type Component, type Text, type TuiMouseEvent, type TuiMouseEventResult } from '@earendil-works/pi-tui';
import { hasNativeEditRenderer, patchToolFolding, ToolActivityGroup } from './folding.ts';
import { estimateToolTokens, formatToolTokens } from './tool-tokens.ts';
import { RoundFrame, layoutRounds } from './rounds.ts';
import { frameBorder as border, padToWidth, replaceVisibleContent, toolFrameTop } from './utils.ts';

const USER_MESSAGE_FRAME_PATCH = Symbol.for('pi.extensions.ui.user-message-frame.v4');
const USER_MESSAGE_RENDER = Symbol.for('pi.extensions.ui.user-message-native-render');
const USER_MESSAGE_MOUSE = Symbol.for('pi.extensions.ui.user-message-native-mouse');

const LEGACY_FINAL_RESPONSE_SEPARATOR_PATCH = Symbol.for('pi.extensions.ui.final-response-separator.v1');
const FINAL_RESPONSE_SEPARATOR_PATCH = Symbol.for('pi.extensions.ui.final-response-separator.v2');
const FINAL_RESPONSE_SEPARATOR_ACTIVITY_PATCH = Symbol.for('pi.extensions.ui.final-response-separator-activity.v3');
const COMPACT_TOOL_ACTIVITY_KEY = Symbol.for('pi.extensions.ui.compact-tool-activity.v10');
const FULLSCREEN_SCROLLBAR_PATCH = Symbol.for('pi.extensions.ui.fullscreen-scrollbar.v1');
const TOOL_DISPLAY_PATCH = Symbol.for('pi.extensions.ui.tool-display.v3');
const TOOL_SHELL_FRAME = Symbol.for('pi.extensions.ui.tool-shell-frame.v2');
const MERGE_CONSECUTIVE_TOOLS_PATCH = Symbol.for('pi.extensions.ui.merge-consecutive-tools.v35');
const RUNTIME_THEME_KEY = Symbol.for('@earendil-works/pi-coding-agent:theme');

let renderedActivitySinceUserMessage = false;
// Survives module reload: an already-installed v2 final separator closes over the
// previous module's activity flag, while the current tool renderer does not.
const compactToolActivity = ((globalThis as any)[COMPACT_TOOL_ACTIVITY_KEY] ??= { rendered: false }) as { rendered: boolean };

type AssistantMessage = Parameters<AssistantMessageComponent['updateContent']>[0];
type AssistantMessagePrototype = {
    lastMessage?: AssistantMessage;
    [key: symbol]: unknown;
};

type RuntimeTheme = {
    fg(color: string, text: string): string;
    bg(color: string, text: string): string;
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

type ToolDisplayState = {
    toolName?: unknown;
    args?: unknown;
    result?: Parameters<typeof estimateToolTokens>[1];
    isPartial?: boolean;
    outputPad?: number;
    getRenderShell?(): string;
    selfRenderContainer?: Component;
    callRendererComponent?: Box;
    contentBox?: Box;
    contentText?: Text;
};

type ToolExecutionPrototype = {
    render(width: number): string[];
    updateDisplay(): void;
    handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined;
    [key: symbol]: unknown;
};

const isVisuallyBlankLine = (line: string): boolean => line
    .replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)|.)/g, '')
    .replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f-\x9f]/g, '')
    .trim().length === 0;

const removeLeadingBlankLine = (lines: string[]): string[] => lines.length > 0 && isVisuallyBlankLine(lines[0]!)
    ? lines.slice(1)
    : lines;

// Style only tool-owned shells, leaving global Box/Text renderers and native children intact.
const frameToolShell = (component: Box | Text, name: string, titleWidth: () => number): void => {
    const shell = component as unknown as { paddingX: number; paddingY: number; bgFn?: (line: string) => string; customBgFn?: (line: string) => string; [TOOL_SHELL_FRAME]?: boolean };
    if (shell[TOOL_SHELL_FRAME]) return;
    const background = 'setBgFn' in component ? 'bgFn' : 'customBgFn';
    const render = component.render;
    const mouse = (component as Component).handleMouse;
    let hiddenTitleRows = 0;
    let frameWidth = 0;
    let cached: { width: number; titleWidth: number; theme: unknown; input: string[]; output: string[] } | undefined;
    const invalidate = component.invalidate;
    component.invalidate = () => { cached = undefined; invalidate.call(component); };
    component.invalidate();
    const withLayout = <T>(width: number, action: () => T): T => {
        const { paddingX, paddingY } = shell;
        const bg = shell[background];
        const borderWidth = width >= 3 ? 1 : 0;
        shell.paddingX = Math.min(paddingX + borderWidth, Math.max(0, Math.floor((width - 1) / 2)));
        shell.paddingY = borderWidth;
        shell[background] = undefined;
        try { return action(); }
        finally {
            shell.paddingX = paddingX;
            shell.paddingY = paddingY;
            shell[background] = bg;
        }
    };
    component.render = (width) => {
        if (width <= 0) { cached = undefined; hiddenTitleRows = frameWidth = 0; return []; }
        return withLayout(width, () => {
            // Always render native children to refresh streaming content and hit layouts.
            // Only the expensive measuring/framing pass is cached, one snapshot per shell.
            const input = render.call(component, width);
            const labelWidth = titleWidth();
            const theme = (globalThis as any)[RUNTIME_THEME_KEY];
            if (cached && cached.width === width && cached.titleWidth === labelWidth && cached.theme === theme
                && cached.input.length === input.length && input.every((line, index) => line === cached!.input[index])) {
                return cached.output.slice();
            }
            hiddenTitleRows = 0;
            let lines = input;
            const remember = (output: string[]) => {
                cached = { width, titleWidth: labelWidth, theme, input: input.slice(), output };
                return output.slice(); // callers may splice/replace rows
            };
            if (width < 3) return remember(lines.map(line => padToWidth(line, width)));
            // Move only a standalone duplicate heading. Inline arguments/status and semantic markers remain native.
            if (width > 6 && lines.length >= 3 && stripTerminalSequences(lines[1]!).trim() === name && !/\x1b\]133;/.test(lines[1]!)) {
                hiddenTitleRows = 1;
                lines = [lines[0]!, ...lines.slice(2)];
            }
            // Keep the native wrapping/layout width, shrinking only unused right space.
            const contentWidth = lines.slice(1, -1).reduce((max, line) => Math.max(max, visibleWidth(stripTerminalSequences(line).trimEnd())), 0);
            frameWidth = lines.some(line => /\x1b(?:_G|\]1337;File=|P[^\x1b]*q)/.test(line)) ? width
                : Math.min(width, Math.max(labelWidth + 6, contentWidth + shell.paddingX));
            return remember(lines.map((line, index) => {
                if (index === 0) return toolFrameTop(name, frameWidth);
                if (index === lines.length - 1) return border('╰', 'bottomLeft', 'tool') + border('─'.repeat(frameWidth - 2), 'bottom', 'tool') + border('╯', 'bottomRight', 'tool');
                const content = line.slice(1); // Replace only the border cell; retain outputPad inside it.
                const markers = replaceVisibleContent(content, '');
                return markers + border('│', 'left', 'tool') + padToWidth(content.slice(markers.length), frameWidth - 2) + border('│', 'right', 'tool');
            }));
        });
    };
    (component as Component).handleMouse = (event) => withLayout(event.width, () => {
        // Consume shell clicks before the native MouseRegion can toggle the tool.
        if (shell.paddingY && (event.x < 1 || event.x >= frameWidth - 1 || event.y < 1 || event.y >= event.height - 1)) {
            return event.type === 'click' && event.button === 'left' ? { handled: true, target: component } : undefined;
        }
        return mouse?.call(component, { ...event, y: event.y + hiddenTitleRows, height: event.height + hiddenTitleRows });
    });
    shell[TOOL_SHELL_FRAME] = true;
};

// The built-in edit renderer owns a Box inside its self shell. Flatten only
// that instance while the outer tool frame supplies padding/background policy.
const flattenedEditShells = new WeakSet<Box>();
const flattenEditShell = (component: Box): void => {
    if (flattenedEditShells.has(component)) return;
    const shell = component as unknown as { paddingX: number; paddingY: number; bgFn?: (line: string) => string };
    const render = component.render;
    const mouse = component.handleMouse;
    const withLayout = <T>(action: () => T): T => {
        const { paddingX, paddingY, bgFn } = shell;
        shell.paddingX = shell.paddingY = 0;
        shell.bgFn = undefined;
        try { return action(); }
        finally { Object.assign(shell, { paddingX, paddingY, bgFn }); }
    };
    component.invalidate();
    component.render = (width) => withLayout(() => render.call(component, width));
    component.handleMouse = (event) => withLayout(() => mouse.call(component, event));
    flattenedEditShells.add(component);
};

export const patchToolDisplay = (): void => {
    const prototype = ToolExecutionComponent.prototype as unknown as ToolExecutionPrototype;
    if (prototype[TOOL_DISPLAY_PATCH]) return;

    const originalRender = prototype.render;
    const originalMouse = prototype.handleMouse;
    const offsets = new WeakMap<object, number>();
    const workerShells = new WeakMap<object, Box>();
    const selfShells = new WeakMap<object, { content: Component; padding: number; box: Box }>();
    const tokenCache = new WeakMap<object, { args: unknown; result: ToolDisplayState['result']; width: number }>();
    const updateDisplay = prototype.updateDisplay;
    prototype.updateDisplay = function () {
        tokenCache.delete(this); // also cover updates that reuse the args/result object
        updateDisplay.call(this);
    };
    prototype.render = function patchedToolRender(this: ToolDisplayState, width: number): string[] {
        offsets.delete(this);
        workerShells.delete(this);
        if (width <= 0) return [];
        const name = typeof this.toolName === 'string' ? this.toolName : '工具';
        const titleWidth = () => {
            if (!this.result || this.isPartial) return visibleWidth(name);
            let cached = tokenCache.get(this);
            if (!cached || cached.args !== this.args || cached.result !== this.result) {
                cached = { args: this.args, result: this.result,
                    width: 1 + visibleWidth(formatToolTokens(estimateToolTokens(this.args, this.result))) };
                tokenCache.set(this, cached);
            }
            return visibleWidth(name) + cached.width;
        };
        if (this.contentBox) frameToolShell(this.contentBox, name, titleWidth);
        if (this.contentText) frameToolShell(this.contentText, name, titleWidth);
        // Edit's native call Box must not paint a second shell inside our frame.
        const editShell = hasNativeEditRenderer(this as unknown as ToolExecutionComponent) && this.callRendererComponent;
        const CoreBoxType = this.contentBox?.constructor as typeof Box | undefined;
        const isNativeEditBox = this.getRenderShell?.() === 'self' && editShell && CoreBoxType && editShell instanceof CoreBoxType;
        if (isNativeEditBox) flattenEditShell(editShell);
        // Self-rendered Worker controls stay hidden. Only visible cards get a frame.
        if ((this.toolName === 'worker' || isNativeEditBox) && this.getRenderShell?.() === 'self' && this.selfRenderContainer) {
            const CoreBox = this.contentBox?.constructor as typeof Box | undefined;
            const padding = this.outputPad ?? 1;
            let cached = selfShells.get(this);
            if (!cached || cached.content !== this.selfRenderContainer || cached.padding !== padding) {
                const box = new (CoreBox ?? Box)(padding, 0);
                frameToolShell(box, name, titleWidth);
                box.addChild(this.selfRenderContainer);
                cached = { content: this.selfRenderContainer, padding, box };
                selfShells.set(this, cached);
            }
            const shell = cached.box;
            const lines = shell.render(width);
            if (!lines.length) return [];
            workerShells.set(this, shell);
            compactToolActivity.rendered = true;
            return lines;
        }
        const nativeLines = originalRender.call(this, width);
        const lines = removeLeadingBlankLine(nativeLines);
        offsets.set(this, nativeLines.length - lines.length);
        if (lines.length) {
            renderedActivitySinceUserMessage = true;
            compactToolActivity.rendered = true;
        }
        return lines;
    };
    prototype.handleMouse = function (event) {
        const shell = workerShells.get(this);
        if (shell) return shell.handleMouse(event);
        const offset = offsets.get(this);
        if (offset === undefined) return;
        return originalMouse.call(this, { ...event, y: event.y + offset, height: event.height + offset });
    };
    prototype[TOOL_DISPLAY_PATCH] = true;
};

type ToolContainer = {
    children: Component[];
    mouseLayout?: { width: number; children: Array<{ component: Component; height: number }> };
    render(width: number): string[];
    [key: symbol]: unknown;
};

export const patchMergeConsecutiveTools = (): void => {
    patchToolFolding();
    // 从 ToolExecutionComponent 的继承链取得 Pi 实际使用的 Container，避免重复依赖实例导致补丁失效。
    const prototype = Object.getPrototypeOf(ToolExecutionComponent.prototype) as ToolContainer;
    if (prototype[MERGE_CONSECUTIVE_TOOLS_PATCH]) return;

    const originalRender = prototype.render;
    prototype.render = function patchedConsecutiveTools(this: ToolContainer, width: number): string[] {
        const hasTool = this.children.some((child) => child instanceof ToolExecutionComponent);
        const hasUserMessage = this.children.some((child) => child instanceof UserMessageComponent);
        const hasAssistant = this.children.some((child) => child instanceof AssistantMessageComponent);
        if (!hasTool && !hasUserMessage && !hasAssistant) return originalRender.call(this, width);

        const rendered = layoutRounds(this.children).map((child) => {
            const lines = child.render(width);
            if ((child instanceof ToolActivityGroup || child instanceof RoundFrame) && lines.length) {
                renderedActivitySinceUserMessage = true;
                compactToolActivity.rendered = true;
            }
            return { child, lines, offset: 0 };
        });
        // 历史用户输入不保留组件外的上下空行，但保留消息自身的边框。
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
                    rendered[next]!.offset++;
                }
                break;
            }
        }

        const lines: string[] = [];
        const mouseChildren: Array<{ component: Component; height: number }> = [];
        for (const { child, lines: childLines, offset } of rendered) {
            lines.push(...childLines);
            const component: Component = offset === 0 ? child : {
                render: () => childLines.slice(),
                invalidate: () => child.invalidate(),
                handleMouse: (event) => child.handleMouse?.({ ...event, y: event.y + offset, height: childLines.length + offset }),
            };
            mouseChildren.push({ component, height: childLines.length });
        }
        // Projected rounds and spacing change row counts; native hit testing must
        // use the actual drawn components, not the original transcript children.
        this.mouseLayout = { width, children: mouseChildren };
        return lines;
    };
    prototype[MERGE_CONSECUTIVE_TOOLS_PATCH] = true;
};

class FinalResponseGap {
    render(width: number): string[] {
        return [' '.repeat(Math.max(1, width))];
    }

    invalidate() {}
}

type FinalResponseComponent = AssistantMessagePrototype & {
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
    if (!prototype[FINAL_RESPONSE_SEPARATOR_PATCH]) {
    const hasLegacySeparatorPatch = Boolean(prototype[LEGACY_FINAL_RESPONSE_SEPARATOR_PATCH]);
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
            if (hasToolCalls) renderedActivitySinceUserMessage = true;
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
    }

    // On reload, the v2 wrapper may still hold a previous module's flag. Keep
    // its existing formatting, and add a gap only if visible v10 tool activity
    // was not already accounted for by that wrapper.
    if (prototype[FINAL_RESPONSE_SEPARATOR_ACTIVITY_PATCH]) return;
    const previousRender = prototype.render;
    prototype.render = function patchedFinalResponseActivity(this: FinalResponseComponent, width: number): string[] {
        const hadActivity = compactToolActivity.rendered;
        const lines = previousRender.call(this, width);
        const message = this.lastMessage;
        if (!message) return lines;
        const hasText = message.content.some((part) => part.type === 'text' && part.text.trim());
        const hasThinking = message.content.some((part) => part.type === 'thinking' && part.thinking.trim());
        const hasToolCalls = message.content.some((part) => part.type === 'toolCall');
        if (!hasText || hasToolCalls) return lines;
        compactToolActivity.rendered = false;
        if (hadActivity && !hasThinking && !(lines.length > 0 && isVisuallyBlankLine(lines[0]!))) {
            return [...new FinalResponseGap().render(width), ...lines];
        }
        return lines;
    };
    prototype[FINAL_RESPONSE_SEPARATOR_ACTIVITY_PATCH] = true;
};

export const patchUserMessageFrame = (): void => {
    const prototype = UserMessageComponent.prototype as UserMessageComponent & { [key: symbol]: unknown };
    if (prototype[USER_MESSAGE_FRAME_PATCH]) return;
    const originalRender = (prototype[USER_MESSAGE_RENDER] ??= prototype.render) as UserMessageComponent['render'];
    const originalMouse = (prototype[USER_MESSAGE_MOUSE] ??= prototype.handleMouse) as UserMessageComponent['handleMouse'];
    const layouts = new WeakMap<UserMessageComponent, { left: number; inset: number; width: number; height: number }>();
    const frames = new WeakMap<UserMessageComponent, { width: number; padding: number; input: string[]; output: string[] }>();

    prototype.render = function (width: number): string[] {
        renderedActivitySinceUserMessage = false;
        compactToolActivity.rendered = false;
        if (width <= 0) { layouts.delete(this); frames.delete(this); return []; }
        // Change only the native Markdown's display style, never the stored message.
        for (const child of this.children) {
            const markdown = child as Component & { defaultTextStyle?: { bgColor?: unknown; [key: string]: unknown } };
            if (markdown.defaultTextStyle?.bgColor) {
                markdown.defaultTextStyle = { ...markdown.defaultTextStyle, bgColor: undefined };
                markdown.invalidate();
            }
        }
        const inset = width >= 8 ? 1 : 0;
        const maximum = inset ? Math.max(8, Math.floor(width * 0.8)) : width;
        const nativeWidth = Math.max(3, maximum - inset * 2);
        const lines = originalRender.call(this, nativeWidth);
        const padding = (this as unknown as { outputPad: number }).outputPad;
        const cached = frames.get(this);
        // Native rendering still refreshes content and mouse layout. Reuse only the frame work.
        if (cached?.width === width && cached.padding === padding && cached.input.length === lines.length
            && lines.every((line, index) => line === cached.input[index])) return cached.output.slice();
        frames.delete(this);
        const bodyWidth = lines.slice(1, -1).reduce((max, line) => Math.max(max, visibleWidth(stripTerminalSequences(line).trimEnd())), 0);
        const frameWidth = inset ? Math.min(maximum, Math.max(8, bodyWidth + padding + 2)) : width;
        const left = width - frameWidth;
        layouts.set(this, { left, inset, width: nativeWidth, height: lines.length });
        if (!inset) return lines.map(line => padToWidth(line, width));
        const indent = ' '.repeat(left);
        const innerWidth = frameWidth - 2;
        const header = border('╭', 'topLeft', 'user') + border('─ ', 'top', 'user') + '你'
            + border(` ${'─'.repeat(frameWidth - 7)}`, 'top', 'user') + border('╮', 'topRight', 'user');
        const footer = border('╰', 'bottomLeft', 'user') + border('─'.repeat(innerWidth), 'bottom', 'user') + border('╯', 'bottomRight', 'user');
        const leftSide = border('│', 'left', 'user');
        const rightSide = border('│', 'right', 'user');
        const output = lines.map((line, index) => {
            if (index === 0) return replaceVisibleContent(line, indent + header);
            if (index === lines.length - 1) return replaceVisibleContent(line, indent + footer);
            const markers = replaceVisibleContent(line, '');
            return markers + indent + leftSide + padToWidth(line.slice(markers.length), innerWidth) + rightSide;
        });
        frames.set(this, { width, padding, input: lines.slice(), output });
        return output.slice();
    };
    prototype.handleMouse = function (event) {
        const layout = layouts.get(this);
        if (!layout) return;
        if (event.x < layout.left + layout.inset || event.x >= event.width - layout.inset) return;
        if (layout.inset && (event.y <= 0 || event.y >= layout.height - 1)) return;
        return originalMouse.call(this, { ...event, x: event.x - layout.left - layout.inset, width: layout.width });
    };
    prototype[USER_MESSAGE_FRAME_PATCH] = true;
};
