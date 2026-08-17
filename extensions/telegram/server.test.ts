import assert from 'node:assert/strict';
import { createHash, randomBytes } from 'node:crypto';
import { EventEmitter } from 'node:events';
import { chmodSync, existsSync, mkdtempSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { createConnection, createServer } from 'node:net';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { TelegramClient } from './client.ts';
import {
    MAX_FRAME_BYTES,
    TelegramService,
    encodeTelegramFrame,
    telegramServiceAddress,
    type TelegramBotService,
} from './server.ts';

const delay = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));
const waitFor = async (condition: () => boolean, timeoutMs = 2_000): Promise<void> => {
    const deadline = Date.now() + timeoutMs;
    while (!condition()) {
        if (Date.now() >= deadline) throw new Error('timed out waiting for Telegram test state');
        await delay(5);
    }
};
const fixture = (): { token: string; address: string } => {
    const id = randomBytes(12).toString('hex');
    return { token: `test-${id}`, address: `\0pi-telegram-test-${id}` };
};

class FakeBot extends EventEmitter implements TelegramBotService {
    options: { polling?: unknown } = {};
    readonly sent: Array<{ chatId: string | number; text: string; options: unknown }> = [];
    readonly edits: Array<{ markup: unknown; options: unknown }> = [];
    readonly answers: Array<{ id: string; options: unknown }> = [];
    polling = false;
    starts = 0;
    stops = 0;
    activeStarts = 0;
    maxActiveStarts = 0;
    nextMessageId = 100;

    async sendMessage(chatId: string | number, text: string, options?: any) {
        this.sent.push({ chatId, text, options });
        return { chat: { id: chatId }, message_id: this.nextMessageId++ };
    }
    async editMessageReplyMarkup(markup: any, options: any) {
        this.edits.push({ markup, options });
        return true;
    }
    async answerCallbackQuery(id: string, options?: any) {
        this.answers.push({ id, options });
        return true;
    }
    async startPolling() {
        this.activeStarts += 1;
        this.maxActiveStarts = Math.max(this.maxActiveStarts, this.activeStarts);
        this.starts += 1;
        this.polling = true;
        this.activeStarts -= 1;
        return true;
    }
    async stopPolling() {
        this.stops += 1;
        this.polling = false;
        return true;
    }
    isPolling(): boolean { return this.polling; }
}

const makeClient = (
    token: string,
    address: string,
    overrides: Partial<ConstructorParameters<typeof TelegramClient>[0]> = {},
): TelegramClient => new TelegramClient({
    token,
    address,
    poll: false,
    heartbeatMs: 20,
    reconnectMs: 10,
    requestTimeoutMs: 500,
    spawnService: () => undefined,
    onReply: () => undefined,
    ...overrides,
});

const shutdown = async (clients: TelegramClient[], services: TelegramService[]): Promise<void> => {
    await Promise.allSettled(clients.map((client) => client.shutdown({ preserveRoutes: false })));
    await Promise.allSettled(services.map((service) => service.stop()));
};

test('concurrent services bind before bot construction and the loser exits without a bot', async () => {
    const { token, address } = fixture();
    assert.equal(telegramServiceAddress(token).includes(token), false);
    let botConstructions = 0;
    const services = [0, 1].map(() => new TelegramService({
        token,
        address,
        botFactory: () => { botConstructions += 1; return new FakeBot(); },
        heartbeatTimeoutMs: 500,
        checkIntervalMs: 100,
    }));
    try {
        const results = await Promise.all(services.map((service) => service.start()));
        assert.deepEqual(results.sort(), ['address-in-use', 'started']);
        assert.equal(botConstructions, 1);
    } finally {
        await shutdown([], services);
    }
});

test('one service handles RPC operations and exact multi-client reply/callback ownership', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 500, checkIntervalMs: 25 });
    const replies = [[], []] as string[][];
    const callbacks: string[] = [];
    const clients = replies.map((items, index) => makeClient(token, address, {
        clientId: `owner-${index}`,
        onReply: (text) => { items.push(text); },
        onCallback: (callback) => { callbacks.push(`${index}:${callback.callbackQueryId}`); },
    }));
    try {
        await service.start();
        clients.forEach((client) => client.start());
        const first = await clients[0]!.sendMessage('-100', 'first');
        const second = await clients[1]!.sendMessage('-100', 'second', { reply_markup: { inline_keyboard: [[{ text: 'A', callback_data: 'aq:abcdefghijkl:o:0' }]] } });
        assert.equal(first.message_id, 100);
        assert.equal(second.message_id, 101);
        clients[0]!.registerRoute('-100', 100);
        clients[0]!.registerRoute('-100', 100);
        await waitFor(() => service.routeCount() === 2);
        await clients[0]!.editMessageReplyMarkup({ inline_keyboard: [] }, { chat_id: '-100', message_id: 100 });
        await assert.rejects(() => clients[1]!.editMessageReplyMarkup({ inline_keyboard: [] }, { chat_id: '-100', message_id: 100 }), /ownership/u);

        bot.emit('message', { from: { is_bot: false }, chat: { id: -100 }, text: 'reply one', reply_to_message: { message_id: 100 } });
        bot.emit('message', { from: { is_bot: false }, chat: { id: -100 }, text: 'reply two', reply_to_message: { message_id: 101 } });
        bot.emit('message', { from: { is_bot: false }, chat: { id: -101 }, text: 'wrong chat', reply_to_message: { message_id: 100 } });
        bot.emit('callback_query', { id: 'callback-1', data: 'aq:abcdefghijkl:o:0', from: { id: 7, is_bot: false }, message: { chat: { id: -100 }, message_id: 101 } });
        await waitFor(() => replies[0]!.length === 1 && replies[1]!.length === 1 && callbacks.length === 1);
        await clients[1]!.answerCallbackQuery('callback-1', { text: 'done' });
        await assert.rejects(() => clients[0]!.answerCallbackQuery('callback-1', { text: 'wrong' }), /ownership/u);
        assert.deepEqual(replies, [['reply one'], ['reply two']]);
        assert.deepEqual(callbacks, ['1:callback-1']);
        assert.equal(bot.edits.length, 1);
        assert.equal(bot.answers.length, 1);
    } finally {
        await shutdown(clients, [service]);
    }
});

test('reload keeps stable route ownership and replays an unacknowledged event once', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 300, checkIntervalMs: 20 });
    const oldDeliveries: string[] = [];
    const freshDeliveries: string[] = [];
    const oldClient = makeClient(token, address, { onReply: (text) => { oldDeliveries.push(text); throw new Error('simulate reload before ack'); } });
    let freshClient: TelegramClient | undefined;
    try {
        await service.start();
        oldClient.start();
        const sent = await oldClient.sendMessage(-200, 'notification');
        bot.emit('message', { from: { is_bot: false }, chat: { id: -200 }, text: 'during reload', reply_to_message: { message_id: sent.message_id } });
        await waitFor(() => oldDeliveries.length === 1 && service.outboxCount() === 1);
        const stableId = oldClient.clientId;
        await oldClient.shutdown({ preserveRoutes: true });

        freshClient = makeClient(token, address, { onReply: (text) => { freshDeliveries.push(text); } });
        assert.equal(freshClient.clientId, stableId);
        freshClient.start();
        await waitFor(() => freshDeliveries.length === 1 && service.outboxCount() === 0);
        await delay(30);
        assert.deepEqual(oldDeliveries, ['during reload']);
        assert.deepEqual(freshDeliveries, ['during reload']);
        assert.equal(service.routeCount(), 1);
    } finally {
        await shutdown([oldClient, ...(freshClient ? [freshClient] : [])], [service]);
    }
});

test('callback events remain replayable across a reload gap and keep callback ownership', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 300, checkIntervalMs: 20 });
    let oldCallbacks = 0;
    const oldClient = makeClient(token, address, {
        onCallback: () => { oldCallbacks += 1; throw new Error('reload callback gap'); },
    });
    let freshClient: TelegramClient | undefined;
    const callbacks: string[] = [];
    try {
        await service.start();
        oldClient.start();
        const sent = await oldClient.sendMessage(-202, 'question');
        bot.emit('callback_query', {
            id: 'reload-callback', data: 'aq:abcdefghijkl:o:0', from: { id: 8, is_bot: false },
            message: { chat: { id: -202 }, message_id: sent.message_id },
        });
        await waitFor(() => oldCallbacks === 1 && service.outboxCount() === 1);
        await delay(5);
        await oldClient.shutdown({ preserveRoutes: true });
        freshClient = makeClient(token, address, {
            onCallback: (callback) => { callbacks.push(callback.callbackQueryId); },
        });
        freshClient.start();
        await waitFor(() => callbacks.length === 1 && service.outboxCount() === 0);
        await freshClient.answerCallbackQuery('reload-callback', { text: 'replayed' });
        assert.deepEqual(callbacks, ['reload-callback']);
        assert.equal(bot.answers.at(-1)?.id, 'reload-callback');
    } finally {
        await shutdown([oldClient, ...(freshClient ? [freshClient] : [])], [service]);
    }
});

test('same-process in-flight dedupe joins replay across a reload connection', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 300, checkIntervalMs: 20 });
    let release!: () => void;
    const gate = new Promise<void>((resolve) => { release = resolve; });
    let oldCalls = 0;
    let freshCalls = 0;
    const oldClient = makeClient(token, address, { onReply: async () => { oldCalls += 1; await gate; } });
    let freshClient: TelegramClient | undefined;
    try {
        await service.start();
        oldClient.start();
        const sent = await oldClient.sendMessage(-201, 'notification');
        bot.emit('message', { from: { is_bot: false }, chat: { id: -201 }, text: 'one-shot', reply_to_message: { message_id: sent.message_id } });
        await waitFor(() => oldCalls === 1 && service.outboxCount() === 1);
        freshClient = makeClient(token, address, { onReply: () => { freshCalls += 1; } });
        freshClient.start();
        await delay(30);
        assert.equal(freshCalls, 0);
        release();
        await waitFor(() => service.outboxCount() === 0);
        assert.equal(oldCalls, 1);
        assert.equal(freshCalls, 0);
    } finally {
        release();
        await shutdown([oldClient, ...(freshClient ? [freshClient] : [])], [service]);
    }
});

test('service waits a full heartbeat timeout when no client ever arrives', async () => {
    const { token, address } = fixture();
    const service = new TelegramService({
        token, address, botFactory: () => new FakeBot(), heartbeatTimeoutMs: 80, checkIntervalMs: 10,
    });
    const startedAt = Date.now();
    try {
        await service.start();
        await delay(35);
        const probe = createConnection({ path: address });
        await new Promise<void>((resolve, reject) => { probe.once('connect', resolve); probe.once('error', reject); });
        probe.destroy();
        await delay(70);
        await assert.rejects(new Promise<void>((resolve, reject) => {
            const socket = createConnection({ path: address });
            socket.once('connect', () => { socket.destroy(); resolve(); });
            socket.once('error', reject);
        }));
        assert.ok(Date.now() - startedAt >= 80);
    } finally {
        await shutdown([], [service]);
    }
});

test('poll demand is aggregate, survives a short gap, and heartbeat expiry stops polling before socket release', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 90, checkIntervalMs: 10 });
    const sendOnly = makeClient(token, address, { clientId: 'send-only', poll: false });
    const poller = makeClient(token, address, { clientId: 'poller', poll: true });
    try {
        await service.start();
        sendOnly.start();
        poller.start();
        await waitFor(() => bot.polling && service.clientCount() === 2);
        assert.equal(bot.starts, 1);
        await poller.shutdown({ preserveRoutes: true });
        await delay(40);
        assert.equal(bot.polling, true);
        assert.equal(bot.stops, 0);
        await sendOnly.shutdown({ preserveRoutes: true });
        await waitFor(() => service.clientCount() === 0 && bot.stops === 1);
        assert.equal(bot.polling, false);
        assert.equal(bot.maxActiveStarts, 1);
        await assert.rejects(new Promise<void>((resolve, reject) => {
            const socket = createConnection({ path: address });
            socket.once('connect', () => { socket.destroy(); resolve(); });
            socket.once('error', reject);
        }));
    } finally {
        await shutdown([sendOnly, poller], [service]);
    }
});

test('ordinary detach removes routes while reload detach retains them', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 500, checkIntervalMs: 25 });
    const reloadClient = makeClient(token, address, { clientId: 'reload-owner' });
    const quittingClient = makeClient(token, address, { clientId: 'quit-owner' });
    try {
        await service.start();
        reloadClient.start();
        quittingClient.start();
        await reloadClient.sendMessage('routes', 'keep');
        await quittingClient.sendMessage('routes', 'remove');
        assert.equal(service.routeCount(), 2);
        await reloadClient.shutdown({ preserveRoutes: true });
        await quittingClient.shutdown({ preserveRoutes: false });
        await waitFor(() => service.routeCount() === 1);
    } finally {
        await shutdown([reloadClient, quittingClient], [service]);
    }
});

test('authenticated schema violations, unauthenticated frames, and oversized frames are rejected', async () => {
    const { token, address } = fixture();
    const service = new TelegramService({ token, address, botFactory: () => new FakeBot(), heartbeatTimeoutMs: 500, checkIntervalMs: 50 });
    const connect = async () => {
        const socket = createConnection({ path: address });
        await new Promise<void>((resolve, reject) => { socket.once('connect', resolve); socket.once('error', reject); });
        return socket;
    };
    try {
        await service.start();
        const malformed = await connect();
        const malformedClosed = new Promise<void>((resolve) => malformed.once('close', () => resolve()));
        const key = createHash('sha256').update(`pi-telegram-service-ipc:${token}`).digest();
        malformed.write(encodeTelegramFrame(key, {
            type: 'hello', clientId: 'schema-client', generation: 'schema-generation', poll: 'not-boolean',
        })!);
        await malformedClosed;

        const rogue = await connect();
        const rogueClosed = new Promise<void>((resolve) => rogue.once('close', () => resolve()));
        rogue.write('{"payload":{},"mac":"00"}\n');
        await rogueClosed;
        const oversized = await connect();
        const oversizedClosed = new Promise<void>((resolve) => oversized.once('close', () => resolve()));
        oversized.write('x'.repeat(MAX_FRAME_BYTES + 1));
        await oversizedClosed;
    } finally {
        await shutdown([], [service]);
    }
});

test('RPC request timeout is bounded when the Bot API never settles', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    bot.sendMessage = async () => await new Promise<never>(() => undefined);
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 500, checkIntervalMs: 25 });
    const client = makeClient(token, address, { requestTimeoutMs: 35 });
    try {
        await service.start();
        client.start();
        const startedAt = Date.now();
        await assert.rejects(() => client.sendMessage('timeout', 'never'), /timed out/u);
        const elapsed = Date.now() - startedAt;
        assert.ok(elapsed >= 25 && elapsed < 300, `unexpected timeout duration: ${elapsed}`);
    } finally {
        await shutdown([client], [service]);
    }
});

test('client reconnects and starts a replacement service after a service crash', async () => {
    const { token, address } = fixture();
    const firstBot = new FakeBot();
    const first = new TelegramService({ token, address, botFactory: () => firstBot, heartbeatTimeoutMs: 500, checkIntervalMs: 25 });
    const services = [first];
    let replacementBot: FakeBot | undefined;
    let spawnCalls = 0;
    const client = makeClient(token, address, {
        spawnService: () => {
            spawnCalls += 1;
            replacementBot = new FakeBot();
            const replacement = new TelegramService({ token, address, botFactory: () => replacementBot!, heartbeatTimeoutMs: 500, checkIntervalMs: 25 });
            services.push(replacement);
            void replacement.start();
        },
    });
    try {
        await first.start();
        client.start();
        await client.sendMessage('crash', 'before');
        await first.stop();
        await waitFor(() => spawnCalls === 1 && client.isConnected());
        await client.sendMessage('crash', 'after');
        assert.equal(replacementBot?.sent.length, 1);
    } finally {
        await shutdown([client], services);
    }
});

test('connection wait is bounded before any service exists and failed spawns retry after cooldown', async () => {
    const { token, address } = fixture();
    let spawnCalls = 0;
    const errors: unknown[] = [];
    const client = makeClient(token, address, {
        requestTimeoutMs: 90,
        reconnectMs: 5,
        spawnCooldownMs: 20,
        spawnService: () => { spawnCalls += 1; throw new Error('injected spawn failure'); },
        onError: (error) => { errors.push(error); },
    });
    const startedAt = Date.now();
    try {
        await assert.rejects(() => client.sendMessage('absent', 'bounded'), /connection timed out|request timed out/u);
        const elapsed = Date.now() - startedAt;
        assert.ok(elapsed >= 70 && elapsed < 300, `unexpected connection timeout: ${elapsed}`);
        assert.ok(spawnCalls >= 2, `expected spawn retry, got ${spawnCalls}`);
        assert.ok(errors.length >= 2);
    } finally {
        await shutdown([client], []);
    }
});

test('raw socket connect is not ready and cannot satisfy RPC startup', async () => {
    const { token, address } = fixture();
    const sockets = new Set<ReturnType<typeof createConnection>>();
    const impostor = createServer((socket) => { sockets.add(socket); socket.once('close', () => sockets.delete(socket)); });
    await new Promise<void>((resolve, reject) => { impostor.once('error', reject); impostor.listen(address, resolve); });
    const client = makeClient(token, address, { requestTimeoutMs: 55 });
    try {
        const request = client.sendMessage('not-ready', 'must wait');
        await delay(15);
        assert.equal(client.isConnected(), false);
        await assert.rejects(request, /connection timed out/u);
    } finally {
        await shutdown([client], []);
        for (const socket of sockets) socket.destroy();
        await new Promise<void>((resolve) => impostor.close(() => resolve()));
    }
});

test('bot initialization failure closes accepted sockets and releases the service address', async () => {
    const { token, address } = fixture();
    let rejectFactory!: (error: Error) => void;
    const factoryGate = new Promise<TelegramBotService>((_resolve, reject) => { rejectFactory = reject; });
    const broken = new TelegramService({ token, address, botFactory: () => factoryGate });
    const starting = broken.start();
    await delay(10);
    const accepted = createConnection({ path: address });
    await new Promise<void>((resolve, reject) => { accepted.once('connect', resolve); accepted.once('error', reject); });
    const acceptedClosed = new Promise<void>((resolve) => accepted.once('close', () => resolve()));
    rejectFactory(new Error('injected bot construction failure'));
    await assert.rejects(starting, /construction failure/u);
    await acceptedClosed;

    const replacement = new TelegramService({ token, address, botFactory: () => new FakeBot() });
    try {
        assert.equal(await replacement.start(), 'started');
    } finally {
        await shutdown([], [broken, replacement]);
    }
});

test('failed stopPolling retains singleton ownership until polling really stops', async () => {
    class FailingStopBot extends FakeBot {
        failStop = true;
        override async stopPolling() {
            this.stops += 1;
            if (this.failStop) throw new Error('injected stop failure');
            this.polling = false;
            return true;
        }
    }
    const { token, address } = fixture();
    const bot = new FailingStopBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 500, checkIntervalMs: 20 });
    const client = makeClient(token, address, { poll: true });
    const successor = new TelegramService({ token, address, botFactory: () => new FakeBot() });
    try {
        await service.start();
        client.start();
        await waitFor(() => bot.polling);
        await assert.rejects(() => service.stop(), /stop failure/u);
        assert.equal(bot.polling, true);
        assert.equal(await successor.start(), 'address-in-use');
        bot.failStop = false;
        await service.stop();
        assert.equal(bot.polling, false);
        assert.equal(await successor.start(), 'started');
    } finally {
        await shutdown([client], [service, successor]);
    }
});

test('owned routes replay after service replacement and ordinary shutdown clears them', async () => {
    const { token, address } = fixture();
    const firstBot = new FakeBot();
    const first = new TelegramService({ token, address, botFactory: () => firstBot, heartbeatTimeoutMs: 500, checkIntervalMs: 20 });
    const deliveries: string[] = [];
    const client = makeClient(token, address, { onReply: (text) => { deliveries.push(text); } });
    const services = [first];
    try {
        await first.start();
        const sent = await client.sendMessage('-300', 'before crash');
        await first.stop();
        const replacementBot = new FakeBot();
        const replacement = new TelegramService({ token, address, botFactory: () => replacementBot, heartbeatTimeoutMs: 500, checkIntervalMs: 20 });
        services.push(replacement);
        await replacement.start();
        await waitFor(() => client.isConnected() && replacement.routeCount() === 1);
        replacementBot.emit('message', {
            from: { is_bot: false }, chat: { id: -300 }, text: 'after crash',
            reply_to_message: { message_id: sent.message_id },
        });
        await waitFor(() => deliveries.length === 1);
        await client.shutdown({ preserveRoutes: false });
        await waitFor(() => replacement.routeCount() === 0);
        assert.deepEqual(deliveries, ['after crash']);
    } finally {
        await shutdown([client], services);
    }
});

test('journal restores authoritative routes and replays a crash-surviving event exactly once', async () => {
    const { token, address } = fixture();
    const directory = mkdtempSync(join(tmpdir(), 'pi-telegram-state-test-'));
    const statePath = join(directory, 'digest.state.json');
    const firstBot = new FakeBot();
    const first = new TelegramService({
        token, address, statePath, botFactory: () => firstBot, heartbeatTimeoutMs: 500, checkIntervalMs: 20,
    });
    let failedDelivery = 0;
    const oldClient = makeClient(token, address, {
        onReply: () => { failedDelivery += 1; throw new Error('crash before ack'); },
        onError: () => undefined,
    });
    let freshClient: TelegramClient | undefined;
    let replacement: TelegramService | undefined;
    const successful: string[] = [];
    try {
        await first.start();
        const sent = await oldClient.sendMessage('-400', 'journal route');
        firstBot.emit('message', {
            from: { is_bot: false }, chat: { id: -400 }, text: 'journal event',
            reply_to_message: { message_id: sent.message_id },
        });
        await waitFor(() => failedDelivery === 1 && first.outboxCount() === 1);
        assert.equal(statSync(statePath).mode & 0o777, 0o600);
        await oldClient.shutdown({ preserveRoutes: true });
        await first.stop();

        const replacementBot = new FakeBot();
        replacement = new TelegramService({
            token, address, statePath, botFactory: () => replacementBot, heartbeatTimeoutMs: 500, checkIntervalMs: 20,
        });
        assert.equal(await replacement.start(), 'started');
        assert.equal(replacement.routeCount(), 1);
        assert.equal(replacement.outboxCount(), 1);
        freshClient = makeClient(token, address, { onReply: (text) => { successful.push(text); } });
        freshClient.start();
        await waitFor(() => successful.length === 1 && replacement!.outboxCount() === 0);
        await delay(30);
        assert.deepEqual(successful, ['journal event']);
        assert.equal(JSON.parse(readFileSync(statePath, 'utf8')).outbox.length, 0);
    } finally {
        await shutdown([oldClient, ...(freshClient ? [freshClient] : [])], [first, ...(replacement ? [replacement] : [])]);
        rmSync(directory, { recursive: true, force: true });
    }
});

test('clean idle shutdown deletes its state journal only after the full idle timeout', async () => {
    const { token, address } = fixture();
    const directory = mkdtempSync(join(tmpdir(), 'pi-telegram-idle-test-'));
    const statePath = join(directory, 'digest.state.json');
    const service = new TelegramService({
        token, address, statePath, botFactory: () => new FakeBot(), heartbeatTimeoutMs: 70, checkIntervalMs: 5,
    });
    const client = makeClient(token, address);
    const startedAt = Date.now();
    try {
        await service.start();
        await client.sendMessage('idle', 'persist state');
        assert.equal(existsSync(statePath), true);
        await client.shutdown({ preserveRoutes: true });
        await waitFor(() => !existsSync(statePath), 500);
        assert.ok(Date.now() - startedAt >= 70);
        await assert.rejects(new Promise<void>((resolve, reject) => {
            const socket = createConnection({ path: address });
            socket.once('connect', () => { socket.destroy(); resolve(); });
            socket.once('error', reject);
        }));
    } finally {
        await shutdown([client], [service]);
        rmSync(directory, { recursive: true, force: true });
    }
});

test('detached spawn removes PI_TG_TOKEN and supplies the token over fd3', async () => {
    const { token, address } = fixture();
    const directory = mkdtempSync(join(tmpdir(), 'pi-telegram-spawn-test-'));
    const outputPath = join(directory, 'result.json');
    const helperPath = join(directory, 'inspect-child.cjs');
    writeFileSync(helperPath, [
        "const { createHash } = require('node:crypto');",
        "const { readFileSync, writeFileSync } = require('node:fs');",
        "const token = readFileSync(3, 'utf8').replace(/\\n$/, '');",
        "writeFileSync(process.env.PI_TG_TEST_OUTPUT, JSON.stringify({ envToken: process.env.PI_TG_TOKEN ?? null, fdHash: createHash('sha256').update(token).digest('hex') }));",
    ].join('\n'));
    chmodSync(helperPath, 0o700);
    const previousToken = process.env.PI_TG_TOKEN;
    const previousOutput = process.env.PI_TG_TEST_OUTPUT;
    process.env.PI_TG_TOKEN = 'must-not-leak';
    process.env.PI_TG_TEST_OUTPUT = outputPath;
    const client = makeClient(token, address, {
        serverPath: helperPath,
        spawnService: undefined,
        requestTimeoutMs: 250,
        spawnCooldownMs: 1_000,
    });
    try {
        const request = assert.rejects(client.sendMessage('spawn', 'inspect'), /connection timed out/u);
        await waitFor(() => existsSync(outputPath));
        const result = JSON.parse(readFileSync(outputPath, 'utf8')) as { envToken: string | null; fdHash: string };
        assert.equal(result.envToken, null);
        assert.equal(result.fdHash, createHash('sha256').update(token).digest('hex'));
        await request;
    } finally {
        if (previousToken === undefined) delete process.env.PI_TG_TOKEN;
        else process.env.PI_TG_TOKEN = previousToken;
        if (previousOutput === undefined) delete process.env.PI_TG_TEST_OUTPUT;
        else process.env.PI_TG_TEST_OUTPUT = previousOutput;
        await shutdown([client], []);
        rmSync(directory, { recursive: true, force: true });
    }
});

test('an unexpired route cannot be stolen while same-owner replay stays idempotent', async () => {
    const { token, address } = fixture();
    const bot = new FakeBot();
    const service = new TelegramService({ token, address, botFactory: () => bot, heartbeatTimeoutMs: 500, checkIntervalMs: 20 });
    const ownerReplies: string[] = [];
    const thiefReplies: string[] = [];
    const owner = makeClient(token, address, { clientId: 'route-owner', onReply: (text) => { ownerReplies.push(text); } });
    const thief = makeClient(token, address, { clientId: 'route-thief', onReply: (text) => { thiefReplies.push(text); } });
    try {
        await service.start();
        const sent = await owner.sendMessage('-500', 'owned');
        owner.registerRoute('-500', sent.message_id);
        owner.registerRoute('-500', sent.message_id);
        thief.start();
        await waitFor(() => thief.isConnected());
        thief.registerRoute('-500', sent.message_id);
        await delay(30);
        assert.equal(service.routeCount(), 1);
        bot.emit('message', {
            from: { is_bot: false }, chat: { id: -500 }, text: 'owner only',
            reply_to_message: { message_id: sent.message_id },
        });
        await waitFor(() => ownerReplies.length === 1);
        assert.deepEqual(ownerReplies, ['owner only']);
        assert.deepEqual(thiefReplies, []);
    } finally {
        await shutdown([owner, thief], [service]);
    }
});
