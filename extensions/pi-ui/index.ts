import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerAskQuestionUI } from "./ask-question/index.ts";
import { registerCodexFastUI } from "./codex-fast/index.ts";
import { registerAutonameUI } from "./autoname/index.ts";
import { registerFilterOutputUI } from "./filter-output/index.ts";
import { registerSolPiUI } from "./sol-pi/index.ts";
import { registerWorkerUI } from "./worker/index.ts";
import { registerExtensionRpc } from "./rpc.ts";

/** Optional frontend adapter. Capability extensions never import this entrypoint. */
export default function piUI(pi: ExtensionAPI): void {
	registerExtensionRpc(pi);
	registerAutonameUI(pi);
	registerFilterOutputUI(pi);
	registerAskQuestionUI(pi);
	registerWorkerUI(pi);
	registerCodexFastUI(pi);
	registerSolPiUI(pi);
}
