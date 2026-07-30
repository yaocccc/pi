import { UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Box, Text } from '@earendil-works/pi-tui';
import { foregroundFromRenderedBox, halfBlockLine, replaceVisibleContent } from './utils.ts';

let paddedBackgroundHalfBlockPatched = false;
let userMessageHalfBlockPatched = false;

type RenderablePrototype = {
    render(width: number): string[];
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
