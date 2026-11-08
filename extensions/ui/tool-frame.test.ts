import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { ToolExecutionComponent } from '@earendil-works/pi-coding-agent';
import { Container, Text, stripTerminalSequences, visibleWidth, type Component, type TuiMouseEvent } from '@earendil-works/pi-tui';
import { patchToolDisplay, patchMergeConsecutiveTools } from './patches.ts';
import { patchToolContext } from './tool-context.ts';
import { frameBorder } from './utils.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
const { createEditToolDefinition } = await import(pathToFileURL(join(root, 'core/tools/edit.js')).href);
initTheme('dark', false);
patchToolDisplay();
patchMergeConsecutiveTools();
patchToolContext();

const setOutputPad = (tool: ToolExecutionComponent, padding: number) => {
    const native = tool as any;
    if (native.setOutputPad) native.setOutputPad(padding);
    else {
        // Pi 1.0.4 predates this setting; mirror the current native layout fields.
        native.outputPad = padding;
        native.contentBox.paddingX = native.contentText.paddingX = padding;
        tool.invalidate();
    }
};
const make = (definition?: any, name = 'frame-test', outputPad = 1) => {
    const tool = new ToolExecutionComponent(
        name, 'frame-test', { path: '中文🙂.ts' }, { outputPad } as any, definition, { requestRender() {} } as any, process.cwd(),
    );
    setOutputPad(tool, outputPad);
    return tool;
};
const click = (component: Component, x: number, y: number, width = 40, extra: Partial<TuiMouseEvent> = {}) => component.handleMouse?.({
    type: 'click', button: 'left', x, y, screenX: x, screenY: y, width, height: component.render(width).length,
    shift: false, alt: false, ctrl: false, ...extra,
});
const plain = (component: Component, width = 40) => component.render(width).map(stripTerminalSequences);

test('native fallback and custom tools have one background-free frame at every supported width', () => {
    const result = { content: [{ type: 'text' as const, text: '原始输出🙂\nsecond line' }], isError: false };
    const before = structuredClone(result);
    for (const definition of [undefined, { renderCall: () => new Text('中文🙂 call', 0, 0), renderResult: () => new Text('result', 0, 0) }]) {
        for (const outputPad of [0, 1, 3]) {
            const tool = make(definition, 'frame-test', outputPad);
            for (const phase of ['pending', 'partial', 'done', 'error']) {
                if (phase !== 'pending') tool.updateResult({ ...result, isError: phase === 'error' }, phase === 'partial');
                for (let width = 0; width <= 48; width++) {
                    const lines = tool.render(width), text = lines.map(stripTerminalSequences);
                    assert.ok(lines.every(line => visibleWidth(line) <= width), `${phase} width ${width}`);
                    assert.doesNotMatch(lines.join('\n'), /\x1b\[48;|[▄▀]/);
                    if (width >= 3) {
                        assert.match(text[0]!, /^╭─.*╮$/);
                        assert.match(text.at(-1)!, /^╰─*╯$/);
                        assert.equal(text.filter(line => /^╭─.*╮$/.test(line)).length, 1);
                    } else if (width === 0) assert.deepEqual(lines, []);
                }
            }
        }
    }
    assert.deepEqual(result, before);
});

test('tool and Worker frame padding preserve native content mouse coordinates and ignore edges', () => {
    for (const worker of [false, true]) for (const outputPad of [0, 1, 3]) {
        const events: TuiMouseEvent[] = [];
        const target: Component = {
            render: () => ['hit target'], invalidate() {},
            handleMouse(event) { events.push(event); return { handled: true }; },
        };
        const tool = make({ ...(worker ? { renderShell: 'self' } : {}), renderCall: () => target }, worker ? 'worker' : 'frame-test', outputPad);
        const lines = plain(tool), row = lines.findIndex(line => line.includes('hit target'));
        click(tool, 4, row);
        assert.equal(events.length, 1);
        assert.equal(events[0]!.x, 4 - outputPad - 1);
        assert.equal(events[0]!.y, 0);
        assert.equal(events[0]!.width, 40 - 2 * (outputPad + 1));
        for (const [x, y] of [[0, row], [39, row], [4, 0], [4, lines.length - 1]]) {
            click(tool, x!, y!);
            assert.equal(click(tool, x!, y!, 40, { type: 'wheel', button: 'none' }), undefined, 'border wheel events remain available to transcript scrolling');
        }
        assert.equal(events.length, 1, 'border cells do not reach the content');
    }
    const fallback = make();
    click(fallback, 1, 1);
    assert.equal((fallback as any).expanded, false, 'pending fallback content does not expand');
    fallback.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    click(fallback, 1, 0);
    assert.equal((fallback as any).expanded, false, 'fallback top edge does not toggle the native result region');
    click(fallback, 1, 1);
    assert.equal((fallback as any).expanded, true, 'fallback content retains the native expand action');
});

test('dynamic outputPad remains inside the border for default, fallback and Worker shells', () => {
    for (const kind of ['default', 'fallback', 'worker']) {
        const tool = make(kind === 'fallback' ? undefined : {
            ...(kind === 'worker' ? { renderShell: 'self' } : {}),
            renderCall: () => new Text('x'.repeat(120), 0, 0),
        }, kind);
        tool.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
        for (const padding of [0, 3, 1, 0]) {
            setOutputPad(tool, padding);
            const originalPadding = [(tool as any).contentBox.paddingX, (tool as any).contentText.paddingX];
            const lines = plain(tool), body = lines.slice(1, -1);
            const first = body.find(line => line.slice(1, -1).trim())!;
            assert.equal(first.search(/[^│ ]/), padding + 1, `${kind}: left outputPad ${padding}`);
            assert.ok(body.every(line => line.endsWith(' '.repeat(padding) + '│')), `${kind}: right outputPad ${padding}`);
            assert.match(lines[0]!, /^╭─ \S+ ≈\S+ tok ─+╮$/);
            assert.doesNotMatch(body.join('\n'), /≈\S+ tok/);
            assert.deepEqual([(tool as any).contentBox.paddingX, (tool as any).contentText.paddingX], originalPadding, 'temporary frame padding is restored, including inactive shells');
            assert.deepEqual(plain(tool), lines, 'cached renders preserve the same padding');
        }
    }
});

test('standalone tool heading moves to the title without changing native content or mouse coordinates', () => {
    const source = ['text(await tools.read({path:"source.ts"}));', 'second line'];
    const events: TuiMouseEvent[] = [];
    const call = new Container();
    call.addChild(new Text('codemode', 0, 0));
    call.addChild({ render: () => source, invalidate() {}, handleMouse(event) { events.push(event); return { handled: true }; } });
    const children = [...call.children];
    const tool = make({ renderCall: () => call }, 'codemode', 3);
    let lines = plain(tool, 80);
    assert.match(lines[0]!, /^╭─ codemode ─+╮$/);
    assert.match(lines[1]!, /^│ {3}text\(await/);
    assert.equal(lines.filter(line => line.includes('codemode')).length, 1);
    click(tool, 6, 1, 80);
    assert.equal(events[0]!.x, 2);
    assert.equal(events[0]!.y, 0);
    assert.equal(events[0]!.width, 72);
    assert.deepEqual(call.children, children);
    assert.deepEqual(source, ['text(await tools.read({path:"source.ts"}));', 'second line']);
    tool.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
    lines = plain(tool, 80);
    assert.match(lines[0]!, /^╭─ codemode ≈\S+ tok ─+╮$/);
    assert.doesNotMatch(lines.slice(1).join('\n'), /≈\S+ tok/);
    for (let width = 1; width <= 48; width++) assert.ok(tool.render(width).every(line => visibleWidth(line) <= width));
});

test('clipped preview retains its bottom edge without forwarding hidden-row clicks', () => {
    let clicks = 0;
    const tool = make({ renderCall: () => ({
        render: () => Array.from({ length: 30 }, (_, i) => `detail ${i}`), invalidate() {},
        handleMouse() { clicks++; return { handled: true }; },
    }) });
    const container = new Container(); container.addChild(tool);
    click(container, 1, 0); // Open group.
    click(container, 1, 1); // Open local compact preview.
    const rows = plain(container), footer = rows.findIndex(line => /╰─+╯$/.test(line));
    assert.equal(footer, 13, 'summary + call row + twelve preview rows including both borders');
    assert.match(rows[footer + 1]!, /Ctrl\+O/);
    click(container, 4, footer); click(container, 4, footer + 1);
    assert.equal(clicks, 0);
    click(container, 4, 3);
    assert.equal(clicks, 1);
    tool.setExpanded(true);
    assert.match(plain(container).join('\n'), /detail 29/);
    assert.doesNotMatch(plain(container).join('\n'), /Ctrl\+O 查看完整输出/);
});

test('built-in edit self shell frames native diff, error and narrow layouts without background', () => {
    const tool = make(createEditToolDefinition(process.cwd()), 'edit');
    const diff = Array.from({ length: 30 }, (_, i) => `+${i + 1} new 中文🙂 line`).join('\n');
    for (const phase of ['pending', 'partial', 'done', 'error']) {
        if (phase !== 'pending') tool.updateResult({
            content: [{ type: 'text', text: phase === 'error' ? 'edit failed' : 'updated' }],
            details: phase === 'error' ? {} : { diff, firstChangedLine: 1 }, isError: phase === 'error',
        }, phase === 'partial');
        for (let width = 0; width <= 60; width++) {
            const lines = tool.render(width), text = lines.map(stripTerminalSequences);
            assert.ok(lines.every(line => visibleWidth(line) <= width), `${phase} width ${width}`);
            assert.doesNotMatch(lines.join('\n'), /\x1b\[48;/);
            if (width >= 3) {
                assert.match(text[0]!, /^╭─.*╮$/);
                assert.match(text.at(-1)!, /^╰─*╯$/);
                assert.equal(text.filter(line => /^╭─.*╮$/.test(line)).length, 1);
            }
        }
    }
    assert.match(plain(tool, 80).join('\n'), /edit failed/);
    tool.updateResult({ content: [{ type: 'text', text: 'updated' }], details: { diff }, isError: false });
    assert.match(tool.render(80).join('\n'), /\x1b\[/, 'native diff retains ANSI styling');
    const rows = plain(tool, 80);
    click(tool, 0, 0, 80);
    assert.equal((tool as any).expanded, false, 'frame edge does not toggle result');
    click(tool, 4, rows.findIndex(line => line.includes('new 中文')), 80);
    assert.equal((tool as any).expanded, true, 'diff body retains native mouse toggle');
    tool.setExpanded(false);
    const container = new Container(); container.addChild(tool);
    click(container, 1, 0); click(container, 1, 1);
    const preview = plain(container);
    assert.match(preview[13]!, /╰─+╯$/);
    assert.match(preview[14]!, /Ctrl\+O/);
    tool.setExpanded(true);
    assert.match(plain(container, 80).join('\n'), /30 new 中文/);
});

test('tool shells shrink to styled content and title while retaining native layout and hit testing', () => {
    for (const worker of [false, true]) for (const padding of [0, 1, 3]) {
        const events: TuiMouseEvent[] = [];
        const widths: number[] = [];
        const tool = make({ ...(worker ? { renderShell: 'self' } : {}), renderCall: () => ({
            render(width: number) { widths.push(width); return ['\x1b[31m中文🙂 short\x1b[0m']; },
            invalidate() {}, handleMouse(event: TuiMouseEvent) { events.push(event); return { handled: true }; },
        }) }, worker ? 'worker' : 'short', padding);
        const rows = plain(tool, 80);
        const frameWidth = visibleWidth(rows[0]!);
        assert.ok(frameWidth < 80);
        assert.ok(rows.every(row => visibleWidth(row) === frameWidth));
        assert.equal(rows[1], '│' + ' '.repeat(padding) + '中文🙂 short' + ' '.repeat(padding) + '│');
        assert.ok(tool.render(80)[1]!.includes('\x1b[31m'));
        click(tool, padding + 1, 1, 80);
        assert.equal(events[0]!.x, 0);
        assert.equal(events[0]!.width, 80 - 2 * (padding + 1));
        click(tool, frameWidth - 1, 1, 80);
        click(tool, frameWidth + 2, 1, 80);
        assert.equal(events.length, 1);
        assert.ok(widths.every(width => width === 80 - 2 * (padding + 1)));
        tool.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false });
        const completed = plain(tool, 80);
        assert.match(completed[0]!, /≈\d+ tok/);
        assert.ok(completed.every(row => visibleWidth(row) === visibleWidth(completed[0]!)));
    }
    const long = make({ renderCall: () => new Text('中文🙂'.repeat(50), 0, 0) });
    const rows = plain(long, 40);
    assert.equal(visibleWidth(rows[0]!), 40);
    assert.equal(rows.slice(1, -1).map(row => row.slice(1, -1).trim()).join(''), '中文🙂'.repeat(50));
});

test('tool frames use subdued blue gray without changing assistant or user borders', () => {
    const gray = '\x1b[38;2;113;137;159m';
    const tool = make();
    const rows = tool.render(40);
    assert.ok(rows[0]!.startsWith(gray + '╭'));
    assert.ok(rows[1]!.startsWith(gray + '│'));
    assert.ok(rows.at(-1)!.startsWith(gray + '╰'));
    assert.ok(frameBorder('│', 'left').startsWith('\x1b[38;2;113;175;230m'));
    assert.ok(frameBorder('│', 'left', 'user').startsWith('\x1b[38;2;98;204;170m'));
});

test('expanded Worker frame aligns with its summary arrow and preserves inner padding and mouse layout', () => {
    for (const outputPad of [0, 1, 3]) {
        const events: TuiMouseEvent[] = [];
        const tool = make({ renderShell: 'self', renderCall: () => ({
            render: () => ['hit target'], invalidate() {},
            handleMouse(event: TuiMouseEvent) { events.push(event); return { handled: true }; },
        }) }, 'worker', outputPad);
        const container = new Container(); container.addChild(tool);
        tool.setExpanded(true);
        const width = 60;
        const rows = plain(container, width);
        const summary = rows.find(line => /[├╰] ⌄/.test(line))!;
        const frame = rows.find(line => line.includes('╭'))!;
        assert.equal(frame.indexOf('╭'), summary.indexOf('⌄'));
        const bodyRow = rows.findIndex(line => line.includes('hit target'));
        const contentX = rows[bodyRow]!.indexOf('hit target');
        assert.equal(contentX - frame.indexOf('╭'), outputPad + 1);
        click(container, contentX + 1, bodyRow, width);
        assert.equal(events[0]!.x, 1);
        assert.equal(events[0]!.y, 0);
        assert.equal(events[0]!.width, width - 2 - 2 * (outputPad + 1));
        click(container, 0, bodyRow, width);
        click(container, 2, bodyRow, width);
        assert.equal(events.length, 1, 'outer gutter and frame edge do not reach content');
        for (let narrow = 0; narrow <= 16; narrow++) {
            assert.ok(container.render(narrow).every(line => visibleWidth(line) <= narrow));
        }
    }
});

test('native image rows stay outside the shell without losing control sequences or reserve rows', () => {
    const tool = make({ renderCall: () => new Text('call', 0, 0) });
    const image = ['\x1b_Ga=T,f=100;image\x1b\\', ' '.repeat(40), ' '.repeat(40)];
    tool.addChild({ render: () => image, invalidate() {} });
    const lines = tool.render(40);
    assert.deepEqual(lines.slice(-3), image);
    assert.match(stripTerminalSequences(lines.at(-4)!), /^╰─*╯$/);
});
