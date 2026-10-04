import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createAgentSession, createCodemodeExtension, DefaultResourceLoader, SessionManager, SettingsManager, VERSION } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { createProvider, getCurrentSystemPrompt, getCurrentTools } from "@earendil-works/pi-ai";
import { createFauxCore, fauxAssistantMessage, fauxToolCall, fauxText } from "@earendil-works/pi-ai/providers/faux";
import { createOnlineContextCompactExtension, POST_COMPACTION_PLAN_REMINDER, DEFAULT_NATIVE_SUMMARY_TOKEN_ESTIMATE } from "../extensions/online-context-compact/extension.ts";
import { estimateNativeCompactionTokens } from "../extensions/online-context-compact/projection.ts";
import { createObservationPackExtension } from "../extensions/observation-pack/index.ts";
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
  assert.equal(VERSION, "1.0.2");
  const cwd = await mkdtemp(join(tmpdir(), "occ-session-"));
  const agentDir = join(cwd, "agent");
  await mkdir(agentDir);
  const settings = SettingsManager.inMemory({
    compaction: { enabled: options.enabled ?? true, keepRecentTokens: options.keepRecentTokens ?? 150, reserveTokens: 1024 },
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
      assert.equal(typeof request.sessionId, "string", "native summary supplies a routing session ID");
      assert.ok(request.sessionId.length > 0);
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
    tools: Number(process.env.PI_WORKER_DEPTH ?? 0) > 0 ? [] : (options.tools ?? ["update_plan"]), resourceLoader: loader, sessionManager: manager, settingsManager: settings }));
  await session.bindExtensions({ onError: error => counts.errors.push(error) });
  session.subscribe(event => { if (event.type === "agent_settled") counts.settled++; });
  t.after(async () => { session.dispose(); await rm(cwd, { recursive: true, force: true }); });
  return { session, manager, counts, settings, faux, run: () => session.prompt("Finish the task", { expandPromptTemplates: false }) };
}

// Pi 1.0.2 test-only access: dispatch its real scheduled refresh immediately,
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

const phasePlan = completed => Array.from({ length: 4 }, (_, i) => ({
  id: `phase-${i}`, goal: `phase ${i}`, status: i < completed ? "completed" : i === completed ? "in_progress" : "pending",
}));
const longCheckpoint = () => fauxAssistantMessage("Checkpoint " + "x".repeat(8000));

for (const rekey of [false, true]) test(`post-compaction ${rekey ? "rekeyed" : "identical"} plan is history, not another boundary`, async t => {
  const restated = phasePlan(1).map(s => rekey ? { ...s, id: `new-${s.id}`, goal: `restated ${s.goal}` } : s);
  const next = restated.map((s, i) => i === 1 ? { ...s, status: "completed" } : s);
  const attempts = new Set(), results = new Map();
  const h = await setup(t, {
    script: [call("open", phasePlan(0)), call("first", phasePlan(1)),
      call("restate", restated), call("repeat", restated), call("next", next), fauxAssistantMessage("FINAL")],
    summary: (_c, _r, counts) => { attempts.add(counts.main); return longCheckpoint(); },
    after: pi => pi.on("turn_end", event => {
      for (const result of event.toolResults) results.set(result.toolCallId, result.details);
    }),
  });
  await h.run();
  assert.deepEqual([...attempts], [2, 5], "the large retained checkpoint is compacted only on genuine progress");
  for (const id of ["restate", "repeat"]) {
    assert.equal(results.get(id).boundary, false);
    assert.deepEqual(results.get(id).completed_step_ids, []);
  }
  assert.equal(results.get("next").boundary, true);
  const state = restoreOnlineState(h.manager.getBranch());
  assert.deepEqual(state.plan, next);
  assert.deepEqual(state.completedBoundaryRequestCounts, [2, 3]);
  assert.equal(state.lastCompactionRequestCount, 5);
  assert.equal(state.nativeCompactionCount, 2);
  assert.equal(h.counts.main, 6);
  assert.equal(h.counts.settled, 1);
  assert.deepEqual(h.counts.errors, []);
});

test("first post-compaction update can be real progress; cooldown defers only immediate recompression", async t => {
  const attempts = new Set(), snapshots = [];
  const h = await setup(t, {
    script: [call("open", phasePlan(0)), call("first", phasePlan(1)),
      call("immediate", phasePlan(2)), call("later", phasePlan(3)), fauxAssistantMessage("FINAL")],
    summary: (_c, _r, counts) => { attempts.add(counts.main); return longCheckpoint(); },
    after: pi => pi.on("turn_end", (event, ctx) => {
      const before = restoreOnlineState(ctx.sessionManager.getBranch());
      if (event.toolResults.some(r => r.toolCallId === "immediate")) {
        assert.equal(event.toolResults[0].details.boundary, true, "no blanket first-restatement suppression");
        assert.deepEqual(before.completedBoundaryRequestCounts, [2, 1]);
        assert.equal(before.lastCompactionRequestCount, 2);
        assert.equal(event.entries.length, 0, "cooldown suppresses the otherwise profitable immediate draft");
      }
      const after = event.entries.find(e => e.customType === ONLINE_STATE_ENTRY)?.data;
      if (after) {
        for (const field of ["plan", "completedBoundaryRequestCounts", "lastBoundaryRequestCount", "positiveContextDeltaTotal", "positiveContextDeltaCount"]) {
          assert.deepEqual(after[field], before[field], `${field} survives compaction`);
        }
        assert.equal(after.lastContextTokens, null);
        snapshots.push(after);
      }
    }),
  });
  await h.run();
  assert.deepEqual([...attempts], [2, 4]);
  assert.equal(snapshots.length, 2);
  assert.ok(snapshots[0].positiveContextDeltaCount > 0);
  assert.deepEqual(restoreOnlineState(h.manager.getBranch()).completedBoundaryRequestCounts, [2, 1, 1]);
  assert.equal(h.counts.main, 5);
  assert.equal(h.counts.settled, 1);
  assert.deepEqual(h.counts.errors, []);
});

test("completed plan survives tool-result/internal continuation; only a new external user task resets horizon, not debt", async t => {
  const inputs = [];
  let h;
  h = await setup(t, {
    before: (_pi, manager) => manager.appendCustomEntry(ONLINE_STATE_ENTRY, {
      ...initialOnlineState(), nativeCompactionCount: 1, lastCompactionRequestCount: 0,
      cacheDebtTokens: 100000, cacheDebtRepaymentTokens: 100,
    }),
    script: [call("open", plan()), call("complete", plan(true, true)), () => {
      const state = restoreOnlineState(h.manager.getBranch());
      assert.deepEqual(state.plan, plan(true, true), "tool-result continuation is still the completed task");
      assert.deepEqual(state.completedBoundaryRequestCounts, [2]);
      return fauxAssistantMessage("FIRST FINAL");
    }, fauxAssistantMessage("INTERNAL FINAL"), fauxAssistantMessage("NEW TASK FINAL")],
    after: pi => pi.on("input", (event, ctx) => inputs.push({ source: event.source, state: restoreOnlineState(ctx.sessionManager.getBranch()) })),
  });
  await h.run();
  const completed = restoreOnlineState(h.manager.getBranch());
  assert.equal(completed.cacheDebtTokens, 99700);
  assert.ok(completed.positiveContextDeltaCount > 0);
  await h.session.prompt("Internal continuation", { source: "extension", expandPromptTemplates: false });
  const internal = restoreOnlineState(h.manager.getBranch());
  assert.deepEqual(internal.plan, completed.plan);
  assert.deepEqual(internal.completedBoundaryRequestCounts, completed.completedBoundaryRequestCounts);
  assert.equal(internal.epoch, completed.epoch);
  await h.session.prompt("A genuinely new user task", { expandPromptTemplates: false });
  const handoff = inputs.at(-1).state;
  assert.equal(inputs.at(-1).source, "interactive");
  assert.deepEqual(handoff.plan, []);
  assert.deepEqual(handoff.completedBoundaryRequestCounts, []);
  assert.equal(handoff.lastContextTokens, null);
  assert.equal(handoff.positiveContextDeltaCount, 0);
  assert.equal(handoff.positiveContextDeltaTotal, 0);
  assert.equal(handoff.lastBoundaryRequestCount, internal.requestCount);
  assert.equal(handoff.epoch, internal.epoch + 1);
  for (const field of ["cacheDebtTokens", "cacheDebtRepaymentTokens", "nativeCompactionCount", "lastCompactionRequestCount"]) {
    assert.equal(handoff[field], internal[field], `${field} survives task handoff`);
  }
  const final = restoreOnlineState(h.manager.getBranch());
  assert.equal(final.cacheDebtTokens, 99500, "only actual requests repay the retained debt");
  assert.equal(final.positiveContextDeltaCount, 0);
  assert.equal(final.lastCompactionRequestCount, 0);
  assert.equal(h.counts.summary, 0);
  assert.deepEqual(h.counts.errors, []);
});

for (const cancellation of ["abort", "discard"]) test(`${cancellation} leaves no speculative cooldown/history; identical retry is inert and next progress still compacts`, async t => {
  let started;
  const ready = new Promise(resolve => { started = resolve; });
  const attempts = new Set();
  let h;
  h = await setup(t, {
    script: [call("open", phasePlan(0)), call("first", phasePlan(1)),
      call("repeat", phasePlan(1), "More work " + "y".repeat(16000)), call("next", phasePlan(2)), fauxAssistantMessage("FINAL")],
    summary: async (_c, request, counts) => {
      attempts.add(counts.main);
      if (cancellation === "abort" && counts.main === 2) {
        started();
        await new Promise(resolve => request.signal.addEventListener("abort", resolve, { once: true }));
      }
      return longCheckpoint();
    },
    after: pi => pi.on("turn_end", (event, ctx) => {
      if (event.toolResults.some(r => r.toolCallId === "repeat")) {
        const state = restoreOnlineState(ctx.sessionManager.getBranch());
        assert.equal(event.toolResults[0].details.boundary, false);
        assert.equal(state.lastCompactionRequestCount, null);
        assert.equal(state.nativeCompactionCount, 0);
        assert.deepEqual(state.completedBoundaryRequestCounts, [2]);
        assert.equal(event.entries.length, 0);
      }
      if (cancellation === "discard" && event.toolResults.some(r => r.toolCallId === "first")) {
        assert.ok(event.entries.some(e => e.type === "compaction"));
        return { entries: [], continue: false };
      }
    }),
  });
  const firstRun = h.run();
  if (cancellation === "abort") {
    await ready;
    const beforeAbort = restoreOnlineState(h.manager.getBranch());
    await Promise.all([h.session.abort(), firstRun]);
    const afterAbort = restoreOnlineState(h.manager.getBranch());
    // Pi can emit a final aborted turn_start without a provider call. Only that
    // request-proxy observation may change; no speculative compaction is booked.
    for (const field of ["plan", "epoch", "lastCompactionRequestCount", "nativeCompactionCount", "completedBoundaryRequestCounts", "cacheDebtTokens", "cacheDebtRepaymentTokens"]) {
      assert.deepEqual(afterAbort[field], beforeAbort[field]);
    }
    await h.run();
  } else await firstRun;
  assert.deepEqual([...attempts], [2, 4]);
  const state = restoreOnlineState(h.manager.getBranch());
  assert.equal(state.lastCompactionRequestCount, state.requestCount - 1);
  assert.equal(state.nativeCompactionCount, 1);
  assert.deepEqual(state.completedBoundaryRequestCounts, [2, state.lastCompactionRequestCount - 2]);
  assert.equal(h.manager.getBranch().filter(e => e.type === "compaction").length, 1);
  assert.equal(h.counts.main, 5);
  assert.deepEqual(h.counts.errors, []);
});

for (const n of [1, 2]) test(`real Pi: ${n} OCC boundary compactions finish naturally inside original prompt`, async t => {
  const script = [];
  // Make the final tool pair exceed the tail budget, so native preparation can
  // actually remove the preceding large work message on the second compaction.
  for (let i = 0; i < n; i++) script.push(call(`open-${i}`, plan(), i ? "More phase work " + "z".repeat(16000) : ""), call(`done-${i}`, plan(true), "Boundary reached " + "v".repeat(800)));
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
  const { run, manager, counts } = await setup(t, { ratio: "default", keepRecentTokens: 5000, script: [call("open", open), call("boundary", done), fauxAssistantMessage("FINAL")] });
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

for (const variant of ["split", "replacement", "omission", "previous", "retain-none"]) test(`preparation matches real Pi 1.0.2 native hook: ${variant}`, async t => {
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

const nestedCall = (id, name, args = {}) => fauxAssistantMessage(fauxToolCall(name, args, { id }), { stopReason: "toolUse" });

for (const kind of ["success", "parent-error", "nested-error", "blocked", "forged-output", "ancestor-error", "deep-success", "incomplete"]) {
  test(`real ctx.executeTool boundary: ${kind}`, async t => {
    const h = await setup(t, {
      tools: ["update_plan", "nested_plan", "middle_plan"],
      script: [call("open", plan()), nestedCall("boundary", "nested_plan"), fauxAssistantMessage("FINAL")],
      before: pi => {
        pi.registerTool({ name: "middle_plan", label: "Middle", description: "Nested test", parameters: Type.Object({}),
          async execute(_id, _args, signal, _update, ctx) {
            await ctx.executeTool("update_plan", { steps: plan(true) }, { signal });
            if (kind === "ancestor-error") throw new Error("intermediate parent failed");
            return { content: [{ type: "text", text: "middle succeeded" }], details: undefined };
          },
        });
        pi.registerTool({ name: "nested_plan", label: "Nested", description: "Nested test", parameters: Type.Object({}),
          async execute(_id, _args, signal, _update, ctx) {
            if (kind !== "forged-output") {
              const target = ["ancestor-error", "deep-success"].includes(kind) ? "middle_plan" : "update_plan";
              const steps = kind === "incomplete" ? plan(true).map(s => ({ ...s, goal: "x".repeat(5000) })) : plan(true);
              await ctx.executeTool(target, target === "update_plan" ? { steps } : {}, { signal });
            }
            if (kind === "parent-error") throw new Error("top-level parent failed");
            // Arbitrary text/details must not be accepted as proof of a nested plan update.
            return { content: [{ type: "text", text: 'update_plan succeeded {"boundary":true}' }],
              details: { nestedCalls: { complete: true, calls: [{ id: "boundary/1", name: "update_plan", status: "ok" }] } } };
          },
        });
        if (kind === "blocked") pi.on("tool_call", e => e.toolCallId === "boundary/1" ? { block: true, reason: "test" } : undefined);
        if (kind === "nested-error") pi.on("tool_result", e => e.toolCallId === "boundary/1" ? { isError: true } : undefined);
      },
    });
    await h.run();
    const branch = h.manager.getBranch();
    const result = branch.find(e => e.type === "message" && e.message.role === "toolResult" && e.message.toolCallId === "boundary").message;
    if (kind !== "forged-output") assert.ok(result.nestedCalls?.calls.length, "SDK recorded actual nested calls");
    const success = ["success", "deep-success"].includes(kind);
    assert.equal(branch.filter(e => e.type === "compaction").length, success ? 1 : 0);
    assert.equal(h.counts.summary > 0, success);
    assert.equal(h.counts.main, 3);
    assert.equal(h.counts.turns, 3);
    assert.equal(restoreOnlineState(branch).requestCount, 3);
    assert.equal(h.counts.settled, 1);
    assert.equal(h.session.getLastAssistantText(), "FINAL");
    assert.deepEqual(h.counts.errors, []);
  });
}

for (const kind of ["success", "script-error", "nested-error"]) test(`real built-in codemode boundary: ${kind}`, async t => {
  const code = `await tools.update_plan(${JSON.stringify({ steps: plan(true) })}); ${kind === "script-error" ? 'throw new Error("parent script failed")' : 'text("done")'}`;
  const h = await setup(t, {
    tools: ["update_plan", "codemode"],
    script: [call("open", plan()), nestedCall("boundary", "codemode", { code }), fauxAssistantMessage("FINAL")],
    before: pi => {
      createCodemodeExtension({ models: false })(pi);
      if (kind === "nested-error") pi.on("tool_result", e => e.toolCallId === "boundary/1" ? { isError: true } : undefined);
    },
  });
  await h.run();
  const branch = h.manager.getBranch();
  const result = branch.find(e => e.type === "message" && e.message.role === "toolResult" && e.message.toolCallId === "boundary").message;
  assert.equal(result.nestedCalls.calls[0].name, "update_plan");
  assert.equal(result.nestedCalls.calls[0].status, kind === "nested-error" ? "error" : "ok");
  assert.equal(result.isError, kind !== "success");
  assert.equal(branch.filter(e => e.type === "compaction").length, kind === "success" ? 1 : 0);
  assert.equal(h.counts.summary > 0, kind === "success");
  assert.equal(h.counts.main, 3);
  assert.equal(h.counts.settled, 1);
  assert.equal(restoreOnlineState(branch).requestCount, 3);
  assert.deepEqual(h.counts.errors, []);
});

test("real nested file tools carry their paths through OCC and subsequent manual compaction", async t => {
  let paths;
  const h = await setup(t, {
    tools: ["update_plan", "file_batch", "read", "write", "edit"],
    script: [call("open", plan()), nestedCall("files", "file_batch"), call("boundary", plan(true), "new phase ".repeat(200)), fauxAssistantMessage("FINAL " + "f".repeat(2000))],
    before: pi => pi.registerTool({ name: "file_batch", label: "Files", description: "Nested file test", parameters: Type.Object({}),
      async execute(_id, _args, signal, _update, ctx) {
        paths = [join(ctx.cwd, "read-only.txt"), join(ctx.cwd, "modified.txt")];
        const run = async (name, args) => {
          const result = await ctx.executeTool(name, args, { signal });
          assert.equal(result.isError, false);
        };
        // Seed the read fixture outside the tool transcript; all operations below use real SDK tools.
        await writeFile(paths[0], "source fixture");
        await run("read", { path: paths[0] });
        await run("write", { path: paths[1], content: "before" });
        await run("edit", { path: paths[1], oldText: "before", newText: "after" });
        return { content: [{ type: "text", text: "files ready" }], details: undefined };
      },
    }),
  });
  await h.run();
  const compacted = () => h.manager.getBranch().filter(e => e.type === "compaction");
  assert.equal(compacted().length, 1);
  for (const entry of compacted()) {
    assert.ok(entry.details.readFiles.includes(paths[0]));
    assert.ok(entry.details.modifiedFiles.includes(paths[1]));
  }
  const nested = h.manager.getBranch().find(e => e.type === "message" && e.message.toolCallId === "files").message.nestedCalls;
  assert.deepEqual(nested.calls.map(c => [c.name, c.status]), [["read", "ok"], ["write", "ok"], ["edit", "ok"]]);
  await h.session.compact();
  assert.equal(compacted().length, 2);
  assert.ok(compacted()[1].details.readFiles.includes(paths[0]));
  assert.ok(compacted()[1].details.modifiedFiles.includes(paths[1]));
  assert.equal(h.counts.main, 4);
  assert.equal(h.counts.settled, 1);
  assert.deepEqual(h.counts.errors, []);
});

test("real ctx.executeTool: cancelling parent after plan completion cannot authorize OCC", async t => {
  let started;
  const ready = new Promise(resolve => { started = resolve; });
  const h = await setup(t, {
    tools: ["update_plan", "nested_plan"],
    script: [call("open", plan()), nestedCall("boundary", "nested_plan")],
    before: pi => pi.registerTool({ name: "nested_plan", label: "Nested", description: "Nested cancellation test", parameters: Type.Object({}),
      async execute(_id, _args, signal, _update, ctx) {
        await ctx.executeTool("update_plan", { steps: plan(true) }, { signal });
        started();
        await new Promise(resolve => signal.addEventListener("abort", resolve, { once: true }));
        return { content: [{ type: "text", text: "late parent result" }], details: undefined };
      },
    }),
  });
  const prompt = h.run();
  await ready;
  await Promise.all([h.session.abort(), prompt]);
  assert.equal(h.counts.summary, 0);
  assert.equal(h.counts.main, 2);
  assert.equal(h.counts.settled, 1);
  assert.equal(h.session.isIdle, true);
  assert.equal(h.manager.getBranch().filter(e => e.type === "compaction").length, 0);
});

test("tool_result marked unsuccessful cannot authorize OCC even if update_plan ran", async t => {
  const { run, manager, counts } = await setup(t, { after: pi => pi.on("tool_result", event => {
    if (event.toolCallId === "boundary") return { isError: true };
  }) });
  await run();
  assert.equal(counts.summary, 0);
  assert.equal(manager.getBranch().filter(e => e.type === "compaction").length, 0);
});

const economicPlan = done => [...plan(done), ...Array.from({ length: 20 }, (_, i) => ({
  id: `later-${i}`, goal: `remaining task ${i}`, status: "pending",
}))];
const carriedState = () => ({ ...initialOnlineState(), nativeCompactionCount: 1,
  cacheDebtTokens: 100000, cacheDebtRepaymentTokens: 100 });

// The gate holds a real provider request while the external follow-up is queued.
// Pi 1.0.2 then emits turn_start -> message_start (no second input) -> request.
async function queuedHandoffSession(t, options = {}) {
  let ready, release;
  const started = new Promise(resolve => { ready = resolve; });
  const gate = new Promise(resolve => { release = resolve; });
  const snapshots = [], events = [];
  const seed = { ...carriedState(), plan: plan(), requestCount: 7,
    lastBoundaryRequestCount: 4, completedBoundaryRequestCounts: [2, 4],
    lastCompactionRequestCount: 4, lastContextTokens: 2000,
    positiveContextDeltaTotal: 700, positiveContextDeltaCount: 2 };
  const h = await setup(t, { ratio: null,
    before: (_pi, manager) => manager.appendCustomEntry(ONLINE_STATE_ENTRY, seed),
    script: [async (_context, request) => {
      ready();
      await Promise.race([gate, new Promise(resolve => request.signal.addEventListener("abort", resolve, { once: true }))]);
      return request.signal.aborted ? fauxAssistantMessage("", { stopReason: "aborted" }) : call("complete-a", plan(true, true));
    }, fauxAssistantMessage("TASK A FINAL"), ...Array.from({ length: 6 }, () => fauxAssistantMessage("FOLLOWUP FINAL"))],
    after: (pi, manager) => {
      pi.on("input", e => { events.push(["input", e.text, e.source]); });
      pi.on("turn_start", () => { events.push(["turn_start"]); });
      pi.on("message_start", e => { events.push(["message_start", e.message.role]); });
      pi.on("before_provider_request", (_e, ctx) => {
        snapshots.push(restoreOnlineState(ctx.sessionManager.getBranch()));
        events.push(["request"]);
      });
      options.after?.(pi, manager);
    },
  });
  t.after(release);
  return { ...h, seed, snapshots, events, started, release };
}

for (const source of ["interactive", "rpc"]) test(`queued ${source} follow-up hands off a completed plan only at delivery`, async t => {
  const h = await queuedHandoffSession(t);
  const running = h.run();
  await h.started;
  const queued = restoreOnlineState(h.manager.getBranch());
  const images = [{ type: "image", data: "dGVzdA==", mimeType: "image/png" }];
  assert.equal(await h.session.followUp("Task B", images, { source }), "queued");
  assert.deepEqual(restoreOnlineState(h.manager.getBranch()), queued, "A is still active at input time");
  h.release();
  await running;
  const [first, finalA, firstB] = h.snapshots;
  assert.equal(h.snapshots.length, 3);
  assert.deepEqual(first.plan, plan());
  assert.deepEqual(finalA.plan, plan(true, true), "tool results/internal continuation keep A's plan");
  assert.ok(finalA.completedBoundaryRequestCounts.length);
  assert.deepEqual(firstB.plan, []);
  assert.deepEqual(firstB.pendingProgress, []);
  assert.deepEqual(firstB.completedBoundaryRequestCounts, []);
  assert.equal(firstB.lastBoundaryRequestCount, firstB.requestCount);
  assert.equal(firstB.lastContextTokens, null);
  assert.equal(firstB.positiveContextDeltaTotal, 0);
  assert.equal(firstB.positiveContextDeltaCount, 0);
  assert.equal(firstB.epoch, finalA.epoch + 1);
  assert.equal(firstB.requestCount, h.seed.requestCount + 3);
  assert.equal(firstB.cacheDebtTokens, h.seed.cacheDebtTokens - 3 * h.seed.cacheDebtRepaymentTokens);
  for (const field of ["nativeCompactionCount", "lastCompactionRequestCount", "cacheDebtRepaymentTokens"]) {
    assert.equal(firstB[field], h.seed[field], `${field} survives delivery handoff`);
  }
  assert.equal(h.events.filter(e => e[0] === "input" && e[1] === "Task B").length, 1);
  const delivered = h.events.findLastIndex(e => e[0] === "message_start" && e[1] === "user");
  assert.equal(h.events[delivered - 1][0], "turn_start");
  assert.equal(h.events[delivered + 1][0], "request");
  assert.equal(h.counts.summary, 0);
  assert.equal(h.counts.settled, 1);
  assert.deepEqual(h.counts.errors, []);
});

for (const kind of ["extension-first", "extension-last", "extension-first-model", "transformed-extension", "steer-first", "steer-last"]) {
  test(`ambiguous queued ${kind} cannot cause a delivery handoff`, async t => {
    const h = await queuedHandoffSession(t, { after: pi => {
      if (kind === "transformed-extension") pi.on("input", e => e.source === "extension"
        ? { action: "transform", text: "Task B" } : undefined);
    } });
    const running = h.run();
    await h.started;
    const collision = () => kind.startsWith("steer") ? h.session.steer("Task B")
      : h.session.sendUserMessage(kind === "transformed-extension" ? "different injected text" : "Task B", { deliverAs: "followUp" });
    if (kind.includes("first")) await collision();
    if (kind === "extension-first-model") await h.session.setModel({ ...h.faux.getModel(), name: "changed queued model" });
    await h.session.followUp("Task B");
    if (!kind.includes("first")) await collision();
    const afterInput = restoreOnlineState(h.manager.getBranch());
    if (kind.startsWith("steer")) {
      assert.deepEqual(afterInput.plan, [], "existing steering correction is still immediate");
      assert.equal(afterInput.cacheDebtTokens, 0, "existing correction debt policy is unchanged");
    }
    h.release();
    await running;
    for (const state of h.snapshots.slice(1)) {
      assert.deepEqual(state.plan, plan(true, true));
      assert.equal(state.epoch, afterInput.epoch, "no extra handoff at user-role delivery");
    }
    assert.equal(h.counts.summary, 0);
    assert.deepEqual(h.counts.errors, []);
  });
}

test("custom extension messages and explicit continuations cannot consume a queued external handoff", async t => {
  let injected = false;
  const h = await queuedHandoffSession(t, { after: pi => pi.on("turn_end", event => {
    if (injected || !event.toolResults.some(r => r.toolCallId === "complete-a")) return;
    injected = true;
    return { entries: [{ type: "custom_message", customType: "test-internal-continuation", content: "Task B", display: false }], continue: true };
  }) });
  const running = h.run();
  await h.started;
  await h.session.followUp("Task B");
  await h.session.sendCustomMessage({ customType: "test-extension", content: "Task B", display: false }, { deliverAs: "steer" });
  h.release();
  await running;
  assert.deepEqual(h.snapshots[1].plan, plan(true, true));
  assert.deepEqual(h.snapshots.at(-1).plan, []);
  assert.equal(h.snapshots.at(-1).epoch, h.seed.epoch + 1);
  // Pi persists boundary custom-message drafts without message_start; only the
  // actual queued custom message emits a delivery event.
  assert.equal(h.events.filter(e => e[0] === "message_start" && e[1] === "custom").length, 1);
  assert.equal(h.manager.getBranch().filter(e => e.type === "custom_message").length, 2);
  assert.deepEqual(h.counts.errors, []);
});

test("cleared queued input cannot authorize a same-text extension replacement", async t => {
  const h = await queuedHandoffSession(t);
  const running = h.run();
  await h.started;
  await h.session.followUp("Task B");
  assert.deepEqual(h.session.clearQueue().followUp, ["Task B"]);
  await h.session.sendUserMessage("Task B", { deliverAs: "followUp" });
  h.release();
  await running;
  assert.equal(h.snapshots.length, 3);
  assert.deepEqual(h.snapshots[2].plan, plan(true, true));
  assert.equal(h.snapshots[2].epoch, h.seed.epoch);
  assert.deepEqual(h.counts.errors, []);
});

test("aborting a queued follow-up drops its delivery authority before a later internal run", async t => {
  const h = await queuedHandoffSession(t);
  const running = h.run();
  await h.started;
  await h.session.followUp("Task B");
  await Promise.all([h.session.abort(), running]);
  assert.deepEqual(h.session.clearQueue().followUp, ["Task B"]);
  const completed = { ...restoreOnlineState(h.manager.getBranch()), plan: plan(true, true) };
  h.manager.appendCustomEntry(ONLINE_STATE_ENTRY, completed);
  // Direct core queues do not emit input and must not inherit an aborted input's
  // authority. A custom-triggered run also bypasses before_agent_start.
  h.session.agent.followUp({ role: "user", content: [{ type: "text", text: "Task B" }], timestamp: Date.now() });
  await h.session.sendCustomMessage({ customType: "test-restart", content: "resume internally", display: false }, { triggerTurn: true });
  for (const state of h.snapshots.slice(1)) {
    assert.deepEqual(state.plan, completed.plan);
    assert.equal(state.epoch, completed.epoch);
  }
  assert.deepEqual(h.counts.errors, []);
});

test("queued delivery cannot transfer handoff authority to a different committed branch", async t => {
  let h;
  h = await queuedHandoffSession(t, { after: pi => pi.on("turn_end", event => {
    if (!event.message.content.some(c => c.type === "text" && c.text === "TASK A FINAL")) return;
    const completed = restoreOnlineState(h.manager.getBranch());
    // Exercise the ancestry guard even for SDK callers that move the manager
    // directly, without session_before_tree/session_tree notifications.
    h.manager.branch(h.manager.getBranch()[0].id);
    h.manager.appendCustomEntry(ONLINE_STATE_ENTRY, completed);
  }) });
  const running = h.run();
  await h.started;
  await h.session.followUp("Task B");
  h.release();
  await running;
  assert.equal(h.snapshots.length, 3);
  assert.deepEqual(h.snapshots[2].plan, plan(true, true));
  assert.equal(h.snapshots[2].epoch, h.snapshots[1].epoch);
  assert.deepEqual(h.counts.errors, []);
});

test("real commits accumulate outstanding debt and repayment using the same estimated post-context cost", async t => {
  let observed;
  const commits = [];
  const h = await setup(t, { ratio: 12.5,
    before: (_pi, manager) => manager.appendCustomEntry(ONLINE_STATE_ENTRY, carriedState()),
    script: [call("open-1", economicPlan(false)), call("done-1", economicPlan(true), "boundary ".repeat(100)),
      call("open-2", economicPlan(false), "new work ".repeat(2000)), call("done-2", economicPlan(true), "boundary ".repeat(100)),
      fauxAssistantMessage("FINAL")],
    summary: () => fauxAssistantMessage("A very short actual checkpoint"),
    after: pi => {
      pi.on("context_with_system", event => { observed = structuredClone(event.messages); });
      pi.on("turn_end", (event, ctx) => {
        if (!event.entries.some(e => e.type === "compaction")) return;
        const before = restoreOnlineState(ctx.sessionManager.getBranch());
        const next = event.entries.find(e => e.customType === ONLINE_STATE_ENTRY).data;
        const prep = prepareCompaction(ctx.sessionManager.getBranch(), { enabled: true, reserveTokens: 1024, keepRecentTokens: 150 });
        const saving = estimateNativeCompactionTokens(prep, observed) - DEFAULT_NATIVE_SUMMARY_TOKEN_ESTIMATE;
        assert.ok(saving > 0);
        assert.equal(next.cacheDebtTokens, before.cacheDebtTokens + Math.max(0, prep.tokensBefore - saving) * 11.5);
        assert.equal(next.cacheDebtRepaymentTokens, before.cacheDebtRepaymentTokens + saving);
        assert.equal(next.nativeCompactionCount, before.nativeCompactionCount + 1);
        assert.ok(before.cacheDebtTokens > 0, "new debt must not overwrite unpaid prior debt");
        assert.ok(before.plan.length > 0, "pre-commit state is still authoritative");
        assert.deepEqual(next.plan, before.plan, "compaction preserves the registered working plan");
        commits.push({ before, next });
      });
    },
  });
  await h.run();
  assert.equal(commits.length, 2);
  assert.equal(commits[1].before.cacheDebtTokens, Math.max(0, commits[0].next.cacheDebtTokens - 2 * commits[0].next.cacheDebtRepaymentTokens));
  assert.equal(commits[1].before.cacheDebtRepaymentTokens, commits[0].next.cacheDebtRepaymentTokens);
  const state = restoreOnlineState(h.manager.getBranch());
  assert.equal(state.nativeCompactionCount, 3);
  assert.equal(state.cacheDebtTokens, Math.max(0, commits[1].next.cacheDebtTokens - commits[1].next.cacheDebtRepaymentTokens));
  assert.equal(h.counts.main, 5);
  assert.equal(h.counts.settled, 1);
  assert.deepEqual(h.counts.errors, []);
  const persisted = SessionManager.open(h.manager.getSessionFile());
  assert.deepEqual(restoreOnlineState(persisted.getBranch()), state);
});

for (const failure of ["error", "empty-split", "dropped-draft"]) test(`positive-cost ${failure} does not book debt or leak it into later manual compaction`, async t => {
  let manual = false, proposals = 0;
  const h = await setup(t, { ratio: 12.5,
    script: [call("open", economicPlan(false)), call("boundary", economicPlan(true)), fauxAssistantMessage("FINAL")],
    before: (_pi, manager) => manager.appendCustomEntry(ONLINE_STATE_ENTRY, carriedState()),
    summary: (_c, _r, counts) => manual || failure === "dropped-draft" || (failure === "empty-split" && counts.summary === 1)
      ? fauxAssistantMessage("Valid checkpoint")
      : failure === "error" ? fauxAssistantMessage("", { stopReason: "error", errorMessage: "fatal test failure" }) : fauxAssistantMessage(""),
    after: pi => pi.on("turn_end", event => {
      if (failure !== "dropped-draft" || !event.entries.some(e => e.type === "compaction")) return;
      proposals++;
      assert.ok(event.entries.find(e => e.customType === ONLINE_STATE_ENTRY).data.cacheDebtTokens > 100000);
      return { entries: [], continue: false };
    }),
  });
  await h.run();
  assert.ok(h.counts.summary > 0);
  if (failure === "empty-split") assert.equal(h.counts.summary, 2);
  if (failure === "dropped-draft") assert.equal(proposals, 1);
  const before = restoreOnlineState(h.manager.getBranch());
  assert.equal(before.cacheDebtTokens, 99700, "only the three real turns repay pre-existing debt");
  assert.equal(before.cacheDebtRepaymentTokens, 100);
  assert.equal(before.nativeCompactionCount, 1);
  assert.equal(before.lastCompactionRequestCount, null, "failed/dropped drafts cannot start a cooldown");
  assert.deepEqual(before.completedBoundaryRequestCounts, [2]);
  assert.deepEqual(before.plan, economicPlan(true));
  assert.equal(h.manager.getBranch().filter(e => e.type === "compaction").length, 0);
  manual = true;
  await h.session.compact();
  const after = restoreOnlineState(h.manager.getBranch());
  assert.equal(after.nativeCompactionCount, 2);
  assert.equal(after.cacheDebtTokens, before.cacheDebtTokens);
  assert.equal(after.cacheDebtRepaymentTokens, before.cacheDebtRepaymentTokens);
  assert.equal(after.lastCompactionRequestCount, before.requestCount);
  assert.deepEqual(after.plan, before.plan);
  assert.deepEqual(after.completedBoundaryRequestCounts, before.completedBoundaryRequestCounts);
  assert.equal(after.positiveContextDeltaTotal, before.positiveContextDeltaTotal);
  assert.equal(after.positiveContextDeltaCount, before.positiveContextDeltaCount);
  assert.equal(h.counts.main, 3);
  assert.equal(h.counts.settled, 1);
  assert.deepEqual(h.counts.errors, []);
});

test("real OP projection removes the apparent OCC saving without rewriting native summary history", async t => {
  let observed, rawSaving, visibleSaving;
  const original = "large observation\n".repeat(3000);
  const h = await setup(t, {
    before: (pi, manager) => {
      createObservationPackExtension()(pi);
      for (const entry of manager.getBranch()) if (entry.type === "message") manager.appendContextEdit(entry.id, null);
      manager.appendMessage({ role: "user", content: "old work", timestamp: 1 });
      manager.appendMessage(fauxAssistantMessage(fauxToolCall("read", { path: "observation.txt" }, { id: "old-read" }), { stopReason: "toolUse" }));
      manager.appendMessage({ role: "toolResult", toolCallId: "old-read", toolName: "read",
        content: [{ type: "text", text: original }], isError: false, timestamp: 2 });
      manager.appendMessage(fauxAssistantMessage("already used once"));
      manager.appendMessage(fauxAssistantMessage("already used twice"));
    },
    after: pi => {
      pi.on("context_with_system", event => { observed = structuredClone(event.messages); });
      pi.on("turn_end", (event, ctx) => {
        if (!event.toolResults.some(r => r.toolCallId === "boundary")) return;
        const prep = prepareCompaction(ctx.sessionManager.getBranch(), { enabled: true, reserveTokens: 1024, keepRecentTokens: 150 });
        rawSaving = estimateNativeCompactionTokens(prep);
        visibleSaving = estimateNativeCompactionTokens(prep, observed);
        assert.ok(JSON.stringify(prep).includes(original.slice(0, 100).replaceAll("\n", "\\n")), "native summary still receives raw history");
      });
    },
  });
  await h.run();
  assert.ok(rawSaving > 10000);
  assert.ok(visibleSaving < DEFAULT_NATIVE_SUMMARY_TOKEN_ESTIMATE);
  const packed = h.counts.requests[0].messages.find(m => m.role === "toolResult" && m.toolCallId === "old-read");
  assert.match(packed.content[0].text, /id: obs_/);
  assert.equal(h.counts.summary, 0, "an already-packed prefix does not pay for another memo");
  assert.equal(h.manager.getBranch().filter(e => e.type === "compaction").length, 0);
  assert.equal(h.counts.main, 3);
  assert.deepEqual(h.counts.errors, []);
});
