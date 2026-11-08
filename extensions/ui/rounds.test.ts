import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent, type SessionEntry } from '@earendil-works/pi-coding-agent';
import { Container, Spacer, Text, stripTerminalSequences, visibleWidth, type Component, type TuiMouseEvent } from '@earendil-works/pi-tui';
import { patchToolDisplay, patchMergeConsecutiveTools } from './patches.ts';
import { RoundFrame, layoutRounds, recordRoundSummary, restoreRoundSummaries, ROUND_SUMMARY_ENTRY } from './rounds.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
initTheme('dark', false);
patchToolDisplay();
patchMergeConsecutiveTools();

let nextId = 1;
const label = 'Turn 42 · ↑1.4M ↓61k · 71.7 TPS · 14m5s';
const title = 'provider-a · model-a';
const message = (text: string, extra: object = {}) => ({ role: 'assistant', timestamp: nextId++, stopReason: 'stop',
    provider: 'provider-a', model: 'model-a', content: [{ type: 'text', text }], ...extra }) as any;
const plain = (container: Container, width = 100) => container.render(width).map(stripTerminalSequences);
const headerRow = (container: Container) => plain(container).findIndex(line => /^╭─ /.test(line));
const click = (container: Container, row: number, extra: Partial<TuiMouseEvent> = {}) => container.handleMouse({
    type: 'click', button: 'left', x: 1, y: row, screenX: 1, screenY: row, width: 100,
    height: container.render(100).length, shift: false, alt: false, ctrl: false, ...extra,
});
function tool(): ToolExecutionComponent {
    const component = new ToolExecutionComponent('read', 'read-test', { path: 'file.ts' }, {},
        { name: 'read', label: 'read', renderCall: () => new Text('read file.ts', 0, 0),
            renderResult: (_result: unknown, options: { expanded: boolean }) => new Text(options.expanded ? 'FULL DETAIL' : 'compact detail', 0, 0),
        } as any, { requestRender() {} } as any, process.cwd());
    component.updateResult({ content: [{ type: 'text', text: 'tool output' }], isError: false });
    return component;
}
function save(final: any, text = label) {
    const entry: SessionEntry = { type: 'message', id: `final-${nextId++}`, parentId: null,
        timestamp: new Date(0).toISOString(), message: final };
    const data = recordRoundSummary([entry], final, text);
    assert.ok(data);
    const metadata: SessionEntry = { type: 'custom', id: `stats-${nextId++}`, parentId: entry.id,
        timestamp: entry.timestamp, customType: ROUND_SUMMARY_ENTRY, data };
    return [entry, metadata];
}

test('completion keeps the entire process and conclusion in one non-collapsible frame', (t) => {
    restoreRoundSummaries([]);
    t.after(() => restoreRoundSummaries([]));
    const progress = message('progress explanation', { stopReason: 'toolUse', content: [
        { type: 'thinking', thinking: 'hidden private thought' }, { type: 'text', text: 'progress explanation' },
        { type: 'toolCall', id: 'read-1', name: 'read', arguments: { path: 'file.ts' } },
    ] });
    const final = message('FINAL CONCLUSION');
    const snapshot = JSON.stringify([progress, final]);
    const container = new Container();
    const work = tool();
    const customNotice: Text & Component = new Text('custom progress notice', 0, 0);
    let localY: number | undefined;
    customNotice.handleMouse = event => { localY = event.y; return { handled: true }; };
    for (const child of [new UserMessageComponent('original question'), customNotice,
        new AssistantMessageComponent(progress), work, new AssistantMessageComponent(final)]) container.addChild(child);
    const originalChildren = [...container.children];
    const frame = layoutRounds(container.children).find(child => child instanceof RoundFrame);
    let lines = plain(container);
    const header = headerRow(container);
    assert.match(lines[header], /provider-a · model-a/);
    assert.ok(lines.findIndex(line => line.includes('original question')) < header);
    assert.match(lines.join('\n'), /custom progress notice[\s\S]*progress explanation[\s\S]*1 call[\s\S]*FINAL CONCLUSION/);
    assert.doesNotMatch(lines.join('\n'), /hidden private thought|Turn 42|FULL DETAIL/);
    assert.ok(lines.slice(header + 1, -1).every(line => line.startsWith('│') && line.endsWith('│')));
    click(container, lines.findIndex(line => line.includes('custom progress notice')));
    assert.equal(localY, 0, 'the in-frame notice receives its own local coordinates');
    click(container, lines.findIndex(line => line.includes('1 call')));
    lines = plain(container);
    click(container, lines.findIndex(line => /› ✓ read/.test(line)));
    const expanded = plain(container);
    assert.match(expanded.join('\n'), /compact detail/);
    save(final);
    assert.deepEqual(plain(container), expanded, 'settlement neither hides the process nor resets opened tool previews');
    assert.equal(layoutRounds(container.children).find(child => child instanceof RoundFrame), frame);
    assert.equal(click(container, header), undefined, 'the model title is not a folding control');
    assert.deepEqual(plain(container), expanded);
    work.setExpanded(true);
    assert.match(plain(container).join('\n'), /progress explanation[\s\S]*FULL DETAIL[\s\S]*FINAL CONCLUSION/);
    work.setExpanded(false);
    lines = plain(container);
    assert.match(lines.join('\n'), /progress explanation[\s\S]*FINAL CONCLUSION/);
    assert.doesNotMatch(lines.join('\n'), /FULL DETAIL|compact detail/);
    assert.deepEqual(container.children, originalChildren);
    assert.equal(JSON.stringify([progress, final]), snapshot, 'projection leaves messages and thinking unchanged');
});

test('history titles use the actual message provider and model, never saved statistics', (t) => {
    restoreRoundSummaries([]);
    t.after(() => restoreRoundSummaries([]));
    const first = message('first final', { provider: 'vendor-one', model: 'first-model' });
    const second = message('second final', { provider: 'vendor-two', model: 'second-model' });
    const saved = [...save(first), ...save(second, 'Turn 2 · ↑4k ↓500 · 20.0 TPS · 30s')];
    const container = new Container();
    for (const [question, final] of [['first question', first], ['second question', second]]) {
        container.addChild(new UserMessageComponent(question));
        container.addChild(new AssistantMessageComponent(message(`${question} progress`, { stopReason: 'toolUse' })));
        container.addChild(new AssistantMessageComponent(JSON.parse(JSON.stringify(final))));
    }
    const lines = plain(container);
    assert.equal(lines.filter(line => /^╭─ /.test(line)).length, 2);
    assert.match(lines.join('\n'), /vendor-one · first-model[\s\S]*first question progress[\s\S]*first final/);
    assert.match(lines.join('\n'), /vendor-two · second-model[\s\S]*second question progress[\s\S]*second final/);
    assert.doesNotMatch(lines.join('\n'), /Turn 42|Turn 2|TPS|14m5s/);
    t.mock.method(Date, 'now', () => 99_999_999);
    assert.deepEqual(plain(container), lines);
    restoreRoundSummaries(saved.slice(0, 2));
    assert.deepEqual(plain(container), lines, 'branch-specific metadata does not change titles or hide process text');
    restoreRoundSummaries([]);
    assert.deepEqual(plain(container), lines, 'old history without metadata still receives model titles');
});

test('saved boundaries split extension follow-ups after reload without hiding their process', async (t) => {
    restoreRoundSummaries([]);
    t.after(() => restoreRoundSummaries([]));
    const first = message('first final');
    const second = message('follow-up final', { provider: 'vendor-two', model: 'second-model' });
    const saved = [...save(first, 'Turn 1 · 1s'), ...save(second, 'Turn 2 · 2s')];
    const container = new Container();
    container.addChild(new UserMessageComponent('question'));
    container.addChild(new AssistantMessageComponent(first));
    container.addChild(new AssistantMessageComponent(message('follow-up process', { stopReason: 'toolUse' })));
    container.addChild(new AssistantMessageComponent(second));
    const lines = plain(container);
    assert.equal(lines.filter(line => /^╭─ /.test(line)).length, 2);
    assert.match(lines.join('\n'), /first final[\s\S]*vendor-two · second-model[\s\S]*follow-up process[\s\S]*follow-up final/);
    assert.doesNotMatch(lines.join('\n'), /Turn [12]/);
    restoreRoundSummaries([]);
    assert.equal(plain(container).filter(line => /^╭─ /.test(line)).length, 1);
    const reloadPath = './rounds.ts?reload';
    const reloaded = await import(reloadPath);
    reloaded.restoreRoundSummaries(saved);
    assert.deepEqual(plain(container), lines, 'installed prototype patches share restored round boundaries after reload');
});

test('whole-reply frames preserve Markdown and fit narrow widths across themes', (t) => {
    restoreRoundSummaries([]);
    t.after(() => { restoreRoundSummaries([]); initTheme('dark', false); });
    const final = message('# 结论\n\n- **重点**：过程保留。\n- [文档](https://example.com/docs)\n\n```ts\nconst ok = true;\n```');
    save(final);
    const container = new Container();
    container.addChild(new AssistantMessageComponent(final));
    const rendered = container.render(100);
    assert.ok(rendered[1].startsWith('\x1b]133;A\x07'), 'native semantic zone starts before the visible frame side');
    assert.ok(rendered.at(-2)!.startsWith('\x1b]133;B\x07\x1b]133;C\x07'), 'native zone end stays at the start of the last content line');
    assert.deepEqual(rendered.join('').match(/\x1b\]133;[ABC]\x07/g), ['\x1b]133;A\x07', '\x1b]133;B\x07', '\x1b]133;C\x07']);
    const wide = rendered.map(stripTerminalSequences).join('\n');
    assert.match(wide, /结论/);
    assert.match(wide, /重点/);
    assert.match(wide, /const ok = true;/);
    assert.doesNotMatch(wide, /\*\*重点\*\*|Turn 42|TPS/);
    assert.equal((wide.match(/provider-a · model-a/g) ?? []).length, 1);
    for (const name of ['dark', 'light']) {
        initTheme(name, false);
        container.invalidate();
        for (const width of [0, 1, 2, 3, 7, 8, 12, 40, 80, 120]) {
            const lines = plain(container, width);
            const frameWidth = visibleWidth(lines[0] ?? '');
            assert.ok(frameWidth <= width && lines.every(line => visibleWidth(line) === frameWidth), `${name}/${width}: all frame lines fit`);
            if (width >= 8) {
                assert.match(lines[0], /^╭.*╮$/);
                assert.match(lines.at(-1)!, /^╰─+╯$/);
                assert.ok(lines.slice(1, -1).every(line => line.startsWith('│') && line.endsWith('│')));
            } else assert.ok(lines.every(line => !/[╭╰│]/.test(line)), 'very narrow terminals use the unframed fallback');
        }
    }
});

test('frame borders do not activate content and nested mouse targets receive local coordinates', () => {
    const process: Text & Component = new Text('clickable process', 0, 0);
    const conclusion: Text & Component = new Text('clickable conclusion', 0, 0);
    const processEvents: TuiMouseEvent[] = [];
    const conclusionEvents: TuiMouseEvent[] = [];
    process.handleMouse = event => { processEvents.push(event); return { handled: true, target: process }; };
    conclusion.handleMouse = event => { conclusionEvents.push(event); return { handled: true }; };
    const native = new Container();
    native.addChild(new Spacer(1));
    native.addChild(process);
    native.addChild(conclusion);
    const frame = new RoundFrame();
    frame.update(title, [native]);
    const container = new Container();
    container.addChild(new Text('outside prefix', 0, 0));
    container.addChild(frame);
    const lines = plain(container);
    assert.match(lines[1], /provider-a · model-a/);
    assert.match(lines[2], /clickable process/);
    assert.match(lines[3], /clickable conclusion/);
    for (const [x, y] of [[0, 2], [99, 2], [4, 1], [4, 4]]) assert.equal(click(container, y, { x }), undefined);
    assert.equal(processEvents.length + conclusionEvents.length, 0);
    assert.equal(click(container, 2, { x: 4, type: 'drag' })?.target, process);
    let event = processEvents.at(-1)!;
    assert.deepEqual([event.x, event.y, event.width, event.height, event.type], [3, 0, 98, 1, 'drag']);
    click(container, 3, { x: 4, type: 'drag' });
    event = conclusionEvents.at(-1)!;
    assert.deepEqual([event.x, event.y, event.width, event.height, event.type], [3, 0, 98, 1, 'drag']);
    assert.deepEqual(plain(container), lines, 'neither title nor content events fold the reply');
});

test('streaming text and tool state refresh inside one frame without modifying source records', (t) => {
    restoreRoundSummaries([]);
    t.after(() => restoreRoundSummaries([]));
    const source = new AssistantMessageComponent();
    const thinking = message('', { content: [{ type: 'thinking', thinking: 'hidden thought' }] });
    source.updateContent(thinking, true);
    const container = new Container();
    container.addChild(new UserMessageComponent('live question'));
    container.addChild(source);
    assert.equal(plain(container).filter(line => /^╭─ /.test(line)).length, 0, 'thinking alone does not create an empty frame');
    const streamed = message('first chunk', { stopReason: 'toolUse', content: [...thinking.content, { type: 'text', text: 'first chunk' }] });
    source.updateContent(streamed, true);
    const frame = layoutRounds(container.children).find(child => child instanceof RoundFrame);
    assert.match(plain(container).join('\n'), /provider-a · model-a[\s\S]*first chunk/);
    streamed.content[1].text = 'second chunk 中文';
    source.updateContent(streamed, true);
    const work = tool();
    work.updateResult({ content: [{ type: 'text', text: 'working' }], isError: false }, true);
    container.addChild(work);
    let lines = plain(container);
    assert.match(lines.join('\n'), /second chunk 中文[\s\S]*1 running/);
    work.updateResult({ content: [{ type: 'text', text: 'failed' }], isError: true });
    const final = message('final answer', { provider: 'actual-provider', model: 'actual-model' });
    container.addChild(new AssistantMessageComponent(final));
    const snapshot = JSON.stringify([thinking, streamed, final]);
    save(final);
    lines = plain(container);
    assert.equal(lines.filter(line => /^╭─ /.test(line)).length, 1);
    assert.match(lines.join('\n'), /actual-provider · actual-model[\s\S]*second chunk 中文[\s\S]*1 failed[\s\S]*final answer/);
    assert.doesNotMatch(lines.join('\n'), /hidden thought|Turn 42|TPS/);
    assert.equal(layoutRounds(container.children).find(child => child instanceof RoundFrame), frame);
    for (const width of [1, 3, 8, 20, 80]) assert.ok(frame!.render(width).every(line => visibleWidth(line) <= width));
    assert.equal(JSON.stringify([thinking, streamed, final]), snapshot);
});

test('image control sequences and their reserved blank rows survive whole-reply framing', () => {
    const kitty = '\x1b_Ga=T,f=100,C=1;AAAA\x1b\\';
    const iterm = '\x1b[1A\x1b]1337;File=inline=1:AAAA\x07\x1b[1B';
    for (const content of [[kitty, '', ''], ['', iterm]]) {
        let innerWidth = 0;
        const frame = new RoundFrame();
        frame.update(title, [{ render(width) { innerWidth = width; return content; }, invalidate() {} }]);
        const lines = frame.render(40);
        assert.equal(innerWidth, 38);
        assert.equal(lines.length, content.length + 2, 'image rows are not mistaken for native text spacers');
        for (let i = 0; i < content.length; i++) assert.ok(lines[i + 1].includes(content[i]));
        // Cursor-moving image protocol lines do not have a meaningful text-only width.
        assert.ok(lines.filter(line => !line.includes('1337;File=')).every(line => visibleWidth(line) === 40));
    }
});

test('short replies shrink leftward, while long replies wrap within the available width', () => {
    const source = new AssistantMessageComponent(message('好的。'));
    const container = new Container();
    container.addChild(source);
    let lines = plain(container, 100);
    const compactWidth = visibleWidth(lines[0]);
    assert.ok(compactWidth < 50);
    assert.ok(lines.every(line => visibleWidth(line) === compactWidth));
    assert.ok(lines[0].startsWith('╭'), 'assistant frames remain left aligned');
    source.updateContent(message('较长的助手回复，需要根据可用空间换行。'.repeat(25)));
    lines = plain(container, 40);
    assert.ok(lines.length > 5);
    assert.ok(lines.every(line => visibleWidth(line) <= 40));
    source.updateContent(message('短回复'));
    lines = plain(container, 100);
    assert.equal(visibleWidth(lines[0]), compactWidth, 'a shorter update does not retain a stale wide frame');
});

test('trailing assistant blank rows disappear without losing paragraph spacing or semantic markers', () => {
    const original = message('', { content: [
        { type: 'text', text: 'first paragraph\n\nlast paragraph' }, { type: 'text', text: '\n\n' },
    ] });
    const snapshot = JSON.stringify(original);
    const container = new Container();
    container.addChild(new AssistantMessageComponent(original));
    container.addChild(new Spacer(2));
    const rendered = container.render(100);
    const lines = rendered.map(stripTerminalSequences);
    assert.match(lines.at(-2)!, /last paragraph/);
    assert.ok(lines.slice(1, -2).some(line => /^│ +│$/.test(line)), 'normal paragraph spacing remains');
    assert.deepEqual(rendered.join('').match(/\x1b\]133;[ABC]\x07/g), ['\x1b]133;A\x07', '\x1b]133;B\x07', '\x1b]133;C\x07']);
    assert.equal(JSON.stringify(original), snapshot);
});

test('aborted, incomplete or unidentifiable conclusions never create persisted round boundaries', () => {
    restoreRoundSummaries([]);
    const valid = message('answer');
    assert.equal(recordRoundSummary([], valid, label), undefined);
    for (const candidate of [message('answer', { stopReason: 'aborted' }), message('', {}), message('answer', { content: [
        { type: 'text', text: 'answer' }, { type: 'toolCall', name: 'read', id: 'x', arguments: {} },
    ] })]) assert.equal(recordRoundSummary([{ type: 'message', id: 'x', parentId: null,
        timestamp: new Date(0).toISOString(), message: candidate }], candidate, label), undefined);
    restoreRoundSummaries([{ type: 'custom', id: 'metadata', parentId: null, timestamp: new Date(0).toISOString(),
        customType: ROUND_SUMMARY_ENTRY, data: { version: 1, messageId: 'missing', label } }]);
    const container = new Container();
    container.addChild(new AssistantMessageComponent(valid));
    assert.match(plain(container).join('\n'), /provider-a · model-a[\s\S]*answer/);
    assert.doesNotMatch(plain(container).join('\n'), /Turn 42/);
});
