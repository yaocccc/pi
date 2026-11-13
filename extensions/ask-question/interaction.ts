/** Session-owned, frontend-independent FIFO request lifecycle. No answers are sent
 * to pending observers. Cancellation also settles requests from unresponsive ports.
 */
export class QuestionInteractions {
    private active = new Map<AbortController, Promise<void>>();
    private tail: Promise<void> = Promise.resolve();
    private closing = 0;
    private readonly pendingChanged: (count: number) => void;
    constructor(pendingChanged: (count: number) => void = () => {}) { this.pendingChanged = pendingChanged; }
    get pending(): number { return this.active.size; }
    private reportPending() {
        try { this.pendingChanged(this.pending); } catch { /* Observers cannot own request lifetime. */ }
    }
    async shutdown() {
        this.closing++;
        try {
            const active = [...this.active];
            for (const [controller] of active) controller.abort(new Error('用户交互会话已关闭'));
            await Promise.all(active.map(([, completed]) => completed));
        } finally { this.closing--; }
    }
    async run<T>(signal: AbortSignal | undefined, operation: (signal: AbortSignal) => Promise<T>): Promise<T> {
        if (this.closing) throw new Error('用户交互会话已关闭');
        const controller = new AbortController();
        let completed!: () => void;
        this.active.set(controller, new Promise<void>((resolve) => { completed = resolve; }));
        this.reportPending();
        const combined = signal ? AbortSignal.any([signal, controller.signal]) : controller.signal;
        const previous = this.tail;
        let release!: () => void;
        const released = new Promise<void>((resolve) => { release = resolve; });
        // Aborting a queued request must not let its successor overtake the owner.
        this.tail = previous.then(() => released);
        let abort!: () => void;
        const aborted = new Promise<never>((_, reject) => { abort = () => reject(combined.reason); });
        combined.addEventListener('abort', abort, { once: true });
        try {
            if (combined.aborted) throw combined.reason;
            await Promise.race([previous, aborted]);
            if (combined.aborted) throw combined.reason;
            const value = await Promise.race([operation(combined), aborted]);
            if (combined.aborted) throw combined.reason;
            return value;
        } finally {
            combined.removeEventListener('abort', abort);
            this.active.delete(controller);
            this.reportPending();
            release();
            completed();
        }
    }
}
