import assert from 'node:assert/strict';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import test from 'node:test';
import { createReadToolDefinition, ToolExecutionComponent } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences } from '@earendil-works/pi-tui';
import { patchToolDisplay } from './patches.ts';
import { patchToolFolding } from './folding.ts';
import { patchToolContext } from './tool-context.ts';
import { RenderCache, renderCacheBudget, withRenderCachePass } from './render-cache.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
initTheme('dark', false);
patchToolDisplay();
patchToolFolding();
patchToolContext();
const tui = { requestRender() {} } as any;
const plain = (lines: string[]) => lines.map(stripTerminalSequences).join('\n');

function fallback() {
    const args = { text: 'input' };
    const result = { content: [{ type: 'text' as const, text: 'output '.repeat(60) }], isError: false };
    const tool = new ToolExecutionComponent('custom', 'snapshot', args, { showImages: false }, undefined, tui, process.cwd());
    tool.updateResult(result);
    tool.setExpanded(true);
    const inner = (tool as any).contentText;
    const render = inner.render;
    let calls = 0;
    inner.render = function (width: number) { calls++; return render.call(this, width); };
    return { tool, args, result, calls: () => calls };
}

test('completed fallback snapshots reuse rendering without exposing cached row arrays', () => {
    const { tool, calls } = fallback();
    const first = tool.render(80);
    const expected = [...first];
    first[1] = 'caller mutation';
    assert.deepEqual(tool.render(80), expected);
    assert.equal(calls(), 1);
    tool.render(60);
    assert.equal(calls(), 2, 'width changes rebuild native layout');
    tool.invalidate();
    tool.render(60);
    assert.equal(calls(), 3, 'native invalidation rebuilds the snapshot');
});

test('same-object result/args updates and partial output invalidate completed snapshots', () => {
    const { tool, args, result, calls } = fallback();
    tool.render(80);
    result.content[0]!.text = 'changed result';
    tool.updateResult(result);
    assert.match(plain(tool.render(80)), /changed result/);
    assert.equal(calls(), 2);
    args.text = 'changed arguments';
    tool.updateArgs(args);
    assert.match(plain(tool.render(80)), /changed arguments/);
    assert.equal(calls(), 3);
    tool.updateResult(result, true);
    tool.render(80);
    tool.render(80);
    assert.equal(calls(), 5, 'running/partial tools remain live');
});

test('known native read renderers reuse output; custom renderers remain live without updates', () => {
    const native = new ToolExecutionComponent('read', 'native', { path: 'file.txt' }, { showImages: false },
        createReadToolDefinition(process.cwd()), tui, process.cwd());
    native.updateResult({ content: [{ type: 'text', text: 'file text' }], isError: false });
    native.setExpanded(true);
    const box = (native as any).contentBox;
    const original = box.render;
    let calls = 0;
    box.render = function (width: number) { calls++; return original.call(this, width); };
    native.render(80);
    native.render(80);
    assert.equal(calls, 1);

    let text = 'live first';
    const renderer = { render: () => [text], invalidate() {} };
    const custom = new ToolExecutionComponent('read', 'custom', {}, { showImages: false }, {
        name: 'read', label: 'read', renderCall: () => renderer, renderResult: () => renderer,
    } as any, tui, process.cwd());
    custom.updateResult({ content: [{ type: 'text', text: 'complete' }], isError: false });
    custom.setExpanded(true);
    assert.match(plain(custom.render(80)), /live first/);
    text = 'live second';
    assert.match(plain(custom.render(80)), /live second/);
});

test('image-bearing native layouts bypass complete snapshots', () => {
    const { tool, calls } = fallback();
    // Exercise the native image-layout guard without requiring terminal image support.
    (tool as any).imageComponents = [{}];
    tool.render(80);
    tool.render(80);
    assert.equal(calls(), 2);
});

test('collapse releases expanded snapshots and later expansion rebuilds correctly', () => {
    renderCacheBudget.clear();
    const { tool, calls } = fallback();
    const expected = tool.render(80);
    assert.ok(renderCacheBudget.usedBytes > 0);
    tool.setExpanded(false);
    assert.equal(renderCacheBudget.usedBytes, 0);
    tool.setExpanded(true);
    assert.deepEqual(tool.render(80), expected);
    assert.equal(calls(), 2);
});

test('budget exhaustion changes caching only, never rendered output or traversal', () => {
    renderCacheBudget.clear();
    const { tool, calls } = fallback();
    const expected = tool.render(80);
    renderCacheBudget.clear();
    const blocker = new RenderCache<string>();
    blocker.set('reserved', renderCacheBudget.maxBytes);
    try {
        for (let pass = 0; pass < 2; pass++) withRenderCachePass(() => {
            assert.equal(blocker.get(), 'reserved');
            assert.deepEqual(tool.render(80), expected);
            assert.equal(renderCacheBudget.usedBytes, renderCacheBudget.maxBytes);
        });
        assert.equal(calls(), 3, 'uncacheable tools continue rendering normally');
    } finally { renderCacheBudget.clear(); }
});
