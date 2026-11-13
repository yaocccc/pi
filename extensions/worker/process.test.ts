import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { createWorkerOwner, runPiWorker } from "./process.ts";

// The local probe reports argv/env without loading Pi or making a model call.
test("child invocation explicitly loads the module-local guard independently of user discovery", async (t) => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-guard-argv-"));
	const originalArgv = process.argv[1];
	process.argv[1] = fileURLToPath(new URL("./fixtures/session-probe.mjs", import.meta.url));
	t.after(() => { process.argv[1] = originalArgv; fs.rmSync(root, { recursive: true, force: true }); });
	const child = await runPiWorker(
		{ mode: "fix", preset: "normal", sessionMode: "new", objective: "probe", allowedPaths: ["src/**"], forbiddenPaths: ["src/private.ts"] },
		{ requestedPreset: "normal", resolvedPreset: "normal", modelId: "fixture/probe", provider: "fixture", thinking: "off", routeReason: [] }, root, "system", "prompt", undefined, 5000, 1, undefined,
		{ owner: createWorkerOwner(), transcriptRoot: path.join(root, "recordings") },
	);
	assert.equal(child.exitCode, 0, child.stderr);
	const report = JSON.parse(child.assistantText);
	const index = report.args.indexOf("-e");
	assert.ok(index >= 0, "child must explicitly load its write guard");
	assert.equal(report.args[index + 1], fileURLToPath(new URL("./guard-entry.ts", import.meta.url)));
	assert.ok(fs.existsSync(report.args[index + 1]));
	assert.equal(report.env.PI_WORKER_DEPTH, "1");
	assert.equal(report.env.PI_WORKER_MODE, "fix");
	assert.deepEqual(JSON.parse(report.env.PI_WORKER_ALLOWED_PATHS), ["src/**"]);
	assert.deepEqual(JSON.parse(report.env.PI_WORKER_FORBIDDEN_PATHS), ["src/private.ts"]);
	assert.ok(report.args.includes("--no-skills"));
	assert.ok(report.args.includes("--no-context-files"));
	assert.ok(report.args.includes("--no-session"));
});
