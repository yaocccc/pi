import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test, { type TestContext } from "node:test";
import { DEFAULT_OPTIONS, PRESETS, RESOLVED_PRESETS, inferPreset, loadRoutingConfig, resolveRoute, validateConfig, validateTask } from "./config.ts";
import type { RoutingConfig, WorkerTask } from "./types.ts";

const settings = (thinking: "high" | "xhigh" | "max" = "high"): RoutingConfig => ({
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
	assert.deepEqual(PRESETS, ["auto", "fast", "normal", "deep"]);
	assert.deepEqual(RESOLVED_PRESETS, ["fast", "normal", "deep"]);
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

test("flat defaults fill only missing options and preserve custom fields", (t) => {
	const file = settingsFile(t);
	for (const key of Object.keys(DEFAULT_OPTIONS)) {
		const raw: Record<string, unknown> = { ...settings(), custom: { enabled: true }, automaticDelegationEnabled: false };
		delete raw[key];
		fs.writeFileSync(file, JSON.stringify(raw));
		const expected = validateConfig({ ...DEFAULT_OPTIONS, ...raw } as RoutingConfig).config;
		assert.deepEqual(loadRoutingConfig().config, expected, key);
		assert.deepEqual(JSON.parse(fs.readFileSync(file, "utf8")), expected, key);
	}
});

test("complete options are normalized without rewriting explicit falsy values", (t) => {
	const file = settingsFile(t);
	const raw = { ...settings(), version: 0, automaticDelegationEnabled: false, maxConcurrentWorkers: 0, defaultTimeoutMs: null, maxOutputBytes: "" };
	const text = JSON.stringify(raw);
	fs.writeFileSync(file, text);
	const { config } = loadRoutingConfig();
	assert.equal(config.version, 0);
	assert.equal(config.automaticDelegationEnabled, false);
	assert.equal(config.maxConcurrentWorkers, DEFAULT_OPTIONS.maxConcurrentWorkers);
	assert.equal(config.defaultTimeoutMs, DEFAULT_OPTIONS.defaultTimeoutMs);
	assert.equal(config.maxOutputBytes, DEFAULT_OPTIONS.maxOutputBytes);
	assert.equal(fs.readFileSync(file, "utf8"), text);
});

test("legacy retry and per-preset output limits are removed and persisted", (t) => {
	const file = settingsFile(t);
	for (const raw of [
		{ ...settings(), maxAutomaticRetries: 2 },
		...RESOLVED_PRESETS.map((preset) => ({ ...settings(), [preset]: { ...settings()[preset], maxOutputBytes: 100 } })),
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

test("max preset is rejected and auto resolves only fast/normal/deep", (t) => {
	const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-route-tiers-"));
	t.after(() => fs.rmSync(root, { recursive: true, force: true }));
	const base: WorkerTask = { mode: "scout", objective: "ordinary work" };
	const invalid = { ...base, preset: "max" as any };
	assert.match(validateTask(invalid, root).join(" "), /无效 preset: max/);
	assert.throws(() => inferPreset(invalid), /无效 preset: max/);
	assert.throws(() => resolveRoute(invalid, settings(), context()), /无效 preset: max/);
	const config = settings();
	for (const preset of RESOLVED_PRESETS) {
		const task = { ...base, preset };
		assert.deepEqual(validateTask(task, root), []);
		assert.equal(resolveRoute(task, config, context()).resolvedPreset, preset);
		assert.equal(resolveRoute(task, config, context()).modelId, config[preset].model);
	}
	for (const [objective, preset] of [
		["find a symbol", "fast"], ["ordinary work", "normal"], ["cross-module concurrency", "deep"],
	] as const) {
		const task = { ...base, objective, preset: "auto" as const };
		assert.equal(inferPreset(task).preset, preset);
		assert.equal(resolveRoute(task, config, context()).requestedPreset, "auto");
		assert.equal(resolveRoute(task, config, context()).resolvedPreset, preset);
	}
});

test("max and xhigh thinking need model support, not an explicit-authorization flag", () => {
	const task: WorkerTask = { mode: "scout", objective: "work", preset: "deep" };
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
	const base: WorkerTask = { mode: "implement", objective: "scoped", cwd: "package", allowedPaths: ["backend/**"] };
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
