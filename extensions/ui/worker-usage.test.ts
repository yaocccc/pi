import assert from "node:assert/strict";
import test from "node:test";
import { combineTokenUsage, WorkerUsageTracker } from "./worker-usage.ts";

test("combined usage adds worker input and output to the main arrows", () => {
    assert.deepEqual(
        combineTokenUsage({ input: 22_000, output: 5_000 }, { input: 30_000, output: 3_000 }),
        { input: 52_000, output: 8_000 },
    );
});

test("worker usage snapshots replace per task and sum concurrent tasks", () => {
    const tracker = new WorkerUsageTracker();
    assert.equal(tracker.update({ taskId: "call-1:0", input: 10_000, output: 1_000 }), true);
    assert.equal(tracker.update({ taskId: "call-1:1", input: 20_000, output: 2_000 }), true);
    assert.deepEqual(tracker.total(), { input: 30_000, output: 3_000 });

    tracker.update({ taskId: "call-1:0", input: 15_000, output: 1_500 });
    assert.deepEqual(tracker.total(), { input: 35_000, output: 3_500 });

    tracker.reset();
    assert.deepEqual(tracker.total(), { input: 0, output: 0 });
});

test("worker usage tracker rejects malformed snapshots", () => {
    const tracker = new WorkerUsageTracker();
    assert.equal(tracker.update({ taskId: "", input: 10, output: 1 }), false);
    assert.equal(tracker.update({ taskId: "call-1:0", input: Number.NaN, output: 1 }), false);
    assert.deepEqual(tracker.total(), { input: 0, output: 0 });
});
