// Bundled support from pi-ui/rpc.ts; update via npm run sync:portable.
// Self-contained at runtime: consumers do not need the source template.
import type { ExtensionAPI, ExtensionCommandContext } from '@earendil-works/pi-coding-agent';
import { getCapability, registerCapability } from '../../codex-fast/capabilities.ts';

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
