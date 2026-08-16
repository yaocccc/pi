import { access, mkdir, readFile, rename, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { getAgentDir } from "@earendil-works/pi-coding-agent";

export const DEFAULT_SETTINGS = {
    enabled: true,
    model: "openai-codex/gpt-5.3-codex-spark",
    maxThinkingLength: 200,
    reasoning: "minimal" as const,
};

export type TranslationSettings = typeof DEFAULT_SETTINGS;

export const settingsPath = (): string => join(getAgentDir(), "thinking-translation-settings.json");

const isRecord = (value: unknown): value is Record<string, unknown> =>
    Boolean(value) && typeof value === "object" && !Array.isArray(value);

const validLength = (value: unknown): number | undefined =>
    typeof value === "number" && Number.isSafeInteger(value) && value > 0 && value <= 10_000
        ? value
        : undefined;

/**
 * A missing file is intentional: it means use the active session model. Existing
 * files are merged with fixed safe defaults so malformed user values cannot make
 * the extension issue an unexpectedly large request.
 */
export async function loadTranslationSettings(path = settingsPath()): Promise<{
    exists: boolean;
    settings: TranslationSettings;
}> {
    try {
        await access(path);
    } catch {
        return { exists: false, settings: { ...DEFAULT_SETTINGS } };
    }

    try {
        const raw: unknown = JSON.parse(await readFile(path, "utf8"));
        if (!isRecord(raw)) return { exists: true, settings: { ...DEFAULT_SETTINGS } };

        return {
            exists: true,
            settings: {
                enabled: raw.enabled !== false,
                model: typeof raw.model === "string" && raw.model.includes("/")
                    ? raw.model
                    : DEFAULT_SETTINGS.model,
                maxThinkingLength: validLength(raw.maxThinkingLength) ?? DEFAULT_SETTINGS.maxThinkingLength,
                // Translation requests always use minimal reasoning, even if a
                // hand-edited config contains another value.
                reasoning: "minimal",
            },
        };
    } catch {
        return { exists: true, settings: { ...DEFAULT_SETTINGS } };
    }
}

/** Persist the user-controlled enable flag without exposing translations to sessions. */
export async function saveTranslationSettings(settings: TranslationSettings, path = settingsPath()): Promise<void> {
    await mkdir(dirname(path), { recursive: true, mode: 0o700 });
    const temporary = `${path}.${process.pid}.${Date.now()}.tmp`;
    const value = {
        version: 1,
        enabled: settings.enabled,
        model: settings.model,
        maxThinkingLength: settings.maxThinkingLength,
        reasoning: "minimal",
    };
    try {
        await writeFile(temporary, `${JSON.stringify(value, null, 2)}\n`, { encoding: "utf8", mode: 0o600, flag: "wx" });
        await rename(temporary, path);
    } catch (error) {
        try { await import("node:fs/promises").then(({ unlink }) => unlink(temporary)); } catch { /* best effort */ }
        throw error;
    }
}
