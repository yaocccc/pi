// RPC-only model-free parent; spawned with Node IPC by isolation.test.ts.
// runPiWorker re-invokes this script as the CLI, which dispatches to the fd3 child.
import { dirname, join } from "node:path";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath, pathToFileURL } from "node:url";

if (process.argv.includes("--mode")) {
	await import("./child.mjs");
} else {
	process.env.PI_WORKER_DEPTH = "0";
	process.env.NODE_OPTIONS = `${process.env.NODE_OPTIONS ?? ""} --experimental-transform-types`;
	process.env.PI_CODING_AGENT_DIR = fileURLToPath(new URL("../../../", import.meta.url));
	// Prefer the installed CLI, not the older extension-local SDK dependency.
	const installed = join(dirname(process.execPath), "../lib/node_modules/@earendil-works/pi-coding-agent");
	const packageRoot = process.env.PI_WORKER_TEST_PI_ROOT ?? (existsSync(join(installed, "dist/core/extensions/loader.js")) ? installed : dirname(dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")))));
	const piVersion = JSON.parse(readFileSync(join(packageRoot, "package.json"), "utf8")).version;
	const loader = await import(pathToFileURL(join(packageRoot, "dist/core/extensions/loader.js")).href);
	const sessions = new Map();
	const signals = new Map();
	const registry = () => globalThis[Symbol.for("pi.worker.liveOwners.v1")]?.owners ?? new Set();
	const stats = () => ({
		updates: globalThis.__workerFixtureUpdates ?? [], usageThrows: globalThis.__workerFixtureUsageThrows ?? 0, progressThrows: globalThis.__workerFixtureProgressThrows ?? 0,
		retained: [...(globalThis.__workerFixtureOwners ?? [])].map((owner) => ({ slots: owner.slots, children: owner.children.size })),
		runtimes: [...(globalThis.__workerFixtureRuntimes ?? [])].map((runtime) => ({ active: runtime.active.size, queued: runtime.queue.length })),
		parentPid: process.pid, piVersion, modules: globalThis.__workerFixtureModules?.size ?? 0, owners: [...registry()].map((owner) => ({ id: owner.id, slots: owner.slots, ready: globalThis.__workerFixtureReady?.has(owner.id) ?? false, pids: [...owner.children].map((child) => child.pid) })) });
	async function request(message) {
		const { op, session: name = "a" } = message;
		if (op === "load") {
			if (message.reload) loader.clearExtensionCache();
			const loaded = await loader.loadExtensionsCached([fileURLToPath(new URL("./extension.ts", import.meta.url))], process.cwd());
			if (loaded.errors.length) throw new Error(JSON.stringify(loaded.errors));
			loaded.runtime.appendEntry = () => {};
			loaded.runtime.sendMessage = () => { throw new Error("unexpected model continuation"); };
			const extension = loaded.extensions[0];
			const ctx = { cwd: process.cwd(), hasUI: false, sessionManager: { getBranch: () => [] }, modelRegistry: { getAvailable: () => [{ provider: "fixture", id: "local", reasoning: true }] } };
			sessions.set(name, { extension, ctx });
			for (const handler of extension.handlers.get("session_start") ?? []) await handler({}, ctx);
			return stats();
		}
		if (op === "stats") return stats();
		if (op === "presentation") {
			globalThis.__workerFixtureThrowUsage = message.usage;
			globalThis.__workerFixtureThrowProgress = message.progress;
			return {};
		}
		if (op === "exit") { process.exit(0); }
		if (op === "exit-hooks") { process.emit("exit", 0); return stats(); }
		if (op === "socket-error") {
			for (const owner of registry()) for (const child of owner.children) if (!message.pid || message.pid === child.pid) child.stdio[3].destroy(Object.assign(new Error("fixture error; no payload"), { code: "ECONNRESET" }));
			return stats();
		}
		if (op === "abort") { signals.get(message.callId)?.abort(); return {}; }
		const { extension, ctx } = sessions.get(name);
		if (op === "shutdown" || op === "restart") {
			for (const handler of extension.handlers.get(op === "shutdown" ? "session_shutdown" : "session_start") ?? []) await handler({}, ctx);
			return stats();
		}
		if (op === "call") {
			const signal = new AbortController();
			signals.set(message.id, signal);
			const update = (result) => {
				const phase = result.details?.tasks?.[0]?.phase;
				if (message.throwUpdate === "all" || message.throwUpdate === phase) {
					(globalThis.__workerFixtureUpdates ??= []).push(phase);
					throw new Error(`fixture onUpdate failure: ${phase}`);
				}
			};
			const result = await extension.tools.get("worker").definition.execute(message.id, message.input, signal.signal, update, ctx);
			return JSON.parse(result.content[0].text);
		}
		throw new Error(`unknown op: ${op}`);
	}
	const send = (message) => { if (process.connected) process.send(message, () => {}); };
	process.on("message", (message) => { void request(message).then((value) => send({ id: message.id, value }), (error) => send({ id: message.id, error: error.stack })); });
	process.on("disconnect", () => process.exit(0));
	send({ ready: true });
}
