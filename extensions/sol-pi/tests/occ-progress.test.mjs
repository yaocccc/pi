import assert from "node:assert/strict";
import { test } from "node:test";
import { analyzePlanTransition } from "../extensions/online-context-compact/plan.ts";
import { initialOnlineState, recordBoundary, recordCompaction, recordCompletedPlanHandoff,
  recordCorrection, recordProviderRequest, restoreOnlineState, ONLINE_STATE_ENTRY } from "../extensions/online-context-compact/state.ts";
import { decideCompaction, DEFAULT_COMPACTION_ECONOMICS } from "../extensions/online-context-compact/economics.ts";

const open = [
  { id: "build", goal: "build", status: "in_progress" },
  { id: "test", goal: "test", status: "pending" },
];
const partial = open.map((s, i) => i === 0 ? { ...s, status: "completed" } : s);
const completed = open.map(s => ({ ...s, status: "completed" }));
const progress = { stepId: "build", goal: "build", filesChanged: ["a.ts"], verification: ["tests"], decisions: [], nextWork: ["test"] };
const entry = data => ({ type: "custom", customType: ONLINE_STATE_ENTRY, data });

function history() {
  let state = { ...initialOnlineState(), plan: open };
  state = recordProviderRequest(state, 1000);
  state = recordProviderRequest(state, 1200);
  state = recordBoundary(state, partial, progress);
  return recordProviderRequest(state, 1500);
}

test("only registered unfinished-to-completed transitions count; identical and rekeyed history are idempotent", () => {
  assert.deepEqual(analyzePlanTransition([], partial).completedSteps, []);
  assert.deepEqual(analyzePlanTransition(open, partial).completedSteps, [partial[0]]);
  assert.deepEqual(analyzePlanTransition(partial, partial).completedSteps, []);
  const rekeyed = partial.map(s => ({ ...s, id: `new-${s.id}`, goal: `reworded ${s.goal}` }));
  assert.deepEqual(analyzePlanTransition(partial, rekeyed).completedSteps, []);
  assert.deepEqual(analyzePlanTransition(rekeyed, rekeyed).completedSteps, []);
  assert.deepEqual(analyzePlanTransition(partial, completed).completedSteps, [completed[1]]);
  const corrected = recordCorrection(history());
  assert.deepEqual(analyzePlanTransition(corrected.plan, partial).completedSteps, []);
});

test("compaction preserves boundary and growth prediction history, but does not compare across summaries", () => {
  const before = history();
  const after = recordCompaction(before, { debtTokens: 1000, repaymentTokens: 100 });
  assert.deepEqual(after.plan, partial);
  assert.deepEqual(after.pendingProgress, []);
  assert.equal(after.lastCompactionRequestCount, 3);
  assert.equal(after.lastBoundaryRequestCount, 2, "manual compaction need not coincide with a boundary");
  assert.deepEqual(after.completedBoundaryRequestCounts, [2]);
  assert.equal(after.positiveContextDeltaTotal, 500);
  assert.equal(after.positiveContextDeltaCount, 2);
  assert.equal(after.lastContextTokens, null);
  let next = recordProviderRequest(after, 5000);
  assert.equal(next.positiveContextDeltaTotal, 500, "summary replacement isn't a growth sample");
  assert.equal(next.positiveContextDeltaCount, 2);
  next = recordProviderRequest(next, 5500);
  next = recordBoundary(next, completed);
  assert.equal(next.positiveContextDeltaTotal, 1000);
  assert.equal(next.positiveContextDeltaCount, 3);
  assert.deepEqual(next.completedBoundaryRequestCounts, [2, 3]);
  assert.deepEqual(restoreOnlineState([entry(next)]), next);
});

test("legacy persisted snapshots default missing compaction timestamp to null and ignore obsolete restatement flags", () => {
  const current = recordCompaction(history(), { debtTokens: 1000, repaymentTokens: 100 });
  const { lastCompactionRequestCount, ...legacy } = current;
  assert.deepEqual(restoreOnlineState([entry({ ...legacy, awaitingPlanRestatement: true })]), {
    ...current, lastCompactionRequestCount: null,
  });
  assert.deepEqual(restoreOnlineState([entry(current)]), current);
  for (const invalid of [-1, 0.5, "3", 4]) {
    assert.deepEqual(restoreOnlineState([entry(current), entry({ ...current, lastCompactionRequestCount: invalid })]), current);
  }
});

test("completed-plan handoff resets task horizon, retains cumulative debt/cooldown and leaves active/empty plans alone", () => {
  const compacted = recordCompaction(history(), { debtTokens: 10000, repaymentTokens: 100 });
  assert.equal(recordCompletedPlanHandoff(compacted), compacted);
  const before = { ...compacted, plan: completed, pendingProgress: [progress] };
  const next = recordCompletedPlanHandoff(before);
  assert.equal(next.epoch, before.epoch + 1);
  assert.deepEqual(next.plan, []);
  assert.deepEqual(next.pendingProgress, []);
  assert.deepEqual(next.completedBoundaryRequestCounts, []);
  assert.equal(next.lastBoundaryRequestCount, before.requestCount);
  assert.equal(next.lastContextTokens, null);
  assert.equal(next.positiveContextDeltaTotal, 0);
  assert.equal(next.positiveContextDeltaCount, 0);
  for (const field of ["requestCount", "nativeCompactionCount", "lastCompactionRequestCount", "cacheDebtTokens", "cacheDebtRepaymentTokens"]) {
    assert.equal(next[field], before[field], `${field} survives handoff`);
  }
  assert.equal(recordCompletedPlanHandoff(next), next);
  const requested = recordProviderRequest(next, 2000);
  assert.equal(requested.cacheDebtTokens, 9900);
  assert.equal(requested.positiveContextDeltaCount, 0);
});

const economic = {
  writeTokens: 10000, archiveTokens: 9000, memoTokens: 1000, contextTokens: 10000,
  completedBoundaryRequestCounts: [2], remainingBoundaries: 20, averageContextTokenIncrement: null,
  contextWindowTokens: 1000000, priorCompactionCount: 1, carriedDebtTokens: 0,
  cacheDebtRepaymentTokens: 0, cacheWriteReadRatio: 12.5, economics: DEFAULT_COMPACTION_ECONOMICS,
};

test("economic cooldown blocks requests 0/1, opens at 2, and defaults unknown legacy timestamps safely", () => {
  for (const requestsSinceLastCompaction of [0, 1]) {
    const result = decideCompaction({ ...economic, requestsSinceLastCompaction });
    assert.equal(result.compact, false);
    assert.equal(result.reason, "deferred_post_compaction_cooldown");
    assert.equal(result.requestsSinceLastCompaction, requestsSinceLastCompaction);
  }
  for (const requestsSinceLastCompaction of [2, 3, null, undefined]) {
    const result = decideCompaction({ ...economic, requestsSinceLastCompaction });
    assert.equal(result.compact, true);
    assert.equal(result.reason, "economic");
  }
  const window = decideCompaction({ ...economic, requestsSinceLastCompaction: 0, contextWindowTokens: 10001 });
  assert.equal(window.compact, true);
  assert.equal(window.reason, "window_protection");
  assert.equal(decideCompaction({ ...economic, requestsSinceLastCompaction: 0, archiveTokens: 1000 }).reason, "non_positive_saving");
});
