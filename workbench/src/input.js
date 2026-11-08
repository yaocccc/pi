/** Only mouse reports are host-owned; all keyboard bytes reach Pi. */
export class InputRouter {
    constructor(/** @type {(data: string) => void} */ send,
        /** @type {() => void} */ changed = () => {},
        /** @type {(event: {button:number,x:number,y:number,release:boolean}) => void} */ mouse = () => {}) {
        this.send = send;
        this.changed = changed;
        this.mouse = mouse;
        this.pending = '';
        this.pasting = false;
        /** @type {ReturnType<typeof setTimeout> | undefined} */ this.timer = undefined;
    }
    feed(/** @type {string} */ data) {
        clearTimeout(this.timer);
        this.pending += data;
        const start = '\x1b[200~', end = '\x1b[201~';
        let output = '';
        const flush = () => { if (output) { this.send(output); output = ''; } };
        while (this.pending) {
            const marker = this.pasting ? end : start;
            if (this.pending.startsWith(marker)) {
                this.pending = this.pending.slice(marker.length);
                this.pasting = !this.pasting;
                output += marker;
                this.changed();
                continue;
            }
            if (marker.startsWith(this.pending)) break;
            if (!this.pasting && this.pending.startsWith('\x1b[<')) {
                const match = /^\x1b\[<(\d+);(\d+);(\d+)([Mm])/.exec(this.pending);
                if (match) {
                    flush();
                    this.pending = this.pending.slice(match[0].length);
                    this.mouse({ button: Number(match[1]), x: Number(match[2]) - 1,
                        y: Number(match[3]) - 1, release: match[4] === 'm' });
                    this.changed();
                    continue;
                }
                // Once identified as a mouse report, never time out into child input.
                if (/^\x1b\[<[\d;]*$/.test(this.pending) && this.pending.length < 128) break;
                this.pending = this.pending.replace(/^\x1b\[<[^A-Za-z]*[A-Za-z]?/, '');
                continue;
            }
            const char = this.pending[0];
            this.pending = this.pending.slice(1);
            output += char;
        }
        flush();
        // A lone Escape remains a usable child key; do not wait forever for a paste marker.
        if (this.pending && !this.pending.startsWith('\x1b[<')) this.timer = setTimeout(() => {
            this.send(this.pending); this.pending = '';
        }, 60);
    }
    dispose() { clearTimeout(this.timer); }
}
