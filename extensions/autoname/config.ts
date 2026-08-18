import { readFile } from "node:fs/promises";
import { join } from "node:path";
import { getAgentDir } from "@earendil-works/pi-coding-agent";
import { DEFAULT_CONFIG, type AutonameConfig, type ReasoningStrength } from "./helpers.ts";

const reasoningStrengths = new Set<ReasoningStrength>([
    "minimal", "low", "medium", "high", "xhigh", "max",
]);

const isRecord = (value: unknown): value is Record<string, unknown> =>
    Boolean(value) && typeof value === "object" && !Array.isArray(value);

const validModel = (value: unknown): value is string =>
    value === "auto" || (typeof value === "string" && /^[^/\s]+\/[^/\s]+$/.test(value));

const validReasoning = (value: unknown): value is ReasoningStrength =>
    typeof value === "string" && reasoningStrengths.has(value as ReasoningStrength);

const validCooldownSeconds = (value: unknown): value is number =>
    typeof value === "number"
    && Number.isSafeInteger(value)
    && value >= 0
    && value <= Math.floor(Number.MAX_SAFE_INTEGER / 1_000);

export const autonameConfigPath = (): string => join(getAgentDir(), "autoname.json");

/** Read on every settled turn so hand edits take effect without an extension reload. */
export async function loadAutonameConfig(path = autonameConfigPath()): Promise<AutonameConfig> {
    try {
        const raw: unknown = JSON.parse(await readFile(path, "utf8"));
        if (!isRecord(raw)) return { ...DEFAULT_CONFIG };
        return {
            enabled: typeof raw.enabled === "boolean" ? raw.enabled : DEFAULT_CONFIG.enabled,
            notify: typeof raw.notify === "boolean" ? raw.notify : DEFAULT_CONFIG.notify,
            cooldownSeconds: validCooldownSeconds(raw.cooldownSeconds)
                ? raw.cooldownSeconds
                : DEFAULT_CONFIG.cooldownSeconds,
            model: validModel(raw.model) ? raw.model : DEFAULT_CONFIG.model,
            reasoning: validReasoning(raw.reasoning) ? raw.reasoning : DEFAULT_CONFIG.reasoning,
        };
    } catch {
        return { ...DEFAULT_CONFIG };
    }
}
