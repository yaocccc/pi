import { ToolExecutionComponent } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences, truncateToWidth } from '@earendil-works/pi-tui';
import { measureWidth as visibleWidth, padToWidth, replaceVisibleContent, toolFrameTop } from './utils.ts';

import { colorToolTokens, estimateToolTokens, formatToolTokens } from './tool-tokens.ts';
export { estimateToolTokens, formatToolTokens } from './tool-tokens.ts';
type ToolResult = Parameters<typeof estimateToolTokens>[1];
type ToolState = { toolName?: string; args?: unknown; result?: ToolResult; isPartial?: boolean };
const PATCH = Symbol.for('pi.extensions.ui.tool-context.v10');
const THEME = Symbol.for('@earendil-works/pi-coding-agent:theme');

export const patchToolContext = (): void => {
    const prototype = ToolExecutionComponent.prototype as typeof ToolExecutionComponent.prototype & { [PATCH]?: boolean };
    if (prototype[PATCH]) return;
    const original = prototype.render;
    const cache = new WeakMap<object, { args: unknown; result: ToolResult; tokens: number; label: string;
        header?: { input: string; theme: unknown; output: string } }>();
    const updateDisplay = (prototype as any).updateDisplay;
    (prototype as any).updateDisplay = function () {
        cache.delete(this);
        updateDisplay.call(this);
    };
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
        const theme = (globalThis as any)[THEME];
        const framed = /^╭─.*╮$/.test(stripTerminalSequences(lines[0]!));
        if (framed) {
            if (entry.header?.input !== lines[0] || entry.header?.theme !== theme) {
                const tokens = theme ? colorToolTokens(entry.tokens, entry.label, theme) : entry.label;
                entry.header = { input: lines[0]!, theme, output: replaceVisibleContent(lines[0]!,
                    toolFrameTop(`${tool.toolName ?? '工具'} ${tokens}`, visibleWidth(lines[0]!))) };
            }
            const output = [...lines];
            output[0] = entry.header.output;
            return output;
        }
        // Unframed custom shells retain the native inline badge; hidden/blank rows stay untouched.
        const index = lines.findIndex(line => stripTerminalSequences(line).trim().length > 0);
        if (index < 0) return lines;
        const row = lines[index]!;
        const available = width;
        const suffix = ` ${entry.label} `;
        if (available < visibleWidth(suffix) + 2) return lines;
        const label = theme ? colorToolTokens(entry.tokens, suffix, theme) : suffix;
        const prefix = padToWidth(truncateToWidth(row, available - visibleWidth(suffix), ''), available - visibleWidth(suffix));
        const output = [...lines];
        output[index] = prefix + label;
        return output;
    };
    prototype[PATCH] = true;
};
