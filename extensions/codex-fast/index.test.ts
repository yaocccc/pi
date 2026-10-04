import assert from 'node:assert/strict';
import fs, { existsSync, mkdirSync, mkdtempSync, readFileSync, renameSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { syncBuiltinESMExports } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test, { type TestContext } from 'node:test';
import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';

type Config = { fast: boolean; ultrafast: boolean };
type Select = (title: string, options: string[]) => string | undefined | Promise<string | undefined>;
const title = 'Codex Fast 设置 · 回车切换并保存 · Esc 退出';
const labels = ({ fast, ultrafast }: Config) => [
    `Fast · ${fast ? '开启' : '关闭'}`,
    `Ultrafast · ${ultrafast ? '开启' : '关闭'}（仅 gpt-6-astra）`,
];
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
    const { default: extension } = await import(`./index.ts?test=${++importId}`);
    extension({
        registerCommand: (name: string, command: any) => commands.set(name, command),
        on: (name: string, handler: any) => handlers.set(name, handler),
    } as unknown as ExtensionAPI);
    const request = (
        model: object | null | undefined,
        payload: unknown = { fixture: true },
    ) => handlers.get('before_provider_request')({ payload }, { model });
    return {
        file, commands, notifications, request,
        open: (select: Select, hasUI = true) => commands.get('codex-fast').handler('', {
            hasUI,
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

test('only codex-fast is registered and the old directory has no entry or tests', async (t) => {
    const fixture = await setup(t, { fast: false, ultrafast: false });
    assert.deepEqual([...fixture.commands.keys()], ['codex-fast']);
    assert.equal(existsSync(new URL('../fast/index.ts', import.meta.url)), false);
    assert.equal(existsSync(new URL('../fast/index.test.ts', import.meta.url)), false);
});

for (const fast of [false, true]) {
    for (const ultrafast of [false, true]) {
        test(`menu independently toggles and atomically saves from fast=${fast}, ultrafast=${ultrafast}`, async (t) => {
            const fixture = await setup(t, { fast, ultrafast });
            const counts = trackSaves(t, fixture.file);
            const states = [
                { fast, ultrafast },
                { fast, ultrafast: !ultrafast },
                { fast: !fast, ultrafast: !ultrafast },
                { fast: !fast, ultrafast },
                { fast, ultrafast },
            ];
            let call = 0;
            await fixture.open((shownTitle, options) => {
                const state = states[call]!;
                assert.equal(shownTitle, title);
                assert.deepEqual(options, labels(state), 'exactly two toggles, no save button or submenu');
                assert.deepEqual(fixture.saved(), state, 'each selection is saved before reopening');
                assert.deepEqual(counts, { writes: call, renames: call });
                assert.equal(fixture.tier(), state.ultrafast ? 'ultrafast' : state.fast ? 'priority' : undefined);
                assert.equal(fixture.tier('gpt-other'), state.fast ? 'priority' : undefined);
                assert.equal(existsSync(`${fixture.file}.${process.pid}.tmp`), false);
                if (call > 0) assert.equal(statSync(fixture.file).mode & 0o777, 0o600);
                return call === 4 ? undefined : options[[1, 0, 1, 0][call++]!];
            });
            assert.equal(call, 4);
            assert.deepEqual(fixture.saved(), { fast, ultrafast });
            assert.equal(fixture.notifications.length, 4);
            assert.ok(fixture.notifications.every(({ level }) => level === 'info'));
            assert.equal(fixture.notifications[0]!.message, `Ultrafast 模式已${!ultrafast ? '开启' : '关闭'}`);
        });

        test(`Esc exits without writing from fast=${fast}, ultrafast=${ultrafast}`, async (t) => {
            const fixture = await setup(t, { fast, ultrafast });
            const original = readFileSync(fixture.file, 'utf8');
            const counts = trackSaves(t, fixture.file);
            let calls = 0;
            await fixture.open(() => { calls++; return undefined; });
            assert.equal(calls, 1);
            assert.deepEqual(counts, { writes: 0, renames: 0 });
            assert.equal(readFileSync(fixture.file, 'utf8'), original);
            assert.deepEqual(fixture.notifications, []);
        });
    }
}

test('without UI the command warns without opening a menu or writing', async (t) => {
    const fixture = await setup(t, { fast: true, ultrafast: true });
    const counts = trackSaves(t, fixture.file);
    await fixture.open(() => { assert.fail('must not select without UI'); }, false);
    assert.deepEqual(counts, { writes: 0, renames: 0 });
    assert.deepEqual(fixture.saved(), { fast: true, ultrafast: true });
    assert.equal(fixture.notifications.length, 1);
    assert.equal(fixture.notifications[0]!.level, 'warning');
});

for (const key of ['fast', 'ultrafast'] as const) {
    for (const stage of ['write', 'rename']) {
        test(`${key} ${stage} failure retains disk and memory and retries in the same menu`, async (t) => {
            const fixture = await setup(t, { fast: true, ultrafast: true });
            const original = readFileSync(fixture.file, 'utf8');
            const temp = `${fixture.file}.${process.pid}.tmp`;
            const backup = `${fixture.file}.backup`;
            if (stage === 'write') mkdirSync(temp);
            else {
                renameSync(fixture.file, backup);
                mkdirSync(fixture.file);
            }
            let call = 0;
            await fixture.open((shownTitle, options) => {
                assert.equal(shownTitle, title);
                if (call === 1) {
                    assert.deepEqual(options, labels({ fast: true, ultrafast: true }));
                    assert.equal(fixture.notifications.at(-1)!.level, 'error');
                    assert.match(fixture.notifications.at(-1)!.message, /保存 Fast 配置失败:/);
                    assert.equal(readFileSync(stage === 'write' ? fixture.file : backup, 'utf8'), original);
                    assert.equal(fixture.tier(), 'ultrafast', 'failed save must retain ultrafast memory');
                    assert.equal(fixture.tier('gpt-other'), 'priority', 'failed save must retain fast memory');
                    if (stage === 'write') rmSync(temp, { recursive: true });
                    else {
                        rmSync(fixture.file, { recursive: true });
                        renameSync(backup, fixture.file);
                    }
                } else if (call === 2) {
                    const expected = { fast: key !== 'fast', ultrafast: key !== 'ultrafast' };
                    assert.deepEqual(options, labels(expected));
                    assert.deepEqual(fixture.saved(), expected, 'retry toggles last successfully saved state');
                    assert.equal(fixture.notifications.at(-1)!.level, 'info');
                    assert.equal(fixture.tier(), key === 'ultrafast' ? 'priority' : 'ultrafast');
                    assert.equal(existsSync(temp), false);
                    return undefined;
                }
                call++;
                return options[key === 'fast' ? 0 : 1];
            });
            assert.equal(call, 2);
            assert.equal(fixture.notifications.length, 2);
        });
    }
}

for (const fast of [false, true]) {
    for (const ultrafast of [false, true]) {
        test(`routing constraints and payload preservation from fast=${fast}, ultrafast=${ultrafast}`, async (t) => {
            const fixture = await setup(t, { fast, ultrafast });
            const supported = { id: 'gpt-6-astra', provider: 'openai-codex', api: 'openai-codex-responses' };
            const ids = ['gpt-6-astra', 'gpt-other', 'gpt-6-astra-preview', 'gpt-6.1-sol', 'o3', 'Gpt-6-astra'];
            for (const id of ids) {
                const payload = { model: id, fixture: true, service_tier: 'existing', nested: { keep: true } };
                const original = structuredClone(payload);
                const result = fixture.request({ ...supported, id }, payload);
                const tier = ultrafast && id === 'gpt-6-astra' ? 'ultrafast' : fast ? 'priority' : undefined;
                assert.deepEqual(result, tier ? { ...payload, service_tier: tier } : undefined);
                assert.deepEqual(payload, original, 'input payload is never mutated');
                if (result) assert.equal(result.nested, payload.nested);
            }
            for (const model of [
                { ...supported, provider: 'openai' },
                { ...supported, api: 'openai-responses' },
                {},
            ]) assert.equal(fixture.request(model), undefined);
            assert.equal(fixture.request(undefined), undefined);
            assert.equal(fixture.request(null), undefined);
            for (const payload of [null, false, 1, 'payload', [], ['payload']]) {
                assert.equal(fixture.request(supported, payload), undefined);
            }
        });
    }
}

test('codex-fast.json retains strict boolean config compatibility and missing-file defaults', async (t) => {
    for (const [initial, expected] of [
        [{ fast: true, ultrafast: false }, { fast: true, ultrafast: false }],
        [{ fast: false, ultrafast: true }, { fast: false, ultrafast: true }],
        [{ fast: 'true', ultrafast: 1 }, { fast: false, ultrafast: false }],
        [{ enabled: true }, { fast: false, ultrafast: false }],
        [{}, { fast: false, ultrafast: false }],
        [undefined, { fast: false, ultrafast: false }],
    ] as const) {
        await t.test(JSON.stringify(initial) ?? 'missing file', async (child) => {
            const fixture = await setup(child, initial);
            const counts = trackSaves(child, fixture.file);
            await fixture.open((_title, options) => {
                assert.deepEqual(options, labels(expected));
                return undefined;
            });
            assert.deepEqual(counts, { writes: 0, renames: 0 });
            assert.equal(fixture.tier(), expected.ultrafast ? 'ultrafast' : expected.fast ? 'priority' : undefined);
        });
    }
});
