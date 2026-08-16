export type TranslationStatus =
    | { state: "pending" }
    | { state: "ready"; translation: string };

export const translationBlockKey = (messageTimestamp: number, contentIndex: number): string =>
    `${messageTimestamp}:${contentIndex}`;

export const splitThinkingSegments = (source: string): string[] => source
    .replace(/\r\n?/g, "\n")
    .split(/\n\s*\n+/)
    .map((segment) => segment.replace(/\s+/g, " ").trim())
    .filter(Boolean);

export const normalizeTranslation = (translation: string): string => translation
    .replace(/\r\n?/g, "\n")
    .split("\n")
    .map((line) => line.replace(/[\t\f\v ]+/g, " ").trim())
    .filter(Boolean)
    .join("\n");

export const parseTranslationResponse = (response: string, expectedSegments: number): string | undefined => {
    const cleaned = response.trim().replace(/^```(?:json)?\s*/i, "").replace(/\s*```$/, "");
    try {
        const parsed: unknown = JSON.parse(cleaned);
        if (Array.isArray(parsed) && parsed.length === expectedSegments && parsed.every((item) => typeof item === "string")) {
            const segments = parsed.map((item) => normalizeTranslation(item).replace(/\n+/g, " "));
            if (segments.every(Boolean)) return segments.join("\n");
        }
    } catch {
        // Fall back for providers that ignore the requested JSON envelope.
    }

    const normalized = normalizeTranslation(cleaned);
    if (!normalized) return undefined;
    if (expectedSegments <= 1) return normalized;
    return normalized.split("\n").length >= expectedSegments ? normalized : undefined;
};

export const preservesThinkingSegments = (source: string, translation: string): boolean => {
    const expectedSegments = splitThinkingSegments(source).length;
    if (expectedSegments <= 1) return Boolean(normalizeTranslation(translation));
    return normalizeTranslation(translation).split("\n").length >= expectedSegments;
};

export const formatThinkingDisplay = (thinking: string, status: TranslationStatus): string =>
    status.state === "pending" ? `${thinking} 🌐` : status.translation;
