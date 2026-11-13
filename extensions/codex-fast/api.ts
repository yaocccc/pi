/** Public capability contract; no UI implementation is required to use it. */
export const CODEX_FAST_CAPABILITY = 'codex-fast';
export const CODEX_FAST_CHANGED = 'codex-fast:changed';
export type FastLevel = 'normal' | 'fast' | 'ultrafast';
export interface FastConfig { level: FastLevel }
export interface CodexFastService {
    readonly levels: readonly FastLevel[];
    /** Reload persisted state; transient read errors retain the last good state. */
    getConfig(): FastConfig;
    /** Validate and atomically persist a level, returning the committed state. */
    setLevel(level: unknown): FastConfig;
}

export class InvalidFastLevelError extends Error {
    readonly code = 'invalid-level';
    constructor(readonly value: unknown) {
        super(`Invalid Codex level: ${String(value)}`);
        this.name = 'InvalidFastLevelError';
    }
}
