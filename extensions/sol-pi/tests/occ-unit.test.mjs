import assert from "node:assert/strict";
import { test } from "node:test";
import { mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { SessionManager } from "@earendil-works/pi-coding-agent";
import { fauxAssistantMessage, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { createOnlineContextCompactExtension } from "../extensions/online-context-compact/extension.ts";
import { resolveOnlineSettings } from "../extensions/online-context-compact/settings.ts";
import { initialOnlineState, recordProviderRequest, recordBoundary, recordCompaction, recordCorrection, restoreOnlineState, ONLINE_STATE_ENTRY } from "../extensions/online-context-compact/state.ts";
import { prepareCompaction, OCC_FILE_TRACKING } from "../extensions/online-context-compact/native-preparation.ts";
import { analyzePlanTransition, parsePlanSteps } from "../extensions/online-context-compact/plan.ts";
import { decideCompaction, DEFAULT_COMPACTION_ECONOMICS } from "../extensions/online-context-compact/economics.ts";

const steps = [{ id: "done", goal: "work", status: "completed" }, { id: "next", goal: "verify", status: "pending" }];
const settings = { compaction: { enabled: true, keepRecentTokens: 150, reserveTokens: 1024 }, retry: { enabled: false, maxRetries: 0, baseDelayMs: 1 } };

function harness() {
  let manager = SessionManager.inMemory();
  manager.appendMessage({ role: "user", content: "history " + "x".repeat(16000), timestamp: 1 });
  manager.appendMessage(fauxAssistantMessage("prior " + "x".repeat(16000)));
  manager.appendMessage({ role: "user", content: "work now", timestamp: 2 });
  const handlers = new Map(), tools = new Map();
  let active = ["update_plan"], nestedSignal, started, finish;
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
    modelRegistry: { streamSimple(_model, _context, options) {
      nestedSignal = options.signal;
      return { result: async () => { started(); await release; return fauxAssistantMessage("Checkpoint"); } };
    } },
  };
  createOnlineContextCompactExtension({ cacheWriteReadRatio: 0, resolveSettings: () => settings })(pi);
  const boundary = async () => {
    handlers.get("turn_start")({}, ctx);
    const message = fauxAssistantMessage(fauxToolCall("update_plan", { steps }, { id: "boundary" }), { stopReason: "toolUse" });
    manager.appendMessage(message);
    const result = await tools.get("update_plan").execute("boundary", { steps }, parent.signal, undefined, ctx);
    const toolResult = { role: "toolResult", toolCallId: "boundary", toolName: "update_plan", ...result, isError: false, timestamp: 3 };
    manager.appendMessage(toolResult);
    return handlers.get("turn_end")({ outcome: "completed", message, toolResults: [toolResult], entries: [], continue: false }, ctx);
  };
  return { handlers, tools, ctx, ready, finish, boundary, signal: () => nestedSignal,
    replaceSession: () => { manager = SessionManager.inMemory(); },
    replaceModel: () => { model = { ...model, id: "changed" }; },
    mutateModel: () => { model.id = "mutated-in-place"; },
  };
}

for (const event of ["session_start", "session_tree", "session_before_tree", "session_before_switch", "session_before_fork", "model_select", "session_shutdown", "input"]) {
  test(`${event} cancels nested summary and invalidates generation`, async () => {
    const h = harness();
    const pending = h.boundary();
    await h.ready;
    await h.handlers.get(event)(event === "input" ? { streamingBehavior: "steer", text: "CORRECTION: different task" } : {}, h.ctx);
    assert.equal(h.signal().aborted, true);
    h.finish();
    assert.equal(await pending, undefined);
    assert.equal(await h.handlers.get("turn_end")({ outcome: "completed", message: fauxAssistantMessage("final"), toolResults: [], entries: [] }, h.ctx), undefined);
  });
}
for (const change of ["replaceSession", "replaceModel", "mutateModel"]) test(`${change} without event still rejects obsolete summary`, async () => {
  const h = harness();
  const pending = h.boundary();
  await h.ready;
  h[change](); h.finish();
  assert.equal(await pending, undefined);
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
  assert.equal(decideCompaction({ ...input, priorCompactionCount: 1, remainingBoundaries: 8 }).reason, "deferred_subsequent_margin");
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
