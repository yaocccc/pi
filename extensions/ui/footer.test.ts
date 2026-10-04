import assert from 'node:assert/strict';
import fs, { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { syncBuiltinESMExports } from 'node:module';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test, { type TestContext } from 'node:test';
import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { stripTerminalSequences, truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';
import type { FooterData } from './types.ts';

let importId = 0;
const theme = { fg: (_color: string, text: string) => `\x1b[2m${text}\x1b[22m` };

async function setup(t: TestContext) {
    const root = mkdtempSync(join(tmpdir(), 'pi-footer-test-'));
    const previous = process.env.PI_CODING_AGENT_DIR;
    process.env.PI_CODING_AGENT_DIR = root;
    t.after(() => {
        if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
        else process.env.PI_CODING_AGENT_DIR = previous;
        rmSync(root, { recursive: true, force: true });
    });
    const file = join(root, 'codex-fast.json');
    const { NoCostFooter } = await import(`./footer.ts?test=${++importId}`);
    const data: FooterData = {
        getGitBranch: () => 'main',
        getExtensionStatuses: () => new Map([['fixture', '状态\nready\tgo']]),
        getAvailableProviderCount: () => 1,
    };
    const footer = (model: object | null = { provider: 'other-provider', id: 'unsupported-model', name: '模型中文' }) =>
        new NoCostFooter({
            cwd: '/project/长路径/'.repeat(10), model: model ?? undefined, thinkingLevel: 'high',
            getContextUsage: () => ({ tokens: 1_500, contextWindow: 200_000, percent: 0.75 }),
        } as unknown as ExtensionContext, theme, data);
    return { file, footer, save: (config: unknown) => writeFileSync(file, JSON.stringify(config)) };
}

for (const fast of [false, true]) {
    for (const ultrafast of [false, true]) {
        test(`footer reflects fast=${fast}, ultrafast=${ultrafast} regardless of model routing`, async (t) => {
            const fixture = await setup(t);
            fixture.save({ fast, ultrafast });
            const icons = `${fast ? '✨ ' : ''}${ultrafast ? '🌟 ' : ''}`;
            for (const [model, name] of [
                [{ provider: 'other-provider', name: '模型中文' }, '模型中文'],
                [{ provider: 'openai-codex', id: 'not-ultrafast-compatible' }, 'not-ultrafast-compatible'],
                [null, 'no-model'],
            ] as const) {
                const footer = fixture.footer(model);
                const lines = footer.render(160);
                assert.equal(lines.length, 1);
                assert.equal(visibleWidth(lines[0]!), 160);
                assert.ok(stripTerminalSequences(lines[0]!).endsWith(`  ${icons}${name} . high`));
            }
        });
    }
}

test('footer reloads both strict boolean flags with one config read per render', async (t) => {
    const fixture = await setup(t);
    const footer = fixture.footer();
    let reads = 0;
    const original = fs.readFileSync;
    const mock = t.mock.method(fs, 'readFileSync', (...args: Parameters<typeof original>) => {
        if (args[0] === fixture.file) reads++;
        return Reflect.apply(original, fs, args);
    });
    syncBuiltinESMExports();
    t.after(() => { mock.mock.restore(); syncBuiltinESMExports(); });
    for (const config of [
        { fast: true, ultrafast: false }, { fast: false, ultrafast: true }, { fast: true, ultrafast: true },
    ]) {
        fixture.save(config);
        const line = stripTerminalSequences(footer.render(100)[0]!);
        assert.equal(line.includes('✨ '), config.fast);
        assert.equal(line.includes('🌟 '), config.ultrafast);
    }
    assert.equal(reads, 3);
});

test('footer rejects legacy and truthy flags and defaults off on invalid or unreadable config', async (t) => {
    const fixture = await setup(t);
    const footer = fixture.footer();
    const check = (fast: boolean, ultrafast: boolean) => {
        const line = stripTerminalSequences(footer.render(100)[0]!);
        assert.equal(line.includes('✨ '), fast);
        assert.equal(line.includes('🌟 '), ultrafast);
    };
    for (const config of [
        { enabled: true }, {}, null, [], true, 'true',
        { fast: 'true', ultrafast: 1 }, { fast: 1, ultrafast: 'true' },
    ]) {
        fixture.save(config);
        check(false, false);
    }
    fixture.save({ fast: true, ultrafast: 'true' });
    check(true, false);
    fixture.save({ fast: 'true', ultrafast: true });
    check(false, true);
    writeFileSync(fixture.file, '{invalid');
    check(false, false);
    rmSync(fixture.file);
    check(false, false);
    mkdirSync(fixture.file);
    check(false, false);
});

test('footer preserves right-first truncation and visible width for narrow terminals with both icons', async (t) => {
    const fixture = await setup(t);
    const footer = fixture.footer();
    for (const fast of [false, true]) {
        for (const ultrafast of [false, true]) {
            fixture.save({ fast, ultrafast });
            const rightText = `${fast ? '✨ ' : ''}${ultrafast ? '🌟 ' : ''}模型中文 . high`;
            const right = theme.fg('dim', rightText);
            for (let width = 0; width <= 80; width++) {
                const lines = footer.render(width);
                assert.equal(lines.length, 1);
                assert.ok(visibleWidth(lines[0]!) <= width, `width=${width}, fast=${fast}, ultrafast=${ultrafast}`);
                if (visibleWidth(right) + 2 >= width) {
                    assert.equal(lines[0], truncateToWidth(right, width, ''));
                } else {
                    assert.equal(visibleWidth(lines[0]!), width);
                    assert.ok(stripTerminalSequences(lines[0]!).endsWith(`  ${rightText}`));
                }
            }
        }
    }
});
