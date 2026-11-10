type CacheEntry = {
    owner: WeakRef<{ clear(): void }>;
    bytes: number;
    pass: number;
};

/** A budget for optional render snapshots, not a measurement of the JS heap. */
export class RenderCacheBudget {
    private entries = new Set<CacheEntry>();
    private bytes = 0;
    private pass = 0;
    private depth = 0;
    private protectedBytes = 0;

    constructor(readonly maxBytes: number) {
        if (!Number.isSafeInteger(maxBytes) || maxBytes < 0) throw new RangeError('Invalid render cache budget');
    }

    get usedBytes(): number { this.sweepDead(); return this.bytes; }
    get size(): number { this.sweepDead(); return this.entries.size; }

    /** Nested synchronous renders share a pass; hits cannot evict each other. */
    run<T>(action: () => T): T {
        if (this.depth++ === 0) {
            this.pass++;
            this.protectedBytes = 0;
        }
        try { return action(); }
        finally { this.depth--; }
    }

    clear(): void {
        for (const entry of this.entries) {
            entry.owner.deref()?.clear();
            this.remove(entry);
        }
    }

    /** @internal The index holds only a weak reference to the snapshot owner. */
    add(owner: { clear(): void }, bytes: number): CacheEntry | undefined {
        // A minimum charge also bounds metadata for empty/zero-byte snapshots.
        if (!Number.isFinite(bytes) || bytes < 0) return;
        bytes = Math.max(1, Math.ceil(bytes));
        if (bytes > this.maxBytes) return;
        // Once a scan fills the budget, reject remaining misses in O(1), rather
        // than walking all protected entries again for every historical tool.
        if (this.depth && this.maxBytes - this.protectedBytes < bytes) return;
        if (this.bytes + bytes > this.maxBytes) {
            const victims: CacheEntry[] = [];
            let available = this.maxBytes - this.bytes;
            for (const entry of this.entries) {
                if (!entry.owner.deref()) {
                    available += entry.bytes;
                    this.remove(entry);
                } else if (!this.depth || entry.pass !== this.pass) {
                    available += entry.bytes;
                    victims.push(entry);
                }
                if (available >= bytes) break;
            }
            // Do not discard useful snapshots if protected entries prevent admission.
            if (available < bytes) return;
            for (const entry of victims) entry.owner.deref()?.clear();
        }
        const entry = { owner: new WeakRef(owner), bytes, pass: this.depth ? this.pass : -1 };
        this.entries.add(entry);
        this.bytes += bytes;
        if (this.depth) this.protectedBytes += bytes;
        return entry;
    }

    /** @internal */
    touch(entry: CacheEntry): void {
        if (this.depth && entry.pass !== this.pass) this.protectedBytes += entry.bytes;
        entry.pass = this.depth ? this.pass : -1;
        this.entries.delete(entry);
        this.entries.add(entry);
    }

    /** @internal */
    remove(entry: CacheEntry): void {
        if (this.entries.delete(entry)) {
            this.bytes -= entry.bytes;
            if (this.depth && entry.pass === this.pass) this.protectedBytes -= entry.bytes;
        }
    }

    private sweepDead(): void {
        for (const entry of this.entries) if (!entry.owner.deref()) this.remove(entry);
    }
}

export const renderCacheBudget = new RenderCacheBudget(128 * 1024 * 1024);

/** The component owns its snapshot; the shared budget must not keep it alive. */
export class RenderCache<T> {
    private value: T | undefined;
    private entry: CacheEntry | undefined;

    constructor(private readonly budget: RenderCacheBudget = renderCacheBudget) {}

    get(): T | undefined {
        if (this.entry) this.budget.touch(this.entry);
        return this.value;
    }

    set(value: T, bytes: number): void {
        this.clear();
        this.entry = this.budget.add(this, bytes);
        if (this.entry) this.value = value;
    }

    clear(): void {
        if (this.entry) this.budget.remove(this.entry);
        this.entry = undefined;
        this.value = undefined;
    }
}

export const withRenderCachePass = <T>(action: () => T): T => renderCacheBudget.run(action);

// A folded tool is no longer traversed by the native renderer. Release its last
// expanded snapshots explicitly; these weak keys do not keep old sessions alive.
const toolReleases = new WeakMap<object, () => void>();
export const onToolCacheRelease = (tool: object, release: () => void): void => { toolReleases.set(tool, release); };
export const releaseToolCache = (tool: object): void => {
    const release = toolReleases.get(tool);
    toolReleases.delete(tool);
    release?.();
};

/** Conservative UTF-16/string/array allowance, not exact V8 allocation accounting. */
export const estimateLineBytes = (lines: readonly string[]): number =>
    lines.reduce((bytes, line) => bytes + 32 + line.length * 2, 24);
