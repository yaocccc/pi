import assert from 'node:assert/strict';
import test from 'node:test';
import { AssistantMessageComponent, ToolExecutionComponent, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Text, stripTerminalSequences } from '@earendil-works/pi-tui';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

test('hot reload upgrades a live v9 tool wrapper and a v2 final separator without layering shells', async () => {
    const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
    const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
    initTheme('dark', false);

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
    patches.patchCompactToolDisplay();
    patches.patchFinalResponseSeparator();
    patches.patchPaddedBackgroundHalfBlocks();
    patches.patchUserMessageHalfBlocks();
    const firstToolRender = ToolExecutionComponent.prototype.render;
    const firstFinalRender = AssistantMessageComponent.prototype.render;
    const firstUpdateDisplay = (ToolExecutionComponent.prototype as any).updateDisplay;
    // Repeated registration and a second module import must not stack wrappers.
    patches.patchCompactToolDisplay(); patches.patchFinalResponseSeparator(); patches.patchPaddedBackgroundHalfBlocks();
    const reload = await loadReloadedPatches('reload-v10-again');
    reload.patchCompactToolDisplay(); reload.patchFinalResponseSeparator(); reload.patchPaddedBackgroundHalfBlocks();
    assert.equal(ToolExecutionComponent.prototype.render, firstToolRender);
    assert.equal(AssistantMessageComponent.prototype.render, firstFinalRender);
    assert.equal((ToolExecutionComponent.prototype as any).updateDisplay, firstUpdateDisplay);

    const ui = { requestRender() {} };
    const make = (name: string, id: string, args: object, definition: object) =>
        new ToolExecutionComponent(name, id, args, {}, definition as any, ui as any, process.cwd());
    const empty = () => ({ render: () => [], invalidate() {} });
    const self = { renderShell: 'self', renderCall: (args: { hidden?: boolean }) =>
        args.hidden ? empty() : new Text('Worker 活动', 0, 0), renderResult: empty };
    const worker = make('worker', 'origin', {}, self);
    const hidden = make('worker', 'control', { hidden: true }, self);
    const ordinary = make('ordinary', 'normal', {}, { renderCall: () => new Text('Ordinary', 0, 0) });
    const compact = make('read', 'compact', { path: 'example.ts' }, { renderCall: () => new Text('Read', 0, 0) });
    const boundaries = (lines: string[]) => lines.map(stripTerminalSequences).filter((line) => /^[▄▀]+$/.test(line));
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
        assert.equal(boundaries(drawn).length, 2, 'visible worker has exactly one shell');
    }
    worker.updateResult({ content: [{ type: 'text', text: 'working' }], isError: false }, true);
    assert.equal(boundaries(worker.render(40)).length, 2, 'partial worker has no nested shell');
    worker.updateResult({ content: [{ type: 'text', text: 'done' }], isError: false }, false);
    assert.equal(boundaries(worker.render(40)).length, 2, 'finished worker has no nested shell');
    assert.equal(legacyActivity, false, 'old v9 closure was not entered for visible worker either');
    assert.equal(final().length, 2, 'v10 activity inserts the final gap even when the old v2 closure is inactive');
    assert.equal(final().length, 1, 'final gap is consumed once');

    hidden.updateResult({ content: [{ type: 'text', text: 'hidden' }], isError: false }, false);
    hidden.setExpanded(true);
    assert.deepEqual(hidden.render(40), []);
    assert.equal(final().length, 1, 'zero-line continuation never creates a final gap');
    const ordinaryLines = ordinary.render(40);
    assert.equal(boundaries(ordinaryLines).length, 2, 'ordinary tools keep a single original shell');
    assert.equal(final().length, 2, 'old v2 and new v3 do not duplicate an ordinary-tool separator');
    assert.equal(final().length, 1);
    assert.equal(boundaries(compact.render(40)).length, 2, 'compact tools bypass the old v9 shell');
    assert.equal(final().length, 2, 'compact tools also share the final separator');

    worker.render(40);
    new UserMessageComponent('下一个问题').render(40);
    assert.equal(final().length, 1, 'user input resets the shared reload-safe activity state');
});
