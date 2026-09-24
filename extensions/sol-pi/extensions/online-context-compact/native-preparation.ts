/**
 * Preparation-only port of Pi 0.87.1 core/compaction/{compaction,utils}.ts.
 * Copyright (c) 2025 Mario Zechner — MIT; see LICENSE.pi.
 * Pinned pure logic; only public package imports. Summary generation stays native.
 * Local deviation: carry file tracking from our marked fromHook compactions too.
 */
import type { AgentMessage } from "@earendil-works/pi-agent-core";
import { getCurrentSystemMessage } from "@earendil-works/pi-ai";
import type { AssistantMessage, Usage } from "@earendil-works/pi-ai";
import { buildSessionProjection, sessionEntryToContextMessages, estimateTokens, calculateContextTokens,
  type compact, type CompactionEntry, type ProjectedSessionEntry, type SessionEntry,
  type SessionProjection, type FileOperations, type CutPointResult,
} from "@earendil-works/pi-coding-agent";
export type CompactionPreparation = Parameters<typeof compact>[0];
export type EffectiveCompactionSettings = CompactionPreparation["settings"];
type CompactionSettings = EffectiveCompactionSettings;
export const NATIVE_PREPARATION_VERSION = "0.87.1";
export const OCC_FILE_TRACKING = "sol-pi-occ-native-0.87.1";
export interface ContextUsageEstimate {
	tokens: number;
	usageTokens: number;
	trailingTokens: number;
	lastUsageIndex: number | null;
}
export interface CompactionDetails {
	readFiles: string[];
	modifiedFiles: string[];
}

/**
 * Extract file operations from messages and previous compaction entries.
 */
function extractFileOperations(
	messages: AgentMessage[],
	entries: SessionEntry[],
	prevCompactionIndex: number,
): FileOperations {
	const fileOps = createFileOps();

	// Collect from previous compaction's details (if pi-generated)
	if (prevCompactionIndex >= 0) {
		const prevCompaction = entries[prevCompactionIndex] as CompactionEntry;
		if ((!prevCompaction.fromHook || (prevCompaction.details as { preparation?: string } | undefined)?.preparation === OCC_FILE_TRACKING) && prevCompaction.details) {
			// fromHook field kept for session file compatibility
			const details = prevCompaction.details as CompactionDetails;
			if (Array.isArray(details.readFiles)) {
				for (const f of details.readFiles) fileOps.read.add(f);
			}
			if (Array.isArray(details.modifiedFiles)) {
				for (const f of details.modifiedFiles) fileOps.edited.add(f);
			}
		}
	}

	// Extract from tool calls in messages
	for (const msg of messages) {
		extractFileOpsFromMessage(msg, fileOps);
	}

	return fileOps;
}

// ============================================================================
// Message Extraction
// ============================================================================

/**
 * Extract AgentMessage from an entry if it produces one.
 * Returns undefined for entries that don't contribute to LLM context.
 */
function getMessagesFromProjectedEntryForCompaction(entry: ProjectedSessionEntry): AgentMessage[] {
	if (entry.sourceEntry.type === "compaction") return [];
	// System messages are prompt state, not conversation; the compaction entry carries their replay.
	return entry.messages.filter((message) => message.role !== "system");
}

export function createFileOps(): FileOperations {
	return {
		read: new Set(),
		written: new Set(),
		edited: new Set(),
	};
}

/**
 * Extract file operations from tool calls in an assistant message.
 */
export function extractFileOpsFromMessage(message: AgentMessage, fileOps: FileOperations): void {
	if (message.role !== "assistant") return;
	if (!("content" in message) || !Array.isArray(message.content)) return;

	for (const block of message.content) {
		if (typeof block !== "object" || block === null) continue;
		if (!("type" in block) || block.type !== "toolCall") continue;
		if (!("arguments" in block) || !("name" in block)) continue;

		const args = block.arguments as Record<string, unknown> | undefined;
		if (!args) continue;

		const path = typeof args.path === "string" ? args.path : undefined;
		if (!path) continue;

		switch (block.name) {
			case "read":
				fileOps.read.add(path);
				break;
			case "write":
				fileOps.written.add(path);
				break;
			case "edit":
				fileOps.edited.add(path);
				break;
		}
	}
}

function getAssistantUsage(msg: AgentMessage): Usage | undefined {
	if (msg.role === "assistant" && "usage" in msg) {
		const assistantMsg = msg as AssistantMessage;
		if (
			assistantMsg.stopReason !== "aborted" &&
			assistantMsg.stopReason !== "error" &&
			assistantMsg.usage &&
			calculateContextTokens(assistantMsg.usage) > 0
		) {
			return assistantMsg.usage;
		}
	}
	return undefined;
}

function getLastAssistantUsageInfo(messages: AgentMessage[]): { usage: Usage; index: number } | undefined {
	for (let i = messages.length - 1; i >= 0; i--) {
		const usage = getAssistantUsage(messages[i]);
		if (usage) return { usage, index: i };
	}
	return undefined;
}

/**
 * Estimate context tokens from messages, using the last assistant usage when available.
 * If there are messages after the last usage, estimate their tokens with estimateTokens.
 */
export function estimateContextTokens(messages: AgentMessage[]): ContextUsageEstimate {
	const usageInfo = getLastAssistantUsageInfo(messages);

	if (!usageInfo) {
		let estimated = 0;
		for (const message of messages) {
			estimated += estimateTokens(message);
		}
		return {
			tokens: estimated,
			usageTokens: 0,
			trailingTokens: estimated,
			lastUsageIndex: null,
		};
	}

	const usageTokens = calculateContextTokens(usageInfo.usage);
	let trailingTokens = 0;
	for (let i = usageInfo.index + 1; i < messages.length; i++) {
		trailingTokens += estimateTokens(messages[i]);
	}

	return {
		tokens: usageTokens + trailingTokens,
		usageTokens,
		trailingTokens,
		lastUsageIndex: usageInfo.index,
	};
}

/** Estimate projected context without trusting usage captured before a later edit or compaction. */
export function estimateProjectedContextTokens(
	projection: SessionProjection,
	branchEntries: SessionEntry[],
): ContextUsageEstimate {
	const estimate = estimateContextTokens(projection.messages);
	if (estimate.lastUsageIndex !== null) {
		let projectedMessageIndex = 0;
		let usageEntryId: string | undefined;
		for (const entry of projection.entries) {
			const nextMessageIndex = projectedMessageIndex + entry.messages.length;
			if (estimate.lastUsageIndex < nextMessageIndex) {
				usageEntryId = entry.sourceEntry.id;
				break;
			}
			projectedMessageIndex = nextMessageIndex;
		}

		const usageEntryIndex = usageEntryId ? branchEntries.findIndex((entry) => entry.id === usageEntryId) : -1;
		let latestInvalidatingEntryIndex = -1;
		for (let i = branchEntries.length - 1; i >= 0; i--) {
			const entry = branchEntries[i];
			if (entry.type === "context_edit" || entry.type === "compaction") {
				latestInvalidatingEntryIndex = i;
				break;
			}
		}
		if (usageEntryIndex > latestInvalidatingEntryIndex) return estimate;
	}

	const currentSystem = getCurrentSystemMessage(projection.messages);
	let tokens = currentSystem ? estimateTokens(currentSystem) : 0;
	for (const message of projection.messages) {
		if (message.role !== "system") tokens += estimateTokens(message);
	}
	return { tokens, usageTokens: 0, trailingTokens: tokens, lastUsageIndex: null };
}

function isCutPointMessage(message: AgentMessage): boolean {
	switch (message.role) {
		case "user":
		case "assistant":
		case "bashExecution":
		case "custom":
		case "branchSummary":
		case "compactionSummary":
			return true;
		case "toolResult":
			return false;
	}
	return false;
}

function isTurnStartMessage(message: AgentMessage): boolean {
	switch (message.role) {
		case "user":
		case "bashExecution":
		case "custom":
		case "branchSummary":
		case "compactionSummary":
			return true;
		case "assistant":
		case "toolResult":
			return false;
	}
	return false;
}

function isProjectedTurnStart(entry: ProjectedSessionEntry): boolean {
	if (entry.sourceEntry.type === "compaction") return false;
	return entry.messages.some(isTurnStartMessage);
}

function findProjectedTurnStartIndex(entries: ProjectedSessionEntry[], entryIndex: number, startIndex: number): number {
	for (let i = entryIndex; i >= startIndex; i--) {
		if (isProjectedTurnStart(entries[i])) return i;
	}
	return -1;
}

function findProjectedCutPoint(
	entries: ProjectedSessionEntry[],
	startIndex: number,
	endIndex: number,
	keepRecentTokens: number,
): CutPointResult {
	const cutPoints: number[] = [];
	for (let i = startIndex; i < endIndex; i++) {
		const entry = entries[i];
		if (entry.sourceEntry.type !== "compaction" && entry.messages.some(isCutPointMessage)) cutPoints.push(i);
	}
	if (cutPoints.length === 0) {
		return { firstKeptEntryIndex: startIndex, turnStartIndex: -1, isSplitTurn: false };
	}

	let accumulatedTokens = 0;
	let exceededBudget = false;
	let cutIndex = cutPoints[0];
	for (let i = endIndex - 1; i >= startIndex; i--) {
		const messageTokens = entries[i].messages.reduce((sum, message) => sum + estimateTokens(message), 0);
		if (messageTokens === 0) continue;
		accumulatedTokens += messageTokens;
		if (accumulatedTokens >= keepRecentTokens) {
			exceededBudget = true;
			cutIndex = cutPoints.find((candidate) => candidate >= i) ?? cutPoints[cutPoints.length - 1];
			break;
		}
	}

	// A recovery attempt and its omission edits are context-invisible after the last
	// visible input. Advance only for a closed suffix containing an omitted assistant
	// attempt; arbitrary metadata must not move the cut past unsent input.
	const suffix = entries.slice(cutIndex + 1, endIndex);
	const isIntrinsicallyVisible = (entry: ProjectedSessionEntry): boolean =>
		entry.sourceEntry.type !== "context_edit" && sessionEntryToContextMessages(entry.sourceEntry).length > 0;
	const isOmitted = (entry: ProjectedSessionEntry): boolean =>
		isIntrinsicallyVisible(entry) && entry.messages.length === 0;
	const omittedSuffixIds = new Set(suffix.filter(isOmitted).map((entry) => entry.sourceEntry.id));
	const hasExternalReplacement = suffix.some(
		(entry) =>
			entry.sourceEntry.type === "context_edit" &&
			entry.sourceEntry.replacement !== null &&
			!omittedSuffixIds.has(entry.sourceEntry.targetId),
	);
	const isRecoveryOmissionSuffix =
		exceededBudget &&
		!hasExternalReplacement &&
		suffix.some(
			(entry) =>
				entry.sourceEntry.type === "message" && entry.sourceEntry.message.role === "assistant" && isOmitted(entry),
		) &&
		suffix.every(
			(entry) => entry.sourceEntry.type !== "compaction" && (!isIntrinsicallyVisible(entry) || isOmitted(entry)),
		);
	if (isRecoveryOmissionSuffix) cutIndex++;

	while (cutIndex > startIndex) {
		const previous = entries[cutIndex - 1];
		if (previous.sourceEntry.type === "compaction" || previous.messages.length > 0) break;
		cutIndex--;
	}
	const startsTurn = isProjectedTurnStart(entries[cutIndex]);
	const turnStartIndex = startsTurn ? -1 : findProjectedTurnStartIndex(entries, cutIndex, startIndex);
	return {
		firstKeptEntryIndex: cutIndex,
		turnStartIndex,
		isSplitTurn: !startsTurn && turnStartIndex !== -1,
	};
}

export function prepareCompaction(
	pathEntries: SessionEntry[],
	settings: CompactionSettings,
): CompactionPreparation | undefined {
	if (pathEntries.length > 0 && pathEntries[pathEntries.length - 1].type === "compaction") {
		return undefined;
	}

	const projection = buildSessionProjection(pathEntries);
	const projectedEntries = projection.entries;
	const sourceEntries = projectedEntries.map((entry) => entry.sourceEntry);
	// The newest compaction is projected first. Older compaction entries can still
	// occur in its retained raw range, but their projected contribution is empty.
	const prevCompactionIndex = projectedEntries.findIndex(
		(entry) => entry.sourceEntry.type === "compaction" && entry.messages.length > 0,
	);

	let previousSummary: string | undefined;
	let boundaryStart = 0;
	if (prevCompactionIndex >= 0) {
		previousSummary = (projectedEntries[prevCompactionIndex].sourceEntry as CompactionEntry).summary;
		// The canonical projection has already selected the previous compaction's retained tail.
		boundaryStart = prevCompactionIndex + 1;
	}
	const boundaryEnd = projectedEntries.length;
	const tokensBefore = estimateProjectedContextTokens(projection, pathEntries).tokens;
	const cutPoint = findProjectedCutPoint(projectedEntries, boundaryStart, boundaryEnd, settings.keepRecentTokens);

	const firstKeptEntry = projectedEntries[cutPoint.firstKeptEntryIndex]?.sourceEntry;
	if (!firstKeptEntry?.id) return undefined;
	const firstKeptEntryId = firstKeptEntry.id;
	const historyEnd = cutPoint.isSplitTurn ? cutPoint.turnStartIndex : cutPoint.firstKeptEntryIndex;

	const messagesToSummarize = projectedEntries
		.slice(boundaryStart, historyEnd)
		.flatMap(getMessagesFromProjectedEntryForCompaction);
	const turnPrefixMessages = cutPoint.isSplitTurn
		? projectedEntries
				.slice(cutPoint.turnStartIndex, cutPoint.firstKeptEntryIndex)
				.flatMap(getMessagesFromProjectedEntryForCompaction)
		: [];

	if (messagesToSummarize.length === 0 && turnPrefixMessages.length === 0) return undefined;

	// Extract file operations from edited model-visible messages and the previous compaction.
	const fileOps = extractFileOperations(messagesToSummarize, sourceEntries, prevCompactionIndex);

	// Also extract file ops from turn prefix if splitting
	if (cutPoint.isSplitTurn) {
		for (const msg of turnPrefixMessages) {
			extractFileOpsFromMessage(msg, fileOps);
		}
	}

	return {
		firstKeptEntryId,
		messagesToSummarize,
		turnPrefixMessages,
		isSplitTurn: cutPoint.isSplitTurn,
		tokensBefore,
		previousSummary,
		fileOps,
		settings,
	};
}
