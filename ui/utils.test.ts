import assert from 'node:assert/strict';
import test from 'node:test';
import { truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';
import { frameBorder, measureWidth, padToWidth, toolFrameTop } from './utils.ts';

const originalPad = (text: string, width: number): string => {
    const truncated = truncateToWidth(text, width, '');
    return truncated + ' '.repeat(Math.max(0, width - visibleWidth(truncated)));
};

const samples = [
    '', 'hello world', '│ ASCII ╰─────╯', '\t', '│\tcolumn', '中文标题', 'a中文b',
    '😀', '👩‍💻', '🇨🇳', '1️⃣', 'e\u0301', '\u0301', 'क्‍ष', '\u200b', '\u200d',
    '\n', '\r', '\0', '\b', '\x7f', '\x85', '\ud800',
    '\x1b[31mhello\x1b[0m', '\x1b[38;2;120;150;180m│ a ╯\x1b[39m',
    '\x1b[31m', '│\x1b[0m', '\x1b[31m中文\x1b[0m', '\x1b[31m😀\x1b[0m',
    '\x1b]8;;https://example.com\x07link\x1b]8;;\x07',
    '\x1b]8;;https://example.com\x1b\\│ link\x1b]8;;\x1b\\',
    '\x1b]133;A\x07\x1b]133;B\x07│ code\x1b]133;C\x07',
    '\x1b_Gf=100;image\x1b\\', '\x1b]1337;File=payload\x07', '\x1bPqdata\x1b\\',
    'e\x1b[31m\u0301', '👩\x1b[31m‍💻', 'क्\x1b[31m‍ष',
    '\x1b[unterminated', '\x1b]unterminated', '\x1b',
];

// Deterministic mixtures also cover ANSI boundaries within Unicode graphemes.
let seed = 12345;
const next = () => (seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0);
const parts = ['a', ' ', '│', '─', '中', '😀', '\u0301', '\u200d', '\t', '\x1b[31m', '\x1b[0m', '\x1b]133;A\x07'];
for (let i = 0; i < 300; i++) {
    let text = '';
    for (let j = 0; j < 8; j++) text += parts[next() % parts.length];
    samples.push(text);
}

test('measureWidth agrees with native width for every box-drawing character', () => {
    for (let code = 0x2500; code <= 0x257f; code++) {
        const text = `\x1b[31m${String.fromCharCode(code)} ASCII\x1b[0m`;
        assert.equal(measureWidth(text), visibleWidth(text), JSON.stringify(text));
    }
});

test('measureWidth preserves Unicode, control, ANSI, OSC and tab widths', () => {
    for (const text of samples) assert.equal(measureWidth(text), visibleWidth(text), JSON.stringify(text));
});

test('padToWidth is byte-identical to native truncation and padding', () => {
    for (const text of samples) {
        for (const width of [-4, -1, 0, 0.5, 1, 2, 3, 4, 5, 8, 12, 40, Number.NaN]) {
            assert.equal(padToWidth(text, width), originalPad(text, width), `${JSON.stringify(text)} at ${width}`);
        }
    }
});

test('single-cell SGR truncation preserves every cut boundary without grapheme segmentation', () => {
    const box = Array.from({ length: 128 }, (_, index) => String.fromCharCode(0x2500 + index)).join('');
    const texts = [
        'plain ASCII', `│${box}╯`, '\x1b[mabc\x1b[0m',
        '\x1b[31m\x1b[1ma\x1b[0m\x1b[32mb\x1b[m│\x1b[39m',
        '\x1b[38:2::120:150:180m╭──\x1b[0m ╮\x1b[m',
        'a\x1b[31mb\x1b[32mc\x1b[39m   ',
        '\x1b[31m\x1b[0m', '',
    ];
    const cases = texts.flatMap(text => Array.from({ length: visibleWidth(text) * 2 + 5 }, (_, index) => {
        const width = index / 2;
        return { text, width, expected: originalPad(text, width) };
    }));
    const original = Intl.Segmenter.prototype.segment;
    let calls = 0;
    Intl.Segmenter.prototype.segment = function (input: string) { calls++; return original.call(this, input); };
    try {
        for (const { text, width, expected } of cases) {
            assert.equal(padToWidth(text, width), expected, `${JSON.stringify(text)} at ${width}`);
        }
        assert.equal(calls, 0, 'safe SGR text never enters Intl.Segmenter');
    } finally { Intl.Segmenter.prototype.segment = original; }
});

test('SGR fast truncation leaves other escape protocols and exceptional widths native', () => {
    const texts = [
        ...samples, '\x1b[31G│abcdef', '\x1b[31Ka│b', '\x1b[;m│abc',
        '\x1b]8;;https://example.com\x07\x1b[31mabc│\x1b[0m',
        '\x1b_abc\x07│abcd', '\x1b[31m中文…\x1b[0m',
    ];
    const outcome = (action: () => string) => {
        try { return { value: action() }; }
        catch (error) { return { error: (error as Error).name }; }
    };
    for (const text of texts) {
        for (const width of [-Infinity, -1, 0, 0.1, 0.9, 1.5, 3.5, 40, NaN, Infinity]) {
            assert.deepEqual(outcome(() => padToWidth(text, width)), outcome(() => originalPad(text, width)),
                `${JSON.stringify(text)} at ${width}`);
        }
    }
});

test('tool headings retain exact frame bytes with the width fast path', () => {
    for (const title of samples.slice(0, 24)) {
        for (const width of [0, 2, 5, 6, 8, 12, 40]) {
            const label = width >= 6 ? truncateToWidth(title, width - 6, '') : '';
            const middle = label ? frameBorder('─ ', 'top', 'tool') + label
                + frameBorder(' ' + '─'.repeat(width - visibleWidth(label) - 5), 'top', 'tool')
                : frameBorder('─'.repeat(Math.max(0, width - 2)), 'top', 'tool');
            const expected = frameBorder('╭', 'topLeft', 'tool') + middle + frameBorder('╮', 'topRight', 'tool');
            assert.equal(toolFrameTop(title, width), expected);
        }
    }
});
