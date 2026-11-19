import assert from "node:assert/strict";
import test from "node:test";
import { WorkerEventLines } from "./event-lines.ts";

test("worker stdout: emit complete lines in order across every UTF-8 split, including EOF without LF", () => {
	const input = Buffer.from(' \n\r\n{"text":"字🧪"}\r\n{"id":2}\n{"id":3}');
	for (let size = 1; size <= input.length; size++) {
		const parser = new WorkerEventLines(64), seen: string[] = [];
		const receive = (line: string) => seen.push(line);
		for (let i = 0; i < input.length; i += size) assert.ok(parser.write(input.subarray(i, i + size), receive));
		parser.end(receive);
		assert.deepEqual(seen, ['{"text":"字🧪"}\r', '{"id":2}', '{"id":3}']);
	}
});

test("worker stdout: cap decoded bytes, not characters or individual pipe chunks", () => {
	const seen: string[] = [], receive = (line: string) => seen.push(line);
	const exact = new WorkerEventLines(4);
	for (const byte of Buffer.from("🧪\n")) assert.ok(exact.write(Buffer.from([byte]), receive));
	assert.deepEqual(seen, ["🧪"]);
	for (const newline of ["", "\n"]) {
		const over = new WorkerEventLines(4);
		assert.ok(over.write(Buffer.from(" 🧪").subarray(0, 2), receive));
		assert.equal(over.write(Buffer.from(" 🧪" + newline).subarray(2), receive), false);
	}
	assert.deepEqual(seen, ["🧪"]);
});

test("worker stdout: a fragmented event is delivered once with linear byte-count work", (t) => {
	// Count input to byte-length checks instead of relying on CI wall-clock limits.
	const original = Buffer.byteLength;
	let measuredChars = 0;
	t.mock.method(Buffer, "byteLength", (...args: Parameters<typeof original>) => {
		if (typeof args[0] === "string") measuredChars += args[0].length;
		return original(...args);
	});
	const parser = new WorkerEventLines(8 << 20), chunk = Buffer.alloc(2048, "x");
	const seen: string[] = [], receive = (line: string) => seen.push(line);
	for (let i = 0; i < 4096; i++) assert.ok(parser.write(chunk, receive));
	assert.equal(seen.length, 0);
	assert.ok(parser.write(Buffer.from("\n"), receive));
	assert.equal(seen.length, 1);
	assert.equal(seen[0].length, 8 << 20);
	assert.ok(measuredChars <= 2 * seen[0].length, `counted ${measuredChars} characters for one 8 MiB line`);
});
