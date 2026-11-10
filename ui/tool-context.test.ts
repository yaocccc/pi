import assert from 'node:assert/strict';
import test from 'node:test';
import { ToolExecutionComponent } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences, visibleWidth } from '@earendil-works/pi-tui';
import { estimateToolTokens, formatToolTokens, patchToolContext } from './tool-context.ts';

test('historical estimate includes full args and result, with compact units', () => {
    assert.equal(estimateToolTokens({}, { content: [{ type: 'text', text: 'abcdefgh' }] }), 3);
    assert.equal(formatToolTokens(2400), '≈2.4k tok');
    assert.equal(formatToolTokens(860), '≈860 tok');
});

test('badge appears only after completion, preserves hidden rows and width, and does not stack', () => {
    const proto = ToolExecutionComponent.prototype;
    proto.render = function (width: number) {
        if ((this as any).hidden) return [];
        if ((this as any).framed) return ['╭' + '─'.repeat(width - 2) + '╮', '│' + 'Read result'.padEnd(width - 2) + '│', '╰' + '─'.repeat(width - 2) + '╯'];
        return [' '.repeat(width), 'Read result'.padEnd(width), ' '.repeat(width)];
    };
    patchToolContext();
    const patched = proto.render;
    patchToolContext();
    assert.equal(proto.render, patched);
    const tool = Object.create(proto);
    tool.args = {};
    assert.doesNotMatch(tool.render(40).join('\n'), /≈/);
    tool.result = { content: [{ type: 'text', text: 'abcdefgh' }] };
    tool.isPartial = true;
    assert.doesNotMatch(tool.render(40).join('\n'), /≈/);
    tool.isPartial = false;
    assert.match(stripTerminalSequences(tool.render(40)[1]), /≈3 tok $/);
    for (const width of [1, 2, 10, 40]) {
        assert.equal(tool.render(width).length, 3);
        assert.equal(tool.render(width)[0], ' '.repeat(width), 'top padding is unchanged');
        assert.equal(tool.render(width)[2], ' '.repeat(width), 'bottom padding is unchanged');
        // The mock itself overflows for tiny widths; decoration must not introduce overflow.
        if (width >= 10) assert.ok(tool.render(width).every((line: string) => visibleWidth(line) <= width));
    }
    tool.result = { content: [{ type: 'text', text: 'a'.repeat(80) }], isError: true };
    assert.match(stripTerminalSequences(tool.render(40)[1]), /≈21 tok $/);
    const themeKey = Symbol.for('@earendil-works/pi-coding-agent:theme');
    const previousTheme = (globalThis as any)[themeKey];
    try {
        (globalThis as any)[themeKey] = {
            fg: (color: string, text: string) => {
                assert.equal(color, 'dim', 'badge follows the theme dim color');
                return `\x1b[2m${text}\x1b[22m`;
            },
            bg: (color: string, text: string) => `\x1b[48;5;${color === 'toolErrorBg' ? 1 : 2}m${text}\x1b[49m`,
        };
        for (const isError of [false, true]) {
            tool.result = { content: [{ type: 'text', text: 'abcdefgh' }], isError };
            const row = tool.render(40)[1];
            assert.match(row, /\x1b\[2m ≈3 tok/);
            assert.doesNotMatch(row, /\x1b\[48;/, 'badge never restores a background block');
            tool.framed = true;
            tool.toolName = 'read';
            const framed = tool.render(40);
            assert.match(stripTerminalSequences(framed[0]), /^╭─ read ≈3 tok ─+╮$/);
            assert.equal(framed[2], '╰' + '─'.repeat(38) + '╯');
            assert.equal(framed[1], '│' + 'Read result'.padEnd(38) + '│', 'title tokens never overwrite native content or padding');
            assert.doesNotMatch(framed.slice(1).join('\n'), /tok/);
            assert.ok(framed.every((line: string) => visibleWidth(line) === 40));
            assert.doesNotMatch(framed.join('\n'), /\x1b\[48;/);
            tool.framed = false;
        }
    } finally {
        (globalThis as any)[themeKey] = previousTheme;
    }
    tool.hidden = true;
    assert.deepEqual(tool.render(40), []);
});
