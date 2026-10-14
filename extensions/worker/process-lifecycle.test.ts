import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { DEFAULT_OPTIONS } from "./config.ts";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { acquireSlot, beginWorkerShutdown, createWorkerOwner, executeTask, runPiWorker, type WorkerOwner } from "./process.ts";
import { until } from "./fixtures/helpers.ts";
import { gitChangedPaths, runCommand } from "./security.ts";
import type { ChildProgress, Route, RoutingConfig, WorkerTask } from "./types.ts";

const route: Route = { requestedPreset: "fast", resolvedPreset: "fast", modelId: "fixture/local", provider: "fixture", thinking: "high", routeReason: [] };
const fixture = fileURLToPath(new URL("./fixtures/child.mjs", import.meta.url));
const launch = (owner: WorkerOwner, scenario: string, signal?: AbortSignal, timeout = 10_000, progress?: (progress: ChildProgress) => void) => runPiWorker(
	{ mode: "scout", preset: "normal", objective: "fixture", sessionMode: "new" }, route, process.cwd(), "fixture", "fixture", signal, timeout, 1, progress,
	{ owner, invocation: { command: process.execPath, args: ["--experimental-transform-types", fixture, scenario] } },
);

test("tool activities estimate complete payloads once across duplicate completion events", async () => {
	const owner = createWorkerOwner();
	const snapshots: ChildProgress[] = [];
	try {
		await launch(owner, "tool-tokens", undefined, 10_000, (progress) => snapshots.push(progress));
		const activity = snapshots.at(-1)!.activities.find((item) => item.id === "tool:read-1")!;
		assert.equal(activity.contextSizeEstimate, Math.ceil(JSON.stringify({ path: "example.ts" }).length / 4) + 750);
		assert.ok(activity.detail!.length < 3000, "count uses full result, not preview");
		assert.equal(snapshots.at(-1)!.toolCalls, 1);
	} finally { beginWorkerShutdown(owner); }
});

test("owner shutdown rejects only its own waiters and slot releases are idempotent", async () => {
	const a = createWorkerOwner(), b = createWorkerOwner();
	const releaseA = await acquireSlot(1, undefined, 1_000, a);
	const releaseB = await acquireSlot(1, undefined, 1_000, b);
	const waitA = assert.rejects(acquireSlot(1, undefined, 1_000, a), /取消/);
	beginWorkerShutdown(a); beginWorkerShutdown(a);
	await waitA;
	assert.equal(a.waiters.length, 0); assert.equal(b.closed, false); assert.equal(b.slots, 1);
	releaseA(); releaseA(); releaseB(); releaseB();
	assert.equal(a.slots, 0); assert.equal(b.slots, 0);
});

test("standalone timeout and pre-spawn task timeout retain explicit sources without claiming parent death", async () => {
	const owner = createWorkerOwner();
	const timeout = await launch(owner, "hang", undefined, 200);
	assert.equal(timeout.timedOut, true); assert.equal(timeout.aborted, false);
	assert.equal(timeout.termination?.source, "timeout");
	const controller = new AbortController();
	controller.abort(Object.assign(new Error("Worker 任务超时"), { source: "task_timeout" }));
	const early = await launch(owner, "done", controller.signal);
	assert.equal(early.timedOut, true); assert.equal(early.aborted, false);
	assert.equal(early.termination?.source, "task_timeout");
	assert.equal(owner.children.size, 0); assert.equal(owner.slots, 0);
});

test("raw onProgress throws throughout cancellation cannot suppress SIGKILL escalation or settlement", { timeout: 8_000 }, async () => {
	const owner = createWorkerOwner();
	const controller = new AbortController();
	const phases: string[] = [];
	let ready = false;
	const work = launch(owner, "stubborn", controller.signal, 10_000, (progress) => {
		ready ||= progress.activities.some((activity) => activity.id === "tool:fixture-ready");
		phases.push(progress.phase);
		throw new Error("fixture raw progress failure");
	});
	try {
		await until(() => ready);
		controller.abort();
		const result = await work;
		assert.equal(result.aborted, true);
		assert.ok(phases.includes("正在取消"));
		assert.ok(phases.includes("已取消"));
		assert.equal(owner.children.size, 0);
		assert.equal(owner.slots, 0);
		assert.doesNotMatch(result.errorMessage!, /FORCE_SETTLE/);
	} finally { beginWorkerShutdown(owner); }
});

test("SIGTERM resistance and suppressed close reach bounded strong settlement; delayed close cannot settle twice", { timeout: 10_000 }, async () => {
	const owner = createWorkerOwner();
	const controller = new AbortController();
	let settled = 0;
	let ready = false;
	const work = launch(owner, "stubborn", controller.signal, 10_000, (progress) => { ready ||= progress.activities.some((activity) => activity.id === "tool:fixture-ready"); }).then((result) => { settled++; return result; });
	await until(() => owner.children.size === 1);
	const child = [...owner.children][0];
	assert.equal(child.stdio.length, 3, "worker uses only stdin/stdout/stderr");
	const originalEmit = child.emit;
	let delayedClose: any[] | undefined;
	child.emit = function (event: string | symbol, ...args: any[]) {
		if (event === "close") { delayedClose = args; return true; }
		return originalEmit.call(this, event, ...args);
	};
	try {
		await until(() => ready);
		controller.abort();
		const result = await work;
		assert.equal(result.exitCode, 1); assert.equal(result.aborted, true);
		assert.equal(result.termination?.source, "abort_signal");
		assert.match(result.errorMessage!, /cleanup=FORCE_SETTLE_NO_CLOSE/);
		assert.equal(settled, 1); assert.equal(owner.slots, 0);
		assert.equal(delayedClose?.[1], "SIGKILL", "SIGKILL closed the resistant child even when the event was hidden");
		assert.equal(owner.children.size, 1, "unconfirmed close remains tracked for the exit fallback");
		originalEmit.call(child, "close", ...delayedClose!);
		assert.equal(owner.children.size, 0);
		assert.equal(owner.stops.size, 0);
		assert.equal(settled, 1);
	} finally {
		child.emit = originalEmit;
		if (delayedClose) originalEmit.call(child, "close", ...delayedClose);
		beginWorkerShutdown(owner);
	}
});

function processRunning(pid: number): boolean {
	try {
		process.kill(pid, 0);
		if (process.platform === "linux") {
			const stat = fs.readFileSync(`/proc/${pid}/stat`, "utf8");
			return !["Z", "X"].includes(stat.slice(stat.lastIndexOf(") ") + 2).split(" ")[0]);
		}
		return true;
	} catch { return false; }
}

test("normal leader exit reaps resistant descendants before releasing ownership and slots", { timeout: 15_000, skip: process.platform === "win32" }, async () => {
	for (const pipes of ["ignore", "inherit", "cancel"]) {
		const owner = createWorkerOwner();
		const controller = new AbortController();
		let leader: number | undefined;
		const script = `
			const {spawn} = require("node:child_process");
			const child = spawn(process.execPath, ["-e", 'process.on("SIGTERM",()=>{}); setInterval(()=>{},1000); process.send("ready"); process.disconnect();'], {stdio:["ignore", "${pipes === "cancel" ? "ignore" : pipes}", "ignore", "ipc"]});
			child.once("message", () => {
				const message = {type:"message_end", message:{role:"assistant", content:[{type:"text", text:JSON.stringify({status:"completed", pid:child.pid})}]}};
				process.stdout.write(JSON.stringify(message)+"\\n", () => process.exit(0));
			});
		`;
		const work = runPiWorker({ mode: "scout", preset: "fast", sessionMode: "new", objective: "descendants" }, route, process.cwd(), "fixture", "fixture", controller.signal, 8_000, 1, (progress) => {
			if (pipes === "cancel" && progress.phase === "清理子进程") controller.abort();
		}, {
			owner, invocation: { command: process.execPath, args: ["-e", script] },
		});
		try {
			await until(() => owner.children.size === 1);
			const child = [...owner.children][0];
			leader = child.pid;
			await until(() => child.exitCode !== null);
			assert.equal(owner.slots, 1, "slot held through descendant cleanup");
			assert.equal(owner.children.size, 1, "closed group leader remains owned during cleanup");
			const result = await work;
			const pid = JSON.parse(result.assistantText).pid;
			assert.equal(processRunning(pid), false, "descendant cannot outlive completed work");
			assert.equal(result.exitCode, 0);
			assert.equal(result.timedOut, false);
			assert.equal(result.aborted, pipes === "cancel");
			if (pipes === "cancel") assert.equal(result.termination?.source, "abort_signal");
			else assert.equal(result.errorMessage, undefined);
			assert.equal(owner.slots, 0);
			assert.equal(owner.children.size, 0);
		} finally {
			if (leader) { try { process.kill(-leader, "SIGKILL"); } catch {} }
			beginWorkerShutdown(owner);
			await work;
		}
	}
});

test("auxiliary command cancellation and timeout await resistant child cleanup", { timeout: 8_000 }, async (t) => {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "worker-command-cleanup-"));
	t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
	for (const reason of ["abort", "timeout"]) {
		const ready = path.join(dir, reason);
		const controller = new AbortController();
		const script = `process.on("SIGTERM",()=>{}); require("node:fs").writeFileSync(${JSON.stringify(ready)}, String(process.pid)); setInterval(()=>{},1000);`;
		const work = runCommand(process.execPath, ["-e", script], dir, { timeoutMs: reason === "timeout" ? 600 : 5_000, signal: controller.signal }).then(() => undefined, (error: Error) => error);
		let pid: number | undefined;
		try {
			await until(() => fs.existsSync(ready));
			pid = Number(fs.readFileSync(ready, "utf8"));
			if (reason === "abort") controller.abort();
			const error = await work;
			assert.match(error?.message ?? "", reason === "abort" ? /已取消/ : /超时/);
			assert.equal(processRunning(pid), false, "command promise must not settle before cleanup");
		} finally {
			if (pid) { try { process.kill(pid, "SIGKILL"); } catch {} }
			controller.abort();
			await work;
		}
	}
});

test("auxiliary normal exit reaps pipe-holding descendants before resolving", { timeout: 5_000, skip: process.platform === "win32" }, async () => {
	const script = `
		const {spawn} = require("node:child_process");
		const child = spawn(process.execPath, ["-e", 'process.on("SIGTERM",()=>{});setInterval(()=>{},1000);process.send("ready");process.disconnect();'], {stdio:["ignore","inherit","ignore","ipc"]});
		child.once("message",()=>process.stdout.write(String(child.pid),()=>process.exit(0)));
	`;
	let settled = false;
	const work = runCommand(process.execPath, ["-e", script], process.cwd(), { timeoutMs: 4_000 }).finally(() => { settled = true; });
	const children = (globalThis as any)[Symbol.for("pi.worker.commandChildren.v1")] as Set<import("node:child_process").ChildProcess>;
	const child = [...children][0];
	assert.ok(child?.pid);
	try {
		await until(() => child.exitCode !== null);
		assert.equal(settled, false);
		assert.ok(children.has(child), "exit fallback retains the original group during cleanup");
		const result = await work;
		assert.equal(result.code, 0);
		assert.equal(processRunning(Number(result.stdout.toString())), false);
		assert.equal(children.size, 0);
	} finally {
		try { process.kill(-child.pid!, "SIGKILL"); } catch {}
		await work;
	}
});

test("parallel Git checks wait for every command cleanup and reject failed evidence", { timeout: 8_000, skip: process.platform === "win32" }, async (t) => {
	const dir = fs.mkdtempSync(path.join(os.tmpdir(), "worker-git-cleanup-"));
	t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
	const git = path.join(dir, "git");
	fs.writeFileSync(git, `#!${process.execPath}
		const fs = require("node:fs");
		const id = process.argv.includes("--cached") ? "cached" : process.argv[2];
		if (id !== "diff") process.on("SIGTERM",()=>{});
		fs.writeFileSync(id+".pid", String(process.pid));
		setInterval(()=>{},1000);
	`, { mode: 0o700 });
	const controller = new AbortController();
	const oldPath = process.env.PATH;
	process.env.PATH = `${dir}${path.delimiter}${oldPath ?? ""}`;
	const work = gitChangedPaths(dir, controller.signal).then(() => undefined, (error: Error) => error);
	process.env.PATH = oldPath;
	const pids: number[] = [];
	try {
		await until(() => ["diff", "cached", "ls-files"].every((id) => fs.existsSync(path.join(dir, id+".pid"))));
		for (const id of ["diff", "cached", "ls-files"]) pids.push(Number(fs.readFileSync(path.join(dir, id+".pid"), "utf8")));
		controller.abort();
		assert.match((await work)?.message ?? "", /已取消/);
		assert.ok(pids.every((pid) => !processRunning(pid)), "all siblings are reaped before the snapshot rejects");
		fs.writeFileSync(git, `#!${process.execPath}\nprocess.stderr.write("fixture git failure"); process.exit(42);\n`, { mode: 0o700 });
		process.env.PATH = `${dir}${path.delimiter}${oldPath ?? ""}`;
		const failed = gitChangedPaths(dir);
		process.env.PATH = oldPath;
		await assert.rejects(failed, /失败（42）.*fixture git failure/);
	} finally {
		if (oldPath === undefined) delete process.env.PATH; else process.env.PATH = oldPath;
		for (const pid of pids) { try { process.kill(-pid, "SIGKILL"); } catch {} }
		controller.abort();
		await work;
	}
});

test("preflight failures preserve category, routing metadata and result shape without launching a worker", async (t) => {
	const cwd = fs.mkdtempSync(path.join(os.tmpdir(), "worker-preflight-"));
	t.after(() => fs.rmSync(cwd, { recursive: true, force: true }));
	const task: WorkerTask = { mode: "scout", sessionMode: "fork", objective: "inspect", preset: "fast" };
	const preset = { model: "fixture/model", thinking: "high" as const };
	const config: RoutingConfig = { ...DEFAULT_OPTIONS, fast: preset, normal: preset, deep: preset };
	for (const available of [false, true]) {
		const ctx = { cwd, sessionManager: { getBranch: () => [] }, modelRegistry: { getAvailable: () => available ? [{ provider: "fixture", id: "model", reasoning: true }] : [] } } as any;
		const result = await executeTask(task, config, ["warning"], ctx, undefined);
		assert.equal(result.status, "blocked");
		assert.equal(result.failure.category, available ? "workspace_snapshot" : "route_or_contract");
		assert.equal(result.failure.retryable, false);
		assert.deepEqual(result.summary, [result.failure.reason]);
		assert.deepEqual(result.recommended_next_action, [result.failure.next_action]);
		assert.equal(result.execution.attempt, 0);
		assert.equal(result.execution.requested_preset, "fast");
		assert.equal(result.execution.resolved_preset, available ? "fast" : null);
		assert.equal(result.execution.resolved_model_id, available ? "fixture/model" : null);
		assert.deepEqual(result.execution.warnings, ["warning"]);
		for (const key of ["changed_files", "observed_changed_files", "validation", "acceptance", "findings", "risks", "out_of_scope"]) {
			assert.deepEqual(result[key], [], key);
		}
	}
});
