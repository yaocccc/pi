import { randomBytes } from 'node:crypto';
import type { EventBus } from '@earendil-works/pi-coding-agent';
import type TelegramBot from 'node-telegram-bot-api';
import {
    ASK_QUESTION_ANSWER_EVENT,
    ASK_QUESTION_CANCEL_EVENT,
    ASK_QUESTION_OPEN_EVENT,
    type AskQuestionResult,
    type TelegramQuestionAnswerRequest,
    type TelegramQuestionCancelRequest,
    type TelegramQuestionOpenRequest,
    type TelegramQuestionSettlement,
} from '../ask-question/telegram-bridge.ts';
import type { RoutedTelegramCallback, TelegramCoordinator } from './coordinator.ts';

const CALLBACK_PREFIX = 'aq';
const MAX_CALLBACK_BYTES = 64;
const MAX_QUESTION_MESSAGE_LENGTH = 4_095;

type InlineKeyboardMarkup = {
    inline_keyboard: Array<Array<{ text: string; callback_data: string }>>;
};

type QuestionBot = Pick<TelegramBot, 'sendMessage' | 'editMessageReplyMarkup' | 'answerCallbackQuery'>;

type LiveQuestion = {
    toolCallId: string;
    questionId: string;
    chatId: string;
    messageId: number;
    options: string[];
    multiSelect: boolean;
    checked: Set<number>;
    operation: Promise<void>;
    closed: boolean;
};

export interface TelegramQuestionManagerOptions {
    bot: QuestionBot;
    chatId: string;
    coordinator: Pick<TelegramCoordinator, 'registerRoute'>;
    events: EventBus;
    onError?: (error: unknown) => void;
}

export interface SendTelegramQuestionInput {
    toolCallId: string;
    question: string;
    options: string[];
    multiSelect: boolean;
}

const optionCallbackData = (questionId: string, index: number): string =>
    `${CALLBACK_PREFIX}:${questionId}:o:${index.toString(36)}`;

const confirmCallbackData = (questionId: string): string =>
    `${CALLBACK_PREFIX}:${questionId}:c`;

const parseCallbackData = (data: string): { questionId: string; action: 'option' | 'confirm'; index?: number } | undefined => {
    if (Buffer.byteLength(data, 'utf8') > MAX_CALLBACK_BYTES) return undefined;
    const match = /^aq:([A-Za-z0-9_-]{12}):(o|c)(?::([0-7]))?$/.exec(data);
    if (!match) return undefined;
    if (match[2] === 'c') {
        if (match[3] !== undefined) return undefined;
        return { questionId: match[1]!, action: 'confirm' };
    }
    if (match[3] === undefined) return undefined;
    return { questionId: match[1]!, action: 'option', index: Number.parseInt(match[3], 36) };
};

export const createTelegramQuestionId = (): string => randomBytes(9).toString('base64url');

export const buildTelegramQuestionText = (question: string, multiSelect: boolean): string => {
    const prefix = '❓ ';
    const suffix = `\n\n${multiSelect ? '可多选；勾选后点击“确认选择”。' : '请选择一项：'}`;
    const budget = Math.max(0, MAX_QUESTION_MESSAGE_LENGTH - prefix.length - suffix.length);
    if (question.length <= budget) return `${prefix}${question}${suffix}`;

    const characters: string[] = [];
    let length = 0;
    for (const character of question) {
        if (length + character.length > Math.max(0, budget - 1)) break;
        characters.push(character);
        length += character.length;
    }
    return `${prefix}${characters.join('')}…${suffix}`;
};

export const buildTelegramQuestionKeyboard = (
    questionId: string,
    options: string[],
    multiSelect: boolean,
    checked: ReadonlySet<number> = new Set(),
): InlineKeyboardMarkup => {
    const inline_keyboard = options.map((option, index) => [{
        text: multiSelect ? `${checked.has(index) ? '☑' : '☐'} ${option}` : option,
        callback_data: optionCallbackData(questionId, index),
    }]);
    if (multiSelect) {
        inline_keyboard.push([{ text: '确认选择', callback_data: confirmCallbackData(questionId) }]);
    }
    return { inline_keyboard };
};

export class TelegramQuestionManager {
    private readonly options: TelegramQuestionManagerOptions;
    private readonly questions = new Map<string, LiveQuestion>();
    private shuttingDown = false;

    constructor(options: TelegramQuestionManagerOptions) {
        this.options = options;
    }

    async sendQuestion(input: SendTelegramQuestionInput): Promise<boolean> {
        if (this.shuttingDown || !input.toolCallId || !input.question.trim() || input.options.length === 0) return false;
        const questionId = createTelegramQuestionId();
        const keyboard = buildTelegramQuestionKeyboard(questionId, input.options, input.multiSelect);
        for (const row of keyboard.inline_keyboard) {
            for (const button of row) {
                if (Buffer.byteLength(button.callback_data, 'utf8') > MAX_CALLBACK_BYTES) return false;
            }
        }

        try {
            const sent = await this.options.bot.sendMessage(
                this.options.chatId,
                buildTelegramQuestionText(input.question, input.multiSelect),
                { reply_markup: keyboard },
            );
            const state: LiveQuestion = {
                toolCallId: input.toolCallId,
                questionId,
                chatId: String(sent.chat.id),
                messageId: sent.message_id,
                options: [...input.options],
                multiSelect: input.multiSelect,
                checked: new Set(),
                operation: Promise.resolve(),
                closed: false,
            };
            if (this.shuttingDown) {
                await this.removeKeyboard(state);
                return false;
            }
            const openRequest: TelegramQuestionOpenRequest = {
                toolCallId: input.toolCallId,
                questionId,
                registered: false,
            };
            this.options.events.emit(ASK_QUESTION_OPEN_EVENT, openRequest);
            if (openRequest.registered !== true) {
                await this.removeKeyboard(state);
                return false;
            }
            this.questions.set(questionId, state);
            this.options.coordinator.registerRoute(sent.chat.id, sent.message_id);
            return true;
        } catch (error) {
            this.options.onError?.(error);
            return false;
        }
    }

    async handleCallback(callback: RoutedTelegramCallback): Promise<boolean> {
        if (callback.userIsBot || !callback.callbackQueryId || !callback.data) return false;
        const parsed = parseCallbackData(callback.data);
        if (!parsed) {
            await this.answerStale(callback.callbackQueryId);
            return false;
        }

        const state = this.questions.get(parsed.questionId);
        if (!state
            || state.chatId !== callback.chatId
            || state.messageId !== callback.messageId) {
            await this.answerStale(callback.callbackQueryId);
            return false;
        }

        return this.serialize(state, async () => {
            if (state.closed || this.questions.get(state.questionId) !== state) {
                await this.answerStale(callback.callbackQueryId);
                return false;
            }
            return this.handleLiveCallback(state, parsed, callback.callbackQueryId);
        });
    }

    handleSettlement(settlement: TelegramQuestionSettlement): void {
        if (settlement.winner !== 'local') return;
        const state = this.questions.get(settlement.questionId);
        if (!state || state.toolCallId !== settlement.toolCallId) return;
        void this.serialize(state, () => this.closeState(state));
    }

    async cancelQuestion(toolCallId: string): Promise<void> {
        this.cancelBridgeQuestion(toolCallId);
        const states = [...this.questions.values()].filter((state) => state.toolCallId === toolCallId);
        await Promise.all(states.map((state) => this.serialize(state, () => this.closeState(state))));
    }

    async shutdown(): Promise<void> {
        this.shuttingDown = true;
        const states = [...this.questions.values()];
        for (const state of states) this.cancelBridgeQuestion(state.toolCallId);
        await Promise.all(states.map((state) => this.serialize(state, () => this.closeState(state))));
    }

    private answerBridgeQuestion(state: LiveQuestion, result: AskQuestionResult): boolean {
        const request: TelegramQuestionAnswerRequest = {
            toolCallId: state.toolCallId,
            questionId: state.questionId,
            result,
            accepted: false,
        };
        this.options.events.emit(ASK_QUESTION_ANSWER_EVENT, request);
        return request.accepted === true;
    }

    private cancelBridgeQuestion(toolCallId: string): void {
        this.options.events.emit(ASK_QUESTION_CANCEL_EVENT, {
            toolCallId,
            accepted: false,
        } satisfies TelegramQuestionCancelRequest);
    }

    private async handleLiveCallback(
        state: LiveQuestion,
        parsed: { questionId: string; action: 'option' | 'confirm'; index?: number },
        callbackQueryId: string,
    ): Promise<boolean> {
        if (parsed.action === 'option') {
            const index = parsed.index;
            if (index === undefined || index < 0 || index >= state.options.length) {
                await this.answerStale(callbackQueryId);
                return false;
            }
            if (!state.multiSelect) {
                const won = this.answerBridgeQuestion(state, {
                    answers: [state.options[index]!],
                    wasCustom: false,
                });
                if (!won) {
                    await this.answerStale(callbackQueryId);
                    await this.closeState(state);
                    return false;
                }
                await this.answer(callbackQueryId, '已选择');
                await this.closeState(state);
                return true;
            }

            if (state.checked.has(index)) state.checked.delete(index);
            else state.checked.add(index);
            await this.answer(callbackQueryId, state.checked.has(index) ? '已勾选' : '已取消');
            await this.updateKeyboard(state);
            return true;
        }

        if (!state.multiSelect) {
            await this.answerStale(callbackQueryId);
            return false;
        }
        if (state.checked.size === 0) {
            await this.answer(callbackQueryId, '请至少选择一项', true);
            return false;
        }

        const answers = [...state.checked]
            .sort((left, right) => left - right)
            .map((index) => state.options[index]!)
            .filter(Boolean);
        const won = this.answerBridgeQuestion(state, {
            answers,
            customAnswers: [],
            wasCustom: false,
        });
        if (!won) {
            await this.answerStale(callbackQueryId);
            await this.closeState(state);
            return false;
        }
        await this.answer(callbackQueryId, `已选择 ${answers.length} 项`);
        await this.closeState(state);
        return true;
    }

    private serialize<T>(state: LiveQuestion, operation: () => Promise<T>): Promise<T> {
        const result = state.operation.then(operation);
        state.operation = result.then(() => undefined, () => undefined);
        return result;
    }

    private async updateKeyboard(state: LiveQuestion): Promise<void> {
        try {
            await this.options.bot.editMessageReplyMarkup(
                buildTelegramQuestionKeyboard(state.questionId, state.options, true, state.checked),
                { chat_id: state.chatId, message_id: state.messageId },
            );
        } catch (error) {
            this.options.onError?.(error);
        }
    }

    private async closeState(state: LiveQuestion): Promise<void> {
        if (state.closed || this.questions.get(state.questionId) !== state) return;
        state.closed = true;
        this.questions.delete(state.questionId);
        await this.removeKeyboard(state);
    }

    private async removeKeyboard(state: LiveQuestion): Promise<void> {
        try {
            await this.options.bot.editMessageReplyMarkup(
                { inline_keyboard: [] },
                { chat_id: state.chatId, message_id: state.messageId },
            );
        } catch (error) {
            this.options.onError?.(error);
        }
    }

    private async answer(callbackQueryId: string, text: string, showAlert = false): Promise<void> {
        try {
            await this.options.bot.answerCallbackQuery(callbackQueryId, {
                text,
                show_alert: showAlert,
            });
        } catch (error) {
            this.options.onError?.(error);
        }
    }

    private answerStale(callbackQueryId: string): Promise<void> {
        return this.answer(callbackQueryId, '问题已失效');
    }
}
