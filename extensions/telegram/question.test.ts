import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createEventBus, type EventBus } from '@earendil-works/pi-coding-agent';
import {
    ASK_QUESTION_ANSWER_EVENT,
    ASK_QUESTION_SETTLED_EVENT,
    AskQuestionTelegramBridge,
    type AskQuestionResult,
    type TelegramQuestionAnswerRequest,
    type TelegramQuestionSettlement,
} from '../ask-question/telegram-bridge.ts';
import type { RoutedTelegramCallback } from './coordinator.ts';
import { buildTelegramQuestionText, TelegramQuestionManager } from './question.ts';

const makeEvents = (): EventBus => createEventBus();

class FakeBot {
    readonly sent: Array<{ chatId: string; text: string; form: any }> = [];
    readonly edits: Array<{ markup: any; form: any }> = [];
    readonly answers: Array<{ id: string; form: any }> = [];
    nextMessageId = 100;
    private nextKeyboardEditDelay?: { started: () => void; gate: Promise<void> };

    delayNextKeyboardEdit(): { started: Promise<void>; release: () => void } {
        let markStarted!: () => void;
        let release!: () => void;
        const started = new Promise<void>((resolve) => { markStarted = resolve; });
        const gate = new Promise<void>((resolve) => { release = resolve; });
        this.nextKeyboardEditDelay = { started: markStarted, gate };
        return { started, release };
    }

    async sendMessage(chatId: string, text: string, form: any) {
        this.sent.push({ chatId, text, form });
        return { chat: { id: -123 }, message_id: this.nextMessageId++ };
    }

    async editMessageReplyMarkup(markup: any, form: any) {
        this.edits.push({ markup, form });
        if (markup.inline_keyboard.length > 0 && this.nextKeyboardEditDelay) {
            const delay = this.nextKeyboardEditDelay;
            this.nextKeyboardEditDelay = undefined;
            delay.started();
            await delay.gate;
        }
        return true;
    }

    async answerCallbackQuery(id: string, form: any) {
        this.answers.push({ id, form });
        return true;
    }
}

const callback = (
    id: string,
    data: string,
    overrides: Partial<RoutedTelegramCallback> = {},
): RoutedTelegramCallback => ({
    callbackQueryId: id,
    data,
    userId: 7,
    userIsBot: false,
    chatId: '-123',
    messageId: 100,
    ...overrides,
});

const setup = () => {
    const bot = new FakeBot();
    const events = makeEvents();
    const routes: Array<[string | number, number]> = [];
    const bridge = new AskQuestionTelegramBridge(events);
    const manager = new TelegramQuestionManager({
        bot: bot as any,
        chatId: 'configured-chat',
        coordinator: { registerRoute: (chatId, messageId) => { routes.push([chatId, messageId]); } },
        events,
    });
    events.on(ASK_QUESTION_SETTLED_EVENT, (data) => manager.handleSettlement(data as TelegramQuestionSettlement));
    return { bot, bridge, events, routes, manager };
};

test('event open -> ask wait/listener -> manager answer settles across separate producer and consumer', async () => {
    const { bot, bridge, routes, manager } = setup();
    const results: AskQuestionResult[] = [];
    assert.equal(await manager.sendQuestion({
        toolCallId: 'tool-call-secret',
        question: 'Pick one',
        options: ['Alpha option', 'Beta option'],
        multiSelect: false,
    }), true);
    const keyboard = bot.sent[0]!.form.reply_markup.inline_keyboard;
    const alphaData = keyboard[0][0].callback_data as string;
    const betaData = keyboard[1][0].callback_data as string;

    assert.deepEqual(routes, [[-123, 100]]);
    assert.equal(Buffer.byteLength(alphaData, 'utf8') <= 64, true);
    assert.equal(alphaData.includes('Alpha option'), false);
    assert.equal(alphaData.includes('tool-call-secret'), false);
    bridge.onAnswer('tool-call-secret', (result) => results.push(result));
    const waiting = bridge.wait('tool-call-secret');
    assert.ok(waiting);

    assert.equal(await manager.handleCallback(callback('bot', alphaData, { userIsBot: true })), false);
    assert.equal(await manager.handleCallback(callback('wrong-chat', alphaData, { chatId: '-999' })), false);
    assert.deepEqual(results, []);
    assert.equal(await manager.handleCallback(callback('valid', betaData)), true);
    assert.deepEqual(results, [{ answers: ['Beta option'], wasCustom: false }]);
    assert.deepEqual(await waiting, { answers: ['Beta option'], wasCustom: false });
    assert.deepEqual(bot.edits.at(-1)!.markup, { inline_keyboard: [] });

    bridge.release('tool-call-secret');
});

test('multi-select toggles independently, rejects empty confirm, and confirms original order', async () => {
    const { bot, bridge, manager } = setup();
    const results: AskQuestionResult[] = [];
    await manager.sendQuestion({
        toolCallId: 'tool-multi',
        question: 'Pick several',
        options: ['First', 'Second', 'Third'],
        multiSelect: true,
    });
    bridge.onAnswer('tool-multi', (result) => results.push(result));
    const keyboard = bot.sent[0]!.form.reply_markup.inline_keyboard;
    const firstData = keyboard[0][0].callback_data as string;
    const thirdData = keyboard[2][0].callback_data as string;
    const confirmData = keyboard[3][0].callback_data as string;

    assert.equal(await manager.handleCallback(callback('empty', confirmData)), false);
    assert.equal(bot.answers.at(-1)!.form.show_alert, true);
    assert.deepEqual(results, []);

    assert.equal(await manager.handleCallback(callback('third', thirdData)), true);
    assert.equal(bot.edits.at(-1)!.markup.inline_keyboard[2][0].text.startsWith('☑'), true);
    assert.equal(await manager.handleCallback(callback('first', firstData)), true);
    assert.equal(bot.edits.at(-1)!.markup.inline_keyboard[0][0].text.startsWith('☑'), true);
    assert.equal(await manager.handleCallback(callback('confirm', confirmData)), true);
    assert.deepEqual(results, [{
        answers: ['First', 'Third'],
        customAnswers: [],
        wasCustom: false,
    }]);

    bridge.release('tool-multi');
});

test('a delayed toggle cannot restore the keyboard after a concurrent confirm', async () => {
    const { bot, bridge, manager } = setup();
    const results: AskQuestionResult[] = [];
    await manager.sendQuestion({
        toolCallId: 'tool-concurrent-confirm',
        question: 'Pick then confirm',
        options: ['One', 'Two'],
        multiSelect: true,
    });
    bridge.onAnswer('tool-concurrent-confirm', (result) => results.push(result));
    const keyboard = bot.sent[0]!.form.reply_markup.inline_keyboard;
    const optionData = keyboard[0][0].callback_data as string;
    const confirmData = keyboard[2][0].callback_data as string;
    const delay = bot.delayNextKeyboardEdit();

    const toggle = manager.handleCallback(callback('toggle-delayed', optionData));
    await delay.started;
    const confirm = manager.handleCallback(callback('confirm-concurrent', confirmData));
    delay.release();

    assert.equal(await toggle, true);
    assert.equal(await confirm, true);
    assert.deepEqual(results, [{ answers: ['One'], customAnswers: [], wasCustom: false }]);
    assert.deepEqual(bot.edits.at(-1)!.markup, { inline_keyboard: [] });
    assert.equal(bot.edits.map((edit) => edit.markup.inline_keyboard.length === 0).lastIndexOf(true), bot.edits.length - 1);
    bridge.release('tool-concurrent-confirm');
});

test('local settlement waits behind a delayed toggle and leaves the keyboard closed', async () => {
    const { bot, bridge, events, manager } = setup();
    await manager.sendQuestion({
        toolCallId: 'tool-local-question',
        question: 'Local wins',
        options: ['One', 'Two'],
        multiSelect: true,
    });
    const keyboard = bot.sent[0]!.form.reply_markup.inline_keyboard;
    const data = keyboard[0][0].callback_data as string;
    const delay = bot.delayNextKeyboardEdit();

    const toggle = manager.handleCallback(callback('toggle-before-local', data));
    await delay.started;
    assert.equal(bridge.settleLocally('tool-local-question'), true);
    delay.release();
    assert.equal(await toggle, true);
    await manager.cancelQuestion('tool-local-question');

    assert.deepEqual(bot.edits.at(-1)!.markup, { inline_keyboard: [] });
    assert.equal(bot.edits.map((edit) => edit.markup.inline_keyboard.length === 0).lastIndexOf(true), bot.edits.length - 1);
    assert.equal(await manager.handleCallback(callback('late', data)), false);
    const late: TelegramQuestionAnswerRequest = {
        toolCallId: 'tool-local-question',
        questionId: 'irrelevant',
        result: { answers: ['Late'] },
        accepted: false,
    };
    events.emit(ASK_QUESTION_ANSWER_EVENT, late);
    assert.equal(late.accepted, false);
});

test('shutdown serializes behind a delayed toggle and cannot revive its keyboard', async () => {
    const { bot, events, manager } = setup();
    await manager.sendQuestion({
        toolCallId: 'tool-shutdown',
        question: 'Shutdown while editing',
        options: ['One', 'Two'],
        multiSelect: true,
    });
    const data = bot.sent[0]!.form.reply_markup.inline_keyboard[0][0].callback_data as string;
    const delay = bot.delayNextKeyboardEdit();

    const toggle = manager.handleCallback(callback('toggle-before-shutdown', data));
    await delay.started;
    const shutdown = manager.shutdown();
    delay.release();
    assert.equal(await toggle, true);
    await shutdown;

    assert.deepEqual(bot.edits.at(-1)!.markup, { inline_keyboard: [] });
    assert.equal(bot.edits.map((edit) => edit.markup.inline_keyboard.length === 0).lastIndexOf(true), bot.edits.length - 1);
    const late: TelegramQuestionAnswerRequest = {
        toolCallId: 'tool-shutdown',
        questionId: 'irrelevant',
        result: { answers: ['Late'] },
        accepted: false,
    };
    events.emit(ASK_QUESTION_ANSWER_EVENT, late);
    assert.equal(late.accepted, false);
});

test('blocked-call cleanup is idempotent and releases manager and bridge state', async () => {
    const { bot, events, manager } = setup();
    await manager.sendQuestion({
        toolCallId: 'tool-blocked',
        question: 'Will never execute',
        options: ['One'],
        multiSelect: false,
    });
    const data = bot.sent[0]!.form.reply_markup.inline_keyboard[0][0].callback_data as string;

    await manager.cancelQuestion('tool-blocked');
    await manager.cancelQuestion('tool-blocked');
    assert.deepEqual(bot.edits.at(-1)!.markup, { inline_keyboard: [] });
    assert.equal(await manager.handleCallback(callback('blocked-late', data)), false);
    const late: TelegramQuestionAnswerRequest = {
        toolCallId: 'tool-blocked',
        questionId: 'irrelevant',
        result: { answers: ['Late'] },
        accepted: false,
    };
    events.emit(ASK_QUESTION_ANSWER_EVENT, late);
    assert.equal(late.accepted, false);
});

test('question previews are Unicode-safe and remain below Telegram sendMessage limits', () => {
    const text = buildTelegramQuestionText('😀'.repeat(3_000), true);
    assert.equal(text.length <= 4_095, true);
    assert.equal(text.startsWith('❓ 😀'), true);
    assert.equal(text.includes('…\n\n可多选；勾选后点击“确认选择”。'), true);

    for (let index = 0; index < text.length; index += 1) {
        const code = text.charCodeAt(index);
        if (code >= 0xD800 && code <= 0xDBFF) {
            const next = text.charCodeAt(index + 1);
            assert.equal(next >= 0xDC00 && next <= 0xDFFF, true);
            index += 1;
        } else {
            assert.equal(code >= 0xDC00 && code <= 0xDFFF, false);
        }
    }
});
