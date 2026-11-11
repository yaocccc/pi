import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import { DEFAULT_OPTIONS, THINKING_LEVELS, validateTask } from "./config.ts";
import { until } from "./fixtures/helpers.ts";
import workerExtension, { validateWorkerInput } from "./index.ts";
import { acquireSlot, baseExecution, createWorkerOwner, runPiWorker } from "./process.ts";
import { buildTaskPrompt } from "./prompt.ts";
import { captureWorkerSession, workerSessionArgs, type WorkerSessionSnapshot } from "./session.ts";
import type { Route, WorkerTask } from "./types.ts";
import { compactWorkerResult, emptyWorkerUsage } from "./data.ts";
import { renderWorkerDetails } from "./ui.ts";

const task: WorkerTask = { mode: "scout", preset: "normal", sessionMode: "fork", objective: "fixture" };
const route: Route = { requestedPreset: "normal", resolvedPreset: "normal", modelId: "fixture/local", provider: "fixture", thinking: "high", routeReason: [] };
const message = (content: string) => ({ role: "user" as const, content, timestamp: 1 });
const probe = fileURLToPath(new URL("./fixtures/session-probe.mjs", import.meta.url));
const parentState = (manager: SessionManager) => JSON.stringify({ header: manager.getHeader(), entries: manager.getEntries(), leaf: manager.getLeafId(), file: manager.getSessionFile() });

function temp(t: test.TestContext) {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "worker-session-test-"));
	t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
	return dir;
}
function useProbe(t: test.TestContext) {
	const previous = process.argv[1];
	process.argv[1] = probe;
	t.after(() => { process.argv[1] = previous; });
}
function trackTempDirs(t: test.TestContext, onCreate?: (dir: string) => void) {
	const dirs: string[] = [];
	const original = fs.promises.mkdtemp;
	t.mock.method(fs.promises, "mkdtemp", async (...args: Parameters<typeof original>) => {
		const result = await original(...args);
		dirs.push(String(result));
		onCreate?.(String(result));
		return result;
	});
	return dirs;
}
function launch(mode: WorkerTask["sessionMode"], snapshot?: WorkerSessionSnapshot, prompt = "inspect", signal?: AbortSignal, timeout = 10_000, onProgress?: Parameters<typeof runPiWorker>[8], extra: Record<string, any> = {}) {
	return runPiWorker({ ...task, sessionMode: mode }, route, process.cwd(), "worker-system-fixture", prompt, signal, timeout, 1, onProgress, { owner: createWorkerOwner(), sessionSnapshot: snapshot, ...extra });
}

test("sessionMode is required in schema and direct validation; only new/fork are accepted", async (t) => {
	for (const sessionMode of ["new", "fork"] as const) {
		const value = { ...task, sessionMode };
		assert.doesNotThrow(() => validateWorkerInput({ tasks: [value] }));
		assert.doesNotThrow(() => validateWorkerInput({ tasks: [task, value] }));
		assert.deepEqual(validateTask(value, process.cwd()), []);
		assert.equal(baseExecution(value, route, 1, []).session_mode, sessionMode);
		assert.match(buildTaskPrompt(value, null), new RegExp(`"sessionMode":"${sessionMode}"`));
	}
	const dir = temp(t);
	for (const sessionMode of [undefined, "", "clone", "NEW", null, false, 0, {}]) {
		const value = { ...task, sessionMode } as WorkerTask;
		assert.throws(() => validateWorkerInput({ tasks: [value] }), /参数错误/);
		assert.ok(validateTask(value, process.cwd()).some((error) => error.includes("sessionMode")));
		await assert.rejects(workerSessionArgs(value, dir, dir), /sessionMode/);
	}
	await assert.rejects(workerSessionArgs({ ...task, sessionMode: "fork" }, dir, dir), /快照/);
	const { sessionMode: _omitted, ...missing } = task;
	assert.throws(() => validateWorkerInput({ tasks: [missing] }), /参数错误/);
	await assert.rejects(workerSessionArgs(missing as WorkerTask, dir, dir), /sessionMode/);
});

test("snapshot selects the active leaf, preserves compaction/edits and never mutates persistent or in-memory parents", async (t) => {
	const root = temp(t);
	for (const persisted of [false, true]) {
		const manager = persisted ? SessionManager.create(root, path.join(root, "parent")) : SessionManager.inMemory(root);
		const first = manager.appendMessage(message("old-history"));
		const kept = manager.appendMessage(message("kept-original"));
		manager.appendCompaction("compacted-history", kept, 500);
		manager.appendContextEdit(kept, { content: "kept-edited" });
		manager.appendCustomMessageEntry("fixture", "custom-context", false);
		manager.branchWithSummary(manager.getLeafId(), "branch-context");
		const activeLeaf = manager.getLeafId()!;
		manager.branch(first);
		manager.appendMessage(message("inactive-last-file-entry"));
		manager.branch(activeLeaf); // Intentionally not the last entry in the file.
		const before = parentState(manager);
		const parentFile = manager.getSessionFile();
		const parentBytes = parentFile ? fs.readFileSync(parentFile) : undefined;
		const expectedContext = manager.buildSessionContext();
		const snapshot = captureWorkerSession(manager);
		const childDir = fs.mkdtempSync(path.join(root, "child-"));
		const args = await workerSessionArgs({ ...task, sessionMode: "fork" }, root, childDir, snapshot);
		assert.deepEqual(args, ["--session", path.join(childDir, "worker-session.jsonl"), "--session-dir", childDir]);
		const child = SessionManager.open(args[1], childDir);
		assert.deepEqual(child.getBranch(), JSON.parse(JSON.stringify(manager.getBranch())));
		assert.deepEqual(child.buildSessionContext(), expectedContext);
		assert.notEqual(child.getSessionId(), manager.getSessionId());
		const siblingDir = fs.mkdtempSync(path.join(root, "sibling-"));
		const siblingArgs = await workerSessionArgs({ ...task, sessionMode: "fork" }, root, siblingDir, snapshot);
		const sibling = SessionManager.open(siblingArgs[1], siblingDir);
		assert.notEqual(sibling.getSessionId(), child.getSessionId());
		assert.notEqual(siblingArgs[1], args[1]);
		assert.equal(child.getCwd(), root);
		assert.ok(!JSON.stringify(child.getEntries()).includes("inactive-last-file-entry"));
		child.appendMessage(message("child-only"));
		assert.deepEqual(sibling.buildSessionContext(), expectedContext);
		assert.equal(parentState(manager), before);
		if (parentFile) assert.deepEqual(fs.readFileSync(parentFile), parentBytes);
		// The captured object is independent of later branch changes and nested edits.
		manager.appendMessage(message("parent-after-admission"));
		const entry = manager.getEntry(kept) as any;
		entry.message.content = "mutated-after-admission";
		assert.ok(!snapshot.branchJsonl.includes("parent-after-admission"));
		assert.ok(!snapshot.branchJsonl.includes("mutated-after-admission"));
	}
});

test("empty branch snapshots do not resurrect inactive history", async (t) => {
	const dir = temp(t);
	const manager = SessionManager.inMemory(dir);
	manager.appendMessage(message("inactive"));
	manager.resetLeaf();
	const args = await workerSessionArgs({ ...task, sessionMode: "fork" }, dir, dir, captureWorkerSession(manager));
	assert.deepEqual(SessionManager.open(args[1], dir).getBranch(), []);
});

test("real process arguments retain isolation guards; explicit fork inherits context and explicit new is empty", async (t) => {
	useProbe(t);
	const manager = SessionManager.inMemory(process.cwd());
	manager.appendMessage(message("parent-visible"));
	const snapshot = captureWorkerSession(manager);
	const before = parentState(manager);
	for (const mode of ["new", "fork"] as const) {
		const result = await launch(mode, snapshot);
		assert.equal(result.exitCode, 0, result.stderr);
		const report = JSON.parse(result.assistantText);
		assert.equal(fs.existsSync(report.tempDir), false, "temporary directory cleaned");
		assert.equal(report.dirMode, 0o700);
		assert.equal(report.env.PI_WORKER_DEPTH, "1");
		assert.equal(report.env.PI_WORKER_MODE, "scout");
		assert.equal(report.env.PI_WORKER_CWD, process.cwd());
		assert.equal(report.env.PI_WORKER_ALLOWED_PATHS, "[]");
		assert.equal(report.env.PI_WORKER_FORBIDDEN_PATHS, "[]");
		assert.ok(report.args.includes("--no-skills"));
		assert.ok(report.args.includes("--no-context-files"));
		assert.ok(!report.args.includes("--extension"));
		assert.ok(report.args.includes(route.modelId));
		assert.ok(report.args.includes(route.thinking));
		if (mode !== "new") {
			assert.ok(!report.args.includes("--no-session"));
			assert.equal(report.fileMode, 0o600);
			assert.deepEqual(report.context, manager.buildSessionContext());
			assert.notEqual(report.header.id, manager.getSessionId());
		} else {
			assert.ok(report.args.includes("--no-session"));
			assert.ok(!report.args.includes("--session"));
			assert.deepEqual(report.entries, []);
		}
	}
	assert.equal(parentState(manager), before);
});

test("configured thinking is passed to the child unchanged at every supported level", async (t) => {
	useProbe(t);
	for (const thinking of THINKING_LEVELS) {
		const result = await runPiWorker(
			{ ...task, sessionMode: "new" }, { ...route, thinking },
			process.cwd(), "worker-system-fixture", "inspect", undefined, 10_000, 1,
			undefined, { owner: createWorkerOwner() },
		);
		assert.equal(result.exitCode, 0, result.stderr);
		const report = JSON.parse(result.assistantText);
		assert.equal(report.args[report.args.indexOf("--thinking") + 1], thinking);
	}
});

test("fork temporary files are removed after child failure, cancellation, timeout and spawn failure", async (t) => {
	useProbe(t);
	const dirs = trackTempDirs(t);
	const snapshot = captureWorkerSession(SessionManager.inMemory());
	const failed = await launch("fork", snapshot, "fail");
	assert.equal(failed.exitCode, 7);
	const controller = new AbortController();
	const cancelled = await launch("fork", snapshot, "wait", controller.signal, 10_000, (progress) => {
		if (progress.activities.some((activity) => activity.id === "tool:session-ready")) controller.abort();
	});
	assert.equal(cancelled.aborted, true);
	const timedOut = await launch("fork", snapshot, "wait", undefined, 2000);
	assert.equal(timedOut.timedOut, true);
	const spawnFailed = await launch("fork", snapshot, "inspect", undefined, 10_000, undefined, { invocation: { command: "/nonexistent/pi-session-fixture", args: [] } });
	assert.equal(spawnFailed.termination?.stage, "spawn");
	assert.equal(dirs.length, 4);
	for (const dir of dirs) assert.equal(fs.existsSync(dir), false, dir);
});

test("fork setup failures and cancellation before a slot also clean temporary files", async (t) => {
	let failWrite = false;
	const dirs = trackTempDirs(t, (dir) => {
		if (failWrite) fs.mkdirSync(path.join(dir, "worker-session.jsonl"));
	});
	await assert.rejects(launch("fork"), /快照/);
	const snapshot = captureWorkerSession(SessionManager.inMemory());
	failWrite = true;
	await assert.rejects(launch("fork", snapshot), /EEXIST|EISDIR/);
	failWrite = false;
	const controller = new AbortController();
	controller.abort();
	const cancelled = await launch("fork", snapshot, "inspect", controller.signal);
	assert.equal(cancelled.aborted, true);
	assert.equal(cancelled.termination?.stage, "slot");
	const owner = createWorkerOwner();
	const release = await acquireSlot(1, undefined, 10_000, owner);
	const waitingController = new AbortController();
	const waiting = launch("fork", snapshot, "inspect", waitingController.signal, 10_000, undefined, { owner });
	await until(() => owner.waiters.length === 1);
	waitingController.abort();
	assert.equal((await waiting).aborted, true);
	release();
	assert.equal(owner.slots, 0);
	assert.equal(dirs.length, 4);
	for (const dir of dirs) assert.equal(fs.existsSync(dir), false, dir);
});

test("mixed queued tasks share admission snapshot; new tasks do not inherit it", async (t) => {
	const manager = SessionManager.inMemory();
	manager.appendMessage(message("at-admission"));
	const tools = new Map<string, any>();
	const handlers = new Map<string, any>();
	const seen: Array<WorkerSessionSnapshot | undefined> = [];
	const oldDepth = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	t.after(() => { if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth; });
	const preset = { model: "fixture/local", thinking: "high" as const };
	workerExtension({ on: (event: string, handler: any) => handlers.set(event, handler), registerTool: (tool: any) => tools.set(tool.name, tool), registerCommand() {}, events: { emit() {} } } as any, {
		loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, maxConcurrentWorkers: 1, fast: preset, normal: preset, deep: preset }, warnings: [], path: "fixture" }),
		executeTask: async (...args) => {
			seen.push(args[9]);
			manager.appendMessage(message("after-admission"));
			return { status: "completed", summary: ["fixture"] };
		},
	});
	t.after(() => handlers.get("session_shutdown")({}));
	const result = await tools.get("worker").execute("session-fixture", { tasks: [task, { ...task, sessionMode: "new" }, { ...task, sessionMode: "fork" }] }, undefined, undefined, { cwd: process.cwd(), hasUI: false, sessionManager: manager });
	assert.equal(JSON.parse(result.content[0].text).finished, true);
	assert.equal(seen.length, 3);
	assert.ok(seen[0]?.branchJsonl.includes("at-admission"));
	assert.ok(!seen[0]?.branchJsonl.includes("after-admission"));
	assert.equal(seen[1], undefined);
	assert.strictEqual(seen[0], seen[2]);
	assert.deepEqual(result.details.tasks.map((item: any) => item.sessionMode), ["fork", "new", "fork"]);
});

test("execution compaction and UI preserve the fork indicator", () => {
	for (const level of ["standard", "minimal"] as const) {
		assert.equal(compactWorkerResult({ status: "completed", execution: baseExecution({ ...task, sessionMode: "fork" }, route, 1, []) }, level).execution.session_mode, "fork");
	}
	const theme = { fg: (_: string, text: string) => text } as any;
	const details = { kind: "worker-ui" as const, startedAt: 1, limit: 1, total: 1, completed: 0, tasks: [{ ...task, index: 0, sessionMode: "fork" as const, status: "queued" as const, requestedPreset: "normal" as const, attempt: 0, phase: "queued", activities: [], toolCalls: 0, usage: emptyWorkerUsage() }] };
	assert.match(renderWorkerDetails(details, theme).render(100).join("\n"), /normal · fork/);
});
