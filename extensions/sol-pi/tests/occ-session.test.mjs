import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createAgentSession, DefaultResourceLoader, SessionManager, SettingsManager, VERSION } from "@earendil-works/pi-coding-agent";
import { createProvider, getCurrentSystemPrompt, getCurrentTools } from "@earendil-works/pi-ai";
import { createFauxCore, fauxAssistantMessage, fauxToolCall, fauxText } from "@earendil-works/pi-ai/providers/faux";
import { createOnlineContextCompactExtension, POST_COMPACTION_PLAN_REMINDER } from "../extensions/online-context-compact/extension.ts";
import { initialOnlineState, ONLINE_STATE_ENTRY, restoreOnlineState } from "../extensions/online-context-compact/state.ts";
import { prepareCompaction } from "../extensions/online-context-compact/native-preparation.ts";

const plan = (done = false, final = false) => [
  { id: "build", goal: "build it", status: done ? "completed" : "in_progress" },
  { id: "verify", goal: "verify it", status: final ? "completed" : "pending" },
];
const call = (id, steps, text = "") => fauxAssistantMessage([
  ...(text ? [fauxText(text)] : []), fauxToolCall("update_plan", { steps }, { id }),
], { stopReason: "toolUse" });

async function setup(t, options = {}) {
  assert.equal(VERSION, "0.87.1");
  const cwd = await mkdtemp(join(tmpdir(), "occ-session-"));
  const agentDir = join(cwd, "agent");
  await mkdir(agentDir);
  const settings = SettingsManager.inMemory({
    compaction: { enabled: options.enabled ?? true, keepRecentTokens: 150, reserveTokens: 1024 },
    retry: { enabled: options.retry ?? false, maxRetries: 1, baseDelayMs: 1 }, cacheWarming: options.warming ? "idle" : "off",
  }, { projectTrusted: false });
  const manager = options.manager ?? (options.ephemeral ? SessionManager.inMemory(cwd) : SessionManager.create(cwd, join(cwd, "sessions")));
  if (!options.manager) {
    manager.appendMessage({ role: "user", content: "history " + "x".repeat(16000), timestamp: 1 });
    manager.appendMessage(fauxAssistantMessage("prior work " + "y".repeat(16000)));
  }
  const core = createFauxCore({ provider: `occ-${Math.random()}`, api: `occ-api-${Math.random()}`,
    models: [{ id: "offline", contextWindow: 1_000_000, maxTokens: 4096 }] });
  if (options.warming) core.getModel().promptCache = { short: 300, long: 3600 };
  const faux = { ...core, provider: createProvider({ id: core.provider, models: core.models,
    auth: { apiKey: { name: "Offline dummy auth", resolve: async () => ({ auth: { apiKey: "offline-test-key" } }) } },
    api: { stream: core.stream, streamSimple: core.streamSimple, fetchDeferred: core.fetchDeferred, cancelDeferred: core.cancelDeferred },
  }) };
  const counts = { main: 0, summary: 0, warm: 0, turns: 0, settled: 0, before: 0, compactHook: 0, requests: [], summaryRequests: [], errors: [] };
  let script = options.script ?? [call("open", plan()), call("boundary", plan(true)), fauxAssistantMessage("FINAL")];
  let session;
  faux.setResponses(Array.from({ length: 40 }, () => async (context, request) => {
    assert.equal(request.apiKey, "offline-test-key", "main and summary calls retain registry request-time authentication");
    const leaf = manager.getLeafId();
    await request?.onPayload?.({ messages: context.messages }, faux.getModel());
    if (options.warming && request.maxTokens === 1) {
      assert.equal(manager.getLeafId(), leaf, "replayed onPayload must not append OCC state or move the leaf");
      counts.warm++;
      return fauxAssistantMessage("warm");
    }
    if (getCurrentSystemPrompt(context.messages).includes("context summarization assistant")) {
      counts.summary++;
      counts.summaryRequests.push(context);
      assert.equal(request.cacheRetention, "none");
      assert.ok(request.signal);
      return options.summary ? options.summary(context, request, counts, () => session) : fauxAssistantMessage("Checkpoint: work done; verification remains.");
    }
    counts.main++;
    counts.requests.push(context);
    if (!script.length) throw new Error("Unexpected extra main request");
    const next = script.shift();
    return typeof next === "function" ? next(context, request) : next;
  }));
  const factory = pi => {
    pi.registerProvider(faux.provider);
    options.before?.(pi, manager);
    createOnlineContextCompactExtension({ cacheWriteReadRatio: options.ratio === "default" ? undefined : (options.ratio ?? 0),
      resolveSettings: ctx => ({ compaction: settings.getCompactionSettings(ctx.model), retry: settings.getRetrySettings() }),
    })(pi);
    pi.on("before_provider_request", () => { counts.before++; });
    pi.on("turn_start", () => { counts.turns++; });
    if (options.warming) pi.on("cache_warming_decision", () => ({ action: "warm" }));
    pi.on("session_before_compact", () => { counts.compactHook++; });
    options.after?.(pi, manager);
  };
  const loader = new DefaultResourceLoader({ cwd, agentDir, settingsManager: settings,
    extensionFactories: [{ name: "occ-offline", factory }], noExtensions: true, noSkills: true,
    noPromptTemplates: true, noThemes: true, noContextFiles: true,
    systemPrompt: "Deterministic offline lifecycle test.",
  });
  await loader.reload();
  assert.deepEqual(loader.getExtensions().errors, []);
  ({ session } = await createAgentSession({ cwd, agentDir, model: faux.getModel(), thinkingLevel: "off",
    tools: Number(process.env.PI_WORKER_DEPTH ?? 0) > 0 ? [] : ["update_plan"], resourceLoader: loader, sessionManager: manager, settingsManager: settings }));
  await session.bindExtensions({ onError: error => counts.errors.push(error) });
  session.subscribe(event => { if (event.type === "agent_settled") counts.settled++; });
  t.after(async () => { session.dispose(); await rm(cwd, { recursive: true, force: true }); });
  return { session, manager, counts, settings, faux, run: () => session.prompt("Finish the task", { expandPromptTemplates: false }) };
}

// Pi 0.87.1 test-only access: dispatch its real scheduled refresh immediately,
// without sleeps, remote providers, or mocking away the SDK's replayed onPayload.
async function warmNow(session, manager, counts) {
  const warmer = session._cacheWarmer;
  assert.equal(warmer.constructor.name, "CacheWarmer");
  const run = warmer.run;
  assert.ok(run?.timer, "native SDK must have scheduled a replayable request");
  assert.equal(run.isCurrent(), true);
  const branch = manager.getBranch();
  const state = restoreOnlineState(branch);
  const before = { ...counts };
  clearTimeout(run.timer);
  await warmer.refresh(run);
  assert.equal(counts.warm, before.warm + 1, "refresh traversed the real provider onPayload callback");
  assert.equal(counts.before, before.before + 1, "replay emitted before_provider_request");
  assert.equal(counts.turns, before.turns, "warming emits no main turn_start");
  assert.deepEqual(restoreOnlineState(manager.getBranch()), state, "warming cannot increment horizon or repay debt");
  // Native usage accounting legitimately advances the raw leaf, but no OCC entry is appended.
  assert.deepEqual(manager.getBranch().slice(0, branch.length), branch);
  const appended = manager.getBranch().slice(branch.length);
  assert.equal(appended.length, 1);
  assert.equal(appended[0].type, "usage");
  assert.equal(appended[0].kind, "cache_warm");
}

test("native CacheWarmer: idle and streaming replays leave OCC counters/debt untouched; real turns count", async t => {
  let h;
  h = await setup(t, { warming: true,
    before: (_pi, manager) => manager.appendCustomEntry(ONLINE_STATE_ENTRY, {
      ...initialOnlineState(), cacheDebtTokens: 1000, cacheDebtRepaymentTokens: 100,
    }),
    script: [async () => {
      assert.equal(h.session.isStreaming, true);
      assert.equal(restoreOnlineState(h.manager.getBranch()).requestCount, 1);
      await warmNow(h.session, h.manager, h.counts);
      await warmNow(h.session, h.manager, h.counts);
      return fauxAssistantMessage("FIRST");
    }, fauxAssistantMessage("SECOND")],
  });
  await h.run();
  assert.equal(h.session.getLastAssistantText(), "FIRST");
  assert.equal(h.session.isStreaming, false);
  assert.equal(h.session._cacheWarmer.run.phase, "idle");
  await warmNow(h.session, h.manager, h.counts);
  await warmNow(h.session, h.manager, h.counts);
  let state = restoreOnlineState(h.manager.getBranch());
  assert.equal(state.requestCount, 1);
  assert.equal(state.cacheDebtTokens, 900);
  await h.run();
  state = restoreOnlineState(h.manager.getBranch());
  assert.equal(state.requestCount, 2);
  assert.equal(state.cacheDebtTokens, 800);
  assert.equal(h.counts.main, 2);
  assert.equal(h.counts.turns, 2);
  assert.equal(h.counts.warm, 4);
  assert.equal(h.settings.getCacheWarmingMode(), "idle");
  assert.deepEqual(h.counts.errors, []);
});

test("native CacheWarmer during OCC summaries preserves valid commit and counts the main continuation", async t => {
  let h;
  h = await setup(t, { warming: true, summary: async () => {
    assert.equal(h.session.isStreaming, true, "isStreaming cannot distinguish summary-time warming");
    await warmNow(h.session, h.manager, h.counts);
    return fauxAssistantMessage("Checkpoint survives native warming usage entries.");
  } });
  await h.run();
  assert.ok(h.counts.summary > 0);
  assert.equal(h.counts.warm, h.counts.summary);
  assert.equal(h.counts.main, 3);
  assert.equal(h.counts.turns, 3);
  assert.equal(h.counts.before, h.counts.main + h.counts.warm);
  assert.equal(h.counts.settled, 1);
  assert.equal(h.session.getLastAssistantText(), "FINAL");
  const branch = h.manager.getBranch();
  assert.equal(branch.filter(e => e.type === "compaction").length, 1);
  assert.equal(branch.filter(e => e.customType === "sol-pi-online-context-compact").length, 1);
  const state = restoreOnlineState(branch);
  assert.equal(state.requestCount, 3);
  assert.equal(state.nativeCompactionCount, 1);
  assert.deepEqual(h.counts.errors, []);
});

test("native warming usage cannot hide an unrelated leaf append during an OCC summary", async t => {
  let h;
  h = await setup(t, { warming: true, summary: async () => {
    h.manager.appendCustomEntry("unrelated-concurrent-edit", {});
    await warmNow(h.session, h.manager, h.counts);
    return fauxAssistantMessage("Obsolete checkpoint");
  } });
  await h.run();
  assert.equal(h.counts.summary, 1);
  assert.equal(h.counts.warm, 1);
  assert.equal(h.counts.main, 3);
  assert.equal(h.manager.getBranch().filter(e => e.type === "compaction").length, 0);
  assert.equal(restoreOnlineState(h.manager.getBranch()).nativeCompactionCount, 0);
  assert.deepEqual(h.counts.errors, []);
});

for (const n of [1, 2]) test(`real Pi: ${n} OCC boundary compactions finish naturally inside original prompt`, async t => {
  const script = [];
  for (let i = 0; i < n; i++) script.push(call(`open-${i}`, plan(), i ? "More phase work " + "z".repeat(16000) : ""), call(`done-${i}`, plan(true)));
  script.push(call("final-plan", plan(true, true)), fauxAssistantMessage("FINAL"));
  const { session, manager, counts, run } = await setup(t, { script });
  await run();
  assert.equal(session.getLastAssistantText(), "FINAL");
  assert.equal(session.isIdle, true);
  assert.equal(counts.main, 2 * n + 2, "no extra model round from continue=true");
  assert.equal(counts.before, counts.main, "direct summaries must not count as main requests");
  assert.equal(counts.settled, 1, "no stop/restart settlement loop");
  assert.equal(counts.compactHook, 0, "draft commits bypass native hooks");
  assert.ok(counts.summary >= n);
  assert.deepEqual(counts.errors, []);
  const branch = manager.getBranch();
  assert.equal(branch.filter(e => e.type === "compaction").length, n);
  const reminders = branch.filter(e => e.type === "custom_message" && e.customType === "sol-pi-online-context-compact");
  assert.equal(reminders.length, n);
  for (const reminder of reminders) { assert.equal(reminder.display, false); assert.ok(reminder.content.startsWith(POST_COMPACTION_PLAN_REMINDER)); }
  const state = restoreOnlineState(branch);
  assert.equal(state.nativeCompactionCount, n);
  assert.equal(state.requestCount, counts.main);
  assert.ok(state.plan.every(step => step.status === "completed"));
  for (const [i, e] of branch.entries()) if (e.type === "compaction") {
    assert.equal(branch[i + 1].customType, ONLINE_STATE_ENTRY);
    assert.equal(branch[i + 2].customType, "sol-pi-online-context-compact");
    assert.ok(e.usage);
  }
  if (n === 2) assert.ok(counts.summaryRequests.some(c => JSON.stringify(c).includes("previous-summary")), "iterative summary includes previous checkpoint");
});

for (const reason of ["error", "aborted", "deferred", "length", "empty", "toolCall"]) test(`summary ${reason}: no compaction, state commit, or ghost continuation`, async t => {
  const { run, manager, counts, session } = await setup(t, { summary: () => {
    if (reason === "empty") return fauxAssistantMessage("   ");
    if (reason === "toolCall") return fauxAssistantMessage(fauxToolCall("read", { path: "never" }), { stopReason: "toolUse" });
    return fauxAssistantMessage(reason === "error" ? "" : "partial unsafe checkpoint", { stopReason: reason, errorMessage: "fatal summary failure" });
  } });
  await run();
  assert.equal(session.getLastAssistantText(), "FINAL");
  assert.equal(counts.main, 3);
  assert.equal(counts.settled, 1);
  assert.ok(counts.summary > 0, "failure path must actually attempt summary");
  assert.equal(manager.getBranch().filter(e => e.type === "compaction" || e.type === "custom_message").length, 0);
  assert.equal(restoreOnlineState(manager.getBranch()).nativeCompactionCount, 0);
  assert.deepEqual(counts.errors, []);
});

test("native summary retry handles error response without extra main request", async t => {
  const { run, manager, counts } = await setup(t, { retry: true, summary: (_c, _r, counts) => counts.summary === 1
    ? fauxAssistantMessage("", { stopReason: "error", errorMessage: "terminated" }) : fauxAssistantMessage("Valid checkpoint") });
  await run();
  assert.ok(counts.summary >= 2);
  assert.equal(counts.main, 3);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, 1);
});

test("user cancellation while summary awaits: original prompt settles, no commit or ghost", async t => {
  let started;
  const ready = new Promise(resolve => { started = resolve; });
  const { run, manager, counts, session } = await setup(t, { summary: async (_c, options) => {
    started();
    await new Promise(resolve => options.signal.addEventListener("abort", resolve, { once: true }));
    return fauxAssistantMessage("late summary must be discarded");
  } });
  const prompt = run();
  await ready;
  await Promise.all([session.abort(), prompt]);
  assert.equal(session.isIdle, true);
  assert.equal(counts.main, 2);
  assert.equal(counts.settled, 1);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction" || e.type === "custom_message").length, 0);
});

test("all-completed final plan is not a compaction boundary", async t => {
  const { run, counts, manager } = await setup(t, { script: [call("open", plan()), call("final", plan(true, true)), fauxAssistantMessage("FINAL")] });
  await run();
  assert.equal(counts.summary, 0);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, 0);
});

for (const enough of [true, false]) test(`default ratio 12.5 ${enough ? "compacts with a long horizon" : "defers with a short horizon"}`, async t => {
  const open = enough ? [...plan(), ...Array.from({ length: 20 }, (_, i) => ({ id: `task-${i}`, goal: `remaining task ${i}`, status: "pending" }))] : plan();
  const done = open.map((step, i) => i === 0 ? { ...step, status: "completed" } : step);
  const { run, manager, counts } = await setup(t, { ratio: "default", script: [call("open", open), call("boundary", done), fauxAssistantMessage("FINAL")] });
  await run();
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, enough ? 1 : 0);
  assert.equal(counts.main, 3);
});

for (const kind of ["ephemeral", "disabled", "worker"]) test(`${kind}: OCC unavailable and update_plan absent from provider tools`, async t => {
  const old = process.env.PI_WORKER_DEPTH;
  try {
    if (kind === "worker") process.env.PI_WORKER_DEPTH = "1";
    const { run, counts, session, manager } = await setup(t, { ephemeral: kind === "ephemeral", enabled: kind !== "disabled", script: [fauxAssistantMessage("FINAL")] });
    await run();
    assert.equal(counts.summary, 0);
    assert.ok(!session.getActiveToolNames().includes("update_plan"));
    assert.ok(!getCurrentTools(counts.requests[0].messages).some(tool => tool.name === "update_plan"));
    assert.equal(manager.getBranch().filter(e => e.type === "custom" && e.customType === ONLINE_STATE_ENTRY).length, 0);
  } finally { process.env.PI_WORKER_DEPTH = old; }
});

test("later extension drops drafts: reconcile real branch, no speculative epoch/debt", async t => {
  let proposals = 0;
  const { run, counts, manager } = await setup(t, { after: pi => pi.on("turn_end", event => {
    if (event.entries.some(e => e.type === "compaction")) proposals++;
    return { entries: [], continue: false };
  }) });
  await run();
  assert.equal(proposals, 1);
  assert.ok(counts.summary > 0);
  assert.equal(counts.main, 3);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, 0);
  const state = restoreOnlineState(manager.getBranch());
  assert.equal(state.nativeCompactionCount, 0);
  assert.equal(state.epoch, 0);
  assert.equal(state.requestCount, 3);
  assert.ok(state.plan.length > 0);
});

for (const kind of ["custom", "custom_message", "context_edit", "compaction"]) test(`earlier ${kind} draft: ${kind === "custom" ? "preserve metadata and compact" : "conservatively skip OCC"}`, async t => {
  const { run, manager, counts } = await setup(t, { before: pi => pi.on("turn_end", event => {
    if (!event.toolResults.some(r => r.toolCallId === "boundary")) return;
    const draft = kind === "custom" ? { type: kind, customType: "earlier", data: { kept: true } }
      : kind === "custom_message" ? { type: kind, customType: "earlier", content: "new input", display: false }
      : kind === "context_edit" ? { type: kind, targetId: event.messageEntryId, replacement: null }
      : { type: kind, summary: "Earlier extension checkpoint", firstKeptEntryId: event.messageEntryId };
    return { entries: [...event.entries, draft] };
  }) });
  await run();
  assert.equal(counts.main, 3);
  if (kind === "custom") {
    assert.ok(counts.summary > 0);
    assert.equal(manager.getBranch().filter(e => e.customType === "earlier").length, 1);
  } else assert.equal(counts.summary, 0);
});

test("native manual compaction refreshes state once; persisted state restores in a new AgentSession", async t => {
  const first = await setup(t);
  await first.run();
  assert.equal(restoreOnlineState(first.manager.getBranch()).nativeCompactionCount, 1);
  await first.session.compact();
  assert.equal(restoreOnlineState(first.manager.getBranch()).nativeCompactionCount, 2);
  first.session.dispose();
  const restored = SessionManager.open(first.manager.getSessionFile());
  const second = await setup(t, { manager: restored, script: [fauxAssistantMessage("restored FINAL")] });
  await second.run();
  const state = restoreOnlineState(restored.getBranch());
  assert.equal(state.nativeCompactionCount, 2);
  assert.equal(state.epoch, 2);
  assert.equal(state.requestCount, 4);
});

for (const kind of ["steering", "model", "leaf"]) test(`real session ${kind} change during summary invalidates pending compaction`, async t => {
  let started, finish;
  const ready = new Promise(resolve => { started = resolve; });
  const release = new Promise(resolve => { finish = resolve; });
  const { run, manager, session, counts, faux } = await setup(t, { summary: async () => {
    started(); await release; return fauxAssistantMessage("Late obsolete checkpoint");
  } });
  const prompt = run();
  await ready;
  if (kind === "steering") await session.steer("CORRECTION: stop the old plan and report");
  else if (kind === "model") await session.setModel({ ...faux.getModel(), name: "changed model snapshot" });
  else manager.appendCustomEntry("concurrent-leaf-change", {});
  finish();
  await prompt;
  assert.equal(counts.summary, 1);
  assert.equal(counts.main, 3);
  assert.equal(counts.settled, 1);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction" || e.type === "custom_message").length, 0);
  assert.equal(restoreOnlineState(manager.getBranch()).nativeCompactionCount, 0);
  if (kind === "steering") assert.deepEqual(restoreOnlineState(manager.getBranch()).plan, []);
});

for (const variant of ["split", "replacement", "omission", "previous", "retain-none"]) test(`preparation matches real Pi 0.87.1 native hook: ${variant}`, async t => {
  const manager = SessionManager.inMemory();
  const user = manager.appendMessage({ role: "user", content: "original input " + "x".repeat(8000), timestamp: 1 });
  const first = manager.appendMessage(fauxAssistantMessage(fauxToolCall("read", { path: "original.ts" }, { id: "read-1" }), { stopReason: "toolUse" }));
  manager.appendMessage({ role: "toolResult", toolCallId: "read-1", toolName: "read", content: [{ type: "text", text: "source " + "y".repeat(8000) }], isError: false, timestamp: 2 });
  if (variant === "replacement") manager.appendContextEdit(first, { content: [fauxToolCall("read", { path: "edited.ts" }, { id: "read-1" })] });
  if (variant === "previous" || variant === "retain-none") {
    manager.appendCompaction("Previous checkpoint", variant === "retain-none" ? null : first, 9000, { readFiles: ["older.ts"], modifiedFiles: ["older-edit.ts"] });
  }
  if (variant === "omission") {
    const omitted = manager.appendMessage(fauxAssistantMessage("abandoned attempt", { stopReason: "error" }));
    manager.appendContextEdit(omitted, null);
  } else {
    manager.appendMessage({ role: "user", content: "later input " + "z".repeat(8000), timestamp: 3 });
    manager.appendMessage(fauxAssistantMessage(fauxToolCall("write", { path: "new.ts", content: "new" }, { id: "write-2" }), { stopReason: "toolUse" }));
    manager.appendMessage({ role: "toolResult", toolCallId: "write-2", toolName: "write", content: [{ type: "text", text: "written".repeat(500) }], isError: false, timestamp: 4 });
  }
  let observed = 0;
  const { session } = await setup(t, { manager, before: pi => pi.on("session_before_compact", event => {
    observed++;
    const port = prepareCompaction(event.branchEntries, event.preparation.settings);
    assert.deepEqual(port, event.preparation);
    if (variant === "replacement") { assert.ok(port.fileOps.read.has("edited.ts")); assert.ok(!port.fileOps.read.has("original.ts")); }
    if (variant === "previous") { assert.ok(port.fileOps.read.has("older.ts")); assert.equal(port.previousSummary, "Previous checkpoint"); }
    if (variant === "retain-none") assert.equal(port.previousSummary, "Previous checkpoint");
    if (variant === "omission") assert.ok(!JSON.stringify(port.messagesToSummarize).includes("abandoned attempt"));
    if (variant === "split") assert.equal(port.isSplitTurn, true);
    assert.notEqual(port.firstKeptEntryId, user);
    return { cancel: true };
  }) });
  await assert.rejects(session.compact(), /cancel/i);
  assert.equal(observed, 1);
});

test("second summary failure in a split turn discards the entire batch", async t => {
  const { run, manager, counts } = await setup(t, { summary: (_c, _r, counts) =>
    fauxAssistantMessage(counts.summary === 1 ? "History checkpoint succeeded" : "") });
  await run();
  assert.equal(counts.summary, 2);
  assert.equal(counts.main, 3);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, 0);
});

test("tool_result marked unsuccessful cannot authorize OCC even if update_plan ran", async t => {
  const { run, manager, counts } = await setup(t, { after: pi => pi.on("tool_result", event => {
    if (event.toolCallId === "boundary") return { isError: true };
  }) });
  await run();
  assert.equal(counts.summary, 0);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, 0);
});
