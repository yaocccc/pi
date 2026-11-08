import xterm from '@xterm/headless';
import unicode from '@xterm/addon-unicode11';

const { Terminal } = xterm;
const { Unicode11Addon } = unicode;
const STATES = new Set(['idle', 'working', 'blocked', 'done', 'error']);

/** Pi's OSC 7501 root status uses colon-separated pairs and a base64 msg. */
export function parseStatus(/** @type {string} */ body) {
    if (body.length > 4096) return undefined;
    const values = Object.fromEntries(body.split(':').map(part => {
        const split = part.indexOf('=');
        return split < 0 ? [part, ''] : [part.slice(0, split), part.slice(split + 1)];
    }));
    if (!STATES.has(values.state)) return undefined;
    return { state: values.state, message: values.msg ? safeText(Buffer.from(values.msg, 'base64').toString('utf8')) : '' };
}

/** Separate metadata protocol: OSC 777;pi-workbench;<base64 JSON snapshot>. */
export function parseMetadata(/** @type {string} */ body) {
    if (!body.startsWith('pi-workbench;') || body.length > 8192) return undefined;
    try {
        const value = JSON.parse(Buffer.from(body.slice(13), 'base64').toString('utf8'));
        if (value?.version !== 1 || typeof value.name !== 'string' ||
            !Number.isSafeInteger(value.pending) || value.pending < 0) return undefined;
        return { name: safeText(value.name), pending: Number(value.pending) };
    } catch { return undefined; }
}

/** Never let terminal titles/status inject escape sequences into the host. */
export function safeText(/** @type {string} */ text) {
    return text.replace(/[\x00-\x1f\x7f-\x9f\u202a-\u202e\u2066-\u2069]/g, '').slice(0, 160);
}

/** Headless xterm owns VT parsing; only its cell grid is painted into the host. */
export class Screen {
    constructor(/** @type {number} */ cols, /** @type {number} */ rows,
        /** @type {(data: string) => void} */ reply,
        /** @type {(state: {state: string, message: string}) => void} */ status,
        /** @type {() => void} */ changed,
        /** @type {(metadata: {name: string, pending: number}) => void} */ metadata = () => {}) {
        this.terminal = new Terminal({ cols, rows, allowProposedApi: true, scrollback: 2000 });
        this.terminal.loadAddon(new Unicode11Addon());
        this.terminal.unicode.activeVersion = '11';
        this.cursorVisible = true;
        this.sgrMouse = false;
        this.disposed = false;
        this.terminal.onData(reply);
        // Capability and color replies stay local to the originating child.
        this.terminal.parser.registerOscHandler(7501, body => {
            if (body === '?') reply('\x1b]7501;?\x1b\\');
            else { const value = parseStatus(body); if (value) status(value); }
            return true;
        });
        this.terminal.parser.registerOscHandler(777, body => {
            const value = parseMetadata(body);
            if (value) metadata(value);
            return true;
        });
        for (const [id, rgb] of [[10, 'dddd/dddd/dddd'], [11, '1616/1b1b/2222']]) {
            this.terminal.parser.registerOscHandler(Number(id), body => {
                if (body === '?') reply(`\x1b]${id};rgb:${rgb}\x1b\\`);
                return true;
            });
        }
        for (const final of ['h', 'l']) {
            this.terminal.parser.registerCsiHandler({ prefix: '?', final }, params => {
                if (params.includes(25)) this.cursorVisible = final === 'h';
                if (params.includes(1006)) this.sgrMouse = final === 'h';
                return false; // xterm must still process all native modes.
            });
        }
        // Never relay child clipboard writes (including those from background panes).
        this.terminal.parser.registerOscHandler(52, () => true);
        this.terminal.parser.registerEscHandler({ final: 'c' }, () => { this.sgrMouse = false; return false; });
        this.terminal.onWriteParsed(changed);
    }
    write(/** @type {string} */ data, /** @type {() => void} */ parsed = () => {}) {
        if (!this.disposed) this.terminal.write(data, parsed);
        else parsed();
    }
    async flush() {
        if (!this.disposed) await new Promise(resolve => this.terminal.write('', () => resolve(undefined)));
    }
    resize(/** @type {number} */ cols, /** @type {number} */ rows) {
        if (!this.disposed) this.terminal.resize(cols, rows);
    }
    lines() {
        const { terminal } = this;
        const buffer = terminal.buffer.active;
        return Array.from({ length: terminal.rows }, (_, y) => {
            const line = buffer.getLine(buffer.viewportY + y);
            if (!line) return ' '.repeat(terminal.cols);
            let result = '', previous = '';
            for (let x = 0; x < terminal.cols; x++) {
                const cell = line.getCell(x);
                if (!cell || cell.getWidth() === 0) continue;
                const style = cellStyle(cell);
                if (previous !== style) { result += style; previous = style; }
                result += cell.getChars() || ' ';
            }
            return result + '\x1b[0m';
        });
    }
    dispose() { this.disposed = true; this.terminal.dispose(); }
}

function cellStyle(/** @type {import('@xterm/headless').IBufferCell} */ cell) {
    const codes = [0];
    for (const [flag, code] of [[cell.isBold(), 1], [cell.isDim(), 2], [cell.isItalic(), 3],
        [cell.isUnderline(), 4], [cell.isBlink(), 5], [cell.isInverse(), 7],
        [cell.isInvisible(), 8], [cell.isStrikethrough(), 9]]) if (flag) codes.push(code);
    /** @type {[boolean, boolean, number, number][]} */
    const colors = [
        [cell.isFgRGB(), cell.isFgPalette(), cell.getFgColor(), 38],
        [cell.isBgRGB(), cell.isBgPalette(), cell.getBgColor(), 48],
    ];
    for (const [rgb, palette, value, base] of colors) {
        if (rgb) codes.push(base, 2, (value >> 16) & 255, (value >> 8) & 255, value & 255);
        else if (palette) codes.push(base, 5, value);
    }
    return `\x1b[${codes.join(';')}m`;
}
