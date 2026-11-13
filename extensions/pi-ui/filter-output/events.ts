import type { FrontendEvent } from "../../filter-output/frontend.ts";
export function nativeEventNotice(event: FrontendEvent) {
	return { message: `Redacted contents of sensitive file: ${event.data.path}`, level: "info" as const };
}
export const eventNames = ["filter.redacted"] as const;
