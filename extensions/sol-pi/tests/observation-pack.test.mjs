// Standalone OP checks; the SDK permission probe runs in an isolated subprocess.
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmod, mkdtemp, readFile, readdir, rm, stat, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createObservationPackExtension } from "../extensions/observation-pack/index.ts";
import { registerOnlineTools } from "../extensions/online-context-compact/tools.ts";
import { runtimeRoot } from "../runtime-paths.ts";
import {
  countLines, createObservation, FULL_SENDS, hash, PLACEHOLDER_EXCERPT_BYTES, placeholderFor,
} from "../extensions/observation-pack/observation.ts";

function setup() {
  let tool;
  let contextHandler;
  createObservationPackExtension()({
    registerTool: (definition) => { tool = definition; },
    on: (event, handler) => {
      assert.equal(event, "context");
      contextHandler = handler;
    },
  });
  return { tool, contextHandler };
}

// The previous implementation is an explicit semantic oracle, not a timing
// assertion: whole-line boundaries, trailing newlines and byte budgets stay exact.
function legacyLineExcerpt(text, budgetBytes, fromEnd) {
  const lines = text.split(/(?<=\n)/);
  const selected = [];
  let selectedBytes = 0;
  for (let index = fromEnd ? lines.length - 1 : 0;
       index >= 0 && index < lines.length; index += fromEnd ? -1 : 1) {
    const line = lines[index];
    const bytes = Buffer.byteLength(line, "utf8");
    if (selectedBytes + bytes > budgetBytes) break;
    if (fromEnd) selected.unshift(line);
    else selected.push(line);
    selectedBytes += bytes;
  }
  return selected.join("");
}

function legacyCountLines(text) {
  if (text.length === 0) return 0;
  let lines = text.endsWith("\n") ? 0 : 1;
  for (const character of text) if (character === "\n") lines++;
  return lines;
}

function legacyPlaceholder(observation) {
  const headBudget = Math.floor(PLACEHOLDER_EXCERPT_BYTES / 2);
  const tailBudget = PLACEHOLDER_EXCERPT_BYTES - headBudget;
  return [
    `[full for first ${FULL_SENDS} provider requests; now packed]`,
    `id: ${observation.id}`, `tool: ${observation.toolName}`,
    `original_bytes: ${observation.bytes}`, `original_lines: ${observation.lines}`,
    `estimated_tokens: ${observation.tokens}`,
    `retrieve: obs_recall {"id":"${observation.id}","offset":0}; continue with next_offset`,
    `[first whole lines, <=${headBudget} bytes]`,
    legacyLineExcerpt(observation.text, headBudget, false),
    `[middle omitted; last whole lines, <=${tailBudget} bytes]`,
    legacyLineExcerpt(observation.text, tailBudget, true), `[end excerpts]`,
  ].join("\n");
}

function observationFixture(text) {
  return {
    id: "obs_" + "a".repeat(24), contentHash: hash(text),
    filePath: "/unused/synthetic-observation", toolName: "bash", text,
    bytes: Buffer.byteLength(text), lines: legacyCountLines(text), tokens: Math.ceil(text.length / 4),
  };
}

function resultFixture(text) {
  return { role: "toolResult", toolCallId: "synthetic-call", toolName: "bash", isError: false,
    content: [{ type: "text", text }] };
}

test("bounded excerpts preserve exact legacy UTF-8, CRLF and whole-line output", () => {
  const samples = ["", "\n", "\n\n", "a", "a\n", "a\r\n", "\r", "a\r\nb\r\nlast",
    "界🙂\n".repeat(300), "\ud800\n\udc00\n".repeat(300),
    "x".repeat(2 * 1024 * 1024), "head\n" + "x".repeat(2 * 1024 * 1024) + "\ntail",
    "\n".repeat(2000), "😀".repeat(400) + "\nsmall\n"];
  for (const bytes of [511, 512, 513]) {
    for (const line of ["x".repeat(bytes), "界".repeat(Math.floor(bytes / 3)) + "x".repeat(bytes % 3)]) {
      samples.push(line, line + "\n", "prefix\n" + line, line + "\nsuffix\n");
    }
  }
  let seed = 0x1badf00d;
  const parts = ["a", "🙂", "界", "\r", "\n", "\ud800", "\udc00", "x".repeat(509)];
  for (let i = 0; i < 150; i++) {
    let text = "";
    for (let j = 0; j < 40; j++) {
      seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
      text += parts[(seed >>> 16) % parts.length];
    }
    samples.push(text);
  }
  for (const [index, text] of samples.entries()) {
    assert.equal(countLines(text), legacyCountLines(text), `line count sample ${index}`);
    const observation = observationFixture(text);
    assert.equal(placeholderFor(observation), legacyPlaceholder(observation), `excerpt sample ${index}`);
  }
});

test("receipt detection still requires an exact complete LF-delimited line", () => {
  const marker = "sol_pi_evidence_receipt_v1";
  const padding = "payload\n".repeat(2000);
  for (const text of [marker + "\n" + padding, padding + marker, padding + marker + "\n",
    padding + marker + "\n" + padding, marker + "_suffix\n" + padding + marker]) {
    assert.equal(createObservation(resultFixture(text), "/unused/root"), undefined);
  }
  for (const text of ["prefix_" + marker + "\n" + padding, marker + "_suffix\n" + padding,
    marker + "\r\n" + padding, " " + marker + "\n" + padding, marker + marker + "\n" + padding]) {
    const observation = createObservation(resultFixture(text), "/unused/root");
    assert.ok(observation, "embedded markers must not exclude ordinary observations");
    assert.equal(observation.text, text);
    assert.equal(observation.contentHash, hash(text));
    assert.equal(observation.id, `obs_${hash(`bash\0synthetic-call\0${hash(text)}`).slice(0, 24)}`);
    assert.equal(observation.lines, legacyCountLines(text));
  }
  const message = resultFixture(padding);
  message.content.push({ type: "text", text: marker });
  assert.equal(createObservation(message, "/unused/root"), undefined, "joined content blocks preserve LF boundaries");
});

test("large observations never split the full text and excerpts only encode bounded lines", t => {
  const texts = ["0123456789abcdef0123456789abcdef0123456789abc\n".repeat(400000),
    "head\n" + "x".repeat(8 * 1024 * 1024) + "\ntail\n"];
  const expected = texts.map(text => legacyPlaceholder(observationFixture(text)));
  const split = String.prototype.split;
  const byteLength = Buffer.byteLength;
  const splitMock = t.mock.method(String.prototype, "split", function (...args) {
    assert.ok(this.length < 1024 * 1024, "metadata and excerpts must not split the full observation");
    return split.apply(this, args);
  });
  for (const [index, text] of texts.entries()) {
    const observation = createObservation(resultFixture(text), "/unused/root");
    assert.equal(observation.bytes, byteLength(text));
    assert.equal(observation.lines, legacyCountLines(text));
    let encodedUnits = 0;
    const encodingMock = t.mock.method(Buffer, "byteLength", (value, encoding) => {
      if (typeof value === "string") {
        assert.ok(value.length <= PLACEHOLDER_EXCERPT_BYTES / 2, "never encode a huge rejected line");
        encodedUnits += value.length;
      }
      return byteLength(value, encoding);
    });
    const placeholder = placeholderFor(observation);
    encodingMock.mock.restore();
    assert.ok(encodedUnits <= PLACEHOLDER_EXCERPT_BYTES);
    assert.ok(byteLength(placeholder) < 2000);
    assert.equal(placeholder, expected[index].replaceAll("obs_" + "a".repeat(24), observation.id));
  }
  splitMock.mock.restore();
});

// Opt-in, reproducible before/after benchmark; functional tests above assert
// bounded work instead of using a machine-dependent elapsed-time threshold.
// SOL_PI_OBSERVATION_BENCHMARK=1 node --expose-gc --experimental-transform-types \
//   --test --test-name-pattern='synthetic OP benchmark' <this file>
if (process.env.SOL_PI_OBSERVATION_BENCHMARK === "1") {
  test("synthetic OP benchmark compares legacy scans with bounded metadata/excerpts", t => {
    const text = "0123456789abcdef0123456789abcdef0123456789abc\n".repeat(400000);
    const message = resultFixture(text);
    const root = "/unused/no-io-benchmark";
    const legacyCreate = () => {
      const body = message.content.map(block => block.text).join("\n");
      if (body.split("\n").some(line => line === "sol_pi_evidence_receipt_v1")) return undefined;
      const bytes = Buffer.byteLength(body);
      const contentHash = hash(body);
      const id = `obs_${hash(`${message.toolName}\0${message.toolCallId}\0${contentHash}`).slice(0, 24)}`;
      return { id, contentHash, filePath: `${root}/observation-pack/objects/${id}.txt`,
        toolName: message.toolName, text: body, bytes, lines: legacyCountLines(body), tokens: Math.ceil(body.length / 4) };
    };
    const results = { legacy: [], bounded: [] };
    for (let round = 0; round < 6; round++) {
      for (const phase of round % 2 ? ["bounded", "legacy"] : ["legacy", "bounded"]) {
        globalThis.gc?.();
        const heap = process.memoryUsage().heapUsed;
        const started = performance.now();
        const observation = phase === "legacy" ? legacyCreate() : createObservation(message, root);
        const prepared = performance.now();
        const placeholder = phase === "legacy" ? legacyPlaceholder(observation) : placeholderFor(observation);
        const ended = performance.now();
        results[phase].push({ metadata_ms: prepared - started, placeholder_ms: ended - prepared,
          total_ms: ended - started, observed_heap_delta_bytes: process.memoryUsage().heapUsed - heap });
        assert.equal(observation.bytes, 18400000);
        assert.equal(observation.lines, 400000);
        assert.equal(placeholder, legacyPlaceholder(observation));
      }
    }
    const median = values => {
      values.sort((a, b) => a - b);
      return +((values[2] + values[3]) / 2).toFixed(3);
    };
    const summary = {};
    for (const [phase, samples] of Object.entries(results)) {
      summary[phase] = Object.fromEntries(Object.keys(samples[0]).map(key => [key, median(samples.map(sample => sample[key]))]));
    }
    t.diagnostic(JSON.stringify({ bytes: 18400000, lines: 400000, rounds: 6,
      exposed_gc: typeof globalThis.gc === "function", median: summary }));
  });
}

test("background capabilities do not register terminal renderers", () => {
  const tools = [setup().tool];
  registerOnlineTools({ registerTool: (tool) => tools.push(tool) }, {
    updatePlan: async () => { throw new Error("registration must not execute tools"); },
  });
  assert.deepEqual(tools.map((tool) => tool.name), ["obs_recall", "update_plan"]);
  for (const tool of tools) {
    assert.equal(tool.renderShell, undefined);
    assert.equal(tool.renderCall, undefined);
    assert.equal(tool.renderResult, undefined);
  }
});

test("standalone pack and paged recall preserve UTF-8 data and stored message history", async (t) => {
  const directory = await mkdtemp(join(tmpdir(), "op-refactor-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const { tool, contextHandler } = setup();
  const ctx = { sessionManager: { getSessionDir: () => directory, getSessionId: () => "op-refactor" } };
  const original = `first\n${"large observation 编译🙂\n".repeat(1100)}last\n`;
  const message = { role: "toolResult", toolCallId: "op-test", toolName: "bash", isError: false, content: [{ type: "text", text: original }] };
  const project = async () => (await contextHandler({ messages: [message] }, ctx)).messages[0].content[0].text;
  assert.equal(await project(), original);
  assert.equal(await project(), original);
  const packed = await project();
  assert.notEqual(packed, original);
  assert.equal(await project(), packed);
  assert.equal(message.content[0].text, original);
  const id = packed.match(/^id: (obs_[a-f0-9]{24})$/m)?.[1];
  assert.ok(id);
  let offset = 0;
  let recalled = "";
  for (;;) {
    const result = await tool.execute("recall-test", { id, offset }, undefined, undefined, ctx);
    const text = result.content[0].text;
    assert.ok(Buffer.byteLength(text) <= 16 * 1024);
    recalled += text.slice(text.indexOf("\n", text.indexOf("\n") + 1) + 1);
    if (result.details.eof) break;
    assert.ok(result.details.nextOffset > offset);
    offset = result.details.nextOffset;
  }
  assert.equal(recalled, original);
});

test("packed observations still validate archive bytes and recreate missing objects", async t => {
  const directory = await mkdtemp(join(tmpdir(), "op-archive-integrity-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const errors = [];
  t.mock.method(console, "error", value => errors.push(value));
  const ctx = { sessionManager: { getSessionDir: () => directory, getSessionId: () => "archive-integrity" } };
  const { contextHandler } = setup();
  const text = "原始🙂 observation\r\n".repeat(3000) + "no final newline";
  const message = resultFixture(text);
  const project = async () => (await contextHandler({ messages: [message] }, ctx)).messages[0].content[0].text;
  assert.equal(await project(), text);
  assert.equal(await project(), text);
  const packed = await project();
  const observation = createObservation(message, runtimeRoot(ctx));
  assert.equal(packed, placeholderFor(observation));
  const bytes = await readFile(observation.filePath);
  const corrupted = Buffer.from(bytes);
  corrupted[0] ^= 1;
  await writeFile(observation.filePath, corrupted);
  assert.equal(await project(), text, "a same-size corrupted archive must fail open to the original result");
  assert.match(errors.pop(), /hash mismatch/);
  await rm(observation.filePath);
  assert.equal(await project(), packed, "a missing object is recreated before reusing its stable placeholder");
  assert.deepEqual(await readFile(observation.filePath), bytes);
  assert.equal(message.content[0].text, text, "native history is never rewritten");
});

test("large hidden toolResult details are never serialized or counted as visible observations", async t => {
  const directory = await mkdtemp(join(tmpdir(), "op-hidden-details-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const ctx = { sessionManager: { getSessionDir: () => directory, getSessionId: () => "hidden-details" } };
  const { contextHandler } = setup();
  let serialized = 0;
  const details = { payload: "x".repeat(2 * 1024 * 1024), toJSON() {
    serialized++;
    throw new Error("hidden details must not be serialized by OP");
  } };
  const visible = "visible observation\n".repeat(2000);
  const messages = Array.from({ length: 24 }, (_, i) => ({
    role: "toolResult", toolCallId: `details-${i}`, toolName: "bash", isError: false,
    content: [{ type: "text", text: i < 12 ? "small visible result" : visible }], details,
  }));
  for (let send = 0; send < 4; send++) {
    const projected = (await contextHandler({ messages }, ctx)).messages;
    for (let i = 0; i < messages.length; i++) {
      assert.equal(projected[i].details, details, "native details stay untouched");
      if (i < 12 || send < 2) assert.equal(projected[i].content[0].text, messages[i].content[0].text);
      else assert.match(projected[i].content[0].text, /now packed/);
    }
  }
  assert.equal(serialized, 0, "24 × 2 MiB hidden payloads must never enter JSON.stringify");
  assert.equal(createObservation(messages[0], runtimeRoot(ctx)), undefined, "details alone cannot trigger packing");
  const observation = createObservation(messages[12], runtimeRoot(ctx));
  assert.equal(observation.bytes, Buffer.byteLength(visible));
  assert.equal(await readFile(observation.filePath, "utf8"), visible);
});

test("one OP instance isolates temporary sessions and send counts, and refuses unsafe recall IDs", async t => {
  const roots = new Set();
  t.after(async () => { for (const root of roots) await rm(root, { recursive: true, force: true }); });
  const { tool, contextHandler } = setup();
  const contexts = [0, 1].map(() => {
    const id = randomUUID();
    return { sessionManager: { getSessionDir: () => "", getSessionId: () => id } };
  });
  for (const ctx of contexts) roots.add(runtimeRoot(ctx));
  const original = "isolated observation\n".repeat(2000);
  const message = { role: "toolResult", toolCallId: "same-call", toolName: "bash", isError: false, content: [{ type: "text", text: original }] };
  const project = async ctx => (await contextHandler({ messages: [message] }, ctx)).messages[0].content[0].text;
  assert.equal(await project(contexts[0]), original);
  assert.equal(await project(contexts[0]), original);
  const packed = await project(contexts[0]);
  assert.notEqual(packed, original);
  const id = createObservation(message, runtimeRoot(contexts[0])).id;
  await assert.rejects(tool.execute("foreign", { id }, undefined, undefined, contexts[1]), /Unknown observation id/);
  assert.equal(await project(contexts[1]), original, "no inherited sends from the first session");
  assert.equal(await project(contexts[0]), packed, "switching contexts does not reset the first session");
  assert.equal(await project(contexts[1]), original);
  assert.equal(await project(contexts[1]), packed);
  for (const ctx of contexts) {
    const recall = await tool.execute("same-session", { id }, undefined, undefined, ctx);
    assert.match(recall.content[0].text, /isolated observation/);
    for (const badId of ["../escape", "obs_" + "f".repeat(24) + "/../escape", "obs_" + "f".repeat(23), "obs_" + "F".repeat(24)]) {
      await assert.rejects(tool.execute("unsafe", { id: badId }, undefined, undefined, ctx), /Unknown observation id/);
    }
  }
});

test("OP never archives obs_recall itself or reducer receipts", async t => {
  const directory = await mkdtemp(join(tmpdir(), "op-excluded-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const ctx = { sessionManager: { getSessionDir: () => directory, getSessionId: () => "excluded" } };
  const { contextHandler } = setup();
  const messages = [
    { role: "toolResult", toolCallId: "recall", toolName: "obs_recall", isError: false, content: [{ type: "text", text: "x".repeat(16000) }] },
    { role: "toolResult", toolCallId: "receipt", toolName: "bash", isError: false, content: [{ type: "text", text: "sol_pi_evidence_receipt_v1\n" + "x".repeat(16000) }] },
  ];
  for (let i = 0; i < 5; i++) assert.deepEqual((await contextHandler({ messages }, ctx)).messages, messages);
  assert.deepEqual(await readdir(directory), []);
});

test("runtime-root failures fail open without throwing away any messages", async t => {
  const { contextHandler } = setup();
  const messages = [{ role: "toolResult", toolCallId: "failure", toolName: "bash", isError: false, content: [{ type: "text", text: "x".repeat(20000) }] }];
  const errors = [];
  t.mock.method(console, "error", message => errors.push(message));
  for (const ctx of [
    { sessionManager: { getSessionDir: () => "", getSessionId: () => "../unsafe" } },
    { sessionManager: { getSessionDir: () => { throw new Error("unavailable directory"); } } },
  ]) {
    for (let i = 0; i < 3; i++) assert.deepEqual((await contextHandler({ messages }, ctx)).messages, messages);
  }
  assert.equal(errors.length, 6);
  assert.ok(errors.every(message => message.includes("fail-open for runtime root")));
});

test("unwritable archive directory fails open and does not consume successful full sends", async t => {
  if (process.platform === "win32" || process.getuid?.() === 0) return t.skip("requires POSIX permission enforcement for an unprivileged user");
  const directory = await mkdtemp(join(tmpdir(), "op-unwritable-"));
  const ctx = { sessionManager: { getSessionDir: () => directory, getSessionId: () => "readonly" } };
  const { contextHandler } = setup();
  const original = "still visible\n".repeat(2000);
  const message = { role: "toolResult", toolCallId: "readonly", toolName: "bash", isError: false, content: [{ type: "text", text: original }] };
  t.after(async () => { await chmod(directory, 0o700); await rm(directory, { recursive: true, force: true }); });
  await chmod(directory, 0o500);
  const errors = [];
  t.mock.method(console, "error", message => errors.push(message));
  for (let i = 0; i < 4; i++) assert.deepEqual((await contextHandler({ messages: [message] }, ctx)).messages, [message]);
  assert.equal(errors.length, 4);
  assert.ok(errors.every(message => /fail-open.*EACCES/.test(message)));
  assert.deepEqual(await readdir(directory), []);
  await chmod(directory, 0o700);
  for (let i = 0; i < 2; i++) assert.deepEqual((await contextHandler({ messages: [message] }, ctx)).messages, [message]);
  const packed = (await contextHandler({ messages: [message] }, ctx)).messages[0].content[0].text;
  assert.match(packed, /now packed/);
  const observation = createObservation(message, runtimeRoot(ctx));
  assert.equal(await readFile(observation.filePath, "utf8"), original);
  assert.equal((await stat(observation.filePath)).mode & 0o777, 0o600);
});

test("real SDK in-memory root creation in an unwritable TMPDIR fails open and retries safely", async t => {
  if (process.platform === "win32" || process.getuid?.() === 0) return t.skip("requires POSIX permission enforcement for an unprivileged user");
  const directory = await mkdtemp(join(tmpdir(), "op-denied-tmp-"));
  t.after(async () => { await chmod(directory, 0o700); await rm(directory, { recursive: true, force: true }); });
  await chmod(directory, 0o500);
  const script = `
    import assert from "node:assert/strict";
    import { chmod, readFile } from "node:fs/promises";
    import { SessionManager } from ${JSON.stringify(import.meta.resolve("@earendil-works/pi-coding-agent"))};
    import { createObservationPackExtension } from ${JSON.stringify(new URL("../extensions/observation-pack/index.ts", import.meta.url).href)};
    import { runtimeRoot } from ${JSON.stringify(new URL("../runtime-paths.ts", import.meta.url).href)};
    import { createObservation } from ${JSON.stringify(new URL("../extensions/observation-pack/observation.ts", import.meta.url).href)};
    let handler;
    createObservationPackExtension()({ registerTool() {}, on: (_event, fn) => { handler = fn; } });
    const ctx = { sessionManager: SessionManager.inMemory() };
    assert.equal(ctx.sessionManager.getSessionDir(), "");
    const message = { role: "toolResult", toolName: "bash", toolCallId: "denied-temp", isError: false, content: [{ type: "text", text: "exact observation\\n".repeat(2000) }] };
    for (let i = 0; i < 4; i++) assert.deepEqual((await handler({ messages: [message] }, ctx)).messages, [message]);
    await chmod(process.env.TMPDIR, 0o700);
    for (let i = 0; i < 2; i++) assert.deepEqual((await handler({ messages: [message] }, ctx)).messages, [message]);
    assert.match((await handler({ messages: [message] }, ctx)).messages[0].content[0].text, /now packed/);
    const observation = createObservation(message, runtimeRoot(ctx));
    assert.equal(await readFile(observation.filePath, "utf8"), message.content[0].text);
    console.log("temporary-root fail-open and recovery passed");
  `;
  const result = spawnSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", script], {
    encoding: "utf8", env: { ...process.env, TMPDIR: directory, TMP: directory, TEMP: directory }, timeout: 20_000,
  });
  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /temporary-root fail-open and recovery passed/);
  assert.equal(result.stderr.match(/fail-open for runtime root:.*EACCES/g)?.length, 4);
});
