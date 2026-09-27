import assert from "node:assert/strict";
import test from "node:test";
import { present } from "./presentation.ts";

test("presentation boundary isolates synchronous throws and rejected async callbacks", async () => {
	assert.doesNotThrow(() => present(() => { throw new Error("detached UI"); }));
	present(async () => { throw new Error("async detached UI"); });
	await new Promise((resolve) => setImmediate(resolve));
});
