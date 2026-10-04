// Standalone OP checks: no OCC/native preparation or pinned SDK runtime imports.
import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createObservationPackExtension } from "../extensions/observation-pack/index.ts";
import { registerOnlineTools } from "../extensions/online-context-compact/tools.ts";

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
