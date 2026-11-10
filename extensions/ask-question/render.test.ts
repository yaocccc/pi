import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { CURSOR_MARKER, stripTerminalSequences, visibleWidth } from '@earendil-works/pi-tui';
import askQuestion from './index.ts';
import { frameBorder } from '../shared/frame.ts';

const piRoot = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme, theme } = await import(pathToFileURL(join(piRoot, 'modes/interactive/theme/theme.js')).href);
initTheme('dark', false);

const plain = stripTerminalSequences;
const left = frameBorder('│', 'left', 'input');
const right = frameBorder('│', 'right', 'input');
const unframe = (line: string) => line.startsWith(left) && line.endsWith(right) ? line.slice(left.length, -right.length) : line;
const assertFrame = (lines: string[], width: number) => {
    assert.equal(plain(lines[0]!), '╭' + '─'.repeat(width - 2) + '╮');
    assert.equal(plain(lines.at(-1)!), '╰' + '─'.repeat(width - 2) + '╯');
    assert.ok(lines.slice(1, -1).every(line => line.startsWith(left) && line.endsWith(right)));
    assert.ok(lines.every(line => visibleWidth(line) === width));
    assert.doesNotMatch(lines.join('\n'), /\x1b\[48;|[▄▀]/, 'no solid or half-block background');
};
const compact = (lines: string[]) => lines.map((line) => plain(line).trimEnd()).join('').replace(/\s/g, '');
const flush = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); };

function harness(params: object) {
    let tool: any;
    let component: any;
    let finish!: (value: unknown) => void;
    const tui = { requestRender() {}, terminal: { rows: 24, columns: 80 } };
    askQuestion({ registerTool: (registered: any) => { tool = registered; }, on() {} } as any);
    const ctx = { hasUI: true, mode: 'tui', ui: {
        custom: (factory: any) => new Promise((resolve) => {
            finish = resolve;
            component = factory(tui, theme, {}, resolve);
        }),
    } };
    const pending = tool.execute('test', params, undefined, undefined, ctx);
    return {
        async open() { await flush(); assert.ok(component, 'the registered tool opened its custom UI'); return component; },
        async close() { finish(null); await pending; },
    };
}

function heading(rendered: string[], kind: 'single' | 'questionnaire') {
    const lines = rendered.map(unframe);
    const start = kind === 'single' ? 1 : 3;
    const optionIndex = lines.findIndex((line) => plain(line).startsWith('> A'));
    assert.ok(optionIndex > start, 'option appears after question heading');
    return lines.slice(start, optionIndex - 1); // Exclude the normal gap before options.
}

for (const kind of ['single', 'questionnaire'] as const) {
    const params = (question: string) => kind === 'single'
        ? { question, options: ['A', 'B'] }
        : { questions: [{ label: '主题', question, options: ['A', 'B'] }] };

    test(`${kind}: long CJK, English, emoji and explicit newlines remain visible on resize`, async () => {
        const question = '请选择详细选项：中文内容很长很长，包含 emoji 👩‍💻 与 🚀。\nSecond line has several English words that must remain visible in the panel.\n\n最后一段结束。';
        const h = harness(params(question));
        try {
            const view = await h.open();
            for (const width of [24, 48, 16, 24]) {
                const rendered = view.render(width);
                assert.strictEqual(view.render(width), rendered, 'same-width render remains cached');
                assert.ok(rendered.every((line: string) => visibleWidth(line) <= width), 'every visible line fits the panel width');
                assertFrame(rendered, width);
                const lines = heading(rendered, kind);
                assert.ok(lines.length > 4, 'heading occupies multiple physical rows');
                assert.equal(compact(lines).replace(kind === 'single' ? '？' : '主题', ''), question.replace(/\s/g, ''), 'no question text is truncated');
                assert.doesNotMatch(lines.join('\n'), /\x1b\[48;/, 'question text has no background fill');
                const first = lines.findIndex((line: string) => plain(line).includes('请选择'));
                const second = lines.findIndex((line: string) => plain(line).includes('Second'));
                const last = lines.findIndex((line: string) => plain(line).includes('最后'));
                assert.ok(first >= 0 && second > first && last > second, 'literal newlines separate the paragraphs');
                assert.ok(lines.slice(second + 1, last).some((line: string) => plain(line).trim() === ''), 'empty literal newline stays empty');
            }
        } finally { await h.close(); }
    });

    test(`${kind}: short heading retains exact foreground and inner padding`, async () => {
        const h = harness(params('Short?'));
        try {
            const view = await h.open();
            const width = 30;
            const line = heading(view.render(width), kind)[0];
            const text = kind === 'single'
                ? theme.fg('accent', ' ？') + theme.fg('text', ' Short?')
                : theme.fg('accent', ' 主题') + theme.fg('text', ' Short?');
            assert.equal(line, text + ' '.repeat(width - 2 - visibleWidth(text)));
            assert.equal(heading(view.render(width), kind).length, 1);
        } finally { await h.close(); }
    });
}

test('narrow panel widths stay bounded, including wide graphemes and resize', async () => {
    const h = harness({ question: '中文👩‍💻 question', options: ['A'] });
    try {
        const view = await h.open();
        for (const width of [0, 1, 2, 3, 4, 8, 30, 2, 30]) {
            const lines = view.render(width);
            assert.ok(lines.every((line: string) => visibleWidth(line) <= width));
            if (width >= 3) assertFrame(lines, width);
            if (!width) assert.deepEqual(lines, []);
        }
    } finally { await h.close(); }
});

test('custom input retains native cursor, focus and editing inside the frame', async () => {
    const h = harness({ question: 'Custom?', options: ['A'] });
    try {
        const view = await h.open();
        view.focused = true;
        view.handleInput('\x1b[B');
        view.handleInput('\r');
        view.handleInput('中文输入');
        for (const width of [30, 18, 30]) {
            const lines = view.render(width);
            assertFrame(lines, width);
            assert.match(lines.join('\n'), /中文输入/);
            assert.ok(lines.join('').includes(CURSOR_MARKER), 'native hardware cursor is retained');
        }
        view.focused = false;
        assert.ok(!view.render(30).join('').includes(CURSOR_MARKER), 'focus invalidates cached cursor');
        view.focused = true;
        assert.ok(view.render(30).join('').includes(CURSOR_MARKER));
        for (const width of [1, 2, 3, 4, 5, 30]) {
            const lines = view.render(width);
            assert.ok(lines.every((line: string) => visibleWidth(line) <= width));
            assert.ok(lines.join('').includes(CURSOR_MARKER), 'tiny viewport keeps the native cursor visible');
        }
        view.handleInput('\x1b');
        assert.ok(!view.render(30).join('').includes(CURSOR_MARKER), 'escape leaves custom input');
    } finally { await h.close(); }
});

test('questionnaire review keeps summary and help inside the same frame', async () => {
    const h = harness({ questions: [{ question: 'Choose?', options: ['A'] }] });
    try {
        const view = await h.open();
        view.handleInput('\r');
        const lines = view.render(40);
        assertFrame(lines, 40);
        assert.match(lines.map(plain).join('\n'), /提交前确认/);
        assert.match(lines.map(plain).join('\n'), /Enter 提交/);
    } finally { await h.close(); }
});

test('questionnaire navigation wraps the currently selected question, not the preceding tab', async () => {
    const first = 'First?';
    const second = '第二题的正文很长很长，带有 👩‍💻 emoji and English words to wrap.';
    const h = harness({ questions: [{ question: first, options: ['A'] }, { question: second, options: ['A'] }] });
    try {
        const view = await h.open();
        assert.ok(compact(heading(view.render(23), 'questionnaire')).includes(first));
        view.handleInput('\x1b[C');
        const lines = heading(view.render(23), 'questionnaire');
        assert.ok(lines.length > 1);
        assert.ok(compact(lines).includes(second.replace(/\s/g, '')));
    } finally { await h.close(); }
});
