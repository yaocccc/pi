import test from 'node:test';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { Workbench, childEnvironment } from '../src/session.js';
import { identity } from '../src/process-tree.js';

const cwd = fileURLToPath(new URL('..', import.meta.url));
const fixture = fileURLToPath(new URL('./fixture.js', import.meta.url));
export const text = session => session.screen.terminal.buffer.active.getLine(0) ? session.screen.lines().join('\n').replace(/\x1b\[[0-9;]*m/g, '') : '';
export async function waitFor(predicate, message = 'condition', timeout = 5000) {
    const until = Date.now() + timeout;
    while (!predicate()) {
        if (Date.now() > until) throw new Error(`Timed out: ${message}`);
        await new Promise(resolve => setTimeout(resolve, 20));
    }
}

function manager(t, launch = { command: process.execPath, args: [fixture], cwd }) {
    const workbench = new Workbench(launch, () => {}, 72, 16);
    t.after(() => workbench.dispose());
    return workbench;
}

test('two actual PTYs keep running independently; input, status, resize, selection and cleanup', async t => {
    const workbench = manager(t);
    const first = workbench.create();
    const second = workbench.create();
    const pids = [first.process.pid, second.process.pid];
    assert.notEqual(...pids);
    await waitFor(() => workbench.sessions.every(session => session.state === 'working'), 'both fixtures ready');
    workbench.select(0);
    workbench.input('alpha-b');
    await waitFor(() => first.state === 'blocked');
    await waitFor(() => /TICK (\d+)/.test(text(first)), 'first heartbeat');
    const previous = Number(text(first).match(/TICK (\d+)/)[1]);
    workbench.select(1);
    workbench.input('zeta-e');
    await waitFor(() => second.state === 'error');
    await waitFor(() => Number(text(first).match(/TICK (\d+)/)?.[1]) > previous + 2, 'background child still updating');
    assert.match(text(first), new RegExp(Buffer.from('alpha-b').toString('hex')));
    assert.doesNotMatch(text(first), new RegExp(Buffer.from('zeta-e').toString('hex')));
    assert.match(text(second), new RegExp(Buffer.from('zeta-e').toString('hex')));
    workbench.resize(52, 12);
    await waitFor(() => workbench.sessions.every(session => text(session).includes('SIZE 52x12')));
    assert.equal(first.screen.terminal.cols, 52);
    assert.equal(second.screen.terminal.rows, 12);
    workbench.input('d');
    await waitFor(() => /DESCENDANT (\d+)/.test(text(second)));
    const descendantPid = Number(text(second).match(/DESCENDANT (\d+)/)[1]);
    await workbench.close(second);
    assert.equal(workbench.active, first);
    const descendant = await identity(descendantPid);
    assert.ok(!descendant || descendant.zombie, 'detached descendant terminated');
    assert.equal(first.exited, false, 'closing another session never aborts first');
    await workbench.dispose();
    for (const pid of pids) {
        const record = await identity(pid);
        assert.ok(!record || record.zombie, `owned process ${pid} terminated`);
    }
});

test('native OSC status messages are never treated as session names', async t => {
    const workbench = manager(t, { command: process.execPath, args: ['-e', 'setInterval(()=>{},1000)'], cwd });
    const session = workbench.create();
    for (const state of ['working', 'done', 'blocked', 'error', 'idle']) {
        session.screen.write('\x1b]7501;state=' + state + ':msg=' + Buffer.from('不是结构化名称').toString('base64') + '\x07');
        await session.screen.flush();
        assert.equal(session.name, '');
        assert.equal(session.state, state);
    }
});

test('structured names update and clear without a fallback', async t => {
    const workbench = manager(t, { command: process.execPath, args: ['-e', 'setInterval(()=>{},1000)'], cwd, bridge: true });
    const session = workbench.create();
    assert.equal(session.name, '');
    for (const name of ['真实名称', '']) {
        const payload = Buffer.from(JSON.stringify({ version: 1, name, pending: 0 })).toString('base64');
        session.screen.write(`\x1b]777;pi-workbench;${payload}\x07`);
        await session.screen.flush();
        assert.equal(session.name, name);
    }
});

test('seven independent PTYs can be created, selected and cleaned up without a fixed limit', async t => {
    const workbench = manager(t);
    const sessions = Array.from({ length: 7 }, () => workbench.create());
    const pids = sessions.map(session => session.process.pid);
    assert.equal(new Set(pids).size, 7);
    await waitFor(() => sessions.every(session => session.state === 'working'));
    assert.equal(workbench.sessions.length, 7);
    for (let i = 0; i < 7; i++) {
        workbench.select(i);
        assert.equal(workbench.active, sessions[i]);
        assert.equal(sessions[i].exited, false);
    }
    await workbench.dispose();
    for (const pid of pids) {
        const record = await identity(pid);
        assert.ok(!record || record.zombie);
    }
});

test('bad executable is removed; child environment drops enclosing pane IDs only', async t => {
    const workbench = manager(t, { command: '/not-a-real-pi-executable', args: [], cwd });
    const failed = workbench.create();
    await waitFor(() => failed.exited, 'exec failure reported asynchronously by node-pty');
    assert.equal(workbench.sessions.includes(failed), false);
    await workbench.close(failed);
    assert.equal(workbench.sessions.length, 0);
    const env = childEnvironment({ HERDR_PANE_ID: 'outer', PI_SESSION_ID: 'outer', PI_SESSION_FILE: 'outer',
        PATH: '/bin', PI_WORKER_DEPTH: '1', EXAMPLE_SECRET: 'retained-for-child' });
    assert.equal(env.HERDR_PANE_ID, undefined);
    assert.equal(env.PI_SESSION_ID, undefined);
    assert.equal(env.PI_SESSION_FILE, undefined);
    assert.equal(env.PI_WORKER_DEPTH, '1', 'do not bypass worker restrictions');
    assert.equal(env.PI_PROGRAM_STATUS, '1');
    assert.equal(env.PI_IMAGE_PROTOCOL, 'none');
});

test('natural exits remove rows, preserve selection, pick a neighbour and notify empty once', async t => {
    let empty = 0;
    const workbench = new Workbench({ command: process.execPath, args: [fixture], cwd }, () => {}, 72, 16, () => empty++);
    t.after(() => workbench.dispose());
    const [first, second, third] = Array.from({ length: 3 }, () => workbench.create());
    await waitFor(() => workbench.sessions.every(session => session.state === 'working'));
    workbench.select(1);
    first.write('quit');
    await waitFor(() => workbench.sessions.length === 2);
    assert.equal(workbench.active, second);
    assert.equal(workbench.selected, 0);
    second.write('quit');
    await waitFor(() => workbench.sessions.length === 1);
    assert.equal(workbench.active, third);
    third.write('quit');
    await waitFor(() => workbench.sessions.length === 0);
    assert.equal(empty, 1);
    workbench.remove(third);
    await workbench.close(third);
    assert.equal(empty, 1, 'duplicate removal does not notify again');
    await Promise.all([workbench.dispose(), workbench.dispose()]);
    assert.ok([first, second, third].every(session => session.disposed));
});

test('concurrent natural exits and disposal drain removed sessions without duplicate empty notifications', async t => {
    let empty = 0;
    const workbench = new Workbench({ command: process.execPath, args: [fixture], cwd }, () => {}, 72, 16, () => empty++);
    t.after(() => workbench.dispose());
    const sessions = Array.from({ length: 5 }, () => workbench.create());
    await waitFor(() => sessions.every(session => session.state === 'working'));
    for (const session of sessions) session.write('quit');
    await waitFor(() => workbench.sessions.length === 0);
    await workbench.dispose();
    assert.equal(empty, 1);
    assert.ok(sessions.every(session => session.disposed));
    for (const session of sessions) {
        const record = await identity(session.process.pid);
        assert.ok(!record || record.zombie);
    }
});

test('two real Pi processes: independent native startup screens and OSC status (no model requests)',
    { skip: process.env.PI_WORKBENCH_REAL_SMOKE !== '1', timeout: 30000 }, async t => {
        const workbench = manager(t, { command: process.env.PI_WORKBENCH_PI ?? 'pi', cwd,
            args: ['--tui-mode', 'fullscreen', '--offline', '--no-session', '--no-extensions', '--no-mcp',
                '--no-context-files', '--no-skills', '--no-prompt-templates', '--no-themes', '--no-approve',
                '-e', fileURLToPath(new URL('./question-extension.js', import.meta.url))] });
        const a = workbench.create(), b = workbench.create();
        await waitFor(() => [a, b].every(session => session.state === 'idle'), 'native Pi idle OSC', 20000);
        await waitFor(() => text(a).trim().length > 0 && text(b).trim().length > 0, 'both native Pi screens rendered');
        workbench.select(0);
        workbench.input('/workbench-smoke-question\r');
        await waitFor(() => a.state === 'blocked' && text(a).includes('Workbench offline question'), 'native dialog waiting');
        workbench.select(1);
        workbench.input('UNSENT-TWO');
        await waitFor(() => text(b).includes('UNSENT-TWO'), 'second Pi editor');
        assert.ok(!text(a).includes('UNSENT-TWO'));
        assert.equal(a.state, 'blocked', 'background question stays pending, never auto-answered');
        workbench.select(0);
        workbench.input('\r');
        await waitFor(() => a.state === 'idle', 'native dialog answered only in selected Pi');
        workbench.input('UNSENT-ONE');
        await waitFor(() => text(a).includes('UNSENT-ONE'), 'first Pi editor');
        assert.ok(!text(b).includes('UNSENT-ONE'));
        workbench.resize(84, 22);
        await a.screen.flush(); await b.screen.flush();
        assert.equal(a.screen.terminal.cols, 84);
        assert.equal(b.screen.terminal.rows, 22);
        assert.equal(a.exited, false); assert.equal(b.exited, false);
    });
