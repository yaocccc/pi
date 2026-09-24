import assert from "node:assert/strict";
import test from "node:test";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { setTimeout as delay } from "node:timers/promises";
import { Text, stripTerminalSequences, visibleWidth } from "@earendil-works/pi-tui";
import type { Theme } from "@earendil-works/pi-coding-agent";
import workerExtension, { batchResponse } from "./index.ts";
import { WorkerRuntime } from "./runtime.ts";
import { start, task, until } from "./fixtures/helpers.ts";
import { DEFAULT_OPTIONS } from "./config.ts";
import type { executeTask } from "./process.ts";
import { renderWorkerDetails } from "./ui.ts";
import { patchCollapsedThinkingPreview, patchCompactToolDisplay, patchFinalResponseSeparator, patchFullscreenScrollbar, patchMergeConsecutiveTools, patchPaddedBackgroundHalfBlocks, patchThinkingSpacing, patchUserMessageHalfBlocks } from "../ui/index.ts";

const theme = { fg: (_: string, text: string) => text, bold: (text: string) => text } as Theme;
const secret = (name: string) => `${name}=${name}-fixture-hidden`;
const questionId = "a".repeat(64);
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
		} as any, { executeTask: execute, loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, fast: preset, normal: preset, deep: preset, max: preset }, warnings: [], path: "fixture" }) });
	} finally {
		if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth;
	}
	const ctx = { sessionManager: { getBranch: () => branch }, cwd: process.cwd(), hasUI: true, mode: "tui", ui: new Proxy({}, { get() { throw new Error("no widget/footer/custom UI allowed"); } }) };
	return { tools, handlers, commands, usage, ctx, branch, eventHandlers };
}

// Exercise the installed Pi implementation, not a mock that assumes invalidate
// works after execute. Resolve relative to the public package, without patching it.
async function piToolComponent() {
	const root = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
	const { initTheme } = await import(pathToFileURL(join(root, "modes/interactive/theme/theme.js")).href);
	initTheme("dark", false);
	return (await import(pathToFileURL(join(root, "modes/interactive/components/tool-execution.js")).href)).ToolExecutionComponent;
}

test("original Pi tool row keeps refreshing after execute, reply and background completion without later wait rendering", async () => {
	let release!: () => void;
	const hold = new Promise<void>((resolve) => { release = resolve; });
	const h = harness(async (task, _config, _warnings, _ctx, signal, progress, _overlap, ask) => {
		progress?.({ status: "running", phase: "运行", usage: { input: 20, output: 10, cacheRead: 0, cacheWrite: 0, contextTokens: 30, turns: 1 } });
		if (task.objective === "ask") {
			await ask!({ id: questionId, question: `继续吗？ ${secret("password")}`, timeoutMs: 2_000 }, signal!);
			await hold;
		} else await delay(20);
		return { ...complete, observed_changed_files: ["observed.ts"], execution: { usage: { input: 20, output: 10, cacheRead: 0, cacheWrite: 0, contextTokens: 30, turns: 1 } } };
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		assert.deepEqual([...h.commands.keys()], ["worker_settings"]);
		assert.deepEqual([...h.tools.keys()], ["worker"]);
		const tool = h.tools.get("worker");
		const args = { tasks: [{ mode: "scout", objective: "ask" }, { mode: "scout", objective: "independent" }] };
		const Component = await piToolComponent();
		let paints = 0;
		const row = new Component("worker", "owner", args, {}, tool, { requestRender() { paints++; } }, process.cwd());
		row.markExecutionStarted(); row.setArgsComplete();
		const controller = new AbortController();
		let updates = 0;
		const first = await tool.execute("owner", args, controller.signal, (value: any) => { updates++; row.updateResult(value, true); }, h.ctx);
		row.updateResult(first, false);
		const read = () => stripTerminalSequences(row.render(160).join("\n"));
		const payload = JSON.parse(first.content[0].text);
		assert.equal(payload.status, "waiting_for_reply");
		assert.match(read(), /等待回答/);
		assert.doesNotMatch(read(), /Batch |已结束|占用槽位与路径锁/);
		assert.doesNotMatch(read(), new RegExp(questionId), "collapsed worker question labels stay concise");
		assert.doesNotMatch(read(), /password-fixture-hidden/);
		const atReturn = updates;
		controller.abort();
		await until(() => /✓/.test(read()));
		assert.equal(updates, atReturn, "no late onUpdate used to update a finished tool");
		const beforeReply = paints;
		const q = payload.questions[0];
		let replyReturned = false;
		const reply = tool.execute("reply", { batchId: payload.batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: `选择 B ${secret("token")}` }] }).then((result: any) => { replyReturned = true; return result; });
		await until(() => /A: 选择 B/.test(read()));
		assert.ok(paints > beforeReply, "ToolRenderContext.invalidate requested the actual TUI redraw");
		assert.match(read(), /已回答/);
		assert.doesNotMatch(read(), /执行中|等待执行/);
		assert.doesNotMatch(read(), /等待主 Agent|token-fixture-hidden/);
		assert.equal(replyReturned, false, "answer invocation waits while the original card updates");
		release();
		assert.equal(JSON.parse((await reply).content[0].text).finished, true);
		await until(() => /✓/.test(read()));
		row.setExpanded(true);
		assert.match(read(), /Q: 继续吗/);
		assert.match(read(), /A: 选择 B/);
		assert.match(read(), new RegExp(questionId), "expanded view exposes the question ID");
		assert.equal(first.details.questions[0].status, "waiting", "serialized initial snapshot is not mutated");
		await assert.rejects(tool.execute("duplicate", { batchId: payload.batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: "再次" }] }), /重复/);
		const completed = JSON.parse((await tool.execute("wait", { batchId: payload.batchId })).content[0].text);
		assert.equal(completed.finished, true);
		assert.deepEqual(completed.result.results[0].observed_changed_files, ["observed.ts"]);
		assert.equal(completed.result.results[0].execution.usage.input, 20);
		assert.ok(h.usage.some((item) => item.taskId === "owner:0" && item.input === 20));
		await h.handlers.get("session_shutdown")({ reason: "reload" });
		await h.handlers.get("session_start")({}, h.ctx);
		row.invalidate();
		assert.doesNotMatch(read(), /Batch |历史快照|已结束/);
		assert.match(read(), /选择 B/);
		const historical = new Component("worker", "owner", args, {}, tool, { requestRender() { paints++; } }, process.cwd());
		historical.updateResult(first, false);
		assert.doesNotMatch(stripTerminalSequences(historical.render(160).join("\n")), /Batch |历史快照|已结束/);
		await assert.rejects(tool.execute("stale", { batchId: payload.batchId }), /过期/);
	} finally {
		release();
		await h.handlers.get("session_shutdown")({ reason: "quit" });
	}
});

test("no 30-second polling return; a later question returns and its expiry updates the original row", async (t) => {
	t.mock.timers.enable({ apis: ["setTimeout", "setInterval"] });
	let allowQuestion!: () => void;
	const gate = new Promise<void>((resolve) => { allowQuestion = resolve; });
	let started!: () => void;
	const running = new Promise<void>((resolve) => { started = resolve; });
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		started();
		await gate;
		await ask!({ id: "later", question: "工具返回后的问题", timeoutMs: 1_000 }, signal!);
		return complete;
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const tool = h.tools.get("worker");
		let updates = 0;
		let returned = false;
		let current: any;
		let rendered = "";
		const context = { state: {}, invalidate() { rendered = tool.renderResult(current, { expanded: true, isPartial: !returned }, theme, context).render(160).join("\n"); } };
		const execution = tool.execute("timed", { task: { mode: "scout", objective: "slow" } }, undefined, (value: any) => { updates++; current = value; context.invalidate(); }, h.ctx).then((result: any) => { returned = true; current = result; return result; });
		await running;
		t.mock.timers.tick(60_000);
		await Promise.resolve();
		assert.equal(returned, false, "no question or completion: still waiting beyond 30 seconds");
		assert.match(rendered, /slow/);
		assert.doesNotMatch(rendered, /执行中|等待执行/);
		allowQuestion();
		const result = await execution;
		assert.equal(JSON.parse(result.content[0].text).status, "waiting_for_reply");
		assert.equal(result.details.questions[0].status, "waiting");
		context.invalidate();
		t.mock.timers.tick(50);
		assert.match(rendered, /工具返回后的问题/);
		assert.match(rendered, /等待回答/);
		t.mock.timers.tick(1_000);
		await tool.execute("wait", { batchId: result.details.batchId });
		t.mock.timers.tick(50);
		assert.match(rendered, /已过期/);
		assert.match(rendered, /✗/);
		assert.doesNotMatch(rendered, /已结束|执行中/);
		assert.equal(updates, 1, "only the initial card snapshot uses onUpdate; later changes use live rendering");
		assert.equal(result.details.questions[0].status, "waiting", "initial snapshot remains immutable");
	} finally {
		allowQuestion();
		await h.handlers.get("session_shutdown")({});
		t.mock.timers.reset();
	}
});

test("switch/tree/shutdown cancels pending questions, freezes old rows and rejects stale replies", async () => {
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		await ask!({ id: "pending", question: "尚未回答", timeoutMs: 10_000 }, signal!);
		return complete;
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const tool = h.tools.get("worker");
		const result = await tool.execute("old", { task: { mode: "scout", objective: "old session" } }, undefined, undefined, h.ctx);
		const context = { state: {}, invalidate() {} };
		tool.renderResult(result, { expanded: true, isPartial: false }, theme, context);
		await h.handlers.get("session_tree")({}, h.ctx);
		const frozen = tool.renderResult(result, { expanded: true, isPartial: false }, theme, context).render(160).join("\n");
		assert.match(frozen, /已取消/);
		assert.doesNotMatch(frozen, /历史快照|已结束|Batch |执行中/);
		const payload = JSON.parse(result.content[0].text);
		await assert.rejects(tool.execute("late", { batchId: payload.batchId, answers: [{ taskId: payload.questions[0].taskId, questionId: "pending", answer: "late" }] }), /过期/);
		await assert.rejects(tool.execute("stale", { batchId: payload.batchId }), /过期/);
	} finally {
		await h.handlers.get("session_shutdown")({});
		await h.handlers.get("session_shutdown")({});
	}
});

test("collapsed cards show pending questions and recent replies; expansion preserves full history and narrow widths", async () => {
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("中文任务.ts")], async () => complete);
	await runtime.wait(batch.id);
	const longQuestion = `中文问题\n${"需要保留的完整上下文".repeat(50)}问题末尾`;
	const longAnswer = `${"回答细节".repeat(100)}答案末尾`;
	batch.questions = Array.from({ length: 5 }, (_, i) => ({
		id: `q-${i}`, batchId: batch.id, taskId: batch.tasks[0].id, status: i === 4 ? "waiting" : "answered", question: i === 0 ? longQuestion : `问题 ${i}`, answer: i === 4 ? undefined : i === 0 ? longAnswer : `回答 ${i}`, askedAt: 1, expiresAt: 2, timeoutMs: 1,
	}));
	const details = batchResponse(batch).details;
	const collapsed = renderWorkerDetails(details, theme).render(160).join("\n");
	assert.match(collapsed, /Q5 · 等待回答/);
	assert.match(collapsed, /回答 3/);
	assert.doesNotMatch(collapsed, /q-0/);
	assert.match(collapsed, /展开工具查看完整问答（5 条）/);
	const expanded = renderWorkerDetails(details, theme, { expanded: true }).render(160).join("\n");
	assert.match(expanded, /问题末尾/);
	assert.match(expanded, /答案末尾/);
	for (const width of [0, 1, 2, 20, 40, 80, 120]) {
		for (const expanded of [false, true]) assert.ok(renderWorkerDetails(details, theme, { expanded }).render(width).every((line) => visibleWidth(line) <= width));
	}
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

test("valid long IDs survive compression while question/context/answer secrets are redacted in snapshots", async () => {
	const runtime = new WorkerRuntime(() => false);
	const batch = start(runtime, [task("a.ts")], async (running) => {
		await runtime.ask(running, { id: questionId, question: secret("password"), context: secret("token"), timeoutMs: 1_000 } as any, running.controller.signal);
		return { status: "completed", summary: ["long".repeat(5_000)], observed_changed_files: ["a.ts"] };
	});
	batch.maxOutputBytes = 256;
	batch.ui.originToolCallId = "origin-" + "a".repeat(100);
	batch.ui.controlErrors = [{ toolCallId: "control-" + "b".repeat(100), message: secret("password") }];
	await runtime.wait(batch.id);
	const waiting = batchResponse(batch);
	const pending = JSON.parse(waiting.content[0].text);
	assert.equal(pending.questions[0].id, questionId);
	assert.equal(waiting.details.questions![0].id, questionId);
	assert.equal(waiting.details.originToolCallId, batch.ui.originToolCallId);
	assert.equal(waiting.details.controlErrors![0].toolCallId, batch.ui.controlErrors![0].toolCallId);
	assert.equal(pending.batchId, batch.id);
	assert.equal(pending.tasks[0].taskId, batch.tasks[0].id);
	assert.doesNotMatch(JSON.stringify(waiting), /password-fixture-hidden|token-fixture-hidden/);
	runtime.reply(batch.id, batch.tasks[0].id, questionId, `ok ${secret("credential")}`);
	await runtime.wait(batch.id);
	const response = batchResponse(batch);
	const done = JSON.parse(response.content[0].text);
	assert.equal(done.finished, true);
	assert.equal(done.result.truncated, true);
	assert.equal(done.history[0].id, questionId);
	assert.equal(response.details.questions![0].id, questionId);
	assert.doesNotMatch(JSON.stringify(response), /credential-fixture-hidden/);
	assert.equal(waiting.details.questions![0].status, "waiting");
	await runtime.dispose();
});

test("installed Pi and complete UI patch stack frame only visible Worker rows like ordinary tools", async () => {
	const Component = await piToolComponent();
	// Same installation order as extensions/ui/index.ts; no mocked render/updateDisplay.
	patchCollapsedThinkingPreview(); patchThinkingSpacing(); patchFinalResponseSeparator();
	patchCompactToolDisplay(); patchMergeConsecutiveTools(); patchFullscreenScrollbar();
	patchPaddedBackgroundHalfBlocks(); patchUserMessageHalfBlocks();
	const root = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
	const { initTheme, theme: liveTheme } = await import(pathToFileURL(join(root, "modes/interactive/theme/theme.js")).href);
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		await ask!({ id: "framed", question: "确认中文？", timeoutMs: 10_000 }, signal!);
		return complete;
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const makeRow = (name: string, id: string, args: any, definition: any) => new Component(name, id, args, {}, definition, { requestRender() {} }, process.cwd());
		const workerTool = h.tools.get("worker");
		const args = { task: { mode: "scout", objective: "宽度中文ABC" } };
		const owner = makeRow("worker", "framed-owner", args, workerTool);
		const ordinary = makeRow("ordinary", "normal", {}, {
			renderCall: () => new Text(liveTheme.fg("toolTitle", liveTheme.bold("Ordinary")), 0, 0),
			renderResult: () => new Text(liveTheme.fg("toolOutput", "宽度中文ABC"), 0, 0),
		});
		const bg = (line: string) => line.match(/\x1b\[48;(?:2;\d+;\d+;\d+|5;\d+)m/)?.[0];
		const boundaries = (lines: string[]) => lines.map(stripTerminalSequences).filter((line) => /^[▄▀]+$/.test(line));
		owner.markExecutionStarted(); ordinary.markExecutionStarted();
		const pending = owner.render(40);
		assert.equal(bg(pending[1]!), bg(ordinary.render(40)[1]!));
		assert.deepEqual(boundaries(pending).map((line) => line[0]), ["▄", "▀"]);
		const initial = await workerTool.execute("framed-owner", args, undefined, (result: any) => owner.updateResult(result, true), h.ctx);
		owner.updateResult(initial, false);
		ordinary.updateResult({ content: [{ type: "text", text: "done" }] }, false);
		const drawn = owner.render(40);
		assert.match(stripTerminalSequences(drawn.join("\n")), /确认中文/);
		assert.equal(bg(drawn[1]!), bg(ordinary.render(40)[1]!), "same active Pi tool background token");
		assert.equal(boundaries(drawn).length, 2);
		assert.equal(boundaries(ordinary.render(40)).length, 2);
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
		const followup = await workerTool.execute("follow-up", { batchId: payload.batchId, answers: [{ taskId: payload.questions[0].taskId, questionId: "framed", answer: "同意" }] });
		hidden.updateResult(followup, false); hidden.setExpanded(true);
		assert.deepEqual(hidden.render(40), [], "answer and final result never acquire a shell");
		const CoreContainer = Object.getPrototypeOf(Component.prototype).constructor;
		const adjacent = new CoreContainer(); adjacent.addChild(owner); adjacent.addChild(hidden); adjacent.addChild(ordinary);
		const joined = adjacent.render(40);
		assert.equal(boundaries(joined).length, 2, "adjacent framed tools share one top and bottom edge");
		await until(() => /同意/.test(stripTerminalSequences(owner.render(40).join("\n"))));
		assert.match(stripTerminalSequences(adjacent.render(40).join("\n")), /同意/, "the original card receives the live reply");
		const unknown = makeRow("worker", "unknown", { batchId: "unrecognized" }, workerTool);
		unknown.updateResult({ content: [{ type: "text", text: "unknown failure" }], isError: true }, false);
		assert.match(stripTerminalSequences(unknown.render(40).join("\n")), /unknown failure/);
		const ordinaryError = makeRow("ordinary", "err", {}, ordinary.toolDefinition);
		ordinaryError.updateResult({ content: [{ type: "text", text: "err" }], isError: true }, false);
		assert.equal(bg(unknown.render(40)[1]!), bg(ordinaryError.render(40)[1]!));
		const otherSelf = makeRow("other-self", "other", {}, { renderShell: "self", renderCall: () => new Text("other shell", 0, 0) });
		assert.equal(boundaries(otherSelf.render(40)).length, 0, "unrelated custom self shells are unchanged");
		initTheme("light", false);
		owner.invalidate(); ordinary.invalidate();
		assert.equal(bg(owner.render(40)[1]!), bg(ordinary.render(40)[1]!), "theme switching uses Pi's active palette");
	} finally {
		initTheme("dark", false);
		await h.handlers.get("session_shutdown")({});
	}
});

const assistantCall = (id: string, args: unknown) => ({ type: "message", message: { role: "assistant", content: [{ type: "toolCall", id, name: "worker", arguments: args }] } });
const toolResult = (id: string, result: unknown) => ({ type: "message", message: { role: "toolResult", toolName: "worker", toolCallId: id, ...result as object } });

test("installed Pi self shell: partial, continue, answer and cancel rows stay exactly zero lines; control errors use original card", async () => {
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		await ask!({ id: "one-card", question: "continue?", timeoutMs: 10_000 }, signal!);
		return complete;
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const tool = h.tools.get("worker");
		const Component = await piToolComponent();
		const makeRow = (id: string, args: any) => new Component("worker", id, args, {}, tool, { requestRender() {} }, process.cwd());
		const args = { task: { mode: "scout", objective: "one card" } };
		const row = makeRow("origin", args);
		const first = await tool.execute("origin", args, undefined, (r: any) => row.updateResult(r, true), h.ctx);
		row.updateResult(first, false);
		const text = () => stripTerminalSequences(row.render(160).join("\n"));
		const batchId = first.details.batchId;
		const q = first.details.questions[0];
		assert.match(text(), /continue\?/);
		const partial = makeRow("partial", {});
		assert.deepEqual(partial.render(160), []);
		partial.markExecutionStarted(); partial.setExpanded(true); partial.invalidate();
		assert.deepEqual(partial.render(20), []);
		for (const [id, input] of [["wait", { batchId }], ["answer", { batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: "yes" }] }], ["cancel", { batchId, cancel: true }]] as const) {
			const next = makeRow(id, {});
			assert.deepEqual(next.render(160), []);
			next.updateArgs(input); next.markExecutionStarted();
			assert.deepEqual(next.render(160), []);
			const result = await tool.execute(id, input);
			next.updateResult(result, false);
			for (const width of [1, 20, 160]) for (const expanded of [true, false]) {
				next.setExpanded(expanded); next.invalidate(); assert.deepEqual(next.render(width), []);
			}
		}
		await assert.rejects(tool.execute("bad-answer", { batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: "again" }] }), /重复/);
		assert.match(text(), /控制错误.*重复/);
		const knownError = makeRow("bad-answer", { batchId });
		knownError.updateResult({ content: [{ type: "text", text: "duplicate" }], isError: true }, false);
		assert.deepEqual(knownError.render(160), []);
		const unknown = makeRow("unknown", { batchId: "missing" });
		unknown.updateResult({ content: [{ type: "text", text: "unknown batch" }], isError: true }, false);
		assert.match(stripTerminalSequences(unknown.render(160).join("\n")), /unknown batch/);
		const unowned = makeRow("unowned", { batchId });
		unowned.updateResult({ content: [{ type: "text", text: "external error" }], isError: true }, false);
		assert.match(stripTerminalSequences(unowned.render(160).join("\n")), /external error/);
	} finally { await h.handlers.get("session_shutdown")({}); }
});

test("reload beforeSessionStart and real Pi renderBeforeBind hydrate initial rows without rebinding old sessions", async () => {
	let executions = 0;
	const run: typeof executeTask = async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		executions++;
		await ask!({ id: "reload-question", question: "reload Q", timeoutMs: 10_000 }, signal!);
		return complete;
	};
	const original = harness(run);
	await original.handlers.get("session_start")({}, original.ctx);
	const sessions = [original];
	try {
		const args = { task: { mode: "scout", objective: "reload origin" } };
		original.branch.push(assistantCall("reload-origin", args));
		const initial = await original.tools.get("worker").execute("reload-origin", args, undefined, undefined, original.ctx);
		original.branch.push(toolResult("reload-origin", initial));
		const fork = [...original.branch];
		const q = initial.details.questions[0];
		const answerArgs = { batchId: initial.details.batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: "latest persisted answer" }] };
		original.branch.push(assistantCall("reload-answer", answerArgs));
		const final = await original.tools.get("worker").execute("reload-answer", answerArgs);
		original.branch.push(toolResult("reload-answer", final));
		await original.handlers.get("session_shutdown")({});

		const Component = await piToolComponent();
		const restored = harness(run); sessions.push(restored); restored.branch.push(...original.branch);
		const row = new Component("worker", "reload-origin", args, {}, restored.tools.get("worker"), { requestRender() {} }, process.cwd());
		// reload's beforeSessionStart has already constructed and rendered this exact row.
		row.updateResult(initial, false);
		const text = (component: any) => stripTerminalSequences(component.render(160).join("\n"));
		assert.match(text(row), /等待回答/); assert.doesNotMatch(text(row), /latest persisted answer/);
		await restored.handlers.get("session_start")({}, restored.ctx);
		assert.match(text(row), /latest persisted answer/, "binding itself invalidates the startup row");
		row.invalidate(); assert.match(text(row), /latest persisted answer/);
		assert.doesNotMatch(text(row), /历史快照|Batch |已结束/);

		// Session switch/fork gets a fresh extension owner; installed Pi renders it BEFORE binding.
		const selected = harness(run); sessions.push(selected); selected.branch.push(...fork);
		const root = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
		const { InteractiveMode } = await import(pathToFileURL(join(root, "modes/interactive/interactive-mode.js")).href);
		let forkRow: any;
		const surface = { session: {}, applyRuntimeSettings() {}, subscribeToAgent() {}, updateAvailableProviderCount() {}, updateEditorBorderColor() {}, updateTerminalTitle() {},
			renderCurrentSessionState() {
				forkRow = new Component("worker", "reload-origin", args, {}, selected.tools.get("worker"), { requestRender() {} }, process.cwd());
				forkRow.updateResult(initial, false); assert.match(text(forkRow), /等待回答/);
			},
			bindCurrentSessionExtensions: () => selected.handlers.get("session_start")({}, selected.ctx),
		};
		await restored.handlers.get("session_shutdown")({});
		await InteractiveMode.prototype.rebindCurrentSession.call(surface, { renderBeforeBind: true });
		forkRow.invalidate(); assert.match(text(forkRow), /等待回答/); assert.doesNotMatch(text(forkRow), /latest persisted answer/);
		row.invalidate(); assert.match(text(row), /latest persisted answer/, "old session row stays frozen");
		// A real replacement on the SAME registered renderer also cannot adopt old rows.
		restored.branch.splice(0, restored.branch.length, ...fork);
		await restored.handlers.get("session_start")({}, restored.ctx);
		row.invalidate(); assert.match(text(row), /latest persisted answer/);
		assert.equal(executions, 1, "history never restarts a worker");
	} finally { for (const h of sessions) await h.handlers.get("session_shutdown")({}); }
});

test("reload and tree use only branch snapshots, merge latest replies/errors into each origin and never resume work", async () => {
	let executions = 0;
	const h = harness(async (_task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
		executions++;
		await ask!({ id: `history-${executions}`, question: "history Q", timeoutMs: 10_000 }, signal!);
		return complete;
	});
	await h.handlers.get("session_start")({}, h.ctx);
	try {
		const tool = h.tools.get("worker"); const Component = await piToolComponent();
		const starts: any[] = [];
		for (let n = 0; n < 2; n++) {
			const id = `origin-${n}`; const args = { task: { mode: "scout", objective: id } };
			h.branch.push(assistantCall(id, args));
			const initial = await tool.execute(id, args, undefined, undefined, h.ctx);
			h.branch.push(toolResult(id, initial));
			const fork = [...h.branch];
			const q = initial.details.questions[0];
			const answerArgs = { batchId: initial.details.batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: `reply-${n}` }] };
			h.branch.push(assistantCall(`answer-${n}`, answerArgs));
			const final = await tool.execute(`answer-${n}`, answerArgs);
			h.branch.push(toolResult(`answer-${n}`, final));
			h.branch.push(assistantCall(`error-${n}`, answerArgs));
			await assert.rejects(tool.execute(`error-${n}`, answerArgs));
			h.branch.push(toolResult(`error-${n}`, { isError: true, content: [{ type: "text", text: "重复回答" }] }));
			starts.push({ id, args, initial, final, fork, answerArgs });
		}
		await h.handlers.get("session_tree")({}, h.ctx);
		for (const [n, item] of starts.entries()) {
			const row = new Component("worker", item.id, item.args, {}, tool, { requestRender() {} }, process.cwd());
			// Historical loader does NOT call setArgsComplete().
			row.updateResult(item.initial, false);
			const text = stripTerminalSequences(row.render(160).join("\n"));
			assert.match(text, new RegExp(`reply-${n}`)); assert.doesNotMatch(text, /历史快照|Batch |已结束/); assert.match(text, /控制错误/);
			const followup = new Component("worker", `answer-${n}`, item.answerArgs, {}, tool, { requestRender() {} }, process.cwd());
			followup.updateResult(item.final, false); assert.deepEqual(followup.render(160), []);
		}
		assert.equal(executions, 2);
		h.branch.splice(0, h.branch.length, ...starts[0].fork);
		await h.handlers.get("session_tree")({}, h.ctx);
		const item = starts[0];
		const isolated = new Component("worker", item.id, item.args, {}, tool, { requestRender() {} }, process.cwd());
		isolated.updateResult(item.initial, false);
		const text = stripTerminalSequences(isolated.render(160).join("\n"));
		assert.match(text, /等待回答/); assert.doesNotMatch(text, /reply-0|控制错误/);
		await assert.rejects(tool.execute("stale", { batchId: item.initial.details.batchId }), /过期/);
		assert.equal(executions, 2);
	} finally { await h.handlers.get("session_shutdown")({}); }
});
