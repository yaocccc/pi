import assert from 'node:assert/strict';
import test from 'node:test';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { AssistantMessageComponent, InteractiveMode, SessionManager, UserMessageComponent } from '@earendil-works/pi-coding-agent';
import { Container, Text, stripTerminalSequences } from '@earendil-works/pi-tui';
import { patchSessionHistory } from './history.ts';

const root = dirname(fileURLToPath(import.meta.resolve('@earendil-works/pi-coding-agent')));
const { initTheme } = await import(pathToFileURL(join(root, 'modes/interactive/theme/theme.js')).href);
initTheme('dark', false);
const nativeRender = (InteractiveMode.prototype as any).renderSessionEntries;
patchSessionHistory();
const usage = { input: 10, output: 5, cacheRead: 0, cacheWrite: 0, totalTokens: 15,
    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 } };
const user = (manager: SessionManager, text: string) => manager.appendMessage({ role: 'user', content: text, timestamp: 1 });
const assistant = (manager: SessionManager, text: string, toolId?: string) => manager.appendMessage({ role: 'assistant',
    api: 'openai-responses', provider: 'openai', model: 'test', timestamp: 2, usage,
    content: [{ type: 'text', text }, ...(toolId ? [{ type: 'toolCall' as const, id: toolId, name: 'read', arguments: { path: 'file.ts' } }] : [])],
    stopReason: toolId ? 'toolUse' : 'stop' });
const plain = (mode: any) => mode.chatContainer.render(120).map(stripTerminalSequences).join('\n');
const count = (text: string, value: string) => text.split(value).length - 1;

function fixture() {
    const manager = SessionManager.inMemory();
    const mode: any = Object.create(InteractiveMode.prototype);
    const history: string[] = [];
    const cleared: string[] = [];
    const flushed: unknown[] = [];
    Object.assign(mode, {
        isInitialized: true, chatContainer: new Container(), pendingTools: new Map(),
        entriesRenderedByBoundaryCompaction: new Set(), toolOutputExpanded: true,
        runtimeHost: { session: { sessionManager: manager, retryAttempt: 0,
            settingsManager: { getShowImages: () => false, getImageWidthCells: () => 60, getOutputPad: () => 1,
                getShowCacheMissNotices: () => false, getShowTerminalProgress: () => false } } },
        ui: { requestRender() {} }, footer: { invalidate() {} }, updateEditorBorderColor() {},
        programStatus: { handleEvent() {} },
        defaultEditor: { onEscape() {} },
        clearStatusIndicator: (kind: string) => cleared.push(kind),
        flushCompactionQueue: (options: unknown) => { flushed.push(options); return Promise.resolve(); },
        showStatus: (text: string) => mode.chatContainer.addChild(new Text(text)),
        showError: (text: string) => mode.chatContainer.addChild(new Text(text)),
        addMessageToChat(message: any, options?: { populateHistory?: boolean }) {
            if (message.role === 'user') {
                if (options?.populateHistory) history.push(message.content);
                mode.chatContainer.addChild(new UserMessageComponent(message.content));
            } else if (message.role === 'assistant') {
                mode.chatContainer.addChild(new AssistantMessageComponent(message));
            } else if (message.role === 'compactionSummary') {
                mode.chatContainer.addChild(new Text(`SUMMARY:${message.summary}`));
            } else if (message.role === 'custom' && message.display) {
                mode.chatContainer.addChild(new Text(message.content));
            }
        },
        addCustomEntryToChat() {},
        addCompactionCostNotice: () => mode.chatContainer.addChild(new Text('COMPACTION COST')),
        getRegisteredToolDefinition: () => ({ name: 'read', label: 'read',
            renderCall: () => new Text('READ file.ts', 0, 0),
            renderResult: (result: any) => new Text(result.content.map((part: any) => part.text ?? '').join('\n'), 0, 0) }),
    });
    return { manager, mode, history, cleared, flushed };
}

test('resume renders the full current branch without changing context edits or persisted entries', () => {
    const { manager, mode, history } = fixture();
    user(manager, 'EARLIEST QUESTION');
    assistant(manager, 'EARLIEST ANSWER');
    const kept = user(manager, 'MIDDLE QUESTION');
    manager.appendCompaction('FIRST SUMMARY', kept, 1000, undefined, false, usage);
    assistant(manager, 'MIDDLE ANSWER');
    const latest = user(manager, 'LATEST QUESTION');
    manager.appendCompaction('SECOND SUMMARY', latest, 2000, undefined, false, usage);
    assistant(manager, 'LATEST ANSWER');
    manager.appendContextEdit(latest, { content: 'MODEL ONLY EDIT' });
    const branch = structuredClone(manager.getBranch());
    const context = structuredClone(manager.buildContextEntries());
    const modelContext = structuredClone(manager.buildSessionContext());
    assert.ok(modelContext.messages.some(message => message.role === 'user' && message.content === 'MODEL ONLY EDIT'));
    const buildContext = manager.buildContextEntries;
    nativeRender.call(mode, manager.buildContextEntries());
    assert.doesNotMatch(plain(mode), /EARLIEST QUESTION|MIDDLE QUESTION/, 'native projection reproduces the missing history');
    mode.chatContainer.clear();
    mode.renderSessionEntries(manager.buildContextEntries(), { populateHistory: true, updateFooter: true });
    const text = plain(mode);
    assert.match(text, /EARLIEST QUESTION[\s\S]*EARLIEST ANSWER[\s\S]*MIDDLE QUESTION[\s\S]*SUMMARY:FIRST SUMMARY[\s\S]*MIDDLE ANSWER[\s\S]*LATEST QUESTION[\s\S]*SUMMARY:SECOND SUMMARY[\s\S]*LATEST ANSWER/);
    assert.equal(count(text, 'COMPACTION COST'), 2);
    assert.doesNotMatch(text, /MODEL ONLY EDIT/);
    assert.deepEqual(history, ['EARLIEST QUESTION', 'MIDDLE QUESTION', 'LATEST QUESTION']);
    assert.deepEqual(manager.getBranch(), branch);
    assert.deepEqual(manager.buildContextEntries(), context);
    assert.deepEqual(manager.buildSessionContext(), modelContext);
    assert.equal(manager.buildContextEntries, buildContext);
    const patched = (InteractiveMode.prototype as any).renderSessionEntries;
    patchSessionHistory();
    assert.equal((InteractiveMode.prototype as any).renderSessionEntries, patched, 'reload does not stack wrappers');
    mode.rebuildChatFromMessages();
    assert.equal(plain(mode), text, 'rebuild neither duplicates history nor drops old messages');
});

test('successive manual and automatic compactions keep every round and native cleanup', async () => {
    const { manager, mode, cleared, flushed } = fixture();
    user(manager, 'FIRST ROUND');
    assistant(manager, 'FIRST RESPONSE');
    for (let index = 0; index < 6; index++) {
        const kept = user(manager, `QUESTION-${index}`);
        assistant(manager, `ANSWER-${index}`);
        const summary = `COMPACT-${index}`;
        manager.appendCompaction(summary, kept, 1000, undefined, false, usage);
        const escape = () => {};
        mode.autoCompactionEscapeHandler = escape;
        const branch = structuredClone(manager.getBranch());
        const context = structuredClone(manager.buildContextEntries());
        const result = mode.handleEvent({ type: 'compaction_end', reason: index % 2 ? 'manual' : 'threshold',
            aborted: false, willRetry: false, result: { summary, firstKeptEntryId: kept, tokensBefore: 1000, usage } });
        assert.match(plain(mode), /FIRST ROUND[\s\S]*FIRST RESPONSE/, 'restoration occurs before the async handler yields');
        await result;
        const text = plain(mode);
        for (let round = 0; round <= index; round++) {
            assert.equal(count(text, `QUESTION-${round}`), 1);
            assert.equal(count(text, `ANSWER-${round}`), 1);
            assert.equal(count(text, `SUMMARY:COMPACT-${round}`), 1);
        }
        assert.equal(count(text, 'COMPACTION COST'), index + 1);
        assert.equal(mode.defaultEditor.onEscape, escape);
        assert.equal(mode.autoCompactionEscapeHandler, undefined);
        assert.deepEqual(manager.getBranch(), branch);
        assert.deepEqual(manager.buildContextEntries(), context);
    }
    assert.deepEqual(cleared, Array(6).fill('compaction'));
    assert.deepEqual(flushed, Array(6).fill({ willRetry: false }));
});

test('online boundary compaction matches tools across the boundary and deduplicates appended entries', async () => {
    const { manager, mode } = fixture();
    user(manager, 'BEFORE BOUNDARY');
    const call = assistant(manager, 'CALL BEFORE BOUNDARY', 'across-boundary');
    const compactionId = manager.appendCompaction('ONLINE SUMMARY', call, 1000, undefined, true, usage);
    manager.appendMessage({ role: 'toolResult', toolCallId: 'across-boundary', toolName: 'read',
        content: [{ type: 'text', text: 'TOOL RESULT AFTER BOUNDARY' }], isError: false, timestamp: 3 });
    manager.appendCustomMessageEntry('progress', 'VISIBLE CUSTOM MESSAGE', true);
    manager.appendCustomMessageEntry('hidden', 'HIDDEN CUSTOM MESSAGE', false);
    assistant(manager, 'AFTER BOUNDARY');
    const branch = structuredClone(manager.getBranch());
    const after = manager.getBranch().slice(manager.getBranch().findIndex(entry => entry.id === compactionId) + 1);
    await mode.handleEvent({ type: 'entry_appended', entry: manager.getEntry(compactionId) });
    const text = plain(mode);
    assert.match(text, /BEFORE BOUNDARY[\s\S]*CALL BEFORE BOUNDARY[\s\S]*TOOL RESULT AFTER BOUNDARY[\s\S]*SUMMARY:ONLINE SUMMARY[\s\S]*VISIBLE CUSTOM MESSAGE[\s\S]*AFTER BOUNDARY/);
    assert.doesNotMatch(text, /HIDDEN CUSTOM MESSAGE/);
    assert.equal(mode.pendingTools.size, 0, 'result resolves the call despite the compaction entry between them');
    assert.equal(count(text, 'SUMMARY:ONLINE SUMMARY'), 1);
    assert.equal(count(text, 'COMPACTION COST'), 1);
    for (const entry of after) await mode.handleEvent({ type: 'entry_appended', entry });
    assert.equal(plain(mode), text, 'native dedup set still prevents duplicate custom messages');
    assert.equal(mode.entriesRenderedByBoundaryCompaction.size, 0);
    assert.deepEqual(manager.getBranch(), branch);
});

test('compaction events awaiting initialization restore history after the native event finishes', async () => {
    const { manager, mode } = fixture();
    user(manager, 'BEFORE INITIALIZATION');
    assistant(manager, 'EARLY RESPONSE');
    const kept = user(manager, 'KEPT QUESTION');
    manager.appendCompaction('INIT SUMMARY', kept, 1000);
    mode.isInitialized = false;
    mode.init = async () => { await Promise.resolve(); mode.isInitialized = true; };
    await mode.handleEvent({ type: 'compaction_end', reason: 'manual', aborted: false, willRetry: false,
        result: { summary: 'INIT SUMMARY', firstKeptEntryId: kept, tokensBefore: 1000 } });
    assert.match(plain(mode), /BEFORE INITIALIZATION[\s\S]*EARLY RESPONSE[\s\S]*KEPT QUESTION[\s\S]*SUMMARY:INIT SUMMARY/);
});

test('branch changes and new sessions never show messages from another branch', () => {
    const { manager, mode } = fixture();
    const forkPoint = user(manager, 'SHARED ROOT');
    assistant(manager, 'MAIN BRANCH ONLY');
    const kept = user(manager, 'MAIN LATEST');
    const mainLeaf = manager.appendCompaction('MAIN SUMMARY', kept, 1000);
    mode.rebuildChatFromMessages();
    assert.match(plain(mode), /MAIN BRANCH ONLY/);
    manager.branch(forkPoint);
    user(manager, 'OTHER BRANCH ONLY');
    mode.rebuildChatFromMessages();
    assert.match(plain(mode), /SHARED ROOT[\s\S]*OTHER BRANCH ONLY/);
    assert.doesNotMatch(plain(mode), /MAIN BRANCH ONLY|MAIN LATEST|MAIN SUMMARY/);
    manager.branch(mainLeaf);
    mode.rebuildChatFromMessages();
    assert.match(plain(mode), /SHARED ROOT[\s\S]*MAIN BRANCH ONLY[\s\S]*MAIN LATEST/);
    assert.doesNotMatch(plain(mode), /OTHER BRANCH ONLY/);
    mode.session.sessionManager = SessionManager.inMemory();
    user(mode.sessionManager, 'NEW SESSION ONLY');
    mode.rebuildChatFromMessages();
    assert.match(plain(mode), /NEW SESSION ONLY/);
    assert.doesNotMatch(plain(mode), /SHARED ROOT|MAIN BRANCH ONLY|OTHER BRANCH ONLY/);
});

test('failed, aborted, stale and already-rendered compactions do not replace existing components', async () => {
    const { manager, mode } = fixture();
    const kept = user(manager, 'UNCHANGED HISTORY');
    const old = manager.appendCompaction('OLD SUMMARY', kept, 1000);
    const latest = manager.appendCompaction('CURRENT SUMMARY', kept, 2000);
    mode.rebuildChatFromMessages();
    const children = [...mode.chatContainer.children];
    await mode.handleEvent({ type: 'compaction_end', reason: 'manual', aborted: true, willRetry: false });
    await mode.handleEvent({ type: 'compaction_end', reason: 'manual', errorMessage: 'COMPACTION FAILED', aborted: false, willRetry: false });
    assert.deepEqual(mode.chatContainer.children.slice(0, children.length), children);
    assert.match(plain(mode), /Compaction cancelled[\s\S]*COMPACTION FAILED/);
    const text = plain(mode);
    await mode.handleEvent({ type: 'entry_appended', entry: manager.getEntry(old) });
    mode.entriesRenderedByBoundaryCompaction.add(latest);
    await mode.handleEvent({ type: 'entry_appended', entry: manager.getEntry(latest) });
    assert.equal(plain(mode), text);
    assert.equal(mode.entriesRenderedByBoundaryCompaction.size, 0);
});
