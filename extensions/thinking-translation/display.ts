import { AssistantMessageComponent } from "@earendil-works/pi-coding-agent";
import type { AssistantMessage } from "@earendil-works/pi-ai";
import { formatThinkingDisplay, translationBlockKey, type TranslationStatus } from "./state.ts";

export type TranslationDisplayStore = {
    /** Match both the live assistant message and its thinking content index. */
    get(key: string, source: string): TranslationStatus | undefined;
    track(component: AssistantMessageComponent): void;
};

type AssistantPrototype = {
    lastMessage?: AssistantMessage;
    updateContent(message: AssistantMessage, isStreaming?: boolean): void;
    [key: symbol]: unknown;
};

type DisplayRuntime = { store?: TranslationDisplayStore };

const DISPLAY_PATCH = Symbol.for("pi.extensions.thinking-translation.display.v3");
const DISPLAY_RUNTIME = Symbol.for("pi.extensions.thinking-translation.runtime.v1");

const runtime = (): DisplayRuntime => {
    const globals = globalThis as typeof globalThis & { [DISPLAY_RUNTIME]?: DisplayRuntime };
    return globals[DISPLAY_RUNTIME] ??= {};
};

/**
 * Patch only the component's presentation input. The component's lastMessage is
 * restored to Pi's original message so invalidation and session/model context
 * never see synthetic translation text.
 */
export function installDisplayPatch(store: TranslationDisplayStore): void {
    const shared = runtime();
    shared.store = store;

    const prototype = AssistantMessageComponent.prototype as unknown as AssistantPrototype;
    if (prototype[DISPLAY_PATCH]) return;

    const originalUpdateContent = prototype.updateContent;
    prototype.updateContent = function translatedAssistantMessage(
        this: AssistantPrototype,
        message: AssistantMessage,
        isStreaming?: boolean,
    ): void {
        const currentStore = runtime().store;
        if (!currentStore) {
            originalUpdateContent.call(this, message, isStreaming);
            return;
        }

        currentStore.track(this as unknown as AssistantMessageComponent);
        const displayMessage = withTranslationDisplay(message, currentStore);
        originalUpdateContent.call(this, displayMessage, isStreaming);
        // extensions/ui intentionally relies on lastMessage during its own
        // collapsed-thinking and spacing patches, so restore the source message
        // after the wrapped update chain has rendered the display-only copy.
        this.lastMessage = message;
    };
    prototype[DISPLAY_PATCH] = true;
}

export function clearDisplayStore(store: TranslationDisplayStore): void {
    const shared = runtime();
    if (shared.store === store) shared.store = undefined;
}

const withTranslationDisplay = (message: AssistantMessage, store: TranslationDisplayStore): AssistantMessage => {
    let changed = false;
    const content: AssistantMessage["content"] = [];

    for (let contentIndex = 0; contentIndex < message.content.length; contentIndex++) {
        const part = message.content[contentIndex]!;
        if (part.type !== "thinking") {
            content.push(part);
            continue;
        }

        const key = translationBlockKey(message.timestamp, contentIndex);
        const translation = store.get(key, part.thinking);
        if (!translation) {
            content.push(part);
            continue;
        }

        changed = true;
        // Keep the translation in a shallow display copy of the thinking block.
        // extensions/ui collapses thinking from this same block, so its preview
        // retains the suffix while the source session message stays untouched.
        content.push({
            ...part,
            thinking: formatThinkingDisplay(part.thinking, translation),
        });
    }

    return changed ? { ...message, content } : message;
};
