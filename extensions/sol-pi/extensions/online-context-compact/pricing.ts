import type { AgentMessage } from "@earendil-works/pi-agent-core";
import { buildSessionProjection, estimateTokens, type SessionEntry } from "@earendil-works/pi-coding-agent";
import { estimateProjectedContextTokens, type CompactionPreparation } from "./native-preparation.ts";
import { estimateNativeCompactionTokens } from "./projection.ts";

export type PricingModel = { provider: string; id: string; api: string };
const RESPONSES_APIS = new Set(["openai-responses", "openai-codex-responses", "azure-openai-responses"]);
const sameModel = (message: AgentMessage, model: PricingModel): boolean => message.role === "assistant" &&
	message.provider === model.provider && message.model === model.id && message.api === model.api;

/** Only Responses replays opaque reasoning items; their byte length is NOT a token count. */
function reasoningExtra(message: AgentMessage, model: PricingModel): { tokens: number; ids: string[] } | undefined {
	if (message.role !== "assistant" || !sameModel(message, model) ||
		!["stop", "toolUse"].includes(message.stopReason)) return;
	const reasoning = message.usage?.reasoning;
	if (typeof reasoning !== "number" || !Number.isSafeInteger(reasoning) || reasoning <= 0 ||
		!Number.isSafeInteger(message.usage?.output) || reasoning > message.usage.output) return;
	const blocks = message.content.filter(block => block.type === "thinking");
	if (!blocks.length) return;
	const ids: string[] = [];
	let visibleChars = 0;
	for (const block of blocks) {
		if (block.type !== "thinking" || !block.thinkingSignature) return;
		try {
			const item = JSON.parse(block.thinkingSignature);
			if (item?.type !== "reasoning" || typeof item.id !== "string" || !item.id ||
				typeof item.encrypted_content !== "string" || !item.encrypted_content) return;
			ids.push(item.id);
		} catch { return; }
		visibleChars += block.thinking.length;
	}
	if (new Set(ids).size !== ids.length) return;
	// estimateTokens already counted the visible thinking text. Usage is per
	// assistant, NOT per reasoning item; neither may be counted twice.
	return { tokens: Math.max(0, reasoning - Math.ceil(visibleChars / 4)), ids };
}

function assistantKey(message: AgentMessage): string | undefined {
	if (message.role !== "assistant") return;
	return JSON.stringify([message.provider, message.api, message.model, message.timestamp, message.content]);
}

/**
 * Keep native/provider usage as the context-size anchor, and credit the native
 * removable prefix for BOTH projected text and replayed opaque reasoning.
 * Historical reasoning usage is an estimate of replay cost, not exact billing.
 * Credit requires same-model, unique, unchanged source/projection matches and
 * fresh native usage. Unexplained usage stays in retained cost, never in savings.
 */
export function estimateCompactionPricing(input: {
	preparation: CompactionPreparation;
	branch: SessionEntry[];
	observedMessages: readonly AgentMessage[] | undefined;
	model: PricingModel | undefined;
}) {
	const { preparation, branch, observedMessages, model } = input;
	const writeTokens = preparation.tokensBefore;
	const visibleArchiveTokens = estimateNativeCompactionTokens(preparation, observedMessages);
	const projected = buildSessionProjection(branch);
	const usage = estimateProjectedContextTokens(projected, branch);
	const lastUsage = usage.lastUsageIndex === null ? undefined : projected.messages[usage.lastUsageIndex];
	const observedVisibleTokens = observedMessages?.reduce((sum, message) => sum + estimateTokens(message), 0) ?? null;
	// The budget prevents stale/inconsistent historical counts from claiming more
	// hidden savings than current usage allows. Retained reasoning shares it too.
	const reasoningBudgetTokens = observedVisibleTokens === null ? 0
		: Math.max(0, writeTokens - Math.max(visibleArchiveTokens, observedVisibleTokens));
	let reasoningStatus = "available";
	if (!model || !RESPONSES_APIS.has(model.api)) reasoningStatus = "unsupported_model";
	else if (!observedMessages?.length) reasoningStatus = "no_observed_projection";
	else if (!lastUsage || usage.tokens !== writeTokens) reasoningStatus = "no_current_usage";
	else if (!sameModel(lastUsage, model)) reasoningStatus = "usage_model_mismatch";

	let observedReasoningTokens = 0, removableReasoningTokens = 0, matchedReasoningMessages = 0;
	if (reasoningStatus === "available" && observedMessages && model) {
		const sources = new Map<string, AgentMessage[]>();
		for (const message of [...preparation.messagesToSummarize, ...preparation.turnPrefixMessages]) {
			const key = assistantKey(message);
			if (key !== undefined) sources.set(key, [...(sources.get(key) ?? []), message]);
		}
		const counts = new Map<string, number>(), signatureCounts = new Map<string, number>();
		const candidates = observedMessages.map(message => {
			const key = assistantKey(message), extra = reasoningExtra(message, model);
			if (key !== undefined) counts.set(key, (counts.get(key) ?? 0) + 1);
			for (const id of extra?.ids ?? []) signatureCounts.set(id, (signatureCounts.get(id) ?? 0) + 1);
			return { key, extra };
		});
		for (const { key, extra } of candidates) {
			if (!key || !extra || counts.get(key) !== 1 || extra.ids.some(id => signatureCounts.get(id) !== 1)) continue;
			observedReasoningTokens += extra.tokens;
			const source = sources.get(key);
			if (source?.length !== 1) continue;
			const original = reasoningExtra(source[0], model);
			if (!original) continue;
			removableReasoningTokens += Math.min(original.tokens, extra.tokens);
			matchedReasoningMessages++;
		}
		if (!observedReasoningTokens) reasoningStatus = "no_verified_reasoning_usage";
	}
	const reasoningCreditScale = observedReasoningTokens > 0 ? Math.min(1, reasoningBudgetTokens / observedReasoningTokens) : 0;
	const reasoningArchiveTokens = Math.floor(removableReasoningTokens * reasoningCreditScale);
	return {
		writeTokens, archiveTokens: visibleArchiveTokens + reasoningArchiveTokens,
		visibleArchiveTokens, reasoningArchiveTokens, observedVisibleTokens,
		observedReasoningTokens, removableReasoningTokens, matchedReasoningMessages,
		reasoningBudgetTokens, reasoningCreditScale, reasoningStatus,
		unattributedUsageTokens: observedVisibleTokens === null ? null
			: Math.max(0, writeTokens - observedVisibleTokens - observedReasoningTokens * reasoningCreditScale),
		contextTokenSource: usage.lastUsageIndex === null ? "visible_estimate" : "provider_usage_plus_tail",
		postCompactionEstimateKind: "usage_anchored_estimate_not_measurement",
	};
}
