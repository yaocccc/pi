// Bundled support from pi-ui/rpc.ts; update via npm run sync:portable.
// Self-contained at runtime: consumers do not need the source template.
import type { ExtensionAPI, ExtensionCommandContext } from '@earendil-works/pi-coding-agent';
import { getCapability, registerCapability, sessionActivities } from '../../worker/capabilities.ts';

export const EXTENSION_RPC_COMMAND = 'pi-ui-rpc';
export const EXTENSION_RPC_PREFIX = 'pi-ui:rpc:v1:';
// notify.message is JSON-encoded again by the host. Reserve ample room for
// its envelope under the Go transport's 64 MiB JSONL line limit.
const MAX_ENCODED_MESSAGE_BYTES = 32 * 1024 * 1024;
export type RpcMethod = (params: unknown, ctx: ExtensionCommandContext) => unknown | Promise<unknown>;
const methodCapability = (name: string) => `pi-ui.rpc.method:v1:${name}`;

/** Optional adapters expose explicit operations; this bridge never owns business logic. */
export function registerRpcMethod(pi: Pick<ExtensionAPI, 'events'>, name: string, handler: RpcMethod): () => void {
    return registerCapability(pi, methodCapability(name), handler);
}

export class RpcUnavailableError extends Error {
    readonly code = 'unavailable';
}

export function registerExtensionRpc(pi: ExtensionAPI): void {
    const key = 'pi-ui.rpc.bridge:v1';
    if (getCapability(pi, key)) return;
    registerCapability(pi, key, true);
    let generation = 0;
    let waitForIdle: (() => Promise<void>) | undefined;
    let waiting = false;
    let unobservedRun = false;
    const reset = () => { generation++; waitForIdle = undefined; waiting = false; unobservedRun = false; };
    const observeRun = () => {
        if (waiting) return;
        if (!waitForIdle) { unobservedRun = true; return; }
        const owner = generation;
        waiting = true;
        unobservedRun = false;
        try {
            // Capture while Pi is busy. Calling waitForIdle only after settled
            // would bypass the SDK resolver and miss its deferred actions.
            void waitForIdle().then(() => {
                if (owner === generation) waiting = false;
            }, () => {
                if (owner === generation) { waiting = false; unobservedRun = true; }
            });
        } catch {
            waiting = false;
            unobservedRun = true;
        }
    };
    const lifecycleAvailable = typeof pi.on === 'function';
    if (lifecycleAvailable) {
        pi.on('session_start', reset);
        pi.on('session_shutdown', reset);
        pi.on('agent_start', observeRun);
    }
    registerRpcMethod(pi, 'session.activity', async (_params, ctx) => {
        if (!lifecycleAvailable || typeof ctx.isIdle !== 'function' || typeof ctx.hasPendingMessages !== 'function' || typeof ctx.waitForIdle !== 'function') {
            throw Object.assign(new Error('Pi idle barrier is unavailable'), { code: 'activity-unavailable' });
        }
        // The frontend primes this once before admitting writes. Busy probes
        // may also attach to the current run, but never await it or add waiters.
        waitForIdle = () => ctx.waitForIdle();
        if (!ctx.isIdle()) observeRun();
        const reasons: string[] = [];
        if (waiting) reasons.push('Pi is finishing its run and deferred actions');
        if (unobservedRun) reasons.push('Pi run completion was not observed; keep this session open');
        if (!ctx.isIdle()) reasons.push('Pi is running or compacting');
        if (ctx.hasPendingMessages()) reasons.push('Pi has queued messages');
        for (const activity of sessionActivities(pi)) {
            if (activity.reason) reasons.push(`${activity.name}: ${activity.reason}`);
        }
        return { version: 1, idle: reasons.length === 0, reasons };
    });
    pi.registerCommand(EXTENSION_RPC_COMMAND, {
        description: 'Optional extension UI RPC bridge (JSON request)',
        handler: async (args, ctx) => {
            if (ctx.mode !== 'rpc' || !ctx.hasUI) return;
            let id = '';
            let response: { id: string; result?: unknown; error?: { code: string; message: string } };
            try {
                const request = JSON.parse(args);
                if (typeof request?.id === 'string' && request.id.length <= 128) id = request.id;
                if (!id || typeof request?.method !== 'string' || !request.method || request.method.length > 128) {
                    throw Object.assign(new Error('Expected {id, method, params}'), { code: 'invalid-request' });
                }
                const handler = getCapability<RpcMethod>(pi, methodCapability(request.method));
                if (!handler) throw new RpcUnavailableError(`Extension method unavailable: ${request.method}`);
                const result = await handler(request.params, ctx);
                response = { id, result: result ?? null };
                // Serialization is transport validation, not capability validation.
                JSON.stringify(response);
            } catch (error) {
                response = { id, error: {
                    code: error instanceof Error && 'code' in error && typeof error.code === 'string' ? error.code : 'extension-error',
                    message: error instanceof Error ? error.message : String(error),
                } };
            }
            let message = EXTENSION_RPC_PREFIX + JSON.stringify(response);
            if (Buffer.byteLength(JSON.stringify(message), 'utf8') > MAX_ENCODED_MESSAGE_BYTES) {
                message = EXTENSION_RPC_PREFIX + JSON.stringify({ id, error: {
                    code: 'response-too-large', message: 'Extension response exceeds the RPC transport limit',
                } });
            }
            ctx.ui.notify(message, 'info');
        },
    });
}
