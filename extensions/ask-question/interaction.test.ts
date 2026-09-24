import assert from "node:assert/strict";
import test from "node:test";
import { EventEmitter } from "node:events";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { Container } from "@earendil-works/pi-tui";
import { setTimeout as delay } from "node:timers/promises";
import askQuestion from "./index.ts";
import workerExtension from "../worker/index.ts";
import { DEFAULT_OPTIONS } from "../worker/config.ts";
import { HUMAN_WAIT_LIMIT_MS, INTERACTION_EVENT, withHumanInteraction, type InteractionEvent } from "../shared/interaction-lifecycle.ts";

// Use installed Pi's real editor-replacing custom lifecycle, not concurrent UI mocks.
const piRoot = dirname(fileURLToPath(import.meta.resolve("@earendil-works/pi-coding-agent")));
const { InteractiveMode } = await import(pathToFileURL(join(piRoot, "modes/interactive/interactive-mode.js")).href);
const { initTheme } = await import(pathToFileURL(join(piRoot, "modes/interactive/theme/theme.js")).href);
initTheme("dark", false);
const single = { question: "Choose?", options: ["A", "B"] };
const multi = { questions: [{ question: "One?", options: ["A", "B"] }, { question: "Two?", options: ["C", "D"] }] };
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
function harness() {
	const tools = new Map<string, any>();
	const handlers = new Map<string, Array<(...args: any[]) => any>>();
	const events = new EventEmitter();
	const lifecycle: InteractionEvent[] = [];
	events.on(INTERACTION_EVENT, (event) => lifecycle.push(event));
	const pi = { registerTool: (tool: any) => tools.set(tool.name, tool), registerCommand() {},
		on: (name: string, handler: any) => handlers.set(name, [...handlers.get(name) ?? [], handler]), events };
	askQuestion(pi as any);
	const components: any[] = [];
	let uiMode: "normal" | "throw" | "undefined" = "normal";
	let opens = 0; let closes = 0;
	let focused: any;
	const editor = { getText: () => "draft", setText() {}, render: () => [], invalidate() {} };
	const surface = { editor, editorContainer: new Container(), keybindings: {}, disposeActiveSelector() {},
		ui: { requestRender() {}, setFocus: (component: any) => { focused = component; }, terminal: { rows: 40, columns: 100 } } };
	surface.editorContainer.addChild(editor);
	const ctx = { cwd: process.cwd(), hasUI: true, mode: "tui", sessionManager: { getBranch: () => [] }, ui: {
		custom: (factory: any) => {
			opens++;
			if (uiMode === "throw") throw new Error("UI failed");
			if (uiMode === "undefined") return undefined;
			return InteractiveMode.prototype.showExtensionCustom.call(surface, (tui: any, theme: any, keys: any, done: any) => {
				const component = factory(tui, theme, keys, (value: unknown) => { closes++; done(value); });
				components.push(component);
				return component;
			});
		},
	} };
	const call = (params: any, signal?: AbortSignal) => tools.get("ask_question").execute("human", params, signal, undefined, ctx);
	const event = async (name: string) => { for (const fn of handlers.get(name) ?? []) await fn({}, ctx); };
	return { pi, ctx, tools, events, lifecycle, components, call, event, surface, editor, input: (key: string) => focused.handleInput(key), setUiMode: (mode: typeof uiMode) => { uiMode = mode; }, opens: () => opens, closes: () => closes };
}

for (const [kind, input] of [["single", single], ["questionnaire", multi]] as const) {
	test(`${kind}: signal/pre-abort, Esc, exception, unsupported custom, noUI/RPC and shutdown always release lifecycle without consent`, async () => {
		for (const exit of ["abort", "pre-abort", "escape", "exception", "undefined", "no-ui", "rpc", "shutdown", "tree"] as const) {
			const h = harness(); const controller = new AbortController();
			if (exit === "pre-abort") controller.abort(new Error("pre aborted"));
			if (exit === "exception") h.setUiMode("throw");
			if (exit === "undefined") h.setUiMode("undefined");
			if (exit === "no-ui") h.ctx.hasUI = false;
			if (exit === "rpc") h.ctx.mode = "rpc";
			const call = h.call(input, controller.signal);
			const fails = ["abort", "pre-abort", "exception", "shutdown", "tree"].includes(exit);
			const assertion = fails ? assert.rejects(call) : call.then((result: any) => {
				if (kind === "single") assert.equal(result.details.answer, null);
				else assert.ok(result.details.questions.every((q: any) => q.answer === null));
			});
			await flush();
			if (exit === "abort") controller.abort(new Error("abort"));
			if (exit === "escape") {
				if (kind === "questionnaire") h.components[0].handleInput("\r"); // An unsubmitted draft is not consent.
				h.components[0].handleInput("\x1b");
			}
			if (exit === "shutdown" || exit === "tree") await h.event(exit === "tree" ? "session_tree" : "session_shutdown");
			await assertion;
			if (exit === "no-ui" || exit === "rpc" || exit === "pre-abort") { assert.equal(h.opens(), 0); assert.equal(h.lifecycle.length, 0); }
			else {
				assert.equal(h.lifecycle.at(-1)?.phase, "end");
				assert.equal(h.lifecycle.filter((e) => e.phase === "begin").length, 1);
			}
			assert.ok(h.closes() <= 1, "abort and UI completion cannot dispose custom twice");
		}
	});

	test(`${kind}: actual UI waits for pause acknowledgements and normal submission releases token`, async () => {
		const h = harness(); let acknowledge!: () => void;
		const ack = new Promise<void>((resolve) => { acknowledge = resolve; });
		h.events.on(INTERACTION_EVENT, (event: InteractionEvent) => { if (event.phase === "begin") event.waitUntil(ack); });
		const call = h.call(input);
		await flush(); assert.equal(h.opens(), 0);
		acknowledge(); await flush(); assert.equal(h.opens(), 1);
		h.components[0].handleInput("\r");
		if (kind === "questionnaire") { h.components[0].handleInput("\r"); h.components[0].handleInput("\r"); }
		const result = await call;
		if (kind === "single") assert.equal(result.details.answer, "A");
		else assert.deepEqual(result.details.questions.map((q: any) => q.answer), ["A", "C"]);
		assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
		assert.equal(h.lifecycle[0].token, h.lifecycle[1].token);
	});

	test(`${kind}: bounded human cap aborts custom instead of returning default approval`, async (t) => {
		t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
		const h = harness();
		const call = h.call(input); const rejected = assert.rejects(call, /上限/);
		await flush(); assert.equal(h.opens(), 1);
		t.mock.timers.tick(HUMAN_WAIT_LIMIT_MS); await rejected;
		assert.equal(h.closes(), 1);
		assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
		t.mock.timers.reset();
	});
}

test("parallel human calls serialize the real Pi editor and a failed preparation still emits finally end", async () => {
	const h = harness();
	const one = h.call(single); const two = h.call(multi);
	await flush(); assert.equal(h.components.length, 1);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin"], "queued UI has no pause token");
	assert.deepEqual(h.surface.editorContainer.children, [h.components[0]]);
	h.input("\r"); assert.equal((await one).details.answer, "A");
	await flush(); assert.equal(h.components.length, 2);
	assert.deepEqual(h.surface.editorContainer.children, [h.components[1]]);
	h.input("\r"); h.input("\r"); h.input("\r");
	assert.deepEqual((await two).details.questions.map((q: any) => q.answer), ["A", "C"]);
	assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end", "begin", "end"]);
	assert.equal(new Set(h.lifecycle.filter((e) => e.phase === "begin").map((e) => e.token)).size, 2);
	const failed = harness();
	failed.events.on(INTERACTION_EVENT, (event: InteractionEvent) => { if (event.phase === "begin") event.waitUntil(Promise.reject(new Error("pause unavailable"))); });
	await assert.rejects(failed.call(single), /pause unavailable/);
	assert.equal(failed.opens(), 0);
	assert.deepEqual(failed.lifecycle.map((e) => e.phase), ["begin", "end"]);
});

test("queued abort/pre-abort never opens or lets a successor overtake the active Pi editor", async () => {
	const h = harness(); const queued = new AbortController(); const pre = new AbortController(); pre.abort();
	const one = h.call(single);
	const two = h.call(single, queued.signal); const rejected = assert.rejects(two);
	await assert.rejects(h.call(single, pre.signal));
	const three = h.call(multi);
	await flush(); queued.abort(); await rejected; await flush();
	assert.equal(h.opens(), 1); assert.equal(h.lifecycle.length, 1);
	h.input("\x1b"); await one; await flush();
	assert.equal(h.opens(), 2); assert.equal(h.surface.editorContainer.children[0], h.components[1]);
	h.input("\x1b"); await three;
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end", "begin", "end"]);
});

for (const event of ["session_shutdown", "session_tree", "session_start"] as const) {
	test(`${event} cancels active and all queued questionnaires without orphan promises/tokens`, async () => {
		const h = harness();
		const calls = [h.call(single), h.call(multi), h.call(single)];
		const rejected = calls.map((call) => assert.rejects(call, /关闭/));
		await flush(); assert.equal(h.opens(), 1);
		await h.event(event); await Promise.all(rejected); await flush();
		assert.equal(h.opens(), 1); assert.equal(h.closes(), 1);
		assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
		assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
		const next = h.call(single); await flush(); h.input("\r"); await next;
		assert.equal(h.opens(), 2, "new session may interact normally");
	});
}

test("queue deadline cannot be renewed when the previous UI closes before overdue timers run", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const h = harness(); const first = h.call(single); const firstRejected = assert.rejects(first, /上限/);
	const second = h.call(multi); const secondRejected = assert.rejects(second, /上限/);
	await flush(); t.mock.timers.setTime(1_000 + HUMAN_WAIT_LIMIT_MS);
	h.input("\r"); await firstRejected; await secondRejected;
	assert.equal(h.opens(), 1);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
});

test("a shared invocation abort cancels the active Pi editor and all queued calls", async () => {
	const h = harness(); const controller = new AbortController();
	const calls = [h.call(single, controller.signal), h.call(multi, controller.signal)];
	const rejected = calls.map((call) => assert.rejects(call));
	await flush(); controller.abort(); await Promise.all(rejected); await flush();
	assert.equal(h.opens(), 1); assert.equal(h.closes(), 1);
	assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
});

test("a queued questionnaire inherits only its remaining cap after the previous UI completes", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const h = harness(); const first = h.call(single); const second = h.call(multi);
	const rejected = assert.rejects(second, /上限/);
	await flush(); t.mock.timers.tick(HUMAN_WAIT_LIMIT_MS - 100);
	h.input("\r"); await first; await flush();
	assert.equal(h.opens(), 2);
	const begins = h.lifecycle.filter((e) => e.phase === "begin");
	assert.equal(begins[1].deadline, begins[0].deadline);
	t.mock.timers.tick(100); await rejected;
	assert.equal(h.closes(), 2); assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end", "begin", "end"]);
});

test("expired pause preparation cannot open a UI before its timeout callback runs", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const h = harness(); let acknowledge!: () => void;
	const gate = new Promise<void>((resolve) => { acknowledge = resolve; });
	h.events.on(INTERACTION_EVENT, (event: InteractionEvent) => { if (event.phase === "begin") event.waitUntil(gate); });
	const call = h.call(single); const rejected = assert.rejects(call, /上限/);
	await flush(); t.mock.timers.setTime(1_000 + HUMAN_WAIT_LIMIT_MS); acknowledge();
	await rejected; assert.equal(h.opens(), 0);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
});

test("queued cancellation is immediate even while active pause preparation is pending", async () => {
	const h = harness(); let acknowledge!: () => void;
	const gate = new Promise<void>((resolve) => { acknowledge = resolve; });
	h.events.on(INTERACTION_EVENT, (event: InteractionEvent) => { if (event.phase === "begin") event.waitUntil(gate); });
	const first = h.call(single); const controller = new AbortController();
	const second = h.call(multi, controller.signal); const rejected = assert.rejects(second);
	await flush(); controller.abort(); await rejected; assert.equal(h.opens(), 0);
	acknowledge(); await flush(); h.input("\x1b"); await first;
	assert.equal(h.opens(), 1);
	assert.deepEqual(h.lifecycle.map((e) => e.phase), ["begin", "end"]);
});

test("shared lifecycle integrates registered ask_question and worker: human delay pauses budgets but never auto-answers", async () => {
	const h = harness();
	const oldDepth = process.env.PI_WORKER_DEPTH;
	process.env.PI_WORKER_DEPTH = "0";
	let received: string | undefined; let independent = false;
	const preset = { model: "fixture/local", thinking: "high" as const };
	try {
		workerExtension(h.pi as any, { loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, defaultTimeoutMs: 300, fast: preset, normal: preset, deep: preset, max: preset }, warnings: [], path: "test" }),
			executeTask: async (task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
				if (task.objective === "ask") received = await ask!({ id: "human-q", question: "Need approval", timeoutMs: 200 }, signal!);
				else { await delay(40); independent = true; }
				return { status: "completed" };
			},
		});
	} finally { if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth; }
	await h.event("session_start");
	const worker = h.tools.get("worker");
	try {
		const first = await worker.execute("origin", { tasks: [{ mode: "scout", objective: "ask" }, { mode: "scout", objective: "independent" }] }, undefined, undefined, h.ctx);
		const human = h.call(single); await flush(); assert.equal(h.opens(), 1);
		await delay(450);
		assert.equal(independent, true); assert.equal(received, undefined);
		const waiting = await worker.execute("wait", { batchId: first.details.batchId });
		assert.ok(waiting.details.questions[0].pausedUntil);
		h.components[0].handleInput("\r");
		assert.equal((await human).details.answer, "A");
		assert.equal(received, undefined, "only the real parent Agent is authorized to answer the Worker");
		const q = first.details.questions[0];
		const result = await worker.execute("reply", { batchId: first.details.batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: "Parent chooses A within existing permissions" }] });
		assert.equal(received, "Parent chooses A within existing permissions");
		assert.equal(result.details.completed, 2);
	} finally { await h.event("session_shutdown"); }
});

test("lifecycle rejects a response at deadline even before the timeout callback runs", async (t) => {
	t.mock.timers.enable({ apis: ["Date"], now: 1_000 });
	const phases: string[] = [];
	await assert.rejects(withHumanInteraction({ emit(_name, value) { phases.push((value as InteractionEvent).phase); } }, undefined, async () => {
		t.mock.timers.setTime(1_100); return "not consent";
	}, 100), /上限/);
	assert.deepEqual(phases, ["begin", "end"]); t.mock.timers.reset();
});
