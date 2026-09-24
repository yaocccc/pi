import { randomUUID } from "node:crypto";

export const INTERACTION_EVENT = "pi:human-interaction:v1";
export const HUMAN_WAIT_LIMIT_MS = 30 * 60_000;
export interface InteractionEvent {
	phase: "begin" | "end";
	token: string;
	deadline: number;
	waitUntil(promise: Promise<unknown>): void;
}
export interface InteractionBus { emit(name: string, event: unknown): void }

/** emit is synchronous: listeners must register preparation/cleanup promises synchronously. */
export async function withHumanInteraction<T>(bus: InteractionBus, signal: AbortSignal | undefined,
	run: (signal: AbortSignal) => Promise<T>, limitMs = HUMAN_WAIT_LIMIT_MS): Promise<T> {
	const token = randomUUID();
	const deadline = Date.now() + Math.min(limitMs, HUMAN_WAIT_LIMIT_MS);
	const controller = new AbortController();
	const abort = () => controller.abort(signal?.reason ?? new Error("用户交互已取消"));
	signal?.addEventListener("abort", abort, { once: true });
	if (signal?.aborted) abort();
	const timer = setTimeout(() => controller.abort(new Error("人工确认等待已达上限，未获得同意")), Math.max(1, deadline - Date.now()));
	timer.unref();
	let rejectAbort!: (reason: unknown) => void;
	const aborted = new Promise<never>((_, reject) => { rejectAbort = reject; });
	const onAbort = () => rejectAbort(controller.signal.reason);
	controller.signal.addEventListener("abort", onAbort, { once: true });
	const dispatch = (phase: InteractionEvent["phase"]) => {
		const waits: Promise<unknown>[] = [];
		try { bus.emit(INTERACTION_EVENT, { phase, token, deadline, waitUntil: (p: Promise<unknown>) => waits.push(p) } satisfies InteractionEvent); }
		catch (error) { waits.push(Promise.reject(error)); }
		return Promise.all(waits);
	};
	try {
		if (controller.signal.aborted) throw controller.signal.reason;
		await Promise.race([dispatch("begin"), aborted]);
		if (controller.signal.aborted || Date.now() >= deadline) throw controller.signal.reason ?? new Error("人工确认等待已达上限，未获得同意");
		const result = await Promise.race([run(controller.signal), aborted]);
		if (controller.signal.aborted || Date.now() >= deadline) throw controller.signal.reason ?? new Error("人工确认等待已达上限，未获得同意");
		return result;
	} finally {
		clearTimeout(timer);
		signal?.removeEventListener("abort", abort);
		controller.signal.removeEventListener("abort", onAbort);
		// End also runs if preparation failed or the UI threw; never infer consent.
		await dispatch("end");
	}
}
