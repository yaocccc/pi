import type { AssistantMessage, TextContent, UserMessage } from "@earendil-works/pi-ai";

export const AUTONAME_ENTRY_TYPE = "autoname";

export type ReasoningStrength = "minimal" | "low" | "medium" | "high" | "xhigh" | "max";

export type AutonameConfig = {
    enabled: boolean;
    model: string;
    reasoning: ReasoningStrength;
};

export const DEFAULT_CONFIG: AutonameConfig = {
    enabled: true,
    model: "auto",
    reasoning: "minimal",
};

export type NamingDecision =
    | { action: "keep" }
    | { action: "rename"; name: string };

export type NamingMessage =
    | (UserMessage & { content: [TextContent] })
    | (AssistantMessage & { content: [TextContent] });

type UnknownRecord = Record<string, unknown>;

const isRecord = (value: unknown): value is UnknownRecord =>
    Boolean(value) && typeof value === "object" && !Array.isArray(value);

const textFromContent = (content: unknown): string => {
    if (typeof content === "string") return content.trim();
    if (!Array.isArray(content)) return "";
    return content
        .filter(isRecord)
        .filter((block) => block.type === "text" && typeof block.text === "string")
        .map((block) => (block.text as string).trim())
        .filter(Boolean)
        .join("\n");
};

/** Extract only user and assistant text from active-branch session entries. */
export function extractNamingMessages(entries: readonly unknown[]): NamingMessage[] {
    const messages: NamingMessage[] = [];
    for (const entry of entries) {
        if (!isRecord(entry) || entry.type !== "message" || !isRecord(entry.message)) continue;
        const message = entry.message;
        if (message.role !== "user" && message.role !== "assistant") continue;
        const text = textFromContent(message.content);
        if (!text) continue;
        const timestamp = typeof message.timestamp === "number" ? message.timestamp : Date.now();
        if (message.role === "user") {
            messages.push({ role: "user", content: [{ type: "text", text }], timestamp });
        } else {
            messages.push({
                role: "assistant",
                content: [{ type: "text", text }],
                // These required transport fields are synthetic metadata; the request
                // still contains only extracted text blocks from the active branch.
                api: "pi-messages",
                provider: "autoname",
                model: "autoname",
                usage: {
                    input: 0,
                    output: 0,
                    cacheRead: 0,
                    cacheWrite: 0,
                    totalTokens: 0,
                    cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, total: 0 },
                },
                stopReason: "stop",
                timestamp,
            });
        }
    }
    return messages;
}

export function hasConversationPair(messages: readonly NamingMessage[]): boolean {
    return messages.some((message) => message.role === "user")
        && messages.some((message) => message.role === "assistant");
}

const fence = /^```(?:json)?\s*([\s\S]*?)\s*```$/i;

/** Parse only the two allowed, strict JSON naming decisions. */
export function parseNamingDecision(response: string): NamingDecision | undefined {
    const candidate = response.trim().match(fence)?.[1] ?? response.trim();
    let value: unknown;
    try {
        value = JSON.parse(candidate);
    } catch {
        return undefined;
    }
    if (!isRecord(value)) return undefined;
    const keys = Object.keys(value).sort();
    if (value.action === "keep" && keys.length === 1 && keys[0] === "action") {
        return { action: "keep" };
    }
    if (value.action === "rename"
        && typeof value.name === "string"
        && keys.length === 2
        && keys[0] === "action"
        && keys[1] === "name") {
        return { action: "rename", name: value.name };
    }
    return undefined;
}

/** Keep display names single-line, readable, and deliberately short. */
export function validateName(value: string): string | undefined {
    const name = value.replace(/\s+/g, " ").trim();
    if (name.length < 3 || name.length > 80) return undefined;
    if (/[\u0000-\u001F\u007F]/.test(name)) return undefined;
    return name;
}

export type AutonameProvenance = {
    version: 1;
    kind: "set-name";
    name: string;
};

export function latestAutonameProvenance(entries: readonly unknown[]): AutonameProvenance | undefined {
    for (let index = entries.length - 1; index >= 0; index--) {
        const entry = entries[index];
        if (!isRecord(entry) || entry.type !== "custom" || entry.customType !== AUTONAME_ENTRY_TYPE || !isRecord(entry.data)) continue;
        const data = entry.data;
        if (data.version === 1 && data.kind === "set-name" && typeof data.name === "string") {
            return { version: 1, kind: "set-name", name: data.name };
        }
    }
    return undefined;
}

export function isExtensionOwnedName(name: string | undefined, entries: readonly unknown[]): boolean {
    return name !== undefined && latestAutonameProvenance(entries)?.name === name;
}

export function responseText(content: readonly unknown[]): string {
    return content
        .filter(isRecord)
        .filter((block) => block.type === "text" && typeof block.text === "string")
        .map((block) => block.text as string)
        .join("\n");
}
