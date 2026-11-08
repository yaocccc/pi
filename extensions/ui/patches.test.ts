import assert from 'node:assert/strict';
import test from 'node:test';
import { AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Box, Text, stripTerminalSequences, visibleWidth } from '@earendil-works/pi-tui';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

test('hot reload upgrades a live v9 tool wrapper and a v2 final separator without layering shells', async () => {
    const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
    const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
    initTheme('dark', false);
    const nativeBoxRender = Box.prototype.render;
    const nativeTextRender = Text.prototype.render;
    const nativeUpdateDisplay = (ToolExecutionComponent.prototype as any).updateDisplay;

    // Model the previous module's installed closures and symbols on the real Pi
    // prototypes. They keep their own activity flag after the new module loads.
    let legacyActivity = false;
    const oldToolRender = ToolExecutionComponent.prototype.render;
    ToolExecutionComponent.prototype.render = function legacyV9Render(width: number) {
        legacyActivity = true;
        const lines = oldToolRender.call(this, width);
        return lines.length && !stripTerminalSequences(lines[0]!).trim() ? lines.slice(1) : lines;
    };
    (ToolExecutionComponent.prototype as any)[Symbol.for('pi.extensions.ui.compact-tool-display.v9')] = true;
    const oldFinalRender = AssistantMessageComponent.prototype.render;
    AssistantMessageComponent.prototype.render = function legacyV2Render(width: number) {
        const lines = oldFinalRender.call(this, width);
        if (!(this as any).lastMessage?.content.some((part: any) => part.type === 'text' && part.text.trim())) return lines;
        const insert = legacyActivity && lines.length > 0 && !!stripTerminalSequences(lines[0]!).trim();
        legacyActivity = false;
        return insert ? [' '.repeat(width), ...lines] : lines;
    };
    (AssistantMessageComponent.prototype as any)[Symbol.for('pi.extensions.ui.final-response-separator.v2')] = true;

    const loadReloadedPatches = (suffix: string): Promise<typeof import('./patches.ts')> => import(`./patches.ts?${suffix}`);
    const patches = await loadReloadedPatches('reload-v10');
    patches.patchToolDisplay();
    patches.patchFinalResponseSeparator();
    patches.patchUserMessageFrame();
    const firstToolRender = ToolExecutionComponent.prototype.render;
    const firstFinalRender = AssistantMessageComponent.prototype.render;
    // Repeated registration and a second module import must not stack wrappers.
    patches.patchToolDisplay(); patches.patchFinalResponseSeparator();
    const reload = await loadReloadedPatches('reload-v10-again');
    reload.patchToolDisplay(); reload.patchFinalResponseSeparator();
    assert.equal(ToolExecutionComponent.prototype.render, firstToolRender);
    assert.equal(AssistantMessageComponent.prototype.render, firstFinalRender);
    assert.equal((ToolExecutionComponent.prototype as any).updateDisplay, nativeUpdateDisplay);
    assert.equal(Box.prototype.render, nativeBoxRender);
    assert.equal(Text.prototype.render, nativeTextRender);

    const ui = { requestRender() {} };
    const make = (name: string, id: string, args: object, definition: object) =>
        new ToolExecutionComponent(name, id, args, {}, definition as any, ui as any, process.cwd());
    const empty = () => ({ render: () => [], invalidate() {} });
    const self = { renderShell: 'self', renderCall: (args: { hidden?: boolean }) =>
        args.hidden ? empty() : new Text('Worker 活动', 0, 0), renderResult: empty };
    const worker = make('worker', 'origin', {}, self);
    const hidden = make('worker', 'control', { hidden: true }, self);
    const ordinary = make('ordinary', 'normal', {}, { renderCall: () => new Text('Ordinary', 0, 0) });
    const read = make('read', 'read', { path: 'example.ts' }, { renderCall: () => new Text('Read', 0, 0) });
    const frameEdges = (lines: string[], width = 40) => {
        assert.doesNotMatch(lines.join('\n'), /[▄▀]|\x1b\[48;/);
        assert.ok(lines.every(line => visibleWidth(line) <= width));
        if (width < 3) return 0;
        assert.match(stripTerminalSequences(lines[0]!), /^╭─.*╮$/);
        assert.match(stripTerminalSequences(lines.at(-1)!), /^╰─*╯$/);
        return lines.filter(line => /^(?:╭─.*╮|╰─*╯)$/.test(stripTerminalSequences(line))).length;
    };
    const final = () => {
        const message = new AssistantMessageComponent({ role: 'assistant', content: [{ type: 'text', text: '最终回复' }] } as any);
        // Pi normally inserts a leading spacer; remove it to make the activity
        // separator observable rather than masked by an existing blank line.
        (message as any).contentContainer.children.shift();
        return message.render(40);
    };

    assert.deepEqual(hidden.render(40), []);
    assert.equal(legacyActivity, false, 'hidden controls never enter the old v9 wrapper');
    worker.markExecutionStarted();
    for (const width of [1, 2, 3, 40]) {
        const drawn = worker.render(width);
        assert.ok(drawn.length > 2, 'worker remains visible at narrow widths');
        if (width === 40) assert.match(stripTerminalSequences(drawn.join('\n')), /Worker 活动/);
        frameEdges(drawn, width);
        const innerWidth = width - 2 * Math.min(2, Math.max(0, Math.floor((width - 1) / 2)));
        assert.equal(drawn.length, (worker as any).selfRenderContainer.render(innerWidth).length + (width >= 3 ? 2 : 0), 'visible worker retains one border shell');
    }
    worker.updateResult({ content: [{ type: 'text', text: 'working' }], isError: false }, true);
    assert.equal(frameEdges(worker.render(40)), 2, 'partial worker has no nested shell');
    worker.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false }, false);
    assert.equal(frameEdges(worker.render(40)), 2, 'finished worker has no nested shell');
    assert.equal(legacyActivity, false, 'old v9 closure was not entered for visible worker either');
    assert.equal(final().length, 2, 'v10 activity inserts the final gap even when the old v2 closure is inactive');
    assert.equal(final().length, 1, 'final gap is consumed once');

    hidden.updateResult({ content: [{ type: 'text', text: 'hidden' }], isError: false }, false);
    hidden.setExpanded(true);
    assert.deepEqual(hidden.render(40), []);
    assert.equal(final().length, 1, 'zero-line continuation never creates a final gap');
    const ordinaryLines = ordinary.render(40);
    assert.ok(frameEdges(ordinaryLines) > 0, 'ordinary tools have one frame');
    assert.equal(final().length, 2, 'old v2 and new v3 do not duplicate an ordinary-tool separator');
    assert.equal(final().length, 1);
    assert.ok(frameEdges(read.render(40)) > 0, 'read has one frame');
    assert.equal(final().length, 2, 'read also shares the final separator');

    worker.render(40);
    new UserMessageComponent('下一个问题').render(40);
    assert.equal(final().length, 1, 'user input resets the shared reload-safe activity state');

    const { patchToolContext } = await import('./tool-context.ts');
    patchToolContext();
    for (const tool of [worker, ordinary, read]) {
        tool.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false }, false);
        const lines = tool.render(40);
        assert.match(stripTerminalSequences(lines.join('\n')), /≈\d+ tok/);
        assert.ok(frameEdges(lines) > 0, 'token badge preserves the tool frame');
        tool.setExpanded(true);
        assert.equal((stripTerminalSequences(tool.render(40).join('\n')).match(/≈\d+ tok/g) ?? []).length, 1);
    }
    assert.deepEqual(hidden.render(40), [], 'token badge does not reveal hidden worker controls');
});

test('formerly compact tool names retain native multiline calls, results and self-render visibility', async () => {
    const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
    const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
    initTheme('dark', false);
    const { patchToolDisplay } = await import('./patches.ts');
    patchToolDisplay();
    const ui = { requestRender() {} };
    const draw = (tool: ToolExecutionComponent) => tool.render(100).map(stripTerminalSequences).join('\n');
    for (const name of ['read', 'find', 'grep', 'ctx_lookup', 'ff_lookup']) {
        const tool = new ToolExecutionComponent(name, name, { path: 'example.ts' }, {}, {
            renderCall: () => new Text(`${name} native call\nsecond argument line`, 0, 0),
            renderResult: (_result: unknown, options: { expanded: boolean }) =>
                new Text(`${options.expanded ? 'full' : 'preview'} result\nsecond result line`, 0, 0),
        } as any, ui as any, process.cwd());
        assert.match(draw(tool), /native call[\s\S]*second argument line/, name);
        for (const partial of [true, false]) {
            tool.updateResult({ content: [{ type: 'text', text: 'result' }], isError: false }, partial);
            assert.match(draw(tool), /second argument line[\s\S]*preview result[\s\S]*second result line/, name);
        }
        tool.setExpanded(true);
        assert.match(draw(tool), /full result[\s\S]*second result line/, name);
        tool.setExpanded(false);
        tool.updateResult({ content: [{ type: 'text', text: 'error' }], isError: true });
        assert.match(draw(tool), /preview result[\s\S]*second result line/, name);
    }
    const empty = () => ({ render: () => [], invalidate() {} });
    const hidden = new ToolExecutionComponent('ctx_hidden', 'hidden', {}, {}, {
        renderShell: 'self', renderCall: empty, renderResult: empty,
    } as any, ui as any, process.cwd());
    assert.deepEqual(hidden.render(100), [], 'a tool name must not turn an empty native renderer into a visible card');
});
