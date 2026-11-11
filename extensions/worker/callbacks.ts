/** Best-effort display/telemetry callbacks only; never wrap task execution or process cleanup. */
export function present(callback: () => unknown): void {
	try {
		const result = callback();
		if (result && typeof (result as PromiseLike<unknown>).then === "function") {
			void Promise.resolve(result).catch(() => {});
		}
	} catch { /* A detached/broken UI must not interrupt lifecycle cleanup. */ }
}

