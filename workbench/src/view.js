import stringWidth from 'string-width';
import { safeText } from './screen.js';

export const NEW_SESSION_LABEL = '[ + 新建会话 ]';
export const SIDEBAR_WIDTH = stringWidth(NEW_SESSION_LABEL) + 2;
export const SIDEBAR_COLORS = {
    text: '200;200;204', muted: '153;158;163', accent: '183;151;229',
    selected: '82;85;88', separator: '91;99;104',
};
/** @type {Record<string, string>} */
export const STATE_COLORS = {
    idle: '116;192;133', working: '183;151;229', blocked: '221;190;112',
    done: '113;181;166', error: '218;126;126', starting: '153;158;163', closing: '153;158;163',
};
const fg = (/** @type {string} */ color) => `\x1b[38;2;${color}m`;

export function layout(/** @type {number} */ cols, /** @type {number} */ rows, hidden = false) {
    const sidebar = !hidden && cols >= SIDEBAR_WIDTH + 1 + 53 && rows >= 10 ? SIDEBAR_WIDTH : 0;
    return { sidebar, left: sidebar ? sidebar + 1 : 0,
        cols: Math.max(2, cols - (sidebar ? sidebar + 1 : 0)), rows: Math.max(2, rows) };
}

export function fit(/** @type {string} */ value, /** @type {number} */ width) {
    let out = '', used = 0;
    const segmenter = new Intl.Segmenter(undefined, { granularity: 'grapheme' });
    for (const { segment } of segmenter.segment(safeText(value))) {
        const size = stringWidth(segment);
        if (used + size > width) break;
        out += segment; used += size;
    }
    return out + ' '.repeat(Math.max(0, width - used));
}

/** A label-only truncator; keep ordinary layout padding unchanged. */
export function ellipsize(/** @type {string} */ value, /** @type {number} */ width) {
    const text = safeText(value);
    if (width <= 0) return '';
    if (stringWidth(text) <= width) return fit(text, width);
    return fit(text, width - 1).trimEnd() + '…';
}

// Shared row geometry keeps drawing and mouse hit testing in sync.
export const EXPAND_LABEL = '»';
export const EXPAND_WIDTH = stringWidth(EXPAND_LABEL);
export const expandPill = () => `\x1b[0m${fg(SIDEBAR_COLORS.text)}${EXPAND_LABEL}\x1b[0m`;

/** Only expose the floating control when expanding is possible. */
export function toggleHit(/** @type {ReturnType<typeof layout>} */ size, /** @type {number} */ cols,
    /** @type {{x:number,y:number}} */ event, hidden = false) {
    if (size.sidebar) return event.y === size.rows - 1 && event.x >= size.sidebar - 4 && event.x < size.sidebar;
    return hidden && Boolean(layout(cols, size.rows).sidebar) && event.y === 0
        && event.x >= 0 && event.x < Math.min(4, cols);
}

export const FIRST_SESSION_ROW = 2;
export const SESSION_ROWS = 2;
const cardCapacity = (/** @type {number} */ rows) => Math.max(0, Math.floor((rows - FIRST_SESSION_ROW - 1) / SESSION_ROWS));

/** Scroll state belongs to the host, not to the individual PTYs. */
export class SidebarViewport {
    offset = 0;
    selected = -1;
    count = -1;
    rows = -1;
    sync(/** @type {number} */ count, /** @type {number} */ selected, /** @type {number} */ rows, reveal = false) {
        const capacity = Math.max(1, cardCapacity(rows));
        if (reveal || selected !== this.selected || count !== this.count || rows !== this.rows) {
            if (selected < this.offset) this.offset = selected;
            if (selected >= this.offset + capacity) this.offset = selected - capacity + 1;
        }
        this.offset = Math.max(0, Math.min(this.offset, count - capacity));
        this.selected = selected; this.count = count; this.rows = rows;
    }
    scroll(/** @type {number} */ delta) {
        this.offset = Math.max(0, Math.min(this.offset + delta, this.count - Math.max(1, cardCapacity(this.rows))));
    }
}

export function sidebarAction(/** @type {ReturnType<typeof layout>} */ size,
    /** @type {number} */ count, /** @type {{button:number,x:number,y:number,release:boolean}} */ event, offset = 0) {
    if (event.y < 0 || event.y >= size.rows || !size.sidebar || event.release || event.button !== 0 || event.x < 0 || event.x >= size.sidebar) return null;
    if (event.y === size.rows - 1) return event.x >= 1 && event.x < 4 ? 'n' : null;
    const row = event.y - FIRST_SESSION_ROW;
    const index = Math.floor(row / SESSION_ROWS);
    return row >= 0 && index < cardCapacity(size.rows) && index + offset < count ? String(index + offset + 1) : null;
}

/** @param {import('./session.js').Workbench} manager */
export function frame(manager, /** @type {ReturnType<typeof layout>} */ size,
    /** @type {number} */ cols, /** @type {string} */ notice, offset = 0) {
    const child = manager.active?.screen.lines() ?? [];
    const end = Math.min(manager.sessions.length, offset + cardCapacity(size.rows));
    const innerWidth = Math.max(0, size.sidebar - 2);
    const side = ['Sessions', notice ? ellipsize(notice, innerWidth) : ''];
    const colors = [SIDEBAR_COLORS.text, SIDEBAR_COLORS.muted];
    manager.sessions.slice(offset, end).forEach((session, index) => {
        const state = session.state === 'blocked' ? '等待回答' : session.state;
        const numberWidth = Math.max(0, innerWidth - stringWidth(state) - 1);
        const ordinal = ellipsize(String(offset + index + 1), numberWidth).trimEnd();
        side.push(`${ordinal} ${state}`, ellipsize(session.name, innerWidth));
        colors.push(STATE_COLORS[session.state] ?? SIDEBAR_COLORS.muted, SIDEBAR_COLORS.text);
    });
    const lines = Array.from({ length: size.rows }, (_, y) => {
        let left = '';
        if (size.sidebar) {
            if (y === size.rows - 1) { side[y] = `new${' '.repeat(innerWidth - 4)}«`; colors[y] = SIDEBAR_COLORS.text; }
            const card = Math.floor((y - FIRST_SESSION_ROW) / SESSION_ROWS);
            const selected = y >= FIRST_SESSION_ROW && card < end - offset && card === manager.selected - offset;
            const background = selected ? `\x1b[48;2;${SIDEBAR_COLORS.selected}m` : '';
            left = '\x1b[0m' + background + fg(colors[y] ?? SIDEBAR_COLORS.text) + (y === 0 ? '\x1b[1m' : '\x1b[22m')
                + ' ' + fit(side[y] ?? '', innerWidth) + ' '
                + '\x1b[0m' + fg(SIDEBAR_COLORS.separator) + '│\x1b[0m';
        }
        return left + (child[y] ?? ' '.repeat(size.cols));
    });

    return lines;
}

/** Only re-paint dirty rows. Output backpressure is handled by the caller. */
export class Painter {
    /** @type {string[]} */ previous = [];
    overlay = '';
    paint(/** @type {string[]} */ lines, /** @type {{x:number,y:number,visible:boolean}} */ cursor, overlay = '') {
        let out = '\x1b[?25l';
        for (let y = 0; y < lines.length; y++) if (lines[y] !== this.previous[y] || (y === 0 && this.overlay !== overlay)) {
            // Erase before drawing: with DECAWM off, a full row leaves the cursor
            // on its last cell; EL after drawing would delete that cell.
            out += `\x1b[${y + 1};1H\x1b[0m\x1b[K${lines[y]}\x1b[0m`;
        }
        this.previous = lines;
        this.overlay = overlay;
        if (overlay) out += `\x1b[1;1H${overlay}`;
        out += `\x1b[${cursor.y + 1};${cursor.x + 1}H`;
        if (cursor.visible && !(overlay && cursor.y === 0 && cursor.x < EXPAND_WIDTH)) out += '\x1b[?25h';
        return out;
    }
    invalidate() { this.previous = []; }
}
