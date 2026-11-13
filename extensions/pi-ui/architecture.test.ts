import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import test from "node:test";

async function sources(directory: URL): Promise<URL[]> {
	const files: URL[] = [];
	for (const entry of await readdir(directory, { withFileTypes: true })) {
		if (["node_modules", "pi-ui", "tests", ".git"].includes(entry.name)) continue;
		const path = new URL(entry.name + (entry.isDirectory() ? "/" : ""), directory);
		if (entry.isDirectory()) files.push(...await sources(path));
		else if (entry.name.endsWith(".ts") && !entry.name.endsWith(".test.ts")) files.push(path);
	}
	return files;
}

test("capability sources do not depend on host UI implementations or frontend identity", async () => {
	for (const path of await sources(new URL("../", import.meta.url))) {
		const source = await readFile(path, "utf8");
		assert.doesNotMatch(source, /from\s+["'][^"']*(?:pi-ui|pi-tui)[^"']*["']/, path.pathname);
		assert.doesNotMatch(source, /\b(?:ctx|context)\??\.(?:ui|hasUI)\b|\["ui"\]|\['ui'\]/, path.pathname);
		assert.doesNotMatch(source, /\b(?:renderCall|renderResult|renderShell|registerToolRenderer|registerMessageRenderer|isGoUI|supportsStructuredUI|PI_GOUI_PROTOCOL|PI_UI_PROTOCOL)\b/, path.pathname);
	}
});

test("Worker settings UI consumes contracts rather than backend persistence modules", async () => {
	for (const path of await sources(new URL("./worker/", import.meta.url))) {
		const source = await readFile(path, "utf8");
		assert.doesNotMatch(source, /from\s+["\x27][^"\x27]*worker\/(?:config|settings|runtime|process|session|transcript-reader|transcript-retention)\.ts["\x27]/, path.pathname);
	}
});

test("Worker public contract owns its DTOs without importing storage implementations", async () => {
	const source = await readFile(new URL("../worker/api.ts", import.meta.url), "utf8");
	assert.doesNotMatch(source, /from\s+["\x27](?:node:|\.\/(?:config|settings|runtime|process|session|transcript-reader|transcript-retention)\.ts)/);
});
