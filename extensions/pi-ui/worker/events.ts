import type { FrontendEvent } from "../../worker/frontend.ts";
export function nativeEventNotice(event: FrontendEvent) {
	return event.name === "worker.settings-saved"
		? { message: "Worker 设置已保存，后续任务立即生效", level: "info" as const }
		: { message: `Worker 设置保存失败：${event.data.message}`, level: "error" as const };
}
export const eventNames = ["worker.settings-saved", "worker.settings-error"] as const;
