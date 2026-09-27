import assert from "node:assert/strict";
import test from "node:test";
import { fork, execFileSync } from "node:child_process";
import { once } from "node:events";
import { mkdtempSync, rmSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { until } from "./fixtures/helpers.ts";

const fixture = fileURLToPath(new URL("./fixtures/parent.mjs", import.meta.url));
const task = (objective = "ask") => ({ task: { mode: "scout", objective } });
const reply = (batch: any) => ({ batchId: batch.batchId, answers: batch.questions.map((q: any) => ({ taskId: q.taskId, questionId: q.id, answer: "authorized fixture answer" })) });
function workspace() {
	const cwd = mkdtempSync(join(tmpdir(), "worker-isolation-"));
	execFileSync("git", ["init", "--quiet", cwd]);
	return { cwd, dispose: () => rmSync(cwd, { recursive: true, force: true }) };
}
async function parent(cwd: string) {
	const child = fork(fixture, [], { cwd, execArgv: [], stdio: ["ignore", "pipe", "pipe", "ipc"] });
	let errors = "";
	child.stderr!.on("data", (chunk) => { errors += chunk; });
	const pending = new Map<string, { resolve: (value: any) => void; reject: (error: Error) => void }>();
	let count = 0;
	let ready!: () => void;
	const started = new Promise<void>((resolve) => { ready = resolve; });
	child.on("message", (message: any) => {
		if (message.ready) { ready(); return; }
		const request = pending.get(message.id);
		pending.delete(message.id);
		if (message.error) request?.reject(new Error(message.error)); else request?.resolve(message.value);
	});
	child.stdout!.resume();
	const closed = once(child, "exit");
	child.on("exit", () => { for (const request of pending.values()) request.reject(new Error(`fixture exited: ${errors}`)); pending.clear(); });
	const call = (op: string, data: Record<string, any> = {}): Promise<any> => new Promise((resolve, reject) => {
		const id = String(++count);
		pending.set(id, { resolve, reject });
		child.send({ id, op, ...data });
	});
	await Promise.race([started, closed.then(() => { throw new Error(`fixture startup failed: ${errors}`); })]);
	return { call, child, closed, dispose: async () => { if (child.connected) child.disconnect(); await closed; } };
}

const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
async function waitReady(p: Awaited<ReturnType<typeof parent>>) {
	const deadline = Date.now() + 5_000;
	while (Date.now() < deadline) {
		if ((await p.call("stats")).owners.some((owner: any) => owner.ready)) return;
		await delay(10);
	}
	throw new Error("fixture child did not become ready");
}
function processRunning(pid: number) {
	try {
		process.kill(pid, 0);
		if (process.platform === "linux") return !/^[ZX] /.test(readFileSync(`/proc/${pid}/stat`, "utf8").split(") ")[1]);
		return true;
	} catch { return false; }
}
function assertReleased(stats: any) {
	assert.equal(stats.owners.length, 0);
	assert.ok(stats.retained.every((owner: any) => owner.slots === 0 && owner.children === 0), "all process slots released");
	assert.ok(stats.runtimes.every((runtime: any) => runtime.active === 0 && runtime.queued === 0), "runtime active/path locks released");
}

// All tests use real installed-loader factories, executeTask, Git checks, process groups and fd3.
test("two real extension instances have independent slots; idle shutdown and late abort cannot cancel the other", { timeout: 30_000 }, async (t) => {
	const repo = workspace(); const p = await parent(repo.cwd);
	try {
		await p.call("load", { session: "a" }); await p.call("load", { session: "b" });
		const first = await p.call("call", { session: "b", input: task() });
		assert.equal(first.status, "waiting_for_reply");
		const before = await p.call("stats");
		t.diagnostic(`Pi loader version: ${before.piVersion}`);
		assert.equal(before.modules, 1, "same loader generation shares the process module");
		await p.call("shutdown", { session: "a" });
		assert.deepEqual((await p.call("stats")).owners, before.owners);
		await p.call("abort", { callId: "3" }); // the already-returned initial B invocation
		const done = await p.call("call", { session: "b", input: reply(first) });
		assert.equal(done.result.status, "completed");
		assert.equal(done.history[0].status, "answered");
		await p.call("restart", { session: "a" });
		const [a, b] = await Promise.all([p.call("call", { session: "a", input: task() }), p.call("call", { session: "b", input: task() })]);
		const stats = await p.call("stats");
		assert.equal(stats.owners.length, 2);
		assert.deepEqual(stats.owners.map((o: any) => o.slots), [1, 1], "each instance admits its own one-slot worker");
		const shuttingDown = p.call("shutdown", { session: "a" });
		const bDone = await p.call("call", { session: "b", input: reply(b) });
		await shuttingDown;
		assert.equal(bDone.result.status, "completed");
		const cancelled = await p.call("call", { session: "b", input: task("done") });
		assert.equal(cancelled.result.status, "completed");
		assert.equal(a.finished, false);
		assert.equal((await p.call("stats")).owners.length, 0, "empty owners are not retained by the global registry");
	} finally { await p.dispose(); repo.dispose(); }
});

test("independent OS parents in the same cwd own independent PGIDs and fd3 channels", { timeout: 30_000 }, async () => {
	const repo = workspace(); const a = await parent(repo.cwd); const b = await parent(repo.cwd);
	try {
		await a.call("load"); await b.call("load");
		const [qa, qb] = await Promise.all([a.call("call", { input: task() }), b.call("call", { input: task() })]);
		const sa = await a.call("stats"), sb = await b.call("stats");
		assert.notEqual(sa.parentPid, sb.parentPid);
		const pa = sa.owners[0].pids[0], pb = sb.owners[0].pids[0];
		assert.notEqual(pa, pb);
		if (process.platform === "linux") {
			for (const pid of [pa, pb]) {
				const stat = readFileSync(`/proc/${pid}/stat`, "utf8").split(") ")[1].split(" ");
				assert.equal(Number(stat[2]), pid, "detached worker is its own process group");
			}
		}
		await a.call("shutdown");
		assert.equal((await a.call("stats")).owners.length, 0);
		assert.deepEqual((await b.call("stats")).owners, sb.owners);
		const done = await b.call("call", { input: reply(qb) });
		assert.equal(done.result.status, "completed");
		assert.equal(qa.finished, false);
	} finally { await Promise.all([a.dispose(), b.dispose()]); repo.dispose(); }
});

test("reload cache generations coexist; process exit hook kills both, is idempotent, and releases empty owners", { timeout: 30_000 }, async () => {
	const repo = workspace(); const p = await parent(repo.cwd);
	try {
		await p.call("load", { session: "old" });
		const old = await p.call("call", { session: "old", input: task() });
		await p.call("load", { session: "new", reload: true });
		const current = await p.call("call", { session: "new", input: task() });
		const stats = await p.call("stats");
		assert.equal(stats.modules, 2, "clearExtensionCache produced two real process module generations");
		assert.equal(stats.owners.length, 2);
		await p.call("exit-hooks"); await p.call("exit-hooks");
		for (const [session, batch] of [["old", old], ["new", current]] as const) {
			// Ordinary continuation waits for cleanup once the transport closes.
			let result = await p.call("call", { session, input: { batchId: batch.batchId } });
			while (!result.finished) result = await p.call("call", { session, input: { batchId: batch.batchId } });
			assert.equal(result.finished, true);
			assert.equal(result.result.status, "failed");
		}
		assert.equal((await p.call("stats")).owners.length, 0);
	} finally { await p.dispose(); repo.dispose(); }
});

test("real OS parent exit invokes the reload-safe fallback for every live generation", { timeout: 30_000 }, async () => {
	const repo = workspace(); const p = await parent(repo.cwd);
	const pids: number[] = [];
	const running = (pid: number) => {
		try {
			process.kill(pid, 0);
			if (process.platform === "linux") return !/^[ZX] /.test(readFileSync(`/proc/${pid}/stat`, "utf8").split(") ")[1]);
			return true;
		} catch { return false; }
	};
	try {
		for (const [session, reload] of [["old", false], ["new", true]] as const) {
			await p.call("load", { session, reload });
			await p.call("call", { session, input: task() });
		}
		const stats = await p.call("stats");
		assert.equal(stats.modules, 2);
		pids.push(...stats.owners.flatMap((owner: any) => owner.pids));
		assert.equal(pids.length, 2);
		p.child.send({ id: "exit", op: "exit" });
		await p.closed;
		await until(() => pids.every((pid) => !running(pid)), 5_000);
	} finally {
		// Only the exact fixture-owned PIDs, never real Pi sessions, even if this regression fails.
		for (const pid of pids) if (running(pid)) { try { process.kill(pid, "SIGKILL"); } catch {} }
		await p.dispose(); repo.dispose();
	}
});

test("old stubborn cleanup overlaps a new loader session without killing or blocking it", { timeout: 30_000 }, async () => {
	const repo = workspace(); const p = await parent(repo.cwd);
	try {
		await p.call("load", { session: "old" });
		const oldWork = p.call("call", { session: "old", input: task("stubborn") });
		void oldWork.catch(() => {}); // Also observe teardown rejection if an assertion fails first.
		let ready = false;
		const deadline = Date.now() + 5_000;
		while (!ready && Date.now() < deadline) {
			ready = !!(await p.call("stats")).owners[0]?.ready;
			if (!ready) await new Promise((resolve) => setTimeout(resolve, 10));
		}
		assert.equal(ready, true, "child installed its SIGTERM handler before shutdown");
		const closing = p.call("shutdown", { session: "old" });
		await p.call("load", { session: "new", reload: true });
		const fresh = await p.call("call", { session: "new", input: task() });
		assert.equal(fresh.status, "waiting_for_reply");
		await closing;
		const cancelled = await oldWork;
		assert.equal(cancelled.result.execution.termination_source, "session_shutdown");
		assert.equal(cancelled.result.execution.termination.source, "session_shutdown");
		assert.equal((await p.call("call", { session: "new", input: reply(fresh) })).result.status, "completed");
		assert.equal((await p.call("stats")).owners.length, 0);
	} finally { await p.dispose(); repo.dispose(); }
});

test("actual parent socket error cannot be caught into a successful worker result; cleanup allows the next batch", { timeout: 30_000 }, async () => {
	const repo = workspace(); const p = await parent(repo.cwd);
	try {
		await p.call("load");
		const pending = await p.call("call", { input: task("catch") });
		await p.call("socket-error");
		const result = await p.call("call", { input: { batchId: pending.batchId } });
		assert.equal(result.finished, true);
		assert.equal(result.result.status, "failed");
		assert.equal(result.result.failure.category, "ipc_failure");
		assert.equal(result.result.execution.termination.code, "ECONNRESET");
		assert.equal(result.result.execution.termination.source, "transport");
		assert.equal(result.result.execution.cancelled, false);
		assert.equal(result.history[0].status, "cancelled");
		assert.equal((await p.call("stats")).owners.length, 0);
		assert.equal((await p.call("call", { input: task("done") })).result.status, "completed");
	} finally { await p.dispose(); repo.dispose(); }
});


test("cancel retains original PGID, slot and runtime lock after leader closes until resistant grandchild is killed", { timeout: 20_000, skip: process.platform !== "linux" }, async (t) => {
	const repo = workspace(); const p = await parent(repo.cwd);
	let group: { leader: number; grandchild: number } | undefined;
	try {
		await p.call("load");
		const work = p.call("call", { input: { task: { mode: "fix", objective: "group-stubborn", allowedPaths: ["fixture-group.json"] } } });
		void work.catch(() => {});
		await waitReady(p);
		group = JSON.parse(readFileSync(join(repo.cwd, "fixture-group.json"), "utf8"));
		const stat = readFileSync(`/proc/${group!.grandchild}/stat`, "utf8").split(") ")[1].split(" ");
		assert.equal(Number(stat[2]), group!.leader, "grandchild shares fixture leader PGID");
		let settled = false;
		const started = Date.now();
		const shutdown = p.call("shutdown").then(() => { settled = true; });
		void shutdown.catch(() => {});
		await until(() => !processRunning(group!.leader), 1_500);
		await delay(200);
		assert.equal(processRunning(group!.grandchild), true, "grandchild resists SIGTERM after direct close");
		assert.equal(settled, false);
		const during = await p.call("stats");
		assert.deepEqual(during.owners[0].pids, [group!.leader], "original PGID remains exit-registered");
		assert.equal(during.owners[0].slots, 1);
		assert.equal(during.runtimes[0].active, 1, "write task retains its runtime path lock");
		await shutdown;
		const result = await work;
		const elapsed = Date.now() - started;
		assert.ok(elapsed >= 2_900 && elapsed < 5_500, `original three-second escalation remains bounded: ${elapsed}ms`);
		assert.equal(result.result.execution.cancelled, true);
		assert.equal(processRunning(group!.grandchild), false);
		assertReleased(await p.call("stats"));
		t.diagnostic(`leader closed early; resistant same-PGID grandchild stopped; cleanup ${elapsed}ms`);
	} finally {
		// Exact fixture PGID only, even when the lifecycle assertion fails.
		if (group) { try { process.kill(-group.leader, "SIGKILL"); } catch {} }
		await p.dispose(); repo.dispose();
	}
});

for (const scenario of [
	{ name: "final onUpdate", update: "子进程已完成" },
	{ name: "intermediate onUpdate and usage", update: "all", usage: true },
	{ name: "final onProgress", progress: "子进程已完成" },
	{ name: "terminating onUpdate", update: "正在取消", cancel: true },
	{ name: "terminating onProgress", progress: "正在取消", cancel: true },
]) test(`real loader isolates throwing ${scenario.name} and releases lifecycle resources`, { timeout: 15_000 }, async (t) => {
	const repo = workspace(); const p = await parent(repo.cwd);
	try {
		await p.call("load");
		await p.call("presentation", { usage: scenario.usage, progress: scenario.progress });
		const work = p.call("call", { input: task(scenario.cancel ? "hang" : "done"), throwUpdate: scenario.update });
		void work.catch(() => {});
		let started = Date.now();
		if (scenario.cancel) {
			await waitReady(p);
			started = Date.now();
			await p.call("shutdown");
		}
		const result = await work;
		const elapsed = Date.now() - started;
		assert.equal(result.finished, true);
		assert.equal(result.result.status, scenario.cancel ? "failed" : "completed");
		if (scenario.cancel) assert.equal(result.result.execution.cancelled, true);
		assert.ok(elapsed < 2_500, `no unnecessary three-second grace: ${elapsed}ms`);
		const stats = await p.call("stats");
		if (scenario.update) assert.ok(stats.updates.includes(scenario.update === "all" ? "子进程已完成" : scenario.update), "target onUpdate actually threw");
		if (scenario.progress) assert.ok(stats.progressThrows > 0, "target onProgress actually threw");
		if (scenario.usage) assert.ok(stats.usageThrows > 0, "usage callback actually threw");
		assertReleased(stats);
		await p.call("shutdown");
		await p.call("restart");
		await p.call("presentation");
		assert.equal((await p.call("call", { input: task("done") })).result.status, "completed");
		assertReleased(await p.call("stats"));
		t.diagnostic(`${scenario.name}: settled and shutdown completed (${elapsed}ms), subsequent work completed`);
	} finally { await p.dispose(); repo.dispose(); }
});
