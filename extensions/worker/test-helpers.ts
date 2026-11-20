import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { TestContext } from "node:test";
import { DEFAULT_OPTIONS } from "./config.ts";
import type { RoutingConfig, Thinking } from "./types.ts";

export const settings = (thinking: Thinking = "high"): RoutingConfig => ({
	...DEFAULT_OPTIONS,
	fast: { model: "fixture/fast", thinking: "high" },
	normal: { model: "fixture/normal", thinking: "high" },
	deep: { model: "fixture/deep", thinking },
});

export const context = (maxThinking = true) => ({
	modelRegistry: { getAvailable: () => ["fast", "normal", "deep"].map((id) => ({
		provider: "fixture", id, reasoning: true,
		thinkingLevelMap: { xhigh: "high", ...(maxThinking ? { max: "max" } : {}) },
	})) },
}) as any;

export function settingsFile(t: TestContext): string {
	const root = mkdtempSync(join(tmpdir(), "worker-settings-test-"));
	const previous = process.env.PI_CODING_AGENT_DIR;
	t.after(() => {
		if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
		else process.env.PI_CODING_AGENT_DIR = previous;
		rmSync(root, { recursive: true, force: true });
	});
	process.env.PI_CODING_AGENT_DIR = root;
	return join(root, "worker-settings.json");
}
