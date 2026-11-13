import assert from "node:assert/strict";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import test, { type TestContext } from "node:test";
import { WorkerTranscriptReader } from "./transcript-reader.ts";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
const id = "12345678-1234-4234-8234-123456789abc";
const descriptor = { version: 1 as const, id };
const header = JSON.stringify({ type: "worker_transcript_header", version: 1, id, entries: [] }) + "\n";
const message = JSON.stringify({ type: "message_end", message: { role: "user", content: "中文 🧪" } }) + "\n";
function fixture(t: TestContext) {
	const base = fs.mkdtempSync(path.join(os.tmpdir(), "worker-reader-"));
	t.after(() => fs.rmSync(base, { recursive: true, force: true }));
	const agent = path.join(base, "custom-agent");
	const primary = path.join(base, "worker-transcripts", `${id}.jsonl`);
	const legacy = path.join(agent, "worker-transcripts", `${id}.jsonl`);
	fs.mkdirSync(path.dirname(primary), { recursive: true });
	fs.mkdirSync(path.dirname(legacy), { recursive: true });
	const reader = new WorkerTranscriptReader(() => agent);
	return { base, agent, primary, legacy, reader, read: (cursor?: string) => reader.read({ descriptor, cursor }) };
}

test("bounded incremental reads retain partial UTF8, are retryable, and reset on rewrite/replacement", t => {
	const f = fixture(t), split = Buffer.from(message).indexOf(Buffer.from("🧪")) + 2;
	fs.writeFileSync(f.primary, Buffer.concat([Buffer.from(header), Buffer.from(message).subarray(0, split)]));
	let a = f.read();
	assert.equal(a.reset, true); assert.equal(a.partial, true); assert.equal(a.records.length, 1);
	fs.appendFileSync(f.primary, Buffer.from(message).subarray(split));
	let b = f.read(a.cursor);
	assert.equal(b.reset, false); assert.equal(b.partial, false); assert.equal(b.records.length, 1);
	assert.equal((b.records[0].message as any).content, "中文 🧪");
	assert.deepEqual(f.read(a.cursor).records, b.records);
	assert.equal(f.read(b.cursor).records.length, 0);
	fs.writeFileSync(f.primary, header);
	a = f.read(b.cursor); assert.equal(a.reset, true);
	fs.writeFileSync(f.primary + ".new", header + message); fs.renameSync(f.primary + ".new", f.primary);
	b = f.read(a.cursor); assert.equal(b.reset, true); assert.equal(b.records.length, 2);
	fs.writeFileSync(f.primary, header + message.replace("中文", "文本"));
	assert.equal(f.read(b.cursor).reset, true, "same inode/size rewrite detected by anchor");
});

test("opaque cursors do not accept offsets, headers, foreign descriptors, or invalid IDs", t => {
	const f = fixture(t); fs.writeFileSync(f.primary, header);
	for (const request of [null, {}, { descriptor: { version: 2, id } }, { descriptor: { version: 1, id: "../secret" } }, { descriptor, cursor: { offset: 10, header: true } }]) {
		assert.throws(() => f.reader.read(request), /Invalid Worker transcript/);
	}
	const first = f.read();
	assert.throws(() => f.reader.read({ descriptor: { version: 1, id: "00000000-0000-0000-0000-000000000000" }, cursor: first.cursor }), /another Worker/);
	f.reader.clear(); assert.equal(f.read(first.cursor).reset, true, "reload resets stale cursors");
	fs.writeFileSync(f.primary, header.replace(id, "00000000-0000-0000-0000-000000000000"));
	assert.throws(() => f.read(), /header/);
	fs.writeFileSync(f.primary, header + header); assert.throws(() => f.read(), /Unexpected/);
});

test("large records are assembled across bounded chunks and oversized recordings rejected", t => {
	const f = fixture(t);
	fs.writeFileSync(f.primary, header + JSON.stringify({ type: "message_end", text: "x".repeat(5 << 20) }) + "\n");
	let a = f.read(); assert.equal(a.more, true); assert.equal(a.partial, true); assert.equal(a.records.length, 1);
	a = f.read(a.cursor); assert.equal(a.more, false); assert.equal(a.partial, false); assert.equal(a.records.length, 1);
	assert.equal((a.records[0].text as string).length, 5 << 20);
	fs.truncateSync(f.primary, (65 << 20) + 1); assert.throws(() => f.read(), /limit/);
});

test("legacy fallback promotes to primary and never reopens an obsolete copy", t => {
	const f = fixture(t); fs.writeFileSync(f.legacy, header + message);
	let a = f.read(); assert.equal(a.records.length, 2);
	assert.equal(f.read(a.cursor).records.length, 0);
	fs.writeFileSync(f.primary, header + message.replace("中文", "新文"));
	a = f.read(a.cursor); assert.equal(a.reset, true); assert.equal(a.records.length, 2);
	fs.unlinkSync(f.primary);
	assert.throws(() => f.read(a.cursor), { code: "transcript-not-found" });
	assert.equal(fs.readFileSync(f.legacy, "utf8"), header + message);
});

for (const kind of ["symlink", "hardlink", "directory", "invalid", "root-symlink", "unreadable", "root-unreadable", "legacy-symlink"]) test(`unsafe ${kind} cannot bypass primary via legacy`, t => {
	const f = fixture(t); fs.writeFileSync(f.legacy, header);
	if (kind === "symlink") fs.symlinkSync(f.legacy, f.primary);
	if (kind === "hardlink") fs.linkSync(f.legacy, f.primary);
	if (kind === "directory") fs.mkdirSync(f.primary);
	if (kind === "invalid") fs.writeFileSync(f.primary, "bad JSON\n");
	if (kind === "root-symlink") { fs.rmdirSync(path.dirname(f.primary)); fs.symlinkSync(path.dirname(f.legacy), path.dirname(f.primary)); }
	if (kind === "legacy-symlink") { const outside = path.join(f.base, "outside"); fs.renameSync(f.legacy, outside); fs.symlinkSync(outside, f.legacy); }
	if (kind === "unreadable") { fs.writeFileSync(f.primary, header); fs.chmodSync(f.primary, 0); }
	if (kind === "root-unreadable") fs.chmodSync(path.dirname(f.primary), 0);
	if ((kind === "unreadable" || kind === "root-unreadable") && process.getuid?.() === 0) return t.skip("root bypasses file permissions");
	try { assert.throws(() => f.read()); }
	finally { if (kind === "root-unreadable") fs.chmodSync(path.dirname(f.primary), 0o700); }
});

test("real child fixture records are read without altering Unicode or terminal state", t => {
	const f = fixture(t);
	const output = execFileSync(process.execPath, [fileURLToPath(new URL("./fixtures/transcript-child.mjs", import.meta.url)), "complete", "real prompt"], { encoding: "utf8" });
	const events = output.trim().split("\n").map(line => JSON.parse(line)).filter(event => event.type !== "session");
	fs.writeFileSync(f.primary, header + events.map(event => JSON.stringify(event) + "\n").join("") + JSON.stringify({ type: "worker_transcript_end", status: "completed", complete: true }) + "\n");
	const result = f.read();
	assert.equal(result.partial, false);
	assert.equal(result.more, false);
	assert.equal(result.records.at(-1)?.status, "completed");
	const text = JSON.stringify(result.records);
	for (const expected of ["real prompt", "中文 🧪", "full content"]) assert.ok(text.includes(expected), expected);
});

test("cursor cache is bounded and evicted tokens restart safely", t => {
	const f = fixture(t); fs.writeFileSync(f.primary, header);
	const first = f.read();
	for (let i = 0; i < 40; i++) f.read();
	assert.equal(f.read(first.cursor).reset, true);
	assert.equal((f.reader as any).positions.size, 32);
});
