import type { FrontendEvent } from "../../autoname/frontend.ts";
const compactTokens = (tokens: number): string => tokens >= 1_000_000
	? `${(tokens / 1_000_000).toFixed(1)}m`
	: tokens >= 1_000 ? `${(tokens / 1_000).toFixed(1)}k` : Math.round(tokens).toString();
export function nativeEventNotice(event: FrontendEvent) {
	return { message: `会话已自动命名：${event.data.name} [${compactTokens(event.data.contextTokens)} ${(event.data.durationMs / 1_000).toFixed(1)}s]`, level: "info" as const };
}
export const eventNames = ["autoname.renamed"] as const;
