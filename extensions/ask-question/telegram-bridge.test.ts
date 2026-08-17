import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createEventBus, type EventBus } from '@earendil-works/pi-coding-agent';
import {
    ASK_QUESTION_ANSWER_EVENT,
    ASK_QUESTION_CANCEL_EVENT,
    ASK_QUESTION_OPEN_EVENT,
    ASK_QUESTION_SETTLED_EVENT,
    AskQuestionTelegramBridge,
    normalizeAskQuestionOptions,
    type AskQuestionResult,
    type TelegramQuestionAnswerRequest,
    type TelegramQuestionCancelRequest,
    type TelegramQuestionOpenRequest,
    type TelegramQuestionSettlement,
} from './telegram-bridge.ts';

const makeEvents = (): { bus: EventBus; settlements: TelegramQuestionSettlement[] } => {
    const bus = createEventBus();
    const settlements: TelegramQuestionSettlement[] = [];
    bus.on(ASK_QUESTION_SETTLED_EVENT, (data) => settlements.push(data as TelegramQuestionSettlement));
    return { bus, settlements };
};

const openQuestion = (events: EventBus, toolCallId: string, questionId: string): boolean => {
    const request: TelegramQuestionOpenRequest = { toolCallId, questionId, registered: false };
    events.emit(ASK_QUESTION_OPEN_EVENT, request);
    return request.registered === true;
};

const answerQuestion = (
    events: EventBus,
    toolCallId: string,
    questionId: string,
    result: AskQuestionResult,
): boolean => {
    const request: TelegramQuestionAnswerRequest = {
        toolCallId,
        questionId,
        result,
        accepted: false,
    };
    events.emit(ASK_QUESTION_ANSWER_EVENT, request);
    return request.accepted === true;
};

test('separate event producer opens and answers the ask-owned bridge', async () => {
    const { bus } = makeEvents();
    const consumer = new AskQuestionTelegramBridge(bus);

    assert.equal(openQuestion(bus, 'tool-events', 'question-events'), true);
    const waiting = consumer.wait('tool-events');
    assert.ok(waiting);
    assert.equal(answerQuestion(bus, 'tool-events', 'question-events', { answers: ['Remote'] }), true);
    assert.deepEqual(await waiting, { answers: ['Remote'] });
    consumer.release('tool-events');
});

test('Telegram and local completion use a one-shot race', () => {
    const { bus, settlements } = makeEvents();
    const bridge = new AskQuestionTelegramBridge(bus);
    const answers: string[][] = [];
    assert.equal(openQuestion(bus, 'tool-telegram', 'question-001'), true);
    const detach = bridge.onAnswer('tool-telegram', (result) => answers.push(result.answers));

    assert.equal(answerQuestion(bus, 'tool-telegram', 'question-001', { answers: ['B'] }), true);
    assert.equal(bridge.settleLocally('tool-telegram'), false);
    assert.deepEqual(answers, [['B']]);
    assert.deepEqual(settlements, [{
        toolCallId: 'tool-telegram',
        questionId: 'question-001',
        winner: 'telegram',
    }]);

    detach();
    bridge.release('tool-telegram');
});

test('an early Telegram answer is delivered when execute later attaches', async () => {
    const { bus } = makeEvents();
    const bridge = new AskQuestionTelegramBridge(bus);
    assert.equal(openQuestion(bus, 'tool-early', 'question-early'), true);
    assert.equal(answerQuestion(bus, 'tool-early', 'question-early', { answers: ['Early'] }), true);

    const waiting = bridge.wait('tool-early');
    assert.ok(waiting);
    assert.deepEqual(await waiting, { answers: ['Early'] });

    const observed: AskQuestionResult[] = [];
    bridge.onAnswer('tool-early', (result) => observed.push(result));
    await new Promise<void>((resolve) => queueMicrotask(resolve));
    assert.deepEqual(observed, [{ answers: ['Early'] }]);
    bridge.release('tool-early');
});

test('a local or ESC completion makes later Telegram callbacks stale', () => {
    const { bus, settlements } = makeEvents();
    const bridge = new AskQuestionTelegramBridge(bus);
    assert.equal(openQuestion(bus, 'tool-local', 'question-002'), true);
    assert.equal(bridge.settleLocally('tool-local'), true);
    assert.equal(answerQuestion(bus, 'tool-local', 'question-002', { answers: ['A'] }), false);
    assert.deepEqual(settlements, [{
        toolCallId: 'tool-local',
        questionId: 'question-002',
        winner: 'local',
    }]);
    bridge.release('tool-local');

    assert.equal(bridge.settleLocally('tool-without-telegram'), true);
});

test('RPC-style waiting consumes a registered Telegram answer without custom UI', async () => {
    const { bus } = makeEvents();
    const bridge = new AskQuestionTelegramBridge(bus);
    assert.equal(bridge.wait('missing-tool'), undefined);
    assert.equal(openQuestion(bus, 'tool-rpc', 'question-rpc'), true);

    const waiting = bridge.wait('tool-rpc');
    assert.ok(waiting);
    assert.equal(answerQuestion(bus, 'tool-rpc', 'question-rpc', { answers: ['Remote'] }), true);
    assert.deepEqual(await waiting, { answers: ['Remote'] });
    bridge.cancel('tool-rpc');
});

test('abort and event cancellation idempotently release bridge state', async () => {
    const { bus, settlements } = makeEvents();
    const bridge = new AskQuestionTelegramBridge(bus);
    const controller = new AbortController();
    assert.equal(openQuestion(bus, 'tool-abort', 'question-abort'), true);

    const waiting = bridge.wait('tool-abort', controller.signal);
    assert.ok(waiting);
    controller.abort();
    assert.equal(await waiting, null);
    assert.deepEqual(settlements, [{
        toolCallId: 'tool-abort',
        questionId: 'question-abort',
        winner: 'local',
    }]);

    const cancel: TelegramQuestionCancelRequest = { toolCallId: 'tool-abort' };
    bus.emit(ASK_QUESTION_CANCEL_EVENT, cancel);
    bus.emit(ASK_QUESTION_CANCEL_EVENT, { toolCallId: 'tool-abort' } satisfies TelegramQuestionCancelRequest);
    assert.equal(cancel.accepted, true);
    assert.equal(answerQuestion(bus, 'tool-abort', 'question-abort', { answers: ['Late'] }), false);
});

test('disposing a reloaded bridge cannot intercept questions for the new bridge', () => {
    const { bus } = makeEvents();
    const oldBridge = new AskQuestionTelegramBridge(bus);
    assert.equal(openQuestion(bus, 'old-tool', 'old-question'), true);
    oldBridge.dispose();

    const newBridge = new AskQuestionTelegramBridge(bus);
    assert.equal(openQuestion(bus, 'new-tool', 'new-question'), true);
    assert.equal(oldBridge.wait('new-tool'), undefined);
    assert.ok(newBridge.wait('new-tool'));
    newBridge.cancel('new-tool');
    newBridge.dispose();
});

test('option normalization matches ask_question execution', () => {
    assert.deepEqual(
        normalizeAskQuestionOptions([' A ', 'A', '\tB\nC ', '', ' D ', 'E', 'F', 'G', 'H', 'I', 'J']),
        ['A', 'B C', 'D', 'E', 'F', 'G', 'H', 'I'],
    );
});
