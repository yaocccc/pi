import assert from "node:assert/strict";
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import test from "node:test";
import { DEFAULT_OPTIONS, supportedThinking } from "../../worker/config.ts";
import { configureWorkerSettings as configureSettings } from "./settings.ts";
import { parseSettings, saveWorkerSettings, SettingsError } from "../../worker/settings.ts";

const configureWorkerSettings = (ctx: any, current: any, path: string) => configureSettings(ctx, {
	getSettings: () => current,
	getModels: () => ctx.modelRegistry.getAvailable().map((model: any) => ({ id: `${model.provider}/${model.id}`, thinking: supportedThinking(model) })),
	saveSettings: (_ctx, value) => saveWorkerSettings(ctx, value, path),
});

const settings = () => ({ ...DEFAULT_OPTIONS, fast: { model: "test/model", thinking: "off" as const }, normal: { model: "test/model", thinking: "off" as const }, deep: { model: "test/model", thinking: "off" as const } });
const registry = { getAvailable: () => [{ provider: "test", id: "model", reasoning: false }] };
const setup = (t: any, protocol = "PI_GOUI_PROTOCOL") => {
	const dir = mkdtempSync(join(tmpdir(), "worker-settings-ui-"));
	const previous = { PI_UI_PROTOCOL: process.env.PI_UI_PROTOCOL, PI_GOUI_PROTOCOL: process.env.PI_GOUI_PROTOCOL };
	for (const name of Object.keys(previous)) delete process.env[name];
	process.env[protocol] = "1";
	t.after(() => { for (const [name, value] of Object.entries(previous)) { if (value === undefined) delete process.env[name]; else process.env[name] = value; } rmSync(dir, { recursive: true, force: true }); });
	return { dir, path: join(dir, "settings.json") };
};

for (const protocol of ["PI_UI_PROTOCOL", "PI_GOUI_PROTOCOL"]) test(`${protocol} settings sends data only; cancellation never writes`, async (t) => {
	const fixture = setup(t, protocol);
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

test("Go submits explicit responses without duplicating capability completion notices", async (t) => {
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
	assert.deepEqual(events, []);
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

test("structured save errors survive separate extension module identities", async (t) => {
	setup(t);
	let calls = 0;
	const error = { code: "thinking-unsupported", field: "fast", model: "test/model", thinking: "high" };
	const ctx: any = { mode: "rpc", hasUI: true, ui: { input: async (_title: string, payload: string) => {
		if (++calls === 1) return JSON.stringify(settings());
		assert.deepEqual(JSON.parse(payload).error, error);
		return undefined;
	} } };
	assert.equal(await configureSettings(ctx, { getSettings: settings, getModels: () => [], saveSettings: () => { throw Object.assign(new Error("foreign module"), { data: error }); } }), undefined);
	assert.equal(calls, 2);
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

test("native menus consume only capability model choices and defer saving validation", async () => {
	const current = { ...settings(), fast: { model: "fixture/model", thinking: "high" as const } };
	const choose = (prefix: string) => (options: string[]) => options.find(value => value.startsWith(prefix));
	const steps = [choose("Fast ·"), choose("模型 ·"),
		(options: string[]) => { assert.ok(options.includes("○ service/only")); return "○ service/only"; },
		choose("Thinking ·"), (options: string[]) => { assert.deepEqual(options, ["○ low"]); return options[0]; },
		choose("返回"), choose("保存并退出")];
	let lookups = 0, saves = 0;
	const notices: string[] = [];
	const ctx = { hasUI: true, mode: "tui", get modelRegistry(): never { throw new Error("UI must not inspect modelRegistry"); }, ui: {
		select: async (_title: string, options: string[]) => { assert.ok(steps.length); return steps.shift()!(options); },
		notify: (message: string) => notices.push(message),
	} };
	const saved = await configureSettings(ctx as any, {
		getSettings: () => current,
		getModels: () => { lookups++; return [{ id: "service/only", thinking: ["low"] }]; },
		saveSettings: (_ctx, value) => { saves++; return structuredClone(value) as any; },
	});
	assert.equal(lookups, 2);
	assert.equal(saves, 1);
	assert.deepEqual(saved?.fast, { model: "service/only", thinking: "low" });
	assert.equal(current.fast.model, "fixture/model");
	assert.equal(steps.length, 0);
	assert.match(notices[0], /保留原值/);
});

test("native save errors keep the draft open and render capability error data", async () => {
	let saves = 0, selects = 0;
	const notices: string[] = [];
	const ctx = { mode: "tui", hasUI: true, get modelRegistry(): never { throw new Error("UI must not inspect modelRegistry"); }, ui: {
		select: async (_title: string, options: string[]) => { const label = ++selects === 1 ? "保存并退出" : "取消"; return options.find(value => value === label); },
		notify: (message: string) => notices.push(message),
	} };
	const result = await configureSettings(ctx as any, {
		getSettings: settings, getModels: () => [],
		saveSettings: () => { saves++; throw new SettingsError({ code: "thinking-unsupported", field: "fast", thinking: "high" }); },
	});
	assert.equal(result, undefined);
	assert.equal(saves, 1);
	assert.deepEqual(notices, ["Fast 模型不支持 high"]);
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
