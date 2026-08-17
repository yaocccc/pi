import { createHash, createHmac, randomBytes, timingSafeEqual } from "node:crypto";
import { createConnection, createServer, type Server, type Socket } from "node:net";
import { userInfo } from "node:os";

const MAX_FRAME_BYTES = 64 * 1024;
const DEFAULT_ROUTE_TTL_MS = 24 * 60 * 60 * 1_000;
const DEFAULT_MAX_ROUTES = 4_096;
const DEFAULT_PENDING_REPLY_TTL_MS = 5_000;
const DEFAULT_MAX_PENDING_REPLIES = 256;
const DEFAULT_PENDING_CALLBACK_TTL_MS = 5_000;
const DEFAULT_MAX_PENDING_CALLBACKS = 256;
const DEFAULT_CALLBACK_DEDUPE_TTL_MS = 60_000;
const DEFAULT_MAX_CALLBACK_DEDUPE = 2_048;
const RETRY_MS = 25;
const EXPECTED_CLIENT_IPC_ERROR_CODES = new Set(["EPIPE", "ECONNRESET", "ECONNREFUSED", "ENOENT"]);

type RoutePayload = {
    type: "route";
    instanceId: string;
    generation: string;
    chatId: string;
    messageId: number;
    expiresAt: number;
};

type HelloPayload = {
    type: "hello";
    instanceId: string;
    generation: string;
};

type DeliverPayload = {
    type: "deliver";
    instanceId: string;
    generation: string;
    text: string;
};

export type RoutedTelegramCallback = {
    callbackQueryId: string;
    data: string;
    userId: number;
    userIsBot: boolean;
    chatId: string;
    messageId: number;
};

type DeliverCallbackPayload = {
    type: "deliver_callback";
    instanceId: string;
    generation: string;
    callback: RoutedTelegramCallback;
};

type Payload = RoutePayload | HelloPayload | DeliverPayload | DeliverCallbackPayload;
type Envelope = { payload: Payload; mac: string };

type OwnedRoute = {
    chatId: string;
    messageId: number;
    expiresAt: number;
};

type Peer = {
    instanceId: string;
    generation: string;
};

type LeaderRoute = Peer & {
    socket: Socket;
    expiresAt: number;
};

type PendingReply = {
    text: string;
    expiresAt: number;
};

type PendingCallback = {
    key: string;
    callback: RoutedTelegramCallback;
    expiresAt: number;
};

export type TelegramCoordinatorOptions = {
    token: string;
    onLeaderStart: () => void | Promise<void>;
    onLeaderStop: () => void | Promise<void>;
    onReply: (text: string) => void | Promise<void>;
    onCallback?: (callback: RoutedTelegramCallback) => void | Promise<void>;
    onError?: (error: unknown) => void;
    routeTtlMs?: number;
    maxRoutes?: number;
    pendingReplyTtlMs?: number;
    maxPendingReplies?: number;
    pendingCallbackTtlMs?: number;
    maxPendingCallbacks?: number;
    callbackDedupeTtlMs?: number;
    maxCallbackDedupe?: number;
    instanceId?: string;
    generation?: string;
};

export const telegramCoordinatorAddress = (token: string): string => {
    const uid = userInfo().uid;
    const digest = createHash("sha256").update(`${uid}:${token}`).digest("hex").slice(0, 32);
    return `\0pi-telegram-${digest}`;
};

const routeKey = (chatId: string, messageId: number): string => JSON.stringify([chatId, messageId]);

const errorCode = (error: unknown): string | undefined =>
    error && typeof error === "object" && "code" in error
        ? String((error as { code?: unknown }).code)
        : undefined;

/** Coordinates one Telegram poller and reply routes across local Pi processes. */
export class TelegramCoordinator {
    readonly instanceId: string;
    readonly generation: string;

    private readonly options: TelegramCoordinatorOptions;
    private readonly address: string;
    private readonly hmacKey: Buffer;
    private readonly routeTtlMs: number;
    private readonly maxRoutes: number;
    private readonly pendingReplyTtlMs: number;
    private readonly maxPendingReplies: number;
    private readonly pendingCallbackTtlMs: number;
    private readonly maxPendingCallbacks: number;
    private readonly callbackDedupeTtlMs: number;
    private readonly maxCallbackDedupe: number;
    private readonly ownedRoutes = new Map<string, OwnedRoute>();
    private readonly leaderRoutes = new Map<string, LeaderRoute>();
    private readonly pendingReplies = new Map<string, PendingReply>();
    private readonly pendingCallbacks: PendingCallback[] = [];
    private readonly seenCallbackIds = new Map<string, number>();
    private readonly receivedCallbackIds = new Map<string, number>();
    private readonly serverSockets = new Set<Socket>();
    private readonly peerSockets = new Map<string, Socket>();
    private server?: Server;
    private candidateServer?: Server;
    private client?: Socket;
    private retryTimer?: ReturnType<typeof setTimeout>;
    private started = false;
    private closed = false;
    private leader = false;

    constructor(options: TelegramCoordinatorOptions) {
        this.options = options;
        this.address = telegramCoordinatorAddress(options.token);
        this.hmacKey = createHash("sha256").update(`pi-telegram-ipc:${options.token}`).digest();
        this.routeTtlMs = options.routeTtlMs ?? DEFAULT_ROUTE_TTL_MS;
        this.maxRoutes = options.maxRoutes ?? DEFAULT_MAX_ROUTES;
        this.pendingReplyTtlMs = options.pendingReplyTtlMs ?? DEFAULT_PENDING_REPLY_TTL_MS;
        this.maxPendingReplies = options.maxPendingReplies ?? DEFAULT_MAX_PENDING_REPLIES;
        this.pendingCallbackTtlMs = options.pendingCallbackTtlMs ?? DEFAULT_PENDING_CALLBACK_TTL_MS;
        this.maxPendingCallbacks = options.maxPendingCallbacks ?? DEFAULT_MAX_PENDING_CALLBACKS;
        this.callbackDedupeTtlMs = options.callbackDedupeTtlMs ?? DEFAULT_CALLBACK_DEDUPE_TTL_MS;
        this.maxCallbackDedupe = options.maxCallbackDedupe ?? DEFAULT_MAX_CALLBACK_DEDUPE;
        this.instanceId = options.instanceId ?? `${process.pid}-${randomBytes(8).toString("hex")}`;
        this.generation = options.generation ?? randomBytes(16).toString("hex");
    }

    start(): void {
        if (this.started || this.closed) return;
        this.started = true;
        this.tryLeadership();
    }

    registerRoute(chatId: string | number, messageId: number): void {
        if (this.closed || !Number.isSafeInteger(messageId)) return;
        this.pruneOwnedRoutes();
        const route: OwnedRoute = {
            chatId: String(chatId),
            messageId,
            expiresAt: Date.now() + this.routeTtlMs,
        };
        const key = routeKey(route.chatId, route.messageId);
        this.ownedRoutes.delete(key);
        this.ownedRoutes.set(key, route);
        while (this.ownedRoutes.size > this.maxRoutes) {
            const oldest = this.ownedRoutes.keys().next().value as string | undefined;
            if (oldest === undefined) break;
            this.ownedRoutes.delete(oldest);
        }
        this.sendRoute(route);
    }

    /** Called only by the elected poller after validating a Telegram reply. */
    dispatchReply(chatId: string | number, messageId: number, text: string): boolean {
        if (!this.leader || this.closed || !Number.isSafeInteger(messageId)) return false;
        this.pruneLeaderRoutes();
        this.prunePendingReplies();
        const key = routeKey(String(chatId), messageId);
        const route = this.leaderRoutes.get(key);
        if (!route || route.expiresAt <= Date.now() || route.socket.destroyed) {
            this.leaderRoutes.delete(key);
            this.pendingReplies.delete(key);
            this.pendingReplies.set(key, {
                text,
                expiresAt: Date.now() + this.pendingReplyTtlMs,
            });
            while (this.pendingReplies.size > this.maxPendingReplies) {
                const oldest = this.pendingReplies.keys().next().value as string | undefined;
                if (oldest === undefined) break;
                this.pendingReplies.delete(oldest);
            }
            return false;
        }
        return this.write(route.socket, {
            type: "deliver",
            instanceId: route.instanceId,
            generation: route.generation,
            text,
        });
    }

    /** Called only by the elected poller for callback queries with an actual message. */
    dispatchCallback(
        chatId: string | number,
        messageId: number,
        callback: Omit<RoutedTelegramCallback, "chatId" | "messageId">,
    ): boolean {
        if (!this.leader
            || this.closed
            || !Number.isSafeInteger(messageId)
            || !callback.callbackQueryId
            || !callback.data
            || !Number.isSafeInteger(callback.userId)
            || callback.userIsBot) return false;

        this.pruneLeaderRoutes();
        this.prunePendingCallbacks();
        const now = Date.now();
        if (this.seenCallbackIds.has(callback.callbackQueryId)) return false;
        this.seenCallbackIds.set(callback.callbackQueryId, now + this.callbackDedupeTtlMs);
        while (this.seenCallbackIds.size > this.maxCallbackDedupe) {
            const oldest = this.seenCallbackIds.keys().next().value as string | undefined;
            if (oldest === undefined) break;
            this.seenCallbackIds.delete(oldest);
        }

        const routedCallback: RoutedTelegramCallback = {
            ...callback,
            chatId: String(chatId),
            messageId,
        };
        const key = routeKey(routedCallback.chatId, messageId);
        const route = this.leaderRoutes.get(key);
        if (route && route.expiresAt > now && !route.socket.destroyed) {
            return this.deliverCallback(route, routedCallback);
        }

        this.leaderRoutes.delete(key);
        this.pendingCallbacks.push({
            key,
            callback: routedCallback,
            expiresAt: now + this.pendingCallbackTtlMs,
        });
        while (this.pendingCallbacks.length > this.maxPendingCallbacks) this.pendingCallbacks.shift();
        return false;
    }

    isLeader(): boolean {
        return this.leader;
    }

    isRunning(): boolean {
        return this.started && !this.closed;
    }

    async shutdown(): Promise<void> {
        if (this.closed) return;
        this.closed = true;
        if (this.retryTimer) clearTimeout(this.retryTimer);
        this.retryTimer = undefined;

        this.client?.destroy();
        this.client = undefined;
        this.candidateServer?.close();
        this.candidateServer = undefined;
        for (const socket of this.serverSockets) socket.destroy();
        this.serverSockets.clear();

        const server = this.server;
        const stopLeader = this.leader;
        this.leader = false;
        if (stopLeader) {
            try {
                await this.options.onLeaderStop();
            } catch (error) {
                this.report(error);
            }
        }
        if (server) {
            await new Promise<void>((resolve) => server.close(() => resolve()));
            if (this.server === server) this.server = undefined;
        }
        this.ownedRoutes.clear();
        this.leaderRoutes.clear();
        this.pendingReplies.clear();
        this.pendingCallbacks.length = 0;
        this.seenCallbackIds.clear();
        this.receivedCallbackIds.clear();
        this.peerSockets.clear();
    }

    private tryLeadership(): void {
        if (this.closed || this.candidateServer || this.server) return;
        const server = createServer((socket) => this.accept(socket));
        this.candidateServer = server;
        server.on("error", (error) => {
            const wasCandidate = this.candidateServer === server;
            if (wasCandidate) this.candidateServer = undefined;
            if (this.closed) return;
            if (wasCandidate && errorCode(error) === "EADDRINUSE") {
                this.schedule(() => this.connectToLeader());
            } else {
                this.report(error);
                if (wasCandidate) this.schedule(() => this.tryLeadership());
            }
        });
        server.listen(this.address, () => {
            if (this.candidateServer === server) this.candidateServer = undefined;
            if (this.closed) {
                server.close();
                return;
            }
            this.server = server;
            this.leader = true;
            void Promise.resolve()
                .then(() => this.options.onLeaderStart())
                .catch((error) => this.report(error));
            this.connectToLeader();
        });
    }

    private connectToLeader(): void {
        if (this.closed || this.client) return;
        const socket = createConnection({ path: this.address });
        this.client = socket;
        socket.once("connect", () => {
            this.write(socket, {
                type: "hello",
                instanceId: this.instanceId,
                generation: this.generation,
            });
            this.pruneOwnedRoutes();
            for (const route of this.ownedRoutes.values()) this.sendRoute(route);
        });
        this.readFrames(socket, (payload) => this.receiveFromLeader(payload));
        socket.on("error", (error) => {
            if (this.closed || EXPECTED_CLIENT_IPC_ERROR_CODES.has(errorCode(error) ?? "")) return;
            this.report(error);
        });
        socket.once("close", () => {
            if (this.client === socket) this.client = undefined;
            if (this.closed) return;
            if (this.leader) this.schedule(() => this.connectToLeader());
            else this.schedule(() => this.tryLeadership());
        });
    }

    private accept(socket: Socket): void {
        if (this.closed) {
            socket.on("error", () => undefined);
            socket.destroy();
            return;
        }
        this.serverSockets.add(socket);
        let peer: Peer | undefined;
        this.readFrames(socket, (payload) => {
            if (!peer) {
                if (payload.type !== "hello") {
                    socket.destroy();
                    return;
                }
                peer = { instanceId: payload.instanceId, generation: payload.generation };
                const peerKey = `${peer.instanceId}:${peer.generation}`;
                const previous = this.peerSockets.get(peerKey);
                if (previous && previous !== socket) previous.destroy();
                this.peerSockets.set(peerKey, socket);
                return;
            }
            if (payload.type !== "route"
                || payload.instanceId !== peer.instanceId
                || payload.generation !== peer.generation
                || payload.expiresAt <= Date.now()) return;
            this.pruneLeaderRoutes();
            const key = routeKey(payload.chatId, payload.messageId);
            const route = { ...peer, socket, expiresAt: payload.expiresAt };
            this.leaderRoutes.delete(key);
            this.leaderRoutes.set(key, route);
            this.deliverPendingReply(key, route);
            this.deliverPendingCallbacks(key, route);
            while (this.leaderRoutes.size > this.maxRoutes) {
                const oldest = this.leaderRoutes.keys().next().value as string | undefined;
                if (oldest === undefined) break;
                this.leaderRoutes.delete(oldest);
            }
        });
        socket.on("error", () => undefined);
        socket.once("close", () => {
            this.serverSockets.delete(socket);
            if (peer) {
                const peerKey = `${peer.instanceId}:${peer.generation}`;
                if (this.peerSockets.get(peerKey) === socket) this.peerSockets.delete(peerKey);
            }
            for (const [key, route] of this.leaderRoutes) {
                if (route.socket === socket) this.leaderRoutes.delete(key);
            }
        });
    }

    private receiveFromLeader(payload: Payload): void {
        if (payload.instanceId !== this.instanceId || payload.generation !== this.generation) return;
        if (payload.type === "deliver") {
            if (!payload.text.trim()) return;
            void Promise.resolve()
                .then(() => this.options.onReply(payload.text))
                .catch((error) => this.report(error));
            return;
        }
        if (payload.type !== "deliver_callback" || !this.options.onCallback) return;
        const callback = payload.callback;
        if (!callback
            || callback.userIsBot
            || !callback.callbackQueryId
            || !callback.data
            || !callback.chatId
            || !Number.isSafeInteger(callback.messageId)
            || !Number.isSafeInteger(callback.userId)) return;
        const now = Date.now();
        for (const [callbackQueryId, expiresAt] of this.receivedCallbackIds) {
            if (expiresAt <= now) this.receivedCallbackIds.delete(callbackQueryId);
        }
        if (this.receivedCallbackIds.has(callback.callbackQueryId)) return;
        this.receivedCallbackIds.set(callback.callbackQueryId, now + this.callbackDedupeTtlMs);
        while (this.receivedCallbackIds.size > this.maxCallbackDedupe) {
            const oldest = this.receivedCallbackIds.keys().next().value as string | undefined;
            if (oldest === undefined) break;
            this.receivedCallbackIds.delete(oldest);
        }
        void Promise.resolve()
            .then(() => this.options.onCallback!(callback))
            .catch((error) => this.report(error));
    }

    private sendRoute(route: OwnedRoute): void {
        const socket = this.client;
        if (!socket || socket.destroyed || route.expiresAt <= Date.now()) return;
        this.write(socket, {
            type: "route",
            instanceId: this.instanceId,
            generation: this.generation,
            chatId: route.chatId,
            messageId: route.messageId,
            expiresAt: route.expiresAt,
        });
    }

    private write(socket: Socket, payload: Payload): boolean {
        const body = JSON.stringify(payload);
        const envelope: Envelope = {
            payload,
            mac: createHmac("sha256", this.hmacKey).update(body).digest("hex"),
        };
        const frame = `${JSON.stringify(envelope)}\n`;
        if (Buffer.byteLength(frame) > MAX_FRAME_BYTES) return false;
        return socket.write(frame);
    }

    private readFrames(socket: Socket, receive: (payload: Payload) => void): void {
        let pending = Buffer.alloc(0);
        socket.on("data", (chunk: Buffer) => {
            pending = Buffer.concat([pending, chunk]);
            let newline = pending.indexOf(10);
            while (newline !== -1) {
                if (newline > MAX_FRAME_BYTES) {
                    socket.destroy();
                    return;
                }
                const frame = pending.subarray(0, newline).toString("utf8");
                pending = pending.subarray(newline + 1);
                const payload = this.authenticate(frame);
                if (!payload) {
                    socket.destroy();
                    return;
                }
                receive(payload);
                newline = pending.indexOf(10);
            }
            if (pending.length > MAX_FRAME_BYTES) socket.destroy();
        });
    }

    private authenticate(frame: string): Payload | undefined {
        try {
            const envelope = JSON.parse(frame) as Partial<Envelope>;
            if (!envelope.payload || typeof envelope.mac !== "string") return undefined;
            const body = JSON.stringify(envelope.payload);
            const expected = createHmac("sha256", this.hmacKey).update(body).digest();
            const actual = Buffer.from(envelope.mac, "hex");
            if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) return undefined;
            return envelope.payload;
        } catch {
            return undefined;
        }
    }

    private pruneOwnedRoutes(): void {
        const now = Date.now();
        for (const [key, route] of this.ownedRoutes) {
            if (route.expiresAt <= now) this.ownedRoutes.delete(key);
        }
    }

    private pruneLeaderRoutes(): void {
        const now = Date.now();
        for (const [key, route] of this.leaderRoutes) {
            if (route.expiresAt <= now || route.socket.destroyed) this.leaderRoutes.delete(key);
        }
    }

    private prunePendingReplies(): void {
        const now = Date.now();
        for (const [key, reply] of this.pendingReplies) {
            if (reply.expiresAt <= now) this.pendingReplies.delete(key);
        }
    }

    private prunePendingCallbacks(): void {
        const now = Date.now();
        for (let index = this.pendingCallbacks.length - 1; index >= 0; index -= 1) {
            if (this.pendingCallbacks[index]!.expiresAt <= now) this.pendingCallbacks.splice(index, 1);
        }
        for (const [callbackQueryId, expiresAt] of this.seenCallbackIds) {
            if (expiresAt <= now) this.seenCallbackIds.delete(callbackQueryId);
        }
    }

    private deliverPendingReply(key: string, route: LeaderRoute): void {
        const reply = this.pendingReplies.get(key);
        if (!reply) return;
        this.pendingReplies.delete(key);
        if (reply.expiresAt <= Date.now() || route.socket.destroyed) return;
        this.write(route.socket, {
            type: "deliver",
            instanceId: route.instanceId,
            generation: route.generation,
            text: reply.text,
        });
    }

    private deliverCallback(route: LeaderRoute, callback: RoutedTelegramCallback): boolean {
        return this.write(route.socket, {
            type: "deliver_callback",
            instanceId: route.instanceId,
            generation: route.generation,
            callback,
        });
    }

    private deliverPendingCallbacks(key: string, route: LeaderRoute): void {
        this.prunePendingCallbacks();
        const retained: PendingCallback[] = [];
        for (const pending of this.pendingCallbacks) {
            if (pending.key !== key) {
                retained.push(pending);
                continue;
            }
            if (!route.socket.destroyed) this.deliverCallback(route, pending.callback);
        }
        this.pendingCallbacks.length = 0;
        this.pendingCallbacks.push(...retained);
    }

    private schedule(action: () => void): void {
        if (this.closed || this.retryTimer) return;
        this.retryTimer = setTimeout(() => {
            this.retryTimer = undefined;
            action();
        }, RETRY_MS + Math.floor(Math.random() * RETRY_MS));
        this.retryTimer.unref();
    }

    private report(error: unknown): void {
        this.options.onError?.(error);
    }
}
