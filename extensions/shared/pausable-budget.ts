/** Active-time budget. Pause never revives an exhausted/disposed deadline. */
export class PausableBudget {
	private timer?: NodeJS.Timeout;
	private deadline: number;
	private remaining: number;
	private tokens = new Set<string>();
	private closed = false;
	constructor(ms: number, private expire: () => void, private keepAlive = false) {
		this.remaining = ms;
		this.deadline = Date.now() + ms;
		this.arm();
	}
	get expiresAt() { return this.deadline; }
	get paused() { return this.tokens.size > 0; }
	get exhausted() { return this.closed || (!this.paused && Date.now() >= this.deadline); }
	private arm() {
		this.deadline = Date.now() + this.remaining;
		this.timer = setTimeout(() => { this.dispose(); this.expire(); }, Math.max(1, this.remaining));
		if (!this.keepAlive) this.timer.unref();
	}
	pause(token: string): boolean {
		if (this.exhausted) return false;
		if (this.tokens.has(token)) return true;
		if (!this.paused) { this.remaining = Math.max(0, this.deadline - Date.now()); clearTimeout(this.timer); }
		this.tokens.add(token);
		return true;
	}
	resume(token: string) {
		if (this.closed || !this.tokens.delete(token) || this.paused) return;
		this.arm();
	}
	dispose() { this.closed = true; clearTimeout(this.timer); this.tokens.clear(); }
}
