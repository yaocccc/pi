import { createHash, randomBytes } from 'node:crypto';
import { spawn } from 'node:child_process';
import { createConnection, type Socket } from 'node:net';
import { fileURLToPath } from 'node:url';
import {
    MAX_FRAME_BYTES,
    decodeTelegramFrame,
    encodeTelegramFrame,
    telegramServiceAddress,
    type RoutedTelegramCallback,
} from './server.ts';

const DEFAULT_HEARTBEAT_MS = 1_000;
const DEFAULT_REQUEST_TIMEOUT_MS = 10_000;
const DEFAULT_RECONNECT_MS = 100;
const DEFAULT_SPAWN_COOLDOWN_MS = 1_000;
const DEFAULT_ROUTE_TTL_MS = 24 * 60 * 60 * 1_000;
const DEFAULT_MAX_OWNED_ROUTES = 4_096;
const MAX_SEEN_EVENTS = 1_024;
const SEEN_EVENT_TTL_MS = 60_000;
const EXPECTED_ERRORS = new Set(['EPIPE', 'ECONNRESET', 'ECONNREFUSED', 'ENOENT']);

type ClientPayload =
    | { type: 'hello'; clientId: string; generation: string; poll: boolean }
    | { type: 'heartbeat'; clientId: string; generation: string; poll: boolean }
    | { type: 'route'; clientId: string; generation: string; chatId: string; messageId: number; expiresAt: number }
    | { type: 'ack'; clientId: string; generation: string; eventId: string }
    | { type: 'detach'; clientId: string; generation: string; requestId: string; preserveRoutes: boolean }
    | { type: 'rpc'; clientId: string; generation: string; requestId: string; method: BotRpcMethod; args: unknown[] };
type ServerPayload =
    | { type: 'ready'; clientId: string; generation: string }
    | { type: 'response'; clientId: string; generation: string; requestId: string; ok: boolean; result?: unknown; error?: string }
    | { type: 'event'; clientId: string; eventId: string; event: 'reply'; text: string }
    | { type: 'event'; clientId: string; eventId: string; event: 'callback'; callback: RoutedTelegramCallback };
type BotRpcMethod = 'sendMessage' | 'editMessageReplyMarkup' | 'answerCallbackQuery';
type PendingRequest = { resolve: (value: unknown) => void; reject: (error: Error) => void; timer: ReturnType<typeof setTimeout> };
type OwnedRoute = { chatId: string; messageId: number; expiresAt: number };
type SharedIdentity = {
    clientId: string;
    seenEvents: Map<string, number>;
    inFlightEvents: Map<string, Promise<void>>;
    routes: Map<string, OwnedRoute>;
};

const IDENTITY_CACHE = Symbol.for('pi.telegram.client-identities');
type IdentityScope = typeof globalThis & { [IDENTITY_CACHE]?: Map<string, SharedIdentity> };

const sharedIdentity = (address: string, requestedClientId?: string): SharedIdentity => {
    const scope = globalThis as IdentityScope;
    const cache = scope[IDENTITY_CACHE] ??= new Map();
    const addressKey = createHash('sha256').update(address).digest('hex');
    const defaultKey = `${addressKey}:default`;
    if (!requestedClientId) {
        const existing = cache.get(defaultKey);
        if (existing) return existing;
        const created = {
            clientId: randomBytes(24).toString('base64url'),
            seenEvents: new Map<string, number>(),
            inFlightEvents: new Map<string, Promise<void>>(),
            routes: new Map<string, OwnedRoute>(),
        };
        cache.set(defaultKey, created);
        return created;
    }
    const explicitKey = `${addressKey}:${createHash('sha256').update(requestedClientId).digest('hex')}`;
    const existing = cache.get(explicitKey);
    if (existing) return existing;
    const created = {
        clientId: requestedClientId,
        seenEvents: new Map<string, number>(),
        inFlightEvents: new Map<string, Promise<void>>(),
        routes: new Map<string, OwnedRoute>(),
    };
    cache.set(explicitKey, created);
    return created;
};

const hmacKey = (token: string): Buffer =>
    createHash('sha256').update(`pi-telegram-service-ipc:${token}`).digest();
const isRecord = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value);
const safeId = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && Buffer.byteLength(value) <= 256;
const errorCode = (error: unknown): string | undefined => isRecord(error) && typeof error.code === 'string' ? error.code : undefined;
const ownedRouteKey = (chatId: string, messageId: number): string => JSON.stringify([chatId, messageId]);

const validServerPayload = (value: unknown): value is ServerPayload => {
    if (!isRecord(value) || !safeId(value.type) || !safeId(value.clientId)) return false;
    if (value.type === 'ready') return safeId(value.generation);
    if (value.type === 'response') return safeId(value.generation) && safeId(value.requestId) && typeof value.ok === 'boolean'
        && (value.ok || typeof value.error === 'string');
    if (value.type !== 'event' || !safeId(value.eventId)) return false;
    if (value.event === 'reply') return typeof value.text === 'string' && value.text.trim().length > 0;
    if (value.event !== 'callback' || !isRecord(value.callback)) return false;
    const callback = value.callback;
    return safeId(callback.callbackQueryId) && safeId(callback.data) && Number.isSafeInteger(callback.userId)
        && callback.userIsBot === false && safeId(callback.chatId) && Number.isSafeInteger(callback.messageId);
};

export type TelegramMessage = { chat: { id: string | number }; message_id: number };
export interface TelegramBotRpc {
    sendMessage(chatId: string | number, text: string, options?: unknown): Promise<TelegramMessage>;
    editMessageReplyMarkup(markup: unknown, options: { chat_id: string | number; message_id: number }): Promise<unknown>;
    answerCallbackQuery(callbackQueryId: string, options?: { text?: string; show_alert?: boolean }): Promise<unknown>;
}
export interface TelegramRouteRegistrar {
    registerRoute(chatId: string | number, messageId: number): void;
}

export interface TelegramClientOptions {
    token: string;
    poll: boolean;
    address?: string;
    serverPath?: string;
    heartbeatMs?: number;
    requestTimeoutMs?: number;
    reconnectMs?: number;
    spawnCooldownMs?: number;
    routeTtlMs?: number;
    maxOwnedRoutes?: number;
    clientId?: string;
    generation?: string;
    spawnService?: (token: string) => void;
    onReply: (text: string) => void | Promise<void>;
    onCallback?: (callback: RoutedTelegramCallback) => void | Promise<void>;
    onError?: (error: unknown) => void;
}

/** Reconnecting Pi-side RPC client. It never constructs TelegramBot or polls Telegram. */
export class TelegramClient implements TelegramBotRpc, TelegramRouteRegistrar {
    readonly clientId: string;
    readonly generation: string;
    readonly address: string;
    private readonly options: TelegramClientOptions;
    private readonly key: Buffer;
    private readonly identity: SharedIdentity;
    private readonly heartbeatMs: number;
    private readonly requestTimeoutMs: number;
    private readonly reconnectMs: number;
    private readonly spawnCooldownMs: number;
    private readonly routeTtlMs: number;
    private readonly maxOwnedRoutes: number;
    private readonly pending = new Map<string, PendingRequest>();
    private socket?: Socket;
    private readySocket?: Socket;
    private heartbeatTimer?: ReturnType<typeof setInterval>;
    private reconnectTimer?: ReturnType<typeof setTimeout>;
    private connectTimer?: ReturnType<typeof setTimeout>;
    private connectPromise?: Promise<void>;
    private resolveConnect?: () => void;
    private rejectConnect?: (error: Error) => void;
    private started = false;
    private closed = false;
    private requestSequence = 0;
    private lastSpawnAt = Number.NEGATIVE_INFINITY;

    constructor(options: TelegramClientOptions) {
        this.options = options;
        this.address = options.address ?? telegramServiceAddress(options.token);
        this.identity = sharedIdentity(this.address, options.clientId);
        this.clientId = this.identity.clientId;
        this.generation = options.generation ?? randomBytes(16).toString('hex');
        this.key = hmacKey(options.token);
        this.heartbeatMs = options.heartbeatMs ?? DEFAULT_HEARTBEAT_MS;
        this.requestTimeoutMs = options.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
        this.reconnectMs = options.reconnectMs ?? DEFAULT_RECONNECT_MS;
        this.spawnCooldownMs = options.spawnCooldownMs ?? DEFAULT_SPAWN_COOLDOWN_MS;
        this.routeTtlMs = options.routeTtlMs ?? DEFAULT_ROUTE_TTL_MS;
        this.maxOwnedRoutes = options.maxOwnedRoutes ?? DEFAULT_MAX_OWNED_ROUTES;
    }

    start(): void {
        if (this.started || this.closed) return;
        this.started = true;
        this.connect();
    }

    isConnected(): boolean {
        return Boolean(this.readySocket && this.readySocket === this.socket && !this.readySocket.destroyed && this.readySocket.readyState === 'open');
    }

    async sendMessage(chatId: string | number, text: string, options?: unknown): Promise<TelegramMessage> {
        const message = await this.rpc('sendMessage', [chatId, text, options]) as TelegramMessage;
        this.rememberRoute(String(message.chat.id), message.message_id, Date.now() + this.routeTtlMs, false);
        return message;
    }

    editMessageReplyMarkup(markup: unknown, options: { chat_id: string | number; message_id: number }): Promise<unknown> {
        return this.rpc('editMessageReplyMarkup', [markup, options]);
    }

    answerCallbackQuery(callbackQueryId: string, options?: { text?: string; show_alert?: boolean }): Promise<unknown> {
        return this.rpc('answerCallbackQuery', [callbackQueryId, options]);
    }

    registerRoute(chatId: string | number, messageId: number): void {
        if (this.closed || !Number.isSafeInteger(messageId)) return;
        this.rememberRoute(String(chatId), messageId, Date.now() + this.routeTtlMs, true);
    }

    async shutdown(options: { preserveRoutes: boolean }): Promise<void> {
        if (this.closed) return;
        this.closed = true;
        if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
        if (this.reconnectTimer) clearTimeout(this.reconnectTimer);
        this.heartbeatTimer = undefined;
        this.reconnectTimer = undefined;
        const socket = this.socket;
        if (this.isConnected() && socket && !socket.destroyed && socket.writable) {
            try {
                await this.request('detach', options.preserveRoutes);
            } catch {
                // A crash is equivalent to a reload-sized gap; heartbeat expiry remains the safety bound.
            }
        }
        if (!options.preserveRoutes) this.identity.routes.clear();
        socket?.destroy();
        this.socket = undefined;
        this.readySocket = undefined;
        this.failPending(new Error('Telegram client closed'));
        this.rejectConnect?.(new Error('Telegram client closed'));
        this.clearConnectPromise();
    }

    private async rpc(method: BotRpcMethod, args: unknown[]): Promise<unknown> {
        if (this.closed) throw new Error('Telegram client closed');
        if (!this.started) this.start();
        const deadline = Date.now() + this.requestTimeoutMs;
        await this.ensureConnected(deadline);
        const remaining = deadline - Date.now();
        if (remaining <= 0) throw new Error('Telegram service request timed out');
        const requestId = this.nextRequestId();
        return await this.awaitResponse(requestId, {
            type: 'rpc', clientId: this.clientId, generation: this.generation, requestId, method, args,
        }, remaining);
    }

    private request(type: 'detach', preserveRoutes: boolean): Promise<unknown> {
        const requestId = this.nextRequestId();
        return this.awaitResponse(requestId, {
            type, clientId: this.clientId, generation: this.generation, requestId, preserveRoutes,
        }, Math.min(this.requestTimeoutMs, 1_000));
    }

    private awaitResponse(requestId: string, payload: ClientPayload, timeoutMs = this.requestTimeoutMs): Promise<unknown> {
        return new Promise((resolve, reject) => {
            const timer = setTimeout(() => {
                this.pending.delete(requestId);
                reject(new Error('Telegram service request timed out'));
            }, Math.max(1, timeoutMs));
            this.pending.set(requestId, { resolve, reject, timer });
            if (!this.write(payload)) {
                clearTimeout(timer);
                this.pending.delete(requestId);
                reject(new Error('Telegram service disconnected'));
            }
        });
    }

    private ensureConnected(deadline: number): Promise<void> {
        if (this.isConnected()) return Promise.resolve();
        if (!this.started) this.start();
        if (!this.connectPromise) {
            this.connectPromise = new Promise<void>((resolve, reject) => {
                this.resolveConnect = resolve;
                this.rejectConnect = reject;
            });
            const remaining = Math.max(1, deadline - Date.now());
            this.connectTimer = setTimeout(() => {
                const reject = this.rejectConnect;
                this.clearConnectPromise();
                reject?.(new Error('Telegram service connection timed out'));
            }, remaining);
        }
        return this.connectPromise;
    }

    private connect(): void {
        if (this.closed || this.socket) return;
        const socket = createConnection({ path: this.address });
        this.socket = socket;
        socket.once('connect', () => {
            this.write({ type: 'hello', clientId: this.clientId, generation: this.generation, poll: this.options.poll });
            this.writeHeartbeat();
            if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
            this.heartbeatTimer = setInterval(() => this.writeHeartbeat(), Math.max(1, this.heartbeatMs));
            this.heartbeatTimer.unref();
        });
        this.readFrames(socket);
        socket.on('error', (error) => {
            const code = errorCode(error);
            if (!this.closed && (code === 'ECONNREFUSED' || code === 'ENOENT')) this.maybeSpawnService();
            else if (!this.closed && !EXPECTED_ERRORS.has(code ?? '')) this.report(error);
        });
        socket.once('close', () => {
            if (this.socket === socket) this.socket = undefined;
            if (this.readySocket === socket) this.readySocket = undefined;
            if (this.heartbeatTimer) clearInterval(this.heartbeatTimer);
            this.heartbeatTimer = undefined;
            this.failPending(new Error('Telegram service disconnected'));
            if (!this.closed) this.scheduleReconnect();
        });
    }

    private maybeSpawnService(): void {
        const now = Date.now();
        if (now - this.lastSpawnAt < Math.max(1, this.spawnCooldownMs)) return;
        this.lastSpawnAt = now;
        try { (this.options.spawnService ?? this.spawnDetachedService)(this.options.token); }
        catch (error) { this.report(error); }
    }

    private readonly spawnDetachedService = (token: string): void => {
        const serverPath = this.options.serverPath ?? fileURLToPath(new URL('./server.ts', import.meta.url));
        const { PI_TG_TOKEN: _removedToken, ...sanitizedEnv } = process.env;
        const child = spawn(process.execPath, ['--experimental-strip-types', serverPath], {
            detached: true,
            env: sanitizedEnv,
            stdio: ['ignore', 'ignore', 'ignore', 'pipe'],
        });
        const tokenPipe = child.stdio[3];
        child.on('error', (error) => this.report(error));
        if (tokenPipe && 'write' in tokenPipe) {
            tokenPipe.write(`${token}\n`);
            tokenPipe.end();
            if ('unref' in tokenPipe && typeof tokenPipe.unref === 'function') tokenPipe.unref();
        }
        child.unref();
    };

    private scheduleReconnect(): void {
        if (this.closed || this.reconnectTimer) return;
        this.reconnectTimer = setTimeout(() => {
            this.reconnectTimer = undefined;
            this.connect();
        }, Math.max(1, this.reconnectMs));
        this.reconnectTimer.unref();
    }

    private writeHeartbeat(): void {
        this.write({ type: 'heartbeat', clientId: this.clientId, generation: this.generation, poll: this.options.poll });
    }

    private write(payload: ClientPayload): boolean {
        const socket = this.socket;
        if (!socket || socket.destroyed || !socket.writable) return false;
        const frame = encodeTelegramFrame(this.key, payload);
        return frame ? socket.write(frame) : false;
    }

    private readFrames(socket: Socket): void {
        let pending = Buffer.alloc(0);
        socket.on('data', (chunk: Buffer) => {
            pending = Buffer.concat([pending, chunk]);
            let newline = pending.indexOf(10);
            while (newline !== -1) {
                if (newline > MAX_FRAME_BYTES) { socket.destroy(); return; }
                const value = decodeTelegramFrame(this.key, pending.subarray(0, newline).toString('utf8'));
                pending = pending.subarray(newline + 1);
                if (!validServerPayload(value)) { socket.destroy(); return; }
                this.receive(socket, value);
                newline = pending.indexOf(10);
            }
            if (pending.length > MAX_FRAME_BYTES) socket.destroy();
        });
    }

    private receive(socket: Socket, payload: ServerPayload): void {
        if (payload.clientId !== this.clientId) return;
        if (payload.type === 'ready') {
            if (payload.generation !== this.generation || socket !== this.socket) return;
            this.readySocket = socket;
            this.lastSpawnAt = Number.NEGATIVE_INFINITY;
            this.replayRoutes();
            const resolve = this.resolveConnect;
            this.clearConnectPromise();
            resolve?.();
            return;
        }
        if (payload.type === 'response') {
            if (payload.generation !== this.generation || socket !== this.readySocket) return;
            const pending = this.pending.get(payload.requestId);
            if (!pending) return;
            this.pending.delete(payload.requestId);
            clearTimeout(pending.timer);
            if (payload.ok) pending.resolve(payload.result);
            else pending.reject(new Error(payload.error || 'Telegram service request failed'));
            return;
        }
        if (socket === this.readySocket) this.receiveEvent(payload);
    }

    private rememberRoute(chatId: string, messageId: number, expiresAt: number, send: boolean): void {
        const key = ownedRouteKey(chatId, messageId);
        this.identity.routes.delete(key);
        this.identity.routes.set(key, { chatId, messageId, expiresAt });
        this.pruneRoutes();
        while (this.identity.routes.size > this.maxOwnedRoutes) this.identity.routes.delete(this.identity.routes.keys().next().value!);
        if (send) this.writeRoute(this.identity.routes.get(key)!);
    }

    private replayRoutes(): void {
        this.pruneRoutes();
        for (const route of this.identity.routes.values()) this.writeRoute(route);
    }

    private pruneRoutes(): void {
        const now = Date.now();
        for (const [key, route] of this.identity.routes) if (route.expiresAt <= now) this.identity.routes.delete(key);
    }

    private writeRoute(route: OwnedRoute): void {
        this.write({
            type: 'route', clientId: this.clientId, generation: this.generation,
            chatId: route.chatId, messageId: route.messageId, expiresAt: route.expiresAt,
        });
    }

    private receiveEvent(payload: Extract<ServerPayload, { type: 'event' }>): void {
        const now = Date.now();
        for (const [id, expiresAt] of this.identity.seenEvents) if (expiresAt <= now) this.identity.seenEvents.delete(id);
        if (this.identity.seenEvents.has(payload.eventId)) { this.ack(payload.eventId); return; }
        const existing = this.identity.inFlightEvents.get(payload.eventId);
        if (existing) { void existing.then(() => this.ack(payload.eventId)); return; }
        const delivery = Promise.resolve().then(() => payload.event === 'reply'
            ? this.options.onReply(payload.text)
            : this.options.onCallback?.(payload.callback));
        this.identity.inFlightEvents.set(payload.eventId, delivery);
        void delivery.then(() => {
            this.identity.seenEvents.delete(payload.eventId);
            this.identity.seenEvents.set(payload.eventId, Date.now() + SEEN_EVENT_TTL_MS);
            while (this.identity.seenEvents.size > MAX_SEEN_EVENTS) this.identity.seenEvents.delete(this.identity.seenEvents.keys().next().value!);
            this.ack(payload.eventId);
        }, (error) => this.report(error)).finally(() => this.identity.inFlightEvents.delete(payload.eventId));
    }

    private ack(eventId: string): void {
        this.write({ type: 'ack', clientId: this.clientId, generation: this.generation, eventId });
    }

    private nextRequestId(): string { return `${this.generation}-${(++this.requestSequence).toString(36)}`; }

    private failPending(error: Error): void {
        for (const pending of this.pending.values()) { clearTimeout(pending.timer); pending.reject(error); }
        this.pending.clear();
    }

    private clearConnectPromise(): void {
        if (this.connectTimer) clearTimeout(this.connectTimer);
        this.connectTimer = undefined;
        this.connectPromise = undefined;
        this.resolveConnect = undefined;
        this.rejectConnect = undefined;
    }

    private report(error: unknown): void { this.options.onError?.(error); }
}

export type { RoutedTelegramCallback } from './server.ts';
