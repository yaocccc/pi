import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test from "node:test";
import { validateWorkerWritePath } from "./guard.ts";

test("write guard keeps exact file and subtree glob semantics without widening bare paths", (t) => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-write-guard-"));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	fs.mkdirSync(path.join(root, "backend"));
	const base = { mode: "implement" as const, cwd: root, forbiddenPaths: [] };
	assert.match(validateWorkerWritePath({ ...base, allowedPaths: ["backend"] }, "backend/new.ts")!, /超出 allowedPaths/);
	assert.equal(validateWorkerWritePath({ ...base, allowedPaths: ["backend/**"] }, "backend/new.ts"), undefined);
	assert.equal(validateWorkerWritePath({ ...base, allowedPaths: ["backend/new.ts"] }, "backend/new.ts"), undefined);
	assert.match(validateWorkerWritePath({ ...base, allowedPaths: ["backend/new.ts"] }, "backend/other.ts")!, /超出 allowedPaths/);
	assert.equal(validateWorkerWritePath({ ...base, allowedPaths: ["backend/**"], forbiddenPaths: ["backend"] }, "backend/new.ts"), undefined);
	assert.match(validateWorkerWritePath({ ...base, allowedPaths: ["backend/**"], forbiddenPaths: ["backend/**"] }, "backend/new.ts")!, /命中 forbiddenPaths/);
});
