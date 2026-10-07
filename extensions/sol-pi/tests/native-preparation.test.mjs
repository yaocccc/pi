// Test-only private imports are resolved from the installed package, never production paths.
import assert from "node:assert/strict";
import { test } from "node:test";
import { buildSessionProjection, SessionManager, VERSION } from "@earendil-works/pi-coding-agent";
import { fauxAssistantMessage, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import * as port from "../extensions/online-context-compact/native-preparation.ts";
const native = await import(new URL("./core/compaction/compaction.js", import.meta.resolve("@earendil-works/pi-coding-agent")));
const utils = await import(new URL("./core/compaction/utils.js", import.meta.resolve("@earendil-works/pi-coding-agent")));
const settings = { enabled: true, reserveTokens: 1024, keepRecentTokens: 150 };
const user = content => ({ role: "user", content, timestamp: 1 });
const result = (id, name, extra = {}) => ({ role: "toolResult", toolCallId: id, toolName: name,
  content: [{ type: "text", text: "result" }], isError: false, timestamp: 2, ...extra });
const nested = (id, name, path, status = "ok") => ({ id, name, arguments: { path }, status });

function compare(manager) {
  const entries = manager.getBranch(), original = structuredClone(entries);
  for (const keepRecentTokens of [0, 1, 150, 2000, 20000]) {
    const effective = { ...settings, keepRecentTokens };
    assert.deepEqual(port.prepareCompaction(entries, effective), native.prepareCompaction(entries, effective));
  }
  const projection = buildSessionProjection(entries);
  assert.deepEqual(port.estimateContextTokens(projection.messages), native.estimateContextTokens(projection.messages));
  assert.deepEqual(port.estimateProjectedContextTokens(projection, entries), native.estimateProjectedContextTokens(projection, entries));
  assert.deepEqual(entries, original, "preparation never rewrites history");
}

for (const variant of ["empty", "metadata", "split", "tool-pair", "system", "replacement", "omission",
  "previous", "retain-none", "compaction-leaf", "unknown-hook", "nested", "nested-omitted", "nested-replaced"]) {
  test(`Pi 1.0.2 preparation differential: ${variant}`, () => {
    assert.equal(VERSION, "1.0.4");
    assert.equal(port.NATIVE_PREPARATION_VERSION, "1.0.2"); // Port baseline; also checked against the installed 1.0.4 implementation.
    const sm = SessionManager.inMemory();
    if (variant === "empty") return compare(sm);
    sm.appendCustomEntry("metadata", {});
    if (variant === "metadata") return compare(sm);
    if (variant === "system") sm.appendMessage({ role: "system", content: "", sections: { preamble: "old prompt" }, timestamp: 1 });
    sm.appendMessage(user("history " + "x".repeat(8000)));
    sm.appendMessage(fauxAssistantMessage("prior answer"));
    const kept = sm.appendMessage(user("current " + "y".repeat(8000)));
    if (["previous", "retain-none", "compaction-leaf", "unknown-hook"].includes(variant)) {
      sm.appendCompaction("previous summary", variant === "retain-none" ? null : kept, 9000,
        { readFiles: ["old-read.ts"], modifiedFiles: ["old-edit.ts"] }, variant === "unknown-hook");
    }
    if (variant === "compaction-leaf") return compare(sm);
    const call = sm.appendMessage(fauxAssistantMessage(fauxToolCall("read", { path: "direct.ts" }, { id: "call" }), { stopReason: "toolUse" }));
    const nestedCalls = { complete: false, calls: [nested("call/1", "read", "nested-read.ts"), nested("call/2", "edit", "nested-edit.ts"),
      nested("call/3", "write", "nested-write.ts", "error"), { id: "call/4", name: "read", argumentsBytes: 9000, status: "ok" }] };
    const res = sm.appendMessage(result("call", "read", variant.startsWith("nested") ? { nestedCalls }
      : variant === "tool-pair" ? { content: [{ type: "text", text: "large result ".repeat(1500) }] } : {}));
    if (variant === "replacement") sm.appendContextEdit(call, { content: [fauxToolCall("write", { path: "replacement.ts" }, { id: "call" })] });
    if (variant === "omission") { sm.appendContextEdit(call, null); sm.appendContextEdit(res, null); }
    if (variant === "nested-omitted") sm.appendContextEdit(res, null);
    if (variant === "nested-replaced") sm.appendContextEdit(res, { content: "edited result" });
    if (variant === "system") sm.appendMessage({ role: "system", content: "", sections: { preamble: "new prompt" }, timestamp: 3 });
    sm.appendMessage(fauxAssistantMessage("tail " + "z".repeat(4000)));
    compare(sm);
  });
}

for (const variant of ["omitted-attempt", "metadata-only", "visible-custom", "input-replaced", "omitted-replaced"]) {
  test(`Pi 1.0.2 recovery suffix differential: ${variant}`, () => {
    const sm = SessionManager.inMemory();
    sm.appendMessage(user("earlier " + "x".repeat(8000)));
    sm.appendMessage(fauxAssistantMessage("answer"));
    const input = sm.appendMessage(user("oversized unsent input " + "y".repeat(16000)));
    if (variant !== "metadata-only") {
      const attempt = sm.appendMessage(fauxAssistantMessage("abandoned", { stopReason: "error" }));
      if (variant === "omitted-replaced") sm.appendContextEdit(attempt, { content: "replaced before omission" });
      sm.appendContextEdit(attempt, null);
    }
    sm.appendCustomEntry("bookkeeping", {});
    if (variant === "visible-custom") sm.appendCustomMessageEntry("new-input", "do not discard", false);
    if (variant === "input-replaced") sm.appendContextEdit(input, { content: "new input " + "z".repeat(16000) });
    compare(sm);
    const prep = port.prepareCompaction(sm.getBranch(), settings);
    assert.ok(prep);
    if (["omitted-attempt", "omitted-replaced"].includes(variant)) assert.notEqual(prep.firstKeptEntryId, input);
    else assert.equal(prep.firstKeptEntryId, input, "metadata and changed input cannot authorize suffix advancement");
  });
}

for (const variant of ["valid", "aborted", "error", "zero", "edited", "compacted", "fresh-after-edit"]) {
  test(`Pi 1.0.2 usage invalidation differential: ${variant}`, () => {
    const sm = SessionManager.inMemory();
    const input = sm.appendMessage(user("history " + "x".repeat(8000)));
    const answer = fauxAssistantMessage("answer", { stopReason: ["aborted", "error"].includes(variant) ? variant : "stop" });
    answer.usage = { ...answer.usage, input: variant === "zero" ? 0 : 12345, totalTokens: variant === "zero" ? 0 : 12345 };
    sm.appendMessage(answer);
    if (["edited", "fresh-after-edit"].includes(variant)) sm.appendContextEdit(input, { content: "shorter" });
    if (variant === "compacted") sm.appendCompaction("summary", input, 12000);
    if (variant === "fresh-after-edit") sm.appendMessage(answer);
    sm.appendMessage(user("tail"));
    compare(sm);
    const estimate = port.estimateProjectedContextTokens(buildSessionProjection(sm.getBranch()), sm.getBranch());
    assert.equal(estimate.lastUsageIndex !== null, ["valid", "fresh-after-edit"].includes(variant));
  });
}

test("Pi 1.0.2 nested file operations match native extraction including bounded/failed calls", () => {
  const messages = [result("root", "codemode", { nestedCalls: { complete: false, calls: [
    nested("root/1", "read", "read.ts"), nested("root/2", "write", "write.ts", "error"), nested("root/3", "edit", "edit.ts", "unfinished"),
    nested("root/4", "bash", "not-a-file.ts"), nested("root/5", "read", ""), { id: "root/6", name: "read", status: "ok", argumentsBytes: 9000 },
  ] } }), fauxAssistantMessage(fauxToolCall("read", { path: "direct.ts" }))];
  const actual = port.createFileOps(), expected = utils.createFileOps();
  for (const message of messages) { port.extractFileOpsFromMessage(message, actual); utils.extractFileOpsFromMessage(message, expected); }
  assert.deepEqual(actual, expected);
  assert.deepEqual([...actual.read], ["read.ts", "direct.ts"]);
  assert.deepEqual([...actual.written], ["write.ts"]);
  assert.deepEqual([...actual.edited], ["edit.ts"]);
});

test("legacy OCC file marker remains readable; only marked hooks extend native behavior", () => {
  assert.equal(port.OCC_FILE_TRACKING, "sol-pi-occ-native-0.87.1", "persisted marker is a format tag, not the host version");
  const sm = SessionManager.inMemory();
  const kept = sm.appendMessage(user("old " + "x".repeat(8000)));
  sm.appendCompaction("legacy OCC", kept, 9000, { preparation: port.OCC_FILE_TRACKING,
    readFiles: ["legacy.ts"], modifiedFiles: ["legacy-edit.ts"] }, true);
  sm.appendMessage(user("tail " + "y".repeat(4000)));
  const actual = port.prepareCompaction(sm.getBranch(), settings);
  const expected = native.prepareCompaction(sm.getBranch(), settings);
  expected.fileOps.read.add("legacy.ts"); expected.fileOps.edited.add("legacy-edit.ts");
  assert.deepEqual(actual, expected);
});
