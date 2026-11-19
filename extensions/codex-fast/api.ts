/** Public capability contract; no UI implementation is required to use it. */
export const CODEX_FAST_CAPABILITY = 'codex-fast';
export const CODEX_FAST_CHANGED = 'codex-fast:changed';
export type FastLevel = 'normal' | 'fast' | 'ultrafast';
export interface FastConfig { level: FastLevel }
export interface CodexFastService {
    readonly levels: readonly FastLevel[];
    /** Read this process's current state, without reloading the startup default. */
    getConfig(): FastConfig;
    /** Validate and atomically persist a level, returning the committed state. */
    setLevel(level: unknown): FastConfig;
    /** Restore a host instance's level in memory only. Older capabilities may omit this. */
    applyLevel?(level: unknown): FastConfig;
}

export class InvalidFastLevelError extends Error {
    readonly code = 'invalid-level';
    constructor(readonly value: unknown) {
        super(`Invalid Codex level: ${String(value)}`);
        this.name = 'InvalidFastLevelError';
    }
}
