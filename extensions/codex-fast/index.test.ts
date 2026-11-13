import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test, { type TestContext } from 'node:test';
import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { getCapability } from '../shared/capabilities.ts';
import { CODEX_FAST_CAPABILITY, CODEX_FAST_CHANGED, InvalidFastLevelError, type CodexFastService } from './api.ts';

let importId = 0;
async function setup(t: TestContext, initial?: unknown) {
    const root = mkdtempSync(join(tmpdir(), 'codex-fast-capability-'));
    const previous = process.env.PI_CODING_AGENT_DIR;
    process.env.PI_CODING_AGENT_DIR = root;
    t.after(() => {
        if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
        else process.env.PI_CODING_AGENT_DIR = previous;
        rmSync(root, { recursive: true, force: true });
    });
    const file = join(root, 'codex-fast.json');
    if (initial !== undefined) writeFileSync(file, JSON.stringify(initial));
    const bus = new EventEmitter();
    const handlers = new Map<string, any>();
    const pi = {
        events: {
            emit: (name: string, data: unknown) => { bus.emit(name, data); },
            on: (name: string, handler: (data: any) => void) => { bus.on(name, handler); return () => { bus.off(name, handler); }; },
        },
        on: (name: string, handler: any) => handlers.set(name, handler),
        registerCommand: () => { throw new Error('Capability must not register UI commands'); },
    } as unknown as ExtensionAPI;
    const { default: extension } = await import(`./index.ts?capability=${++importId}`);
    extension(pi);
    const service = getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY)!;
    assert.ok(service);
    return { service, bus, file, handlers };
}

test('headless service exposes levels, validates and atomically saves without a UI', async (t) => {
    const { service, file, bus } = await setup(t);
    assert.deepEqual(service.levels, ['normal', 'fast', 'ultrafast']);
    assert.deepEqual(service.getConfig(), { level: 'normal' });
    const changes: unknown[] = [];
    bus.on(CODEX_FAST_CHANGED, (state) => changes.push(state));
    for (const level of service.levels) {
        assert.deepEqual(service.setLevel(level), { level });
        assert.deepEqual(JSON.parse(readFileSync(file, 'utf8')), { level });
    }
    assert.deepEqual(changes, service.levels.map((level) => ({ level })));
    for (const invalid of ['bogus', null, 1, {}, undefined]) {
        assert.throws(() => service.setLevel(invalid), InvalidFastLevelError);
    }
    assert.deepEqual(service.getConfig(), { level: 'ultrafast' });
    assert.equal(changes.length, 3);
    const snapshot = service.getConfig();
    snapshot.level = 'normal';
    assert.equal(service.getConfig().level, 'ultrafast');
});

test('headless service preserves legacy migration and provider-specific tiers', async (t) => {
    const { service, handlers, file } = await setup(t, { fast: true, ultrafast: true });
    assert.equal(service.getConfig().level, 'ultrafast');
    const request = handlers.get('before_provider_request');
    const model = { id: 'gpt-6-astra', provider: 'openai-codex', api: 'openai-codex-responses' };
    assert.equal(request({ payload: { original: true } }, { model }).service_tier, 'ultrafast');
    assert.equal(request({ payload: {} }, { model: { ...model, id: 'other' } }).service_tier, 'priority');
    assert.equal(request({ payload: {} }, { model: { ...model, provider: 'other' } }), undefined);
    writeFileSync(file, JSON.stringify({ level: 'normal' }));
    assert.equal(request({ payload: {} }, { model }), undefined);
    assert.equal(service.getConfig().level, 'normal');
});

test('failed persistence leaves committed state unchanged and emits no event', async (t) => {
    const { service, file, bus } = await setup(t, { level: 'fast' });
    let changes = 0;
    bus.on(CODEX_FAST_CHANGED, () => changes++);
    // A directory at the staging path deterministically rejects the write.
    mkdirSync(`${file}.${process.pid}.tmp`);
    assert.throws(() => service.setLevel('ultrafast'));
    assert.equal(service.getConfig().level, 'fast');
    assert.equal(changes, 0);
});

test('notification failures cannot undo a committed configuration', async (t) => {
    const { service, bus, file } = await setup(t);
    bus.on(CODEX_FAST_CHANGED, () => { throw new Error('observer failed'); });
    assert.deepEqual(service.setLevel('fast'), { level: 'fast' });
    assert.equal(JSON.parse(readFileSync(file, 'utf8')).level, 'fast');
});

test('committed facts are immutable across observers without freezing returned snapshots', async (t) => {
    const { service, bus } = await setup(t);
    const observed: unknown[] = [];
    bus.on(CODEX_FAST_CHANGED, (state) => { Reflect.set(state, 'level', 'normal'); });
    bus.on(CODEX_FAST_CHANGED, (state) => observed.push(state));
    const saved = service.setLevel('fast');
    assert.deepEqual(observed, [{ level: 'fast' }]);
    assert.ok(Object.isFrozen(observed[0]));
    assert.equal(Object.isFrozen(saved), false);
    saved.level = 'normal';
    assert.equal(service.getConfig().level, 'fast');
    assert.deepEqual(observed, [{ level: 'fast' }]);
});

test('capability source has no UI imports, identity checks or drawing hooks', () => {
    for (const name of ['index.ts', 'api.ts']) {
        const source = readFileSync(new URL(name, import.meta.url), 'utf8');
        assert.doesNotMatch(source, /(?:ctx\.ui|isGoUI|pi-tui|pi-ui\/|renderCall|renderResult|registerCommand)/);
    }
});
