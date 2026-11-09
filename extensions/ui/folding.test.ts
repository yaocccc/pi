import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { createEditToolDefinition, AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Container, Text, stripTerminalSequences, visibleWidth, type TuiMouseEvent } from '@earendil-works/pi-tui';
import { patchToolDisplay, patchFinalResponseSeparator, patchMergeConsecutiveTools, patchUserMessageFrame } from './patches.ts';
import { patchToolContext } from './tool-context.ts';
import { frameBorder } from './utils.ts';
import { ToolActivityGroup } from './folding.ts';
import { WorkerRenderRefresh } from '../worker/presentation.ts';
import { emptyWorkerUsage } from '../worker/ui.ts';
import type { WorkerUiDetails, WorkerUiTask } from '../worker/types.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
initTheme('dark', false);
const nativeUpdateContent = AssistantMessageComponent.prototype.updateContent;
const thinkingMessage = { role: 'assistant', content: [
    { type: 'thinking', thinking: `${'long thinking '.repeat(20)}\nsecond line` },
    { type: 'text', text: 'answer after thinking' },
] } as any;
const nativeThinking = [false, true].map((hidden) => new AssistantMessageComponent(thinkingMessage, hidden).render(80));
patchToolDisplay();
patchFinalResponseSeparator();
patchMergeConsecutiveTools();
patchUserMessageFrame();
patchToolContext();

let renders = 0;
let id = 0;
const tool = (name = 'read', options: { pending?: boolean; error?: boolean; self?: boolean; hidden?: boolean } = {}) => {
    const component = new ToolExecutionComponent(name, `call-${id++}`, { path: `${name}.ts` }, {}, {
        ...(options.self ? { renderShell: 'self' } : {}),
        renderCall: () => new Text(options.hidden ? '' : `${name} call`, 0, 0),
        renderResult: (_result: unknown, { expanded }: { expanded: boolean }) =>
            new Text(options.hidden ? '' : `${name} ${expanded ? 'FULL OUTPUT' : 'preview'}`, 0, 0),
    } as any, { requestRender() { renders++; } } as any, process.cwd());
    if (!options.pending) component.updateResult({ content: [{ type: 'text', text: 'output' }], isError: options.error ?? false });
    return component;
};
const assistant = (type: 'text' | 'thinking', value: string) => new AssistantMessageComponent({
    role: 'assistant', content: [{ type, [type]: value }],
} as any);
const plain = (container: Container, width = 80) => container.render(width).map(stripTerminalSequences);
const click = (container: Container, row: number, width = 80, extra: Partial<TuiMouseEvent> = {}) => container.handleMouse({
    type: 'click', button: 'left', x: 1, y: row, screenX: 1, screenY: row,
    width, height: container.render(width).length, shift: false, alt: false, ctrl: false, ...extra,
});

test('stable group rows reuse only the latest formatting snapshot and still render children', (t) => {
    const component = tool();
    component.setExpanded(true);
    const render = t.mock.method(component, 'render');
    const group = new ToolActivityGroup();
    group.open = true;
    group.update([component], 0);
    const expected = group.render(120);
    const summary = (group as any).summaryBadge.current;
    const previewCache = (group as any).previewBadges.get(component);
    const preview = previewCache.current;
    for (let i = 0; i < 5; i++) {
        assert.deepEqual(group.render(120), expected);
        assert.equal((group as any).summaryBadge.current, summary);
        assert.equal(previewCache.current, preview);
    }
    assert.equal(render.mock.callCount(), 6, 'cached row formatting never skips native rendering or hit layout');
    for (let i = 0; i < 20; i++) {
        (component as any).args.path = `stream-${i}.ts`;
        assert.match(group.render(120).map(stripTerminalSequences).join('\n'), new RegExp(`stream-${i}\\.ts`));
        assert.equal((group as any).previewBadges.get(component), previewCache);
        assert.notEqual(previewCache.current, preview);
        assert.match(previewCache.current.text, new RegExp(`stream-${i}\\.ts`));
    }
    group.invalidate();
    assert.equal((group as any).summaryBadge.current, undefined);
    assert.equal((group as any).previewBadges.get(component), undefined);
});

test('cached badge rows match fresh formatting after content, width, status and folding changes', () => {
    const component = tool();
    const group = new ToolActivityGroup();
    group.open = true;
    let children = [component];
    let notices = 0;
    const compare = () => {
        group.update(children, notices);
        for (const width of [120, 120, 80, 79, 25, 24, 8, 1, 0, 120]) {
            const fresh = new ToolActivityGroup();
            fresh.open = group.open;
            fresh.update(children, notices);
            assert.deepEqual(group.render(width), fresh.render(width));
            assert.deepEqual(group.render(width), fresh.render(width));
        }
    };
    compare();
    (component as any).args.path = 'changed 中文.ts';
    component.updateResult({ content: [{ type: 'text', text: 'new result '.repeat(800) }], isError: true });
    compare();
    notices = 2;
    children = [component, tool('bash', { pending: true })];
    compare();
    group.open = false;
    compare();
    component.setExpanded(true);
    compare();
    children = [component];
    compare();
});

test('cached rows notice badge-only ANSI changes on the same theme object', () => {
    const theme = (globalThis as any)[Symbol.for('@earendil-works/pi-coding-agent:theme')];
    const original = theme.fg;
    let badgeColor = 31;
    theme.fg = function (color: string, text: string) {
        return text.startsWith('≈') ? `\x1b[${badgeColor}m${text}\x1b[39m` : original.call(this, color, text);
    };
    try {
        const component = tool();
        const group = new ToolActivityGroup();
        group.open = true;
        group.update([component], 0);
        const before = group.render(120);
        const summary = (group as any).summaryBadge.current;
        const preview = (group as any).previewBadges.get(component).current;
        badgeColor = 32;
        const after = group.render(120);
        assert.deepEqual(after.map(stripTerminalSequences), before.map(stripTerminalSequences));
        assert.notDeepEqual(after, before);
        assert.equal((group as any).summaryBadge.current.text, summary.text);
        assert.equal((group as any).previewBadges.get(component).current.text, preview.text);
        assert.notEqual((group as any).summaryBadge.current, summary);
        assert.notEqual((group as any).previewBadges.get(component).current, preview);
        const fresh = new ToolActivityGroup();
        fresh.open = true;
        fresh.update([component], 0);
        assert.deepEqual(after, fresh.render(120));
    } finally { theme.fg = original; }
});

test('native edit folding never renders a hidden diff, including pending, failed and narrow states', (t) => {
    const native = createEditToolDefinition(process.cwd());
    const edit = new ToolExecutionComponent('edit', 'folded-native-edit', { path: 'example.ts' }, {}, native,
        { requestRender() {} } as any, process.cwd());
    const render = t.mock.method(edit, 'render');
    const container = new Container();
    container.addChild(edit);
    for (const result of [undefined, { content: [{ type: 'text', text: 'changed' }], details: { diff: '-old\n+new' }, isError: false },
        { content: [{ type: 'text', text: 'failed' }], isError: true }]) {
        if (result) edit.updateResult(result as any);
        for (const width of [120, 40, 1]) container.render(width);
        assert.match(plain(container).join('\n'), /1 call/);
    }
    assert.equal(render.mock.callCount(), 0, 'visibility is established by native renderer identity, not rendering');
    edit.setExpanded(true);
    assert.ok(plain(container).some(line => line.includes('failed')));
    assert.equal(render.mock.callCount(), 1, 'expanded native edit renders exactly once, without a visibility probe');
    edit.setExpanded(false);
    container.render(80);
    assert.equal(render.mock.callCount(), 1);
});

test('overriding only native edit renderCall still probes dynamic self visibility on every frame', () => {
    let visible = false;
    let probes = 0;
    const body = { render: () => { probes++; return visible ? ['custom edit'] : []; }, invalidate() {} };
    const definition = { ...createEditToolDefinition(process.cwd()), renderCall: () => body };
    const edit = new ToolExecutionComponent('edit', 'dynamic-edit', {}, {}, definition as any,
        { requestRender() {} } as any, process.cwd());
    const container = new Container();
    container.addChild(edit);
    assert.equal(plain(container).length, 0);
    visible = true; // no updateDisplay or result identity change
    assert.match(plain(container).join('\n'), /1 call/);
    visible = false;
    assert.equal(plain(container).length, 0);
    assert.equal(probes, 3);
});

test('summary → call rows → compact → full → collapsed, using the actual folded mouse layout', () => {
    const first = tool();
    const second = tool('bash');
    const container = new Container();
    container.addChild(new UserMessageComponent('question'));
    container.addChild(assistant('text', ''));
    container.addChild(first);
    container.addChild(new AssistantMessageComponent({ role: 'assistant', content: [
        { type: 'toolCall', id: 'carrier', name: 'bash', arguments: {} },
    ] } as any));
    container.addChild(second);
    container.addChild(assistant('text', 'final answer'));
    const originalChildren = [...container.children];
    let lines = plain(container);
    assert.equal(lines.filter((line) => line.includes('calls')).length, 1);
    const summary = lines.findIndex((line) => line.includes('2 calls'));
    assert.match(lines[summary]!, /› 2 calls · read×1  bash×1/);
    assert.ok(container.render(80)[summary]!.startsWith(frameBorder('│', 'left') + ' › 2 calls'), 'summary uses plain text inside the colored frame');
    const theme = (globalThis as any)[Symbol.for('@earendil-works/pi-coding-agent:theme')];
    assert.ok(container.render(80)[summary]!.includes(theme.fg('dim', 'read×1  bash×1')), 'breakdown uses dim');
    assert.ok(lines.some((line) => line.includes('final answer')));
    assert.ok(!lines.some((line) => /read.ts|FULL OUTPUT/.test(line)));
    assert.equal(click(container, summary, 80, { button: 'right' }), undefined);
    assert.equal(click(container, summary)?.handled, true);
    lines = plain(container);
    assert.match(lines[summary]!, /⌄ 2 calls/);
    const row = lines.findIndex((line) => line.includes('› ✓ read  read.ts'));
    assert.ok(row > summary);
    assert.ok(container.render(80)[summary]!.startsWith(frameBorder('│', 'left') + ' ⌄ 2 calls'));
    assert.ok(container.render(80)[row]!.includes(' read  '), 'call name has no extra ANSI styling');
    assert.equal(click(container, row)?.handled, true);
    lines = plain(container);
    assert.ok(lines.some((line) => line.includes('read preview')), 'clicking uses the native abbreviated renderer');
    assert.ok(!lines.some((line) => line.includes('FULL OUTPUT')));
    assert.equal(click(container, row)?.handled, true);
    assert.match(plain(container).join('\n'), /read FULL OUTPUT/);
    assert.equal(click(container, row)?.handled, true);
    assert.doesNotMatch(plain(container).join('\n'), /FULL OUTPUT|read preview/);
    click(container, summary);
    assert.ok(!plain(container).some((line) => line.includes('read.ts')));
    assert.deepEqual(container.children, originalChildren, 'never replace transcript components');
    assert.ok(renders > 0);
});

test('clicked previews stay compact across updates and are bounded even for custom renderers', () => {
    const expandedOptions: boolean[] = [];
    const component = new ToolExecutionComponent('custom', 'long-preview', {}, {}, {
        renderCall: () => new Text('custom call', 0, 0),
        renderResult: (_result: unknown, { expanded }: { expanded: boolean }) => {
            expandedOptions.push(expanded);
            return new Text(Array.from({ length: 50 }, (_, index) => `detail line ${index}`).join('\n'), 0, 0);
        },
    } as any, { requestRender() {} } as any, process.cwd());
    component.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    const container = new Container();
    container.addChild(component);
    click(container, 0);
    click(container, 1);
    assert.equal(expandedOptions.at(-1), false);
    let lines = plain(container);
    assert.ok(lines.length <= 15, '12 preview rows plus the group, call and truncation notice');
    assert.match(lines.join('\n'), /Ctrl\+O 查看完整输出/);
    assert.doesNotMatch(lines.join('\n'), /detail line 49/);
    assert.equal(click(container, lines.length - 1), undefined, 'the truncation notice cannot activate hidden native controls');
    component.updateResult({ content: [{ type: 'text', text: 'updated' }], isError: true });
    component.invalidate();
    assert.equal(expandedOptions.at(-1), false, 'refreshes do not accidentally switch to full output');
    assert.doesNotMatch(plain(container).join('\n'), /detail line 49/);
    component.setExpanded(true);
    assert.equal(expandedOptions.at(-1), true);
    lines = plain(container);
    assert.match(lines.join('\n'), /detail line 49/, 'explicit global expansion remains available');
});

test('local clicks cycle clipped output without pre-rendering full output, and keep full updates full', () => {
    const options: boolean[] = [];
    const component = new ToolExecutionComponent('custom', 'local-cycle', {}, {}, {
        renderCall: () => new Text('custom call', 0, 0),
        renderResult: (_result: unknown, { expanded }: { expanded: boolean }) => {
            options.push(expanded);
            return new Text(Array.from({ length: 30 }, (_, i) => `row ${i}`).join('\n'), 0, 0);
        },
    } as any, { requestRender() {} } as any, process.cwd());
    component.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    const container = new Container();
    container.addChild(component);
    click(container, 0);
    click(container, 1);
    assert.doesNotMatch(plain(container).join('\n'), /row 29/);
    for (let i = 0; i < 4; i++) plain(container);
    assert.ok(options.every(option => !option), 'scroll/input redraws do not probe the full renderer');
    click(container, 1);
    assert.equal(options.at(-1), true);
    assert.match(plain(container).join('\n'), /row 29/);
    component.updateResult({ content: [{ type: 'text', text: 'stream update' }], isError: false }, true);
    component.invalidate();
    assert.equal(options.at(-1), true);
    assert.match(plain(container).join('\n'), /row 29/);
    click(container, 1);
    assert.equal(plain(container).length, 2);
    click(container, 1);
    assert.equal(options.at(-1), false, 'reopening starts at the preview again');
    assert.doesNotMatch(plain(container).join('\n'), /row 29/);
    click(container, 0);
    assert.equal(plain(container).length, 1, 'closing the group does not advance previews to full');
    assert.equal((component as any).expanded, false);
});

test('short results without a second stage collapse immediately, including after resizing', () => {
    const component = new ToolExecutionComponent('custom', 'short-cycle', {}, {}, {
        renderCall: () => new Text('custom call', 0, 0),
        renderResult: () => new Text('short result', 0, 0),
    } as any, { requestRender() {} } as any, process.cwd());
    component.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    const container = new Container();
    container.addChild(component);
    click(container, 0);
    for (const width of [80, 24, 80]) {
        click(container, 1, width);
        assert.match(plain(container, width).join('\n'), /short result/);
        click(container, 1, width);
        assert.equal((component as any).expanded, false);
        assert.equal(plain(container, width).length, 2);
    }
    component.setExpanded(true);
    assert.match(plain(container).join('\n'), /short result/);
    click(container, 1);
    assert.equal((component as any).expanded, false, 'a globally expanded tool closes on one local click');
});

test('image reserve rows are never clipped and do not add a redundant full stage', () => {
    const component = tool();
    const imageLines = ['\x1b_Ga=T,f=32;AAAA\x1b\\', ...Array.from({ length: 25 }, () => '')];
    const updateDisplay = (component as any).updateDisplay.bind(component);
    (component as any).updateDisplay = () => {
        updateDisplay();
        (component as any).imageComponents = [{}];
    };
    component.render = () => imageLines.slice();
    const container = new Container();
    container.addChild(component);
    click(container, 0);
    click(container, 1);
    assert.equal(container.render(80).length, imageLines.length + 2);
    click(container, 1);
    assert.equal((component as any).expanded, false);
    assert.equal(container.render(80).length, 2);
});

test('tool borders consume only left clicks without cycling; call rows cycle and scrolling stays native', () => {
    const component = tool();
    const container = new Container();
    container.addChild(component);
    click(container, 0);
    click(container, 1);
    let lines = plain(container);
    const header = lines.findIndex(line => line.includes('╭─ read'));
    assert.ok(header > 1);
    const wheel = click(container, header, 80, { x: 2, type: 'scroll', button: 'wheel-down' } as any);
    assert.equal(wheel, undefined);
    assert.match(plain(container).join('\n'), /read preview/);
    assert.equal(click(container, header, 80, { x: 2 })?.handled, true);
    assert.match(plain(container).join('\n'), /read preview/);
    click(container, 1);
    assert.match(plain(container).join('\n'), /read FULL OUTPUT/);
    lines = plain(container);
    assert.equal(click(container, lines.findIndex(line => line.includes('╭─ read')), 80, { x: 2 })?.handled, true);
    assert.match(plain(container).join('\n'), /read FULL OUTPUT/);
    click(container, 1);
    assert.equal(plain(container).length, 2);
});

test('native global expand/collapse, restored state and repeated patch installation', async () => {
    const first = tool();
    const second = tool('bash');
    const container = new Container();
    container.addChild(first);
    container.addChild(second);
    plain(container);
    for (const child of [first, second]) child.setExpanded(true);
    assert.match(plain(container).join('\n'), /⌄ 2 calls/);
    assert.equal(plain(container).filter((line) => line.includes('FULL OUTPUT')).length, 2);
    for (const child of [first, second]) child.setExpanded(false);
    assert.equal(plain(container).length, 1);

    const render = Object.getPrototypeOf(ToolExecutionComponent.prototype).render;
    const setExpanded = ToolExecutionComponent.prototype.setExpanded;
    const reloadPath = './patches.ts?folding-reload';
    const reloaded = await import(reloadPath);
    reloaded.patchMergeConsecutiveTools();
    reloaded.patchToolDisplay();
    assert.equal(Object.getPrototypeOf(ToolExecutionComponent.prototype).render, render);
    assert.equal(ToolExecutionComponent.prototype.setExpanded, setExpanded);
    assert.equal(plain(container).length, 1);

    const restored = new Container();
    const expanded = tool();
    expanded.setExpanded(true);
    restored.addChild(expanded);
    assert.match(plain(restored).join('\n'), /⌄ 1 call[\s\S]*FULL OUTPUT/);
});

test('edits, Worker cards and custom messages fold while explanations remain outside', () => {
    const container = new Container();
    for (const child of [
        tool(), assistant('text', 'explanation'), tool(), new UserMessageComponent('next'), tool(),
        new Text('custom message', 0, 0), tool(), tool('edit'), tool(), tool('write'), tool(),
        tool('worker', { self: true }), tool(), tool('worker', { self: true, hidden: true }), tool(),
    ]) container.addChild(child);
    let lines = plain(container);
    assert.equal(lines.filter((line) => /\b\d+ calls?\b/.test(line)).length, 3);
    assert.match(lines[1]!, /1 call/);
    assert.ok(lines.some((line) => line.includes('9 calls')));
    assert.ok(lines.some((line) => line.includes('next')));
    assert.match(lines.join('\n'), /explanation/);
    assert.doesNotMatch(lines.join('\n'), /custom message|edit preview|write preview|worker preview|worker call/);
    assert.ok(lines.some((line) => line.includes('worker×1')));
    click(container, 1);
    assert.match(plain(container).join('\n'), /explanation/);
    const summary = plain(container).findIndex((line) => line.includes('9 calls'));
    click(container, summary);
    lines = plain(container);
    assert.match(lines.join('\n'), /custom message/);
    assert.doesNotMatch(lines.join('\n'), /worker preview|worker call/);
    const row = lines.findIndex((line) => line.includes('› ✓ worker'));
    assert.ok(row > summary);
    click(container, row);
    lines = plain(container);
    assert.match(lines.join('\n'), /worker preview/);
    assert.equal(lines.filter((line) => line.includes('worker call')).length, 1);
});

test('pending and failed calls stay folded, with clear status and on-demand error details', () => {
    const container = new Container();
    const pending = tool('read', { pending: true });
    const failed = tool('grep', { error: true });
    container.addChild(pending);
    container.addChild(failed);
    let lines = plain(container);
    assert.equal(lines.length, 1);
    const inline = ' › 2 calls · read×1  grep×1  ⋯ 1 running  ✗ 1 failed';
    assert.ok(lines[0]!.startsWith(inline), 'status follows the breakdown without right-alignment padding');
    assert.ok(plain(container, 120)[0]!.startsWith(inline), 'wider terminals do not move status to the right edge');
    assert.match(plain(container, 12)[0]!, /^ › .*✗1$/);
    assert.doesNotMatch(lines.join('\n'), /read.ts|grep preview/);
    click(container, 0);
    lines = plain(container);
    assert.equal(lines.length, 3, 'opening a group shows only one row per call');
    const failedRow = lines.findIndex((line) => line.includes('› ✗ grep'));
    click(container, failedRow);
    assert.match(plain(container).join('\n'), /grep preview/);
    click(container, 0);
    pending.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    failed.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    assert.equal(plain(container).length, 1);
    for (const width of [1, 2, 3, 12, 80]) {
        assert.ok(container.render(width).every((line) => visibleWidth(line) <= width));
    }
    click(container, 0);
    for (const width of [1, 2, 3, 12, 80]) {
        assert.ok(container.render(width).every((line) => visibleWidth(line) <= width));
    }
});

test('new tools enter the summary from their first frame without flashing a native card', () => {
    const container = new Container();
    const renderCounts = new Map<string, number>();
    let activeWorker = false;
    const folded = () => {
        const lines = plain(container);
        assert.equal(lines.length, activeWorker ? 2 : 1, 'only an active Worker gets a compact row; native cards never flash');
        assert.doesNotMatch(lines.join('\n'), /[▄▀]|FULL OUTPUT|preview/);
    };
    for (const name of ['read', 'bash', 'edit', 'write', 'worker']) {
        const component = tool(name, { pending: true, self: name === 'worker' });
        const original = component.render.bind(component);
        component.render = (width) => {
            renderCounts.set(name, (renderCounts.get(name) ?? 0) + 1);
            return original(width);
        };
        container.addChild(component);
        activeWorker = name === 'worker';
        folded();
        assert.match(plain(container)[0]!, /^ › \d+ calls?.* {2}⋯ 1 running(?: +≈\S+ tok)? *$/);
        component.updateArgs({ path: `${name}.ts` });
        folded();
        component.markExecutionStarted();
        folded();
        component.updateResult({ content: [{ type: 'text', text: 'streaming status' }], isError: false }, true);
        folded();
        component.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false }, false);
        activeWorker = false;
        folded();
        assert.doesNotMatch(plain(container)[0]!, /running|⋯/);
    }
    for (const name of ['read', 'bash', 'edit', 'write']) {
        assert.equal(renderCounts.get(name) ?? 0, 0, `${name}: folded tools do not render full output offscreen either`);
    }
    click(container, 0);
    let lines = plain(container);
    assert.equal(lines.length, 6, 'five compact call rows, not five native cards');
    assert.match(lines[1]!, /^├ › ✓ read/);
    assert.match(lines[5]!, /^╰ › ✓ worker/);
    const row = lines.findIndex((line) => line.includes('› ✓ read'));
    click(container, row);
    assert.match(plain(container).join('\n'), /read preview/);
    assert.ok((renderCounts.get('read') ?? 0) > 0);
    click(container, 0);
    folded();
    click(container, 0);
    const next = tool('find', { pending: true });
    container.addChild(next);
    lines = plain(container);
    assert.equal(lines.length, 7, 'a new call in an explicitly opened group adds only a compact row');
    assert.doesNotMatch(lines.join('\n'), /FULL OUTPUT|[▄▀]/);
});

test('folded groups show compact active Worker rows, then hide them on completion', () => {
    const container = new Container();
    const live = tool('worker', { pending: true, self: true });
    live.updateArgs({ tasks: [{ objective: 'inspect the current UI' }] });
    const read = tool('read', { pending: true });
    const done = tool('worker', { self: true });
    done.updateArgs({ tasks: [{ objective: 'inspect the docs' }] });
    const hidden = tool('worker', { pending: true, self: true, hidden: true });
    for (const child of [live, read, done, hidden]) container.addChild(child);
    let lines = plain(container);
    assert.equal(lines.length, 2);
    assert.match(lines[0]!, /^ › 3 calls .*worker×2/);
    assert.match(lines[1]!, /^╰ › ○ task 1 · inspect the current UI/);
    assert.doesNotMatch(lines.join('\n'), /[▄▀]|FULL OUTPUT|read.ts/);
    live.updateResult({ content: [{ type: 'text', text: 'working' }], isError: false }, true);
    click(container, 1);
    assert.match(plain(container).join('\n'), /worker preview/);
    assert.match(plain(container)[0]!, /^ › /, 'opening the Worker does not open unrelated calls');
    live.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    assert.match(plain(container).join('\n'), /worker preview/, 'preserve explicitly opened previews after completion');
    click(container, 1);
    assert.match(plain(container).join('\n'), /worker FULL OUTPUT/);
    click(container, 1);
    assert.equal(plain(container).length, 1);
    live.updateResult({ content: [{ type: 'text', text: 'working' }], isError: false }, true);
    done.updateResult({ content: [{ type: 'text', text: 'working' }], isError: false }, true);
    lines = plain(container);
    assert.equal(lines.length, 3);
    assert.match(lines[1]!, /^├ › ○ task 1/);
    assert.match(lines[2]!, /^╰ › ○ task 1/);
    click(container, 0);
    assert.equal(plain(container).filter((line) => /[├╰] › ○ task 1/.test(line)).length, 2, 'no duplicate task rows when expanded');
    click(container, 0);
    for (const width of [1, 2, 3, 12, 80]) {
        assert.ok(container.render(width).every((line) => visibleWidth(line) <= width));
    }
    live.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    done.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    assert.equal(plain(container).length, 1, 'completed default rows retract even while other tools are running');
});

test('every Worker task shows its own live metadata before its objective', (t) => {
    const makeTask = (index: number, status: WorkerUiTask['status']): WorkerUiTask => ({
        index, status, mode: 'scout', objective: `objective ${index + 1}`, requestedPreset: 'normal',
        resolvedPreset: 'fast', sessionMode: 'new', attempt: 1, phase: '', activities: [], toolCalls: 0,
        usage: { ...emptyWorkerUsage(), input: 9000, output: 284, turns: 3 }, startedAt: 1000,
        ...(status === 'completed' ? { finishedAt: 3000 } : {}),
    });
    const details: WorkerUiDetails = {
        kind: 'worker-ui', batchId: 'preview-batch', startedAt: 1000, snapshotAt: 9600,
        limit: 2, total: 5, completed: 1,
        tasks: ['running', 'queued', 'completed', 'failed', 'blocked'].map((status, index) => makeTask(index, status as WorkerUiTask['status'])),
    };
    details.tasks[1] = { ...details.tasks[1]!, mode: 'review', sessionMode: 'fork', usage: emptyWorkerUsage(), startedAt: undefined };
    const component = tool('worker', { self: true, pending: true });
    component.updateArgs({ tasks: details.tasks.map((task) => ({ mode: task.mode, preset: 'normal', sessionMode: task.sessionMode, objective: task.objective })) });
    const container = new Container();
    container.addChild(component);
    let lines = plain(container, 160);
    assert.equal(lines.length, 6, 'all input tasks appear even before the first result');
    assert.match(lines[1]!, /○ scout 󰐖 · normal · objective 1/);
    component.updateResult({ content: [{ type: 'text', text: 'working' }], details, isError: false }, true);
    let latest = details;
    const refresh = new WorkerRenderRefresh(() => latest, {
        setInterval: () => 1, clearInterval() {}, setTimeout: () => 2, clearTimeout() {},
    });
    t.after(() => refresh.dispose());
    refresh.resolve(details, { state: (component as any).rendererState, invalidate() {} });
    lines = plain(container, 160);
    assert.equal(lines.length, 6);
    assert.match(lines[1]!, /^├ › ⋯ scout 󰐖 · fast · Turn 3 · ↑9k ↓284 · 8\.6s · objective 1/);
    assert.match(lines[2]!, /^├ › ○ review  · fast · objective 2/);
    assert.match(lines[3]!, /^├ › ✓ scout 󰐖 · fast · Turn 3 · ↑9k ↓284 · 2\.0s · objective 3/);
    assert.match(lines[4]!, /^├ › ✗ scout/);
    assert.match(lines[5]!, /^╰ › ! scout/);
    assert.doesNotMatch(lines.join('\n'), /FULL OUTPUT|[▄▀]/);
    latest = { ...details, snapshotAt: 10600, tasks: details.tasks.map((task) => task.index === 0
        ? { ...task, usage: { ...task.usage, input: 10000, output: 500, turns: 4 } } : task) };
    refresh.refreshBatch('preview-batch');
    assert.match(plain(container, 160)[1]!, /Turn 4 · ↑10k ↓500 · 9\.6s · objective 1/);
    assert.equal(details.tasks[0]!.usage.turns, 3, 'live metadata does not mutate the original result');
    click(container, 3, 160);
    assert.match(plain(container, 160).join('\n'), /worker preview/);
    click(container, 3, 160);
    assert.match(plain(container, 160).join('\n'), /worker FULL OUTPUT/);
    click(container, 3, 160);
    assert.equal(plain(container, 160).length, 6);
    for (const width of [1, 3, 12, 80]) {
        assert.ok(container.render(width).every((line) => visibleWidth(line) <= width));
    }
    component.updateResult({ content: [{ type: 'text', text: 'done' }], details: latest, isError: false });
    assert.equal(plain(container).length, 1);
    component.setExpanded(true);
    t.mock.method(Date, 'now', () => 100000);
    const first = plain(container, 160)[1];
    t.mock.method(Date, 'now', () => 200000);
    assert.equal(plain(container, 160)[1], first, 'snapshot time is stable, not elapsed wall time after completion');

    component.setExpanded(false);
    const framed = new Container();
    framed.addChild(assistant('text', 'Worker progress stays visible'));
    framed.addChild(component);
    component.updateResult({ content: [{ type: 'text', text: 'working' }], details: latest, isError: false }, true);
    let framedLines = plain(framed, 160);
    assert.match(framedLines[0]!, /^╭─ 助手/);
    assert.ok(framedLines.slice(1, -1).every(line => line.startsWith('│') && line.endsWith('│')));
    const taskRow = framedLines.findIndex(line => line.includes('objective 1'));
    assert.ok(taskRow > 0);
    click(framed, taskRow, 160);
    assert.match(plain(framed, 160).join('\n'), /worker preview/);
    click(framed, taskRow, 160);
    assert.match(plain(framed, 160).join('\n'), /worker FULL OUTPUT/);
    click(framed, taskRow, 160);
    component.updateResult({ content: [{ type: 'text', text: 'done' }], details: latest, isError: false });
    framedLines = plain(framed, 160);
    assert.match(framedLines.join('\n'), /Worker progress stays visible/);
    assert.doesNotMatch(framedLines.join('\n'), /objective 1|worker preview/);
});

test('summary tokens follow the call breakdown without shifting right on wider terminals', () => {
    const container = new Container();
    container.addChild(tool('bash'));
    const summary = plain(container, 80)[0]!.trimEnd();
    assert.match(summary, /^ › 1 call · bash×1 ≈\d+ tok$/);
    for (const width of [40, 80, 120]) assert.equal(plain(container, width)[0]!.trimEnd(), summary);
});

test('tree detail indentation preserves mouse coordinates and summary badges fit the terminal', (t) => {
    const component = tool();
    component.setExpanded(true);
    const container = new Container();
    container.addChild(component);
    const seen: TuiMouseEvent[] = [];
    t.mock.method(component, 'handleMouse', (event: TuiMouseEvent) => {
        seen.push(event);
        return { handled: true };
    });
    const lines = plain(container);
    assert.match(lines[0]!, /read×1 ≈\d+ tok +$/);
    assert.match(lines[1]!, /^╰ ⌄ ✓ read/);
    assert.ok(lines[2]!.startsWith('│ '));
    assert.equal(click(container, 2, 80, { x: 4, screenX: 4 })?.handled, true);
    assert.equal(seen[0]!.x, 2);
    assert.equal(seen[0]!.y, 0);
    assert.equal(seen[0]!.width, 78);
    assert.equal(click(container, 2, 80, { x: 0 }), undefined, 'the rail itself is not native tool content');
    for (const width of [5, 12, 64, 80]) {
        assert.ok(container.render(width).every((line) => visibleWidth(line) <= width));
    }
});

test('thinking rendering and visibility remain native', () => {
    assert.equal(AssistantMessageComponent.prototype.updateContent, nativeUpdateContent);
    const component = new AssistantMessageComponent(thinkingMessage, false);
    assert.deepEqual(component.render(80), nativeThinking[0]);
    component.setHideThinkingBlock(true);
    assert.deepEqual(component.render(80), nativeThinking[1]);
    component.setHideThinkingBlock(false);
    assert.deepEqual(component.render(80), nativeThinking[0]);
});

test('assistant failures remain accessible without displaying any thinking', () => {
    for (const stopReason of ['error', 'aborted', 'length']) {
        const container = new Container();
        container.addChild(tool());
        container.addChild(new AssistantMessageComponent({ role: 'assistant', content: [], stopReason, errorMessage: 'failure detail' } as any));
        assert.equal(plain(container).length, 3, 'one summary inside a single frame');
        assert.match(plain(container)[1]!, /✗ 1 failed/);
        click(container, 1);
        assert.match(plain(container).join('\n'), stopReason === 'length' ? /truncated/ : /failure detail/);
    }
    const container = new Container();
    container.addChild(tool());
    const thinking = assistant('thinking', 'still working');
    const message = { role: 'assistant', content: [{ type: 'thinking', thinking: 'still working' }] } as any;
    thinking.updateContent(message, true);
    container.addChild(thinking);
    assert.doesNotMatch(plain(container).join('\n'), /still working/);
    thinking.updateContent(message, false);
    assert.doesNotMatch(plain(container).join('\n'), /still working/);
    assert.equal(plain(container).length, 3);
    container.addChild(tool('bash'));
    assert.equal(plain(container).filter((line) => line.includes('2 calls')).length, 1);
    click(container, 1);
    assert.doesNotMatch(plain(container).join('\n'), /still working|thinking/i);
});

test('streamed commentary becomes an external boundary with independent group expansion', () => {
    const container = new Container();
    const first = tool();
    const message = assistant('text', '');
    const second = tool('bash');
    container.addChild(first);
    container.addChild(message);
    container.addChild(second);
    assert.match(plain(container)[1]!, /2 calls/);
    message.updateContent({ role: 'assistant', content: [{ type: 'text', text: 'visible commentary' }] } as any);
    assert.equal(plain(container).filter((line) => line.includes('1 call')).length, 2);
    assert.match(plain(container).join('\n'), /visible commentary/);
    second.setExpanded(true);
    const lines = plain(container);
    assert.match(lines[1]!, /› 1 call/);
    assert.ok(lines.some((line) => line.includes('⌄ 1 call')));
    assert.ok(lines.some((line) => line.includes('visible commentary')));
});

test('explanations precede their folded tool groups and never duplicate on expansion', () => {
    const container = new Container();
    const explanation = (text: string) => new AssistantMessageComponent({ role: 'assistant', content: [
        { type: 'text', text }, { type: 'toolCall', id: text, name: 'read', arguments: {} },
    ] } as any);
    container.addChild(explanation('EXPLANATION_ONE'));
    container.addChild(tool());
    container.addChild(assistant('text', '   '));
    container.addChild(tool('write'));
    container.addChild(explanation('EXPLANATION_TWO'));
    const pending = tool('edit', { pending: true });
    container.addChild(pending);
    container.addChild(tool('bash'));
    container.addChild(assistant('text', 'FINAL_ANSWER'));
    const check = () => {
        const lines = plain(container);
        const summaries = lines.flatMap((line, index) => line.includes('2 calls') ? [index] : []);
        assert.equal(summaries.length, 2);
        const first = lines.findIndex((line) => line.includes('EXPLANATION_ONE'));
        const second = lines.findIndex((line) => line.includes('EXPLANATION_TWO'));
        const final = lines.findIndex((line) => line.includes('FINAL_ANSWER'));
        assert.ok(first < summaries[0]! && summaries[0]! < second && second < summaries[1]! && summaries[1]! < final);
        assert.equal(summaries[0], first + 1, 'first summary immediately follows the explanation');
        assert.equal(summaries[1], second + 1, 'second summary immediately follows the explanation');
        assert.equal(lines.filter((line) => line.includes('EXPLANATION_ONE')).length, 1);
        assert.equal(lines.filter((line) => line.includes('EXPLANATION_TWO')).length, 1);
        assert.doesNotMatch(lines.join('\n'), /FULL OUTPUT|[▄▀]/);
        return summaries;
    };
    let summaries = check();
    pending.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    summaries = check();
    click(container, summaries[0]!);
    summaries = check();
    click(container, summaries[1]!);
    summaries = check();
    click(container, summaries[0]!);
    check();
});

test('all thinking stays hidden even in expanded groups and restored branches', () => {
    const container = new Container();
    const oldFinal = new AssistantMessageComponent({ role: 'assistant', content: [
        { type: 'thinking', thinking: 'HISTORICAL_FINAL' }, { type: 'text', text: 'old answer' },
    ] } as any);
    const latestFinal = new AssistantMessageComponent({ role: 'assistant', content: [
        { type: 'thinking', thinking: 'EARLIER_FINAL_BLOCK' },
        { type: 'thinking', thinking: 'LATEST_FINAL_THOUGHT' },
        { type: 'text', text: 'latest answer' },
    ] } as any);
    const tools = [tool(), tool(), tool('edit'), tool('bash')];
    const components = [
        new UserMessageComponent('old question'), assistant('thinking', 'OLD_STEP'), tools[0]!, oldFinal,
        new UserMessageComponent('new question'), assistant('thinking', 'NEW_STEP'), tools[1]!,
        assistant('text', 'intermediate commentary'), tools[2]!, assistant('thinking', 'BEFORE_LAST_TOOL'), tools[3]!, latestFinal,
    ];
    components.forEach((component) => container.addChild(component));
    const originals = components.filter((component) => component instanceof AssistantMessageComponent)
        .map((component) => ({ component, message: (component as any).lastMessage, content: JSON.stringify((component as any).lastMessage) }));
    assert.doesNotMatch(plain(container).join('\n'), /THOUGHT|FINAL_BLOCK|HISTORICAL|OLD_STEP|NEW_STEP|BEFORE_LAST_TOOL/);
    let output = plain(container).join('\n');
    assert.match(output, /old answer/);
    assert.match(output, /latest answer/);
    assert.doesNotMatch(output, /LATEST_FINAL_THOUGHT|EARLIER_FINAL_BLOCK|HISTORICAL|OLD_STEP|NEW_STEP|BEFORE_LAST_TOOL/);
    assert.match(output, /intermediate commentary/);
    assert.equal(plain(container).filter((line) => /\b\d+ calls?\b/.test(line)).length, 3);
    tools.forEach((component) => component.setExpanded(true));
    output = plain(container).join('\n');
    assert.match(output, /intermediate commentary/);
    assert.doesNotMatch(output, /LATEST_FINAL_THOUGHT|EARLIER_FINAL_BLOCK|HISTORICAL|OLD_STEP|NEW_STEP|BEFORE_LAST_TOOL|thinking/i);
    const summary = plain(container).findIndex((line) => line.includes('2 calls'));
    click(container, summary);
    assert.doesNotMatch(plain(container).join('\n'), /LATEST_FINAL_THOUGHT/);
    const nextUser = new UserMessageComponent('next question');
    container.addChild(nextUser);
    assert.doesNotMatch(plain(container).join('\n'), /LATEST_FINAL_THOUGHT/);
    assert.match(plain(container).join('\n'), /old answer[\s\S]*latest answer/);
    container.removeChild(nextUser);
    assert.doesNotMatch(plain(container).join('\n'), /LATEST_FINAL_THOUGHT/);
    click(container, summary);
    assert.doesNotMatch(plain(container).join('\n'), /LATEST_FINAL_THOUGHT|thinking/i, 'restored branches also omit thinking');
    assert.deepEqual(container.children, components);
    for (const { component, message, content } of originals) {
        assert.equal((component as any).lastMessage, message);
        assert.equal(JSON.stringify((component as any).lastMessage), content);
    }
});

test('thinking-only messages produce no summary or placeholder regardless of native visibility', () => {
    const container = new Container();
    container.addChild(new UserMessageComponent('old'));
    container.addChild(assistant('thinking', 'HIDDEN_OLD_THINKING'));
    container.addChild(assistant('text', 'old answer'));
    container.addChild(new UserMessageComponent('new'));
    const latest = new AssistantMessageComponent({ role: 'assistant', content: [
        { type: 'thinking', thinking: 'HIDDEN_FIRST_BLOCK' }, { type: 'thinking', thinking: 'LAST_BLOCK' },
        { type: 'text', text: 'final answer' },
    ] } as any, true);
    container.addChild(latest);
    for (const hidden of [true, false, true]) {
        latest.setHideThinkingBlock(hidden);
        const output = plain(container).join('\n');
        assert.match(output, /old answer/);
        assert.match(output, /final answer/);
        assert.doesNotMatch(output, /LAST_BLOCK|HIDDEN_|thinking/i);
    }
    const updated = { role: 'assistant', content: [{ type: 'thinking', thinking: 'UPDATED_LAST_BLOCK' }] } as any;
    latest.updateContent(updated);
    assert.doesNotMatch(plain(container).join('\n'), /UPDATED_LAST_BLOCK|HIDDEN_|thinking/i);
    const standalone = new Container();
    standalone.addChild(latest);
    assert.deepEqual(standalone.render(80), [], 'no empty thinking summary remains');
    // Native user/assistant markdown reserves one padding cell on each side.
    for (const width of [3, 4, 12, 80]) {
        const lines = container.render(width);
        assert.ok(lines.every((line) => visibleWidth(line) <= width));
    }
});

test('agent lifecycle never displays thinking, including after agent_end', async (t) => {
    const { default: ui } = await import('./index.ts');
    const handlers = new Map<string, (event: any, ctx: any) => void>();
    ui({ on: (name: string, handler: any) => handlers.set(name, handler), events: { on() {} } } as any);
    const ctx = { ui: { setWorkingVisible() {}, theme: { fg: (_color: string, text: string) => text } } };
    const emit = (name: string, event = {}) => handlers.get(name)!(event, ctx);
    t.after(() => emit('session_shutdown'));
    const container = new Container();
    container.addChild(assistant('thinking', 'final lifecycle thought'));
    emit('agent_start');
    assert.doesNotMatch(plain(container).join('\n'), /final lifecycle thought/);
    emit('turn_start', { turnIndex: 2 });
    assert.doesNotMatch(plain(container).join('\n'), /final lifecycle thought/);
    emit('agent_end');
    assert.deepEqual(plain(container), []);
});
