import * as pty from 'node-pty';
import { fileURLToPath } from 'node:url';
import { Screen } from './screen.js';
import { descendants, identity, signalGroup, signalIdentity } from './process-tree.js';

/** @typedef {{command: string, args: string[], cwd: string, env?: Record<string, string>, bridge?: boolean, cols: number, rows: number}} Launch */

export function childEnvironment(/** @type {NodeJS.ProcessEnv} */ source = process.env) {
    /** @type {Record<string, string>} */
    const env = {};
    for (const [key, value] of Object.entries(source)) {
        // These identify the enclosing pane/session, not a newly launched Pi.
        if (value === undefined || /^(HERDR_|KITTY_|WEZTERM_)/.test(key)
            || ['TMUX', 'TMUX_PANE', 'STY', 'WINDOWID', 'PI_SESSION_ID', 'PI_SESSION_FILE'].includes(key)) continue;
        env[key] = value;
    }
    return { ...env, TERM: 'xterm-256color', COLORTERM: 'truecolor', TERM_PROGRAM: 'pi-workbench',
        PI_PROGRAM_STATUS: '1', PI_IMAGE_PROTOCOL: 'none' };
}

export class Session {
    constructor(/** @type {number} */ id, /** @type {Launch} */ launch, /** @type {() => void} */ changed,
        /** @type {(session: Session) => void} */ ended) {
        this.id = id;
        this.name = '';
        this.state = 'starting';
        this.nativeState = 'starting';
        this.pendingQuestions = 0;
        this.message = '';
        this.exited = false;
        this.disposed = false;
        /** @type {Promise<void> | undefined} */ this.closing = undefined;
        /** @type {pty.IPty | undefined} */ this.process = undefined;
        this.screen = new Screen(launch.cols, launch.rows, data => this.write(data), status => {
            if (!this.exited && !this.closing) {
                this.nativeState = status.state;
                this.message = status.message;
                this.state = this.pendingQuestions ? 'blocked' : this.nativeState;
                changed();
            }
        }, changed, metadata => {
            if (this.exited || this.closing || !launch.bridge) return;
            this.name = metadata.name.trim();
            this.pendingQuestions = metadata.pending;
            this.state = this.pendingQuestions ? 'blocked' : this.nativeState;
            changed();
        });
        this.exit = new Promise(resolve => { this.resolveExit = resolve; });
        try {
            const args = launch.bridge ? [...launch.args, '-e', fileURLToPath(new URL('./bridge.js', import.meta.url))] : launch.args;
            this.process = pty.spawn(launch.command, args, {
                name: 'xterm-256color', cols: launch.cols, rows: launch.rows, cwd: launch.cwd,
                env: { ...(launch.env ?? childEnvironment()), ...(launch.bridge ? { PI_WORKBENCH_BRIDGE: '1' } : {}) }, handleFlowControl: false,
            });
            this.identity = identity(this.process.pid);
            let queuedBytes = 0;
            this.dataListener = this.process.onData(data => {
                queuedBytes += data.length;
                if (queuedBytes > 1024 * 1024) this.process?.pause();
                this.screen.write(data, () => {
                    queuedBytes -= data.length;
                    if (queuedBytes < 256 * 1024 && !this.exited && !this.closing) this.process?.resume();
                });
            });
            this.exitListener = this.process.onExit(event => {
                if (this.exited) return;
                this.exited = true;
                this.state = event.exitCode === 0 && !event.signal ? 'exited' : 'error';
                this.message = event.signal ? `signal ${event.signal}` : `exit ${event.exitCode}`;
                this.resolveExit?.(event);
                ended(this);
            });
        } catch (error) {
            this.screen.dispose();
            throw error;
        }
    }
    write(/** @type {string} */ data) {
        if (this.process && !this.exited && !this.closing) this.process.write(data);
    }
    resize(/** @type {number} */ cols, /** @type {number} */ rows) {
        if (this.disposed) return;
        this.screen.resize(cols, rows);
        if (!this.exited) this.process?.resize(cols, rows);
    }
    close() {
        return this.closing ??= this.cleanup();
    }
    async cleanup() {
        if (!this.process) return;
        const pid = this.process.pid;
        const root = await this.identity;
        this.state = 'closing';
        const current = await identity(pid);
        const tree = root && current?.born === root.born ? await descendants(pid, root.born) : [];
        // Include detached descendants before the root can exit and reparent them.
        await signalGroup(pid, root?.born, 'SIGTERM');
        await Promise.all(tree.map(record => signalIdentity(record, 'SIGTERM')));
        // Give Pi and its tools time to shut down; retain the original identities.
        await new Promise(resolve => setTimeout(resolve, 600));
        await signalGroup(pid, root?.born, 'SIGKILL');
        await Promise.all(tree.map(record => signalIdentity(record, 'SIGKILL')));
        /** @type {ReturnType<typeof setTimeout> | undefined} */ let timer;
        await Promise.race([this.exit, new Promise(resolve => { timer = setTimeout(resolve, 500); })]);
        clearTimeout(timer);
        this.dataListener?.dispose();
        this.exitListener?.dispose();
        this.screen.dispose();
        this.disposed = true;
    }
}

/** Independent live instances, not a SessionManager history query. */
export class Workbench {
    constructor(/** @type {Omit<Launch, 'cols' | 'rows'>} */ launch, /** @type {() => void} */ changed,
        /** @type {number} */ cols, /** @type {number} */ rows,
        /** @type {() => void} */ empty = () => {}) {
        this.empty = empty;
        /** @type {Set<Promise<void>>} */ this.pending = new Set();
        /** @type {unknown[]} */ this.failures = [];
        /** @type {Promise<void> | undefined} */ this.disposing = undefined;
        this.launch = launch;
        this.changed = changed;
        this.cols = cols;
        this.rows = rows;
        /** @type {Session[]} */ this.sessions = [];
        this.selected = 0;
        this.nextId = 1;
        this.stopping = false;
    }
    get active() { return this.sessions[this.selected]; }
    create() {
        if (this.stopping) return;
        const session = new Session(this.nextId++, { ...this.launch, cols: this.cols, rows: this.rows }, this.changed,
            ended => { this.trackClose(ended); this.remove(ended); });
        this.sessions.push(session);
        this.selected = this.sessions.length - 1;
        this.changed();
        return session;
    }
    select(/** @type {number} */ index) {
        if (index >= 0 && index < this.sessions.length) { this.selected = index; this.changed(); }
    }
    input(/** @type {string} */ data) { this.active?.write(data); }
    resize(/** @type {number} */ cols, /** @type {number} */ rows) {
        this.cols = cols; this.rows = rows;
        for (const session of this.sessions) session.resize(cols, rows);
        this.changed();
    }
    trackClose(/** @type {Session} */ session) {
        const closing = session.close();
        if (!this.pending.has(closing) && !session.disposed) {
            this.pending.add(closing);
            void closing.then(() => this.pending.delete(closing), error => {
                this.pending.delete(closing);
                this.failures.push(error);
            });
        }
        return closing;
    }
    remove(/** @type {Session} */ session) {
        const index = this.sessions.indexOf(session);
        if (index < 0) return;
        const active = this.active;
        this.sessions.splice(index, 1);
        this.selected = active === session ? Math.min(index, this.sessions.length - 1) : this.sessions.indexOf(active);
        this.selected = Math.max(0, this.selected);
        this.changed();
        if (!this.sessions.length && !this.stopping) this.empty();
    }
    async close(/** @type {Session} */ session) {
        await this.trackClose(session);
        this.remove(session);
    }
    dispose() {
        return this.disposing ??= this.cleanup();
    }
    async cleanup() {
        this.stopping = true;
        for (const session of this.sessions) this.trackClose(session);
        await Promise.allSettled([...this.pending]);
        this.sessions.length = 0;
        this.selected = 0;
        if (this.failures.length) throw new AggregateError(this.failures, 'Some child processes failed to stop');
    }
}
