/** Compatibility aggregate. Standalone UI modules own their presentation. */
import type { FrontendEvent } from "../../shared/frontend-events.ts";
import { nativeEventNotice as autoname } from "../autoname/events.ts";
import { nativeEventNotice as filter } from "../filter-output/events.ts";
import { nativeEventNotice as worker } from "../worker/events.ts";
import { nativeEventNotice as context } from "../sol-pi/events.ts";
export function nativeEventNotice(event: FrontendEvent): { message: string; level: "info" | "warning" | "error" } {
	switch (event.name) {
		case "autoname.renamed": return autoname(event);
		case "filter.redacted": return filter(event);
		case "worker.settings-saved": case "worker.settings-error": return worker(event);
		default: return context(event);
	}
}
