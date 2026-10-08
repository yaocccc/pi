import assert from 'node:assert/strict';
import test, { type TestContext } from 'node:test';
import type { ExtensionAPI, ExtensionContext } from '@earendil-works/pi-coding-agent';
import ui from './index.ts';
import type { NoCostFooter } from './footer.ts';
import { USAGE_TICK_MS } from './codex-usage.ts';
import { WORKER_USAGE_EVENT } from '../worker/events.ts';
import { getWorkingMessageLine, setWorkingMessageActive, WORKING_FRAME_INTERVAL_MS } from './working-message.ts';

function setup(t: TestContext) {
    const handlers = new Map<string, (event: any, ctx: ExtensionContext) => void>();
    const bus = new Map<string, (data: unknown) => void>();
    const intervals = new Map<NodeJS.Timeout, () => void>();
    const calls: string[] = [];
    let now = 10_000;
    let nextId = 0;
    t.mock.method(Date, 'now', () => now);
    t.mock.method(globalThis, 'fetch', () => { throw new Error('network is forbidden in lifecycle tests'); });
    t.mock.method(globalThis, 'setInterval', (callback: () => void, delay: number) => {
        assert.equal(delay, WORKING_FRAME_INTERVAL_MS);
        const handle = { id: ++nextId } as unknown as NodeJS.Timeout;
        intervals.set(handle, callback);
        calls.push('schedule');
        return handle;
    });
    t.mock.method(globalThis, 'clearInterval', (handle: NodeJS.Timeout) => {
        assert.equal(intervals.delete(handle), true, 'only live timers may be cleared');
        calls.push('clear');
    });
    setWorkingMessageActive(false);
    const ctx = {
        ui: {
            theme: { fg: (_color: string, text: string) => text },
            setWorkingVisible: (visible: boolean) => {
                assert.equal(visible, false);
                calls.push('refresh');
            },
            setHeader() {},
            setEditorComponent() {},
            setFooter() {},
        },
    } as unknown as ExtensionContext;
    ui({
        on: (name: string, handler: (event: any, ctx: ExtensionContext) => void) => handlers.set(name, handler),
        events: { on: (name: string, handler: (data: unknown) => void) => bus.set(name, handler) },
    } as unknown as ExtensionAPI);
    const emit = (name: string, event: object = {}) => {
        assert.ok(handlers.has(name), `missing handler: ${name}`);
        handlers.get(name)!(event, ctx);
    };
    t.after(() => {
        emit('session_shutdown');
        assert.equal(intervals.size, 0);
        setWorkingMessageActive(false);
    });
    return {
        emit, calls, intervals,
        advance: (ms: number) => { now += ms; },
        worker: (data: unknown) => bus.get(WORKER_USAGE_EVENT)!(data),
        tick: () => { for (const callback of intervals.values()) callback(); },
    };
}

test('installed footer integrates Codex model changes, session replacement, redraw and shutdown disposal', async (t) => {
    t.mock.method(Date, 'now', () => 1_800_000_000_000);
    const handlers = new Map<string, (event: any, ctx: ExtensionContext) => void>();
    const intervals = new Map<NodeJS.Timeout, () => void>();
    t.mock.method(globalThis, 'setInterval', (callback: () => void, delay: number) => {
        assert.equal(delay, USAGE_TICK_MS);
        const handle = {} as NodeJS.Timeout;
        intervals.set(handle, callback);
        return handle;
    });
    t.mock.method(globalThis, 'clearInterval', (handle: NodeJS.Timeout) => assert.ok(intervals.delete(handle)));
    let requests = 0;
    const fetch = t.mock.method(globalThis, 'fetch', async () => {
        requests++;
        return new Response(JSON.stringify({ rate_limit: { secondary_window: {
            used_percent: 23, reset_at: Date.now() / 1000 + 5.5 * 86400,
        } } }));
    });
    let footer: NoCostFooter | undefined;
    let redraws = 0;
    const theme = { fg: (_color: string, text: string) => text };
    const ctx = {
        cwd: '/project', model: { provider: 'openai-codex', id: 'codex', baseUrl: 'https://chatgpt.com/backend-api/codex' },
        thinkingLevel: 'high',
        modelRegistry: {
            getProvider: () => ({ baseUrl: 'https://chatgpt.com' }),
            getProviderAuth: async () => undefined,
            getApiKeyAndHeaders: async () => ({ ok: true, apiKey: 'fixture-secret' }),
        },
        ui: {
            theme, setWorkingVisible() {}, setHeader() {}, setEditorComponent() {},
            setFooter(factory: any) {
                footer?.dispose();
                footer = factory({ requestRender: () => redraws++ }, theme, {
                    getGitBranch: () => null, getExtensionStatuses: () => new Map(), getAvailableProviderCount: () => 1,
                });
            },
        },
    } as unknown as ExtensionContext;
    ui({ on: (name: string, handler: any) => handlers.set(name, handler), events: { on() {} } } as unknown as ExtensionAPI);
    const emit = (name: string) => handlers.get(name)!({}, ctx);
    const flush = () => new Promise<void>((resolve) => setImmediate(resolve));
    t.after(() => emit('session_shutdown'));
    emit('session_start');
    assert.equal(intervals.size, 1);
    assert.ok(!footer!.render(120)[0]!.includes('[77% · '), 'factory returns before network finishes');
    await flush();
    const line = footer!.render(120)[0]!;
    assert.ok(line.includes('[77% · 5d12h] '));
    assert.ok(line.endsWith('codex . high'));
    assert.equal(redraws, 1);
    (ctx.model as any).provider = 'other';
    emit('model_select');
    assert.equal(intervals.size, 0);
    assert.ok(!footer!.render(120)[0]!.includes('[77% · '));
    (ctx.model as any).provider = 'openai-codex';
    emit('model_select');
    await flush();
    assert.equal(requests, 2);
    assert.equal(intervals.size, 1);
    const previous = footer!;
    emit('session_start');
    await flush();
    assert.equal(intervals.size, 1, 'session replacement retains just one usage timer');
    assert.ok(!previous.render(120)[0]!.includes('[77% · '));
    assert.equal(fetch.mock.callCount(), 3);
    emit('session_shutdown');
    emit('session_shutdown');
    assert.equal(intervals.size, 0);
    assert.ok(!footer!.render(120)[0]!.includes('[77% · '));
});

test('agent restart clears its timer before refreshing reset usage and scheduling one replacement', (t) => {
    const fixture = setup(t);
    assert.equal(fixture.intervals.size, 0, 'registration does not allocate a timer');
    fixture.emit('session_start');
    fixture.calls.length = 0;
    fixture.emit('agent_start');
    assert.deepEqual(fixture.calls, ['refresh', 'schedule']);
    assert.equal(fixture.intervals.size, 1);
    assert.equal(getWorkingMessageLine(), '⠋ Turn 1 · 0s');

    fixture.emit('turn_start', { turnIndex: 2 });
    fixture.emit('message_end', { message: { role: 'assistant', usage: { input: 10, output: 4 } } });
    fixture.worker({ taskId: 'worker', input: 5, output: 2 });
    fixture.advance(1_000);
    fixture.tick();
    assert.equal(getWorkingMessageLine(), '⠇ Turn 3 · ↑15 ↓6 · 6.0 TPS · 1s');

    // Also leave streamed usage pending, so both usage accumulators must reset.
    fixture.emit('message_update', {
        message: { role: 'assistant', content: [], usage: { input: 3, output: 2 } },
    });
    fixture.calls.length = 0;
    fixture.emit('agent_start');
    assert.deepEqual(fixture.calls, ['clear', 'refresh', 'schedule']);
    assert.equal(fixture.intervals.size, 1);
    assert.equal(getWorkingMessageLine(), '⠇ Turn 1 · 0s');
    fixture.advance(1_000);
    fixture.tick();
    assert.equal(getWorkingMessageLine(), '⠦ Turn 1 · 1s');
});

test('agent end refreshes final usage after clearing once and repeated termination is timer-idempotent', (t) => {
    const fixture = setup(t);
    fixture.emit('agent_start');
    fixture.emit('message_end', { message: { role: 'assistant', usage: { input: 10, output: 4 } } });
    fixture.advance(2_000);
    fixture.calls.length = 0;
    fixture.emit('agent_end');
    assert.deepEqual(fixture.calls, ['clear', 'refresh']);
    assert.equal(fixture.intervals.size, 0);
    assert.equal(getWorkingMessageLine(), '✓ Turn 1 · ↑10 ↓4 · 2.0 TPS · 2s');
    fixture.tick();
    assert.deepEqual(fixture.calls, ['clear', 'refresh']);

    fixture.calls.length = 0;
    fixture.emit('agent_end');
    assert.deepEqual(fixture.calls, ['refresh'], 'a second end retains its existing refresh behavior');
    assert.equal(getWorkingMessageLine(), undefined);
    fixture.calls.length = 0;
    fixture.emit('session_shutdown');
    fixture.emit('session_shutdown');
    assert.deepEqual(fixture.calls, []);
    assert.equal(fixture.intervals.size, 0);
});

test('shutdown clears an active timer without refreshing and permits a clean restart', (t) => {
    const fixture = setup(t);
    fixture.emit('session_shutdown');
    assert.deepEqual(fixture.calls, []);
    fixture.emit('agent_start');
    fixture.calls.length = 0;
    fixture.emit('session_shutdown');
    assert.deepEqual(fixture.calls, ['clear']);
    assert.equal(fixture.intervals.size, 0);
    fixture.emit('session_shutdown');
    assert.deepEqual(fixture.calls, ['clear']);
    fixture.calls.length = 0;
    fixture.advance(2_000);
    fixture.emit('agent_start');
    assert.deepEqual(fixture.calls, ['refresh', 'schedule']);
    assert.equal(fixture.intervals.size, 1);
    assert.equal(getWorkingMessageLine(), '⠋ Turn 1 · 0s');
});
