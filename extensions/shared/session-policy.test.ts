import assert from "node:assert/strict";
import test from "node:test";
import { isPrimarySession } from "./session-policy.ts";

test("background integrations use explicit session role, never UI availability", () => {
	for (const env of [{}, { PI_WORKER_DEPTH: "0" }, { PI_WORKER_DEPTH: "" }]) assert.equal(isPrimarySession(env), true);
	for (const depth of ["1", "2", "10"]) assert.equal(isPrimarySession({ PI_WORKER_DEPTH: depth }), false);
});
