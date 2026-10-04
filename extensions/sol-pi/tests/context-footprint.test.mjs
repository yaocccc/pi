// SDK-independent: only Node built-ins and observation.ts (whose SDK imports are type-only).
// Run: node --experimental-transform-types --test extensions/sol-pi/tests/context-footprint.test.mjs
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import {
  countLines,
  createObservation,
  estimateTokens,
  FULL_SENDS,
  PLACEHOLDER_EXCERPT_BYTES,
  placeholderFor,
  THRESHOLD_BYTES,
} from "../extensions/observation-pack/observation.ts";

const utf8Bytes = (text) => Buffer.byteLength(text, "utf8");
const packSource = await readFile(new URL("../extensions/observation-pack/index.ts", import.meta.url), "utf8");
// Measured from /tmp/pi-extensions-baseline-M3Zc8z before the wording-only change.
// These are UTF-8 bytes, NOT token counts. No baseline directory is needed to run the tests.
const baselineBytes = { typicalPlaceholder: 1425 };
const typicalLines = Array.from({ length: 400 }, (_, i) => `compile ${String(i).padStart(4, "0")}: ok 编译 ✓\n`);

function messageFor(text) {
  return {
    role: "toolResult",
    toolName: "bash",
    toolCallId: "context-footprint",
    isError: false,
    content: [{ type: "text", text }],
  };
}

function observationFor(text) {
  // createObservation is pure: this path is never created or read.
  const observation = createObservation(messageFor(text), "/unused-context-footprint");
  assert.ok(observation);
  return observation;
}

function excerptsFrom(packed) {
  const match = packed.match(/\[first whole lines, <=512 bytes\]\n([\s\S]*?)\n\[middle omitted; last whole lines, <=512 bytes\]\n([\s\S]*?)\n\[end excerpts\]$/u);
  assert.ok(match, "explicit whole-line budgets, omitted middle and excerpt end remain visible");
  return { head: match[1], tail: match[2] };
}

test("typical placeholder retains metadata, paged retrieval and first-full-send semantics", (t) => {
  assert.equal(FULL_SENDS, 2);
  assert.equal(PLACEHOLDER_EXCERPT_BYTES, 1024);
  // Static guard only; SDK-dependent projection lifecycle tests remain in the full suite.
  assert.match(packSource, /if \(previousSends < FULL_SENDS\) \{[\s\S]*?sentCounts\.set\(sendCountKey, previousSends \+ 1\);\s*continue;/u);
  const text = typicalLines.join("");
  const observation = observationFor(text);
  const packed = placeholderFor(observation);
  assert.equal(observation.bytes, 11200);
  assert.equal(observation.lines, 400);
  assert.equal(observation.tokens, estimateTokens(text));
  const header = packed.split("\n");
  assert.deepEqual(header.slice(0, 6), [
    `[full for first ${FULL_SENDS} provider requests; now packed]`,
    `id: ${observation.id}`,
    "tool: bash",
    `original_bytes: ${utf8Bytes(text)}`,
    `original_lines: ${countLines(text)}`,
    `estimated_tokens: ${estimateTokens(text)}`,
  ]);
  const retrieve = header[6].match(/^retrieve: obs_recall (\{.*\}); continue with next_offset$/u);
  assert.ok(retrieve);
  assert.deepEqual(JSON.parse(retrieve[1]), { id: observation.id, offset: 0 });
  const { head, tail } = excerptsFrom(packed);
  assert.equal(head, typicalLines.slice(0, 18).join(""));
  assert.equal(tail, typicalLines.slice(-18).join(""));
  assert.equal(utf8Bytes(head), 504);
  assert.equal(utf8Bytes(tail), 504);
  assert.equal(placeholderFor(observation), packed, "placeholder remains stable");
  const bytes = utf8Bytes(packed);
  assert.ok(bytes <= 1360, "typical placeholder byte budget");
  assert.ok(bytes - utf8Bytes(head) - utf8Bytes(tail) <= 352, "metadata/labels byte budget");
  assert.ok(bytes < baselineBytes.typicalPlaceholder);
  t.diagnostic(`UTF-8 bytes (not tokens): typical placeholder ${baselineBytes.typicalPlaceholder} -> ${bytes}; saved ${baselineBytes.typicalPlaceholder - bytes}; fixture=${observation.bytes} bytes/${observation.lines} lines`);
});

const exactLine = `${"界".repeat(169)}🙂\n`; // 507 + 4 + 1 = 512 UTF-8 bytes.
const oversizedLine = `${"界".repeat(170)}🙂\n`; // 515 bytes, despite only 173 UTF-16 code units.
const crlfLine = `${"界".repeat(84)}🙂\r\n`; // 258 bytes: two complete lines will not fit.
const middle = `${"m".repeat(THRESHOLD_BYTES + 1)}\n`;
const boundaryCases = [
  { name: "exact 512-byte UTF-8 lines", start: exactLine, end: exactLine, head: exactLine, tail: exactLine },
  { name: "oversized boundary lines are not sliced or skipped", start: oversizedLine, end: oversizedLine, head: "", tail: "" },
  { name: "unused tail budget is not borrowed", start: exactLine, end: oversizedLine, head: exactLine, tail: "" },
  { name: "unused head budget is not borrowed", start: oversizedLine, end: exactLine, head: "", tail: exactLine },
  { name: "UTF-8 cumulative budget and intact CRLF", start: crlfLine.repeat(2), end: crlfLine.repeat(2), head: crlfLine, tail: crlfLine },
  { name: "unterminated final line is preserved", start: "first\n", end: exactLine.slice(0, -1), head: "first\n", tail: exactLine.slice(0, -1) },
];

for (const fixture of boundaryCases) {
  test(`whole-line excerpts: ${fixture.name}`, () => {
    const text = fixture.start + middle + fixture.end;
    const packed = placeholderFor(observationFor(text));
    const { head, tail } = excerptsFrom(packed);
    assert.equal(head, fixture.head);
    assert.equal(tail, fixture.tail);
    assert.ok(utf8Bytes(head) <= 512);
    assert.ok(utf8Bytes(tail) <= 512);
    assert.ok(text.startsWith(head));
    assert.ok(text.endsWith(tail));
    assert.ok(!packed.includes("\uFFFD"), "no UTF-8 replacement characters");
    assert.ok(!packed.includes(middle), "middle remains omitted");
  });
}

test("packing threshold still measures UTF-8 bytes, not characters", () => {
  assert.equal(THRESHOLD_BYTES, 10240);
  const atThreshold = "界".repeat(3413) + "x";
  assert.equal(utf8Bytes(atThreshold), THRESHOLD_BYTES);
  assert.equal(createObservation(messageFor(atThreshold), "/unused-context-footprint"), undefined);
  assert.equal(observationFor(atThreshold + "x").bytes, THRESHOLD_BYTES + 1);
});
