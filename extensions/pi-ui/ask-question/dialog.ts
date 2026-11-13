import type { ExtensionContext } from '@earendil-works/pi-coding-agent';

type Factory<T> = Parameters<ExtensionContext['ui']['custom']>[0] extends (...args: infer A) => any
    ? (tui: A[0], theme: A[1], keybindings: A[2], done: (value: T | null) => void) => ReturnType<Parameters<ExtensionContext['ui']['custom']>[0]>
    : never;

/** Owns only the native editor surface. Serialization and request lifetime belong
 * to the question capability; abort closes this surface before its slot is released.
 */
export async function customQuestion<T>(ctx: ExtensionContext, signal: AbortSignal | undefined, factory: Factory<T>): Promise<T | null> {
    if (!ctx.hasUI || ctx.mode !== 'tui') return null;
    signal?.throwIfAborted();
    let detach = () => {};
    try {
        const value = await ctx.ui.custom<T | null>((tui, theme, keys, done) => {
            let settled = false;
            const finish = (value: T | null) => { if (settled) return; settled = true; detach(); done(value); };
            const abort = () => finish(null);
            signal?.addEventListener('abort', abort, { once: true });
            detach = () => signal?.removeEventListener('abort', abort);
            const component = factory(tui, theme, keys, finish);
            if (signal?.aborted) queueMicrotask(abort);
            return component;
        });
        signal?.throwIfAborted();
        return value ?? null;
    } finally { detach(); }
}
