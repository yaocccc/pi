import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { AssistantMessageComponent, ToolExecutionComponent } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences, type TuiMouseEvent } from '@earendil-works/pi-tui';
import { patchToolDisplay, patchMergeConsecutiveTools } from './patches.ts';
import { ToolActivityGroup } from './folding.ts';
import { layoutRounds, RoundFrame, restoreRoundSummaries } from './rounds.ts';
import { RenderCache, renderCacheBudget, withRenderCachePass } from './render-cache.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
initTheme('dark', false);
patchToolDisplay();
patchMergeConsecutiveTools();
const themeKey = Symbol.for('@earendil-works/pi-coding-agent:theme');
const plain = (lines: string[]) => lines.map(stripTerminalSequences).join('\n');
const message = (text: string): any => ({ role: 'assistant', content: [{ type: 'text', text }],
    provider: 'test', model: 'test-model', api: 'openai-responses', timestamp: 1, stopReason: 'stop',
    usage: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, totalTokens: 0,
        cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } } });
function tool(name = 'read', definition?: any) {
    const args = { path: 'same.txt' };
    const result = { content: [{ type: 'text' as const, text: 'same output' }], isError: false };
    const component = new ToolExecutionComponent(name, `stable-${name}`, args, { showImages: false }, definition,
        { requestRender() {} } as any, process.cwd());
    component.updateResult(result);
    return { component, args, result };
}
function fixture(name = 'read', definition?: any) {
    restoreRoundSummaries([]);
    const msg = message('settled explanation');
    const source = new AssistantMessageComponent(msg);
    const item = tool(name, definition);
    // Real history keeps an empty assistant/tool-call carrier inside the group.
    const carrier = new AssistantMessageComponent(message(''));
    const frame = layoutRounds([source, carrier, item.component])[0] as RoundFrame;
    const children = (frame as any).body.children;
    const group = children.find((child: any) => child instanceof ToolActivityGroup) as ToolActivityGroup;
    return { ...item, source, carrier, msg, frame, group, view: children[0] };
}
function click(frame: RoundFrame, lines: string[], pattern: RegExp) {
    const row = lines.findIndex(line => pattern.test(stripTerminalSequences(line)));
    assert.ok(row >= 0, `missing click target ${pattern}`);
    return frame.handleMouse({ type: 'click', button: 'left', x: 3, y: row,
        width: 100, height: lines.length } as TuiMouseEvent);
}

test('settled projected rounds skip both child renders and protect cached row arrays', t => {
    const { frame, view, group } = fixture();
    const assistantRender = t.mock.method(view, 'render');
    const groupRender = t.mock.method(group, 'render');
    const first = frame.render(100);
    const expected = [...first];
    assert.ok((frame as any).cache.get().keys, 'fixture must actually qualify for the fast path');
    first[0] = 'caller changed returned array';
    for (let i = 0; i < 4; i++) assert.deepEqual(frame.render(100), expected);
    assert.equal(assistantRender.mock.callCount(), 1);
    assert.equal(groupRender.mock.callCount(), 1);
    assert.match(plain(expected), /settled explanation/);
    assert.match(plain(expected), /1 call/);
});

test('unchanged expanded rows retain their original raw/framed snapshot sharing', t => {
    const { frame, group } = fixture();
    group.open = true;
    const render = t.mock.method(group, 'render');
    const expected = frame.render(100);
    const { content, lines } = (frame as any).cache.get();
    assert.deepEqual(frame.render(100), expected);
    assert.equal(render.mock.callCount(), 2, 'open groups must still render');
    assert.equal((frame as any).cache.get().content, content);
    assert.equal((frame as any).cache.get().lines, lines);
});

test('a hidden assistant carrier that gains text leaves the cached group', () => {
    const { source, carrier, component, frame } = fixture();
    frame.render(100);
    carrier.updateContent(message('new visible explanation'));
    const next = layoutRounds([source, carrier, component])[0] as RoundFrame;
    assert.equal(next, frame);
    assert.match(plain(next.render(100)), /new visible explanation/);
});

test('same-object assistant updates, streaming, configuration, theme, width and invalidation rebuild', t => {
    const { frame, view, source, msg } = fixture();
    const render = t.mock.method(view, 'render');
    frame.render(100);
    const rebuild = (action: () => void, width = 100) => {
        const before = render.mock.callCount();
        action();
        const output = frame.render(width);
        assert.equal(render.mock.callCount(), before + 1);
        return output;
    };
    const updated = rebuild(() => { msg.content[0].text = 'same-object update'; source.updateContent(msg); });
    assert.match(plain(updated), /same-object update/);
    rebuild(() => source.updateContent(msg, true));
    rebuild(() => {});
    rebuild(() => source.updateContent(msg, false));
    frame.render(100);
    rebuild(() => source.setOutputPad(3));
    rebuild(() => frame.invalidate());
    rebuild(() => {}, 70);
    rebuild(() => {}, 100);
    const originalTheme = (globalThis as any)[themeKey];
    try { rebuild(() => { initTheme('light', false); }); }
    finally { (globalThis as any)[themeKey] = originalTheme; }
});

test('tool revisions, hidden/failed states, group opening and tool expansion invalidate stable keys', t => {
    const { frame, group, component, args, result } = fixture();
    const render = t.mock.method(group, 'render');
    frame.render(100);
    const rebuild = (action: () => void) => {
        const before = render.mock.callCount();
        action();
        const lines = frame.render(100);
        assert.equal(render.mock.callCount(), before + 1);
        return lines;
    };
    rebuild(() => { args.path = 'updated.txt'; component.updateArgs(args); });
    rebuild(() => { result.content[0].text = 'updated result '.repeat(50); component.updateResult(result); });
    assert.match(plain(rebuild(() => { result.isError = true; })), /failed/);
    assert.doesNotMatch(plain(rebuild(() => { (component as any).hideComponent = true; })), /call/);
    assert.match(plain(rebuild(() => { (component as any).hideComponent = false; })), /call/);
    rebuild(() => { group.open = true; });
    rebuild(() => {});
    rebuild(() => { group.open = false; });
    rebuild(() => { component.setExpanded(true); group.open = false; });
});

test('unknown self visibility remains live without updateDisplay and Worker/image groups never qualify', t => {
    let visible = true;
    const renderer = { render: () => visible ? ['live self'] : [], invalidate() {} };
    const custom = fixture('custom', { name: 'custom', label: 'custom', renderShell: 'self',
        renderCall: () => renderer, renderResult: () => renderer });
    const render = t.mock.method(custom.group, 'render');
    assert.match(plain(custom.frame.render(100)), /1 call/);
    visible = false;
    assert.doesNotMatch(plain(custom.frame.render(100)), /1 call/);
    visible = true;
    assert.match(plain(custom.frame.render(100)), /1 call/);
    assert.equal(render.mock.callCount(), 3);
    for (const name of ['worker', 'read']) {
        const item = fixture(name);
        if (name === 'read') (item.component as any).imageComponents = [{}];
        const childRender = t.mock.method(item.group, 'render');
        item.frame.render(100);
        item.frame.render(100);
        assert.equal(item.group.getStableRenderKey(), undefined);
        assert.equal(childRender.mock.callCount(), 2);
    }
});

test('custom Markdown and image-producing assistants do not authorize whole-round reuse', t => {
    for (const mode of ['transformer', 'image']) {
        const { frame, source, view } = fixture();
        if (mode === 'transformer') (source as any).markdownTransformers = [(text: string) => text];
        frame.render(100);
        if (mode === 'image') {
            frame.invalidate();
            frame.render(100);
            t.mock.method(view.display, 'render', () => ['\x1b_Ga=T,f=100;AAAA\x1b\\']);
            // Force one genuine child render so the image protocol is observed.
            renderCacheBudget.clear();
        }
        const render = t.mock.method(view, 'render');
        const first = frame.render(100);
        const second = frame.render(100);
        assert.deepEqual(second, first);
        assert.equal(render.mock.callCount(), 2);
        assert.equal(view.getStableRenderKey(), undefined);
    }
});

test('identical-text A/B/A group replacement refreshes layout and clicks reach the current tool', () => {
    const { frame, source, component: a, group: groupA } = fixture();
    const b = tool().component;
    const original = frame.render(100);
    frame.render(100);
    assert.equal(layoutRounds([source, b])[0], frame);
    const groupB = (frame as any).body.children[1] as ToolActivityGroup;
    assert.notEqual(groupB, groupA);
    assert.deepEqual(frame.render(100), original);
    assert.equal(layoutRounds([source, a])[0], frame);
    assert.deepEqual(frame.render(100), original);
    assert.equal(click(frame, frame.render(100), /1 call/)?.handled, true);
    assert.equal(groupA.open, true);
    assert.equal(groupB.open, false);
    assert.equal(click(frame, frame.render(100), /›.*same\.txt/)?.handled, true);
    assert.equal((a as any).expanded, true);
    assert.equal((b as any).expanded, false);
});

test('identical-text assistant A/B/A replacement restores the current native mouse target', t => {
    const { frame, view: viewA, group } = fixture();
    const other = new AssistantMessageComponent(message('settled explanation'));
    const otherFrame = layoutRounds([other])[0] as RoundFrame;
    const viewB = (otherFrame as any).body.children[0];
    const label = (frame as any).label;
    const renderA = t.mock.method(viewA, 'render');
    const original = frame.render(100);
    const mouseA = t.mock.method(viewA.display, 'handleMouse', () => ({ handled: true }));
    frame.update(label, [viewB, group]);
    assert.deepEqual(frame.render(100), original);
    const mouseB = t.mock.method(viewB.display, 'handleMouse', () => ({ handled: true }));
    frame.update(label, [viewA, group]);
    assert.deepEqual(frame.render(100), original);
    assert.equal(renderA.mock.callCount(), 2, 'returning to A must rebuild the layout last rendered for B');
    assert.equal(click(frame, frame.render(100), /settled explanation/)?.handled, true);
    assert.equal(mouseA.mock.callCount(), 1);
    assert.equal(mouseB.mock.callCount(), 0);
});

test('same-theme badge-only ANSI changes invalidate a collapsed whole-round snapshot', () => {
    const theme = (globalThis as any)[themeKey];
    const original = theme.fg;
    let color = 31;
    theme.fg = function (name: string, text: string) {
        return text.startsWith('≈') ? `\x1b[${color}m${text}\x1b[39m` : original.call(this, name, text);
    };
    try {
        const { frame } = fixture();
        const first = frame.render(100);
        assert.deepEqual(frame.render(100), first);
        color = 32;
        const changed = frame.render(100);
        assert.equal(plain(changed), plain(first));
        assert.notDeepEqual(changed, first);
    } finally { theme.fg = original; }
});

test('cleared and exhausted budgets preserve rows and click routing', () => {
    renderCacheBudget.clear();
    const { frame, group } = fixture();
    const expected = frame.render(100);
    renderCacheBudget.clear();
    assert.deepEqual(frame.render(100), expected);
    renderCacheBudget.clear();
    const blocker = new RenderCache<string>();
    blocker.set('reserved', renderCacheBudget.maxBytes);
    try {
        for (let pass = 0; pass < 2; pass++) withRenderCachePass(() => {
            assert.equal(blocker.get(), 'reserved');
            assert.deepEqual(frame.render(100), expected);
            assert.equal(renderCacheBudget.usedBytes, renderCacheBudget.maxBytes);
        });
        assert.equal(click(frame, expected, /1 call/)?.handled, true);
        assert.equal(group.open, true);
    } finally { renderCacheBudget.clear(); }
});
