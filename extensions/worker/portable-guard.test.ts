import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { createEventBus, ExtensionRunner, SessionManager } from "@earendil-works/pi-coding-agent";

const loaderURL = new URL("./core/extensions/loader.js", import.meta.resolve("@earendil-works/pi-coding-agent"));
const { loadExtensions } = await import(loaderURL.href);
const source = fileURLToPath(new URL("./", import.meta.url));

function fixture(t: test.TestContext, mode?: string) {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-portable-guard-"));
	const copied = path.join(root, "arbitrary", "capability");
	fs.cpSync(source, copied, { recursive: true });
	const env: Record<string, string | undefined> = {
		PI_CODING_AGENT_DIR: path.join(root, "unrelated-agent"), PI_WORKER_DEPTH: "1",
		PI_WORKER_MODE: mode, PI_WORKER_CWD: root,
		PI_WORKER_ALLOWED_PATHS: JSON.stringify(["src/**"]),
		PI_WORKER_FORBIDDEN_PATHS: JSON.stringify(["src/private.ts"]),
	};
	const previous = Object.fromEntries(Object.keys(env).map((key) => [key, process.env[key]]));
	for (const [key, value] of Object.entries(env)) {
		if (value === undefined) delete process.env[key]; else process.env[key] = value;
	}
	t.after(() => {
		for (const [key, value] of Object.entries(previous)) {
			if (value === undefined) delete process.env[key]; else process.env[key] = value;
		}
		fs.rmSync(root, { recursive: true, force: true });
	});
	return { root, copied };
}

async function load(root: string, entries: string[]) {
	const loaded = await loadExtensions(entries, root, createEventBus());
	assert.deepEqual(loaded.errors, [], "copied modules must load without sibling directories or node_modules");
	const runner = new ExtensionRunner(loaded.extensions, loaded.runtime, root, SessionManager.inMemory(root), undefined!);
	const failures: unknown[] = [];
	runner.onError((error) => failures.push(error));
	assert.equal(runner.getAllRegisteredTools().length, 0, "child guard/full Worker must not expose the scheduler");
	return {
		loaded, failures,
		call: (name: string, input: unknown) => runner.emitToolCall({ type: "tool_call", toolCallId: "guard-test", toolName: name, input } as any),
	};
}

test("copied Worker resolves bundled roles and installs exactly one guard in either load order", async (t) => {
	const { root, copied } = fixture(t, "fix");
	const marker = "copied-role-resource-marker";
	fs.appendFileSync(path.join(copied, "agents", "fix.md"), `\n${marker}\n`);
	const probe = path.join(copied, "probe.ts");
	fs.writeFileSync(probe, 'import { workerPromptBody } from "./prompt.ts";\nexport default function(pi) { pi.registerCommand("role-probe", {description: workerPromptBody("fix"), handler: async () => {}}); }\n');
	for (const order of [["guard-entry.ts", "index.ts"], ["index.ts", "guard-entry.ts"]]) {
		const { loaded, call, failures } = await load(root, [...order.map((name) => path.join(copied, name)), probe]);
		assert.equal(loaded.extensions.reduce((count: number, ext: any) => count + (ext.handlers.get("tool_call")?.length ?? 0), 0), 1);
		const description = loaded.extensions.at(-1).commands.get("role-probe").description;
		assert.match(description, new RegExp(marker));
		assert.match(description, /allowedPaths/);
		assert.equal(await call("write", { path: "src/allowed.ts", content: "safe" }), undefined);
		assert.equal(await call("edit", { path: "src/allowed.ts", edits: [] }), undefined);
		assert.equal((await call("write", { path: "other.ts", content: "deny" }))?.block, true);
		assert.match((await call("edit", { path: "src/private.ts", edits: [] }))?.reason ?? "", /forbiddenPaths/);
		assert.equal((await call("write", { path: "../outside.ts", content: "deny" }))?.block, true);
		assert.deepEqual(failures, []);
	}
});

for (const [label, mode, invalidAllowed] of [
	["scout", "scout", false], ["review", "review", false],
	["missing contract", undefined, false], ["invalid mode", "invalid", false],
	["malformed write scope", "fix", true],
] as const) {
	test(`standalone copied guard fails closed for ${label} without exposing Worker`, async (t) => {
		const { root, copied } = fixture(t, mode);
		if (invalidAllowed) process.env.PI_WORKER_ALLOWED_PATHS = '"not-an-array"';
		const { call, failures } = await load(root, [path.join(copied, "guard-entry.ts")]);
		for (const tool of ["edit", "write"]) {
			const response = await call(tool, { path: "src/allowed.ts", content: "denied", edits: [] });
			assert.equal(response?.block, true);
			assert.match(response?.reason ?? "", invalidAllowed ? /PI_WORKER_ALLOWED_PATHS 格式无效/ : mode === "scout" || mode === "review" ? /禁止调用 edit\/write/ : /缺少有效的 Worker mode/);
		}
		// The documented contract is not a bash/filesystem sandbox.
		assert.equal(await call("bash", { command: "true" }), undefined);
		assert.deepEqual(failures, []);
	});
}
