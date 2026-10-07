import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SessionManager, buildSessionProjection, estimateTokens } from "@earendil-works/pi-coding-agent";
import { fauxAssistantMessage, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { createOnlineContextCompactExtension } from "../extensions/online-context-compact/extension.ts";
import { resolveOnlineSettings, resolveSummarySettings } from "../extensions/online-context-compact/settings.ts";
import { initialOnlineState, recordProviderRequest, recordBoundary, recordCompaction, recordCompletedPlanHandoff, recordCorrection, restoreOnlineState, ONLINE_STATE_ENTRY } from "../extensions/online-context-compact/state.ts";
import { prepareCompaction, OCC_FILE_TRACKING } from "../extensions/online-context-compact/native-preparation.ts";
import { analyzePlanTransition, parsePlanSteps } from "../extensions/online-context-compact/plan.ts";
import { decideCompaction, DEFAULT_COMPACTION_ECONOMICS } from "../extensions/online-context-compact/economics.ts";

const steps = [{ id: "done", goal: "work", status: "completed" }, { id: "next", goal: "verify", status: "pending" }];
const settings = { compaction: { enabled: true, keepRecentTokens: 150, reserveTokens: 1024 }, retry: { enabled: false, maxRetries: 0, baseDelayMs: 1 } };

function harness(options = {}) {
  let manager = SessionManager.inMemory();
  manager.appendMessage({ role: "user", content: "history " + "x".repeat(16000), timestamp: 1 });
  manager.appendMessage(fauxAssistantMessage("prior " + "x".repeat(16000)));
  manager.appendMessage({ role: "user", content: "work now", timestamp: 2 });
  const handlers = new Map(), tools = new Map();
  let active = ["update_plan"], nestedSignal, started, finish;
  const notices = [], requests = [];
  const ready = new Promise(resolve => { started = resolve; });
  const release = new Promise(resolve => { finish = resolve; });
  let model = { id: "test", provider: "faux", api: "faux", contextWindow: 1000000, maxTokens: 4096, reasoning: false };
  const parent = new AbortController();
  const pi = {
    on(name, fn) { handlers.set(name, fn); },
    registerTool(tool) { tools.set(tool.name, tool); },
    getActiveTools() { return active; }, setActiveTools(next) { active = next; },
    appendEntry(type, data) { manager.appendCustomEntry(type, data); },
  };
  const ctx = {
    sessionManager: { getSessionFile: () => "persistent-test.jsonl", getSessionId: () => manager.getSessionId(), getBranch: () => manager.getBranch(), getLeafId: () => manager.getLeafId() },
    get model() { return model; }, signal: parent.signal, getSystemPrompt: () => "test", thinkingLevel: "off",
    hasUI: true, ui: { notify: (...args) => notices.push(args) },
    modelRegistry: { streamSimple(_model, _context, options) {
      nestedSignal = options.signal;
      requests.push({ model: _model, options });
      return { result: async () => { started(); await release; return fauxAssistantMessage("Checkpoint"); } };
    } },
  };
  createOnlineContextCompactExtension({ cacheWriteReadRatio: 0, resolveSettings: () => settings, ...options })(pi);
  const boundary = async () => {
    await tools.get("update_plan").execute("open", { steps: steps.map(s => ({ ...s, status: "pending" })) }, parent.signal, undefined, ctx);
    handlers.get("turn_start")({}, ctx);
    const message = fauxAssistantMessage(fauxToolCall("update_plan", { steps }, { id: "boundary" }), { stopReason: "toolUse" });
    manager.appendMessage(message);
    const result = await tools.get("update_plan").execute("boundary", { steps }, parent.signal, undefined, ctx);
    const toolResult = { role: "toolResult", toolCallId: "boundary", toolName: "update_plan", ...result, isError: false, timestamp: 3 };
    manager.appendMessage(toolResult);
    return handlers.get("turn_end")({ outcome: "completed", message, toolResults: [toolResult], entries: [], continue: false }, ctx);
  };
  return { handlers, tools, ctx, ready, finish, boundary, notices, requests, parent, manager: () => manager, signal: () => nestedSignal,
    replaceSession: () => { manager = SessionManager.inMemory(); },
    replaceModel: () => { model = { ...model, id: "changed" }; },
    mutateModel: () => { model.id = "mutated-in-place"; },
  };
}

const tick = () => new Promise(resolve => setImmediate(resolve));
const settle = h => h.handlers.get("agent_before_settle")({ entries: [], outcome: "completed" }, h.ctx);

test("background summary does not block, survives another run, preserves new messages and current plan", async () => {
  const h = harness();
  assert.equal(await h.boundary(), undefined); // Must resolve before the summary is released.
  await h.ready;
  assert.equal(settle(h), undefined); // Not ready: never wait.
  h.handlers.get("before_agent_start")({}, h.ctx);
  h.manager().appendMessage({ role: "user", content: "NEW MESSAGE", timestamp: 4 });
  const currentSteps = [...steps, { id: "later", goal: "new work", status: "pending" }];
  await h.tools.get("update_plan").execute("later", { steps: currentSteps }, h.parent.signal, undefined, h.ctx);
  const requestsBefore = restoreOnlineState(h.manager().getBranch()).requestCount;
  h.handlers.get("turn_start")({}, h.ctx);
  h.finish(); await tick();
  assert.equal(h.requests.length, 1);
  assert.equal(h.notices.length, 1);
  assert.match(h.notices[0][0], /摘要已就绪/);
  assert.equal(h.manager().getBranch().some(e => e.type === "compaction"), false);
  const draft = settle(h);
  assert.equal(draft.entries[0].type, "compaction");
  assert.ok(h.manager().getBranch().some(e => e.id === draft.entries[0].firstKeptEntryId));
  const state = draft.entries.find(e => e.customType === ONLINE_STATE_ENTRY).data;
  assert.deepEqual(state.plan, currentSteps);
  assert.equal(state.requestCount, requestsBefore + 1);
  assert.match(draft.entries.at(-1).content, /new work/);
  assert.equal(settle(h), undefined); // Consume exactly once; no uncommitted success notice.
  assert.equal(h.notices.length, 1);
});

test("applied notification measures the committed boundary, includes the reminder, and separates generation from waiting", async t => {
  let now = 1000;
  t.mock.method(performance, "now", () => now);
  const h = harness(); await h.boundary(); await h.ready;
  h.manager().appendMessage({ role: "user", content: "DURING SUMMARY " + "x".repeat(1000), timestamp: 4 });
  now = 3500;
  h.finish(); await tick();
  assert.match(h.notices[0][0], /生成耗时 2\.5s/);
  assert.doesNotMatch(h.notices[0][0], /tokens|已压缩/);
  now = 8500;
  const draft = settle(h), compacted = draft.entries[0];
  assert.equal(h.notices.length, 1, "not applied until the draft is committed");
  const estimate = () => buildSessionProjection(h.manager().getBranch()).messages.reduce((sum, m) => sum + estimateTokens(m), 0);
  const before = estimate();
  h.manager().appendCompaction(compacted.summary, compacted.firstKeptEntryId, 1000, compacted.details);
  const withoutReminder = estimate();
  for (const entry of draft.entries.slice(1)) {
    if (entry.type === "custom") h.manager().appendCustomEntry(entry.customType, entry.data);
    else h.manager().appendCustomMessageEntry(entry.customType, entry.content, entry.display);
  }
  const after = estimate();
  assert.ok(after > withoutReminder);
  h.manager().appendMessage({ role: "user", content: "AFTER COMMIT " + "x".repeat(5000), timestamp: 5 });
  now = 9000;
  h.handlers.get("agent_settled")({}, h.ctx);
  assert.equal(h.notices.length, 2);
  const message = h.notices[1][0];
  assert.ok(message.includes(`约 ${before.toLocaleString("en-US")} → ${after.toLocaleString("en-US")} tokens`));
  assert.ok(message.includes(`减少 ${(before - after).toLocaleString("en-US")}，${((before - after) / before * 100).toFixed(1)}%`));
  assert.match(message, /生成 2\.5s，总耗时 8\.0s（含等待安全边界）/);
  h.handlers.get("agent_settled")({}, h.ctx);
  assert.equal(h.notices.length, 2);
});

test("token notification reports growth rather than claiming savings for a larger summary", async () => {
  const h = harness();
  const stream = h.ctx.modelRegistry.streamSimple;
  h.ctx.modelRegistry.streamSimple = (...args) => {
    const response = stream(...args);
    return { result: async () => { await response.result(); return fauxAssistantMessage("x".repeat(100000)); } };
  };
  await h.boundary(); await h.ready;
  h.finish(); await tick();
  const compacted = settle(h).entries[0];
  h.manager().appendCompaction(compacted.summary, compacted.firstKeptEntryId, 1000, compacted.details);
  h.handlers.get("agent_settled")({}, h.ctx);
  assert.match(h.notices[1][0], /tokens（增加 [\d,]+，\d+\.\d%）/);
  assert.doesNotMatch(h.notices[1][0], /减少|-\d/);
});

test("one running job deduplicates further boundaries; completed plan needs no continuation", async () => {
  const h = harness();
  await h.boundary(); await h.ready;
  await h.boundary();
  assert.equal(h.requests.length, 1);
  await h.tools.get("update_plan").execute("all", { steps: steps.map(s => ({ ...s, status: "completed" })) }, h.parent.signal, undefined, h.ctx);
  h.finish(); await tick();
  const draft = settle(h);
  assert.equal(draft.continue, false);
  assert.equal(draft.entries.length, 2);
});

for (const change of ["compaction", "context_edit", "branch", "mutate", "native", "abort", "draft"]) {
  test(`background summary cannot overwrite ${change}`, async () => {
    const h = harness(); await h.boundary(); await h.ready;
    h.finish(); await tick(); // Invalidate even an already-ready result.
    if (change === "compaction") h.manager().appendCompaction("other", h.manager().getLeafId(), 1000);
    if (change === "context_edit") h.manager().appendContextEdit(h.manager().getBranch().find(e => e.type === "message").id, null);
    if (change === "branch") h.manager().branch(h.manager().getBranch()[0].id);
    if (change === "mutate") h.manager().getBranch()[0].message.content = "changed";
    if (change === "native") h.handlers.get("session_before_compact")({ branchEntries: [] });
    if (change === "abort") h.parent.abort();
    const draft = h.handlers.get("agent_before_settle")({ entries: change === "draft" ? [{ type: "compaction" }] : [], outcome: "completed" }, h.ctx);
    assert.equal(draft, undefined);
    assert.equal(h.notices.length, 1); // Never says applied.
  });
}

test("cancelled late result cannot erase a newer background job", async () => {
  const h = harness(); await h.boundary(); await h.ready;
  h.handlers.get("model_select")({}, h.ctx);
  await h.boundary();
  assert.equal(h.requests.length, 2);
  h.finish(); await tick();
  assert.equal(settle(h).entries[0].type, "compaction");
  assert.equal(h.notices.length, 1);
});

test("summary model and thinking are frozen independently of the main session", async () => {
  const summaryModel = { id: "summary", provider: "other", api: "faux", maxTokens: 2000, contextWindow: 100000, reasoning: true };
  const h = harness({ resolveSummarySettings: () => ({ model: summaryModel, thinking: "high" }) });
  await h.boundary(); await h.ready;
  assert.equal(h.requests[0].model.id, "summary");
  assert.equal(h.requests[0].options.reasoning, "high");
  assert.equal(h.ctx.model.id, "test");
  h.finish(); await tick(); assert.ok(settle(h));
});

for (const tier of [undefined, "auto", "fast", "ultrafast"]) {
  test(`summary service_tier ${tier ?? "omitted"} is request-local, literal and frozen`, async () => {
    let summarySettings;
    const h = harness({ resolveSummarySettings: ctx => {
      summarySettings = { model: { ...structuredClone(ctx.model), reasoning: true }, thinking: "low", service_tier: tier };
      return summarySettings;
    } });
    await h.boundary(); await h.ready;
    const { model, options } = h.requests[0];
    assert.equal(model.provider, "faux", "injection must not require an OpenAI provider/model");
    assert.equal(options.reasoning, "low");
    assert.equal(options.cacheRetention, "none");
    assert.equal(options.signal, h.signal());
    summarySettings.service_tier = tier === "ultrafast" ? "fast" : "ultrafast";
    const payload = Object.freeze({ model: "test", messages: [] });
    if (tier === undefined || tier === "auto") {
      assert.equal(options.onPayload, undefined, "auto must not install a tier hook");
      assert.equal(Object.hasOwn(options, "service_tier"), false);
    } else {
      assert.deepEqual(await options.onPayload(payload, model), { ...payload, service_tier: tier });
      assert.equal(Object.hasOwn(payload, "service_tier"), false, "do not mutate the provider payload");
      assert.equal((await options.onPayload({ ...payload, service_tier: "provider-default" }, model)).service_tier, tier);
      for (const body of [null, "text", []]) assert.equal(await options.onPayload(body, model), body);
    }
    assert.equal(h.ctx.model.service_tier, undefined);
    h.finish(); await tick(); assert.ok(settle(h));
  });
}

test("summary configuration defaults, explicit provider/model, reload and validation", async t => {
  const dir = await mkdtemp(join(tmpdir(), "occ-summary-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const path = join(dir, "online-compact-settings.json");
  const model = { provider: "main", id: "main" }, other = { provider: "summary", id: "org/model" };
  const ctx = { model, thinkingLevel: "medium", modelRegistry: { find: (p, id) => p === other.provider && id === other.id ? other : undefined } };
  assert.deepEqual(resolveSummarySettings(ctx, path), { model, thinking: "medium", service_tier: "auto" });
  assert.notEqual(resolveSummarySettings(ctx, path).model, model);
  await writeFile(path, JSON.stringify({ model: "summary/org/model", thinking: "high" }));
  assert.deepEqual(resolveSummarySettings(ctx, path), { model: other, thinking: "high", service_tier: "auto" });
  await writeFile(path, JSON.stringify({ model: "auto", thinking: "auto" }));
  ctx.thinkingLevel = "low";
  assert.deepEqual(resolveSummarySettings(ctx, path), { model, thinking: "low", service_tier: "auto" });
  for (const service_tier of ["fast", "ultrafast", "auto"]) {
    await writeFile(path, JSON.stringify({ service_tier }));
    assert.deepEqual(resolveSummarySettings(ctx, path), { model, thinking: "low", service_tier });
  }
  for (const config of ["{", "null", "[]", '{"model":"unknown"}', '{"model":"bad/id"}', '{"thinking":"invalid"}',
    ...["priority", "invalid", 1, true, [], {}].map(service_tier => JSON.stringify({ service_tier }))]) {
    await writeFile(path, config); assert.throws(() => resolveSummarySettings(ctx, path));
  }
});

test("invalid summary configuration fails open with one warning", async () => {
  const h = harness({ resolveSummarySettings: () => { throw new Error("bad config"); } });
  assert.equal(await h.boundary(), undefined); await tick();
  assert.equal(h.requests.length, 0);
  assert.equal(settle(h), undefined);
  assert.equal(h.notices.length, 1);
  assert.equal(h.notices[0][1], "warning");
  assert.match(h.notices[0][0], /未完成（耗时 \d+\.\ds）/);
  assert.doesNotMatch(h.notices[0][0], /tokens|已压缩/);
});

test("update_plan renderers emit no rows for calls and all result states", () => {
  const h = harness();
  const tool = h.tools.get("update_plan");
  assert.equal(tool.renderShell, "self");
  const callComponent = tool.renderCall({ steps }, {});
  assert.deepEqual(callComponent.render(80), []);
  assert.deepEqual(callComponent.render(1), []);
  for (const isPartial of [true, false]) {
    for (const details of [undefined, { boundary: false }, { boundary: true }]) {
      const component = tool.renderResult({ content: [], details, isError: false }, { isPartial }, {});
      assert.deepEqual(component.render(80), []);
      assert.deepEqual(component.render(1), []);
    }
  }
});

for (const event of ["session_start", "session_tree", "session_before_tree", "session_before_switch", "session_before_fork", "model_select", "session_shutdown", "input"]) {
  test(`${event} cancels nested summary and invalidates generation`, async () => {
    const h = harness();
    const pending = h.boundary();
    await h.ready;
    await h.handlers.get(event)(event === "input" ? { streamingBehavior: "steer", text: "CORRECTION: different task" } : {}, h.ctx);
    assert.equal(h.signal().aborted, true);
    h.finish();
    await new Promise(resolve => setImmediate(resolve));
    assert.equal(await pending, undefined);
    assert.equal(await h.handlers.get("turn_end")({ outcome: "completed", message: fauxAssistantMessage("final"), toolResults: [], entries: [] }, h.ctx), undefined);
  });
}
for (const change of ["replaceSession", "replaceModel", "mutateModel"]) test(`${change} without event still rejects obsolete summary`, async () => {
  const h = harness();
  const pending = h.boundary();
  await h.ready;
  h[change](); h.finish();
  await new Promise(resolve => setImmediate(resolve));
  assert.equal(await pending, undefined);
  assert.equal(h.handlers.get("agent_before_settle")({ entries: [], outcome: "completed" }, h.ctx), undefined);
  assert.deepEqual(h.notices, []);
});

test("Worker depth skips all OCC registration, not just execution", () => {
  const previous = process.env.PI_WORKER_DEPTH;
  try {
    process.env.PI_WORKER_DEPTH = "2";
    createOnlineContextCompactExtension()({ on() { assert.fail("worker registered OCC handler"); }, registerTool() { assert.fail("worker registered update_plan"); } });
  } finally { process.env.PI_WORKER_DEPTH = previous; }
});

test("default settings resolver is read-only, respects trust and recursive model overrides", async t => {
  const dir = await mkdtemp(join(tmpdir(), "occ-settings-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  const agent = join(dir, "agent"), project = join(dir, "workspace");
  await mkdir(agent); await mkdir(join(project, ".pi"), { recursive: true });
  const globalPath = join(agent, "settings.json"), localPath = join(project, ".pi/settings.json");
  const global = JSON.stringify({ compaction: { enabled: false, keepRecentTokens: 100, reserveTokens: 200, modelOverrides: { "test/a": { reserveTokens: 500 } } } });
  const local = JSON.stringify({ compaction: { enabled: true, keepRecentTokens: 300, reserveTokens: 400 } });
  await writeFile(globalPath, global); await writeFile(localPath, local);
  const old = process.env.PI_CODING_AGENT_DIR;
  try {
    process.env.PI_CODING_AGENT_DIR = agent;
    const ctx = { cwd: project, model: { provider: "test", id: "a" }, isProjectTrusted: () => false };
    assert.deepEqual(resolveOnlineSettings(ctx).compaction, { enabled: false, keepRecentTokens: 100, reserveTokens: 500 });
    ctx.isProjectTrusted = () => true;
    assert.deepEqual(resolveOnlineSettings(ctx).compaction, { enabled: true, keepRecentTokens: 300, reserveTokens: 500 });
    assert.equal(await readFile(globalPath, "utf8"), global);
    assert.equal(await readFile(localPath, "utf8"), local);
    assert.deepEqual(await readdir(agent), ["settings.json"]);
    await writeFile(localPath, "invalid untrusted settings");
    ctx.isProjectTrusted = () => false;
    assert.equal(resolveOnlineSettings(ctx).compaction.enabled, false);
    ctx.isProjectTrusted = () => true;
    assert.throws(() => resolveOnlineSettings(ctx));
    await writeFile(localPath, JSON.stringify({ compaction: { modelOverrides: { "test/a": { keepRecentTokens: -1 } } } }));
    assert.throws(() => resolveOnlineSettings(ctx), /non-negative|invalid/i);
  } finally {
    if (old === undefined) delete process.env.PI_CODING_AGENT_DIR; else process.env.PI_CODING_AGENT_DIR = old;
  }
});

test("upstream plan/state semantics: transitions, horizon counts, debt repayment, correction and branch restore", () => {
  assert.equal(parsePlanSteps([...steps, steps[0]]), undefined);
  assert.equal(analyzePlanTransition(steps, steps).completedSteps.length, 0);
  let state = initialOnlineState();
  state = recordProviderRequest(state, 8000);
  state = recordProviderRequest(state, 9000);
  state = recordBoundary(state, steps);
  assert.deepEqual(state.completedBoundaryRequestCounts, [2]);
  assert.equal(state.positiveContextDeltaTotal, 1000);
  state = recordCompaction(state, { debtTokens: 1000, repaymentTokens: 400 });
  state = recordProviderRequest(state, 1000);
  assert.equal(state.cacheDebtTokens, 600);
  const sm = SessionManager.inMemory();
  const saved = sm.appendCustomEntry(ONLINE_STATE_ENTRY, state);
  sm.appendCustomEntry(ONLINE_STATE_ENTRY, recordCorrection(state));
  assert.equal(restoreOnlineState(sm.getBranch()).epoch, 2);
  sm.branch(saved);
  assert.equal(restoreOnlineState(sm.getBranch()).epoch, 1);
  sm.appendCustomEntry(ONLINE_STATE_ENTRY, { malformed: true });
  assert.equal(restoreOnlineState(sm.getBranch()).epoch, 1);
});

test("economics keeps upstream saving, subsequent margin and carried debt gates", () => {
  const input = { writeTokens: 10000, archiveTokens: 9000, memoTokens: 1000, contextTokens: 10000,
    completedBoundaryRequestCounts: [2], remainingBoundaries: 20, averageContextTokenIncrement: null,
    contextWindowTokens: 1000000, priorCompactionCount: 0, carriedDebtTokens: 0, cacheDebtRepaymentTokens: 0,
    cacheWriteReadRatio: 12.5, economics: DEFAULT_COMPACTION_ECONOMICS };
  assert.equal(decideCompaction(input).reason, "economic");
  assert.equal(decideCompaction({ ...input, archiveTokens: 500 }).reason, "non_positive_saving");
  assert.equal(decideCompaction({ ...input, priorCompactionCount: 1, remainingBoundaries: 1 }).reason, "deferred_subsequent_margin");
  assert.equal(decideCompaction({ ...input, priorCompactionCount: 1, carriedDebtTokens: 10000000 }).reason, "deferred_carried_debt");
});

test("native preparation preserves tool pairs and cumulative OCC-marked file tracking", () => {
  const sm = SessionManager.inMemory();
  sm.appendMessage({ role: "user", content: "old " + "x".repeat(8000), timestamp: 1 });
  sm.appendMessage(fauxAssistantMessage("done"));
  const kept = sm.appendMessage({ role: "user", content: "new " + "x".repeat(8000), timestamp: 2 });
  sm.appendCompaction("prior checkpoint", kept, 9000, { readFiles: ["old-read.ts"], modifiedFiles: ["old-edit.ts"], preparation: OCC_FILE_TRACKING }, true);
  sm.appendMessage(fauxAssistantMessage(fauxToolCall("read", { path: "next-read.ts" }, { id: "read" }), { stopReason: "toolUse" }));
  sm.appendMessage({ role: "toolResult", toolCallId: "read", toolName: "read", content: [{ type: "text", text: "read" }], isError: false, timestamp: 3 });
  const tool = sm.appendMessage(fauxAssistantMessage(fauxToolCall("write", { path: "last.ts", content: "done" }, { id: "write" }), { stopReason: "toolUse" }));
  sm.appendMessage({ role: "toolResult", toolCallId: "write", toolName: "write", content: [{ type: "text", text: "written".repeat(1000) }], isError: false, timestamp: 4 });
  const prep = prepareCompaction(sm.getBranch(), settings.compaction);
  assert.equal(prep.firstKeptEntryId, tool, "large final result keeps its assistant tool call");
  assert.equal(prep.previousSummary, "prior checkpoint");
  assert.ok(prep.fileOps.read.has("old-read.ts"));
  assert.ok(prep.fileOps.read.has("next-read.ts"));
  assert.ok(prep.fileOps.edited.has("old-edit.ts"));
  assert.ok(!prep.fileOps.written.has("last.ts"), "kept tool is not summarized yet");
});
