import test from 'node:test';
import assert from 'node:assert/strict';
import * as pty from 'node-pty';
import { fileURLToPath } from 'node:url';
import { Screen } from '../src/screen.js';
import { childEnvironment } from '../src/session.js';
import { identity } from '../src/process-tree.js';

const cwd = fileURLToPath(new URL('..', import.meta.url));
const cli = fileURLToPath(new URL('../src/main.js', import.meta.url));
const fixture = fileURLToPath(new URL('./fixture.js', import.meta.url));
async function until(predicate, description, timeout = 8000) {
    const end = Date.now() + timeout;
    while (!predicate()) {
        if (Date.now() > end) throw new Error(`Timeout: ${description}`);
        await new Promise(resolve => setTimeout(resolve, 20));
    }
}

async function start(t, args = []) {
    const process = pty.spawn(globalThis.process.execPath, [cli, '--pi', fixture, ...args], { cwd, cols: 110, rows: 30, env: childEnvironment() });
    const screen = new Screen(110, 30, data => process.write(data), () => {}, () => {});
    let raw = '', exit;
    process.onData(data => { raw += data; screen.write(data); });
    process.onExit(value => { exit = value; });
    t.after(async () => {
        if (!exit) { process.kill('SIGTERM'); await until(() => exit, 'host cleanup'); }
        screen.dispose();
    });
    const visible = () => screen.lines().join('\n').replace(/\x1b\[[0-9;]*m/g, '');
    await until(() => visible().includes('PID '), 'host startup');
    return { process, screen, visible, raw: () => raw, exit: () => exit };
}

test('host maps pane drag/scroll and never forwards child clipboard', { timeout: 15000 }, async t => {
    const host = await start(t, ['--', '--mouse-test']);
    assert.ok(host.raw().includes('\x1b[?1003h'));
    host.process.write('\x1b[<0;18;1M');
    await until(() => host.visible().includes(Buffer.from('\x1b[<0;1;1M').toString('hex')), 'translated press');
    host.process.write('\x1b[<32;20;1M\x1b[<0;20;1m');
    await until(() => host.visible().includes(Buffer.from('\x1b[<0;3;1m').toString('hex')), 'translated release');
    assert.ok(!host.raw().includes('\x1b]52;'));
    host.process.write('\x1b[<64;18;2M');
    await until(() => host.visible().includes(Buffer.from('\x1b[<64;1;2M').toString('hex')), 'translated wheel');
    host.process.write('\x1b[<0;3;30M');
    await until(() => /2 working/.test(host.visible()), 'new current pane');
    assert.ok(!host.raw().includes('\x1b]52;c;RVZJTA=='), 'no unsolicited current/background OSC 52');
    host.process.kill('SIGTERM');
    await until(() => host.exit(), 'host exit');
    assert.ok(host.raw().includes('\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?1006l'));
});

test('sidebar mouse toggle resizes panes and captures complete gestures', { timeout: 15000 }, async t => {
    const host = await start(t, ['--', '--mouse-test']);
    host.process.write('\x1b[<0;16;30M\x1b[<0;16;30m');
    await until(() => host.visible().includes('»') && host.visible().includes('SIZE 110x30'), 'collapsed full width');
    assert.ok(!host.visible().includes('Sessions'));
    assert.ok(!host.visible().includes(Buffer.from('\x1b[<0;16;30m').toString('hex')), 'toggle release isolated');
    host.process.write('\x1b[<0;8;2M\x1b[<0;8;2m');
    await until(() => host.visible().includes(Buffer.from('\x1b[<0;8;2m').toString('hex')), 'ordinary collapsed input');
    host.process.write('\x1b[<0;4;1M\x1b[<0;4;1m');
    await until(() => host.visible().includes('Sessions') && host.visible().includes('SIZE 93x30'), 'expanded resize');
    assert.ok(!host.visible().includes(Buffer.from('\x1b[<0;4;1m').toString('hex')), 'expanded hit release isolated');
    assert.ok(!host.visible().includes('»'));
    host.process.write('\x1b[<0;15;30M\x1b[<0;15;30m');
    await until(() => host.visible().includes('»'), 'collapsed again');
    host.screen.resize(60, 20); host.process.resize(60, 20);
    await until(() => host.visible().includes('SIZE 60x20') && !host.visible().includes('»'), 'narrow hides unusable pill');
    host.screen.resize(110, 30); host.process.resize(110, 30);
    await until(() => host.visible().includes('»'), 'manual collapse survives resize');
    host.process.write('\x1b[<0;1;1M\x1b[<0;1;1m');
    await until(() => host.visible().includes('Sessions'), 'can expand after resize');
});

test('mouse-only host: scrolling, keyboard passthrough and full height', {timeout:15000}, async t => {
 const host = await start(t);
 const first = Number(host.visible().match(/PID (\d+)/)[1]);
 assert.ok(!host.visible().includes('Ctrl+G'));
 for (let i=2;i<=10;i++) {
   host.process.write('\x1b[<0;3;30M');
   await until(()=>new RegExp(`(?:^|\\n)\\s*${i} (?:starting|working)`).test(host.visible()), 'new '+i);
 }
 host.screen.resize(80,10); host.process.resize(80,10);
 await until(()=>/10 working/.test(host.visible()), 'selected visible');
 await until(()=>host.visible().includes('SIZE 63x10'), 'child uses full height');
 host.process.write('\x1b[<64;4;8M\x1b[<64;4;8M\x1b[<64;4;8M');
 await until(()=>/(?:^|\n)\s*1 working/.test(host.visible()), 'scroll up');
 host.process.write('\x1b[<0;4;3M');
 await until(()=>host.visible().includes('PID '+first), 'first clicked');
 host.process.write('\x07n');
 await until(()=>host.visible().includes('INPUT 076e'), 'old shortcut passed through');
 assert.doesNotMatch(host.visible(), /复制选区|关闭当前会话|确认关闭/);
 host.process.write('quit');
 await until(()=>host.visible().includes('PID ') && !host.visible().includes('PID '+first), 'native exit removes row');
 host.screen.resize(65,20); host.process.resize(65,20);
 await until(()=>!host.visible().includes('Sessions'), 'narrow');
 host.process.write('i');
 await until(()=>host.visible().includes('INPUT 69'), 'narrow input');
 host.process.kill('SIGTERM');
 await until(()=>host.exit(), 'cleanup');
 assert.ok(host.raw().includes('\x1b[?1049l'));
});

test('last natural session exit exits host and restores terminal', { timeout: 10000 }, async t => {
    const host = await start(t);
    const first = Number(host.visible().match(/PID (\d+)/)[1]);
    host.process.write('\x1b[<0;3;30M');
    await until(() => /2 working/.test(host.visible()) && /PID (\d+)/.test(host.visible()) && Number(host.visible().match(/PID (\d+)/)?.[1]) !== first, 'second ready');
    const second = Number(host.visible().match(/PID (\d+)/)[1]);
    host.process.write('quit');
    await until(() => !/2 working/.test(host.visible()) && host.visible().includes(`PID ${first}`), 'exited row removed and neighbour selected');
    assert.equal(host.exit(), undefined);
    assert.doesNotMatch(host.visible(), /exited/);
    host.process.write('quit');
    await until(() => host.exit(), 'empty host auto exits');
    assert.equal(host.exit().exitCode, 0);
    assert.ok(host.raw().includes('\x1b[?1049l'));
    assert.ok(host.raw().includes('\x1b[?1000l\x1b[?1006l'));
    assert.ok(host.raw().includes('\x1b[?2004l'));
    for (const pid of [first, second]) {
        const record = await identity(pid);
        assert.ok(!record || record.zombie);
    }
});

test('startup exec failure removes the only instance and restores terminal without waiting for input', { timeout: 10000 }, async t => {
    const host = pty.spawn(globalThis.process.execPath, [cli, '--pi', '/not-a-real-pi-executable'], {
        cwd, cols: 110, rows: 30, env: childEnvironment(),
    });
    let raw = '', exit;
    host.onData(data => { raw += data; });
    host.onExit(value => { exit = value; });
    t.after(async () => {
        if (!exit) { host.kill('SIGTERM'); await until(() => exit, 'failed host cleanup'); }
    });
    await until(() => exit, 'failed child auto exits host');
    assert.ok(raw.includes('\x1b[?1049l'));
    assert.ok(raw.includes('\x1b[?1000l\x1b[?1006l'));
});

test('external SIGTERM triggers child cleanup and terminal restoration', { timeout: 10000 }, async t => {
    const host = await start(t);
    const pid = Number(host.visible().match(/PID (\d+)/)[1]);
    host.process.kill('SIGTERM');
    await until(() => host.exit(), 'signal exit');
    assert.ok(host.raw().includes('\x1b[?1049l'));
    assert.ok(host.raw().includes('\x1b[?1000l\x1b[?1006l'));
    const record = await identity(pid);
    assert.ok(!record || record.zombie);
});
