import { createHash, createHmac, randomBytes, timingSafeEqual } from 'node:crypto';
import { chmodSync, mkdirSync, readFileSync, renameSync, statSync, unlinkSync, writeFileSync } from 'node:fs';
import { createServer, type Server, type Socket } from 'node:net';
import { tmpdir, userInfo } from 'node:os';
import { dirname, join } from 'node:path';
import { pathToFileURL } from 'node:url';
import type TelegramBot from 'node-telegram-bot-api';

export const MAX_FRAME_BYTES = 64 * 1024;
const MAX_STRING_BYTES = 48 * 1024;
const MAX_JOURNAL_BYTES = 16 * 1024 * 1024;
const DEFAULT_HEARTBEAT_TIMEOUT_MS = 10_000;
const DEFAULT_CHECK_INTERVAL_MS = 1_000;
const DEFAULT_ROUTE_TTL_MS = 24 * 60 * 60 * 1_000;
const DEFAULT_OUTBOX_TTL_MS = 60_000;
const DEFAULT_MAX_ROUTES = 4_096;
const DEFAULT_MAX_OUTBOX = 512;
const DEFAULT_MAX_REQUEST_CACHE = 512;

type InlineKeyboardMarkup = { inline_keyboard: Array<Array<{ text: string; callback_data: string }>> };
type SendOptions = { parse_mode?: 'MarkdownV2'; reply_markup?: InlineKeyboardMarkup };
type EditOptions = { chat_id: string | number; message_id: number };
type AnswerOptions = { text?: string; show_alert?: boolean };

export interface TelegramBotService {
    options?: { polling?: unknown };
    sendMessage(chatId: string | number, text: string, options?: SendOptions): Promise<{ chat: { id: string | number }; message_id: number }>;
    editMessageReplyMarkup(markup: InlineKeyboardMarkup, options: EditOptions): Promise<unknown>;
    answerCallbackQuery(callbackQueryId: string, options?: AnswerOptions): Promise<unknown>;
    on(event: 'message' | 'callback_query' | 'polling_error', listener: (...args: any[]) => void): this;
    removeListener(event: 'message' | 'callback_query' | 'polling_error', listener: (...args: any[]) => void): this;
    startPolling(): Promise<unknown>;
    stopPolling(options?: { cancel?: boolean }): Promise<unknown>;
    isPolling(): boolean;
}

export type RoutedTelegramCallback = {
    callbackQueryId: string;
    data: string;
    userId: number;
    userIsBot: boolean;
    chatId: string;
    messageId: number;
};

type RpcMethod = 'sendMessage' | 'editMessageReplyMarkup' | 'answerCallbackQuery';
type ClientPayload =
    | { type: 'hello'; clientId: string; generation: string; poll: boolean }
    | { type: 'heartbeat'; clientId: string; generation: string; poll: boolean }
    | { type: 'route'; clientId: string; generation: string; chatId: string; messageId: number; expiresAt: number }
    | { type: 'ack'; clientId: string; generation: string; eventId: string }
    | { type: 'detach'; clientId: string; generation: string; requestId: string; preserveRoutes: boolean }
    | { type: 'rpc'; clientId: string; generation: string; requestId: string; method: RpcMethod; args: unknown[] };
type ServerPayload =
    | { type: 'ready'; clientId: string; generation: string }
    | { type: 'response'; clientId: string; generation: string; requestId: string; ok: true; result: unknown }
    | { type: 'response'; clientId: string; generation: string; requestId: string; ok: false; error: string }
    | { type: 'event'; clientId: string; eventId: string; event: 'reply'; text: string }
    | { type: 'event'; clientId: string; eventId: string; event: 'callback'; callback: RoutedTelegramCallback };
type Envelope = { payload: unknown; mac: string };

type ClientState = {
    socket?: Socket;
    generation: string;
    lastHeartbeat: number;
    poll: boolean;
};
type Route = { clientId: string; expiresAt: number };
type OutboxEvent = Extract<ServerPayload, { type: 'event' }> & { expiresAt: number };
type CachedResponse = { response: Extract<ServerPayload, { type: 'response' }>; expiresAt: number };
type Journal = {
    version: 1;
    routes: Array<[string, Route]>;
    outbox: Array<[string, OutboxEvent]>;
    callbackOwners: Array<[string, { clientId: string; expiresAt: number }]>;
};

const serviceDigest = (token: string): string =>
    createHash('sha256').update(`${userInfo().uid}:${token}`).digest('hex').slice(0, 32);

export const telegramServiceAddress = (token: string): string =>
    `\0pi-telegram-service-${userInfo().uid}-${serviceDigest(token)}`;

export const telegramServiceStatePath = (token: string): string =>
    join(tmpdir(), `pi-telegram-service-${userInfo().uid}-${serviceDigest(token)}.state.json`);

const hmacKey = (token: string): Buffer =>
    createHash('sha256').update(`pi-telegram-service-ipc:${token}`).digest();

const byteLengthWithin = (value: string, maximum = MAX_STRING_BYTES): boolean =>
    value.length > 0 && Buffer.byteLength(value, 'utf8') <= maximum;
const safeId = (value: unknown, maximum = 256): value is string =>
    typeof value === 'string' && byteLengthWithin(value, maximum);
const safeMessageId = (value: unknown): value is number => Number.isSafeInteger(value) && Number(value) >= 0;
const isRecord = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value);
const routeKey = (chatId: string, messageId: number): string => JSON.stringify([chatId, messageId]);
const errorCode = (error: unknown): string | undefined =>
    isRecord(error) && typeof error.code === 'string' ? error.code : undefined;
const publicError = (error: unknown): string => {
    if (error instanceof Error && new Set([
        'invalid request',
        'invalid Telegram response',
        'route ownership required',
        'callback ownership required',
        'service unavailable',
    ]).has(error.message)) return error.message;
    if (!isRecord(error)) return 'Telegram API request failed';
    const code = typeof error.code === 'string' ? error.code : undefined;
    const status = typeof error.response === 'object' && error.response && 'statusCode' in error.response
        ? Number((error.response as { statusCode?: unknown }).statusCode)
        : undefined;
    return [code, Number.isFinite(status) ? String(status) : undefined].filter(Boolean).join(':') || 'Telegram API request failed';
};

const validKeyboard = (value: unknown): value is InlineKeyboardMarkup => {
    if (!isRecord(value) || !Array.isArray(value.inline_keyboard) || value.inline_keyboard.length > 16) return false;
    return value.inline_keyboard.every((row) => Array.isArray(row) && row.length <= 8 && row.every((button) =>
        isRecord(button) && safeId(button.text, 512) && safeId(button.callback_data, 64)));
};

export const encodeTelegramFrame = (key: Buffer, payload: unknown): string | undefined => {
    const body = JSON.stringify(payload);
    const envelope: Envelope = { payload, mac: createHmac('sha256', key).update(body).digest('hex') };
    const frame = `${JSON.stringify(envelope)}\n`;
    return Buffer.byteLength(frame) <= MAX_FRAME_BYTES ? frame : undefined;
};

export const decodeTelegramFrame = (key: Buffer, frame: string): unknown => {
    try {
        const envelope = JSON.parse(frame) as Partial<Envelope>;
        if (!('payload' in envelope) || typeof envelope.mac !== 'string' || !/^[a-f0-9]{64}$/u.test(envelope.mac)) return undefined;
        const expected = createHmac('sha256', key).update(JSON.stringify(envelope.payload)).digest();
        const actual = Buffer.from(envelope.mac, 'hex');
        if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) return undefined;
        return envelope.payload;
    } catch {
        return undefined;
    }
};

const validClientPayload = (value: unknown): value is ClientPayload => {
    if (!isRecord(value) || !safeId(value.type, 32) || !safeId(value.clientId) || !safeId(value.generation)) return false;
    if (value.type === 'hello' || value.type === 'heartbeat') return typeof value.poll === 'boolean';
    if (value.type === 'ack') return safeId(value.eventId);
    if (value.type === 'detach') return safeId(value.requestId) && typeof value.preserveRoutes === 'boolean';
    if (value.type === 'route') return safeId(value.chatId) && safeMessageId(value.messageId)
        && Number.isSafeInteger(value.expiresAt) && Number(value.expiresAt) > 0;
    return value.type === 'rpc' && safeId(value.requestId) && Array.isArray(value.args) && value.args.length <= 3
        && (value.method === 'sendMessage' || value.method === 'editMessageReplyMarkup' || value.method === 'answerCallbackQuery');
};

const validJournalEvent = (value: unknown): value is OutboxEvent => {
    if (!isRecord(value) || value.type !== 'event' || !safeId(value.clientId) || !safeId(value.eventId)
        || !Number.isSafeInteger(value.expiresAt)) return false;
    if (value.event === 'reply') return typeof value.text === 'string' && byteLengthWithin(value.text);
    if (value.event !== 'callback' || !isRecord(value.callback)) return false;
    const callback = value.callback;
    return safeId(callback.callbackQueryId) && safeId(callback.data, 64) && Number.isSafeInteger(callback.userId)
        && callback.userIsBot === false && safeId(callback.chatId) && safeMessageId(callback.messageId);
};

export interface TelegramServiceOptions {
    token: string;
    address?: string;
    botFactory?: () => Promise<TelegramBotService> | TelegramBotService;
    heartbeatTimeoutMs?: number;
    checkIntervalMs?: number;
    routeTtlMs?: number;
    outboxTtlMs?: number;
    maxRoutes?: number;
    maxOutbox?: number;
    statePath?: string | false;
    now?: () => number;
    onError?: (error: unknown) => void;
}

/** Detached owner of Telegram polling, Bot API calls, routes, and replayable inbound events. */
export class TelegramService {
    readonly address: string;
    private readonly options: TelegramServiceOptions;
    private readonly key: Buffer;
    private readonly heartbeatTimeoutMs: number;
    private readonly checkIntervalMs: number;
    private readonly routeTtlMs: number;
    private readonly outboxTtlMs: number;
    private readonly maxRoutes: number;
    private readonly maxOutbox: number;
    private readonly statePath?: string;
    private readonly now: () => number;
    private readonly clients = new Map<string, ClientState>();
    private readonly routes = new Map<string, Route>();
    private readonly outbox = new Map<string, OutboxEvent>();
    private readonly callbackOwners = new Map<string, { clientId: string; expiresAt: number }>();
    private readonly requestCache = new Map<string, CachedResponse>();
    private readonly sockets = new Set<Socket>();
    private server?: Server;
    private bot?: TelegramBotService;
    private checkTimer?: ReturnType<typeof setInterval>;
    private polling = false;
    private ready = false;
    private pollTransition: Promise<void> = Promise.resolve();
    private startPromise?: Promise<'started' | 'address-in-use'>;
    private stopPromise?: Promise<void>;
    private stopping = false;
    private readonly eventNonce = randomBytes(12).toString('base64url');
    private nextEventId = 0;
    private lastValidHeartbeat: number;

    constructor(options: TelegramServiceOptions) {
        this.options = options;
        this.address = options.address ?? telegramServiceAddress(options.token);
        this.key = hmacKey(options.token);
        this.heartbeatTimeoutMs = options.heartbeatTimeoutMs ?? DEFAULT_HEARTBEAT_TIMEOUT_MS;
        this.checkIntervalMs = options.checkIntervalMs ?? DEFAULT_CHECK_INTERVAL_MS;
        this.routeTtlMs = options.routeTtlMs ?? DEFAULT_ROUTE_TTL_MS;
        this.outboxTtlMs = options.outboxTtlMs ?? DEFAULT_OUTBOX_TTL_MS;
        this.maxRoutes = options.maxRoutes ?? DEFAULT_MAX_ROUTES;
        this.maxOutbox = options.maxOutbox ?? DEFAULT_MAX_OUTBOX;
        this.statePath = options.statePath === false
            ? undefined
            : options.statePath ?? (options.botFactory ? undefined : telegramServiceStatePath(options.token));
        this.now = options.now ?? Date.now;
        this.lastValidHeartbeat = this.now();
    }

    start(): Promise<'started' | 'address-in-use'> {
        if (this.ready) return Promise.resolve('started');
        if (this.startPromise) return this.startPromise;
        this.startPromise = this.performStart().finally(() => { this.startPromise = undefined; });
        return this.startPromise;
    }

    private async performStart(): Promise<'started' | 'address-in-use'> {
        if (this.server || this.stopping) return 'started';
        const server = createServer((socket) => this.accept(socket));
        const result = await new Promise<'started' | 'address-in-use'>((resolve, reject) => {
            const onError = (error: Error): void => {
                if (errorCode(error) === 'EADDRINUSE') resolve('address-in-use');
                else reject(error);
            };
            server.once('error', onError);
            server.listen(this.address, () => {
                server.off('error', onError);
                server.on('error', (error) => this.report(error));
                resolve('started');
            });
        });
        if (result === 'address-in-use') {
            server.close();
            return result;
        }

        this.server = server;
        let bot: TelegramBotService | undefined;
        try {
            this.loadState();
            bot = await (this.options.botFactory?.() ?? this.createBot());
            this.bot = bot;
            this.attachBot(bot);
            this.ready = true;
            this.lastValidHeartbeat = this.now();
            this.checkTimer = setInterval(() => this.checkLiveness(), Math.max(1, this.checkIntervalMs));
            this.checkTimer.unref();
            for (const clientId of this.clients.keys()) this.activateClient(clientId);
            this.refreshPolling();
            return result;
        } catch (error) {
            this.ready = false;
            if (bot) {
                try { this.detachBot(bot); } catch { /* best effort for partially attached factories */ }
            }
            this.bot = undefined;
            for (const socket of this.sockets) socket.destroy();
            this.sockets.clear();
            this.clients.clear();
            if (this.server === server) this.server = undefined;
            await new Promise<void>((resolve) => server.close(() => resolve()));
            throw error;
        }
    }

    stop(): Promise<void> { return this.requestStop(false); }

    private requestStop(cleanIdle: boolean): Promise<void> {
        if (this.stopPromise) return this.stopPromise;
        const attempt = this.performStop(cleanIdle);
        this.stopPromise = attempt;
        void attempt.finally(() => {
            if (this.stopPromise === attempt) this.stopPromise = undefined;
        }).catch(() => undefined);
        return attempt;
    }

    private async performStop(cleanIdle: boolean): Promise<void> {
        this.stopping = true;
        try {
            await this.setPolling(false);
        } catch (error) {
            this.stopping = false;
            throw error;
        }
        if (this.checkTimer) clearInterval(this.checkTimer);
        this.checkTimer = undefined;
        const bot = this.bot;
        if (bot) this.detachBot(bot);
        this.bot = undefined;
        this.ready = false;
        for (const socket of this.sockets) socket.destroy();
        this.sockets.clear();
        const server = this.server;
        this.server = undefined;
        if (server) await new Promise<void>((resolve) => server.close(() => resolve()));
        if (cleanIdle) this.deleteJournal();
    }

    isPolling(): boolean { return this.polling; }
    clientCount(): number { return this.clients.size; }
    routeCount(): number { return this.routes.size; }
    outboxCount(): number { return this.outbox.size; }

    private async createBot(): Promise<TelegramBotService> {
        const module = await import('node-telegram-bot-api');
        const Bot = module.default as typeof TelegramBot;
        const bot = new Bot(this.options.token, { polling: false });
        bot.options.polling = { params: { timeout: 1 } };
        return bot as unknown as TelegramBotService;
    }

    private attachBot(bot: TelegramBotService): void {
        bot.on('message', this.handleMessage);
        bot.on('callback_query', this.handleCallback);
        bot.on('polling_error', this.handlePollingError);
    }

    private detachBot(bot: TelegramBotService): void {
        bot.removeListener('message', this.handleMessage);
        bot.removeListener('callback_query', this.handleCallback);
        bot.removeListener('polling_error', this.handlePollingError);
    }

    private readonly handleMessage = (message: any): void => {
        if (!message?.from || message.from.is_bot || typeof message.text !== 'string' || !message.text.trim()
            || !message.reply_to_message || !safeMessageId(message.reply_to_message.message_id)
            || !message.chat || !Number.isSafeInteger(message.chat.id)) return;
        const route = this.getRoute(String(message.chat.id), message.reply_to_message.message_id);
        if (route) this.enqueue(route.clientId, { event: 'reply', text: message.text });
    };

    private readonly handleCallback = (query: any): void => {
        const message = query?.message;
        if (!message || query?.from?.is_bot || !safeId(query?.id) || !safeId(query?.data, 64)
            || !Number.isSafeInteger(query.from.id) || !Number.isSafeInteger(message?.chat?.id)
            || !safeMessageId(message.message_id)) return;
        if (this.callbackOwners.has(query.id)) return;
        const route = this.getRoute(String(message.chat.id), message.message_id);
        if (!route) return;
        const callback: RoutedTelegramCallback = {
            callbackQueryId: query.id,
            data: query.data,
            userId: query.from.id,
            userIsBot: false,
            chatId: String(message.chat.id),
            messageId: message.message_id,
        };
        this.callbackOwners.set(query.id, { clientId: route.clientId, expiresAt: this.now() + this.outboxTtlMs });
        if (!this.enqueue(route.clientId, { event: 'callback', callback })) this.callbackOwners.delete(query.id);
    };

    private readonly handlePollingError = (error: unknown): void => this.report(error);

    private accept(socket: Socket): void {
        if (this.stopping) { socket.destroy(); return; }
        this.sockets.add(socket);
        let peer: { clientId: string; generation: string } | undefined;
        this.readFrames(socket, (value) => {
            if (!validClientPayload(value)) { socket.destroy(); return; }
            if (!peer) {
                if (value.type !== 'hello') { socket.destroy(); return; }
                peer = { clientId: value.clientId, generation: value.generation };
                const previous = this.clients.get(peer.clientId)?.socket;
                if (previous && previous !== socket) previous.destroy();
                const connectedAt = this.now();
                this.lastValidHeartbeat = connectedAt;
                this.clients.set(peer.clientId, {
                    socket,
                    generation: peer.generation,
                    lastHeartbeat: connectedAt,
                    poll: value.poll,
                });
                if (this.ready) this.activateClient(peer.clientId);
                return;
            }
            if (value.clientId !== peer.clientId || value.generation !== peer.generation || value.type === 'hello') {
                socket.destroy();
                return;
            }
            this.receive(socket, value);
        });
        socket.on('error', () => undefined);
        socket.once('close', () => {
            this.sockets.delete(socket);
            if (!peer) return;
            const state = this.clients.get(peer.clientId);
            if (state?.socket === socket && state.generation === peer.generation) state.socket = undefined;
            this.refreshPolling();
        });
    }

    private activateClient(clientId: string): void {
        const state = this.clients.get(clientId);
        const socket = state?.socket;
        if (!state || !socket) return;
        if (!this.write(socket, { type: 'ready', clientId, generation: state.generation })) return;
        this.replay(clientId);
        this.refreshPolling();
    }

    private receive(socket: Socket, payload: Exclude<ClientPayload, { type: 'hello' }>): void {
        const state = this.clients.get(payload.clientId);
        if (!state || state.socket !== socket || state.generation !== payload.generation) return;
        if (payload.type === 'heartbeat') {
            state.lastHeartbeat = this.now();
            this.lastValidHeartbeat = state.lastHeartbeat;
            state.poll = payload.poll;
            if (this.ready) this.refreshPolling();
            return;
        }
        if (!this.ready) { socket.destroy(); return; }
        if (payload.type === 'ack') {
            const event = this.outbox.get(payload.eventId);
            if (event?.clientId === payload.clientId) {
                this.outbox.delete(payload.eventId);
                try { this.persistState(); }
                catch (error) { this.outbox.set(payload.eventId, event); this.report(error); }
            }
            return;
        }
        if (payload.type === 'route') {
            try {
                if (!this.registerRoute(payload.clientId, payload.chatId, payload.messageId, payload.expiresAt)) socket.destroy();
            } catch (error) {
                this.report(error);
                socket.destroy();
            }
            return;
        }
        if (payload.type === 'detach') {
            try {
                if (!payload.preserveRoutes) this.removeClient(payload.clientId);
                else state.socket = undefined;
                this.respond(socket, payload.clientId, payload.generation, payload.requestId, true, true);
                socket.end();
                this.refreshPolling();
            } catch (error) {
                this.respond(socket, payload.clientId, payload.generation, payload.requestId, false, publicError(error));
            }
            return;
        }
        void this.handleRpc(socket, payload);
    }

    private async handleRpc(socket: Socket, payload: Extract<ClientPayload, { type: 'rpc' }>): Promise<void> {
        const cacheKey = `${payload.clientId}:${payload.requestId}`;
        const cached = this.requestCache.get(cacheKey);
        if (cached && cached.expiresAt > this.now()) { this.write(socket, cached.response); return; }
        try {
            const result = await this.callBot(payload);
            const response = this.response(payload.clientId, payload.generation, payload.requestId, true, result);
            this.cacheResponse(cacheKey, response);
            this.write(socket, response);
        } catch (error) {
            const response = this.response(payload.clientId, payload.generation, payload.requestId, false, publicError(error));
            this.cacheResponse(cacheKey, response);
            this.write(socket, response);
        }
    }

    private async callBot(payload: Extract<ClientPayload, { type: 'rpc' }>): Promise<unknown> {
        const bot = this.bot;
        if (!bot) throw new Error('service unavailable');
        if (payload.method === 'sendMessage') {
            const [chatId, text, options] = payload.args;
            if ((typeof chatId !== 'string' && typeof chatId !== 'number') || !safeId(String(chatId), 256)
                || !safeId(text) || (options != null && !this.validSendOptions(options))) throw new Error('invalid request');
            const sent = await bot.sendMessage(chatId, text, options == null ? undefined : options as SendOptions);
            if (!sent?.chat || !safeMessageId(sent.message_id)) throw new Error('invalid Telegram response');
            if (!this.registerRoute(payload.clientId, String(sent.chat.id), sent.message_id, this.now() + this.routeTtlMs)) {
                throw new Error('route ownership required');
            }
            return { chat: { id: sent.chat.id }, message_id: sent.message_id };
        }
        if (payload.method === 'editMessageReplyMarkup') {
            const [markup, options] = payload.args;
            if (!validKeyboard(markup) || !this.validEditOptions(options)) throw new Error('invalid request');
            const key = routeKey(String(options.chat_id), options.message_id);
            if (this.routes.get(key)?.clientId !== payload.clientId) throw new Error('route ownership required');
            return Boolean(await bot.editMessageReplyMarkup(markup, options));
        }
        const [callbackQueryId, options] = payload.args;
        if (!safeId(callbackQueryId) || (options != null && !this.validAnswerOptions(options))) throw new Error('invalid request');
        if (this.callbackOwners.get(callbackQueryId)?.clientId !== payload.clientId) throw new Error('callback ownership required');
        return Boolean(await bot.answerCallbackQuery(callbackQueryId, options == null ? undefined : options as AnswerOptions));
    }

    private validSendOptions(value: unknown): boolean {
        if (!isRecord(value)) return false;
        if (value.parse_mode !== undefined && value.parse_mode !== 'MarkdownV2') return false;
        return value.reply_markup === undefined || validKeyboard(value.reply_markup);
    }

    private validEditOptions(value: unknown): value is EditOptions {
        return isRecord(value) && (typeof value.chat_id === 'string' || typeof value.chat_id === 'number')
            && safeId(String(value.chat_id), 256) && safeMessageId(value.message_id);
    }

    private validAnswerOptions(value: unknown): value is AnswerOptions {
        return isRecord(value) && (value.text === undefined || (typeof value.text === 'string' && Buffer.byteLength(value.text) <= 512))
            && (value.show_alert === undefined || typeof value.show_alert === 'boolean');
    }

    private registerRoute(clientId: string, chatId: string, messageId: number, requestedExpiry: number): boolean {
        const now = this.now();
        const expiresAt = Math.min(requestedExpiry, now + this.routeTtlMs);
        if (expiresAt <= now) return true;
        const key = routeKey(chatId, messageId);
        const previous = this.routes.get(key);
        if (previous && previous.expiresAt > now && previous.clientId !== clientId) return false;
        this.routes.delete(key);
        this.routes.set(key, { clientId, expiresAt });
        const evicted: Array<[string, Route]> = [];
        while (this.routes.size > this.maxRoutes) {
            const oldestKey = this.routes.keys().next().value!;
            evicted.push([oldestKey, this.routes.get(oldestKey)!]);
            this.routes.delete(oldestKey);
        }
        try { this.persistState(); }
        catch (error) {
            this.routes.delete(key);
            if (previous) this.routes.set(key, previous);
            for (const [evictedKey, route] of evicted) this.routes.set(evictedKey, route);
            throw error;
        }
        return true;
    }

    private getRoute(chatId: string, messageId: number): Route | undefined {
        const key = routeKey(chatId, messageId);
        const route = this.routes.get(key);
        if (!route || route.expiresAt <= this.now()) {
            if (route) {
                this.routes.delete(key);
                try { this.persistState(); } catch (error) { this.report(error); }
            }
            return undefined;
        }
        return route;
    }

    private enqueue(clientId: string, event: { event: 'reply'; text: string } | { event: 'callback'; callback: RoutedTelegramCallback }): boolean {
        const eventId = `${this.eventNonce}-${this.now().toString(36)}-${(++this.nextEventId).toString(36)}`;
        const item = { type: 'event', clientId, eventId, ...event, expiresAt: this.now() + this.outboxTtlMs } as OutboxEvent;
        this.outbox.set(eventId, item);
        while (this.outbox.size > this.maxOutbox) this.outbox.delete(this.outbox.keys().next().value!);
        try { this.persistState(); }
        catch (error) {
            this.outbox.delete(eventId);
            this.report(error);
            return false;
        }
        const socket = this.clients.get(clientId)?.socket;
        if (socket) this.write(socket, item);
        return true;
    }

    private replay(clientId: string): void {
        this.prune();
        const socket = this.clients.get(clientId)?.socket;
        if (!socket) return;
        for (const event of this.outbox.values()) if (event.clientId === clientId) this.write(socket, event);
    }

    private removeClient(clientId: string): void {
        this.clients.delete(clientId);
        for (const [key, route] of this.routes) if (route.clientId === clientId) this.routes.delete(key);
        for (const [id, event] of this.outbox) if (event.clientId === clientId) this.outbox.delete(id);
        for (const [id, owner] of this.callbackOwners) if (owner.clientId === clientId) this.callbackOwners.delete(id);
        this.persistState();
    }

    private checkLiveness(): void {
        const now = this.now();
        for (const [clientId, state] of this.clients) {
            if (now - state.lastHeartbeat >= this.heartbeatTimeoutMs) {
                try { this.removeClient(clientId); } catch (error) { this.report(error); }
            }
        }
        this.prune();
        this.refreshPolling();
        if (this.clients.size === 0 && now - this.lastValidHeartbeat >= this.heartbeatTimeoutMs) {
            void this.requestStop(true).catch((error) => this.report(error));
        }
    }

    private prune(): void {
        const now = this.now();
        let changed = false;
        for (const [key, route] of this.routes) if (route.expiresAt <= now) { this.routes.delete(key); changed = true; }
        for (const [id, event] of this.outbox) if (event.expiresAt <= now) { this.outbox.delete(id); changed = true; }
        for (const [id, owner] of this.callbackOwners) if (owner.expiresAt <= now) { this.callbackOwners.delete(id); changed = true; }
        for (const [id, cached] of this.requestCache) if (cached.expiresAt <= now) this.requestCache.delete(id);
        if (changed) {
            try { this.persistState(); } catch (error) { this.report(error); }
        }
    }

    private refreshPolling(): void {
        if (!this.ready || this.stopping) return;
        const now = this.now();
        const demanded = [...this.clients.values()].some((client) => client.poll && now - client.lastHeartbeat < this.heartbeatTimeoutMs);
        void this.setPolling(demanded).catch((error) => this.report(error));
    }

    private setPolling(enabled: boolean): Promise<void> {
        const transition = this.pollTransition.catch(() => undefined).then(async () => {
            const bot = this.bot;
            if (!bot) return;
            const actuallyPolling = bot.isPolling();
            this.polling = actuallyPolling;
            if (enabled === actuallyPolling) return;
            if (enabled) {
                await bot.startPolling();
                if (!bot.isPolling()) throw new Error('Telegram polling did not start');
                this.polling = true;
                return;
            }
            try {
                await bot.stopPolling({ cancel: true });
            } catch (error) {
                if (bot.isPolling()) {
                    this.polling = true;
                    throw error;
                }
            }
            if (bot.isPolling()) {
                this.polling = true;
                throw new Error('Telegram polling did not stop');
            }
            this.polling = false;
        });
        this.pollTransition = transition;
        return transition;
    }

    private response(clientId: string, generation: string, requestId: string, ok: boolean, value: unknown): Extract<ServerPayload, { type: 'response' }> {
        return ok
            ? { type: 'response', clientId, generation, requestId, ok: true, result: value }
            : { type: 'response', clientId, generation, requestId, ok: false, error: String(value) };
    }

    private respond(socket: Socket, clientId: string, generation: string, requestId: string, ok: boolean, value: unknown): void {
        this.write(socket, this.response(clientId, generation, requestId, ok, value));
    }

    private cacheResponse(key: string, response: Extract<ServerPayload, { type: 'response' }>): void {
        this.requestCache.delete(key);
        this.requestCache.set(key, { response, expiresAt: this.now() + this.outboxTtlMs });
        while (this.requestCache.size > DEFAULT_MAX_REQUEST_CACHE) this.requestCache.delete(this.requestCache.keys().next().value!);
    }

    private write(socket: Socket, payload: ServerPayload | OutboxEvent): boolean {
        if (socket.destroyed || !socket.writable) return false;
        const { expiresAt: _expiresAt, ...wirePayload } = payload as OutboxEvent;
        const frame = encodeTelegramFrame(this.key, wirePayload);
        return frame ? socket.write(frame) : false;
    }

    private readFrames(socket: Socket, receive: (payload: unknown) => void): void {
        let pending = Buffer.alloc(0);
        socket.on('data', (chunk: Buffer) => {
            pending = Buffer.concat([pending, chunk]);
            let newline = pending.indexOf(10);
            while (newline !== -1) {
                if (newline > MAX_FRAME_BYTES) { socket.destroy(); return; }
                const value = decodeTelegramFrame(this.key, pending.subarray(0, newline).toString('utf8'));
                pending = pending.subarray(newline + 1);
                if (value === undefined) { socket.destroy(); return; }
                receive(value);
                newline = pending.indexOf(10);
            }
            if (pending.length > MAX_FRAME_BYTES) socket.destroy();
        });
    }

    private loadState(): void {
        if (!this.statePath) return;
        try {
            if (statSync(this.statePath).size > MAX_JOURNAL_BYTES) throw new Error('Telegram state journal is too large');
            const parsed: unknown = JSON.parse(readFileSync(this.statePath, 'utf8'));
            if (!isRecord(parsed) || parsed.version !== 1 || !Array.isArray(parsed.routes)
                || !Array.isArray(parsed.outbox) || !Array.isArray(parsed.callbackOwners)) {
                throw new Error('Invalid Telegram state journal');
            }
            const now = this.now();
            for (const entry of parsed.routes.slice(-this.maxRoutes)) {
                if (!Array.isArray(entry) || entry.length !== 2 || !safeId(entry[0], 1024) || !isRecord(entry[1])
                    || !safeId(entry[1].clientId) || !Number.isSafeInteger(entry[1].expiresAt) || Number(entry[1].expiresAt) <= now) continue;
                this.routes.set(entry[0], { clientId: entry[1].clientId, expiresAt: Number(entry[1].expiresAt) });
            }
            for (const entry of parsed.outbox.slice(-this.maxOutbox)) {
                if (!Array.isArray(entry) || entry.length !== 2 || entry[0] !== (isRecord(entry[1]) ? entry[1].eventId : undefined)
                    || !validJournalEvent(entry[1]) || entry[1].expiresAt <= now) continue;
                this.outbox.set(entry[0], entry[1]);
            }
            for (const entry of parsed.callbackOwners.slice(-this.maxOutbox)) {
                if (!Array.isArray(entry) || entry.length !== 2 || !safeId(entry[0]) || !isRecord(entry[1])
                    || !safeId(entry[1].clientId) || !Number.isSafeInteger(entry[1].expiresAt) || Number(entry[1].expiresAt) <= now) continue;
                this.callbackOwners.set(entry[0], { clientId: entry[1].clientId, expiresAt: Number(entry[1].expiresAt) });
            }
            this.persistState();
        } catch (error) {
            if (errorCode(error) !== 'ENOENT') throw error;
        }
    }

    private persistState(): void {
        if (!this.statePath) return;
        const state: Journal = {
            version: 1,
            routes: [...this.routes],
            outbox: [...this.outbox],
            callbackOwners: [...this.callbackOwners],
        };
        mkdirSync(dirname(this.statePath), { recursive: true, mode: 0o700 });
        const temporary = `${this.statePath}.${process.pid}.${randomBytes(6).toString('hex')}.tmp`;
        try {
            writeFileSync(temporary, JSON.stringify(state), { encoding: 'utf8', mode: 0o600, flag: 'wx' });
            chmodSync(temporary, 0o600);
            renameSync(temporary, this.statePath);
            chmodSync(this.statePath, 0o600);
        } catch (error) {
            try { unlinkSync(temporary); } catch { /* ignore temporary cleanup errors */ }
            throw error;
        }
    }

    private deleteJournal(): void {
        if (!this.statePath) return;
        try { unlinkSync(this.statePath); }
        catch (error) { if (errorCode(error) !== 'ENOENT') this.report(error); }
    }

    private report(error: unknown): void { this.options.onError?.(error); }
}

const readTokenFromParent = async (): Promise<string | undefined> => {
    const chunks: Buffer[] = [];
    let size = 0;
    try {
        const stream = (await import('node:fs')).createReadStream('', { fd: 3, autoClose: true });
        for await (const chunk of stream) {
            const item = Buffer.from(chunk);
            size += item.length;
            if (size > 16 * 1024) return undefined;
            chunks.push(item);
        }
        const token = Buffer.concat(chunks).toString('utf8').replace(/\n$/u, '');
        return token && !token.includes('\n') ? token : undefined;
    } catch {
        return undefined;
    }
};

const runDetachedService = async (): Promise<void> => {
    const token = await readTokenFromParent();
    if (!token) return;
    const service = new TelegramService({ token });
    const result = await service.start();
    if (result === 'address-in-use') return;
};

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
    void runDetachedService().catch(() => { process.exitCode = 1; });
}
