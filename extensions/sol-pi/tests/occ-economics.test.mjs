import assert from "node:assert/strict";
import { test } from "node:test";
import { buildSessionProjection, estimateTokens, SessionManager } from "@earendil-works/pi-coding-agent";
import { fauxAssistantMessage, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { decideCompaction, DEFAULT_COMPACTION_ECONOMICS } from "../extensions/online-context-compact/economics.ts";
import { initialOnlineState, recordCompaction, recordProviderRequest, restoreOnlineState, ONLINE_STATE_ENTRY } from "../extensions/online-context-compact/state.ts";
import { prepareCompaction } from "../extensions/online-context-compact/native-preparation.ts";
import { estimateNativeCompactionTokens } from "../extensions/online-context-compact/projection.ts";

const input = { writeTokens: 10000, archiveTokens: 9000, memoTokens: 1000, contextTokens: 10000,
  completedBoundaryRequestCounts: [2], remainingBoundaries: 20, averageContextTokenIncrement: null,
  contextWindowTokens: 1000000, priorCompactionCount: 0, carriedDebtTokens: 0, cacheDebtRepaymentTokens: 0,
  cacheWriteReadRatio: 12.5, economics: DEFAULT_COMPACTION_ECONOMICS };
const settings = { enabled: true, keepRecentTokens: 150, reserveTokens: 1024 };
const user = content => ({ role: "user", content, timestamp: 1 });
const toolResult = (content, toolCallId = "tool") => ({ role: "toolResult", toolCallId, toolName: "read",
  content: [{ type: "text", text: content }], isError: false, timestamp: 2 });

test("cache-write cost prices retained context plus estimated memo, not pre-compaction context", () => {
  const result = decideCompaction(input);
  assert.equal(result.postCompactionTokens, 2000);
  assert.equal(result.breakevenRequests, 23000 / 8000);
  assert.equal(result.combinedBreakevenRequests, result.breakevenRequests);
  assert.equal(decideCompaction({ ...input, writeTokens: 1000 }).postCompactionTokens, 0);
  for (const cacheWriteReadRatio of [0, 1]) {
    assert.equal(decideCompaction({ ...input, cacheWriteReadRatio }).breakevenRequests, 0);
  }
  assert.equal(decideCompaction({ ...input, cacheWriteReadRatio: null }).reason, "cache_ratio_unavailable");
  assert.equal(decideCompaction({ ...input, archiveTokens: 1000 }).reason, "non_positive_saving");
});

test("combined breakeven uses outstanding debt and cumulative per-request repayment", () => {
  const next = { ...input, priorCompactionCount: 1, carriedDebtTokens: 80000, remainingBoundaries: 2 };
  assert.equal(decideCompaction(next).reason, "deferred_carried_debt");
  const result = decideCompaction({ ...next, cacheDebtRepaymentTokens: 16000 });
  assert.equal(result.breakevenRequests, 23000 / 8000, "margin gate still uses only the new investment");
  assert.equal(result.combinedBreakevenRequests, 103000 / 24000);
  assert.equal(result.reason, "economic");
  assert.equal(decideCompaction({ ...input, priorCompactionCount: 1, remainingBoundaries: 1 }).reason, "deferred_subsequent_margin");
  assert.equal(decideCompaction({ ...next, contextWindowTokens: 10001 }).reason, "window_protection");
});

test("new compaction adds to remaining debt and repayment; repayments reset only when paid off", () => {
  const first = recordCompaction(initialOnlineState(), { debtTokens: 1000, repaymentTokens: 100 });
  const paid = recordProviderRequest(first, 5000);
  const second = recordCompaction(paid, { debtTokens: 600, repaymentTokens: 200 });
  assert.equal(second.cacheDebtTokens, 1500);
  assert.equal(second.cacheDebtRepaymentTokens, 300);
  assert.equal(second.nativeCompactionCount, 2);
  assert.equal(second.epoch, 2);
  assert.deepEqual(second.plan, paid.plan, "compaction preserves the current plan");
  let current = second;
  for (let n = 1; n <= 5; n++) {
    current = recordProviderRequest(current, 1000);
    assert.equal(current.cacheDebtTokens, 1500 - n * 300);
    assert.equal(current.cacheDebtRepaymentTokens, n === 5 ? 0 : 300);
  }
  const manager = SessionManager.inMemory();
  const checkpoint = manager.appendCustomEntry(ONLINE_STATE_ENTRY, second);
  manager.appendCustomEntry(ONLINE_STATE_ENTRY, current);
  manager.branch(checkpoint);
  assert.deepEqual(restoreOnlineState(manager.getBranch()), second);
  const manual = recordCompaction(second, { debtTokens: 0, repaymentTokens: 0 });
  assert.equal(manual.cacheDebtTokens, 1500, "unknown/manual cost must not erase existing debt");
  assert.equal(manual.cacheDebtRepaymentTokens, 300);
  const negative = recordCompaction(second, { debtTokens: -1, repaymentTokens: -1 });
  assert.equal(negative.cacheDebtTokens, 1500);
  assert.equal(negative.cacheDebtRepaymentTokens, 300);
});

test("actual prefix excludes a huge retained tool pair even when keepRecentTokens is tiny", () => {
  const manager = SessionManager.inMemory();
  const old = user("small history");
  manager.appendMessage(old);
  const kept = manager.appendMessage(fauxAssistantMessage(fauxToolCall("read", { path: "large.txt" }, { id: "tool" }), { stopReason: "toolUse" }));
  manager.appendMessage(toolResult("x".repeat(60000)));
  const prep = prepareCompaction(manager.getBranch(), settings);
  assert.equal(prep.firstKeptEntryId, kept);
  assert.equal(prep.isSplitTurn, true);
  assert.equal(estimateNativeCompactionTokens(prep), estimateTokens(old));
  assert.ok(prep.tokensBefore - settings.keepRecentTokens > 10000, "old subtraction would claim false savings");
  assert.equal(decideCompaction({ ...input, archiveTokens: estimateNativeCompactionTokens(prep) }).reason, "non_positive_saving");
});

test("native prefix includes split-turn messages and one previous summary, never system replay or kept tail", () => {
  const manager = SessionManager.inMemory();
  const kept = manager.appendMessage(user("old turn " + "x".repeat(8000)));
  manager.appendCompaction("previous checkpoint", kept, 9000);
  manager.appendMessage({ role: "system", content: "fixed ".repeat(1000), timestamp: 2 });
  manager.appendMessage(fauxAssistantMessage(fauxToolCall("read", { path: "large.txt" }, { id: "tool" }), { stopReason: "toolUse" }));
  manager.appendMessage(toolResult("y".repeat(16000)));
  const prep = prepareCompaction(manager.getBranch(), settings);
  const expected = [...prep.messagesToSummarize, ...prep.turnPrefixMessages].reduce((sum, m) => sum + estimateTokens(m), Math.ceil("previous checkpoint".length / 4));
  assert.ok(prep.isSplitTurn);
  assert.equal(estimateNativeCompactionTokens(prep), expected);
  assert.equal(estimateNativeCompactionTokens(prep, buildSessionProjection(manager.getBranch()).messages), expected);
  assert.ok(expected < 3000);
});

test("persisted edits and omissions affect native removable messages before pricing", () => {
  const manager = SessionManager.inMemory();
  const first = manager.appendMessage(user("x".repeat(16000)));
  const second = manager.appendMessage(fauxAssistantMessage("y".repeat(16000)));
  manager.appendMessage(user("tail".repeat(1000)));
  manager.appendContextEdit(first, { content: "replacement" });
  manager.appendContextEdit(second, null);
  const prep = prepareCompaction(manager.getBranch(), settings);
  assert.equal(estimateNativeCompactionTokens(prep), estimateTokens(user("replacement")));
});

test("OP projection earns only placeholder savings without changing native summary inputs", () => {
  const manager = SessionManager.inMemory();
  const original = toolResult("x".repeat(60000));
  manager.appendMessage(user("old"));
  manager.appendMessage(original);
  manager.appendMessage(user("tail ".repeat(1000)));
  const prep = prepareCompaction(manager.getBranch(), settings);
  const before = structuredClone(prep);
  const observed = buildSessionProjection(manager.getBranch()).messages.map(m => m.role === "toolResult" ? { ...m, content: [{ type: "text", text: "[obs placeholder]" }] } : m);
  assert.equal(estimateNativeCompactionTokens(prep, observed), estimateTokens(user("old")) + estimateTokens(observed[1]));
  assert.ok(estimateNativeCompactionTokens(prep) > 15000);
  assert.deepEqual(prep, before, "pricing never replaces native summarization source data");
  assert.equal(estimateNativeCompactionTokens(prep, []), 0, "unproven/omitted savings are zero");
  assert.equal(estimateNativeCompactionTokens(prep, [...observed, observed[1]]), estimateTokens(user("old")), "ambiguous duplicate tool identity earns no savings");
});

test("an edit after the observed request cannot earn stale large tool-result savings", () => {
  const manager = SessionManager.inMemory();
  manager.appendMessage(user("old"));
  const id = manager.appendMessage(toolResult("x".repeat(60000)));
  manager.appendMessage(user("tail ".repeat(1000)));
  const observed = buildSessionProjection(manager.getBranch()).messages;
  manager.appendContextEdit(id, { content: "shortened again" });
  const prep = prepareCompaction(manager.getBranch(), settings);
  assert.equal(estimateNativeCompactionTokens(prep, observed), estimateNativeCompactionTokens(prep));
  assert.ok(estimateNativeCompactionTokens(prep, observed) < 10);
});
