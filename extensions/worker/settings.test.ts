import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import test from "node:test";
import { DEFAULT_OPTIONS } from "./config.ts";
import { configureWorkerSettings, parseSettings, saveWorkerSettings } from "./settings.ts";

const settings = () => ({ ...DEFAULT_OPTIONS, fast: { model: "test/model", thinking: "off" as const }, normal: { model: "test/model", thinking: "off" as const }, deep: { model: "test/model", thinking: "off" as const } });
const registry = { getAvailable: () => [{ provider: "test", id: "model", reasoning: false }] };
const setup = (t: any) => {
	const dir = mkdtempSync(join(tmpdir(), "worker-settings-ui-"));
	const previous = process.env.PI_GOUI_PROTOCOL;
	process.env.PI_GOUI_PROTOCOL = "1";
	t.after(() => { if (previous === undefined) delete process.env.PI_GOUI_PROTOCOL; else process.env.PI_GOUI_PROTOCOL = previous; rmSync(dir, { recursive: true, force: true }); });
	return { dir, path: join(dir, "settings.json") };
};

test("Go settings sends data only; cancellation never writes", async (t) => {
	const fixture = setup(t);
	const current = settings();
	let requests = 0;
	const ctx = { mode: "rpc", hasUI: true, modelRegistry: registry, ui: {
		input: async (title: string, payload: string) => {
			requests++;
			assert.equal(title, "pi-goui:worker-settings:v1");
			assert.deepEqual(JSON.parse(payload), { config: current, original: current, models: [{ id: "test/model", thinking: ["off"] }] });
			return undefined;
		}, select: () => assert.fail("Go must own menus"), notify: () => assert.fail("cancel must not notify"),
	} };
	assert.equal(await configureWorkerSettings(ctx as any, current, fixture.path), undefined);
	assert.equal(requests, 1);
	assert.equal(existsSync(fixture.path), false);
});

test("Go saves only a validated explicit response and emits structured completion", async (t) => {
	const fixture = setup(t);
	const current = settings();
	const draft = { ...current, maxConcurrentWorkers: 4 };
	const events: string[] = [];
	const ctx = { mode: "rpc", hasUI: true, modelRegistry: registry, ui: {
		input: async () => { assert.equal(existsSync(fixture.path), false); return JSON.stringify(draft); },
		notify: (value: string) => events.push(value),
	} };
	assert.deepEqual(await configureWorkerSettings(ctx as any, current, fixture.path), draft);
	assert.deepEqual(JSON.parse(readFileSync(fixture.path, "utf8")), draft);
	assert.deepEqual(events, ['pi-goui:event:v1:{"name":"worker.settings-saved","data":{}}']);
	assert.equal(current.maxConcurrentWorkers, DEFAULT_OPTIONS.maxConcurrentWorkers);
});

test("Go retries unsupported model/thinking with draft and original intact", async (t) => {
	const fixture = setup(t);
	const current = settings();
	const draft = { ...current, fast: { model: "test/model", thinking: "high" } };
	let requests = 0;
	const ctx = { mode: "rpc", hasUI: true, modelRegistry: registry, ui: {
		input: async (_: string, payload: string) => {
			if (++requests === 1) return JSON.stringify(draft);
			const request = JSON.parse(payload);
			assert.deepEqual(request.config, draft);
			assert.deepEqual(request.original, current);
			assert.deepEqual(request.error, { code: "thinking-unsupported", field: "fast", model: "test/model", thinking: "high" });
			return undefined;
		}, notify: () => assert.fail("invalid settings must not emit saved"),
	} };
	assert.equal(await configureWorkerSettings(ctx as any, current, fixture.path), undefined);
	assert.equal(existsSync(fixture.path), false);
});

test("strict saves reject malformed shape, coercions, ranges and unavailable models without changing file", (t) => {
	const fixture = setup(t);
	writeFileSync(fixture.path, "original");
	const current = settings();
	for (const raw of [null, [], {}, { ...current, extra: true }, { ...current, version: 1 }, { ...current, maxConcurrentWorkers: "3" }, { ...current, maxConcurrentWorkers: 1.5 }, { ...current, defaultTimeoutMs: 0 }, { ...current, maxOutputBytes: 1048577 }, { ...current, fast: { model: "test/model", thinking: "off", extra: true } }, { ...current, fast: { model: "test/model", thinking: "unknown" } }]) {
		assert.throws(() => parseSettings(raw));
		assert.throws(() => saveWorkerSettings({ modelRegistry: registry } as any, raw, fixture.path));
	}
	assert.throws(() => saveWorkerSettings({ modelRegistry: registry } as any, { ...current, fast: { model: "missing/model", thinking: "off" } }, fixture.path), /model-unavailable/);
	assert.equal(readFileSync(fixture.path, "utf8"), "original");
});

test("save errors return structured error and keep draft available until cancellation", async (t) => {
	const fixture = setup(t);
	const current = settings();
	const draft = { ...current, maxConcurrentWorkers: 5 };
	let requests = 0;
	const ctx = { mode: "rpc", hasUI: true, modelRegistry: registry, ui: {
		input: async (_: string, payload: string) => {
			if (++requests === 1) return JSON.stringify(draft);
			const next = JSON.parse(payload);
			assert.equal(next.error.code, "save-failed");
			assert.deepEqual(next.config, draft);
			return undefined;
		}, notify: () => assert.fail("failed save must not emit saved"),
	} };
	await configureWorkerSettings(ctx as any, current, fixture.dir);
});

test("native mode retains native menu despite Go environment", async (t) => {
	const fixture = setup(t);
	let menus = 0;
	const ctx = { mode: "tui", hasUI: true, modelRegistry: registry, ui: {
		select: async (title: string) => { assert.match(title, /Worker 设置/); menus++; return undefined; },
		input: () => assert.fail("native settings must not use Go request"),
	} };
	assert.equal(await configureWorkerSettings(ctx as any, settings(), fixture.path), undefined);
	assert.equal(menus, 1);
});

test("malformed JSON response is retried without overwriting configuration", async (t) => {
	const fixture = setup(t);
	let requests = 0;
	const ctx = { mode: "rpc", hasUI: true, modelRegistry: registry, ui: {
		input: async (_: string, payload: string) => {
			if (++requests === 1) return "{broken";
			assert.equal(JSON.parse(payload).error.code, "invalid-config");
			return undefined;
		},
	} };
	await configureWorkerSettings(ctx as any, settings(), fixture.path);
	assert.equal(existsSync(fixture.path), false);
});
