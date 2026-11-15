import assert from "node:assert/strict";
import test from "node:test";
import { chmod, mkdir, mkdtemp, readFile, rm, symlink, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	attributeChangedFiles,
	changedSince,
	fileHash,
	runCommand,
	snapshotWorkspace,
	declaredWriteBoundary,
	filterChangedFilesToBoundary,
	workerTaskScopesConflict,
} from "./security.ts";
import type { WorkerTask } from "./types.ts";
import { validateWorkerWritePath } from "./guard.ts";

const workerTasksConflict = (left: WorkerTask, right: WorkerTask, cwd: string) => workerTaskScopesConflict(left, cwd, right, cwd);

const readTask = (cwd?: string): WorkerTask => ({ mode: "scout", preset: "normal", sessionMode: "fork", objective: "read", cwd });
const writeTask = (allowedPaths: string[], cwd?: string): WorkerTask => ({ mode: "implement", preset: "normal", sessionMode: "fork", objective: "write", allowedPaths, cwd });

async function workspace(t: test.TestContext): Promise<string> {
	const root = await mkdtemp(join(tmpdir(), "worker-security-"));
	t.after(() => rm(root, { recursive: true, force: true }));
	return root;
}

test("already-dirty files retain content while worktree and index executable modes are detected", async (t) => {
	const root = await workspace(t);
	const git = async (...args: string[]) => assert.equal((await runCommand("git", args, root)).code, 0);
	await git("init", "-q");
	await git("config", "core.filemode", "true");
	const file = join(root, "file.txt");
	await writeFile(file, "original");
	await chmod(file, 0o644);
	await git("add", "file.txt");
	await writeFile(file, "user modification");
	const before = await snapshotWorkspace(root);
	assert.equal(before.statusPaths.has("file.txt"), true);
	await chmod(file, 0o755);
	const changed = await changedSince(before);
	assert.deepEqual(changed.changed, ["file.txt"]);
	assert.deepEqual(changed.after.statusPaths, before.statusPaths);
	assert.equal(await readFile(file, "utf8"), "user modification");
	await git("update-index", "--chmod=+x", "file.txt");
	assert.deepEqual((await changedSince(changed.after)).changed, ["file.txt"]);
	assert.equal(await readFile(file, "utf8"), "user modification");
});

test("symlink fingerprints use link text even when both targets contain identical bytes", async (t) => {
	const root = await workspace(t);
	assert.equal((await runCommand("git", ["init", "-q"], root)).code, 0);
	await writeFile(join(root, "a"), "same bytes");
	await writeFile(join(root, "b"), "same bytes");
	const link = join(root, "link");
	await symlink("a", link);
	assert.equal((await runCommand("git", ["add", "."], root)).code, 0);
	const before = await snapshotWorkspace(root);
	const oldBytes = await readFile(link);
	const oldHash = fileHash(link);
	await unlink(link);
	await symlink("b", link);
	// Minimal reproduction of the old stat/readFile implementation's blind spot.
	assert.deepEqual(await readFile(link), oldBytes);
	assert.notEqual(fileHash(link), oldHash);
	assert.deepEqual((await changedSince(before)).changed, ["link"]);
});

test("read/read is concurrent while read/write is exclusive", async (t) => {
	const root = await workspace(t);
	assert.equal(workerTasksConflict(readTask(), readTask(), root), false);
	assert.equal(workerTasksConflict(readTask(), writeTask(["a.ts"]), root), true);
});

test("directory literals are exact while explicit globs describe subtrees", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "src", "left"), { recursive: true });
	await mkdir(join(root, "src", "right"), { recursive: true });
	await writeFile(join(root, "src", "left.ts"), "");
	await writeFile(join(root, "src", "right.ts"), "");

	assert.equal(workerTasksConflict(writeTask(["src/left.ts"]), writeTask(["src/right.ts"]), root), false);
	assert.equal(workerTasksConflict(writeTask(["src/left.ts"]), writeTask(["src/left.ts"]), root), true);
	assert.equal(workerTasksConflict(writeTask(["src"]), writeTask(["src/left/file.ts"]), root), true);
	assert.equal(workerTasksConflict(writeTask(["src"]), writeTask(["src/**"]), root), true);
	assert.equal(workerTasksConflict(writeTask(["src/left/**"]), writeTask(["src/right/**"]), root), false);
	assert.equal(workerTasksConflict(writeTask(["src/**"]), writeTask(["src/left/*.ts"]), root), true);
	assert.equal(workerTasksConflict(writeTask(["src/*.ts"]), writeTask(["src/*.js"]), root), true);
	assert.equal(workerTasksConflict(writeTask(["*.ts"]), writeTask(["other/file.ts"]), root), true);
	assert.equal(workerTasksConflict(writeTask(["src/[ab].ts"]), writeTask(["other/file.ts"]), root), true);
});

test("task cwd boundaries use canonical main-workspace coordinates", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "packages", "a", "src"), { recursive: true });
	await mkdir(join(root, "packages", "b", "src"), { recursive: true });

	assert.equal(workerTasksConflict(
		writeTask(["src/**"], "packages/a"),
		writeTask(["src/**"], "packages/b"),
		root,
	), false);
	assert.equal(workerTasksConflict(
		writeTask(["a/src/**"], "packages"),
		writeTask(["src/**"], "packages/a"),
		root,
	), true);
	assert.equal(workerTasksConflict(
		writeTask(["*.ts"], "packages/a"),
		writeTask(["src/**"], "packages/b"),
		root,
	), true);
});

test("symlink aliases resolve to one write boundary", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "real"), { recursive: true });
	await symlink(join(root, "real"), join(root, "alias"), "dir");
	assert.equal(workerTasksConflict(writeTask(["alias/**"]), writeTask(["real/**"]), root), true);
});

test("path boundaries preserve filename whitespace and distinguish dot names from parent traversal", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, " spaced"));
	await mkdir(join(root, "..cache"));
	assert.equal(workerTasksConflict(writeTask([" spaced/**"]), writeTask(["new.ts"], " spaced"), root), true);
	assert.equal(workerTasksConflict(writeTask(["..cache/**"]), writeTask(["new.ts"], "..cache"), root), true);
	assert.equal(workerTasksConflict(writeTask(["..cache/**"]), writeTask(["..cache/..file.ts"]), root), true);
	const policy = { mode: "implement" as const, cwd: root, allowedPaths: ["..cache/**", " spaced/**"], forbiddenPaths: [] };
	assert.equal(validateWorkerWritePath(policy, "..cache/..file.ts"), undefined);
	assert.equal(validateWorkerWritePath(policy, " spaced/new.ts"), undefined);
	assert.match(validateWorkerWritePath(policy, "../escape.ts")!, /越出/);
});

test("dangling symlink boundaries are unknown and conflict conservatively", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "other"), { recursive: true });
	await symlink(join(root, "missing"), join(root, "dangling"), "dir");
	assert.equal(declaredWriteBoundary(writeTask(["dangling/**"]), root).unknown, true);
	assert.equal(workerTasksConflict(writeTask(["dangling/**"]), writeTask(["other/**"]), root), true);
});

test("changed_files attribution preserves raw observations and only filters real overlap", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "packages", "a", "src"), { recursive: true });
	await mkdir(join(root, "packages", "b", "src"), { recursive: true });
	const task = writeTask(["src/**"], "packages/a");
	const observed = ["src/own.ts", "../b/src/sibling.ts", "../shared.ts"];

	assert.deepEqual(filterChangedFilesToBoundary(task, root, observed), ["src/own.ts"]);
	assert.deepEqual(attributeChangedFiles(task, root, observed, false), {
		changedFiles: observed,
		observedChangedFiles: observed,
		attributedToDeclaredPaths: false,
	});
	assert.deepEqual(attributeChangedFiles(task, root, observed, true), {
		changedFiles: ["src/own.ts"],
		observedChangedFiles: observed,
		attributedToDeclaredPaths: true,
	});
});

test("changed-file filtering matches exact literal and glob guard semantics", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "src"), { recursive: true });
	assert.deepEqual(filterChangedFilesToBoundary(writeTask(["src"]), root, ["src", "src/file.ts"]), ["src"]);
	assert.deepEqual(filterChangedFilesToBoundary(writeTask(["src/**"]), root, ["src", "src/file.ts"]), ["src/file.ts"]);
	assert.deepEqual(filterChangedFilesToBoundary(writeTask(["src/*.ts"]), root, ["src/file.ts", "src/file.js", "src/nested/file.ts"]), ["src/file.ts"]);
});

test("nested aliases cannot escape canonical write scopes or forbidden targets", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "left"));
	await mkdir(join(root, "right"));
	await writeFile(join(root, "right", "existing.ts"), "");
	await symlink(join(root, "right"), join(root, "left", "link"), "dir");
	await symlink(join(root, "right", "existing.ts"), join(root, "left", "file-alias"), "file");
	const policy = { mode: "implement" as const, cwd: root, allowedPaths: ["left/**"], forbiddenPaths: [] as string[] };
	for (const file of ["left/link/existing.ts", "left/link/new/deep.ts", "left/file-alias"]) {
		assert.match(validateWorkerWritePath(policy, file)!, /超出 allowedPaths/);
		assert.match(validateWorkerWritePath({ ...policy, allowedPaths: ["left/**", "right/**"], forbiddenPaths: ["right/**"] }, file)!, /命中 forbiddenPaths/);
	}
	assert.match(validateWorkerWritePath({ ...policy, allowedPaths: ["right/**"], forbiddenPaths: ["left/link/**"] }, "right/new.ts")!, /命中 forbiddenPaths/);
	// Explicit alias prefixes retain their meaning, and the scheduler locks their real targets.
	assert.equal(validateWorkerWritePath({ ...policy, allowedPaths: ["left/link/**"] }, "left/link/new.ts"), undefined);
	assert.equal(workerTasksConflict(writeTask(["left/link/**"]), writeTask(["right/**"]), root), true);
	assert.equal(validateWorkerWritePath({ ...policy, allowedPaths: ["left/file-alias"] }, "left/file-alias"), undefined);
	assert.equal(workerTasksConflict(writeTask(["left/file-alias"]), writeTask(["right/existing.ts"]), root), true);
	// A broad subtree remains independent only because escaping aliases above are denied.
	assert.equal(workerTasksConflict(writeTask(["left/**"]), writeTask(["right/**"]), root), false);
	assert.equal(validateWorkerWritePath({ ...policy, allowedPaths: ["left/**", "right/**"] }, "left/link/new.ts"), undefined);
	assert.equal(workerTasksConflict(writeTask(["left/**", "right/**"]), writeTask(["right/**"]), root), true);
});

test("write guard keeps exact file and subtree glob semantics without widening bare paths", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "backend"));
	const base = { mode: "implement" as const, cwd: root, forbiddenPaths: [] };
	assert.match(validateWorkerWritePath({ ...base, allowedPaths: ["backend"] }, "backend/new.ts")!, /超出 allowedPaths/);
	assert.equal(validateWorkerWritePath({ ...base, allowedPaths: ["backend/**"] }, "backend/new.ts"), undefined);
	assert.equal(validateWorkerWritePath({ ...base, allowedPaths: ["backend/new.ts"] }, "backend/new.ts"), undefined);
	assert.match(validateWorkerWritePath({ ...base, allowedPaths: ["backend/new.ts"] }, "backend/other.ts")!, /超出 allowedPaths/);
	assert.equal(validateWorkerWritePath({ ...base, allowedPaths: ["backend/**"], forbiddenPaths: ["backend"] }, "backend/new.ts"), undefined);
	assert.match(validateWorkerWritePath({ ...base, allowedPaths: ["backend/**"], forbiddenPaths: ["backend/**"] }, "backend/new.ts")!, /命中 forbiddenPaths/);
});
