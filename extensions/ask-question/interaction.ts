import type { ExtensionContext } from "@earendil-works/pi-coding-agent";
import { HUMAN_WAIT_LIMIT_MS, withHumanInteraction, type InteractionBus } from "../shared/interaction-lifecycle.ts";

type Factory<T> = Parameters<ExtensionContext["ui"]["custom"]>[0] extends (...args: infer A) => any
	? (tui: A[0], theme: A[1], keybindings: A[2], done: (value: T | null) => void) => ReturnType<Parameters<ExtensionContext["ui"]["custom"]>[0]>
	: never;

/** Shared by single-question and questionnaire UIs; no user answer is sent to Workers. */
export class QuestionInteractions {
	private active = new Map<AbortController, Promise<void>>();
	// Pi's non-overlay custom UI replaces the editor, so only one may own it.
	private tail: Promise<void> = Promise.resolve();
	private stopping = false;
	constructor(private bus: InteractionBus) {}
	async shutdown() {
		this.stopping = true;
		try {
			const active = [...this.active];
			for (const [controller] of active) controller.abort(new Error("用户交互会话已关闭"));
			await Promise.all(active.map(([, completed]) => completed));
		} finally { this.stopping = false; }
	}
	async custom<T>(ctx: ExtensionContext, signal: AbortSignal | undefined, factory: Factory<T>): Promise<T | null> {
		if (!ctx.hasUI || ctx.mode !== "tui") return null;
		if (this.stopping) throw new Error("用户交互会话已关闭");
		const controller = new AbortController();
		let completed!: () => void;
		this.active.set(controller, new Promise<void>((resolve) => { completed = resolve; }));
		const combined = signal ? AbortSignal.any([signal, controller.signal]) : controller.signal;
		const previous = this.tail;
		let release!: () => void;
		const released = new Promise<void>((resolve) => { release = resolve; });
		// Cancelling a queued entry must not let its successor overtake the owner.
		this.tail = previous.then(() => released);
		const deadline = Date.now() + HUMAN_WAIT_LIMIT_MS;
		const timer = setTimeout(() => controller.abort(new Error("人工确认等待已达上限，未获得同意")), HUMAN_WAIT_LIMIT_MS);
		timer.unref();
		let abort!: () => void;
		const aborted = new Promise<never>((_, reject) => { abort = () => reject(combined.reason); });
		combined.addEventListener("abort", abort, { once: true });
		let detach = () => {};
		try {
			if (combined.aborted) throw combined.reason;
			await Promise.race([previous, aborted]);
			if (combined.aborted) throw combined.reason;
			if (Date.now() >= deadline) throw new Error("人工确认等待已达上限，未获得同意");
			// Queue time is bounded but is NOT human interaction: no pause token yet.
			return await withHumanInteraction(this.bus, combined, async (interactionSignal) => {
				const value = await ctx.ui.custom<T | null>((tui, theme, keys, done) => {
					let settled = false;
					const finish = (value: T | null) => { if (settled) return; settled = true; detach(); done(value); };
					const abort = () => finish(null);
					interactionSignal.addEventListener("abort", abort, { once: true });
					detach = () => interactionSignal.removeEventListener("abort", abort);
					const component = factory(tui, theme, keys, finish);
					if (interactionSignal.aborted) queueMicrotask(abort);
					return component;
				});
				return value ?? null; // RPC/custom unavailable must never fabricate a choice.
			}, deadline - Date.now());
		} finally {
			detach(); clearTimeout(timer); combined.removeEventListener("abort", abort);
			this.active.delete(controller); release(); completed();
		}
	}
}
