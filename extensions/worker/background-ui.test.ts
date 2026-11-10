import assert from "node:assert/strict";
import test from "node:test";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { Text, stripTerminalSequences, visibleWidth } from "@earendil-works/pi-tui";
import type { Theme } from "@earendil-works/pi-coding-agent";
import workerExtension, { batchResponse } from "./index.ts";
import { WorkerRuntime } from "./runtime.ts";
import { start, task, until } from "./fixtures/helpers.ts";
import { DEFAULT_OPTIONS } from "./config.ts";
import type { executeTask } from "./process.ts";
import { renderWorkerDetails } from "./ui.ts";
// Optional legacy UI compatibility; the archived root ui/ is never loaded here.
const legacyUiEntry = new URL("../ui/index.ts", import.meta.url);

const theme = { fg: (_: string, text: string) => text, bold: (text: string) => text } as Theme;
const secret = (name: string) => `${name}=${name}-fixture-hidden`;
const complete = { status: "completed", summary: ["done"] };

function harness(execute: typeof executeTask) {
	const tools = new Map<string, any>();
	const handlers = new Map<string, any>();
	const commands = new Map<string, any>();
	const usage: any[] = [];
	const branch: any[] = [];
	const eventHandlers = new Map<string, any>();
	const oldDepth = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	const preset = { model: "fixture/local", thinking: "high" as const };
	try {
		workerExtension({
			on: (event: string, fn: any) => handlers.set(event, fn), registerTool: (tool: any) => tools.set(tool.name, tool),
			registerCommand: (name: string, value: any) => commands.set(name, value),
			events: { on: (name: string, fn: any) => eventHandlers.set(name, fn), emit: (name: string, item: any) => { usage.push(item); eventHandlers.get(name)?.(item); } },
			appendEntry: (customType: string, data: any) => branch.push({ type: "custom", customType, data }),
			sendMessage: () => { throw new Error("must not inject messages"); },
		} as any, { executeTask: execute, loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, fast: preset, normal: preset, deep: preset }, warnings: [], path: "fixture" }) });
	} finally {
		if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth;
	}
	const ctx = { sessionManager: { getBranch: () => branch }, cwd: process.cwd(), hasUI: true, mode: "tui", ui: new Proxy({}, { get() { throw new Error("no widget/footer/custom UI allowed"); } }) };
	return { tools, handlers, commands, usage, ctx, branch, eventHandlers };
}

test("worker discovers its bundled orchestration skill", async () => {
	const h = harness(async () => complete);
	const resources = await h.handlers.get("resources_discover")();
	assert.deepEqual(resources, {
		skillPaths: [fileURLToPath(new URL("./SKILL.md", import.meta.url))],
	});
	const { readFile } = await import("node:fs/promises");
	assert.match(await readFile(resources.skillPaths[0], "utf8"), /^name: worker-orchestration$/m);
});

// Exercise the installed Pi implementation, not a mock that assumes invalidate
// works after execute. Resolve relative to the public package, without patching it.
async function piToolComponent() {
	const root = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
	const { initTheme } = await import(pathToFileURL(join(root, "modes/interactive/theme/theme.js")).href);
	initTheme("dark", false);
	return (await import(pathToFileURL(join(root, "modes/interactive/components/tool-execution.js")).href)).ToolExecutionComponent;
}


test("original Pi card receives progress until completion and continuations remain hidden", async () => {
	let release!: () => void, ready = false;
	const hold = new Promise<void>((resolve) => { release = resolve; });
	const h = harness(async (_task, _config, _warnings, _ctx, _signal, progress) => {
		progress?.({ status: "running", phase: "运行", usage: { input: 20, output: 10, cacheRead: 0, cacheWrite: 0, contextTokens: 30, turns: 1 } });
		ready = true; await hold;
		return { ...complete, execution: { usage: { input: 20, output: 10, cacheRead: 0, cacheWrite: 0, contextTokens: 30, turns: 1 } } };
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const tool = h.tools.get("worker");
		const args = { tasks: [{ mode: "scout", preset: "normal", sessionMode: "fork", objective: "live task" }] };
		const Component = await piToolComponent();
		const row = new Component("worker", "owner", args, {}, tool, { requestRender() {} }, process.cwd());
		row.markExecutionStarted(); row.setArgsComplete();
		let details: any;
		const first = tool.execute("owner", args, undefined, (value: any) => { details = value.details; row.updateResult(value, true); }, h.ctx);
		await until(() => ready);
		assert.match(stripTerminalSequences(row.render(160).join("\n")), /live task/);
		assert.equal(details.tasks[0].status, "running");
		assert.ok(h.usage.length > 0);
		const invalid = { batchId: details.batchId, cancel: false };
		await assert.rejects(tool.execute("invalid", invalid, undefined, undefined, h.ctx), /参数错误/);
		await until(() => /控制错误/.test(stripTerminalSequences(row.render(160).join("\n"))));
		const hidden = new Component("worker", "invalid", invalid, {}, tool, { requestRender() {} }, process.cwd());
		hidden.updateResult({ content: [{ type: "text", text: "参数错误" }], isError: true }, false);
		assert.deepEqual(hidden.render(80), []);
		release();
		const result = await first; row.updateResult(result, false);
		assert.equal(result.details.tasks[0].status, "completed");
		const before = structuredClone(result.details);
		for (const expanded of [false, true]) {
			const render = (value: any) => tool.renderResult(value, { expanded, isPartial: false }, theme).render(160);
			const normal = render(result);
			assert.ok(normal.length > 0);
			assert.deepEqual(render({ details: result.details, get content() { throw new Error("UI must not read JSON content"); } }), normal);
		}
		assert.deepEqual(result.details, before, "rendering must not mutate details");
		const continued = await tool.execute("continue", { batchId: details.batchId }, undefined, undefined, h.ctx);
		const followup = new Component("worker", "continue", { batchId: details.batchId }, {}, tool, { requestRender() {} }, process.cwd());
		followup.updateResult(continued, false);
		assert.deepEqual(followup.render(80), []);
		assert.ok(h.branch.length > 0, "progress and completion snapshots persist");
	} finally { release(); await h.handlers.get("session_shutdown")({}); }
});

test("snapshots are detached and redact secrets while preserving routing IDs outside compression", async () => {
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a.ts")], async () => ({ status: "completed", summary: ["long".repeat(5_000)] }));
	batch.maxOutputBytes = 256;
	batch.ui.originToolCallId = "origin-" + "a".repeat(100);
	batch.ui.controlErrors = [{ toolCallId: "control-" + "b".repeat(100), message: secret("password") }];
	await runtime.wait(batch.id);
	const response = batchResponse(batch);
	const payload = JSON.parse(response.content[0].text);
	assert.equal(payload.result.truncated, true);
	assert.equal(payload.batchId, batch.id);
	assert.equal(payload.tasks[0].taskId, batch.tasks[0].id);
	assert.equal(response.details.originToolCallId, batch.ui.originToolCallId);
	assert.equal(response.details.controlErrors![0].toolCallId, batch.ui.controlErrors[0].toolCallId);
	assert.doesNotMatch(JSON.stringify(response), /password-fixture-hidden/);
	batch.ui.tasks[0].objective = "changed later";
	assert.notEqual(response.details.tasks[0].objective, "changed later");
	await runtime.dispose();
});

test("static historical cards freeze elapsed time and rebuild with the current theme", async (t) => {
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("history.ts")], async () => complete);
	await runtime.wait(batch.id);
	const details = batchResponse(batch).details;
	details.finishedAt = undefined;
	details.snapshotAt = 5_000;
	details.tasks[0].startedAt = 1_000;
	details.tasks[0].finishedAt = undefined;
	details.tasks[0].status = "running";
	t.mock.method(Date, "now", () => 60_000);
	const first = renderWorkerDetails(details, theme, { snapshot: true }).render(160).join("\n");
	t.mock.method(Date, "now", () => 120_000);
	const later = renderWorkerDetails(details, theme, { snapshot: true }).render(160).join("\n");
	assert.equal(first, later);
	assert.match(first, /4\.0s/);
	assert.doesNotMatch(first, /历史快照|Batch |执行中/);
	const alternate = { ...theme, fg: (_: string, text: string) => `<new>${text}</new>` } as Theme;
	assert.match(renderWorkerDetails(details, alternate, { snapshot: true }).render(160).join("\n"), /<new>/);
	await runtime.dispose();
});


test("installed Pi and complete UI patch stack frame only visible Worker rows like ordinary tools", {
	skip: existsSync(legacyUiEntry) ? false : "legacy UI extension is archived",
}, async () => {
	const { patchToolDisplay, patchFinalResponseSeparator, patchFullscreenScrollbar, patchMergeConsecutiveTools, patchUserMessageFrame } = await import(legacyUiEntry.href);
	const Component = await piToolComponent();
	// Same installation order as extensions/ui/index.ts; no mocked render/updateDisplay.
	patchFinalResponseSeparator();
	patchToolDisplay(); patchMergeConsecutiveTools(); patchFullscreenScrollbar();
	patchUserMessageFrame();
	const root = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
	const { initTheme, theme: liveTheme } = await import(pathToFileURL(join(root, "modes/interactive/theme/theme.js")).href);
	const h = harness(async () => complete);
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const makeRow = (name: string, id: string, args: any, definition: any) => new Component(name, id, args, {}, definition, { requestRender() {} }, process.cwd());
		const workerTool = h.tools.get("worker");
		const args = { tasks: [{ mode: "scout", preset: "normal", sessionMode: "fork", objective: "宽度中文ABC" }] };
		const owner = makeRow("worker", "framed-owner", args, workerTool);
		const ordinary = makeRow("ordinary", "normal", {}, {
			renderCall: () => new Text(liveTheme.fg("toolTitle", liveTheme.bold("Ordinary")), 0, 0),
			renderResult: () => new Text(liveTheme.fg("toolOutput", "宽度中文ABC"), 0, 0),
		});
		const edgeColor = (line: string) => line.match(/\x1b\[38;2;\d+;\d+;\d+m/)?.[0];
		const toolFrame = (lines: string[]) => {
			assert.doesNotMatch(lines.join("\n"), /[▄▀]|\x1b\[48;/);
			assert.match(stripTerminalSequences(lines[0]!), /^╭─.*╮$/);
			assert.match(stripTerminalSequences(lines.at(-1)!), /^╰─*╯$/);
			assert.ok(edgeColor(lines[0]!));
			assert.notEqual(edgeColor(lines[0]!), edgeColor(lines.at(-1)!));
			assert.ok(lines.every(line => visibleWidth(line) === 40));
		};
		owner.markExecutionStarted(); ordinary.markExecutionStarted();
		const pending = owner.render(40);
		assert.equal(edgeColor(pending[1]!), edgeColor(ordinary.render(40)[1]!));
		toolFrame(pending);
		const initial = await workerTool.execute("framed-owner", args, undefined, (result: any) => owner.updateResult(result, true), h.ctx);
		owner.updateResult(initial, false);
		ordinary.updateResult({ content: [{ type: "text", text: "done" }] }, false);
		const drawn = owner.render(40);
		assert.match(stripTerminalSequences(drawn.join("\n")), /宽度中文ABC/);
		assert.equal(edgeColor(drawn[1]!), edgeColor(ordinary.render(40)[1]!), "same tool border color");
		toolFrame(drawn);
		toolFrame(ordinary.render(40));
		for (const width of [1, 2, 3, 8, 20, 40]) for (const expanded of [false, true]) {
			owner.setExpanded(expanded);
			assert.ok(owner.render(width).every((line: string) => visibleWidth(line) <= width), `owner width ${width}`);
		}
		owner.setExpanded(false);
		const payload = JSON.parse(initial.content[0].text);
		const hidden = makeRow("worker", "follow-up", {}, workerTool);
		assert.deepEqual(hidden.render(40), []);
		hidden.updateArgs({ batchId: payload.batchId }); hidden.markExecutionStarted();
		assert.deepEqual(hidden.render(40), []);
		const followup = await workerTool.execute("follow-up", { batchId: payload.batchId });
		hidden.updateResult(followup, false); hidden.setExpanded(true);
		assert.deepEqual(hidden.render(40), [], "continuation and final result never acquire a shell");
		const CoreContainer = Object.getPrototypeOf(Component.prototype).constructor;
		const adjacent = new CoreContainer(); adjacent.addChild(owner); adjacent.addChild(hidden); adjacent.addChild(ordinary);
		const joined = adjacent.render(40);
		assert.equal(joined.length, 1, "completed Worker and ordinary tools start in one folded summary");
		assert.doesNotMatch(joined.join("\n"), /[▄▀]/, "no half-block card edges");
		owner.setExpanded(true);
		assert.ok(adjacent.render(40).some((line: string) => edgeColor(line) === edgeColor(drawn[1]!)), "explicit expansion restores the Worker frame");
		assert.doesNotMatch(adjacent.render(40).join("\n"), /[▄▀]/);
		assert.match(stripTerminalSequences(adjacent.render(40).join("\n")), /宽度中文ABC/);
		const unknown = makeRow("worker", "unknown", { batchId: "unrecognized" }, workerTool);
		unknown.updateResult({ content: [{ type: "text", text: "unknown failure" }], isError: true }, false);
		assert.match(stripTerminalSequences(unknown.render(40).join("\n")), /unknown failure/);
		const ordinaryError = makeRow("ordinary", "err", {}, ordinary.toolDefinition);
		ordinaryError.updateResult({ content: [{ type: "text", text: "err" }], isError: true }, false);
		assert.equal(edgeColor(unknown.render(40)[1]!), edgeColor(ordinaryError.render(40)[1]!));
		const otherSelf = makeRow("other-self", "other", {}, { renderShell: "self", renderCall: () => new Text("other shell", 0, 0) });
		assert.doesNotMatch(otherSelf.render(40).join("\n"), /[▄▀]/, "unrelated custom self shells are unchanged");
		initTheme("light", false);
		owner.invalidate(); ordinary.invalidate();
		assert.equal(edgeColor(owner.render(40)[1]!), edgeColor(ordinary.render(40)[1]!), "theme switching preserves the shared frame palette");
	} finally {
		initTheme("dark", false);
		await h.handlers.get("session_shutdown")({});
	}
});


test("history reload merges only the selected branch snapshot and rejects stale live control", async () => {
	const h = harness(async () => complete);
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const tool = h.tools.get("worker");
		const args = { tasks: [{ mode: "scout", preset: "normal", sessionMode: "fork", objective: "branch snapshot" }] };
		const result = await tool.execute("origin", args, undefined, undefined, h.ctx);
		const batchId = result.details.batchId;
		await h.handlers.get("session_start")({}, h.ctx);
		assert.match(tool.renderResult(result, {}, theme, { toolCallId: "origin", args, state: {}, invalidate() {} }).render(100).join("\n"), /branch snapshot/);
		await assert.rejects(tool.execute("stale", { batchId }, undefined, undefined, h.ctx), /过期/);
		const otherCtx = { ...h.ctx, sessionManager: { getBranch: () => [] } };
		await h.handlers.get("session_start")({}, otherCtx);
		const continuation = tool.renderResult({ content: [{ type: "text", text: "unrecognized" }] }, {}, theme, { toolCallId: "new", args: { batchId } });
		assert.match(continuation.render(100).join("\n"), /unrecognized/);
	} finally { await h.handlers.get("session_shutdown")({}); }
});
