import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test, { type TestContext } from "node:test";
import { DEFAULT_OPTIONS, loadRoutingConfig, resolveRoute, validateConfig, validateTask } from "./config.ts";
import { PRESETS, THINKING_LEVELS } from "./api.ts";
import type { RoutingConfig, Thinking, WorkerTask } from "./types.ts";

const settings = (thinking: Thinking = "high"): RoutingConfig => ({
	...DEFAULT_OPTIONS,
	fast: { model: "fixture/fast", thinking: "high" },
	normal: { model: "fixture/normal", thinking: "high" },
	deep: { model: "fixture/deep", thinking },
});

const context = (maxThinking = true) => ({
	modelRegistry: { getAvailable: () => ["fast", "normal", "deep"].map((id) => ({
		provider: "fixture", id, reasoning: true,
		thinkingLevelMap: { xhigh: "high", ...(maxThinking ? { max: "max" } : {}) },
	})) },
}) as any;

test("three-tier config requires no max preset and drops legacy max without mutating input", () => {
	assert.deepEqual(PRESETS, ["fast", "normal", "deep"]);
	const original = { ...settings(), max: { model: "fixture/old", thinking: "max" } };
	const validated = validateConfig(original).config;
	assert.equal("max" in validated, false);
	assert.equal("max" in original, true);
	assert.deepEqual(validateConfig(settings()).config, settings());
});

function settingsFile(t: TestContext): string {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-settings-test-"));
	const previous = process.env.PI_CODING_AGENT_DIR;
	t.after(() => {
		if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
		else process.env.PI_CODING_AGENT_DIR = previous;
		fs.rmSync(root, { recursive: true, force: true });
	});
	process.env.PI_CODING_AGENT_DIR = root;
	return path.join(root, "worker-settings.json");
}

test("loadRoutingConfig persists removal of only the legacy top-level max preset", (t) => {
	const file = settingsFile(t);
	fs.writeFileSync(file, JSON.stringify({ ...settings(), max: { model: "fixture/old", thinking: "high" } }));
	const first = loadRoutingConfig();
	assert.equal("max" in first.config, false);
	const onDisk = JSON.parse(fs.readFileSync(file, "utf8"));
	assert.equal("max" in onDisk, false);
	assert.deepEqual(onDisk, settings());
	assert.deepEqual(loadRoutingConfig().config, first.config);
});

test("thinking is preserved exactly during validation, loading, migration and routing", (t) => {
	const file = settingsFile(t);
	for (const thinking of THINKING_LEVELS) {
		const raw = settings();
		for (const preset of PRESETS) raw[preset].thinking = thinking;
		const original = structuredClone(raw);
		const text = JSON.stringify(raw);
		fs.writeFileSync(file, text);
		const loaded = loadRoutingConfig();
		assert.deepEqual(loaded.config, original);
		assert.deepEqual(loaded.warnings, []);
		assert.deepEqual(validateConfig(raw), { config: original, warnings: [] });
		assert.deepEqual(raw, original);
		assert.equal(fs.readFileSync(file, "utf8"), text, "valid settings must not be rewritten");
		for (const preset of PRESETS) {
			const task: WorkerTask = { mode: "scout", objective: "fixture", preset, sessionMode: "new" };
			assert.equal(resolveRoute(task, loaded.config, context()).thinking, thinking);
		}
		const { maxOutputBytes: _missingDefault, ...legacy } = raw;
		fs.writeFileSync(file, JSON.stringify(legacy));
		assert.deepEqual(loadRoutingConfig().config, original);
		assert.deepEqual(JSON.parse(fs.readFileSync(file, "utf8")), original);
	}
});

test("missing, malformed and unsupported thinking is rejected without conversion", (t) => {
	const file = settingsFile(t);
	for (const preset of PRESETS) {
		for (const thinking of [undefined, null, "", "automatic", "HIGH", false, 0, {}]) {
			const raw = settings();
			(raw[preset] as any).thinking = thinking;
			const text = JSON.stringify(raw);
			fs.writeFileSync(file, text);
			assert.throws(() => loadRoutingConfig(), new RegExp(preset + "\\.thinking"));
			assert.equal(fs.readFileSync(file, "utf8"), text);
		}
	}
	const task: WorkerTask = { mode: "scout", objective: "fixture", preset: "deep", sessionMode: "new" };
	const ctx = { modelRegistry: { getAvailable: () => [{ provider: "fixture", id: "deep", reasoning: false }] } } as any;
	assert.equal(resolveRoute(task, settings("off"), ctx).thinking, "off");
	for (const thinking of THINKING_LEVELS.filter((level) => level !== "off")) {
		const config = settings(thinking);
		assert.throws(() => resolveRoute(task, config, ctx), /不支持 thinking/);
		assert.equal(config.deep.thinking, thinking);
	}
});

test("flat defaults fill only missing options and preserve custom fields", (t) => {
	const file = settingsFile(t);
	for (const key of Object.keys(DEFAULT_OPTIONS)) {
		const raw: Record<string, unknown> = { ...settings(), custom: { enabled: true } };
		delete raw[key];
		fs.writeFileSync(file, JSON.stringify(raw));
		const expected = validateConfig({ ...DEFAULT_OPTIONS, ...raw } as RoutingConfig).config;
		assert.deepEqual(loadRoutingConfig().config, expected, key);
		assert.deepEqual(JSON.parse(fs.readFileSync(file, "utf8")), expected, key);
	}
});

test("complete options are normalized without rewriting explicit falsy values", (t) => {
	const file = settingsFile(t);
	const raw = { ...settings(), version: 0, maxConcurrentWorkers: 0, defaultTimeoutMs: null, maxOutputBytes: "" };
	const text = JSON.stringify(raw);
	fs.writeFileSync(file, text);
	const { config } = loadRoutingConfig();
	assert.equal(config.version, 0);
	assert.equal(config.maxConcurrentWorkers, DEFAULT_OPTIONS.maxConcurrentWorkers);
	assert.equal(config.defaultTimeoutMs, DEFAULT_OPTIONS.defaultTimeoutMs);
	assert.equal(config.maxOutputBytes, DEFAULT_OPTIONS.maxOutputBytes);
	assert.equal(fs.readFileSync(file, "utf8"), text);
});

test("obsolete delegation switch is removed without changing other settings", (t) => {
	const file = settingsFile(t);
	for (const automaticDelegationEnabled of [true, false]) {
		const expected = { ...settings("minimal"), custom: { enabled: false } };
		const raw = { ...expected, automaticDelegationEnabled };
		const before = structuredClone(raw);
		assert.deepEqual(validateConfig(raw).config, expected);
		assert.deepEqual(raw, before, "validation must not mutate input");
		fs.writeFileSync(file, JSON.stringify(raw));
		assert.deepEqual(loadRoutingConfig().config, expected);
		assert.deepEqual(JSON.parse(fs.readFileSync(file, "utf8")), expected);
		const migrated = fs.readFileSync(file, "utf8");
		assert.deepEqual(loadRoutingConfig().config, expected);
		assert.equal(fs.readFileSync(file, "utf8"), migrated);
	}
});

test("legacy retry and per-preset output limits are removed and persisted", (t) => {
	const file = settingsFile(t);
	for (const raw of [
		{ ...settings(), maxAutomaticRetries: 2 },
		...PRESETS.map((preset) => ({ ...settings(), [preset]: { ...settings()[preset], maxOutputBytes: 100 } })),
	]) {
		fs.writeFileSync(file, JSON.stringify(raw));
		assert.deepEqual(loadRoutingConfig().config, settings());
		assert.deepEqual(JSON.parse(fs.readFileSync(file, "utf8")), settings());
	}
});

test("invalid settings remain untouched rather than persisting defaults", (t) => {
	const file = settingsFile(t);
	for (const text of ["{", "null", "[]", "42", '"text"', "{}", JSON.stringify({ fast: settings().fast })]) {
		fs.writeFileSync(file, text);
		assert.throws(() => loadRoutingConfig());
		assert.equal(fs.readFileSync(file, "utf8"), text);
	}
});

test("preset is required and explicit routes never infer a tier", (t) => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-route-tiers-"));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	const base: WorkerTask = { mode: "scout", objective: "ordinary work", preset: "normal", sessionMode: "new" };
	for (const preset of [undefined, null, "", "auto", "max", "FAST", false, 0]) {
		const invalid = { ...base, preset } as WorkerTask;
		assert.match(validateTask(invalid, root).join(" "), /无效 preset/);
		assert.throws(() => resolveRoute(invalid, settings(), context()), /无效 preset/);
	}
	const config = settings();
	for (const preset of PRESETS) {
		for (const objective of ["find a symbol", "ordinary work", "cross-module concurrency"]) {
			const task = { ...base, preset, objective, relevantFiles: Array(8).fill("fixture.ts") };
			assert.deepEqual(validateTask(task, root), []);
			const route = resolveRoute(task, config, context());
			assert.equal(route.requestedPreset, preset);
			assert.equal(route.resolvedPreset, preset);
			assert.equal(route.modelId, config[preset].model);
			assert.deepEqual(route.routeReason, [`任务显式指定 ${preset}`]);
		}
	}
});

test("max and xhigh thinking need model support, not an explicit-authorization flag", () => {
	const task: WorkerTask = { mode: "scout", objective: "work", preset: "deep", sessionMode: "new" };
	assert.equal(resolveRoute(task, settings("max"), context()).thinking, "max");
	assert.equal(resolveRoute(task, settings("xhigh"), context(false)).thinking, "xhigh");
	assert.throws(() => resolveRoute(task, settings("max"), context(false)), /fixture\/deep 不支持 thinking max/);
});

test("directory literals are rejected relative to task.cwd, including ./ and trailing slashes", (t) => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-path-validation-"));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	fs.mkdirSync(path.join(root, "package", "backend"), { recursive: true });
	fs.mkdirSync(path.join(root, "package", "frontend"));
	fs.writeFileSync(path.join(root, "package", "backend", "existing.ts"), "");
	const base: WorkerTask = { mode: "implement", objective: "scoped", preset: "normal", sessionMode: "new", cwd: "package", allowedPaths: ["backend/**"] };
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
