import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { loadAutonameConfig } from "./config.ts";
import {
    AUTONAME_ENTRY_TYPE,
    extractNamingMessages,
    hasConversationPair,
    isAutonameCoolingDown,
    isExtensionOwnedName,
    parseNamingDecision,
    responseText,
    validateName,
} from "./helpers.ts";

type NamingModel = Parameters<ExtensionContext["modelRegistry"]["complete"]>[0];

type ProposedSessionName = {
    name: string;
    contextTokens: number;
    durationMs: number;
};

const buildNamingSystemPrompt = (currentName: string | undefined): string => [
    "请在内部总结所提供的对话，并判断当前会话名称是否仍然准确、具体地概括了对话。",
    `当前会话名称：${currentName === undefined ? "null（尚未命名）" : JSON.stringify(currentName)}。`,
    "只能依据所提供的对话和当前会话名称。如果尚未命名且内容仍然过于模糊，或者现有名称已经足够准确，请保持不变。",
    "只有在尚未命名且已有足够信息，或现有名称明显不准确、过时、不够具体时，才返回最能概括当前对话的简洁中文标题。",
    "仅返回以下两种 JSON 对象之一，不要使用 Markdown，也不要附加解释：{\"action\":\"keep\"} 或 {\"action\":\"rename\",\"name\":\"简洁的中文标题\"}。",
    "名称必须具体、单行，并且不超过 80 个字符。",
].join("");

const configuredModel = (ctx: ExtensionContext, modelId: string): NamingModel | undefined => {
    if (modelId === "auto") return ctx.model;
    const slash = modelId.indexOf("/");
    if (slash <= 0 || slash === modelId.length - 1) return undefined;
    return ctx.modelRegistry.find(modelId.slice(0, slash), modelId.slice(slash + 1));
};

const logFailure = (message: string, error?: unknown): void => {
    const detail = error instanceof Error ? `: ${error.message}` : "";
    console.warn(`[autoname] ${message}${detail}`);
};

const formatCompactTokens = (tokens: number): string => tokens >= 1_000_000
    ? `${(tokens / 1_000_000).toFixed(1)}m`
    : tokens >= 1_000
        ? `${(tokens / 1_000).toFixed(1)}k`
        : Math.round(tokens).toString();

const extractNamingContext = (branch: readonly unknown[], currentName: string | undefined, cooldownSeconds: number) => {
    const messages = extractNamingMessages(branch);
    if (!hasConversationPair(messages) || isAutonameCoolingDown(branch, cooldownSeconds)) return undefined;
    if (currentName !== undefined && !isExtensionOwnedName(currentName, branch)) return undefined;
    return { messages };
};

const requestSessionName = async (
    ctx: ExtensionContext,
    model: NamingModel,
    messages: ReturnType<typeof extractNamingMessages>,
    currentName: string | undefined,
    reasoning: string,
    signal?: AbortSignal,
): Promise<ProposedSessionName | undefined> => {
    const requestStartedAt = performance.now();
    const response = await ctx.modelRegistry.complete(model, {
        systemPrompt: buildNamingSystemPrompt(currentName),
        messages,
    }, {
        reasoningEffort: reasoning,
        cacheRetention: "none",
        signal,
    });
    if (response.stopReason === "aborted" || response.stopReason === "error") return;

    const decision = parseNamingDecision(responseText(response.content));
    if (!decision || decision.action === "keep") return;
    const nextName = validateName(decision.name);
    if (!nextName || nextName === currentName) return;

    const contextTokens = response.usage.input + response.usage.cacheRead + response.usage.cacheWrite;
    return {
        name: nextName,
        contextTokens,
        durationMs: Math.max(0, performance.now() - requestStartedAt),
    };
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
            const currentName = pi.getSessionName();
            const prepared = extractNamingContext(branch, currentName, config.cooldownSeconds);
            if (!prepared) return;

            const model = configuredModel(ctx, config.model);
            if (!model || !ctx.modelRegistry.hasConfiguredAuth(model)) return;

            pi.appendEntry(AUTONAME_ENTRY_TYPE, { version: 1, kind: "attempt", startedAt: Date.now() });
            const leafId = ctx.sessionManager.getLeafId();
            const proposed = await requestSessionName(
                ctx,
                model,
                prepared.messages,
                currentName,
                config.reasoning,
                controller.signal,
            );
            if (!isCurrent(run, sessionId, controller) || !proposed) return;

            // Never overwrite a user edit or a later branch/turn while the request was in flight.
            if (ctx.sessionManager.getLeafId() !== leafId || pi.getSessionName() !== currentName) return;

            pi.setSessionName(proposed.name);
            pi.appendEntry(AUTONAME_ENTRY_TYPE, { version: 1, kind: "set-name", name: proposed.name });
            if (config.notify) {
                ctx.ui.notify(
                    `会话已自动命名：${proposed.name} [${formatCompactTokens(proposed.contextTokens)} ${(proposed.durationMs / 1_000).toFixed(1)}s]`,
                    "info",
                );
            }
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
