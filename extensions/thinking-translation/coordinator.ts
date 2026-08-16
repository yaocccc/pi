import { normalizeTranslation, translationBlockKey, type TranslationStatus } from "./state.ts";

export type TranslationRunner = (source: string, signal: AbortSignal) => Promise<string | undefined>;

type StoredStatus = TranslationStatus & { source: string };
type TranslationJob = {
    controller: AbortController;
    source: string;
    version: number;
};

type ThinkingBlock = {
    key: string;
    messageTimestamp: number;
    source: string;
    version: number;
    requestsStarted: number;
    lastRequestedSource?: string;
    timer?: ReturnType<typeof setTimeout>;
    job?: TranslationJob;
    status?: StoredStatus;
    finalized: boolean;
};

export type TranslationCoordinatorOptions = {
    maxThinkingLength: number;
    translate: TranslationRunner;
    onChange: () => void;
    idleMs?: number;
};

/** Coordinates display-only translation jobs for thinking blocks seen in the live stream. */
export class TranslationCoordinator {
    private readonly blocks = new Map<string, ThinkingBlock>();
    private readonly options: TranslationCoordinatorOptions;
    private readonly idleMs: number;
    private enabled: boolean;
    private closed = false;

    constructor(options: TranslationCoordinatorOptions, enabled: boolean) {
        this.options = options;
        this.enabled = enabled;
        this.idleMs = options.idleMs ?? 500;
    }

    setEnabled(enabled: boolean): void {
        if (this.closed || this.enabled === enabled) return;
        this.enabled = enabled;
        if (!enabled) {
            for (const block of this.blocks.values()) this.cancelBlock(block);
            this.blocks.clear();
        }
        this.options.onChange();
    }

    get(key: string, source: string): TranslationStatus | undefined {
        if (!this.enabled || this.closed) return undefined;
        const block = this.blocks.get(key);
        if (block?.source !== source || !block.status) return undefined;
        // A ready translation remains the display fallback while a correction
        // for a longer version of the same block is pending.
        if (block.status.state === "ready") {
            return { state: "ready", translation: block.status.translation };
        }
        return block.status.source === source ? { state: "pending" } : undefined;
    }

    thinkingStart(messageTimestamp: number, contentIndex: number): void {
        if (!this.accepting()) return;
        const key = translationBlockKey(messageTimestamp, contentIndex);
        this.flushMessage(messageTimestamp, false, key);
        if (!this.blocks.has(key)) {
            this.blocks.set(key, {
                key,
                messageTimestamp,
                source: "",
                version: 0,
                requestsStarted: 0,
                finalized: false,
            });
        }
    }

    thinkingDelta(messageTimestamp: number, contentIndex: number, delta: string): void {
        if (!this.accepting() || !delta) return;
        const block = this.ensureBlock(messageTimestamp, contentIndex);
        if (block.finalized) return;
        this.updateSource(block, `${block.source}${delta}`);
        this.scheduleIdle(block);
    }

    thinkingEnd(messageTimestamp: number, contentIndex: number, content: string): void {
        if (!this.accepting()) return;
        const block = this.ensureBlock(messageTimestamp, contentIndex);
        this.updateSource(block, content);
        this.clearTimer(block);
        this.flush(block);
        block.finalized = true;
    }

    boundary(messageTimestamp: number): void {
        if (!this.accepting()) return;
        this.flushMessage(messageTimestamp, false);
    }

    finishMessage(messageTimestamp: number): void {
        if (!this.accepting()) return;
        this.flushMessage(messageTimestamp, true);
    }

    abortMessage(messageTimestamp: number): void {
        let changed = false;
        for (const [key, block] of this.blocks) {
            if (block.messageTimestamp !== messageTimestamp) continue;

            this.clearTimer(block);
            block.version++;
            if (block.job) block.job.controller.abort();
            block.job = undefined;
            block.finalized = true;

            // Esc/error ends only unfinished work. A completed translation is
            // still valid display state and must survive "Operation aborted".
            if (block.status?.state === "ready") continue;
            changed = block.status !== undefined || changed;
            block.status = undefined;
            this.blocks.delete(key);
        }
        if (changed) this.options.onChange();
    }

    shutdown(): void {
        if (this.closed) return;
        this.closed = true;
        for (const block of this.blocks.values()) this.cancelBlock(block);
        this.blocks.clear();
    }

    private accepting(): boolean {
        return this.enabled && !this.closed;
    }

    private ensureBlock(messageTimestamp: number, contentIndex: number): ThinkingBlock {
        const key = translationBlockKey(messageTimestamp, contentIndex);
        let block = this.blocks.get(key);
        if (!block) {
            block = {
                key,
                messageTimestamp,
                source: "",
                version: 0,
                requestsStarted: 0,
                finalized: false,
            };
            this.blocks.set(key, block);
        }
        return block;
    }

    private updateSource(block: ThinkingBlock, source: string): void {
        if (block.source === source) return;
        block.source = source;
        block.version++;

        let changed = false;
        if (block.status?.state === "pending" && block.status.source !== source) {
            block.status = undefined;
            changed = true;
        }
        if (block.job && block.job.source !== source) {
            block.job.controller.abort();
            block.job = undefined;
        }
        if (!this.validSource(source)) {
            this.clearTimer(block);
            if (block.job) {
                block.job.controller.abort();
                block.job = undefined;
            }
            if (block.status) {
                block.status = undefined;
                changed = true;
            }
        }
        if (changed) this.options.onChange();
    }

    private scheduleIdle(block: ThinkingBlock): void {
        this.clearTimer(block);
        // A second idle request is the bounded correction for content that
        // resumed after the provisional request; do not wait for thinking_end.
        if (!this.validSource(block.source)
            || block.requestsStarted >= 2
            || block.lastRequestedSource === block.source) return;
        block.timer = setTimeout(() => {
            block.timer = undefined;
            this.flush(block);
        }, this.idleMs);
    }

    private flushMessage(messageTimestamp: number, final: boolean, exceptKey?: string): void {
        for (const block of this.blocks.values()) {
            if (block.messageTimestamp !== messageTimestamp || block.key === exceptKey || block.finalized) continue;
            this.clearTimer(block);
            this.flush(block);
            if (final) block.finalized = true;
        }
    }

    private flush(block: ThinkingBlock): void {
        const source = block.source;
        if (!this.validSource(source)) {
            this.updateSource(block, source);
            return;
        }
        if (block.lastRequestedSource === source || block.requestsStarted >= 2) return;

        block.requestsStarted++;
        block.lastRequestedSource = source;
        block.version++;
        if (block.job) block.job.controller.abort();

        const job: TranslationJob = {
            controller: new AbortController(),
            source,
            version: block.version,
        };
        block.job = job;
        // Do not replace a usable translation with a pending marker. If this
        // block has never completed, pending still renders the source + globe.
        if (block.status?.state !== "ready") {
            block.status = { state: "pending", source };
            this.options.onChange();
        }

        void this.options.translate(source, job.controller.signal)
            .then((translation) => this.complete(block, job, translation))
            .catch(() => this.complete(block, job, undefined));
    }

    private complete(block: ThinkingBlock, job: TranslationJob, translation: string | undefined): void {
        if (!this.accepting()
            || job.controller.signal.aborted
            || block.job !== job
            || block.version !== job.version
            || block.source !== job.source) return;

        block.job = undefined;
        const normalized = translation === undefined ? "" : normalizeTranslation(translation);
        if (normalized) {
            block.status = { state: "ready", translation: normalized, source: job.source };
        } else if (block.status?.state === "pending" && block.status.source === job.source) {
            block.status = undefined;
        }
        this.options.onChange();
    }

    private validSource(source: string): boolean {
        return Boolean(source.trim())
            && Array.from(source).length <= Math.min(200, this.options.maxThinkingLength);
    }

    private clearTimer(block: ThinkingBlock): void {
        if (block.timer === undefined) return;
        clearTimeout(block.timer);
        block.timer = undefined;
    }

    private cancelBlock(block: ThinkingBlock): boolean {
        this.clearTimer(block);
        block.version++;
        if (block.job) block.job.controller.abort();
        block.job = undefined;
        const changed = block.status !== undefined;
        block.status = undefined;
        return changed;
    }
}
