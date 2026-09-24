/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: MIT
 * Pi 0.87.1 boundary-draft lifecycle port. No abort/settled-message continuation.
 */
import {
	buildSessionProjection, compact,
	type ExtensionContext, type ExtensionFactory,
} from "@earendil-works/pi-coding-agent";
import { DEFAULT_COMPACTION_ECONOMICS, decideCompaction } from "./economics.ts";
import { analyzePlanTransition, formatPlanSnapshot, parsePlanSteps } from "./plan.ts";
import {
	ONLINE_STATE_ENTRY, appendOnlineState, recordBoundary, recordCompaction,
	recordCorrection, recordProviderRequest, restoreOnlineState,
	type OnlineState, type ProgressSummary,
} from "./state.ts";
import {
	estimateProjectedContextTokens, OCC_FILE_TRACKING, prepareCompaction,
} from "./native-preparation.ts";
import { resolveOnlineSettings, type SettingsResolver } from "./settings.ts";
import { registerOnlineTools, type PlanUpdateInput } from "./tools.ts";

export const DEFAULT_NATIVE_SUMMARY_TOKEN_ESTIMATE = 1_000;
/** Heuristic, NOT a measured/model-specific cache price. */
export const DEFAULT_CACHE_WRITE_READ_RATIO = 12.5;
export const BOUNDARY_COMPACTION_INSTRUCTIONS =
	"Preserve completed work, verification results, important decisions, and remaining work.";
export const POST_COMPACTION_PLAN_REMINDER =
	"Online context compaction finished. The parent task is still active. " +
	"Before continuing work, call update_plan with a fresh plan for the remaining work.";

export type OnlineContextCompactOptions = {
	readonly cacheWriteReadRatio?: number | null;
	/** SDK hosts must inject their effective SettingsManager here (including runtime overrides). */
	readonly resolveSettings?: SettingsResolver;
};

function progressSummary(input: PlanUpdateInput, completedStepId: string): ProgressSummary | undefined {
	const step = input.steps.find((item) => item.id === completedStepId);
	if (!step || !input.progress) return;
	return {
		stepId: step.id, goal: step.goal,
		filesChanged: [...input.progress.files_changed], verification: [...input.progress.verification],
		decisions: [...input.progress.decisions],
		nextWork: input.steps.filter((item) => item.status !== "completed").map((item) => item.goal),
	};
}

export function createOnlineContextCompactExtension(options: OnlineContextCompactOptions = {}): ExtensionFactory {
	const ratio = options.cacheWriteReadRatio === undefined ? DEFAULT_CACHE_WRITE_READ_RATIO : options.cacheWriteReadRatio;
	if (ratio !== null && (!Number.isFinite(ratio) || ratio < 0)) throw new Error("Invalid cacheWriteReadRatio");
	const resolveSettings = options.resolveSettings ?? resolveOnlineSettings;
	return (pi) => {
		if (Number(process.env.PI_WORKER_DEPTH ?? 0) > 0) return;
		let generation = 0;
		let controller: AbortController | undefined;
		let pending: { id: string; generation: number; session: string }[] = [];
		let disabledByUs = false;
		const identity = (ctx: ExtensionContext): string =>
			`${ctx.sessionManager.getSessionFile()}\n${ctx.sessionManager.getSessionId()}`;
		const cancel = (): void => {
			generation++;
			pending = [];
			controller?.abort();
			controller = undefined;
		};
		const eligible = (ctx: ExtensionContext): boolean => {
			if (!ctx.sessionManager.getSessionFile()) return false;
			try { return resolveSettings(ctx).compaction.enabled === true; }
			catch { return false; } // Invalid/unreadable settings must never authorize spend.
		};
		const syncTool = (ctx: ExtensionContext): void => {
			const active = pi.getActiveTools();
			if (!eligible(ctx)) {
				if (active.includes("update_plan")) {
					pi.setActiveTools(active.filter((name) => name !== "update_plan"));
					disabledByUs = true;
				}
			} else if (disabledByUs) {
				pi.setActiveTools([...active, "update_plan"]);
				disabledByUs = false;
			}
		};
		// Never trust a proposed draft: every operation starts from the committed branch.
		// Also reconcile native/manual compactions, which do not have our same-batch state.
		const reconcile = (ctx: ExtensionContext): OnlineState => {
			const branch = ctx.sessionManager.getBranch();
			let state = restoreOnlineState(branch);
			let stateIndex = -1;
			for (let i = branch.length - 1; i >= 0; i--) {
				const entry = branch[i];
				if (entry.type === "custom" && entry.customType === ONLINE_STATE_ENTRY &&
					JSON.stringify(restoreOnlineState([entry])) === JSON.stringify(state)) { stateIndex = i; break; }
			}
			for (const entry of branch.slice(stateIndex + 1)) {
				if (entry.type === "compaction") state = recordCompaction(state, { debtTokens: 0, repaymentTokens: 0 });
			}
			return state;
		};
		const tokens = (ctx: ExtensionContext): number => {
			const branch = ctx.sessionManager.getBranch();
			return estimateProjectedContextTokens(buildSessionProjection(branch), branch).tokens;
		};

		registerOnlineTools(pi, {
			updatePlan: async (input) => {
				if (!eligible(input.context)) throw new Error("Online Context Compact requires an enabled, persistent main session");
				if (input.signal?.aborted || input.context.signal?.aborted) throw new Error("Plan update was aborted");
				const steps = parsePlanSteps(input.steps);
				if (!steps?.length) throw new Error("Plan must contain at least one valid step");
				let state = reconcile(input.context);
				const transition = analyzePlanTransition(state.plan, steps);
				const completedIds = transition.completedSteps.map((step) => step.id);
				if (completedIds.length) {
					state = recordBoundary(state, steps, progressSummary(input, completedIds[0]));
					pending.push({ id: input.toolCallId, generation, session: identity(input.context) });
				} else state = { ...state, plan: [...steps] };
				appendOnlineState(pi, state);
				return {
					content: [{ type: "text", text: [formatPlanSnapshot(steps), ...transition.advice].join("\n") }],
					details: { boundary: completedIds.length > 0, completed_step_ids: completedIds,
						progress_recorded: completedIds.length > 0 && input.progress !== undefined,
						task_status: "active", plan: steps },
				};
			},
		});
		pi.on("session_start", (_event, ctx) => { cancel(); syncTool(ctx); });
		pi.on("session_tree", (_event, ctx) => { cancel(); syncTool(ctx); });
		pi.on("session_before_tree", cancel);
		pi.on("session_before_switch", cancel);
		pi.on("session_before_fork", cancel);
		pi.on("session_shutdown", cancel);
		pi.on("model_select", (_event, ctx) => { cancel(); syncTool(ctx); });
		pi.on("before_agent_start", (_event, ctx) => { cancel(); syncTool(ctx); });
		pi.on("turn_start", (_event, ctx) => {
			pending = [];
			// Count main-agent turns as the request-horizon proxy, not transport attempts.
			// CacheWarmer replays onPayload/before_provider_request even while streaming
			// or awaiting our summary; only the agent lifecycle may repay OCC debt.
			if (eligible(ctx)) appendOnlineState(pi, recordProviderRequest(reconcile(ctx), tokens(ctx)));
		});
		pi.on("input", (event, ctx) => {
			if (event.streamingBehavior === "steer" || event.text.startsWith("CORRECTION:")) {
				cancel();
				if (eligible(ctx)) appendOnlineState(pi, recordCorrection(reconcile(ctx)));
			}
			return { action: "continue" as const };
		});
		pi.on("session_before_compact", (event) => {
			cancel();
			// Native preparation ignores fromHook details. Carry only our native-shaped, marked lists.
			const previous = [...event.branchEntries].reverse().find((entry) => entry.type === "compaction");
			const details = previous?.details as { preparation?: string; readFiles?: string[]; modifiedFiles?: string[] } | undefined;
			if (details?.preparation === OCC_FILE_TRACKING) {
				for (const path of details.readFiles ?? []) event.preparation.fileOps.read.add(path);
				for (const path of details.modifiedFiles ?? []) event.preparation.fileOps.edited.add(path);
			}
		});
		pi.on("session_compact", (_event, ctx) => {
			cancel();
			if (ctx.sessionManager.getSessionFile()) appendOnlineState(pi, reconcile(ctx));
		});

		pi.on("turn_end", async (event, ctx) => {
			const boundaries = pending;
			pending = []; // Consume once, even on failure or a later extension dropping the drafts.
			if (!eligible(ctx) || !ctx.model || controller || ctx.signal?.aborted || event.outcome !== "completed" ||
				event.message.role !== "assistant" || !["stop", "toolUse"].includes(event.message.stopReason)) return;
			if (!boundaries.some((boundary) => boundary.generation === generation && boundary.session === identity(ctx) &&
				event.toolResults.some((item) => item.toolCallId === boundary.id && item.toolName === "update_plan" && !item.isError))) return;
			// The native preparation MUST see real IDs, never a speculative preview tree.
			if (event.entries.some((entry) => entry.type !== "custom")) return;
			const state = reconcile(ctx);
			const remaining = state.plan.filter((step) => step.status !== "completed");
			if (!remaining.length) return;
			const local = new AbortController();
			controller = local;
			const parentSignal = ctx.signal; // Capture once: ctx.signal is a live getter.
			const abort = (): void => local.abort();
			parentSignal?.addEventListener("abort", abort, { once: true });
			const modelKey = (): string => JSON.stringify([ctx.model?.provider, ctx.model?.id, ctx.model?.api]);
			const captured = { generation, session: identity(ctx), leaf: ctx.sessionManager.getLeafId(), model: ctx.model, modelKey: modelKey() };
			const leafUnchanged = (): boolean => {
				if (ctx.sessionManager.getLeafId() === captured.leaf) return true;
				// Native CacheWarmer appends non-context usage after a successful replay.
				// Permit only that suffix; any other append or branch change is stale.
				const branch = ctx.sessionManager.getBranch();
				for (let i = branch.length - 1; i >= 0; i--) {
					const entry = branch[i];
					if (entry.id === captured.leaf) return true;
					if (entry.type !== "usage" || entry.kind !== "cache_warm") return false;
				}
				return false;
			};
			const valid = (): boolean => !local.signal.aborted && !parentSignal?.aborted && generation === captured.generation &&
				identity(ctx) === captured.session && leafUnchanged() &&
				ctx.model === captured.model && modelKey() === captured.modelKey;
			try {
				if (!valid()) return;
				const settings = resolveSettings(ctx);
				if (!settings.compaction.enabled) return;
				const preparation = prepareCompaction(ctx.sessionManager.getBranch(), settings.compaction);
				if (!preparation) return;
				const writeTokens = preparation.tokensBefore;
				const fixedTokens = Math.ceil(Buffer.byteLength(ctx.getSystemPrompt()) / 4);
				const archiveTokens = Math.max(0, writeTokens - fixedTokens - settings.compaction.keepRecentTokens);
				const decision = decideCompaction({
					writeTokens, archiveTokens, memoTokens: DEFAULT_NATIVE_SUMMARY_TOKEN_ESTIMATE,
					contextTokens: writeTokens, completedBoundaryRequestCounts: state.completedBoundaryRequestCounts,
					remainingBoundaries: remaining.length,
					averageContextTokenIncrement: state.positiveContextDeltaCount ? state.positiveContextDeltaTotal / state.positiveContextDeltaCount : null,
					contextWindowTokens: ctx.model.contextWindow > 0 ? ctx.model.contextWindow : null,
					priorCompactionCount: state.nativeCompactionCount, carriedDebtTokens: state.cacheDebtTokens,
					cacheDebtRepaymentTokens: state.cacheDebtRepaymentTokens, cacheWriteReadRatio: ratio,
					economics: { ...DEFAULT_COMPACTION_ECONOMICS, windowReserveTokens: settings.compaction.reserveTokens },
				});
				if (!decision.compact) return;
				const stream: NonNullable<Parameters<typeof compact>[7]> = (model, context, requestOptions) => {
					if (!valid()) throw new Error("Stale OCC summary");
					// Registry supplies request-time auth. No main-session onPayload/onResponse hooks.
					const response = ctx.modelRegistry.streamSimple(model, context, { ...requestOptions, signal: local.signal });
					const result = response.result.bind(response);
					response.result = async () => {
						const message = await result();
						if (!valid()) throw new Error("Cancelled OCC summary");
						// Leave error responses intact for native retryAssistantCall classification/retry.
						if (message.stopReason === "error") return message;
						if (message.stopReason === "aborted" || message.stopReason === "deferred" ||
							!message.content.some((part) => part.type === "text" && part.text.trim())) {
							throw new Error("OCC summary was aborted, deferred, or empty");
						}
						return message;
					};
					return response;
				};
				const result = await compact(preparation, captured.model, undefined, undefined,
					BOUNDARY_COMPACTION_INSTRUCTIONS, local.signal, ctx.thinkingLevel, stream, undefined, settings.retry);
				if (!valid() || !result.summary.trim() || !eligible(ctx)) return;
				const next = recordCompaction(state, {
					debtTokens: decision.writeTokens * (decision.incrementalCacheCostRatio ?? 0),
					repaymentTokens: Math.max(0, decision.archiveTokens - decision.memoTokens),
				});
				return {
					entries: [...event.entries,
						{ type: "compaction" as const, summary: result.summary, firstKeptEntryId: result.firstKeptEntryId,
							usage: result.usage, details: { ...result.details as object, preparation: OCC_FILE_TRACKING } },
						{ type: "custom" as const, customType: ONLINE_STATE_ENTRY, data: next },
						{ type: "custom_message" as const, customType: "sol-pi-online-context-compact", display: false,
							content: `${POST_COMPACTION_PLAN_REMINDER}\nRemaining work: ${JSON.stringify(remaining)}` },
					],
					continue: true,
				};
			} catch {
				// Optional optimization: fail open to the existing scheduler; never enqueue a ghost turn.
				return;
			} finally {
				parentSignal?.removeEventListener("abort", abort);
				if (controller === local) controller = undefined;
			}
		});
	};
}
