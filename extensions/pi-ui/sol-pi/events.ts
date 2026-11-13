import type { FrontendEvent } from "../../sol-pi/frontend.ts";
const seconds = (ms: number): string => (ms / 1_000).toFixed(1);
export function nativeEventNotice(event: FrontendEvent): { message: string; level: "info" | "warning" } {
	switch (event.name) {
		case "context.ready": return { message: `上下文摘要已就绪，生成耗时 ${seconds(event.data.generationMs)}s；将在下一个安全边界应用。`, level: "info" };
		case "context.applied": {
			const { beforeTokens: before, afterTokens: after, generationMs, durationMs } = event.data;
			const delta = before - after;
			const change = delta === 0 ? "不变" : `${delta > 0 ? "减少" : "增加"} ${Math.abs(delta).toLocaleString("en-US")}${before > 0 ? `，${(Math.abs(delta) / before * 100).toFixed(1)}%` : ""}`;
			return { message: `上下文已压缩：约 ${before.toLocaleString("en-US")} → ${after.toLocaleString("en-US")} tokens（${change}）；生成 ${seconds(generationMs)}s，总耗时 ${seconds(durationMs)}s（含等待安全边界）。新增消息已保留。`, level: "info" };
		}
		case "context.failed": return { message: `后台上下文压缩未完成（耗时 ${seconds(event.data.durationMs)}s），当前对话不受影响。原因：${event.data.reason}`, level: "warning" };
	}
}
export const eventNames = ["context.ready", "context.applied", "context.failed"] as const;
