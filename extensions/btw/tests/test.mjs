import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { join } from 'node:path';
const { default: register, BtwPopup, snapshotContext } = await import(process.env.BTW_MODULE);
const require = createRequire(join(process.env.BTW_SDK, 'package.json'));
const { visibleWidth } = await import(require.resolve('@earendil-works/pi-tui'));
const themes = await import(new URL('file://' + join(process.env.BTW_SDK, 'dist/modes/interactive/theme/theme.js')));
themes.initTheme('dark', false);
const theme = themes.theme;
const message = (id, parentId, text) => ({ id, parentId, type: 'message', timestamp: new Date().toISOString(), message: { role: 'user', content: text, timestamp: 1 } });
const entries = [message('a', null, 'parent'), message('b', 'a', 'wrong branch'), message('c', 'a', 'current branch')];
let request, signal, done = 0, renders = 0;
let fail = false;
const answer = { role: 'assistant', content: [{ type: 'text', text: '回答中文🙂' }], stopReason: 'stop', timestamp: 2 };
const ctx = {
  model: { id: 'test', provider: 'test' },
  sessionManager: { getEntries: () => entries, getLeafId: () => 'c' },
  getSystemPrompt: () => 'Parent system prompt',
  modelRegistry: { streamSimple(_model, context, options) {
    request = context; signal = options.signal;
    return {
      async *[Symbol.asyncIterator]() {
        if (fail) throw new Error('offline');
        yield { type: 'text_delta', delta: '回答中文🙂' };
      },
      result: async () => answer,
    };
  } },
};
const tui = { terminal: { rows: 30 }, requestRender: () => renders++ };
const original = JSON.stringify(entries);
const snapshot = snapshotContext(ctx);
assert.deepEqual(snapshot.messages.map(m => m.content), ['parent', 'current branch']);
assert.match(snapshot.systemPrompt, /No tools are available/);
snapshot.messages[0].content = 'not shared';
assert.equal(JSON.stringify(entries), original);

const edited = [...entries, { id: 'd', parentId: 'c', type: 'context_edit', targetId: 'a', replacement: null, timestamp: new Date().toISOString() }];
const editedContext = snapshotContext({ ...ctx, sessionManager: { getEntries: () => edited, getLeafId: () => 'd' } });
assert.deepEqual(editedContext.messages.map(m => m.content), ['current branch']);
const compacted = [...entries, { id: 'compact', parentId: 'c', type: 'compaction', summary: 'old conversation summary', firstKeptEntryId: 'c', tokensBefore: 100, timestamp: new Date().toISOString() }];
const compactContext = snapshotContext({ ...ctx, sessionManager: { getEntries: () => compacted, getLeafId: () => 'compact' } });
assert(!compactContext.messages.some(m => m.content === 'parent'));
assert(JSON.stringify(compactContext.messages).includes('old conversation summary'));

const popup = new BtwPopup(tui, theme, ctx, () => done++);
popup.focused = true;
assert.equal(popup.focused, true);
await popup.ask('question');
assert.equal(request.tools, undefined);
assert.equal(request.messages.at(-1).content, 'question');
assert.equal(signal.aborted, false);
await popup.ask('follow up');
assert.equal(request.messages.length, 5);
assert.equal(request.messages.at(-2).role, 'assistant');
for (const width of [1, 4, 20, 80, 120]) {
  const lines = popup.render(width);
  assert(lines.length <= 24);
  assert(lines.every(line => visibleWidth(line) <= width), `overflow at ${width}`);
}
fail = true;
await popup.ask('failed');
assert(popup.render(80).join('\n').includes('offline'));
fail = false;
await popup.ask('retry');
assert(!request.messages.some(m => m.content === 'failed'));
popup.handleInput('\x1b');
assert.equal(done, 1);
const rendersAfterClose = renders;
await popup.ask('ignored');
assert.equal(renders, rendersAfterClose);
assert.equal(JSON.stringify(entries), original);

// Even unsolicited tool output must never be executed or enter follow-up history.
answer.content.push({ type: 'toolCall', id: 'x', name: 'bash', arguments: { command: 'touch nope' } });
const toolPopup = new BtwPopup(tui, theme, ctx, () => {});
await toolPopup.ask('try a tool');
assert(toolPopup.render(100).join('\n').includes('未执行任何操作'));
toolPopup.dispose();
answer.content.pop();

// Closing during a blocked stream aborts immediately; late events cannot update UI.
let release;
ctx.modelRegistry.streamSimple = (_model, _context, options) => {
  signal = options.signal;
  return {
    async *[Symbol.asyncIterator]() {
      await new Promise(resolve => { release = resolve; });
      yield { type: 'text_delta', delta: 'late' };
    }, result: async () => answer,
  };
};
const pending = new BtwPopup(tui, theme, ctx, () => done++);
const task = pending.ask('pending');
pending.handleInput('\x1b');
assert(signal.aborted);
const closedRenders = renders;
release();
await task;
assert.equal(renders, closedRenders);
const fresh = new BtwPopup(tui, theme, ctx, () => {});
assert(!fresh.render(80).join('\n').includes('follow up'));
fresh.dispose();

let command, shortcut;
register({
  registerCommand(name, options) { assert.equal(name, 'btw'); command = options; },
  registerShortcut(key, options) { assert.equal(key, 'ctrl+b'); shortcut = options; },
});
let notices = 0;
await command.handler('', { hasUI: false, ui: { notify() { notices++; } } });
await command.handler('', { hasUI: true, ui: { notify() { notices++; } } });
await shortcut.handler({ hasUI: false, ui: { notify() { notices++; } } });
assert.equal(notices, 3);
let opened = 0;
await shortcut.handler({ ...ctx, hasUI: true, ui: { async custom() { opened++; } } });
assert.equal(opened, 1);
console.log('BTW checks passed: branch snapshot, isolation, follow-up, errors, widths, cancellation, fresh opening, guards.');
