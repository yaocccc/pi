/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */
import assert from "node:assert/strict";
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { createEditToolDefinition, DefaultResourceLoader, SettingsManager } from "@earendil-works/pi-coding-agent";
import entry from "../index.ts";
import { createActionFusionExtension } from "../extensions/action-fusion/index.ts";
import { THRESHOLD_BYTES } from "../extensions/observation-pack/index.ts";

const extensionDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");

async function sandbox(t) {
  const dir = await mkdtemp(join(tmpdir(), "sol-pi-local-test-"));
  t.after(() => rm(dir, { recursive: true, force: true }));
  return dir;
}

function fakePi() {
  const tools = new Map();
  const handlers = new Map();
  return {
    tools,
    handlers,
    registerTool(tool) {
      assert.ok(!tools.has(tool.name), `duplicate tool: ${tool.name}`);
      tools.set(tool.name, tool);
    },
    on(name, handler) {
      const list = handlers.get(name) ?? [];
      list.push(handler);
      handlers.set(name, list);
    },
  };
}

function context(cwd, sessionDir = cwd) {
  return {
    cwd,
    mode: "json",
    hasUI: false,
    ui: {},
    sessionManager: { getSessionDir: () => sessionDir, getSessionId: () => "isolated-session", getSessionFile: () => undefined },
  };
}

async function project(pi, messages, ctx) {
  let result = structuredClone(messages);
  for (const handler of pi.handlers.get("context") ?? []) {
    result = (await handler({ type: "context", messages: result }, ctx)).messages;
  }
  return result;
}

function resultText(result) {
  return result.content.filter(block => block.type === "text").map(block => block.text).join("\n");
}

test("entry retains AF/OP tools and one context handler, adds OCC without Reducer", () => {
  const pi = fakePi();
  entry(pi);
  assert.deepEqual([...pi.tools.keys()], ["edit", "write", "obs_recall", "update_plan"]);
  assert.equal(pi.handlers.get("turn_end").length, 1);
  assert.equal(pi.handlers.get("context").length, 1);
});

test("native edit edits[] schema remains compatible; write then_run runs after mutation", async t => {
  const cwd = await sandbox(t);
  const calls = [];
  const file = join(cwd, "file.txt");
  const pi = fakePi();
  createActionFusionExtension({
    bashOptions: { operations: { exec: async (command, bashCwd, { onData }) => {
      assert.equal(bashCwd, cwd);
      assert.equal(await readFile(file, "utf8"), command === "check edit" ? "edited\n" : "written\n");
      calls.push(command);
      onData(Buffer.from("check passed\n"));
      return { exitCode: 0 };
    } } },
  })(pi);
  const edit = pi.tools.get("edit");
  const write = pi.tools.get("write");
  assert.deepEqual(Object.keys(edit.parameters.properties), Object.keys(createEditToolDefinition(cwd).parameters.properties).concat("then_run"));
  assert.deepEqual(Object.keys(write.parameters.properties), ["path", "content", "then_run"]);
  assert.ok(!edit.parameters.required.includes("then_run"));
  assert.ok(!write.parameters.required.includes("then_run"));

  await writeFile(file, "before\n");
  await edit.execute("edit-1", { path: file, edits: [{ oldText: "before", newText: "after" }] }, undefined, undefined, context(cwd));
  assert.equal(await readFile(file, "utf8"), "after\n");
  const result = await write.execute("write-1", { path: file, content: "written\n", then_run: { command: "check file" } }, undefined, undefined, context(cwd));
  assert.deepEqual(calls, ["check file"]);
  assert.match(resultText(result), /\[then_run:succeeded\][\s\S]*check passed/);
  const edited = await edit.execute("edit-2", { path: file, edits: [{ oldText: "written", newText: "edited" }], then_run: { command: "check edit" } }, undefined, undefined, context(cwd));
  assert.deepEqual(calls, ["check file", "check edit"]);
  assert.match(resultText(edited), /\[then_run:succeeded\]/);
});

test("failed mutation skips command; failed command retains file mutation", async t => {
  const cwd = await sandbox(t);
  let calls = 0;
  const pi = fakePi();
  createActionFusionExtension({ bashOptions: { operations: { exec: async () => {
    calls++;
    return { exitCode: 7 };
  } } } })(pi);
  await assert.rejects(
    pi.tools.get("edit").execute("edit-fail", { path: "missing.txt", edits: [{ oldText: "a", newText: "b" }], then_run: { command: "never" } }, undefined, undefined, context(cwd)),
    /\[then_run:skipped\]/,
  );
  assert.equal(calls, 0);
  await assert.rejects(
    pi.tools.get("write").execute("write-fail", { path: "kept.txt", content: "kept", then_run: { command: "fails" } }, undefined, undefined, context(cwd)),
    /\[then_run:failed\]/,
  );
  assert.equal(calls, 1);
  assert.equal(await readFile(join(cwd, "kept.txt"), "utf8"), "kept");
});

test("large text is sent twice, then referenced; recall reconstructs the original bytes", async t => {
  const cwd = await sandbox(t);
  const pi = fakePi();
  entry(pi);
  const original = `head ☾\n${"middle observation\n".repeat(1100)}tail\n`;
  const message = { role: "toolResult", toolCallId: "call-1", toolName: "bash", content: [{ type: "text", text: original }], isError: false, timestamp: 1 };
  const ctx = context(cwd);
  const first = await project(pi, [message], ctx);
  const second = await project(pi, [message], ctx);
  const third = await project(pi, [message], ctx);
  assert.equal(resultText(first[0]), original);
  assert.equal(resultText(second[0]), original);
  assert.notEqual(resultText(third[0]), original);
  assert.equal(resultText((await project(pi, [message], ctx))[0]), resultText(third[0]));
  assert.equal(resultText(message), original, "stored history stays unmodified");
  const id = resultText(third[0]).match(/id: (obs_[a-f0-9]{24})/)?.[1];
  assert.ok(id);
  assert.equal(await readFile(join(cwd, "sol-pi", "isolated-session", "observation-pack", "objects", `${id}.txt`), "utf8"), original);
  assert.match(await readFile(join(cwd, "sol-pi", "isolated-session", "observation-pack", "ledger.jsonl"), "utf8"), /"event":"placeholder"/);
  let offset = 0;
  let recalled = "";
  for (;;) {
    const response = await pi.tools.get("obs_recall").execute("recall", { id, offset }, undefined, undefined, ctx);
    const text = resultText(response);
    recalled += text.slice(text.indexOf("\n", text.indexOf("\n") + 1) + 1);
    offset = response.details.nextOffset;
    if (response.details.eof) break;
  }
  assert.equal(recalled, original);
});

test("error, short, mixed results stay unmodified even after three projections", async t => {
  const cwd = await sandbox(t);
  const pi = fakePi();
  entry(pi);
  const large = "x".repeat(THRESHOLD_BYTES + 1);
  const base = { role: "toolResult", toolCallId: "call-1", toolName: "bash", isError: false, timestamp: 1 };
  for (const message of [
    { ...base, toolCallId: "err", isError: true, content: [{ type: "text", text: large }] },
    { ...base, toolCallId: "short", content: [{ type: "text", text: "short" }] },
    { ...base, toolCallId: "mixed", content: [{ type: "text", text: large }, { type: "image", mimeType: "image/png", data: "AA==" }] },
  ]) {
    for (let i = 0; i < 3; i++) assert.deepEqual((await project(pi, [message], context(cwd)))[0], message);
  }
});

test("real Pi resource loader discovers the single directory entry without a provider", async t => {
  const cwd = await sandbox(t);
  const agentDir = join(cwd, "agent");
  await mkdir(join(agentDir, "extensions"), { recursive: true });
  await symlink(extensionDir, join(agentDir, "extensions", "sol-pi"), "dir");
  const loader = new DefaultResourceLoader({
    cwd,
    agentDir,
    settingsManager: SettingsManager.inMemory({}, { projectTrusted: true }),
    noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true,
  });
  await loader.reload();
  const { extensions, errors } = loader.getExtensions();
  assert.deepEqual(errors, []);
  assert.equal(extensions.length, 1);
  assert.deepEqual([...extensions[0].tools.keys()], ["edit", "write", "obs_recall", "update_plan"]);
  assert.equal(extensions[0].handlers.get("context").length, 1);
  assert.equal(extensions[0].handlers.get("turn_end").length, 1);
});
