/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 */
import type { AgentMessage } from "@earendil-works/pi-agent-core";
import { estimateTokens } from "@earendil-works/pi-coding-agent";
import type { CompactionPreparation } from "./native-preparation.ts";

/** OP preserves tool identity while replacing only its model-facing content. */
function messageKey(message: AgentMessage): string {
	if (message.role === "toolResult") {
		return `tool:${JSON.stringify([message.toolCallId, message.toolName, message.timestamp])}`;
	}
	// Pi may attach accounting/model metadata while persisting an assistant.
	// Compare its context content, not transport-only fields such as usage.
	if (message.role === "assistant" || message.role === "user") {
		return JSON.stringify([message.role, message.timestamp, message.content]);
	}
	return JSON.stringify(message);
}

/**
 * Price the actual native prefix, including a split turn and the replaced checkpoint.
 * Never infer it by subtracting keepRecentTokens: the native cut preserves tool pairs.
 * When a request projection is available, count only proven visible savings. This
 * observes OP without rerunning its stateful hooks or changing summary source data.
 */
export function estimateNativeCompactionTokens(
	preparation: CompactionPreparation,
	observedMessages?: readonly AgentMessage[],
): number {
	const prefix = [...preparation.messagesToSummarize, ...preparation.turnPrefixMessages];
	const previousSummary = preparation.previousSummary;
	const summaryTokens = previousSummary === undefined ? 0 : estimateTokens({
		role: "compactionSummary", summary: previousSummary, tokensBefore: 0, timestamp: 0,
	});
	if (observedMessages === undefined) {
		return prefix.reduce((total, message) => total + estimateTokens(message), summaryTokens);
	}

	const sources = new Map<string, { count: number; tokens: number }>();
	for (const message of prefix) {
		const key = messageKey(message);
		const source = sources.get(key) ?? { count: 0, tokens: 0 };
		sources.set(key, { count: source.count + 1, tokens: source.tokens + estimateTokens(message) });
	}
	const visible = new Map<string, number[]>();
	for (const message of observedMessages) {
		const key = messageKey(message);
		const sizes = visible.get(key) ?? [];
		sizes.push(estimateTokens(message));
		visible.set(key, sizes);
	}
	let tokens = 0;
	for (const [key, source] of sources) {
		const sizes = visible.get(key);
		// New, omitted, rewritten or ambiguously duplicated messages prove no saving.
		if (!sizes || sizes.length !== source.count || (key.startsWith("tool:") && source.count > 1)) continue;
		// A durable edit after the observed request may have shortened this result
		// again. Never claim more than either the current prefix or its projection.
		tokens += Math.min(source.tokens, sizes.reduce((total, size) => total + size, 0));
	}
	if (previousSummary !== undefined && observedMessages.filter(
		(message) => message.role === "compactionSummary" && message.summary === previousSummary,
	).length === 1) tokens += summaryTokens;
	return tokens;
}
