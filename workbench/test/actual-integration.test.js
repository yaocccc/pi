import test from 'node:test';
import assert from 'node:assert/strict';
import { fileURLToPath } from 'node:url';
import { Workbench } from '../src/session.js';
import { frame, layout } from '../src/view.js';

const cwd = fileURLToPath(new URL('..', import.meta.url));
const text = session => session.screen.lines().join('\n').replace(/\x1b\[[0-9;]*m/g, '');
async function until(predicate, label, timeout = 8000) {
    const end = Date.now() + timeout;
    while (!predicate()) {
        if (Date.now() > end) throw new Error('Timed out: ' + label);
        await new Promise(resolve => setTimeout(resolve, 20));
    }
}

test('real local ask_question + explicit name lifecycle in two real Pi processes (offline)',
    { skip: process.env.PI_WORKBENCH_REAL_SMOKE !== '1', timeout: 45000 }, async t => {
        const manager = new Workbench({
            command: process.env.PI_WORKBENCH_PI ?? 'pi', cwd, bridge: true,
            args: ['--tui-mode', 'fullscreen', '--offline', '--no-session', '--no-extensions', '--no-mcp',
                '--no-context-files', '--no-skills', '--no-prompt-templates', '--no-themes', '--no-approve',
                '-e', fileURLToPath(new URL('./actual-question-extension.ts', import.meta.url))],
        }, () => {}, 90, 25);
        t.after(() => manager.dispose());
        const a = manager.create(), b = manager.create();
        await until(() => [a,b].every(s => s.state === 'idle'), 'both Pi started', 20000);

        manager.select(0);
        manager.input('/name 真正的会话名称很长需要省略中文和emoji 🧑‍💻\r');
        await until(() => a.name === '真正的会话名称很长需要省略中文和emoji 🧑‍💻', 'native /name while idle');
        assert.equal(b.name, '', 'unnamed metadata remains process-local');
        assert.ok(frame(manager, layout(111,25),111,'').join('\n').includes('…'), 'sidebar truncates actual long name');

        manager.input('/wb-real-question rename\r');
        await until(() => a.pendingQuestions === 1 && a.state === 'blocked' && text(a).includes('真实提问 1'), 'REAL local question waiting');
        await until(() => a.name === '等待期间异步命名 🧑‍💻', 'async setSessionName during blocked');
        assert.ok(frame(manager, layout(111,25),111,'').join('\n').includes('等待回答'));
        // A native working update must not override the explicit outstanding question.
        a.screen.write('\x1b]7501;state=working\x07');
        await a.screen.flush();
        assert.equal(a.state, 'blocked');
        manager.select(1);
        manager.input('/wb-async-name 后台独立名称\r');
        await until(() => b.name === '后台独立名称', 'asynchronous name in second Pi');
        assert.equal(a.pendingQuestions, 1);
        assert.equal(a.state, 'blocked', 'background prompt never auto-answered');
        manager.select(0);
        manager.input('\r');
        await until(() => a.pendingQuestions === 0, 'answer clears pending');
        assert.equal(a.state, a.nativeState, 'restore latest native status, not a hardcoded state');
        assert.equal(a.name, '等待期间异步命名 🧑‍💻');

        manager.input('/wb-real-question queue\r');
        await until(() => a.pendingQuestions === 2 && text(a).includes('真实提问 1'), 'two queued real questions');
        manager.input('\r');
        await until(() => a.pendingQuestions === 1 && text(a).includes('真实提问 2'), 'next real question owns UI');
        assert.equal(a.state, 'blocked');
        manager.input('\x1b');
        await until(() => a.pendingQuestions === 0, 'Escape cancellation clears last question');

        manager.input('/wb-real-question abort\r');
        await until(() => a.pendingQuestions === 2, 'abort fixture has active and queued question');
        await until(() => a.pendingQuestions === 0, 'abort clears active and queued questions');
        assert.equal(a.state, a.nativeState);

        manager.input('/wb-async-name 空闲异步改名\r');
        await until(() => a.name === '空闲异步改名', 'idle asynchronous naming without another turn');
        manager.input('/wb-real-question\r');
        await until(() => a.pendingQuestions === 1, 'question before process close');
        await manager.close(a);
        assert.equal(manager.active, b);
        assert.equal(b.pendingQuestions, 0, 'closing pending instance cannot contaminate another instance');
    });
