import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import test from "node:test";

test("Worker capability has no native UI imports, hooks or interaction calls", () => {
	for (const name of readdirSync(new URL(".", import.meta.url)).filter((name) => name.endsWith(".ts") && !name.endsWith(".test.ts"))) {
		const text = readFileSync(new URL(name, import.meta.url), "utf8");
		assert.doesNotMatch(text, /from\s+[\x27\x22][^\x27\x22]*(?:pi-tui|pi-ui|\/ui\.ts|\/native-ui\.ts|\/presentation\.ts|\/tool-tokens\.ts)[\x27\x22]/, name);
		assert.doesNotMatch(text, /\b(?:renderCall|renderResult|WorkerRenderRefresh|isGoUI)\b|ctx\.ui\b/, name);
	}
});
