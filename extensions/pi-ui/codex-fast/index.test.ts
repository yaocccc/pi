import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { registerCodexFastUI } from './index.ts';
import { registerExtensionRpc, EXTENSION_RPC_PREFIX } from '../rpc.ts';
import { getCapability } from '../../shared/capabilities.ts';
import { CODEX_FAST_CAPABILITY, type CodexFastService } from '../../codex-fast/api.ts';
import fs, { existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { syncBuiltinESMExports } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test, { type TestContext } from 'node:test';
import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';

type Level = 'normal' | 'fast' | 'ultrafast';
type Config = { level: Level };
const levels: Level[] = ['normal', 'fast', 'ultrafast'];
type Select = (title: string, options: string[]) => string | undefined | Promise<string | undefined>;
const labels = ({ level }: Config) => levels.map((item) => `${item}${item === level ? ' · 当前' : ''}`);
let importId = 0;

async function setup(t: TestContext, initial: unknown) {
    const root = mkdtempSync(join(tmpdir(), 'pi-codex-fast-test-'));
    const previous = process.env.PI_CODING_AGENT_DIR;
    process.env.PI_CODING_AGENT_DIR = root;
    t.after(() => {
        if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
        else process.env.PI_CODING_AGENT_DIR = previous;
        rmSync(root, { recursive: true, force: true });
    });
    const file = join(root, 'codex-fast.json');
    if (initial !== undefined) writeFileSync(file, `${JSON.stringify(initial)}\n`);
    const commands = new Map<string, any>();
    const handlers = new Map<string, any>();
    const notifications: { message: string; level: string }[] = [];
    const { default: extension } = await import(`../../codex-fast/index.ts?test=${++importId}`);
    const bus = new EventEmitter();
    const pi = {
        events: {
            emit: (name: string, data: unknown) => { bus.emit(name, data); },
            on: (name: string, handler: (data: any) => void) => { bus.on(name, handler); return () => { bus.off(name, handler); }; },
        },
        registerCommand: (name: string, command: any) => commands.set(name, command),
        on: (name: string, handler: any) => {
            const previous = handlers.get(name);
            handlers.set(name, (...args: any[]) => { previous?.(...args); return handler(...args); });
        },
    } as unknown as ExtensionAPI;
    // Resolve services at use time, not adapter installation time.
    registerCodexFastUI(pi);
    extension(pi);
    const request = (
        model: object | null | undefined,
        payload: unknown = { fixture: true },
    ) => handlers.get('before_provider_request')({ payload }, { model });
    return {
        file, commands, notifications, request, pi, handlers,
        open: (select: Select, hasUI = true) => commands.get('codex-fast').handler('', {
            hasUI,
            sessionManager: { getSessionId: () => 'session' },
            ui: { select, notify: (message: string, level: string) => notifications.push({ message, level }) },
        }),
        saved: (): Config => JSON.parse(readFileSync(file, 'utf8')),
        tier: (id = 'gpt-6-astra') => request({ id, provider: 'openai-codex', api: 'openai-codex-responses' })?.service_tier,
    };
}

function trackSaves(t: TestContext, file: string) {
    const counts = { writes: 0, renames: 0 };
    const write = fs.writeFileSync;
    const rename = fs.renameSync;
    const writeMock = t.mock.method(fs, 'writeFileSync', (...args: Parameters<typeof write>) => {
        if (args[0] === `${file}.${process.pid}.tmp`) counts.writes++;
        return Reflect.apply(write, fs, args);
    });
    const renameMock = t.mock.method(fs, 'renameSync', (...args: Parameters<typeof rename>) => {
        if (args[1] === file) counts.renames++;
        return Reflect.apply(rename, fs, args);
    });
    syncBuiltinESMExports();
    t.after(() => {
        writeMock.mock.restore();
        renameMock.mock.restore();
        syncBuiltinESMExports();
    });
    return counts;
}


test('running extension reloads externally saved mode for requests and menu', async (t) => {
    const fixture = await setup(t, { level: 'normal' });
    for (const level of levels) {
        const temp = `${fixture.file}.gui.tmp`;
        writeFileSync(temp, JSON.stringify({ level }), { mode: 0o600 });
        renameSync(temp, fixture.file);
        assert.equal(fixture.tier(), level === 'ultrafast' ? 'ultrafast' : level === 'fast' ? 'priority' : undefined);
        assert.equal(fixture.tier('other'), level === 'normal' ? undefined : 'priority');
        await fixture.open((_title, options) => {
            assert.deepEqual(options, labels({ level }));
            return undefined;
        });
    }
    writeFileSync(fixture.file, '{broken');
    assert.equal(fixture.tier(), 'ultrafast', 'bad external write retains last known mode');
});

test('standalone adapter registers codex-fast and its RPC bridge', async (t) => {
    const fixture = await setup(t, { level: 'normal' });
    assert.deepEqual([...fixture.commands.keys()], ['pi-ui-rpc', 'codex-fast']);
});

for (const level of levels) {
    test(`select and atomically save every tier from ${level}`, async (t) => {
        const fixture = await setup(t, { level });
        const counts = trackSaves(t, fixture.file);
        let current = level;
        for (const next of levels) {
            let calls = 0;
            await fixture.open((title, options) => {
                calls++;
                assert.match(title, new RegExp(`当前 ${current}`));
                assert.deepEqual(options, labels({ level: current }));
                return options[levels.indexOf(next)];
            });
            assert.equal(calls, 1, 'selection exits instead of reopening');
            assert.deepEqual(fixture.saved(), { level: next });
            assert.equal(fixture.notifications.at(-1)?.message, `Codex 档位已设为 ${next}`);
            assert.equal(fixture.notifications.at(-1)?.level, 'info');
            assert.equal(fixture.tier(), next === 'ultrafast' ? 'ultrafast' : next === 'fast' ? 'priority' : undefined);
            assert.equal(fixture.tier('gpt-other'), next === 'normal' ? undefined : 'priority');
            assert.equal(statSync(fixture.file).mode & 0o777, 0o600);
            assert.equal(existsSync(`${fixture.file}.${process.pid}.tmp`), false);
            current = next;
        }
        assert.deepEqual(counts, { writes: 3, renames: 3 });
    });

    test(`cancel or unexpected selection preserves ${level}`, async (t) => {
        const fixture = await setup(t, { level });
        const counts = trackSaves(t, fixture.file);
        const original = readFileSync(fixture.file, 'utf8');
        for (const choice of [undefined, 'unexpected']) await fixture.open(() => choice);
        assert.deepEqual(counts, { writes: 0, renames: 0 });
        assert.equal(readFileSync(fixture.file, 'utf8'), original);
        assert.deepEqual(fixture.notifications, []);
    });

    test(`routing and payload preservation for ${level}`, async (t) => {
        const fixture = await setup(t, { level });
        const supported = { id: 'gpt-6-astra', provider: 'openai-codex', api: 'openai-codex-responses' };
        for (const id of ['gpt-6-astra', 'gpt-other', 'gpt-6-astra-preview', 'gpt-6.1-sol', 'o3', 'Gpt-6-astra']) {
            const payload = { model: id, fixture: true, service_tier: 'existing', nested: { keep: true } };
            const original = structuredClone(payload);
            const result = fixture.request({ ...supported, id }, payload);
            const tier = level === 'ultrafast' && id === 'gpt-6-astra' ? 'ultrafast' : level !== 'normal' ? 'priority' : undefined;
            assert.deepEqual(result, tier ? { ...payload, service_tier: tier } : undefined);
            assert.deepEqual(payload, original);
            if (result) assert.equal(result.nested, payload.nested);
        }
        for (const model of [{ ...supported, provider: 'openai' }, { ...supported, api: 'openai-responses' }, {}, null, undefined]) {
            assert.equal(fixture.request(model), undefined);
        }
        for (const payload of [null, false, 1, 'payload', [], ['payload']]) {
            assert.equal(fixture.request(supported, payload), undefined);
        }
    });
}

test('without UI warns without opening or saving', async (t) => {
    const fixture = await setup(t, { level: 'ultrafast' });
    const counts = trackSaves(t, fixture.file);
    await fixture.open(() => assert.fail('must not select without UI'), false);
    assert.deepEqual(counts, { writes: 0, renames: 0 });
    assert.deepEqual(fixture.saved(), { level: 'ultrafast' });
    assert.equal(fixture.notifications[0]?.level, 'warning');
});

for (const stage of ['write', 'rename']) {
    test(`${stage} failure retains config and memory, later invocation retries`, async (t) => {
        const fixture = await setup(t, { level: 'ultrafast' });
        const original = readFileSync(fixture.file, 'utf8');
        const temp = `${fixture.file}.${process.pid}.tmp`;
        const backup = `${fixture.file}.backup`;
        if (stage === 'write') mkdirSync(temp);
        else {
            renameSync(fixture.file, backup);
            mkdirSync(fixture.file);
        }
        await fixture.open((_title, options) => options[0]);
        assert.equal(fixture.notifications.at(-1)?.level, 'error');
        assert.match(fixture.notifications.at(-1)!.message, /保存 Fast 配置失败:/);
        assert.equal(readFileSync(stage === 'write' ? fixture.file : backup, 'utf8'), original);
        assert.equal(fixture.tier(), 'ultrafast');
        assert.equal(fixture.tier('gpt-other'), 'priority');
        if (stage === 'write') rmSync(temp, { recursive: true });
        else {
            rmSync(fixture.file, { recursive: true });
            renameSync(backup, fixture.file);
        }
        await fixture.open((_title, options) => {
            assert.deepEqual(options, labels({ level: 'ultrafast' }));
            return options[0];
        });
        assert.deepEqual(fixture.saved(), { level: 'normal' });
        assert.equal(fixture.tier(), undefined);
        assert.equal(fixture.notifications.at(-1)?.level, 'info');
        assert.equal(existsSync(temp), false);
    });
}

test('new level precedence, strict legacy booleans, malformed values, and missing defaults', async (t) => {
    const cases: [unknown, Level][] = [
        [{ fast: false, ultrafast: false }, 'normal'],
        [{ fast: true, ultrafast: false }, 'fast'],
        [{ fast: false, ultrafast: true }, 'ultrafast'],
        [{ fast: true, ultrafast: true }, 'ultrafast'],
        [{ fast: 'true', ultrafast: 1 }, 'normal'],
        [{ level: 'normal', fast: true, ultrafast: true }, 'normal'],
        [{ level: 'fast', ultrafast: true }, 'fast'],
        [{ level: 'ultrafast' }, 'ultrafast'],
        [{ level: 'invalid', fast: true }, 'normal'],
        [{ level: null, ultrafast: true }, 'normal'],
        [{ level: false }, 'normal'],
        [{ level: 1 }, 'normal'],
        [{ enabled: true }, 'normal'],
        [{}, 'normal'], [null, 'normal'], [[], 'normal'], ['fast', 'normal'], [undefined, 'normal'],
    ];
    for (const [initial, level] of cases) {
        await t.test(JSON.stringify(initial) ?? 'missing', async (child) => {
            const fixture = await setup(child, initial);
            const counts = trackSaves(child, fixture.file);
            await fixture.open((_title, options) => {
                assert.deepEqual(options, labels({ level }));
                return undefined;
            });
            assert.deepEqual(counts, { writes: 0, renames: 0 }, 'loading does not migrate disk');
            assert.equal(fixture.tier(), level === 'ultrafast' ? 'ultrafast' : level === 'fast' ? 'priority' : undefined);
            assert.equal(fixture.tier('other'), level === 'normal' ? undefined : 'priority');
            await fixture.open((_title, options) => options[levels.indexOf(level)]);
            assert.deepEqual(fixture.saved(), { level }, 'saving migrates legacy config to single level');
        });
    }
});

for (const answer of [undefined, 'fast', 'invalid']) {
    test(`RPC settings without protocol flags: ${answer ?? 'cancel'}`, async (t) => {
        const fixture = await setup(t, { level: 'normal' });
        const previous = { PI_UI_PROTOCOL: process.env.PI_UI_PROTOCOL, PI_GOUI_PROTOCOL: process.env.PI_GOUI_PROTOCOL };
        for (const name of Object.keys(previous)) delete process.env[name];
        t.after(() => { for (const [name, value] of Object.entries(previous)) { if (value === undefined) delete process.env[name]; else process.env[name] = value; } });
        const events: any[] = [];
        const counts = trackSaves(t, fixture.file);
        await fixture.commands.get('codex-fast').handler('', {
            mode: 'rpc', hasUI: true,
            sessionManager: { getSessionId: () => 'session' },
            ui: {
                select: () => assert.fail('Go owns the settings menu'),
                input: async (title: string, payload: string) => {
                    assert.equal(title, 'pi-goui:codex-fast:v1');
                    assert.deepEqual(JSON.parse(payload), { level: 'normal' });
                    assert.deepEqual(counts, { writes: 0, renames: 0 });
                    return answer;
                },
                notify: (message: string) => {
                    assert.ok(message.startsWith('pi-goui:event:v1:'));
                    events.push(JSON.parse(message.slice('pi-goui:event:v1:'.length)));
                },
            },
        });
        assert.deepEqual(fixture.saved(), { level: answer === 'fast' ? 'fast' : 'normal' });
        assert.deepEqual(counts, answer === 'fast' ? { writes: 1, renames: 1 } : { writes: 0, renames: 0 });
        assert.deepEqual(events, answer === undefined ? [] : answer === 'fast'
            ? [{ name: 'codex.settings-saved', data: { level: 'fast' } }]
            : [{ name: 'codex.settings-error', data: { code: 'invalid-level', message: 'invalid' } }]);
    });
}

test('native mode does not select Go protocol even with capability environment', async (t) => {
    const fixture = await setup(t, { level: 'normal' });
    const previous = process.env.PI_GOUI_PROTOCOL;
    process.env.PI_GOUI_PROTOCOL = '1';
    t.after(() => { if (previous === undefined) delete process.env.PI_GOUI_PROTOCOL; else process.env.PI_GOUI_PROTOCOL = previous; });
    await fixture.commands.get('codex-fast').handler('', {
        mode: 'tui', hasUI: true,
        sessionManager: { getSessionId: () => 'session' },
        ui: { select: async (_title: string, options: string[]) => options[1], input: () => assert.fail('native must not use Go protocol'), notify: () => {} },
    });
    assert.deepEqual(fixture.saved(), { level: 'fast' });
});

test('Go save failure emits data error and retains persisted configuration', async (t) => {
    const fixture = await setup(t, { level: 'normal' });
    const previous = process.env.PI_GOUI_PROTOCOL;
    process.env.PI_GOUI_PROTOCOL = '1';
    t.after(() => { if (previous === undefined) delete process.env.PI_GOUI_PROTOCOL; else process.env.PI_GOUI_PROTOCOL = previous; });
    mkdirSync(`${fixture.file}.${process.pid}.tmp`);
    const events: any[] = [];
    await fixture.commands.get('codex-fast').handler('', {
        mode: 'rpc', hasUI: true,
        sessionManager: { getSessionId: () => 'session' },
        ui: { input: async () => 'fast', notify: (message: string) => events.push(JSON.parse(message.slice('pi-goui:event:v1:'.length))) },
    });
    assert.deepEqual(fixture.saved(), { level: 'normal' });
    assert.equal(events.length, 1);
    assert.equal(events[0].name, 'codex.settings-error');
    assert.match(events[0].data.message, /EISDIR/);
});


test('direct capability saves and RPC saves share committed facts', async (t) => {
    const previous = process.env.PI_GOUI_PROTOCOL;
    process.env.PI_GOUI_PROTOCOL = '1';
    t.after(() => { if (previous === undefined) delete process.env.PI_GOUI_PROTOCOL; else process.env.PI_GOUI_PROTOCOL = previous; });
    const h = await setup(t, { level: 'normal' });
    const messages: string[] = [];
    const ctx = { mode: 'rpc', hasUI: true, sessionManager: { getSessionId: () => 'session' }, ui: { notify: (message: string) => messages.push(message) } };
    h.handlers.get('session_start')({}, ctx);
    const service = getCapability<CodexFastService>(h.pi, CODEX_FAST_CAPABILITY)!;
    service.setLevel('fast');
    assert.equal(messages.filter(message => message.includes('codex.settings-saved')).length, 1);
    registerExtensionRpc(h.pi);
    const invoke = async (id: string, method: string, params?: unknown) => {
        await h.commands.get('pi-ui-rpc').handler(JSON.stringify({ id, method, params }), ctx);
        return JSON.parse(messages.filter(message => message.startsWith(EXTENSION_RPC_PREFIX)).at(-1)!.slice(EXTENSION_RPC_PREFIX.length));
    };
    assert.deepEqual(await invoke('read', 'codex-fast.get'), { id: 'read', result: { level: 'fast', levels } });
    assert.deepEqual(await invoke('save', 'codex-fast.set', { level: 'ultrafast' }), { id: 'save', result: { level: 'ultrafast', levels } });
    assert.equal(h.saved().level, 'ultrafast');
    assert.equal(messages.filter(message => message.includes('codex.settings-saved')).length, 2);
    assert.equal((await invoke('bad', 'codex-fast.set', { level: 'invalid' })).error.code, 'invalid-level');
    assert.equal(messages.filter(message => message.includes('codex.settings-saved')).length, 2);
    mkdirSync(h.file + '.' + process.pid + '.tmp');
    assert.equal((await invoke('failed', 'codex-fast.set', { level: 'normal' })).error.code, 'EISDIR');
    assert.equal(h.saved().level, 'ultrafast');
    assert.equal(messages.filter(message => message.includes('codex.settings-saved')).length, 2);
    h.handlers.get('session_shutdown')({});
});

test('Codex dialog lifecycle is isolated between adapter instances', async (t) => {
    const previous = process.env.PI_CODING_AGENT_DIR;
    const old = await setup(t, { level: 'normal' });
    const current = await setup(t, { level: 'normal' });
    t.after(() => {
        if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
        else process.env.PI_CODING_AGENT_DIR = previous;
    });
    const oldNotices: string[] = [];
    const currentNotices: string[] = [];
    const answer = deferred<string | undefined>();
    const ctx = {
        mode: 'rpc', hasUI: true, sessionManager: { getSessionId: () => 'same-id' },
        ui: { input: () => answer.promise, notify: (message: string) => oldNotices.push(message) },
    };
    const pending = old.commands.get('codex-fast').handler('', ctx);
    old.handlers.get('session_shutdown')({});
    await current.commands.get('codex-fast').handler('', {
        ...ctx, ui: { input: async () => 'fast', notify: (message: string) => currentNotices.push(message) },
    });
    answer.resolve('fast');
    await pending;
    assert.deepEqual(old.saved(), { level: 'normal' });
    assert.deepEqual(oldNotices, []);
    assert.deepEqual(current.saved(), { level: 'fast' });
    assert.equal(currentNotices.length, 1);
});

for (const method of ['codex-fast.get', 'codex-fast.set']) {
    test(`${method} cannot leave a stale menu able to overwrite the current owner`, async (t) => {
        const h = await setup(t, { level: 'normal' });
        const counts = trackSaves(t, h.file);
        const answer = deferred<string | undefined>();
        const oldNotices: string[] = [];
        const notices: string[] = [];
        const ctx = {
            mode: 'rpc', hasUI: true, sessionManager: { getSessionId: () => 'origin' },
            ui: { input: () => answer.promise, notify: (message: string) => oldNotices.push(message) },
        };
        const pending = h.commands.get('codex-fast').handler('', ctx);
        await h.commands.get('pi-ui-rpc').handler(JSON.stringify({ id: 'read', method, params: { level: 'normal' } }), {
            ...ctx,
            sessionManager: { getSessionId: () => method.endsWith('.get') ? 'replacement' : 'origin' },
            ui: { notify: (message: string) => notices.push(message) },
        });
        answer.resolve('fast');
        await pending;
        assert.deepEqual(h.saved(), { level: 'normal' });
        assert.deepEqual(oldNotices, []);
        assert.deepEqual(counts, method.endsWith('.set') ? { writes: 1, renames: 1 } : { writes: 0, renames: 0 });
        assert.equal(notices.filter(message => message.includes('codex.settings-saved')).length, method.endsWith('.set') ? 1 : 0);
    });
}

function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (error: Error) => void;
    const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
    return { promise, resolve, reject };
}

for (const mode of ['rpc', 'tui'] as const) {
    for (const change of ['session_start', 'session_shutdown', 'session-id'] as const) {
        for (const rejected of [false, true]) {
            test(`${mode}: ${change} ignores late ${rejected ? 'errors' : 'answers'}`, async (t) => {
                const h = await setup(t, { level: 'normal' });
                const counts = trackSaves(t, h.file);
                const notices: string[] = [];
                let sessionId = 'origin';
                let answer = deferred<string | undefined>();
                const ctx = {
                    mode, hasUI: true,
                    sessionManager: { getSessionId: () => sessionId },
                    ui: {
                        input: () => answer.promise, select: () => answer.promise,
                        notify: (message: string) => notices.push(message),
                    },
                };
                const pending = h.commands.get('codex-fast').handler('', ctx);
                if (change === 'session-id') sessionId = 'replacement';
                else h.handlers.get(change)({}, ctx);
                if (rejected) answer.reject(new Error('late dialog failure'));
                else answer.resolve('fast');
                await pending;
                assert.deepEqual(h.saved(), { level: 'normal' });
                assert.deepEqual(counts, { writes: 0, renames: 0 });
                assert.deepEqual(notices, [], 'stale dialogs must not emit saved/error notices');
                // A fresh request remains usable after lifecycle invalidation.
                h.handlers.get('session_start')({}, ctx);
                answer = deferred<string | undefined>();
                answer.resolve('fast');
                await h.commands.get('codex-fast').handler('', ctx);
                assert.deepEqual(h.saved(), { level: 'fast' });
                assert.deepEqual(counts, { writes: 1, renames: 1 });
                assert.equal(notices.length, 1);
            });
        }
    }

    test(`${mode}: a newer dialog supersedes an earlier response`, async (t) => {
        const h = await setup(t, { level: 'normal' });
        const counts = trackSaves(t, h.file);
        const notices: string[] = [];
        const old = deferred<string | undefined>();
        let answer = old;
        const ctx = {
            mode, hasUI: true, sessionManager: { getSessionId: () => 'origin' },
            ui: { input: () => answer.promise, select: () => answer.promise, notify: (message: string) => notices.push(message) },
        };
        const pending = h.commands.get('codex-fast').handler('', ctx);
        answer = deferred<string | undefined>();
        answer.resolve(mode === 'rpc' ? 'normal' : 'normal · 当前');
        await h.commands.get('codex-fast').handler('', ctx);
        old.resolve('fast');
        await pending;
        assert.deepEqual(h.saved(), { level: 'normal' });
        assert.deepEqual(counts, { writes: 1, renames: 1 });
        assert.equal(notices.length, 1);
    });
}
