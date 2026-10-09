import test from "node:test";
import assert from "node:assert/strict";
import { buildSessionProjection, SessionManager } from "@earendil-works/pi-coding-agent";
import { fauxAssistantMessage } from "@earendil-works/pi-ai/providers/faux";
import { estimateCompactionPricing } from "../extensions/online-context-compact/pricing.ts";
import { prepareCompaction } from "../extensions/online-context-compact/native-preparation.ts";
import { decideCompaction, DEFAULT_COMPACTION_ECONOMICS } from "../extensions/online-context-compact/economics.ts";

const model = { provider: "openai-codex", id: "same-model", api: "openai-codex-responses" };
const settings = { enabled: true, reserveTokens: 16384, keepRecentTokens: 150 };
function assistant(id, reasoning, totalTokens = 518359) {
  const message = fauxAssistantMessage("answer");
  Object.assign(message, { provider: model.provider, api: model.api, model: model.id, timestamp: id.length });
  if (reasoning) message.content = [0, 1].map(n => ({ type: "thinking", thinking: "hint",
    thinkingSignature: JSON.stringify({ type: "reasoning", id: `${id}-${n}`, encrypted_content: "opaque-not-tokenizable" }) }));
  message.usage = { ...message.usage, reasoning, output: reasoning + 20, input: totalTokens - reasoning - 20, totalTokens };
  return message;
}
function fixture() {
  const sm = SessionManager.inMemory();
  sm.appendMessage({ role: "user", content: "x".repeat(134000 * 4), timestamp: 1 });
  sm.appendMessage(assistant("old", 190000));
  sm.appendMessage({ role: "user", content: "tail ".repeat(200), timestamp: 2 });
  sm.appendMessage(assistant("kept", 50000));
  sm.appendMessage(assistant("latest", 0));
  const branch = sm.getBranch();
  return { sm, branch, preparation: prepareCompaction(branch, settings),
    observedMessages: buildSessionProjection(branch).messages, model };
}
const price = input => estimateCompactionPricing(input);

test("usage-anchored pricing includes replayed reasoning once per assistant, not once per item", () => {
  const input = fixture();
  const before = structuredClone({ branch: input.branch, observed: input.observedMessages, prep: input.preparation });
  const p = price(input);
  assert.equal(p.reasoningStatus, "available");
  assert.equal(p.reasoningArchiveTokens, 190000 - 2, "visible thinking is not charged twice");
  assert.equal(p.observedReasoningTokens, 240000 - 4);
  assert.equal(p.archiveTokens, p.visibleArchiveTokens + p.reasoningArchiveTokens);
  assert.equal(p.writeTokens, 518359);
  assert.equal(p.matchedReasoningMessages, 1, "retained reasoning earns no savings");
  assert.equal(p.reasoningCreditScale, 1);
  assert.deepEqual({ branch: input.branch, observed: input.observedMessages, prep: input.preparation }, before);
  const economics = { writeTokens: p.writeTokens, archiveTokens: p.archiveTokens, memoTokens: 1000,
    contextTokens: p.writeTokens, completedBoundaryRequestCounts: [12, 21, 19], remainingBoundaries: 1,
    averageContextTokenIncrement: null, contextWindowTokens: 800000, priorCompactionCount: 1,
    requestsSinceLastCompaction: 20, carriedDebtTokens: 0, cacheDebtRepaymentTokens: 0,
    cacheWriteReadRatio: 12.5, economics: DEFAULT_COMPACTION_ECONOMICS };
  assert.equal(decideCompaction({ ...economics, archiveTokens: p.visibleArchiveTokens }).compact, false);
  assert.equal(decideCompaction(economics).reason, "economic");
  assert.ok(decideCompaction(economics).postCompactionTokens < 200000);
});

test("ciphertext size never serves as a token estimate", () => {
  const input = fixture(), original = price(input);
  for (const messages of [input.preparation.messagesToSummarize, input.preparation.turnPrefixMessages, input.observedMessages]) {
    for (const m of messages) if (m.role === "assistant" && m.usage.reasoning === 190000) {
      for (const block of m.content) {
        const item = JSON.parse(block.thinkingSignature); item.encrypted_content = "z".repeat(200000);
        block.thinkingSignature = JSON.stringify(item);
      }
    }
  }
  assert.equal(price(input).reasoningArchiveTokens, original.reasoningArchiveTokens);
});

for (const variant of ["missing_projection", "empty_projection", "omitted", "rewritten", "duplicate", "duplicate_source", "wrong_model",
  "wrong_api", "unsupported_api", "missing_usage", "invalid_usage", "invalid_signature", "unsigned", "partial_unsigned", "duplicate_signature", "error_response"]) {
  test(`unproven reasoning earns no credit: ${variant}`, () => {
    const input = fixture();
    const old = messages => messages.find(m => m.role === "assistant" && m.usage.reasoning === 190000);
    const source = old([...input.preparation.messagesToSummarize, ...input.preparation.turnPrefixMessages]);
    const projected = old(input.observedMessages);
    if (variant === "missing_projection") input.observedMessages = undefined;
    if (variant === "empty_projection") input.observedMessages = [];
    if (variant === "omitted") input.observedMessages = input.observedMessages.filter(m => m !== projected);
    if (variant === "rewritten") projected.content = [{ type: "text", text: "replaced" }];
    if (variant === "duplicate") input.observedMessages.push(structuredClone(projected));
    if (variant === "duplicate_source") input.preparation.messagesToSummarize.push(structuredClone(source));
    if (variant === "wrong_model") input.model = { ...model, id: "changed" };
    if (variant === "wrong_api") projected.api = "openai-responses";
    if (variant === "unsupported_api") input.model = { ...model, api: "anthropic-messages" };
    if (variant === "missing_usage") delete source.usage.reasoning;
    if (variant === "invalid_usage") source.usage.reasoning = source.usage.output + 1;
    if (variant === "invalid_signature") source.content[0].thinkingSignature = "invalid";
    if (variant === "unsigned") for (const b of source.content) delete b.thinkingSignature;
    if (variant === "partial_unsigned") delete source.content[0].thinkingSignature;
    if (variant === "duplicate_signature") source.content[1].thinkingSignature = source.content[0].thinkingSignature;
    if (variant === "error_response") source.stopReason = "error";
    assert.equal(price(input).reasoningArchiveTokens, 0);
  });
}

test("current usage budgets hidden costs across removed AND retained reasoning", () => {
  const input = fixture();
  const initial = price(input);
  const total = initial.observedVisibleTokens + 100;
  const latest = input.branch.findLast(e => e.type === "message" && e.message.role === "assistant");
  latest.message.usage.totalTokens = total;
  input.preparation = prepareCompaction(input.branch, settings);
  const p = price(input);
  assert.equal(p.reasoningBudgetTokens, 100);
  assert.ok(p.reasoningCreditScale > 0 && p.reasoningCreditScale < 1);
  assert.equal(p.reasoningArchiveTokens, Math.floor((190000 - 2) * 100 / (240000 - 4)));
  assert.ok(p.reasoningArchiveTokens < 100, "do not allocate retained reasoning's budget to the prefix");
  assert.ok(p.archiveTokens <= p.writeTokens);
});

test("usage invalidated by a context edit cannot finance opaque-reasoning savings", () => {
  const input = fixture();
  const latest = input.branch.findLast(e => e.type === "message" && e.message.role === "assistant");
  input.sm.appendContextEdit(latest.id, { content: "changed" });
  input.branch = input.sm.getBranch();
  input.preparation = prepareCompaction(input.branch, settings);
  const p = price(input);
  assert.equal(p.reasoningStatus, "no_current_usage");
  assert.equal(p.reasoningArchiveTokens, 0);
  assert.equal(p.contextTokenSource, "visible_estimate");
});
