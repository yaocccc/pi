import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { loadAutonameConfig } from "./config.ts";
import { DEFAULT_CONFIG } from "./helpers.ts";

test("loadAutonameConfig merges valid values with safe defaults", async () => {
    const directory = await mkdtemp(join(tmpdir(), "autoname-test-"));
    const path = join(directory, "autoname.json");
    await writeFile(path, JSON.stringify({
        enabled: false,
        notify: false,
        cooldownSeconds: 30,
        model: "auto",
        reasoning: "high",
    }));
    assert.deepEqual(await loadAutonameConfig(path), {
        enabled: false,
        notify: false,
        cooldownSeconds: 30,
        model: "auto",
        reasoning: "high",
    });

    await writeFile(path, JSON.stringify({ model: "openai-codex/gpt-5.6-sol" }));
    assert.equal((await loadAutonameConfig(path)).model, "openai-codex/gpt-5.6-sol");

    await writeFile(path, JSON.stringify({
        enabled: "yes",
        cooldownSeconds: -1,
        model: "not a model",
        reasoning: "infinite",
    }));
    assert.deepEqual(await loadAutonameConfig(path), DEFAULT_CONFIG);
    await writeFile(path, "not json");
    assert.deepEqual(await loadAutonameConfig(path), DEFAULT_CONFIG);
});
