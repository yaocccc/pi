import type { FrontendEvent } from "./frontend-events.ts";

const compactTokens = (tokens: number): string => tokens >= 1_000_000
	? `${(tokens / 1_000_000).toFixed(1)}m`
	: tokens >= 1_000 ? `${(tokens / 1_000).toFixed(1)}k` : Math.round(tokens).toString();
const seconds = (ms: number): string => (ms / 1_000).toFixed(1);

/** Native Pi notification adapter. Never used to format Go UI events. */
export function nativeEventNotice(event: FrontendEvent): { message: string; level: "info" | "warning" | "error" } {
	switch (event.name) {
		case "autoname.renamed": return { message: `会话已自动命名：${event.data.name} [${compactTokens(event.data.contextTokens)} ${seconds(event.data.durationMs)}s]`, level: "info" };
		case "filter.redacted": return { message: `Redacted contents of sensitive file: ${event.data.path}`, level: "info" };
		case "context.ready": return { message: `上下文摘要已就绪，生成耗时 ${seconds(event.data.generationMs)}s；将在下一个安全边界应用。`, level: "info" };
		case "context.applied": {
			const { beforeTokens: before, afterTokens: after, generationMs, durationMs } = event.data;
			const delta = before - after;
			const change = delta === 0 ? "不变" : `${delta > 0 ? "减少" : "增加"} ${Math.abs(delta).toLocaleString("en-US")}${before > 0 ? `，${(Math.abs(delta) / before * 100).toFixed(1)}%` : ""}`;
			return { message: `上下文已压缩：约 ${before.toLocaleString("en-US")} → ${after.toLocaleString("en-US")} tokens（${change}）；生成 ${seconds(generationMs)}s，总耗时 ${seconds(durationMs)}s（含等待安全边界）。新增消息已保留。`, level: "info" };
		}
		case "context.failed": return { message: `后台上下文压缩未完成（耗时 ${seconds(event.data.durationMs)}s），当前对话不受影响。原因：${event.data.reason}`, level: "warning" };
		case "worker.settings-saved": return { message: "Worker 设置已保存，后续任务立即生效", level: "info" };
		case "worker.settings-error": return { message: `Worker 设置保存失败：${event.data.message}`, level: "error" };
	}
}
