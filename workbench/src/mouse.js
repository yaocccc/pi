/** @typedef {{button:number,x:number,y:number,release:boolean}} MouseEvent */
/** @typedef {import('./session.js').Session} Session */
/** @typedef {ReturnType<import('./view.js').layout>} Layout */

/** Encode only events requested by the child, in its negotiated protocol. */
export function mouseReport(/** @type {Session} */ session, /** @type {MouseEvent} */ event) {
    const mode = session.screen.terminal.modes.mouseTrackingMode;
    const motion = Boolean(event.button & 32), wheel = Boolean(event.button & 64);
    if (mode === 'none' || (mode === 'x10' && (event.release || motion || wheel)) ||
        (motion && mode !== 'any' && !(mode === 'drag' && (event.button & 3) !== 3))) return '';
    if (session.screen.sgrMouse) return `\x1b[<${event.button};${event.x + 1};${event.y + 1}${event.release ? 'm' : 'M'}`;
    if (event.x >= 223 || event.y >= 223) return ''; // Legacy X10 has a byte-sized coordinate range.
    const button = event.release ? (event.button & 28) | 3 : event.button;
    return '\x1b[M' + String.fromCharCode(button + 32, event.x + 33, event.y + 33);
}

/** Route right-pane mouse gestures; clipboard operations remain owned by Pi/terminal. */
export class PaneMouse {
    /** @type {{session:Session,button:number,x:number,y:number} | undefined} */ capture;
    reset() {
        if (this.capture) {
            const { session, button, x, y } = this.capture;
            session.write(mouseReport(session, { button, x, y, release: true }));
        }
        this.capture = undefined;
    }
    /** Returns true when this event belongs to the pane, not the sidebar. */
    handle(/** @type {MouseEvent} */ event, /** @type {Session | undefined} */ active, /** @type {Layout} */ size) {
        if (this.capture && this.capture.session !== active) this.reset();
        if (!active) return false;
        const inside = event.x >= size.left && event.x < size.left + size.cols && event.y >= 0 && event.y < size.rows;
        if (!inside && !this.capture) return false;
        const local = { ...event, x: Math.max(0, Math.min(size.cols - 1, event.x - size.left)),
            y: Math.max(0, Math.min(size.rows - 1, event.y)) };
        const motion = Boolean(event.button & 32), wheel = Boolean(event.button & 64);
        if (!event.release && !motion && !wheel) {
            this.capture = { session: active, button: event.button, x: local.x, y: local.y };
        }
        if (this.capture) {
            this.capture.x = local.x; this.capture.y = local.y;
        }
        active.write(mouseReport(active, local));
        if (event.release) this.capture = undefined;
        return true;
    }
}
