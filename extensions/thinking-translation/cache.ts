import { createHash, randomUUID } from "node:crypto";
import { chmod, mkdir, readFile, readdir, rename, stat, unlink, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { basename, join, resolve } from "node:path";

type LegacyCacheRecord = {
    version: 1;
    translation: string;
};

export type TranslationMetrics = {
    inputTokens: number;
    outputTokens: number;
    durationMs: number;
};

type CacheRecord = {
    version: 2;
    translation: string;
    metrics: TranslationMetrics;
};

const CACHE_MODE = 0o700;
const FILE_MODE = 0o600;
export const CACHE_MAX_FILES = 200;
export const CACHE_PRUNE_COUNT = 50;
export const CACHE_PRUNE_INTERVAL_MS = 10 * 60 * 1_000;

const CACHE_PRUNE_RUNTIME = Symbol.for("pi.extensions.thinking-translation.cache-prune.v1");

type CachePruneState = {
    lastRunAt: number;
    inFlight?: Promise<void>;
};

const pruneStates = (): Map<string, CachePruneState> => {
    const globals = globalThis as typeof globalThis & Record<symbol, unknown>;
    let states = globals[CACHE_PRUNE_RUNTIME] as Map<string, CachePruneState> | undefined;
    if (!states) {
        states = new Map();
        globals[CACHE_PRUNE_RUNTIME] = states;
    }
    return states;
};

export const defaultCacheDirectory = (): string => join(homedir(), ".pi", "thinking-translations");

/** Persistent, content-addressed cache for display-only translations. */
export class TranslationCache {
    private readonly memory = new Map<string, string>();
    private readonly directory: string;

    constructor(directory = defaultCacheDirectory()) {
        this.directory = resolve(directory);
    }

    key(text: string): string {
        return createHash("sha256").update(text, "utf8").digest("hex");
    }

    async get(text: string): Promise<string | undefined> {
        const key = this.key(text);
        const inMemory = this.memory.get(key);
        if (inMemory !== undefined) return inMemory;

        await this.ensureDirectory();
        try {
            const raw: unknown = JSON.parse(await readFile(this.fileFor(key), "utf8"));
            if (!isCacheRecord(raw)) return undefined;
            this.memory.set(key, raw.translation);
            return raw.translation;
        } catch {
            return undefined;
        }
    }

    async set(text: string, translation: string, metrics: TranslationMetrics): Promise<void> {
        const key = this.key(text);
        this.memory.set(key, translation);
        await this.ensureDirectory();

        const destination = this.fileFor(key);
        const temporary = `${destination}.${process.pid}.${randomUUID()}.tmp`;
        const record: CacheRecord = { version: 2, translation, metrics };
        try {
            await writeFile(temporary, `${JSON.stringify(record)}\n`, { encoding: "utf8", mode: FILE_MODE, flag: "wx" });
            await chmod(temporary, FILE_MODE);
            await rename(temporary, destination);
            await chmod(destination, FILE_MODE);
        } catch (error) {
            await safeRemove(temporary);
            throw error;
        }

        await this.pruneAfterInsert(basename(destination));
    }

    private async pruneAfterInsert(insertedFileName: string): Promise<void> {
        let cacheFiles: string[];
        try {
            cacheFiles = (await readdir(this.directory, { withFileTypes: true }))
                .filter((entry) => entry.isFile() && entry.name.endsWith(".json"))
                .map((entry) => entry.name);
        } catch {
            return;
        }
        if (cacheFiles.length <= CACHE_MAX_FILES) return;

        const states = pruneStates();
        const state = states.get(this.directory) ?? { lastRunAt: 0 };
        states.set(this.directory, state);
        if (state.inFlight) {
            await state.inFlight;
            return;
        }

        const now = Date.now();
        if (now - state.lastRunAt < CACHE_PRUNE_INTERVAL_MS) return;
        state.lastRunAt = now;
        const cleanup = this.deleteOldest(cacheFiles, insertedFileName);
        state.inFlight = cleanup;
        try {
            await cleanup;
        } finally {
            if (state.inFlight === cleanup) state.inFlight = undefined;
        }
    }

    private async deleteOldest(cacheFiles: string[], insertedFileName: string): Promise<void> {
        const candidates = (await Promise.all(cacheFiles
            .filter((name) => name !== insertedFileName)
            .map(async (name) => {
                try {
                    const metadata = await stat(join(this.directory, name));
                    return { name, mtimeMs: metadata.mtimeMs };
                } catch {
                    return undefined;
                }
            })))
            .filter((item): item is { name: string; mtimeMs: number } => item !== undefined)
            .sort((left, right) => left.mtimeMs - right.mtimeMs || left.name.localeCompare(right.name))
            .slice(0, CACHE_PRUNE_COUNT);

        await Promise.all(candidates.map(async ({ name }) => {
            try {
                await unlink(join(this.directory, name));
                this.memory.delete(name.slice(0, -".json".length));
            } catch {
                // Cache pruning is best-effort and must not fail a successful insert.
            }
        }));
    }

    private fileFor(key: string): string {
        return join(this.directory, `${key}.json`);
    }

    private async ensureDirectory(): Promise<void> {
        await mkdir(this.directory, { recursive: true, mode: CACHE_MODE });
        // mkdir respects umask and does not change an existing directory.
        await chmod(this.directory, CACHE_MODE);
    }
}

const isNonNegativeNumber = (value: unknown): value is number =>
    typeof value === "number" && Number.isFinite(value) && value >= 0;

const isTranslationMetrics = (value: unknown): value is TranslationMetrics =>
    Boolean(value)
    && typeof value === "object"
    && isNonNegativeNumber((value as TranslationMetrics).inputTokens)
    && isNonNegativeNumber((value as TranslationMetrics).outputTokens)
    && isNonNegativeNumber((value as TranslationMetrics).durationMs);

const isCacheRecord = (value: unknown): value is LegacyCacheRecord | CacheRecord => {
    if (!value || typeof value !== "object" || typeof (value as LegacyCacheRecord).translation !== "string") {
        return false;
    }
    if ((value as LegacyCacheRecord).version === 1) return true;
    return (value as CacheRecord).version === 2 && isTranslationMetrics((value as CacheRecord).metrics);
};

async function safeRemove(path: string): Promise<void> {
    try {
        await unlink(path);
    } catch {
        // The original write error is the useful error; stale temp files are harmless.
    }
}
