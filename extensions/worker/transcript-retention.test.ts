import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import { randomUUID } from "node:crypto";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { cleanupTranscripts, holdTranscript, WORKER_TRANSCRIPT_BUDGET, WORKER_TRANSCRIPT_CHECK_INTERVAL } from "./transcript-retention.ts";
import { WorkerTranscriptWriter } from "./transcript.ts";

const interval = WORKER_TRANSCRIPT_CHECK_INTERVAL;
const fixture = fileURLToPath(new URL("./fixtures/retention-child.mjs", import.meta.url));
function directory(t: any): string {
 const root = fs.mkdtempSync(path.join(os.tmpdir(), "worker-retention-"));
 t.after(() => fs.rmSync(root, { recursive: true, force: true }));
 return root;
}
function log(root: string, age: number, terminal = true, id = randomUUID()): string {
 const file = path.join(root, `${id}.jsonl`);
 fs.writeFileSync(file, '{"type":"worker_transcript_header"}\n' + (terminal ? '{"type":"worker_transcript_end","complete":true}\n' : "partial"));
 fs.utimesSync(file, age / 1000, age / 1000);
 return file;
}
function child(mode: string, root: string, idOrTime: string | number, budget = 0) {
 return spawn(process.execPath, ["--experimental-transform-types", fixture, mode, root, String(idOrTime), String(budget)], { stdio: ["pipe", "pipe", "pipe"] });
}

test("under or exactly 10GiB budget retains every age; exceeding deletes oldest first", async (t) => {
 assert.equal(WORKER_TRANSCRIPT_BUDGET, 10 * 1024 ** 3);
 assert.equal(interval, 24 * 60 * 60 * 1000);
 const root = directory(t);
 const files = [log(root, 100), log(root, 200), log(root, 300)];
 const size = fs.statSync(files[0]).size;
 await cleanupTranscripts(root, { now: 10 * interval, maxBytes: size * 3 });
 assert.ok(files.every(fs.existsSync));
 await cleanupTranscripts(root, { now: 11 * interval, maxBytes: size * 2 });
 assert.deepEqual(files.map(fs.existsSync), [false, true, true]);
 await cleanupTranscripts(root, { now: 12 * interval, maxBytes: size });
 assert.deepEqual(files.map(fs.existsSync), [false, false, true]);
});

test("cross-process active lease survives regardless of age, then stale crash log is reclaimed", async (t) => {
 const root = directory(t);
 const id = randomUUID();
 const live = child("crash", root, id);
 const exited = once(live, "close");
 t.after(() => live.stdin.end());
 await once(live.stdout, "data");
 const active = log(root, 0, false, id);
 const old = log(root, 1);
 await cleanupTranscripts(root, { now: 20 * interval, maxBytes: 0 });
 assert.ok(fs.existsSync(active));
 assert.ok(!fs.existsSync(old));
 live.stdin.end();
 assert.equal((await exited)[0], 0);
 await cleanupTranscripts(root, { now: 21 * interval, maxBytes: 0 });
 assert.ok(!fs.existsSync(active));
 assert.ok(!fs.existsSync(path.join(root, `${id}.active`)));
});

test("lease survives size-limit and is released only when execution finishes", async (t) => {
 const root = directory(t);
 const writer = new WorkerTranscriptWriter({ root, cwd: root, prompt: "task", maxBytes: 4096 });
 const dir = path.join(root, "worker-transcripts");
 const id = writer.descriptor.id;
 writer.record({ type: "message_end", message: { role: "assistant", content: "x".repeat(5000) } });
 assert.equal(writer.descriptor.error, "size_limit");
 const now = Date.now() + 10 * interval;
 await cleanupTranscripts(dir, { now, maxBytes: 0 });
 assert.ok(fs.existsSync(path.join(dir, `${id}.jsonl`)));
 assert.ok(fs.existsSync(path.join(dir, `${id}.active`)));
 writer.finish("completed");
 assert.equal(JSON.parse(fs.readFileSync(path.join(dir, `${id}.active`), "utf8")).finished, true);
 await cleanupTranscripts(dir, { now: now + interval * 2, maxBytes: 0 });
 assert.ok(!fs.existsSync(path.join(dir, `${id}.jsonl`)));
});

test("finished leases recover partial terminal failures and orphan metadata without dropping retained logs", async (t) => {
 const root = directory(t);
 const id = randomUUID();
 const release = holdTranscript(root, id);
 const incomplete = log(root, 0, false, id);
 release();
 assert.equal(JSON.parse(fs.readFileSync(path.join(root, `${id}.active`), "utf8")).finished, true);
 const orphan = randomUUID();
 holdTranscript(root, orphan)();
 const retained = log(root, 1);
 await cleanupTranscripts(root, { now: 25 * interval, maxBytes: 10000 });
 assert.ok(fs.existsSync(incomplete));
 assert.ok(fs.existsSync(retained));
 assert.ok(!fs.existsSync(path.join(root, `${orphan}.active`)));
 await cleanupTranscripts(root, { now: 26 * interval, maxBytes: fs.statSync(retained).size });
 assert.ok(!fs.existsSync(incomplete));
 assert.ok(fs.existsSync(retained));
});

test("PID reuse does not keep dead Linux leases indefinitely", { skip: process.platform !== "linux" }, async (t) => {
 const root = directory(t);
 const id = randomUUID();
 const file = log(root, 0, false, id);
 fs.writeFileSync(path.join(root, `${id}.active`), JSON.stringify({ version: 1, pid: process.pid, start: "previous-boot:wrong-start-time" }));
 await cleanupTranscripts(root, { now: 30 * interval, maxBytes: 0 });
 assert.ok(!fs.existsSync(file));
});

test("one persistent lock throttles separate instances for a full 24 hours, not calendar days", async (t) => {
 const root = directory(t);
 const first = log(root, 0);
 const time = 40 * interval + interval - 1;
 const initial = child("cleanup", root, time);
 assert.equal((await once(initial, "close"))[0], 0);
 assert.ok(!fs.existsSync(first));
 const retained = log(root, 0);
 const lock = path.join(root, ".retention.lock");
 assert.equal(fs.statSync(lock).mode & 0o777, 0o600);
 assert.ok(Math.abs(fs.statSync(lock).mtimeMs - time) < 1);
 for (const now of [time, time + 2, time + interval - 1]) {
  const other = child("cleanup", root, now);
  assert.equal((await once(other, "close"))[0], 0);
  assert.ok(fs.existsSync(retained));
 }
 const later = child("cleanup", root, time + interval);
 assert.equal((await once(later, "close"))[0], 0);
 assert.ok(!fs.existsSync(retained));
 assert.deepEqual(fs.readdirSync(root), [".retention.lock"]);
 assert.ok(Math.abs(fs.statSync(lock).mtimeMs - (time + interval)) < 1);
});

test("expired lock recovers after a crashed scan without managing old bucket files", async (t) => {
 const root = directory(t);
 const lock = path.join(root, ".retention.lock");
 fs.writeFileSync(lock, "", { mode: 0o600 });
 fs.utimesSync(lock, 0, 0);
 const oldBucket = path.join(root, ".retention-0.check");
 fs.writeFileSync(oldBucket, "legacy marker");
 const file = log(root, 0);
 await cleanupTranscripts(root, { now: interval, maxBytes: 0 });
 assert.ok(!fs.existsSync(file));
 assert.deepEqual(fs.readdirSync(root).sort(), [".retention-0.check", ".retention.lock"]);
 assert.equal(fs.readFileSync(oldBucket, "utf8"), "legacy marker");
});

test("failed scan retains its timestamp and does not enumerate again until the next day", async (t) => {
 const root = directory(t);
 const file = log(root, 0);
 const read = fs.promises.readdir.bind(fs.promises);
 let scans = 0;
 const spy = t.mock.method(fs.promises, "readdir", async (...args: any[]) => {
  scans++;
  if (scans === 1) throw Object.assign(new Error("unavailable"), { code: "EACCES" });
  return (read as any)(...args);
 });
 try {
  await cleanupTranscripts(root, { now: interval, maxBytes: 0 });
  await cleanupTranscripts(root, { now: interval + 1, maxBytes: 0 });
  await cleanupTranscripts(root, { now: 2 * interval - 1, maxBytes: 0 });
  assert.equal(scans, 1);
  assert.ok(fs.existsSync(file));
  await cleanupTranscripts(root, { now: 2 * interval, maxBytes: 0 });
  assert.equal(scans, 2);
  assert.ok(!fs.existsSync(file));
 } finally { spy.mock.restore(); }
});

test("concurrent collectors claim once and cannot affect the next same-interval log", async (t) => {
 const root = directory(t);
 const files = Array.from({ length: 20 }, (_, i) => log(root, i));
 const jobs = Array.from({ length: 5 }, () => child("cleanup", root, 50 * interval));
 assert.deepEqual(await Promise.all(jobs.map(async (job) => (await once(job, "close"))[0])), [0, 0, 0, 0, 0]);
 assert.ok(files.every((file) => !fs.existsSync(file)));
 const next = log(root, 0);
 await cleanupTranscripts(root, { now: 50 * interval, maxBytes: 0 });
 assert.ok(fs.existsSync(next));
});

test("links, unknown files, malformed leases and unfinished legacy logs are protected", async (t) => {
 const root = directory(t);
 const outside = directory(t);
 const target = log(outside, 0);
 const alias = path.join(root, `${randomUUID()}.jsonl`);
 fs.symlinkSync(target, alias);
 const unknown = path.join(root, "ordinary-session.jsonl");
 fs.writeFileSync(unknown, "keep");
 const ambiguous = log(root, 0, false);
 const legacyLimit = log(root, 0, false);
 fs.appendFileSync(legacyLimit, '\n{"type":"worker_transcript_end","status":"incomplete","complete":false}\n');
 const id = randomUUID();
 const badLease = log(root, 0, true, id);
 fs.writeFileSync(path.join(root, `${id}.active`), "{");
 const symlinkID = randomUUID();
 const linkedLease = log(root, 0, true, symlinkID);
 fs.symlinkSync(target, path.join(root, `${symlinkID}.active`));
 const done = log(root, 0);
 await cleanupTranscripts(root, { now: 60 * interval, maxBytes: 0 });
 for (const file of [target, alias, unknown, ambiguous, legacyLimit, badLease, linkedLease]) assert.ok(fs.existsSync(file));
 assert.ok(!fs.existsSync(done));
 const rootAlias = path.join(directory(t), "alias");
 fs.symlinkSync(outside, rootAlias);
 await cleanupTranscripts(rootAlias, { now: 61 * interval, maxBytes: 0 });
 assert.ok(fs.existsSync(target));
});

test("root/claim IO errors and disappeared storage never fail task execution", async (t) => {
 const root = directory(t);
 const file = log(root, 0);
 fs.symlinkSync(file, path.join(root, ".retention.lock"));
 await cleanupTranscripts(root, { now: 70 * interval, maxBytes: 0 });
 assert.ok(fs.existsSync(file));
 await cleanupTranscripts(path.join(root, "missing"));
 await cleanupTranscripts(file);
 const release = holdTranscript(root, randomUUID());
 fs.rmSync(root, { recursive: true, force: true });
 assert.doesNotThrow(release);
});
