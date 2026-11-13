/** Optional whole-package compatibility facade; standalone adapters use support.ts. */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerFactUI } from "./portable.ts";
import { nativeEventNotice } from "./native-events.ts";
export { supportsStructuredUI, emitStructuredEvent } from "./portable.ts";
export function registerFrontendEvents(pi: ExtensionAPI): void {
	registerFactUI(pi, ["autoname.renamed", "filter.redacted", "context.ready", "context.applied", "context.failed", "worker.settings-saved", "worker.settings-error"], nativeEventNotice);
}
