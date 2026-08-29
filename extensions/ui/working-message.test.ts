import assert from "node:assert/strict";
import test from "node:test";
import { workingMessage } from "./working-message.ts";

test("working message renders combined input and output usage", () => {
    assert.equal(
        workingMessage(undefined, { input: 42_400, output: 8_000 }, 3, 15.2, 840),
        "Turn 3 · ↑42k ↓8k · 15.2 TPS · TTFT 840ms · 0s",
    );
});

test("working message omits zero output usage", () => {
    assert.equal(workingMessage(undefined, { input: 120 }, 1), "Turn 1 · ↑120 · 0s");
});
