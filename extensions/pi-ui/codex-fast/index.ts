import type { ExtensionAPI, ExtensionContext } from '@earendil-works/pi-coding-agent';
import { CODEX_FAST_CAPABILITY, CODEX_FAST_CHANGED, type CodexFastService, type FastConfig } from '../../codex-fast/api.ts';
import { registerExtensionRpc, registerRpcMethod, RpcUnavailableError } from './bridge.ts';
import { getCapability } from '../../codex-fast/capabilities.ts';
import { emitStructuredEvent, supportsStructuredUI } from './support.ts';
import { notifyNativeError, notifyNativeSaved, notifyNativeUnavailable, selectNativeLevel } from './native-ui.ts';

export default registerCodexFastUI;

/** Optional UI adapter. All validation and persistence belong to the capability. */
export function registerCodexFastUI(pi: ExtensionAPI): void {
    registerExtensionRpc(pi);
    let activeContext: ExtensionContext | undefined;
    const service = () => {
        const value = getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY);
        if (!value) throw new RpcUnavailableError('Codex Fast capability unavailable');
        return value;
    };
    pi.on('session_start', (_event, ctx) => { activeContext = ctx; });
    pi.on('session_shutdown', () => { activeContext = undefined; });
    pi.events.on(CODEX_FAST_CHANGED, (data: unknown) => {
        const ctx = activeContext;
        if (!ctx?.hasUI) return;
        const saved = data as FastConfig;
        if (supportsStructuredUI(ctx)) emitStructuredEvent(ctx, 'codex.settings-saved', { level: saved.level });
        else notifyNativeSaved(ctx, saved.level);
    });
    registerRpcMethod(pi, 'codex-fast.get', (_params, ctx) => {
        activeContext = ctx;
        const capability = service();
        return { ...capability.getConfig(), levels: [...capability.levels] };
    });
    registerRpcMethod(pi, 'codex-fast.set', (params, ctx) => {
        activeContext = ctx;
        const capability = service();
        const level = params && typeof params === 'object' ? (params as { level?: unknown }).level : undefined;
        return { ...capability.setLevel(level), levels: [...capability.levels] };
    });
    pi.registerCommand('codex-fast', {
        description: '设置 Codex 档位：normal / fast / ultrafast',
        handler: async (_args, ctx) => {
            activeContext = ctx;
            const goUI = supportsStructuredUI(ctx);
            if (!ctx.hasUI) {
                if (!goUI) notifyNativeUnavailable(ctx);
                return;
            }
            const service = getCapability<CodexFastService>(pi, CODEX_FAST_CAPABILITY);
            if (!service) {
                ctx.ui.notify('Codex Fast capability unavailable', 'warning');
                return;
            }
            const current = service.getConfig();
            const selected = goUI
                ? await ctx.ui.input('pi-goui:codex-fast:v1', JSON.stringify(current))
                : await selectNativeLevel(ctx, current.level, service.levels);
            if (selected === undefined) return;
            const emit = (name: string, data: Record<string, unknown>) => emitStructuredEvent(ctx, name, data);
            try {
                service.setLevel(selected); // The capability's committed event drives every UI.
            } catch (error) {
                if (goUI) emit('codex.settings-error', error instanceof Error && 'code' in error && error.code === 'invalid-level'
                    ? { code: error.code, message: String(selected) }
                    : { message: error instanceof Error ? error.message : String(error) });
                else notifyNativeError(ctx, error);
            }
        },
    });
}
