import assert from "node:assert/strict";
import test from "node:test";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
	attributeChangedFiles,
	declaredWriteBoundary,
	filterChangedFilesToBoundary,
	mapWithConflicts,
	workerTasksConflict,
} from "./security.ts";
import type { WorkerTask } from "./types.ts";

const readTask = (cwd?: string): WorkerTask => ({ mode: "scout", objective: "read", cwd });
const writeTask = (allowedPaths: string[], cwd?: string): WorkerTask => ({ mode: "implement", objective: "write", allowedPaths, cwd });

async function workspace(t: test.TestContext): Promise<string> {
	const root = await mkdtemp(join(tmpdir(), "worker-security-"));
	t.after(() => rm(root, { recursive: true, force: true }));
	return root;
}

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

test("dangling symlink boundaries are unknown and conflict conservatively", async (t) => {
	const root = await workspace(t);
	await mkdir(join(root, "other"), { recursive: true });
	await symlink(join(root, "missing"), join(root, "dangling"), "dir");
	assert.equal(declaredWriteBoundary(writeTask(["dangling/**"]), root).unknown, true);
	assert.equal(workerTasksConflict(writeTask(["dangling/**"]), writeTask(["other/**"]), root), true);
});

test("scheduler scans past a blocked head, preserves order, isolates failures, and honors limit", async () => {
	const items = [
		{ id: 0, group: "a", delay: 35 },
		{ id: 1, group: "a", delay: 1 },
		{ id: 2, group: "b", delay: 5, fail: true },
		{ id: 3, group: "c", delay: 1 },
	];
	const starts: number[] = [];
	let active = 0;
	let maximum = 0;
	const results = await mapWithConflicts(items, 2, (left, right) => left.group === right.group, async (item) => {
		starts.push(item.id);
		active++;
		maximum = Math.max(maximum, active);
		await new Promise((resolve) => setTimeout(resolve, item.delay));
		active--;
		if (item.fail) throw new Error("isolated");
		return { status: "completed", id: item.id };
	});

	assert.deepEqual(starts.slice(0, 2), [0, 2]);
	assert.equal(maximum, 2);
	assert.deepEqual(results.map((item) => item.id), [0, 1, undefined, 3]);
	assert.deepEqual(results[2], { status: "failed", summary: ["isolated"] });
});

test("scheduler does not reorder tasks that conflict with an earlier pending task", async () => {
	const items = [
		{ id: 0, scopes: ["x"], delay: 10 },
		{ id: 1, scopes: ["x", "a"], delay: 2 },
		{ id: 2, scopes: ["a"], delay: 1 },
	];
	const starts: number[] = [];
	await mapWithConflicts(items, 2, (left, right) => left.scopes.some((scope) => right.scopes.includes(scope)), async (item) => {
		starts.push(item.id);
		await new Promise((resolve) => setTimeout(resolve, item.delay));
		return item.id;
	});
	assert.deepEqual(starts, [0, 1, 2]);
});

test("limit one leaves every execution unmarked as overlapping", async () => {
	const overlaps = await mapWithConflicts([0, 1], 1, () => false, async (_item, _index, execution) => {
		await new Promise((resolve) => setTimeout(resolve, 2));
		return [...execution.overlappingIndices];
	});
	assert.deepEqual(overlaps, [[], []]);
});

test("scheduler marks real overlap in both mutable execution contexts", async () => {
	const items = [
		{ id: 0, delay: 35 },
		{ id: 1, delay: 10 },
		{ id: 2, delay: 5 },
	];
	let firstBeforeLaterStart: number[] = [];
	let firstAfterLaterStart: number[] = [];
	let laterContext: number[] = [];
	await mapWithConflicts(items, 2, (left, right) => new Set([left.id, right.id]).size === 2 && left.id !== 0 && right.id !== 0, async (item, _index, execution) => {
		if (item.id === 0) {
			await new Promise((resolve) => setTimeout(resolve, 2));
			firstBeforeLaterStart = [...execution.overlappingIndices];
			await new Promise((resolve) => setTimeout(resolve, item.delay));
			firstAfterLaterStart = [...execution.overlappingIndices];
		} else {
			if (item.id === 2) laterContext = [...execution.overlappingIndices];
			await new Promise((resolve) => setTimeout(resolve, item.delay));
		}
		return item.id;
	});
	assert.deepEqual(firstBeforeLaterStart, [1]);
	assert.deepEqual(firstAfterLaterStart, [1, 2]);
	assert.deepEqual(laterContext, [0]);
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
