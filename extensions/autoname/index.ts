import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { loadAutonameConfig } from "./config.ts";
import {
    AUTONAME_ENTRY_TYPE,
    extractNamingMessages,
    hasConversationPair,
    isExtensionOwnedName,
    parseNamingDecision,
    responseText,
    validateName,
} from "./helpers.ts";

const NAMING_SYSTEM_PROMPT = [
    "请在内部总结所提供的对话，并判断它是否已经包含足够的信息，可以为当前 Pi 会话生成有意义的名称。",
    "只能依据所提供的对话。如果内容仍然过于模糊，请保持会话未命名；否则请返回最能概括当前对话的简洁中文标题。",
    "仅返回以下两种 JSON 对象之一，不要使用 Markdown，也不要附加解释：{\"action\":\"keep\"} 或 {\"action\":\"rename\",\"name\":\"简洁的中文标题\"}。",
    "名称必须具体、单行，并且不超过 80 个字符。",
].join("");

const configuredModel = (ctx: ExtensionContext, modelId: string) => {
    if (modelId === "auto") return ctx.model;
    const slash = modelId.indexOf("/");
    if (slash <= 0 || slash === modelId.length - 1) return undefined;
    return ctx.modelRegistry.find(modelId.slice(0, slash), modelId.slice(slash + 1));
};

const logFailure = (message: string, error?: unknown): void => {
    const detail = error instanceof Error ? `: ${error.message}` : "";
    console.warn(`[autoname] ${message}${detail}`);
};

export default function autoname(pi: ExtensionAPI): void {
    let generation = 0;
    let activeSessionId: string | undefined;
    let running: AbortController | undefined;

    const invalidate = (): void => {
        generation++;
        running?.abort();
        running = undefined;
    };

    const isCurrent = (run: number, sessionId: string, controller: AbortController): boolean =>
        run === generation
        && activeSessionId === sessionId
        && running === controller
        && !controller.signal.aborted;

    const nameCurrentSession = async (ctx: ExtensionContext): Promise<void> => {
        invalidate();
        const controller = new AbortController();
        running = controller;
        const run = generation;
        const sessionId = ctx.sessionManager.getSessionId();
        activeSessionId = sessionId;

        try {
            const config = await loadAutonameConfig();
            if (!isCurrent(run, sessionId, controller) || !config.enabled) return;

            const branch = ctx.sessionManager.getBranch();
            const messages = extractNamingMessages(branch);
            if (!hasConversationPair(messages)) return;

            const currentName = pi.getSessionName();
            if (currentName !== undefined && !isExtensionOwnedName(currentName, branch)) return;
            const leafId = ctx.sessionManager.getLeafId();

            const model = configuredModel(ctx, config.model);
            if (!model || !ctx.modelRegistry.hasConfiguredAuth(model)) return;

            const response = await ctx.modelRegistry.complete(model, {
                systemPrompt: NAMING_SYSTEM_PROMPT,
                messages,
            }, {
                reasoningEffort: config.reasoning,
                cacheRetention: "none",
                signal: controller.signal,
            });
            if (!isCurrent(run, sessionId, controller) || response.stopReason === "aborted" || response.stopReason === "error") return;

            const decision = parseNamingDecision(responseText(response.content));
            if (!decision || decision.action === "keep") return;
            const nextName = validateName(decision.name);
            if (!nextName || nextName === currentName) return;

            // Never overwrite a user edit or a later branch/turn while the request was in flight.
            if (!isCurrent(run, sessionId, controller)
                || ctx.sessionManager.getLeafId() !== leafId
                || pi.getSessionName() !== currentName) return;

            pi.setSessionName(nextName);
            pi.appendEntry(AUTONAME_ENTRY_TYPE, { version: 1, kind: "set-name", name: nextName });
            const contextTokens = response.usage.input + response.usage.cacheRead + response.usage.cacheWrite;
            ctx.ui.notify(
                `会话已自动命名：${nextName}\n上下文消耗：${contextTokens.toLocaleString()} tokens`,
                "info",
            );
        } catch (error) {
            if (!controller.signal.aborted) logFailure("naming request failed", error);
        } finally {
            if (running === controller) running = undefined;
        }
    };

    pi.on("session_start", (_event, ctx) => {
        invalidate();
        activeSessionId = ctx.sessionManager.getSessionId();
    });

    pi.on("agent_start", () => {
        // A new turn means a previous nested request can no longer name this branch.
        invalidate();
    });

    pi.on("agent_settled", (_event, ctx) => {
        if (!ctx.hasUI) return;
        void nameCurrentSession(ctx);
    });

    pi.on("session_shutdown", () => {
        invalidate();
        activeSessionId = undefined;
    });
}
