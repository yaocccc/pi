import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { createProvider } from "@earendil-works/pi-ai";
import { createFauxCore, fauxAssistantMessage } from "@earendil-works/pi-ai/providers/faux";
import { createAgentSession, DefaultResourceLoader, SessionManager, SettingsManager } from "@earendil-works/pi-coding-agent";
import { EXTENSION_RPC_PREFIX, registerExtensionRpc } from "./rpc.ts";

test("real SDK idle resolver keeps a settled run occupied during deferred prompt preflight", { timeout: 5_000 }, async (t) => {
    const cwd = await mkdtemp(join(tmpdir(), "pi-release-barrier-"));
    t.after(() => rm(cwd, { recursive: true, force: true }));
    const faux = createFauxCore({ provider: "release-offline", api: "release-offline-api", models: [{ id: "local", contextWindow: 100_000, maxTokens: 4096 }] });
    faux.setResponses([fauxAssistantMessage("first completed"), fauxAssistantMessage("continuation completed")]);
    const provider = createProvider({
        id: faux.provider, models: faux.models,
        auth: { apiKey: { name: "Offline fixture", resolve: async () => ({ auth: { apiKey: "offline-fixture" } }) } },
        api: { stream: faux.stream, streamSimple: faux.streamSimple, fetchDeferred: faux.fetchDeferred, cancelDeferred: faux.cancelDeferred },
    });
    let startDeferred!: () => void, releaseDeferred!: () => void;
    const deferredStarted = new Promise<void>((resolve) => { startDeferred = resolve; });
    const deferredGate = new Promise<void>((resolve) => { releaseDeferred = resolve; });
    t.after(releaseDeferred);
    let continued = false;
    const settings = SettingsManager.inMemory({ compaction: { enabled: false }, retry: { enabled: false }, cacheWarming: "off" });
    const loader = new DefaultResourceLoader({
        cwd, agentDir: cwd, settingsManager: settings,
        noExtensions: true, noSkills: true, noPromptTemplates: true, noThemes: true, noContextFiles: true,
        extensionFactories: [{ name: "release-fixture", factory: (pi) => {
            pi.registerProvider(provider);
            registerExtensionRpc(pi);
            pi.on("agent_settled", () => {
                if (!continued) { continued = true; pi.sendUserMessage("deferred continuation"); }
            });
            pi.on("input", async (event) => {
                if (event.text === "deferred continuation") { startDeferred(); await deferredGate; }
            });
        } }],
    });
    await loader.reload();
    assert.deepEqual(loader.getExtensions().errors, []);
    const { session } = await createAgentSession({ cwd, agentDir: cwd, model: faux.getModel(), thinkingLevel: "off", tools: [], resourceLoader: loader, sessionManager: SessionManager.inMemory(cwd), settingsManager: settings });
    t.after(() => session.dispose());
    const replies: any[] = [];
    await session.bindExtensions({
        mode: "rpc",
        uiContext: { notify: (message: string) => {
            if (message.startsWith(EXTENSION_RPC_PREFIX)) replies.push(JSON.parse(message.slice(EXTENSION_RPC_PREFIX.length)));
        } } as any,
        commandContextActions: { waitForIdle: () => session.waitForIdle() } as any,
        onError: (error) => assert.fail(JSON.stringify(error)),
    });
    const probe = async () => {
        await session.prompt('/pi-ui-rpc {"id":"release-probe","method":"session.activity"}');
        const reply = replies.pop();
        assert.ok(reply, "activity reply was not emitted");
        assert.equal(reply.error, undefined);
        return reply.result;
    };
    assert.equal((await probe()).idle, true); // Prime before the first run.
    const run = session.prompt("initial task", { expandPromptTemplates: false });
    await deferredStarted;
    assert.equal(session.isIdle, true, "fixture must reach the native idle gap before deferred admission");
    const state = await probe();
    assert.equal(state.idle, false);
    assert.match(state.reasons.join(" "), /deferred/);
    releaseDeferred();
    await run;
    assert.equal((await probe()).idle, true);
});
