import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerWorkerWriteGuard } from "./guard.ts";

/** Explicit child-only entry: installs policy enforcement, never the scheduler. */
export default function workerGuard(pi: ExtensionAPI): void {
	registerWorkerWriteGuard(pi);
}
