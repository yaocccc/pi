import { ToolExecutionComponent } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences, truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';
import { padToWidth } from './utils.ts';

import { colorToolTokens, estimateToolTokens, formatToolTokens } from './tool-tokens.ts';
export { estimateToolTokens, formatToolTokens } from './tool-tokens.ts';
type ToolResult = Parameters<typeof estimateToolTokens>[1];
type ToolState = { args?: unknown; result?: ToolResult; isPartial?: boolean };
const PATCH = Symbol.for('pi.extensions.ui.tool-context.v7');
const THEME = Symbol.for('@earendil-works/pi-coding-agent:theme');

export const patchToolContext = (): void => {
    const prototype = ToolExecutionComponent.prototype as typeof ToolExecutionComponent.prototype & { [PATCH]?: boolean };
    if (prototype[PATCH]) return;
    const original = prototype.render;
    const cache = new WeakMap<object, { args: unknown; result: ToolResult; tokens: number; label: string }>();
    prototype.render = function (width: number): string[] {
        const lines = original.call(this, width);
        const tool = this as unknown as ToolState;
        if (!tool.result || tool.isPartial || !lines.length) return lines;
        let entry = cache.get(this);
        if (!entry || entry.result !== tool.result || entry.args !== tool.args) {
            const tokens = estimateToolTokens(tool.args, tool.result);
            entry = { args: tool.args, result: tool.result, tokens, label: formatToolTokens(tokens) };
            cache.set(this, entry);
        }
        // Keep hidden control rows hidden; never add a new line or decorate borders.
        const index = lines.findIndex(line => {
            const text = stripTerminalSequences(line).trim();
            return text.length > 0 && !/^[▄▀]+$/.test(text);
        });
        const suffix = ` ${entry.label} `;
        if (index < 0 || width < visibleWidth(suffix) + 2) return lines;
        const theme = (globalThis as any)[THEME];
        const background = tool.result.isError ? 'toolErrorBg' : 'toolSuccessBg';
        // The original row/truncation can end with an ANSI background reset.
        // Start the badge's background explicitly rather than relying on an outer wrap.
        const label = theme ? theme.bg(background, colorToolTokens(entry.tokens, suffix, theme)) : suffix;
        const prefix = padToWidth(truncateToWidth(lines[index]!, width - visibleWidth(suffix), ''), width - visibleWidth(suffix));
        const output = [...lines];
        output[index] = theme ? theme.bg(background, prefix) + label : prefix + label;
        return output;
    };
    prototype[PATCH] = true;
};
