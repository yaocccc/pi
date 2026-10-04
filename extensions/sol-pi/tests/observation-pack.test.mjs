// Standalone OP checks; the SDK permission probe runs in an isolated subprocess.
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { spawnSync } from "node:child_process";
import { chmod, mkdtemp, readFile, readdir, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createObservationPackExtension } from "../extensions/observation-pack/index.ts";
import { registerOnlineTools } from "../extensions/online-context-compact/tools.ts";
import { runtimeRoot } from "../runtime-paths.ts";
import { createObservation } from "../extensions/observation-pack/observation.ts";

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

test("all background tool cards stay hidden for calls, progress, success and errors", () => {
  const tools = [setup().tool];
  registerOnlineTools({ registerTool: (tool) => tools.push(tool) }, {
    updatePlan: async () => { throw new Error("rendering must not execute tools"); },
  });
  assert.deepEqual(tools.map((tool) => tool.name), ["obs_recall", "update_plan"]);
  for (const tool of tools) {
    assert.equal(tool.renderShell, "self", "suppress the native tool shell as well");
    const components = [tool.renderCall({}, {})];
    for (const isPartial of [true, false]) {
      for (const isError of [true, false]) {
        for (const details of [undefined, { bytes: 42, lines: 3 }, { boundary: true }]) {
          components.push(tool.renderResult({ content: [{ type: "text", text: "result" }], details, isError }, { isPartial }, {}));
        }
      }
    }
    for (const component of components) {
      for (const width of [1, 80, 120]) assert.deepEqual(component.render(width), []);
      component.invalidate();
      assert.deepEqual(component.render(80), []);
    }
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
    import { SessionManager } from "@earendil-works/pi-coding-agent";
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
