import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, utimes, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import {
    CACHE_MAX_FILES,
    CACHE_PRUNE_COUNT,
    TranslationCache,
} from "./cache.ts";
import { TranslationCoordinator } from "./coordinator.ts";
import { TranslationSession } from "./index.ts";
import {
    formatThinkingDisplay,
    parseTranslationResponse,
    preservesThinkingSegments,
    translationBlockKey,
} from "./state.ts";

type Request = {
    source: string;
    signal: AbortSignal;
    resolve: (translation: string | undefined) => void;
};

const delay = (milliseconds: number): Promise<void> =>
    new Promise((resolve) => setTimeout(resolve, milliseconds));

const TEST_METRICS = {
    inputTokens: 120,
    outputTokens: 30,
    durationMs: 450,
};

const harness = (idleMs = 15, maxThinkingLength = 200) => {
    const requests: Request[] = [];
    let changes = 0;
    const coordinator = new TranslationCoordinator({
        idleMs,
        maxThinkingLength,
        onChange: () => { changes++; },
        translate: (source, signal) => new Promise((resolve) => {
            requests.push({ source, signal, resolve });
        }),
    }, true);
    return { coordinator, requests, changes: () => changes };
};

test("continuous deltas translate after idle without waiting for thinking_end", async () => {
    const { coordinator, requests } = harness();
    coordinator.thinkingStart(100, 0);
    coordinator.thinkingDelta(100, 0, "first");
    await delay(5);
    coordinator.thinkingDelta(100, 0, " stable");
    await delay(25);

    assert.equal(requests.length, 1);
    assert.equal(requests[0]!.source, "first stable");
    assert.deepEqual(coordinator.get(translationBlockKey(100, 0), "first stable"), { state: "pending" });
    coordinator.shutdown();
});

test("a resumed block schedules its bounded correction before delayed thinking_end", async () => {
    const { coordinator, requests } = harness();
    const key = translationBlockKey(150, 0);
    coordinator.thinkingStart(150, 0);
    coordinator.thinkingDelta(150, 0, "draft");
    await delay(25);
    assert.equal(requests.length, 1);

    coordinator.thinkingDelta(150, 0, " refined");
    await delay(25);
    assert.equal(requests.length, 2);
    assert.equal(requests[1]!.source, "draft refined");
    assert.deepEqual(coordinator.get(key, "draft refined"), { state: "pending" });
    coordinator.shutdown();
});

test("a new thinking block is a fast flush boundary for the preceding block", () => {
    const { coordinator, requests } = harness(1_000);
    coordinator.thinkingStart(200, 0);
    coordinator.thinkingDelta(200, 0, "block one");
    coordinator.thinkingStart(200, 1);

    assert.equal(requests.length, 1);
    assert.equal(requests[0]!.source, "block one");
    coordinator.shutdown();
});

test("a completed translation stays visible when its block grows and a second block appears", async () => {
    const { coordinator, requests } = harness();
    const firstKey = translationBlockKey(250, 0);
    coordinator.thinkingStart(250, 0);
    coordinator.thinkingDelta(250, 0, "first");
    await delay(25);
    requests[0]!.resolve("第一段");
    await delay(0);
    assert.deepEqual(coordinator.get(firstKey, "first"), { state: "ready", translation: "第一段" });

    coordinator.thinkingDelta(250, 0, " expanded");
    coordinator.thinkingStart(250, 1);
    assert.equal(requests.length, 2);
    assert.equal(requests[1]!.source, "first expanded");
    assert.deepEqual(coordinator.get(firstKey, "first expanded"), {
        state: "ready",
        translation: "第一段",
    });

    requests[1]!.resolve("第一段扩展");
    await delay(0);
    assert.deepEqual(coordinator.get(firstKey, "first expanded"), {
        state: "ready",
        translation: "第一段扩展",
    });
    coordinator.shutdown();
});

test("Operation aborted preserves completed translation and cancels only correction work", async () => {
    const { coordinator, requests } = harness();
    const key = translationBlockKey(275, 0);
    coordinator.thinkingStart(275, 0);
    coordinator.thinkingDelta(275, 0, "translated source");
    await delay(25);
    requests[0]!.resolve("已完成译文");
    await delay(0);

    coordinator.thinkingDelta(275, 0, " growing");
    await delay(25);
    assert.equal(requests.length, 2);
    coordinator.abortMessage(275);
    coordinator.abortMessage(275); // message_update:error + message_end:aborted

    assert.equal(requests[1]!.signal.aborted, true);
    assert.deepEqual(coordinator.get(key, "translated source growing"), {
        state: "ready",
        translation: "已完成译文",
    });
    requests[1]!.resolve("不应覆盖");
    await delay(0);
    assert.deepEqual(coordinator.get(key, "translated source growing"), {
        state: "ready",
        translation: "已完成译文",
    });
    coordinator.shutdown();
});

test("thinking_end can launch one final correction and stale provisional output is ignored", async () => {
    const { coordinator, requests } = harness();
    const key = translationBlockKey(300, 0);
    coordinator.thinkingStart(300, 0);
    coordinator.thinkingDelta(300, 0, "draft");
    await delay(25);
    assert.equal(requests.length, 1);

    coordinator.thinkingDelta(300, 0, " final");
    assert.equal(requests[0]!.signal.aborted, true);
    coordinator.thinkingEnd(300, 0, "draft final");
    assert.equal(requests.length, 2);
    assert.equal(requests[1]!.source, "draft final");

    requests[0]!.resolve("过期译文");
    await delay(0);
    assert.deepEqual(coordinator.get(key, "draft final"), { state: "pending" });
    requests[1]!.resolve("最终\n译文");
    await delay(0);
    assert.deepEqual(coordinator.get(key, "draft final"), {
        state: "ready",
        translation: "最终\n译文",
    });
    coordinator.shutdown();
});

test("disable, over-limit input, and message abort cancel or hide pending work", async () => {
    const disabled = harness();
    const disabledKey = translationBlockKey(400, 0);
    disabled.coordinator.thinkingStart(400, 0);
    disabled.coordinator.thinkingDelta(400, 0, "pending");
    await delay(25);
    disabled.coordinator.setEnabled(false);
    assert.equal(disabled.requests[0]!.signal.aborted, true);
    disabled.requests[0]!.resolve("不应显示");
    await delay(0);
    assert.equal(disabled.coordinator.get(disabledKey, "pending"), undefined);

    const limited = harness();
    limited.coordinator.thinkingStart(401, 0);
    limited.coordinator.thinkingDelta(401, 0, "short");
    await delay(25);
    limited.coordinator.thinkingDelta(401, 0, "界".repeat(201));
    assert.equal(limited.requests[0]!.signal.aborted, true);
    assert.equal(limited.coordinator.get(translationBlockKey(401, 0), `short${"界".repeat(201)}`), undefined);

    const aborted = harness();
    aborted.coordinator.thinkingStart(402, 0);
    aborted.coordinator.thinkingDelta(402, 0, "abort me");
    await delay(25);
    aborted.coordinator.abortMessage(402);
    assert.equal(aborted.requests[0]!.signal.aborted, true);
    assert.equal(aborted.coordinator.get(translationBlockKey(402, 0), "abort me"), undefined);

    disabled.coordinator.shutdown();
    limited.coordinator.shutdown();
    aborted.coordinator.shutdown();
});

test("pending shows a trailing globe while ready shows only the Chinese translation", () => {
    assert.equal(formatThinkingDisplay("thinking", { state: "pending" }), "thinking 🌐");
    assert.equal(formatThinkingDisplay("thinking", { state: "ready", translation: "中文译文" }), "中文译文");
    assert.equal(formatThinkingDisplay("thinking", { state: "pending" }).includes("\n"), false);
});

test("structured translation preserves separate thinking segments", () => {
    const source = "first block\n\nsecond block";
    assert.equal(
        parseTranslationResponse('["第一段", "第二段"]', 2),
        "第一段\n第二段",
    );
    assert.equal(preservesThinkingSegments(source, "第一段\n第二段"), true);
    assert.equal(preservesThinkingSegments(source, "第一段和第二段"), false);
    assert.equal(parseTranslationResponse("第一段和第二段", 2), undefined);
});

test("cache stores translation token usage and request duration", async (t) => {
    const directory = await mkdtemp(join(tmpdir(), "thinking-translation-metrics-test-"));
    t.after(() => rm(directory, { recursive: true, force: true }));
    const cache = new TranslationCache(directory);

    await cache.set("measured thought", "带指标的译文", TEST_METRICS);
    const raw = JSON.parse(await readFile(
        join(directory, `${cache.key("measured thought")}.json`),
        "utf8",
    ));
    assert.deepEqual(raw, {
        version: 2,
        translation: "带指标的译文",
        metrics: TEST_METRICS,
    });
    assert.equal(await cache.get("measured thought"), "带指标的译文");

    const legacyText = "legacy thought";
    await writeFile(
        join(directory, `${cache.key(legacyText)}.json`),
        '{"version":1,"translation":"旧版译文"}\n',
    );
    assert.equal(await new TranslationCache(directory).get(legacyText), "旧版译文");
});

test("translation selects compact single or structured multi-segment requests and caches metrics", async (t) => {
    const directory = await mkdtemp(join(tmpdir(), "thinking-translation-request-metrics-test-"));
    t.after(() => rm(directory, { recursive: true, force: true }));
    const cache = new TranslationCache(directory);
    type TranslationRequest = {
        systemPrompt: string;
        messages: Array<{ content: Array<{ type: string; text: string }> }>;
    };
    const requests: TranslationRequest[] = [];
    const ctx = {
        modelRegistry: {
            hasConfiguredAuth: () => true,
            complete: async (_model: unknown, request: TranslationRequest) => {
                requests.push(request);
                const input = request.messages[0]!.content[0]!.text;
                return {
                    content: [{
                        type: "text",
                        text: input.startsWith("[") ? '["第一段","第二段"]' : "模型译文",
                    }],
                    usage: { input: 37, output: 9 },
                    stopReason: "stop",
                };
            },
        },
    };
    const session = new TranslationSession(ctx as never, {} as never, 200, true, cache);
    const translate = (session as unknown as {
        translate(source: string, signal: AbortSignal): Promise<string | undefined>;
    }).translate.bind(session);

    assert.equal(await translate("model thought", new AbortController().signal), "模型译文");
    assert.equal(requests[0]!.systemPrompt.includes("JSON"), false);
    assert.equal(requests[0]!.messages[0]!.content[0]!.text, "model thought");

    assert.equal(
        await translate("first block\n\nsecond block", new AbortController().signal),
        "第一段\n第二段",
    );
    assert.equal(requests[1]!.systemPrompt.includes("JSON array"), true);
    assert.equal(
        requests[1]!.messages[0]!.content[0]!.text,
        '["first block","second block"]',
    );

    const raw = JSON.parse(await readFile(
        join(directory, `${cache.key("model thought")}.json`),
        "utf8",
    ));
    assert.equal(raw.metrics.inputTokens, 37);
    assert.equal(raw.metrics.outputTokens, 9);
    assert.equal(Number.isInteger(raw.metrics.durationMs), true);
    assert.equal(raw.metrics.durationMs >= 0, true);
    session.shutdown();
});

test("cache insert prunes the oldest 50 files and throttles cleanup for ten minutes", async (t) => {
    const directory = await mkdtemp(join(tmpdir(), "thinking-translation-prune-test-"));
    t.after(() => rm(directory, { recursive: true, force: true }));
    const oldestNames = Array.from({ length: CACHE_MAX_FILES + 1 }, (_, index) =>
        `old-${String(index).padStart(3, "0")}.json`);
    const baseTime = Date.now() / 1_000 - 10_000;
    await Promise.all(oldestNames.map(async (name, index) => {
        const path = join(directory, name);
        await writeFile(path, '{"version":1,"translation":"old"}\n');
        await utimes(path, baseTime + index, baseTime + index);
    }));

    const cache = new TranslationCache(directory);
    await cache.set("first trigger", "首次写入", TEST_METRICS);
    let files = (await readdir(directory)).filter((name) => name.endsWith(".json"));
    assert.equal(files.length, CACHE_MAX_FILES + 2 - CACHE_PRUNE_COUNT);
    for (const name of oldestNames.slice(0, CACHE_PRUNE_COUNT)) {
        assert.equal(files.includes(name), false);
    }

    await Promise.all(Array.from({ length: CACHE_PRUNE_COUNT - 1 }, async (_, index) => {
        await writeFile(join(directory, `recent-${index}.json`), '{"version":1,"translation":"recent"}\n');
    }));
    await cache.set("second trigger", "十分钟内再次写入", TEST_METRICS);
    files = (await readdir(directory)).filter((name) => name.endsWith(".json"));
    assert.equal(files.length, CACHE_MAX_FILES + 2);
});

test("history displays persistent cache hits and never requests translations for misses", async (t) => {
    const directory = await mkdtemp(join(tmpdir(), "thinking-translation-test-"));
    t.after(() => rm(directory, { recursive: true, force: true }));
    const cache = new TranslationCache(directory);
    await cache.set("cached thought", "缓存译文", TEST_METRICS);
    await cache.set("first block\n\nsecond block", "被旧版本压平的译文", TEST_METRICS);

    let modelRequests = 0;
    const branch = [
        null,
        { type: "message", message: { role: "assistant", content: "malformed" } },
        {
            type: "message",
            message: {
                role: "assistant",
                content: [
                    { type: "thinking", thinking: "cached thought" },
                    { type: "thinking", thinking: "cache miss" },
                    { type: "thinking", thinking: "first block\n\nsecond block" },
                    { type: "thinking", thinking: "cached thought" },
                ],
            },
        },
    ];
    const ctx = {
        sessionManager: { getBranch: () => branch },
        modelRegistry: {
            hasConfiguredAuth: () => true,
            complete: async () => {
                modelRequests++;
                throw new Error("history must not call the model");
            },
        },
    };
    const session = new TranslationSession(ctx as never, {} as never, 200, true, cache);
    let invalidations = 0;
    session.track({ invalidate: () => { invalidations++; } });

    await session.hydrateHistory();
    const hit = session.get("historical:0", "cached thought");
    assert.deepEqual(hit, { state: "ready", translation: "缓存译文" });
    assert.equal(formatThinkingDisplay("cached thought", hit!), "缓存译文");
    assert.equal(session.get("historical:1", "cache miss"), undefined);
    assert.equal(session.get("historical:2", "first block\n\nsecond block"), undefined);
    assert.equal(modelRequests, 0);
    assert.equal(invalidations, 1);

    session.setEnabled(false);
    assert.equal(session.get("historical:0", "cached thought"), undefined);
    session.shutdown();
});
