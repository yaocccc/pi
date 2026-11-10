import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { CURSOR_MARKER, stripTerminalSequences, visibleWidth, type TuiMouseEvent } from '@earendil-works/pi-tui';
import { TextAreaEditor } from './editor.ts';
import { patchUserMessageFrame } from './patches.ts';
import { frameBorder, type FrameEdge, type FrameKind } from './utils.ts';
import { RoundFrame } from './rounds.ts';
import { applyWorkingMessage, clearWorkingMessage, setWorkingMessageActive } from './working-message.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme, getEditorTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
const { KeybindingsManager } = await import(pathToFileURL(join(root, 'core/keybindings.js')).href);
initTheme('dark', false);
patchUserMessageFrame();
const mouse = (x: number, y: number, width: number, height: number, type: TuiMouseEvent['type'] = 'click'): TuiMouseEvent => ({
    type, button: 'left', x, y, screenX: x, screenY: y, width, height, shift: false, alt: false, ctrl: false,
});
const editor = () => {
    clearWorkingMessage();
    const value = new TextAreaEditor({ terminal: { rows: 20, columns: 80 }, requestRender() {} } as any,
        getEditorTheme(), new KeybindingsManager());
    value.focused = true;
    return value;
};

test('each frame kind selects its own eight-edge truecolor palette in both themes', () => {
    const edges: FrameEdge[] = ['topLeft', 'top', 'topRight', 'right', 'bottomRight', 'bottom', 'bottomLeft', 'left'];
    const colors: Record<FrameKind, string[]> = {
        user: ['104;211;174', '110;216;180', '121;220;188', '135;222;195', '123;212;184', '108;205;175', '95;198;165', '98;204;170'],
        assistant: ['120;181;239', '128;191;245', '138;199;246', '145;200;239', '133;187;232', '119;175;224', '110;166;220', '113;175;230'],
        tool: Array(8).fill('113;137;159'),
        input: ['182;160;232', '188;168;238', '196;176;242', '200;181;239', '189;169;231', '177;157;221', '168;146;216', '173;151;223'],
    };
    edges.forEach((edge, index) => {
        assert.equal(new Set(Object.values(colors).map(palette => palette[index])).size, 4);
        for (const kind of Object.keys(colors) as FrameKind[]) {
            assert.equal(frameBorder('─', edge, kind), `\x1b[38;2;${colors[kind][index]}m─\x1b[39m`);
        }
        assert.equal(frameBorder('─', edge), frameBorder('─', edge, 'assistant'));
    });
    for (const name of ['light', 'dark']) {
        initTheme(name, false);
        const assistant = new RoundFrame();
        assistant.update('provider · model', [{ render: () => ['主题'], invalidate() {} }]);
        const output: Record<Exclude<FrameKind, 'tool'>, string> = {
            user: new UserMessageComponent('主题').render(40).join('\n'),
            assistant: assistant.render(40).join('\n'),
            input: editor().render(40).join('\n'),
        };
        for (const kind of Object.keys(output) as Array<keyof typeof output>) {
            for (const [edge, glyph] of [['topLeft', '╭'], ['top', '─'], ['topRight', '╮'], ['right', '│'],
                ['bottomRight', '╯'], ['bottom', '─'], ['bottomLeft', '╰'], ['left', '│']] as const) {
                assert.ok(output[kind].includes(frameBorder(glyph, edge, kind).slice(0, -'\x1b[39m'.length)), `${name}/${kind}/${edge}`);
            }
        }
    }
});

test('user bubbles align right, shrink to content, wrap long prompts and preserve semantic markers', () => {
    const user = new UserMessageComponent('你好');
    const raw = user.render(80);
    const lines = raw.map(stripTerminalSequences);
    assert.ok(lines[0].indexOf('╭') > 60);
    assert.match(lines[0], /╭─ 你 ─╮$/);
    assert.match(lines[1], /│ 你好 │$/);
    assert.ok(lines.every(line => visibleWidth(line) === 80));
    assert.ok(raw[0].startsWith('\x1b]133;A\x07'));
    assert.ok(raw.at(-1)!.startsWith('\x1b]133;B\x07\x1b]133;C\x07'));
    assert.ok(raw[0].includes(frameBorder('╭', 'topLeft', 'user')) && raw[0].endsWith(frameBorder('╮', 'topRight', 'user')));
    assert.ok(raw[1].includes(frameBorder('│', 'left', 'user')) && raw[1].endsWith(frameBorder('│', 'right', 'user')));
    assert.doesNotMatch(raw.join(''), /\x1b\[(?:2m|48;)|[▄▀]/);
    assert.equal((user as any).text, '你好');
    const long = new UserMessageComponent('很长的用户消息 abc '.repeat(30));
    const wrapped = long.render(80).map(stripTerminalSequences);
    assert.equal(wrapped[0].indexOf('╭'), 16);
    assert.ok(wrapped.length > 3);
    for (const width of [0, 1, 2, 3, 7, 8, 12, 40, 80]) {
        assert.ok(long.render(width).every(line => visibleWidth(line) === width), `width ${width}`);
    }
});

test('user frame caches are isolated and follow native content, width, padding and style changes', (t) => {
    patchUserMessageFrame();
    t.after(() => initTheme('dark', false));
    const user = new UserMessageComponent('unchanged message');
    const expected = user.render(80).slice();
    const damaged = user.render(80);
    damaged.shift();
    damaged[0] = 'corrupted by a render consumer';
    assert.deepEqual(user.render(80), expected);
    // Narrow/empty renders must not leave cached hit-test geometry for another width.
    for (const width of [3, 80, 0, 80, 120, 80]) {
        const actual = user.render(width);
        const fresh = new UserMessageComponent('unchanged message').render(width);
        assert.deepEqual(actual, fresh);
    }
    const body = user.children[0];
    let calls = 0;
    let text = 'changed content';
    const normalRender = body.render.bind(body);
    body.render = width => { calls++; return normalRender(width).map(line => line.replace('unchanged message', text)); };
    assert.match(user.render(80).map(stripTerminalSequences).join('\n'), /changed content/);
    const current = user.render(80);
    assert.ok(calls >= 2, 'cache hits still call native child rendering');
    text = 'updated again';
    user.invalidate();
    assert.match(user.render(80).map(stripTerminalSequences).join('\n'), /updated again/);
    assert.notDeepEqual(user.render(80), current);
    body.render = normalRender;
    (user as any).outputPad = 3;
    const fresh = new UserMessageComponent('unchanged message');
    (fresh as any).outputPad = 3;
    assert.deepEqual(user.render(80), fresh.render(80));
    initTheme('light', false);
    user.invalidate();
    fresh.invalidate();
    assert.deepEqual(user.render(80), fresh.render(80));
});

test('right-aligned user content receives native mouse coordinates and repeated registration is stable', async () => {
    const user = new UserMessageComponent('clickable user message');
    const events: TuiMouseEvent[] = [];
    Object.assign(user.children[0], { handleMouse(event: TuiMouseEvent) { events.push(event); return { handled: true }; } });
    const lines = user.render(80).map(stripTerminalSequences);
    user.render(3);
    user.render(80);
    user.render(80); // Cached frames retain the geometry after returning from a narrow layout.
    const left = lines[0].indexOf('╭');
    assert.equal(user.handleMouse(mouse(left - 1, 1, 80, lines.length)), undefined);
    assert.equal(user.handleMouse(mouse(left, 1, 80, lines.length)), undefined);
    assert.equal(user.handleMouse(mouse(left + 4, 0, 80, lines.length)), undefined);
    const result = user.handleMouse(mouse(left + 4, 1, 80, lines.length));
    assert.equal(result?.handled, true);
    assert.equal((result as any).target.originX, left + 1, 'captured mouse targets retain the visible origin');
    assert.deepEqual([events[0].x, events[0].y, events[0].width], [3, 1, 62]);
    const render = UserMessageComponent.prototype.render;
    const handleMouse = UserMessageComponent.prototype.handleMouse;
    const reloadPath = './patches.ts?user-frame-reload';
    const reloaded = await import(reloadPath);
    reloaded.patchUserMessageFrame();
    assert.equal(UserMessageComponent.prototype.render, render);
    assert.equal(UserMessageComponent.prototype.handleMouse, handleMouse);
});

test('input frames retain the cursor, exact widths, scrolling labels and segment colors', () => {
    const input = editor();
    input.borderColor = text => `\x1b[2m${text}\x1b[22m`;
    const previous = input.borderColor;
    input.setText('你好 abc');
    for (const width of [0, 1, 2, 7, 8, 12, 40, 80]) {
        const raw = input.render(width);
        const lines = raw.map(stripTerminalSequences);
        assert.ok(lines.every(line => visibleWidth(line) === width), `width ${width}`);
        if (width >= 8) {
            assert.ok(raw[0].startsWith(frameBorder('╭', 'topLeft', 'input')));
            assert.ok(raw[0].endsWith(frameBorder('╮', 'topRight', 'input')));
            assert.ok(raw.at(-1)!.startsWith(frameBorder('╰', 'bottomLeft', 'input')));
            assert.ok(raw.at(-1)!.endsWith(frameBorder('╯', 'bottomRight', 'input')));
            assert.equal(raw.filter(line => line.includes(CURSOR_MARKER)).length, 1);
            assert.ok(lines.slice(1, -1).every(line => line.startsWith('│') && line.endsWith('│')));
        }
        assert.doesNotMatch(raw.join(''), /\x1b\[(?:2m|48;)|[▄▀]/);
    }
    assert.equal(input.borderColor, previous, 'native state color is not overwritten outside rendering');
    input.setText(Array.from({ length: 30 }, (_, i) => `line ${i}`).join('\n'));
    assert.match(stripTerminalSequences(input.render(40)[0]), /↑/);
    for (let i = 0; i < 29; i++) input.handleInput('\x1b[A');
    assert.equal(input.getCursor().line, 0);
    assert.match(stripTerminalSequences(input.render(40).at(-1)!), /↓/);
    assert.equal(input.getText().split('\n').length, 30);
});

test('input mouse mapping accounts for frame sides and the working line; paste and submit remain native', (t) => {
    const input = editor();
    t.after(clearWorkingMessage);
    input.setText('甲乙abc');
    let lines = input.render(40);
    input.handleMouse(mouse(4, 1, 40, lines.length));
    assert.deepEqual(input.getCursor(), { line: 0, col: 1 });
    const before = input.getCursor();
    input.handleMouse(mouse(0, 1, 40, lines.length));
    assert.deepEqual(input.getCursor(), before, 'border clicks do not move the caret');
    assert.equal(input.handleMouse(mouse(4, 1, 40, lines.length, 'drag')), undefined);
    setWorkingMessageActive(true);
    applyWorkingMessage({ ui: { setWorkingVisible() {}, theme: { fg: (_: string, text: string) => text } } } as any, Date.now());
    lines = input.render(40);
    assert.match(stripTerminalSequences(lines[0]), /Turn 1/);
    assert.equal(input.handleMouse(mouse(4, 0, 40, lines.length)), undefined);
    input.handleMouse(mouse(2, 2, 40, lines.length));
    assert.deepEqual(input.getCursor(), { line: 0, col: 0 });
    input.setText('');
    input.handleInput('\x1b[200~first\nsecond\x1b[201~');
    assert.equal(input.getExpandedText(), 'first\nsecond');
    let submitted: string | undefined;
    input.onSubmit = text => { submitted = text; };
    input.handleInput('\r');
    assert.equal(submitted, 'first\nsecond');
});

test('autocomplete remains outside the input frame with working pointer and keyboard selection', async () => {
    const input = editor();
    input.setAutocompleteProvider({
        async getSuggestions() { return { prefix: '/', items: [
            { value: '/alpha', label: 'alpha' }, { value: '/beta', label: 'beta' },
        ] }; },
        applyCompletion(_lines, _line, _col, item) { return { lines: [item.value], cursorLine: 0, cursorCol: item.value.length }; },
    });
    input.handleInput('/');
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(input.isShowingAutocomplete(), true);
    let lines = input.render(40).map(stripTerminalSequences);
    const bottom = lines.findIndex(line => line.startsWith('╰'));
    assert.ok(lines.length > bottom + 1);
    assert.ok(lines.slice(bottom + 1).every(line => !line.startsWith('│') && !line.endsWith('│')));
    input.handleMouse(mouse(4, bottom + 1, 40, lines.length));
    assert.equal(input.getText(), '/alpha');
    input.setText('');
    input.handleInput('/');
    await new Promise(resolve => setImmediate(resolve));
    lines = input.render(40).map(stripTerminalSequences);
    input.handleInput('\x1b[B');
    input.handleInput('\t');
    assert.equal(input.getText(), '/beta');
});
