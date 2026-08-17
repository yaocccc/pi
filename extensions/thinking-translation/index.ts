import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import type { Model } from "@earendil-works/pi-ai";
import { TranslationCache } from "./cache.ts";
import { TranslationCoordinator } from "./coordinator.ts";
import { loadTranslationSettings, saveTranslationSettings } from "./config.ts";
import {
    clearDisplayStore,
    installDisplayPatch,
    type TranslationDisplayStore,
} from "./display.ts";
import {
    normalizeTranslation,
    parseTranslationResponse,
    preservesThinkingSegments,
    splitThinkingSegments,
    type TranslationStatus,
} from "./state.ts";

const SINGLE_TRANSLATION_SYSTEM_PROMPT =
    "Translate the user's text into natural Simplified Chinese. Return only the translation.";

const MULTI_TRANSLATION_SYSTEM_PROMPT = [
    "The user sends a JSON array of text segments.",
    "Translate each string independently into natural Simplified Chinese.",
    "Return only a JSON array of strings in the same order and with the same length.",
].join(" ");

const HISTORY_CACHE_CONCURRENCY = 8;

const isRecord = (value: unknown): value is Record<string, unknown> =>
    Boolean(value) && typeof value === "object" && !Array.isArray(value);

/** Collect only recent, unique thinking sources from the active branch. */
const historicalThinkingSources = (entries: readonly unknown[], maxThinkingLength: number): string[] => {
    const sources: string[] = [];
    const seen = new Set<string>();

    for (let entryIndex = entries.length - 1; entryIndex >= 0; entryIndex--) {
        const entry = entries[entryIndex];
        if (!isRecord(entry) || entry.type !== "message" || !isRecord(entry.message)) continue;
        const message = entry.message;
        if (message.role !== "assistant" || !Array.isArray(message.content)) continue;

        for (let contentIndex = message.content.length - 1; contentIndex >= 0; contentIndex--) {
            const part = message.content[contentIndex];
            if (!isRecord(part) || part.type !== "thinking" || typeof part.thinking !== "string") continue;
            const source = part.thinking;
            if (!source.trim() || Array.from(source).length > Math.min(200, maxThinkingLength) || seen.has(source)) continue;
            seen.add(source);
            sources.push(source);
        }
    }
    return sources;
};

export class TranslationSession implements TranslationDisplayStore {
    private readonly coordinator: TranslationCoordinator;
    private readonly components = new Set<{ invalidate(): void }>();
    private readonly historicalTranslations = new Map<string, string>();
    private readonly ctx: ExtensionContext;
    private readonly model: Model<any> | undefined;
    private readonly maxThinkingLength: number;
    private readonly cache: TranslationCache;
    private hydrationVersion = 0;
    private closed = false;
    private enabled: boolean;

    constructor(
        ctx: ExtensionContext,
        model: Model<any> | undefined,
        maxThinkingLength: number,
        enabled: boolean,
        cache = new TranslationCache(),
    ) {
        this.ctx = ctx;
        this.model = model;
        this.maxThinkingLength = maxThinkingLength;
        this.enabled = enabled;
        this.cache = cache;
        this.coordinator = new TranslationCoordinator({
            maxThinkingLength,
            translate: (source, signal) => this.translate(source, signal),
            onChange: () => this.invalidateComponents(),
        }, enabled);
    }

    setEnabled(enabled: boolean): void {
        if (this.closed || this.enabled === enabled) return;
        this.enabled = enabled;
        this.hydrationVersion++;
        this.historicalTranslations.clear();
        this.coordinator.setEnabled(enabled);
        if (enabled) void this.hydrateHistory();
    }

    get(key: string, source: string): TranslationStatus | undefined {
        if (this.closed || !this.enabled) return undefined;
        const live = this.coordinator.get(key, source);
        if (live) return live;
        const translation = this.historicalTranslations.get(source);
        return translation === undefined ? undefined : { state: "ready", translation };
    }

    track(component: { invalidate(): void }): void {
        if (!this.closed) this.components.add(component);
    }

    thinkingStart(messageTimestamp: number, contentIndex: number): void {
        this.coordinator.thinkingStart(messageTimestamp, contentIndex);
    }

    thinkingDelta(messageTimestamp: number, contentIndex: number, delta: string): void {
        this.coordinator.thinkingDelta(messageTimestamp, contentIndex, delta);
    }

    thinkingEnd(messageTimestamp: number, contentIndex: number, content: string): void {
        this.coordinator.thinkingEnd(messageTimestamp, contentIndex, content);
    }

    boundary(messageTimestamp: number): void {
        this.coordinator.boundary(messageTimestamp);
    }

    finishMessage(messageTimestamp: number): void {
        this.coordinator.finishMessage(messageTimestamp);
    }

    abortMessage(messageTimestamp: number): void {
        this.coordinator.abortMessage(messageTimestamp);
    }

    /** Hydrate display state from persistent cache only; cache misses never translate. */
    async hydrateHistory(): Promise<void> {
        const version = ++this.hydrationVersion;
        if (this.closed || !this.enabled) return;

        let entries: readonly unknown[];
        try {
            entries = this.ctx.sessionManager.getBranch();
        } catch {
            return;
        }
        const sources = historicalThinkingSources(entries, this.maxThinkingLength);
        let cursor = 0;
        let changed = false;

        const worker = async (): Promise<void> => {
            while (cursor < sources.length) {
                if (this.closed || !this.enabled || version !== this.hydrationVersion) return;
                const source = sources[cursor++]!;
                const cached = await this.cache.get(source).catch(() => undefined);
                if (this.closed || !this.enabled || version !== this.hydrationVersion) return;
                const translation = cached === undefined ? "" : normalizeTranslation(cached);
                if (!translation
                    || !preservesThinkingSegments(source, translation)
                    || this.historicalTranslations.get(source) === translation) continue;
                this.historicalTranslations.set(source, translation);
                changed = true;
            }
        };

        await Promise.all(Array.from(
            { length: Math.min(HISTORY_CACHE_CONCURRENCY, sources.length) },
            () => worker(),
        ));
        if (!this.closed && this.enabled && version === this.hydrationVersion && changed) {
            this.invalidateComponents();
        }
    }

    shutdown(): void {
        if (this.closed) return;
        this.closed = true;
        this.enabled = false;
        this.hydrationVersion++;
        this.historicalTranslations.clear();
        this.coordinator.shutdown();
        this.components.clear();
    }

    private async translate(source: string, signal: AbortSignal): Promise<string | undefined> {
        const segments = splitThinkingSegments(source);
        if (segments.length === 0) return undefined;

        const cached = await this.cache.get(source);
        if (signal.aborted || this.closed) return undefined;
        if (cached !== undefined && preservesThinkingSegments(source, cached)) {
            return normalizeTranslation(cached);
        }
        if (!this.model || !this.ctx.modelRegistry.hasConfiguredAuth(this.model)) return undefined;

        const multipleSegments = segments.length > 1;
        const requestStartedAt = performance.now();
        const response = await this.ctx.modelRegistry.complete(
            this.model,
            {
                systemPrompt: multipleSegments
                    ? MULTI_TRANSLATION_SYSTEM_PROMPT
                    : SINGLE_TRANSLATION_SYSTEM_PROMPT,
                messages: [{
                    role: "user",
                    content: [{
                        type: "text",
                        text: multipleSegments ? JSON.stringify(segments) : segments[0]!,
                    }],
                    timestamp: Date.now(),
                }],
            },
            {
                reasoningEffort: "minimal",
                signal,
                cacheRetention: "none",
            },
        );
        if (signal.aborted || this.closed || response.stopReason === "aborted" || response.stopReason === "error") {
            return undefined;
        }

        const rawTranslation = response.content
            .filter((part): part is { type: "text"; text: string } => part.type === "text")
            .map((part) => part.text)
            .join("\n")
            .trim();
        const translation = parseTranslationResponse(rawTranslation, segments.length);
        if (!translation) return undefined;

        // A cache filesystem failure must not suppress an otherwise valid
        // display-only result.
        await this.cache.set(source, translation, {
            inputTokens: response.usage.input,
            outputTokens: response.usage.output,
            durationMs: Math.max(0, Math.round(performance.now() - requestStartedAt)),
        }).catch(() => undefined);
        return signal.aborted || this.closed ? undefined : translation;
    }

    private invalidateComponents(): void {
        for (const component of this.components) {
            try {
                component.invalidate();
            } catch {
                // A component can disappear while a translation is completing.
            }
        }
    }
}

const configuredModel = (ctx: ExtensionContext, modelId: string): Model<any> | undefined => {
    const slash = modelId.indexOf("/");
    if (slash <= 0 || slash === modelId.length - 1) return undefined;
    return ctx.modelRegistry.find(modelId.slice(0, slash), modelId.slice(slash + 1));
};

export default function thinkingTranslation(pi: ExtensionAPI): void {
    let session: TranslationSession | undefined;

    pi.on("session_start", async (_event, ctx) => {
        if (session) {
            session.shutdown();
            clearDisplayStore(session);
        }
        if (!ctx.hasUI) {
            session = undefined;
            return;
        }

        const loaded = await loadTranslationSettings();
        // Removing the config deliberately opts into the currently selected model.
        // A stale configured model must not silently disable translations.
        const model = loaded.exists
            ? configuredModel(ctx, loaded.settings.model) ?? ctx.model
            : ctx.model;
        const nextSession = new TranslationSession(
            ctx,
            model,
            loaded.settings.maxThinkingLength,
            loaded.settings.enabled,
        );
        session = nextSession;
        installDisplayPatch(nextSession);
        // Session startup must not wait for history I/O. This path consults only
        // the persistent cache and never calls the translation model.
        void nextSession.hydrateHistory();
    });

    pi.registerCommand("thinking_translation", {
        description: "切换 Thinking 中文翻译",
        handler: async (_args, ctx) => {
            const loaded = await loadTranslationSettings();
            const enabled = !loaded.settings.enabled;
            await saveTranslationSettings({ ...loaded.settings, enabled });
            session?.setEnabled(enabled);
            ctx.ui.notify(`Thinking 中文翻译已${enabled ? "开启" : "关闭"}`, "info");
        },
    });

    pi.on("message_update", (event, ctx) => {
        if (!ctx.hasUI || !session || event.message.role !== "assistant") return;
        const update = event.assistantMessageEvent;
        const timestamp = event.message.timestamp;

        switch (update.type) {
            case "thinking_start":
                session.thinkingStart(timestamp, update.contentIndex);
                break;
            case "thinking_delta":
                session.thinkingDelta(timestamp, update.contentIndex, update.delta);
                break;
            case "thinking_end":
                session.thinkingEnd(timestamp, update.contentIndex, update.content);
                break;
            case "text_start":
            case "toolcall_start":
                session.boundary(timestamp);
                break;
            case "done":
                session.finishMessage(timestamp);
                break;
            case "error":
                session.abortMessage(timestamp);
                break;
        }
    });

    pi.on("message_end", (event, ctx) => {
        if (!ctx.hasUI || !session || event.message.role !== "assistant") return;
        if (event.message.stopReason === "aborted" || event.message.stopReason === "error") {
            session.abortMessage(event.message.timestamp);
        } else {
            session.finishMessage(event.message.timestamp);
        }
    });

    pi.on("session_shutdown", () => {
        if (!session) return;
        session.shutdown();
        clearDisplayStore(session);
        session = undefined;
    });
}
