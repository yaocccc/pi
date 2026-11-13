import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerFactUI } from "./support.ts";
import { eventNames, nativeEventNotice } from "./events.ts";

export function registerAutonameUI(pi: ExtensionAPI): void {
	registerFactUI(pi, eventNames, nativeEventNotice);
}
export default registerAutonameUI;
