import assert from "node:assert/strict";
import { EventEmitter } from "node:events";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { getCapability } from "../shared/capabilities.ts";
import { FRONTEND_EVENT } from "../shared/frontend.ts";
import { WORKER_CAPABILITY, type WorkerCapability } from "./api.ts";
import type { RoutingConfig } from "./types.ts";
import workerExtension from "./index.ts";
import { DEFAULT_OPTIONS } from "./config.ts";

function fixture(t: test.TestContext) {
	const dir = mkdtempSync(join(tmpdir(), "worker-api-"));
	t.after(() => rmSync(dir, { recursive: true, force: true }));
	const events = new EventEmitter();
	const handlers = new Map<string, any>();
	const tools = new Map<string, any>();
	const preset = { model: "fixture/local", thinking: "off" as const };
	let config: RoutingConfig = { ...DEFAULT_OPTIONS, fast: preset, normal: preset, deep: preset };
	const pi = {
		events: { emit: (name: string, value: unknown) => events.emit(name, value), on: (name: string, handler: any) => { events.on(name, handler); return () => { events.off(name, handler); }; } },
		on: (name: string, handler: any) => handlers.set(name, handler),
		registerTool: (tool: any) => tools.set(tool.name, tool),
		registerCommand: () => assert.fail("core must not register a settings menu"),
	};
	const ctx: any = { cwd: dir, hasUI: false, mode: "sdk", get ui() { throw new Error("no UI exists"); }, sessionManager: { getBranch: () => [] }, modelRegistry: { getAvailable: () => [{ provider: "fixture", id: "local", reasoning: false }] } };
	const previous = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	try {
		workerExtension(pi as any, { executeTask: async () => ({ status: "completed", summary: ["done"] }), loadRoutingConfig: () => ({ config, path: join(dir, "settings.json"), warnings: [] }) });
	} finally { if (previous === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = previous; }
	t.after(() => handlers.get("session_shutdown")());
	return { pi, ctx, tools, handlers, dir, setConfig: (value: typeof config) => { config = value; }, capability: getCapability<WorkerCapability>(pi as any, WORKER_CAPABILITY)! };
}

test("headless transcript reads are capability-owned and shutdown resets continuations", async (t) => {
	const h = fixture(t);
	const previous = process.env.PI_CODING_AGENT_DIR;
	process.env.PI_CODING_AGENT_DIR = join(h.dir, "custom-agent");
	try {
		const id = "12345678-1234-4234-8234-123456789abc";
		mkdirSync(join(h.dir, "worker-transcripts"));
		writeFileSync(join(h.dir, "worker-transcripts", id + ".jsonl"), JSON.stringify({ type: "worker_transcript_header", version: 1, id, entries: [] }) + "\n");
		const descriptor = { version: 1, id };
		const first = h.capability.readTranscript({ descriptor });
		assert.equal(first.records.length, 1);
		assert.equal(h.capability.readTranscript({ descriptor, cursor: first.cursor }).reset, false);
		await h.handlers.get("session_shutdown")();
		assert.equal(h.capability.readTranscript({ descriptor, cursor: first.cursor }).reset, true);
		assert.throws(() => h.capability.readTranscript({ descriptor: { version: 1, id: "../private" } }), /Invalid/);
	} finally {
		if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
		else process.env.PI_CODING_AGENT_DIR = previous;
	}
});

test("headless capability exposes validated settings operations without UI or renderer registration", (t) => {
	const h = fixture(t);
	assert.ok(h.capability);
	const tool = h.tools.get("worker");
	assert.equal(tool.renderResult, undefined);
	assert.equal(tool.renderCall, undefined);
	const settings = h.capability.getSettings();
	settings.fast.model = "mutated/value";
	assert.equal(h.capability.getSettings().fast.model, "fixture/local");
	assert.deepEqual(h.capability.getModels(h.ctx), [{ id: "fixture/local", thinking: ["off"] }]);
	const value = { ...h.capability.getSettings(), maxConcurrentWorkers: 4 };
	assert.deepEqual(h.capability.saveSettings(h.ctx, value), value);
	assert.deepEqual(JSON.parse(readFileSync(join(h.dir, "settings.json"), "utf8")), value);
	assert.throws(() => h.capability.saveSettings(h.ctx, { ...value, maxConcurrentWorkers: "4" }), /invalid-number/);
});

test("headless saves publish one fact only after a changed configuration is persisted", (t) => {
	const h = fixture(t);
	const observed: unknown[] = [];
	h.pi.events.on(FRONTEND_EVENT, (message: any) => {
		assert.deepEqual(JSON.parse(readFileSync(join(h.dir, "settings.json"), "utf8")), changed);
		assert.equal(message.context, h.ctx);
		observed.push(message.event);
	});
	const initial = h.capability.getSettings();
	h.capability.saveSettings(h.ctx, initial);
	assert.deepEqual(observed, []);
	const changed = { ...initial, maxConcurrentWorkers: 4 };
	h.capability.saveSettings(h.ctx, changed);
	assert.deepEqual(observed, [{ name: "worker.settings-saved", data: {} }]);
	h.setConfig(changed);
	h.capability.saveSettings(h.ctx, structuredClone(changed));
	assert.equal(observed.length, 1);
	assert.throws(() => h.capability.saveSettings(h.ctx, { ...changed, maxConcurrentWorkers: "5" }), /invalid-number/);
	assert.throws(() => h.capability.saveSettings(h.ctx, { ...changed, fast: { model: "unknown/model", thinking: "off" } }), /model-unavailable/);
	assert.throws(() => h.capability.saveSettings(h.ctx, { ...changed, fast: { model: "fixture/local", thinking: "high" } }), /thinking-unsupported/);
	assert.equal(observed.length, 1);
	assert.deepEqual(JSON.parse(readFileSync(join(h.dir, "settings.json"), "utf8")), changed);
});

test("failed persistence never publishes a saved fact", (t) => {
	const h = fixture(t);
	mkdirSync(join(h.dir, "settings.json"));
	const observed: unknown[] = [];
	h.pi.events.on(FRONTEND_EVENT, (message: unknown) => observed.push(message));
	assert.throws(() => h.capability.saveSettings(h.ctx, { ...h.capability.getSettings(), maxConcurrentWorkers: 4 }), /save-failed/);
	assert.deepEqual(observed, []);
});

test("saved-fact observer failure cannot fail a successful headless save", (t) => {
	const h = fixture(t);
	h.pi.events.on(FRONTEND_EVENT, () => { throw new Error("broken observer"); });
	const value = { ...h.capability.getSettings(), maxConcurrentWorkers: 4 };
	assert.deepEqual(h.capability.saveSettings(h.ctx, value), value);
	assert.deepEqual(JSON.parse(readFileSync(join(h.dir, "settings.json"), "utf8")), value);
});

test("session views publish isolated snapshots, survive observer errors and never adopt replacement sessions", async (t) => {
	const h = fixture(t);
	const first = h.capability.getSession();
	assert.equal(first.awaitingHistory, true);
	const changes: string[] = [];
	first.subscribe(() => { throw new Error("broken optional UI"); });
	first.subscribe((event) => changes.push(event.type));
	await h.handlers.get("session_start")({}, h.ctx);
	assert.equal(first.awaitingHistory, false);
	assert.deepEqual(changes, ["history"]);
	const result = await h.tools.get("worker").execute("first", { tasks: [{ mode: "scout", preset: "normal", sessionMode: "new", objective: "inspect" }] }, undefined, undefined, h.ctx);
	const id = result.details.batchId;
	assert.equal(first.lookup(id)?.tasks[0].status, "completed");
	const snapshot = first.lookup(id)!;
	snapshot.tasks[0].objective = "mutated";
	assert.equal(first.lookup(id)?.tasks[0].objective, "inspect");
	await h.handlers.get("session_tree")({}, h.ctx);
	assert.equal(first.closed, true);
	assert.equal(changes.at(-1), "closed");
	assert.notEqual(first, h.capability.getSession());
	assert.equal(h.capability.getSession().lookup(id), undefined);
	const count = changes.length;
	await h.handlers.get("session_shutdown")();
	assert.equal(changes.length, count);
});
