import assert from "node:assert/strict";
import test from "node:test";
import { workerBranchSnapshots, WORKER_SNAPSHOT_ENTRY } from "./history.ts";

const call = (id: string, args: unknown) => ({ type: "message", message: { role: "assistant", content: [{ type: "toolCall", name: "worker", id, arguments: args }] } });
const snapshot = (batchId: string, originToolCallId: string, revision: number) => ({ kind: "worker-ui", batchId, originToolCallId, revision, tasks: [], startedAt: 1, total: 0, completed: 0, limit: 1 });
const saved = (data: unknown) => ({ type: "custom", customType: WORKER_SNAPSHOT_ENTRY, data });
const result = (toolCallId: string, details: unknown) => ({ type: "message", message: { role: "toolResult", toolName: "worker", toolCallId, details } });

test("history revision rejects late-written stale results, foreign batches/origins, unknown and malformed records", () => {
	const starts = [call("a", { task: {} }), call("b", { tasks: [{}] }), call("answer-a", { batchId: "batch-a" })];
	const current = snapshot("batch-a", "a", 3);
	const branch = [...starts, saved(snapshot("batch-a", "a", 1)), saved(current),
		result("answer-a", snapshot("batch-a", "a", 2)),
		result("answer-a", snapshot("batch-b", "b", 99)),
		saved(snapshot("batch-a", "b", 99)), saved(snapshot("unknown", "missing", 99)),
		saved({ ...current, tasks: null }), saved(snapshot("batch-b", "b", 4))];
	const index = workerBranchSnapshots(branch);
	assert.equal(index.size, 2);
	assert.equal(index.get("batch-a")!.revision, 3);
	assert.equal(index.get("batch-a")!.originToolCallId, "a");
	assert.equal(index.get("batch-b")!.revision, 4);
	const isolated = workerBranchSnapshots([starts[0], saved(snapshot("batch-a", "a", 1))]);
	assert.equal(isolated.size, 1); assert.equal(isolated.get("batch-a")!.revision, 1);
});

test("legacy tool-result snapshots derive origin from exact start call, not continuation or another tool", () => {
	const details = { ...snapshot("legacy", "a", 1), originToolCallId: undefined, revision: undefined };
	const branch = [call("a", { task: {} }), call("wait", { batchId: "legacy" }), result("a", details), result("wait", { ...details, completed: 1 })];
	assert.equal(workerBranchSnapshots(branch).get("legacy")!.originToolCallId, "a");
	assert.equal(workerBranchSnapshots(branch).get("legacy")!.completed, 1);
	assert.equal(workerBranchSnapshots(branch.slice(1)).size, 0);
	assert.equal(workerBranchSnapshots([{ ...branch[0], message: { role: "assistant", content: [{ type: "toolCall", name: "other", id: "a", arguments: { task: {} } }] } }, result("a", details)]).size, 0);
});
