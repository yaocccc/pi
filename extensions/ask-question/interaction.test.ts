import assert from "node:assert/strict";
import test from "node:test";
import { EventEmitter } from "node:events";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { Container } from "@earendil-works/pi-tui";
import askQuestion from "./index.ts";
import workerExtension from "../worker/index.ts";
import { DEFAULT_OPTIONS } from "../worker/config.ts";

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
	return { pi, ctx, tools, components, call, event, surface, editor, input: (key: string) => focused.handleInput(key), setUiMode: (mode: typeof uiMode) => { uiMode = mode; }, opens: () => opens, closes: () => closes };
}

for (const [kind, input] of [["single", single], ["questionnaire", multi]] as const) {
	test(`${kind}: signal/pre-abort, Esc, exception, unsupported custom, noUI/RPC and shutdown release UI without consent`, async () => {
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
				if (kind === "questionnaire") h.input("\r"); // An unsubmitted draft is not consent.
				h.input("\x1b");
			}
			if (exit === "shutdown" || exit === "tree") await h.event(exit === "tree" ? "session_tree" : "session_shutdown");
			await assertion;
			if (exit === "no-ui" || exit === "rpc" || exit === "pre-abort") assert.equal(h.opens(), 0);
			assert.ok(h.closes() <= 1, "abort and UI completion cannot dispose custom twice");
			assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
			// Every exit also releases the queue for a subsequent interaction.
			h.ctx.hasUI = true; h.ctx.mode = "tui"; h.setUiMode("normal");
			const next = h.call(single); await flush(); h.input("\r");
			assert.equal((await next).details.answer, "A");
		}
	});

	test(`${kind}: UI opens directly and submits the selected answers`, async () => {
		const h = harness();
		const call = h.call(input);
		await flush(); assert.equal(h.opens(), 1);
		h.input("\r");
		if (kind === "questionnaire") { h.input("\r"); h.input("\r"); }
		const result = await call;
		if (kind === "single") assert.equal(result.details.answer, "A");
		else assert.deepEqual(result.details.questions.map((q: any) => q.answer), ["A", "C"]);
		assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
	});
}

test("parallel calls serialize the real Pi editor in FIFO order", async () => {
	const h = harness();
	const one = h.call(single); const two = h.call(multi);
	await flush(); assert.equal(h.components.length, 1);
	assert.deepEqual(h.surface.editorContainer.children, [h.components[0]]);
	h.input("\r"); assert.equal((await one).details.answer, "A");
	await flush(); assert.equal(h.components.length, 2);
	assert.deepEqual(h.surface.editorContainer.children, [h.components[1]]);
	h.input("\r"); h.input("\r"); h.input("\r");
	assert.deepEqual((await two).details.questions.map((q: any) => q.answer), ["A", "C"]);
	assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
});

test("queued abort/pre-abort immediately rejects without letting a successor overtake the active editor", async () => {
	const h = harness(); const queued = new AbortController(); const pre = new AbortController(); pre.abort();
	const one = h.call(single);
	const two = h.call(single, queued.signal); const rejected = assert.rejects(two);
	await assert.rejects(h.call(single, pre.signal));
	const three = h.call(multi);
	await flush(); queued.abort(); await rejected; await flush();
	assert.equal(h.opens(), 1);
	h.input("\x1b"); await one; await flush();
	assert.equal(h.opens(), 2); assert.equal(h.surface.editorContainer.children[0], h.components[1]);
	h.input("\x1b"); await three;
	assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
});

for (const event of ["session_shutdown", "session_tree", "session_start"] as const) {
	test(`${event} cancels active and all queued questionnaires without orphan promises`, async () => {
		const h = harness();
		const calls = [h.call(single), h.call(multi), h.call(single)];
		const rejected = calls.map((call) => assert.rejects(call, /关闭/));
		await flush(); assert.equal(h.opens(), 1);
		await h.event(event); await Promise.all(rejected); await flush();
		assert.equal(h.opens(), 1); assert.equal(h.closes(), 1);
		assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
		const next = h.call(single); await flush(); h.input("\r"); await next;
		assert.equal(h.opens(), 2, "new session may interact normally");
	});
}

test("a shared invocation abort cancels the active editor and all queued calls", async () => {
	const h = harness(); const controller = new AbortController();
	const calls = [h.call(single, controller.signal), h.call(multi, controller.signal)];
	const rejected = calls.map((call) => assert.rejects(call));
	await flush(); controller.abort(); await Promise.all(rejected); await flush();
	assert.equal(h.opens(), 1); assert.equal(h.closes(), 1);
	assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
});

for (const expires of ["question", "task", "neither"] as const) {
	test(`registered ask_question keeps ordinary deadlines and explicit parent replies (${expires} expires)`, async (t) => {
		t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
		const h = harness();
		const oldDepth = process.env.PI_WORKER_DEPTH;
		process.env.PI_WORKER_DEPTH = "0";
		let received: string | undefined; let independent = false;
		const preset = { model: "fixture/local", thinking: "high" as const };
		try {
			workerExtension(h.pi as any, { loadRoutingConfig: () => ({ config: { ...DEFAULT_OPTIONS, defaultTimeoutMs: expires === "task" ? 100 : 300, fast: preset, normal: preset, deep: preset, max: preset }, warnings: [], path: "test" }),
				executeTask: async (task, _config, _warnings, _ctx, signal, _progress, _overlap, ask) => {
					if (task.objective === "ask") received = await ask!({ id: "human-q", question: "Need approval", timeoutMs: expires === "question" ? 100 : 300 }, signal!);
					else independent = true;
					return { status: "completed" };
				},
			});
		} finally { if (oldDepth === undefined) delete process.env.PI_WORKER_DEPTH; else process.env.PI_WORKER_DEPTH = oldDepth; }
		await h.event("session_start");
		const worker = h.tools.get("worker");
		try {
			const first = await worker.execute("origin", { tasks: [{ mode: "scout", objective: "ask" }, { mode: "scout", objective: "independent" }] }, undefined, undefined, h.ctx);
			const human = h.call(single); const queued = h.call(multi);
			await flush(); assert.equal(h.opens(), 1);
			t.mock.timers.tick(100); await flush();
			assert.equal(independent, true); assert.equal(received, undefined);
			const state = await worker.execute("wait", { batchId: first.details.batchId });
			assert.equal(state.details.completed, expires === "neither" ? 1 : 2);
			assert.equal(state.details.questions[0].status, expires === "neither" ? "waiting" : expires === "question" ? "expired" : "cancelled");
			if (expires === "task") assert.equal(state.details.tasks[0].result.failure.category, "timeout");
			assert.equal(h.closes(), 0, "ordinary Worker expiry does not fabricate or close a user answer");
			h.input("\r"); assert.equal((await human).details.answer, "A");
			await flush(); h.input("\x1b"); await queued;
			assert.equal(received, undefined, "only the real parent Agent may answer the Worker");
			const q = first.details.questions[0];
			const reply = worker.execute("reply", { batchId: first.details.batchId, answers: [{ taskId: q.taskId, questionId: q.id, answer: "parent chooses A" }] });
			if (expires === "neither") {
				const result = await reply;
				assert.equal(result.details.completed, 2);
				assert.equal(received, "parent chooses A");
			} else await assert.rejects(reply, /过期|取消/);
		} finally { await h.event("session_shutdown"); t.mock.timers.reset(); }
	});
}

// No separate UI waiting limit: lifetime is owned by completion, signal and session.
test("elapsed wall time does not impose a separate questionnaire or queue timeout", async (t) => {
	t.mock.timers.enable({ apis: ["Date", "setTimeout"], now: 1_000 });
	const h = harness(); const first = h.call(single); const second = h.call(multi);
	await flush(); t.mock.timers.tick(24 * 60 * 60_000); await flush();
	assert.equal(h.opens(), 1); assert.equal(h.closes(), 0);
	h.input("\r"); assert.equal((await first).details.answer, "A");
	await flush(); assert.equal(h.opens(), 2);
	h.input("\x1b"); await second;
	assert.deepEqual(h.surface.editorContainer.children, [h.editor]);
});
