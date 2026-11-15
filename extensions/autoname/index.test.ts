import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import autoname from "./index.ts";
import { createEventBus } from "@earendil-works/pi-coding-agent";
import { sessionActivities } from "./capabilities.ts";

test("autoname names a settled primary session even without a UI", { timeout: 5_000 }, async (t) => {
    const dir = await mkdtemp(join(tmpdir(), "autoname-registration-"));
    const previous = process.env.PI_CODING_AGENT_DIR;
    process.env.PI_CODING_AGENT_DIR = dir;
    t.after(async () => {
        if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
        else process.env.PI_CODING_AGENT_DIR = previous;
        await rm(dir, { recursive: true, force: true });
    });
    await writeFile(join(dir, "autoname.json"), JSON.stringify({ notify: false }));
    const handlers = new Map<string, (...args: any[]) => void>();
    const commands: string[] = [];
    const entries: any[] = [];
    let currentName: string | undefined;
    let resolveNamed!: () => void;
    const named = new Promise<void>((resolve) => { resolveNamed = resolve; });
    autoname({
        events: createEventBus(),
        on: (name: string, handler: (...args: any[]) => void) => handlers.set(name, handler),
        registerCommand: (name: string) => commands.push(name),
        getSessionName: () => currentName,
        setSessionName: (name: string) => { currentName = name; resolveNamed(); },
        appendEntry: (type: string, data: any) => entries.push({ type, data }),
    } as any);
    assert.deepEqual(commands, []);
    assert.deepEqual([...handlers.keys()], ["session_start", "agent_start", "agent_settled", "session_shutdown"]);
    const ctx = {
        hasUI: false,
        model: { provider: "fixture", id: "local" },
        sessionManager: {
            getSessionId: () => "current",
            getLeafId: () => "leaf",
            getBranch: () => [
                { type: "message", message: { role: "user", content: "实现自动命名" } },
                { type: "message", message: { role: "assistant", content: "已实现" } },
            ],
        },
        modelRegistry: {
            hasConfiguredAuth: () => true,
            complete: async (_model: any, request: any, options: any) => {
                assert.ok(options.signal instanceof AbortSignal);
                assert.match(request.systemPrompt, /保持不变/);
                assert.doesNotMatch(request.systemPrompt, /必须返回 rename/);
                return { content: [{ type: "text", text: '{"action":"rename","name":"自动命名测试"}' }], usage: { input: 1, cacheRead: 0, cacheWrite: 0 }, stopReason: "stop" };
            },
        },
    };
    handlers.get("session_start")!({}, ctx);
    handlers.get("agent_settled")!({}, ctx);
    await named;
    assert.equal(currentName, "自动命名测试");
    assert.deepEqual(entries.map((entry) => entry.data.kind), ["attempt", "set-name"]);
    handlers.get("session_shutdown")!({}, ctx);
});

test("autoname remains active through aborted request cleanup after agent_settled", { timeout: 5_000 }, async (t) => {
    const dir = await mkdtemp(join(tmpdir(), "autoname-activity-"));
    const previous = process.env.PI_CODING_AGENT_DIR;
    process.env.PI_CODING_AGENT_DIR = dir;
    t.after(async () => {
        if (previous === undefined) delete process.env.PI_CODING_AGENT_DIR;
        else process.env.PI_CODING_AGENT_DIR = previous;
        await rm(dir, { recursive: true, force: true });
    });
    await writeFile(join(dir, "autoname.json"), JSON.stringify({ notify: false }));
    const handlers = new Map<string, (...args: any[]) => void>();
    const pi = {
        events: createEventBus(),
        on: (name: string, handler: (...args: any[]) => void) => handlers.set(name, handler),
        getSessionName: () => undefined,
        setSessionName: () => assert.fail("cancelled naming must not persist"),
        appendEntry: () => {},
    } as any;
    autoname(pi);
    let started!: () => void, finish!: () => void;
    const ready = new Promise<void>((resolve) => { started = resolve; });
    const gate = new Promise<void>((resolve) => { finish = resolve; });
    t.after(finish);
    let signal: AbortSignal | undefined;
    const ctx = {
        model: { provider: "fixture", id: "local" },
        sessionManager: {
            getSessionId: () => "current", getLeafId: () => "leaf",
            getBranch: () => [{ type: "message", message: { role: "user", content: "request" } }, { type: "message", message: { role: "assistant", content: "answer" } }],
        },
        modelRegistry: {
            hasConfiguredAuth: () => true,
            complete: async (_model: any, _request: any, options: any) => {
                signal = options.signal;
                started(); await gate;
                return { content: [], usage: { input: 0, cacheRead: 0, cacheWrite: 0 }, stopReason: "aborted" };
            },
        },
    };
    handlers.get("session_start")!({}, ctx);
    handlers.get("agent_settled")!({}, ctx);
    assert.match(sessionActivities(pi)[0].reason!, /still running/, "configuration loading is also active");
    await ready;
    handlers.get("agent_start")!({}, ctx);
    assert.equal(signal?.aborted, true);
    assert.match(sessionActivities(pi)[0].reason!, /still running/);
    finish();
    await new Promise<void>((resolve) => setImmediate(resolve));
    assert.deepEqual(sessionActivities(pi), [{ name: "Autoname" }]);
    handlers.get("session_shutdown")!({}, ctx);
});
