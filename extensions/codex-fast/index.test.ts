import assert from 'node:assert/strict';
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

test('only codex-fast is registered', async (t) => {
    const fixture = await setup(t, { level: 'normal' });
    assert.deepEqual([...fixture.commands.keys()], ['codex-fast']);
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
