import assert from "node:assert/strict";
import test from "node:test";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { createEventBus } from "@earendil-works/pi-coding-agent";
import { getCapability } from "../shared/capabilities.ts";
import { ASK_QUESTION_SERVICE, ASK_QUESTION_INTERACTION } from "../ask-question/api.ts";
import { WORKER_CAPABILITY } from "../worker/api.ts";
import { CODEX_FAST_CAPABILITY } from "../codex-fast/api.ts";

// Exercise the real extension loader (including its module isolation), not just
// a mock with one statically imported copy of every module.
const sdk = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
const { loadExtensions } = await import(pathToFileURL(join(sdk, "core/extensions/loader.js")).href);
const root = fileURLToPath(new URL("../", import.meta.url));
const capabilities = ["worker", "ask-question", "codex-fast", "sol-pi"].map(name => join(root, name, "index.ts"));
const adapter = join(root, "pi-ui/index.ts");

for (const order of ["absent", "first", "last"]) test(`real loader: pi-ui ${order}`, async () => {
	const events = createEventBus();
	const oldDepth = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	try {
		const paths = order === "absent" ? capabilities : order === "first" ? [adapter, ...capabilities] : [...capabilities, adapter];
		const result = await loadExtensions(paths, root, events);
		assert.deepEqual(result.errors, []);
		assert.equal(result.extensions.length, paths.length);
		for (const name of [ASK_QUESTION_SERVICE, WORKER_CAPABILITY, CODEX_FAST_CAPABILITY]) {
			assert.ok(getCapability({ events }, name), `${name} must be discoverable`);
		}
		assert.equal(Boolean(getCapability({ events }, ASK_QUESTION_INTERACTION)), order !== "absent");
		const ui = result.extensions.find((extension: any) => extension.path === adapter);
		for (const extension of result.extensions) {
			if (extension === ui) continue;
			assert.equal(extension.toolRenderers?.length ?? 0, 0);
			for (const { definition } of extension.tools.values()) {
				assert.equal(definition.renderCall, undefined);
				assert.equal(definition.renderResult, undefined);
			}
		}
		if (ui) {
			assert.equal(ui.tools.size, 0, "the adapter never replaces capability tools");
			assert.ok(ui.commands.has("worker_settings"));
			assert.ok(ui.commands.has("codex-fast"));
			for (const name of ["worker", "ask_question", "obs_recall", "update_plan"]) {
				assert.ok(ui.toolRenderers.some((resolve: any) => resolve(name, () => undefined)), `${name} renderer`);
			}
		}
	} finally {
		events.clear();
		if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH;
		else process.env.PI_WORKER_DEPTH = oldDepth;
	}
});
