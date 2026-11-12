import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { WorkerTranscriptWriter } from "./transcript.ts";
import { createWorkerOwner, runPiWorker } from "./process.ts";
import { batchUiSnapshot } from "./index.ts";
import { workerBranchSnapshots } from "./history.ts";
import type { WorkerTask } from "./types.ts";

function temporary(t: any): string {
 const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-transcript-test-"));
 t.after(() => fs.rmSync(root, { recursive: true, force: true }));
 return root;
}
function records(root: string, id: string): any[] {
 return fs.readFileSync(path.join(root, "worker-transcripts", `${id}.jsonl`), "utf8").trimEnd().split("\n").map((line) => JSON.parse(line));
}

test("private bounded log preserves real messages/tools and strips only system declarations", (t) => {
 const root = temporary(t);
 const inherited = [
  { type: "message", id: "s", parentId: null, message: { role: "system", content: "PRIVATE_SYSTEM" } },
  { type: "message", id: "u", parentId: "s", message: { role: "user", content: "parent context" } },
  { type: "compaction", id: "c", parentId: "u", firstKeptEntryId: "s", summary: "summary", systemMessage: { content: "PRIVATE_SYSTEM" } },
 ];
 const snapshot = { branchJsonl: inherited.map((entry) => JSON.stringify(entry)).join("\n") };
 const writer = new WorkerTranscriptWriter({ root, cwd: root, prompt: "actual task", snapshot });
 assert.match(writer.descriptor.id, /^[a-f0-9-]{36}$/);
 assert.deepEqual(Object.keys(writer.descriptor).sort(), ["id", "version"]);
 const user = { role: "user", content: "actual task", timestamp: 1 };
 const events = [
  { type: "agent_start" },
  { type: "message_start", message: user },
  { type: "message_end", message: user },
  { type: "message_start", message: { role: "assistant", content: [], timestamp: 2 } },
  { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "完整\n🧪\u2028text" } },
  { type: "tool_execution_start", toolCallId: "call", toolName: "read", args: { path: "a" } },
  { type: "tool_execution_end", toolCallId: "call", result: { content: [{ type: "text", text: "FULL_RESULT" }], details: { unknown: "preserved" } } },
  { type: "agent_end", messages: [] },
 ];
 for (const event of events) writer.record(event);
 writer.record({ type: "message_end", message: { role: "system", content: "PRIVATE_SYSTEM" } });
 writer.record({ type: "compaction_end", result: { summary: "kept", systemMessage: { content: "PRIVATE_SYSTEM" } } });
 writer.finish("completed");
 writer.finish("failed");
 const all = records(root, writer.descriptor.id);
 assert.equal(all[0].type, "worker_transcript_header");
 assert.equal(all[0].entries[0].parentId, null);
 assert.equal(all[0].entries[1].firstKeptEntryId, "u");
 assert.deepEqual(all.slice(1, events.length + 1), events);
 assert.equal(all.at(-1).status, "completed");
 assert.equal(all.at(-1).complete, true);
 assert.equal(all.filter((event) => event.type === "worker_transcript_end").length, 1);
 assert.ok(!JSON.stringify(all).includes("PRIVATE_SYSTEM"));
 assert.ok(snapshot.branchJsonl.includes("PRIVATE_SYSTEM"), "does not mutate source snapshot");
 assert.equal(fs.statSync(path.join(root, "worker-transcripts")).mode & 0o777, 0o700);
 assert.equal(fs.statSync(path.join(root, "worker-transcripts", `${writer.descriptor.id}.jsonl`)).mode & 0o777, 0o600);
});

test("fallback prompt precedes assistant output, and no stdout still records the actual prompt", (t) => {
 const root = temporary(t);
 for (const status of ["completed", "failed", "cancelled", "timed_out"] as const) {
  const writer = new WorkerTranscriptWriter({ root, cwd: root, prompt: "the actual prompt" });
  if (status === "completed") writer.record({ type: "message_end", message: { role: "assistant", content: [] } });
  writer.finish(status);
  const all = records(root, writer.descriptor.id);
  assert.equal(all[1].message.role, "user");
  assert.equal(all[2].message.content, "the actual prompt");
  assert.equal(all.at(-1).status, status);
 }
});

test("size cap and unsafe/unwritable storage explicitly mark incomplete without throwing", (t) => {
 const root = temporary(t);
 const writer = new WorkerTranscriptWriter({ root, cwd: root, prompt: "task", maxBytes: 4096 });
 writer.record({ type: "message_end", message: { role: "assistant", content: "x".repeat(5000) } });
 writer.finish("completed");
 assert.equal(writer.descriptor.error, "size_limit");
 assert.equal(writer.descriptor.incomplete, true);
 assert.equal(records(root, writer.descriptor.id).at(-1).complete, false);
 assert.ok(fs.statSync(path.join(root, "worker-transcripts", `${writer.descriptor.id}.jsonl`)).size <= 4096);
 const fork = new WorkerTranscriptWriter({ root, cwd: root, prompt: "task", maxBytes: 4096, snapshot: { branchJsonl: JSON.stringify({ type: "message", id: "large", message: { role: "user", content: "x".repeat(6000) } }) } });
 assert.equal(fork.descriptor.error, "size_limit");
 assert.equal(records(root, fork.descriptor.id)[0].type, "worker_transcript_header");
 const outside = temporary(t);
 const alias = temporary(t);
 fs.symlinkSync(outside, path.join(alias, "worker-transcripts"));
 const refused = new WorkerTranscriptWriter({ root: alias, cwd: root, prompt: "task" });
 assert.equal(refused.descriptor.error, "io_error");
 assert.deepEqual(fs.readdirSync(outside), []);
 const invalid = path.join(root, "not-a-directory");
 fs.writeFileSync(invalid, "occupied");
 const unavailable = new WorkerTranscriptWriter({ root: invalid, cwd: root, prompt: "task" });
 assert.equal(unavailable.descriptor.incomplete, true);
});

const task: WorkerTask = { mode: "scout", objective: "inspect", preset: "fast", sessionMode: "new" };
const route = { requestedPreset: "fast" as const, resolvedPreset: "fast" as const, routeReason: ["fixture"], provider: "fixture", modelId: "fixture/model", thinking: "low" as const };
const fixture = fileURLToPath(new URL("./fixtures/transcript-child.mjs", import.meta.url));

test("real process pipeline publishes live descriptor and preserves UTF-8 chunks/new/fork/end states", { timeout: 15000 }, async (t) => {
 const root = temporary(t);
 for (const mode of ["new", "fork"] as const) {
  for (const scenario of ["complete", "fail", "bad-json"]) {
   const prompt = "实际任务🧪";
   const progress: any[] = [];
   const child = await runPiWorker({ ...task, sessionMode: mode }, route, root, "DO_NOT_LOG_SYSTEM_PROMPT", prompt, undefined, 5000, 1, (patch) => {
    progress.push(patch);
    if (patch.transcript && !patch.transcript.incomplete) assert.equal(records(root, patch.transcript.id)[0].id, patch.transcript.id);
   }, { owner: createWorkerOwner(), transcriptRoot: root, invocation: { command: process.execPath, args: [fixture, scenario, prompt] }, sessionSnapshot: { branchJsonl: JSON.stringify({ type: "message", id: "parent", parentId: null, message: { role: "user", content: "fork-parent" } }) } });
   assert.ok(child.transcript);
   assert.ok(progress.some((patch) => patch.transcript?.id === child.transcript!.id && patch.phase !== "子进程已完成"));
   const all = records(root, child.transcript.id);
   assert.equal(all[0].entries.length, mode === "fork" ? 1 : 0);
   assert.equal(all.filter((event) => event.type === "message_end" && event.message?.role === "user").length, 1);
   assert.equal(all.find((event) => event.type === "message_update").assistantMessageEvent.delta, "中文 🧪\u2028完整");
   assert.ok(all.some((event) => event.type === "tool_execution_end"));
   assert.equal(all.at(-1).status, scenario === "complete" ? "completed" : scenario === "fail" ? "failed" : "incomplete");
   assert.ok(!JSON.stringify(all).includes("DO_NOT_LOG_SYSTEM_PROMPT"));
   if (scenario === "bad-json") assert.equal(child.transcript.error, "protocol_error");
  }
 }
});

test("cancelled child keeps a readable transcript and final descriptor", { timeout: 10000 }, async (t) => {
 const root = temporary(t);
 const controller = new AbortController();
 let liveID: string | undefined;
 const child = await runPiWorker(task, route, root, "system", "actual", controller.signal, 5000, 1, (patch) => {
  if (patch.transcript && !liveID) {
   liveID = patch.transcript.id;
   const timer = setTimeout(() => controller.abort(), 200);
   t.after(() => clearTimeout(timer));
  }
 }, { owner: createWorkerOwner(), transcriptRoot: root, invocation: { command: process.execPath, args: [fixture, "wait", "actual"] } });
 assert.equal(child.aborted, true);
 assert.equal(child.transcript?.id, liveID);
 assert.equal(records(root, liveID!).at(-1).status, "cancelled");
});

test("recording IO failure does not fail execution, and timeout/spawn failure close logs", { timeout: 10000 }, async (t) => {
 const root = temporary(t);
 const occupied = path.join(root, "occupied");
 fs.writeFileSync(occupied, "not a directory");
 const child = await runPiWorker(task, route, root, "system", "actual", undefined, 5000, 1, undefined,
  { owner: createWorkerOwner(), transcriptRoot: occupied, invocation: { command: process.execPath, args: [fixture, "complete", "actual"] } });
 assert.equal(child.exitCode, 0);
 assert.equal(child.transcript?.error, "io_error");
 const timeout = await runPiWorker(task, route, root, "system", "actual", undefined, 200, 1, undefined,
  { owner: createWorkerOwner(), transcriptRoot: root, invocation: { command: process.execPath, args: [fixture, "wait", "actual"] } });
 assert.equal(timeout.timedOut, true);
 assert.equal(records(root, timeout.transcript!.id).at(-1).status, "timed_out");
 const missing = await runPiWorker(task, route, root, "system", "actual", undefined, 5000, 1, undefined,
  { owner: createWorkerOwner(), transcriptRoot: root, invocation: { command: path.join(root, "missing-executable"), args: [] } });
 assert.notEqual(missing.exitCode, 0);
 assert.equal(records(root, missing.transcript!.id).at(-1).status, "failed");
});

test("configured agent directory is the default storage root", (t) => {
 const root = temporary(t);
 const before = process.env.PI_CODING_AGENT_DIR;
 try {
  process.env.PI_CODING_AGENT_DIR = root;
  const writer = new WorkerTranscriptWriter({ cwd: root, prompt: "task" });
  writer.finish("completed");
  assert.equal(records(root, writer.descriptor.id)[0].cwd, root);
 } finally {
  if (before === undefined) delete process.env.PI_CODING_AGENT_DIR;
  else process.env.PI_CODING_AGENT_DIR = before;
 }
});

test("snapshot metadata survives sanitization and parent history persistence", () => {
 const transcript = { version: 1 as const, id: "12345678-1234-4234-8234-123456789abc", incomplete: true, error: "size_limit" as const };
 const batch: any = { id: "batch", ui: { kind: "worker-ui", originToolCallId: "call", startedAt: 1, total: 1, completed: 1, limit: 1, tasks: [{ index: 0, mode: "scout", objective: "inspect", status: "completed", requestedPreset: "fast", attempt: 1, phase: "done", activities: [], usage: {}, toolCalls: 0, transcript }] } };
 const snapshot = batchUiSnapshot(batch);
 assert.deepEqual(snapshot.tasks[0].transcript, transcript);
 assert.notEqual(snapshot.tasks[0].transcript, transcript);
 const restored = workerBranchSnapshots([
  { type: "message", message: { role: "assistant", content: [{ type: "toolCall", id: "call", name: "worker", arguments: { tasks: [task] } }] } },
  { type: "custom", customType: "worker-snapshot-v1", data: JSON.parse(JSON.stringify(snapshot)) },
 ]);
 assert.deepEqual(restored.get("batch")?.tasks[0].transcript, transcript);
});
