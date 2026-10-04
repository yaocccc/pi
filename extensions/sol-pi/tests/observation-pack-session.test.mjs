import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createAgentSession, DefaultResourceLoader, SessionManager, SettingsManager, VERSION } from "@earendil-works/pi-coding-agent";
import { createProvider, getCurrentTools } from "@earendil-works/pi-ai";
import { createFauxCore, fauxAssistantMessage, fauxToolCall } from "@earendil-works/pi-ai/providers/faux";
import { createObservationPackExtension } from "../extensions/observation-pack/index.ts";
import { createOnlineContextCompactExtension } from "../extensions/online-context-compact/extension.ts";
import { createObservation, countLines, THRESHOLD_BYTES } from "../extensions/observation-pack/observation.ts";
import { runtimeRoot } from "../runtime-paths.ts";

async function setup(t, { history, persistent = false } = {}) {
  assert.equal(VERSION, "1.0.2");
  const cwd = await mkdtemp(join(tmpdir(), "op-sdk-test-"));
  const agentDir = join(cwd, "agent");
  await mkdir(agentDir);
  const manager = persistent ? SessionManager.create(cwd, join(cwd, "sessions")) : SessionManager.inMemory(cwd);
  const settings = SettingsManager.inMemory({ compaction: { enabled: true }, retry: { enabled: false }, cacheWarming: "off" }, { projectTrusted: false });
  const core = createFauxCore({ provider: `op-${manager.getSessionId()}`, api: `op-api-${manager.getSessionId()}`,
    models: [{ id: "offline", contextWindow: 1_000_000, maxTokens: 4096 }] });
  const provider = createProvider({ id: core.provider, models: core.models,
    auth: { apiKey: { name: "Offline test", resolve: async () => ({ auth: { apiKey: "offline-test" } }) } },
    api: { stream: core.stream, streamSimple: core.streamSimple },
  });
  if (history) {
    manager.appendMessage({ role: "user", content: "read data", timestamp: 1 });
    manager.appendMessage(fauxAssistantMessage(fauxToolCall("read", {}, { id: history.toolCallId }), { stopReason: "toolUse" }));
    manager.appendMessage(history);
  }
  let ctx;
  const errors = [];
  const loader = new DefaultResourceLoader({ cwd, agentDir, settingsManager: settings,
    noExtensions: true, noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true,
    systemPrompt: "Offline OP session regression.",
    extensionFactories: [{ name: "op-sdk-test", factory: pi => {
      pi.registerProvider(provider);
      createObservationPackExtension()(pi);
      createOnlineContextCompactExtension()(pi);
      pi.on("session_start", (_event, context) => { ctx = context; });
    } }],
  });
  let session, root;
  t.after(async () => {
    session?.dispose();
    if (root && !persistent) await rm(root, { recursive: true, force: true });
    await rm(cwd, { recursive: true, force: true });
  });
  await loader.reload();
  assert.deepEqual(loader.getExtensions().errors, []);
  ({ session } = await createAgentSession({ cwd, agentDir, sessionManager: manager, settingsManager: settings,
    resourceLoader: loader, model: core.getModel(), thinkingLevel: "off", tools: ["obs_recall", "update_plan"] }));
  await session.bindExtensions({ onError: error => errors.push(error) });
  assert.ok(ctx, "real extension context supplied by SDK");
  root = runtimeRoot(ctx);
  if (!persistent) {
    assert.equal(ctx.sessionManager.getSessionDir(), "");
    assert.equal(ctx.sessionManager.getSessionFile(), undefined);
    assert.equal(manager.isPersisted(), false);
  }
  return { session, manager, ctx, root, core, errors, cwd,
    run: () => session.prompt("continue", { expandPromptTemplates: false }) };
}

const original = `head\n${"编译🙂 exact bytes ".repeat(2200)}\ntail\n`;
const message = { role: "toolResult", toolCallId: "large-read", toolName: "read", isError: false,
  content: [{ type: "text", text: original }], timestamp: 2 };

for (const persistent of [false, true]) test(`real SDK ${persistent ? "persistent" : "in-memory"} OP packs and recalls every UTF-8 byte without repacking recall`, async t => {
  const h = await setup(t, { history: message, persistent });
  const id = createObservation(message, h.root).id;
  const visible = [];
  h.core.setResponses([0, 1, 2].map(() => async context => {
    const result = context.messages.find(m => m.role === "toolResult" && m.toolCallId === message.toolCallId);
    visible.push(result.content[0].text);
    if (!persistent) assert.ok(!getCurrentTools(context.messages).some(tool => tool.name === "update_plan"));
    return fauxAssistantMessage("ready");
  }));
  await h.run(); await h.run(); await h.run();
  assert.deepEqual(visible.slice(0, 2), [original, original]);
  assert.match(visible[2], new RegExp(`id: ${id}`));
  assert.equal(h.manager.getBranch().find(e => e.type === "message" && e.message.toolCallId === message.toolCallId).message.content[0].text, original);
  const objectPath = join(h.root, "observation-pack", "objects", `${id}.txt`);
  assert.equal(await readFile(objectPath, "utf8"), original);
  if (!persistent) {
    assert.ok(!h.session.getActiveToolNames().includes("update_plan"));
    await assert.rejects(stat(join(h.cwd, "sol-pi")), { code: "ENOENT" });
    await assert.rejects(stat(join(h.cwd, "sessions")), { code: "ENOENT" });
  }

  let offset = 0, recalled = "", pages = 0;
  const page = async context => {
    const results = context.messages.filter(m => m.role === "toolResult" && m.toolName === "obs_recall");
    const text = results.at(-1).content[0].text;
    assert.ok(Buffer.byteLength(text) <= 16 * 1024);
    assert.ok(countLines(text) <= 400);
    assert.ok(!text.includes("now packed"));
    assert.match(text, new RegExp(`offset=${offset} `));
    const nextOffset = Number(text.match(/next_offset=(\d+)/)[1]);
    const eof = /eof=true/.test(text);
    recalled += text.slice(text.indexOf("\n", text.indexOf("\n") + 1) + 1);
    pages++;
    assert.ok(nextOffset > offset);
    offset = nextOffset;
    return eof ? fauxAssistantMessage("recall complete") :
      fauxAssistantMessage(fauxToolCall("obs_recall", { id, offset }, { id: `recall-${pages}` }), { stopReason: "toolUse" });
  };
  h.core.setResponses([
    fauxAssistantMessage(fauxToolCall("obs_recall", { id, offset: 0 }, { id: "recall-0" }), { stopReason: "toolUse" }),
    ...Array.from({ length: 20 }, () => page),
  ]);
  await h.run();
  assert.ok(pages > 1);
  assert.equal(recalled, original);
  assert.equal(offset, Buffer.byteLength(original));
  const recalls = h.manager.getBranch().filter(e => e.type === "message" && e.message.toolName === "obs_recall").map(e => e.message);
  assert.ok(recalls.some(m => Buffer.byteLength(m.content[0].text) > THRESHOLD_BYTES));
  h.core.setResponses([0, 1, 2].map(() => async context => {
    const current = context.messages.filter(m => m.role === "toolResult" && m.toolName === "obs_recall");
    assert.deepEqual(current.map(m => m.content), recalls.map(m => m.content), "aged recall pages remain exact, never archived recursively");
    return fauxAssistantMessage("done");
  }));
  await h.run(); await h.run(); await h.run();
  assert.deepEqual(h.errors, []);
  const ledger = (await readFile(join(h.root, "observation-pack", "ledger.jsonl"), "utf8")).trim().split("\n").map(JSON.parse);
  assert.equal(ledger.filter(e => e.event === "full").length, 2);
  assert.equal(ledger.filter(e => e.event === "recall").length, pages);
  assert.ok(ledger.every(e => e.id === id), "only the original observation was archived");
  h.session.dispose();
  assert.equal(await readFile(objectPath, "utf8"), original, "archives survive disposal for parent-process recall");
});

test("real SDK in-memory sessions isolate archives; Worker flag still disables OCC, not OP", async t => {
  const a = await setup(t, { history: message });
  a.core.setResponses([fauxAssistantMessage("stored")]);
  await a.run();
  const id = createObservation(message, a.root).id;
  const old = process.env.PI_WORKER_DEPTH;
  try {
    process.env.PI_WORKER_DEPTH = "1"; // Exercise eligibility only; no Worker is started.
    const b = await setup(t);
    assert.notEqual(b.root, a.root);
    assert.ok(!b.session.getActiveToolNames().includes("update_plan"));
    b.core.setResponses([
      fauxAssistantMessage(fauxToolCall("obs_recall", { id }, { id: "foreign-recall" }), { stopReason: "toolUse" }),
      async context => {
        const result = context.messages.find(m => m.role === "toolResult" && m.toolCallId === "foreign-recall");
        assert.equal(result.isError, true);
        assert.match(result.content[0].text, /Unknown observation id/);
        assert.ok(!getCurrentTools(context.messages).some(tool => tool.name === "update_plan"));
        return fauxAssistantMessage("isolated");
      },
    ]);
    await b.run();
    assert.deepEqual(b.errors, []);
    assert.equal(b.manager.getBranch().filter(e => e.type === "compaction").length, 0);
    assert.equal(a.manager.getBranch().filter(e => e.type === "compaction").length, 0);
  } finally {
    if (old === undefined) delete process.env.PI_WORKER_DEPTH;
    else process.env.PI_WORKER_DEPTH = old;
  }
});
