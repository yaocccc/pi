/** Native Pi token labels and colors; accounting lives in token-estimate.ts. */
import type { Theme } from "@earendil-works/pi-coding-agent";
export { estimateToolTokens } from "./token-estimate.ts";

// Reset only foreground so the enclosing tool/card background remains intact.
export const colorToolTokens = (tokens: number, text: string, theme: Pick<Theme, 'fg'>): string =>
    tokens >= 10_000 ? theme.fg('error', text)
        : tokens >= 5_000 ? `\x1b[38;2;255;165;0m${text}\x1b[39m`
            : theme.fg('dim', text);

export const formatToolTokens = (tokens: number): string => {
    const count = tokens >= 1_000_000 ? `${(tokens / 1_000_000).toFixed(1)}M`
        : tokens >= 1_000 ? `${(tokens / 1_000).toFixed(1)}k` : String(tokens);
    return `≈${count} tok`;
};

