import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { createConnection, type Socket } from "node:net";
import { test } from "node:test";
import {
    TelegramCoordinator,
    telegramCoordinatorAddress,
    type TelegramCoordinatorOptions,
} from "./coordinator.ts";

const delay = (milliseconds: number): Promise<void> =>
    new Promise((resolve) => setTimeout(resolve, milliseconds));

const waitFor = async (condition: () => boolean, timeoutMs = 3_000): Promise<void> => {
    const deadline = Date.now() + timeoutMs;
    while (!condition()) {
        if (Date.now() >= deadline) throw new Error("timed out waiting for coordinator state");
        await delay(10);
    }
};

const tokenForTest = (): string => `test-token-${randomBytes(12).toString("hex")}`;

const makeCoordinator = (
    token: string,
    replies: string[],
    leaderStarts: { count: number },
    overrides: Partial<TelegramCoordinatorOptions> = {},
): TelegramCoordinator => new TelegramCoordinator({
    token,
    onLeaderStart: () => { leaderStarts.count += 1; },
    onLeaderStop: () => undefined,
    onReply: (text) => { replies.push(text); },
    ...overrides,
});

const shutdownAll = async (coordinators: TelegramCoordinator[]): Promise<void> => {
    await Promise.all(coordinators.map((coordinator) => coordinator.shutdown()));
};

test("one abstract-socket leader routes exact replies to the owning generation", async () => {
    const token = tokenForTest();
    const replies = [[], [], []] as string[][];
    const starts = [{ count: 0 }, { count: 0 }, { count: 0 }];
    const coordinators = replies.map((ownedReplies, index) =>
        makeCoordinator(token, ownedReplies, starts[index]!));

    try {
        for (const coordinator of coordinators) coordinator.start();
        await waitFor(() => coordinators.filter((coordinator) => coordinator.isLeader()).length === 1);
        await delay(75);

        assert.equal(starts.reduce((sum, item) => sum + item.count, 0), 1);
        const leader = coordinators.find((coordinator) => coordinator.isLeader())!;
        coordinators[0]!.registerRoute(-10001, 41);
        coordinators[1]!.registerRoute(-10001, 42);

        leader.dispatchReply(-10001, 41, "first reply");
        leader.dispatchReply(-10001, 42, "second reply");
        await waitFor(() => replies[0]!.length === 1 && replies[1]!.length === 1);

        assert.deepEqual(replies, [["first reply"], ["second reply"], []]);
        assert.equal(leader.dispatchReply(-10001, 999, "unknown"), false);
        assert.equal(leader.dispatchReply(-10002, 41, "wrong chat"), false);
        await delay(25);
        assert.deepEqual(replies, [["first reply"], ["second reply"], []]);
    } finally {
        await shutdownAll(coordinators);
    }
});

test("leader handover replays bounded owned routes and stale generations are removed", async () => {
    const token = tokenForTest();
    const repliesA: string[] = [];
    const repliesB: string[] = [];
    const startsA = { count: 0 };
    const startsB = { count: 0 };
    const stopsA = { count: 0 };
    const stopsB = { count: 0 };
    const first = makeCoordinator(token, repliesA, startsA, {
        routeTtlMs: 2_000,
        maxRoutes: 2,
        onLeaderStop: () => { stopsA.count += 1; },
    });
    const second = makeCoordinator(token, repliesB, startsB, {
        routeTtlMs: 2_000,
        maxRoutes: 2,
        onLeaderStop: () => { stopsB.count += 1; },
    });
    const coordinators = [first, second];

    try {
        first.start();
        second.start();
        await waitFor(() => coordinators.filter((item) => item.isLeader()).length === 1);
        const oldLeader = coordinators.find((item) => item.isLeader())!;
        const owner = coordinators.find((item) => !item.isLeader())!;
        const ownerReplies = owner === first ? repliesA : repliesB;

        owner.registerRoute("handover-chat", 100);
        oldLeader.dispatchReply("handover-chat", 100, "before");
        await waitFor(() => ownerReplies.length === 1);

        await oldLeader.shutdown();
        assert.equal(oldLeader === first ? stopsA.count : stopsB.count, 1);
        await waitFor(() => owner.isLeader());
        owner.dispatchReply("handover-chat", 100, "after");
        await waitFor(() => ownerReplies.length === 2);
        assert.deepEqual(ownerReplies, ["before", "after"]);

        owner.registerRoute("handover-chat", 101);
        owner.registerRoute("handover-chat", 102);
        owner.registerRoute("handover-chat", 103);
        owner.dispatchReply("handover-chat", 102, "kept-102");
        owner.dispatchReply("handover-chat", 103, "kept-103");
        await waitFor(() => ownerReplies.length === 4);
        assert.deepEqual(ownerReplies.slice(2), ["kept-102", "kept-103"]);
        assert.equal(owner.dispatchReply("handover-chat", 101, "evicted"), false);

        const staleGeneration = owner.generation;
        await owner.shutdown();
        const replacementReplies: string[] = [];
        const replacement = makeCoordinator(token, replacementReplies, { count: 0 }, {
            instanceId: owner.instanceId,
            generation: `${staleGeneration}-replacement`,
        });
        coordinators.push(replacement);
        replacement.start();
        await waitFor(() => replacement.isLeader());
        assert.equal(replacement.dispatchReply("handover-chat", 103, "stale"), false);
        await delay(25);
        assert.deepEqual(replacementReplies, []);
    } finally {
        await shutdownAll(coordinators);
    }
});

test("transient client IPC errors stay silent through leader handover while unexpected errors report", async () => {
    const token = tokenForTest();
    const firstErrors: unknown[] = [];
    const secondErrors: unknown[] = [];
    const first = makeCoordinator(token, [], { count: 0 }, { onError: (error) => firstErrors.push(error) });
    const second = makeCoordinator(token, [], { count: 0 }, { onError: (error) => secondErrors.push(error) });
    const coordinators = [first, second];
    const clientOf = (coordinator: TelegramCoordinator): Socket | undefined =>
        (coordinator as unknown as { client?: Socket }).client;

    try {
        first.start();
        second.start();
        await waitFor(() => coordinators.filter((coordinator) => coordinator.isLeader()).length === 1);
        await waitFor(() => coordinators.every((coordinator) => Boolean(clientOf(coordinator))));
        const oldLeader = coordinators.find((coordinator) => coordinator.isLeader())!;
        const successor = coordinators.find((coordinator) => !coordinator.isLeader())!;
        const successorErrors = successor === first ? firstErrors : secondErrors;

        for (const code of ["EPIPE", "ECONNRESET", "ECONNREFUSED", "ENOENT"]) {
            clientOf(successor)!.emit("error", Object.assign(new Error(code), { code }));
        }
        assert.deepEqual(firstErrors, []);
        assert.deepEqual(secondErrors, []);

        await oldLeader.shutdown();
        await waitFor(() => successor.isLeader());
        await waitFor(() => Boolean(clientOf(successor)));
        assert.equal(coordinators.filter((coordinator) => coordinator.isLeader()).length, 1);
        assert.deepEqual(firstErrors, []);
        assert.deepEqual(secondErrors, []);

        const unexpected = Object.assign(new Error("unexpected client failure"), { code: "EACCES" });
        clientOf(successor)!.emit("error", unexpected);
        assert.deepEqual(successorErrors, [unexpected]);
    } finally {
        await shutdownAll(coordinators);
    }
});

test("shutdown keeps the lock through delayed poller stop and rejects late accepts", async () => {
    const token = tokenForTest();
    let releaseStop!: () => void;
    const stopGate = new Promise<void>((resolve) => { releaseStop = resolve; });
    let stopStarted = false;
    let stopFinished = false;
    let leadershipOverlap = false;
    const first = makeCoordinator(token, [], { count: 0 }, {
        onLeaderStop: async () => {
            stopStarted = true;
            await stopGate;
            stopFinished = true;
        },
    });
    const secondStarts = { count: 0 };
    const second = makeCoordinator(token, [], secondStarts, {
        onLeaderStart: () => {
            secondStarts.count += 1;
            if (!stopFinished) leadershipOverlap = true;
        },
    });

    try {
        first.start();
        await waitFor(() => first.isLeader());
        second.start();
        await delay(75);

        const shuttingDown = first.shutdown();
        await waitFor(() => stopStarted);

        const probe = createConnection({ path: telegramCoordinatorAddress(token) });
        const probeClosed = new Promise<void>((resolve) => probe.once("close", resolve));
        await new Promise<void>((resolve, reject) => {
            probe.once("connect", resolve);
            probe.once("error", reject);
        });
        await Promise.race([
            probeClosed,
            delay(1_000).then(() => { throw new Error("closed coordinator retained a late socket"); }),
        ]);
        await delay(75);
        assert.equal(second.isLeader(), false);
        assert.equal(secondStarts.count, 0);

        releaseStop();
        await Promise.race([
            shuttingDown,
            delay(1_000).then(() => { throw new Error("shutdown waited on a late accepted socket"); }),
        ]);
        await waitFor(() => second.isLeader());
        assert.equal(stopFinished, true);
        assert.equal(leadershipOverlap, false);
        assert.equal(secondStarts.count, 1);
    } finally {
        releaseStop();
        await shutdownAll([first, second]);
    }
});

test("a one-shot reply before handover route replay is delivered exactly once", async () => {
    const token = tokenForTest();
    const first = makeCoordinator(token, [], { count: 0 });
    const replies: string[] = [];
    let dispatches = 0;
    let initialDispatchResult: boolean | undefined;
    let owner!: TelegramCoordinator;

    try {
        first.start();
        await waitFor(() => first.isLeader());
        owner = makeCoordinator(token, replies, { count: 0 }, {
            onLeaderStart: () => {
                dispatches += 1;
                initialDispatchResult = owner.dispatchReply("replay-chat", 501, "buffered once");
            },
        });
        owner.registerRoute("replay-chat", 501);
        owner.start();
        await delay(75);

        await first.shutdown();
        await waitFor(() => owner.isLeader());
        await waitFor(() => replies.length === 1);
        await delay(50);

        assert.equal(dispatches, 1);
        assert.equal(initialDispatchResult, false);
        assert.deepEqual(replies, ["buffered once"]);
    } finally {
        await shutdownAll([first, ...(owner ? [owner] : [])]);
    }
});

test("pending replies are exact-keyed, bounded, and expire", async () => {
    const token = tokenForTest();
    const replies: string[] = [];
    const coordinator = makeCoordinator(token, replies, { count: 0 }, {
        pendingReplyTtlMs: 100,
        maxPendingReplies: 2,
    });

    try {
        coordinator.start();
        await waitFor(() => coordinator.isLeader());

        assert.equal(coordinator.dispatchReply("bounded", 1, "evicted"), false);
        assert.equal(coordinator.dispatchReply("bounded", 2, "kept-2"), false);
        assert.equal(coordinator.dispatchReply("bounded", 3, "kept-3"), false);
        coordinator.registerRoute("bounded", 1);
        coordinator.registerRoute("bounded", 2);
        coordinator.registerRoute("bounded", 3);
        await waitFor(() => replies.length === 2);
        assert.deepEqual(replies, ["kept-2", "kept-3"]);

        assert.equal(coordinator.dispatchReply("exact-chat", 10, "exact"), false);
        coordinator.registerRoute("wrong-chat", 10);
        coordinator.registerRoute("exact-chat", 11);
        await delay(25);
        assert.deepEqual(replies, ["kept-2", "kept-3"]);
        coordinator.registerRoute("exact-chat", 10);
        await waitFor(() => replies.length === 3);
        assert.deepEqual(replies, ["kept-2", "kept-3", "exact"]);

        assert.equal(coordinator.dispatchReply("expires", 20, "expired"), false);
        await delay(150);
        coordinator.registerRoute("expires", 20);
        await delay(50);
        assert.deepEqual(replies, ["kept-2", "kept-3", "exact"]);
    } finally {
        await coordinator.shutdown();
    }
});

test("callback queries route by exact message, reject bots, and dedupe callback ids", async () => {
    const token = tokenForTest();
    const callbacks = [[], []] as Array<Array<{ callbackQueryId: string; data: string }>>;
    const coordinators = callbacks.map((ownedCallbacks) => makeCoordinator(token, [], { count: 0 }, {
        onCallback: (callback) => { ownedCallbacks.push(callback); },
    }));

    try {
        for (const coordinator of coordinators) coordinator.start();
        await waitFor(() => coordinators.filter((coordinator) => coordinator.isLeader()).length === 1);
        const leader = coordinators.find((coordinator) => coordinator.isLeader())!;
        coordinators[0]!.registerRoute("callback-chat", 71);
        coordinators[1]!.registerRoute("callback-chat", 72);

        const first = {
            callbackQueryId: "callback-1",
            data: "aq:abcdefghijkl:o:0",
            userId: 10,
            userIsBot: false,
        };
        leader.dispatchCallback("callback-chat", 71, first);
        leader.dispatchCallback("callback-chat", 72, {
            ...first,
            callbackQueryId: "callback-2",
            data: "aq:abcdefghijkl:c",
        });
        await waitFor(() => callbacks[0]!.length === 1 && callbacks[1]!.length === 1);

        assert.equal(leader.dispatchCallback("callback-chat", 71, first), false);
        assert.equal(leader.dispatchCallback("callback-chat", 71, {
            ...first,
            callbackQueryId: "bot-callback",
            userIsBot: true,
        }), false);
        assert.equal(leader.dispatchCallback("wrong-chat", 71, {
            ...first,
            callbackQueryId: "wrong-route",
        }), false);
        await delay(25);
        assert.deepEqual(callbacks.map((items) => items.map((item) => item.callbackQueryId)), [
            ["callback-1"],
            ["callback-2"],
        ]);
    } finally {
        await shutdownAll(coordinators);
    }
});

test("leader handover buffers callback queries in FIFO order exactly once", async () => {
    const token = tokenForTest();
    const first = makeCoordinator(token, [], { count: 0 });
    const delivered: string[] = [];
    const base = {
        data: "aq:abcdefghijkl:o:0",
        userId: 11,
        userIsBot: false,
    };
    let owner!: TelegramCoordinator;

    try {
        first.start();
        await waitFor(() => first.isLeader());
        owner = makeCoordinator(token, [], { count: 0 }, {
            pendingCallbackTtlMs: 1_000,
            maxPendingCallbacks: 3,
            onCallback: (callback) => { delivered.push(callback.callbackQueryId); },
            onLeaderStart: () => {
                owner.dispatchCallback("handover-callback", 801, { ...base, callbackQueryId: "before-and-after" });
                owner.dispatchCallback("handover-callback", 801, { ...base, callbackQueryId: "fifo-1" });
                owner.dispatchCallback("handover-callback", 801, { ...base, callbackQueryId: "fifo-2" });
                owner.dispatchCallback("handover-callback", 801, { ...base, callbackQueryId: "fifo-2" });
            },
        });
        owner.registerRoute("handover-callback", 801);
        owner.start();
        await delay(75);
        first.dispatchCallback("handover-callback", 801, { ...base, callbackQueryId: "before-and-after" });
        await waitFor(() => delivered.length === 1);

        await first.shutdown();
        await waitFor(() => owner.isLeader());
        await waitFor(() => delivered.length === 3);
        await delay(25);
        assert.deepEqual(delivered, ["before-and-after", "fifo-1", "fifo-2"]);
    } finally {
        await shutdownAll([first, ...(owner ? [owner] : [])]);
    }
});

test("pending callback FIFO is bounded and expires before a route appears", async () => {
    const token = tokenForTest();
    const delivered: string[] = [];
    const coordinator = makeCoordinator(token, [], { count: 0 }, {
        pendingCallbackTtlMs: 60,
        maxPendingCallbacks: 2,
        onCallback: (callback) => { delivered.push(callback.callbackQueryId); },
    });
    const base = {
        data: "aq:abcdefghijkl:o:0",
        userId: 12,
        userIsBot: false,
    };

    try {
        coordinator.start();
        await waitFor(() => coordinator.isLeader());
        coordinator.dispatchCallback("bounded-callback", 900, { ...base, callbackQueryId: "evicted" });
        coordinator.dispatchCallback("bounded-callback", 900, { ...base, callbackQueryId: "kept-1" });
        coordinator.dispatchCallback("bounded-callback", 900, { ...base, callbackQueryId: "kept-2" });
        coordinator.registerRoute("bounded-callback", 900);
        await waitFor(() => delivered.length === 2);
        assert.deepEqual(delivered, ["kept-1", "kept-2"]);

        coordinator.dispatchCallback("expired-callback", 901, { ...base, callbackQueryId: "expired" });
        await delay(90);
        coordinator.registerRoute("expired-callback", 901);
        await delay(30);
        assert.deepEqual(delivered, ["kept-1", "kept-2"]);
    } finally {
        await coordinator.shutdown();
    }
});

test("expired routes are not delivered", async () => {
    const token = tokenForTest();
    const replies: string[] = [];
    const coordinator = makeCoordinator(token, replies, { count: 0 }, { routeTtlMs: 40 });
    try {
        coordinator.start();
        await waitFor(() => coordinator.isLeader());
        coordinator.registerRoute(7, 8);
        await delay(100);
        assert.equal(coordinator.dispatchReply(7, 8, "too late"), false);
        assert.deepEqual(replies, []);
    } finally {
        await coordinator.shutdown();
    }
});

test("IPC rejects unauthenticated and oversized frames", async () => {
    const token = tokenForTest();
    const coordinator = makeCoordinator(token, [], { count: 0 });
    try {
        coordinator.start();
        await waitFor(() => coordinator.isLeader());
        const socket = createConnection({ path: telegramCoordinatorAddress(token) });
        await new Promise<void>((resolve, reject) => {
            socket.once("connect", resolve);
            socket.once("error", reject);
        });
        const closed = new Promise<void>((resolve) => socket.once("close", () => resolve()));
        socket.write(`${JSON.stringify({
            payload: {
                type: "hello",
                instanceId: "rogue",
                generation: "rogue",
            },
            mac: "00",
        })}\n`);
        await Promise.race([
            closed,
            delay(1_000).then(() => { throw new Error("unauthenticated socket remained open"); }),
        ]);

        const oversized = createConnection({ path: telegramCoordinatorAddress(token) });
        await new Promise<void>((resolve, reject) => {
            oversized.once("connect", resolve);
            oversized.once("error", reject);
        });
        const oversizedClosed = new Promise<void>((resolve) => oversized.once("close", () => resolve()));
        oversized.write("x".repeat(70 * 1_024));
        await Promise.race([
            oversizedClosed,
            delay(1_000).then(() => { throw new Error("oversized frame remained open"); }),
        ]);
    } finally {
        await coordinator.shutdown();
    }
});
