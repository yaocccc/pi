import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test from "node:test";
import { validateTask } from "./config.ts";
import type { WorkerTask } from "./types.ts";

test("directory literals are rejected relative to task.cwd, including ./ and trailing slashes", (t) => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-path-validation-"));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	fs.mkdirSync(path.join(root, "package", "backend"), { recursive: true });
	fs.mkdirSync(path.join(root, "package", "frontend"));
	fs.writeFileSync(path.join(root, "package", "backend", "existing.ts"), "");
	const base: WorkerTask = { mode: "implement", objective: "scoped", cwd: "package", allowedPaths: ["backend/**"] };
	for (const declaration of ["backend", "./backend", "backend/", "./backend/"]) {
		const errors = validateTask({ ...base, allowedPaths: [declaration] }, root);
		assert.equal(errors.length, 1, declaration);
		assert.match(errors[0], /allowedPaths.*backend\/\*\*.*task\.cwd/);
	}
	for (const declaration of ["frontend", "./frontend/"]) {
		const errors = validateTask({ ...base, forbiddenPaths: [declaration] }, root);
		assert.equal(errors.length, 1, declaration);
		assert.match(errors[0], /forbiddenPaths.*frontend\/\*\*.*task\.cwd/);
	}
	assert.match(validateTask({ ...base, allowedPaths: ["missing/"] }, root)[0], /missing\/\*\*/);
	assert.match(validateTask({ ...base, forbiddenPaths: ["./future/"] }, root)[0], /future\/\*\*/);
	assert.deepEqual(validateTask({ ...base, allowedPaths: ["backend/existing.ts", "backend/new.ts", "backend/**", "backend/*.ts", "missing", "missing/**"], forbiddenPaths: ["frontend/**", "new.ts"] }, root), []);
	// Resolution must use task.cwd rather than the main session's root.
	assert.deepEqual(validateTask({ ...base, allowedPaths: ["package"] }, root), []);
});
