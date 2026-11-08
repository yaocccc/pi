#!/usr/bin/env node
import { parseArgs } from 'node:util';
import { resolve } from 'node:path';
import { stat } from 'node:fs/promises';
import { Workbench } from './session.js';
import { InputRouter } from './input.js';
import { PaneMouse } from './mouse.js';
import { frame, layout, Painter, sidebarAction, SidebarViewport, toggleHit, expandPill } from './view.js';

const separator = process.argv.indexOf('--', 2);
const hostArgs = separator < 0 ? process.argv.slice(2) : process.argv.slice(2, separator);
const piArgs = separator < 0 ? [] : process.argv.slice(separator + 1);
const help = `Pi Workbench (Linux, clickable-sidebar prototype, independent live Pi instances)
Usage: node src/main.js [--cwd DIR] [--pi EXECUTABLE] [--two] [-- PI_OPTIONS...]

Use the sidebar to create and select sessions. All keyboard input goes to Pi.

Click + 新建会话 or a session in the sidebar. Pi receives right-pane mouse/scroll.
Use Pi's native copy support or Shift+drag with your terminal's copy action.
No inline images or enhanced Kitty keyboard in this first prototype.
A switch never stops the other Pi. Closing/exit terminates its process tree.
Sessions share filesystem/config/credentials; there is no worktree isolation.
`;

async function main() {
    const { values } = parseArgs({ args: hostArgs, options: {
        cwd: { type: 'string' }, pi: { type: 'string' }, two: { type: 'boolean' }, help: { type: 'boolean', short: 'h' },
    } });
    if (values.help) { process.stdout.write(help); return; }
    const forbidden = new Set(['--continue', '-c', '--resume', '-r', '--session', '--session-id', '--fork', '--mode', '--print', '-p', '--tui-mode']);
    if (piArgs.some(arg => forbidden.has(arg.split('=')[0]))) {
        throw new Error('Each pane starts a fresh interactive session; session-reuse and alternate output-mode flags are not supported.');
    }
    if (process.platform !== 'linux') throw new Error('This prototype currently requires Linux (/proc process-tree cleanup).');
    if (!process.stdin.isTTY || !process.stdout.isTTY) throw new Error('Run in an interactive terminal, not a pipe or Pi tool output.');
    const cwd = resolve(values.cwd ?? process.cwd());
    if (!(await stat(cwd)).isDirectory()) throw new Error(`Not a directory: ${cwd}`);
    const input = process.stdin, output = process.stdout;
    let hidden = false, toggleCapture = false;
    let size = layout(output.columns, output.rows);
    let dirty = true, blocked = false, stopping = false, notice = '';
    const requestRender = () => { dirty = true; };
    const manager = new Workbench({ command: values.pi ?? 'pi', args: ['--tui-mode', 'fullscreen', ...piArgs], cwd, bridge: true }, requestRender, size.cols, size.rows, () => stop());
    const painter = new Painter();
    const sidebar = new SidebarViewport();
    const syncSidebar = (reveal = false) => sidebar.sync(manager.sessions.length, manager.selected, size.rows, reveal);
    const paneMouse = new PaneMouse();
    const wasRaw = input.isRaw;
    let terminalStarted = false;
    /** @type {ReturnType<typeof setInterval> | undefined} */ let timer;
    /** @type {() => void} */ let finish;
    const done = new Promise(resolve => { finish = () => resolve(undefined); });
    const report = (/** @type {unknown} */ error) => {
        notice = error instanceof Error ? error.message : String(error); requestRender();
    };
    const stop = () => { if (!stopping) { stopping = true; finish(); } };
    const send = (/** @type {string} */ data) => {
        if (stopping) return;
        paneMouse.reset(); manager.input(data);
    };
    const command = (/** @type {string} */ key) => {
        if (stopping) return;
        notice = '';
        try {
            if (key === 'n') { paneMouse.reset(); manager.create(); }
            else if (/^\d+$/.test(key)) { paneMouse.reset(); manager.select(Number(key) - 1); }
        } catch (error) { report(error); }
        syncSidebar(true); requestRender();
    };
    const router = new InputRouter(send, requestRender, event => {
        if (stopping) return;
        // Consume the complete host gesture, including release after the layout changes.
        if (toggleCapture && (event.release || (event.button & 32))) {
            if (event.release) toggleCapture = false;
            return;
        }
        if (!event.release && !(event.button & 96)) toggleCapture = false;
        if (toggleHit(size, output.columns, event, hidden) && !paneMouse.capture) {
            if (event.button === 0 && !event.release) {
                toggleCapture = true;
                hidden = !hidden;
                resize();
                syncSidebar(true);
            }
            return;
        }
        if (paneMouse.handle(event, manager.active, size)) return;
        syncSidebar();
        if (size.sidebar && event.x >= 0 && event.x < size.sidebar && event.y >= 0 && event.y < size.rows
            && !event.release && (event.button === 64 || event.button === 65)) {
            sidebar.scroll(event.button === 64 ? -3 : 3); requestRender(); return;
        }
        const action = sidebarAction(size, manager.sessions.length, event, sidebar.offset);
        if (action) command(action);
    });
    const resize = () => {
        paneMouse.reset();
        size = layout(output.columns, output.rows, hidden);
        painter.invalidate();
        manager.resize(size.cols, size.rows);
    };
    const onInput = (/** @type {string} */ data) => router.feed(data);
    const onDrain = () => { blocked = false; requestRender(); };
    const onSignal = () => { process.exitCode = 130; stop(); };
    const onFatal = (/** @type {Error} */ error) => { report(error); process.exitCode = 1; stop(); };
    const paint = () => {
        if (!dirty || blocked || stopping) return;
        dirty = false;
        if (output.columns < 20 || output.rows < 5) {
            blocked = !output.write('\x1b[2J\x1b[HResize terminal'); return;
        }
        const terminal = manager.active?.screen.terminal;
        const cursor = terminal?.buffer.active;
        syncSidebar();
        const lines = frame(manager, size, output.columns, notice, sidebar.offset);
        blocked = !output.write(painter.paint(lines, {
            x: size.left + Math.min(size.cols - 1, cursor?.cursorX ?? 0),
            y: Math.min(size.rows - 1, cursor?.cursorY ?? 0),
            visible: Boolean(manager.active?.screen.cursorVisible && !manager.active.exited),
        }, hidden && !size.sidebar && layout(output.columns, output.rows).sidebar ? expandPill() : ''));
    };
    try {
        input.setRawMode(true);
        input.setEncoding('utf8');
        input.resume();
        // The host owns screen/paste modes. Never forward raw child control output.
        output.write('\x1b[?1049h\x1b[?7l\x1b[?2004h\x1b[?1002l\x1b[?1003l\x1b[?1000h\x1b[?1006h\x1b[?1003h\x1b[2J');
        terminalStarted = true;
        input.on('data', onInput);
        input.on('end', stop);
        input.on('error', onFatal);
        output.on('resize', resize);
        output.on('drain', onDrain);
        output.on('error', onFatal);
        for (const signal of ['SIGTERM', 'SIGHUP', 'SIGINT']) process.on(signal, onSignal);
        manager.create();
        if (values.two) manager.create();
        timer = setInterval(paint, 33);
        paint();
        await done;
    } finally {
        stopping = true;
        clearInterval(timer);
        router.dispose();
        paneMouse.reset();
        input.off('data', onInput);
        input.off('end', stop);
        input.off('error', onFatal);
        output.off('resize', resize);
        output.off('drain', onDrain);
        try { await manager.dispose(); }
        finally {
            if (terminalStarted) output.write('\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?1006l\x1b[0m\x1b[?25h\x1b[?7h\x1b[?2004l\x1b[?1049l');
            input.setRawMode(wasRaw ?? false);
            input.pause();
            output.off('error', onFatal);
            for (const signal of ['SIGTERM', 'SIGHUP', 'SIGINT']) process.off(signal, onSignal);
        }
    }
}

main().catch(error => { process.stderr.write(`pi-workbench: ${error.message}\n`); process.exitCode = 1; });
