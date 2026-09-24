export interface RenderRefreshContext {
	state: Record<string, unknown>;
	invalidate: () => void;
}

export interface RenderRefreshTimerApi {
	setInterval: (callback: () => void, delayMs: number) => unknown;
	clearInterval: (handle: unknown) => void;
}

type RefreshTimer = {
	handle: unknown;
	timerApi: RenderRefreshTimerApi;
};

type RefreshState = Record<string, unknown> & {
	__workerElapsedRefresh?: RefreshTimer;
};

const REFRESH_INTERVAL_MS = 1_000;
const defaultTimerApi: RenderRefreshTimerApi = {
	setInterval: (callback, delayMs) => setInterval(callback, delayMs),
	clearInterval: (handle) => clearInterval(handle as ReturnType<typeof setInterval>),
};

/**
 * Keeps an active tool row repainting so elapsed time advances without a
 * progress update. The timer belongs to the renderer's row-local state.
 */
export function updateRenderRefresh(
	context: RenderRefreshContext | undefined,
	isPartial: boolean,
	timerApi: RenderRefreshTimerApi = defaultTimerApi,
): void {
	if (!context) return;
	const state = context.state as RefreshState;
	const existing = state.__workerElapsedRefresh;

	if (isPartial) {
		if (existing) return;
		const timer: RefreshTimer = {
			handle: undefined,
			timerApi,
		};
		timer.handle = timerApi.setInterval(() => context.invalidate(), REFRESH_INTERVAL_MS);
		state.__workerElapsedRefresh = timer;
		return;
	}

	if (existing) {
		existing.timerApi.clearInterval(existing.handle);
		delete state.__workerElapsedRefresh;
	}
}
