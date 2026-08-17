import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { basename } from 'node:path';
import TelegramBot, { type CallbackQuery } from 'node-telegram-bot-api';
import {
    ASK_QUESTION_SETTLED_EVENT,
    normalizeAskQuestionOptions,
    type TelegramQuestionSettlement,
} from '../ask-question/telegram-bridge.ts';
import { TelegramCoordinator } from './coordinator.ts';
import { TelegramQuestionManager } from './question.ts';

const TELEGRAM_MESSAGE_LIMIT = 3_900;
const TELEGRAM_POLL_TIMEOUT_SECONDS = 1;
const pollEnabled = /^(?:1|true|yes|on)$/iu.test(process.env.PI_TG_POLL?.trim() ?? '');

const token = process.env.PI_TG_TOKEN?.trim();
const chatId = process.env.PI_TG_CHAT?.trim();
const bot = token ? new TelegramBot(token, { polling: false }) : undefined;
if (bot) bot.options.polling = { params: { timeout: TELEGRAM_POLL_TIMEOUT_SECONDS } };

type Message = {
    role: string;
    content?: unknown;
};

const textFromContent = (content: unknown, includeImages = false): string => {
    if (typeof content === 'string') return content;
    if (!Array.isArray(content)) return '';

    return content.flatMap((block): string[] => {
        if (!block || typeof block !== 'object') return [];
        const item = block as Record<string, unknown>;
        if (item.type === 'text' && typeof item.text === 'string') return [item.text];
        if (includeImages && item.type === 'image') return ['[图片]'];
        return [];
    }).join('\n');
};

const splitTelegramMessage = (message: string): string[] => {
    const characters = Array.from(message);
    const chunks: string[] = [];
    let offset = 0;

    while (offset < characters.length) {
        let end = Math.min(offset + TELEGRAM_MESSAGE_LIMIT, characters.length);
        if (end < characters.length) {
            const newline = characters.lastIndexOf('\n', end - 1);
            if (newline > offset) end = newline;

            let trailingBackslashes = 0;
            for (let index = end - 1; index >= offset && characters[index] === '\\'; index -= 1) {
                trailingBackslashes += 1;
            }
            if (trailingBackslashes % 2 === 1) end -= 1;
        }

        chunks.push(characters.slice(offset, end).join(''));
        offset = characters[end] === '\n' ? end + 1 : end;
    }

    return chunks;
};

const escapeMarkdownV2 = (text: string): string =>
    text.replace(/([_*\[\]()~`>#+\-=|{}.!\\])/gu, '\\$1');

const formatSection = (icon: string, title: string, content: string): string => [
    `${icon} *${escapeMarkdownV2(title)}*`,
    ...content.split('\n').map((line) => `>${line ? ` ${escapeMarkdownV2(line)}` : ''}`),
].join('\n');

const truncateSessionName = (name: string): string => {
    const characters = Array.from(name.trim());
    return characters.length > 10
        ? `${characters.slice(0, 10).join('')}…`
        : characters.join('');
};

const buildNotification = (
    messages: Message[],
    projectName: string,
    sessionName: string | undefined,
): string | undefined => {
    const firstUserMessage = messages.find((message) => message.role === 'user');
    const userMessage = [...messages].reverse().find((message) => message.role === 'user');
    const assistantMessage = [...messages].reverse().find((message) => message.role === 'assistant');
    if (!userMessage || !assistantMessage) return undefined;

    const firstUserInput = textFromContent(firstUserMessage?.content, true).trim();
    const userInput = textFromContent(userMessage.content, true).trim();
    const finalOutput = textFromContent(assistantMessage.content).trim();
    if (!userInput && !finalOutput) return undefined;

    const sessionLabel = sessionName?.trim() || firstUserInput || '临时会话';
    return [
        `📁 *来自项目* · ${escapeMarkdownV2(projectName)}`,
        `💬 *来自会话* · ${escapeMarkdownV2(truncateSessionName(sessionLabel))}`,
        '',
        '━━━━━━━━━━━━━━━━━━━━',
        '',
        formatSection('👤', '用户输入', userInput || '（无文本输入）'),
        '',
        formatSection('🤖', '最终回复', finalOutput || '（无最终文本输出）'),
    ].join('\n');
};

const sendToTelegram = async (
    message: string,
    coordinator?: TelegramCoordinator,
): Promise<void> => {
    if (!bot || !chatId || !message.trim()) return;

    try {
        for (const chunk of splitTelegramMessage(message)) {
            const sent = await bot.sendMessage(chatId, chunk, { parse_mode: 'MarkdownV2' });
            coordinator?.registerRoute(sent.chat.id, sent.message_id);
        }
    } catch (error) {
        const detail = error instanceof Error ? error.message : String(error);
        const sanitized = token ? detail.replaceAll(token, '[redacted]') : detail;
        console.error(`[telegram] 发送消息失败: ${sanitized}`);
    }
};

export default (pi: ExtensionAPI) => {
    let coordinator: TelegramCoordinator | undefined;
    let questionManager: TelegramQuestionManager | undefined;

    const logTelegramError = (label: string, error: unknown): void => {
        const detail = error instanceof Error ? error.message : String(error);
        const sanitized = token ? detail.replaceAll(token, '[redacted]') : detail;
        console.error(`[telegram] ${label}: ${sanitized}`);
    };
    const logPollingError = (error: unknown): void => logTelegramError('轮询失败', error);

    pi.events.on(ASK_QUESTION_SETTLED_EVENT, (data) => {
        if (!data || typeof data !== 'object') return;
        const settlement = data as TelegramQuestionSettlement;
        if (typeof settlement.toolCallId !== 'string'
            || typeof settlement.questionId !== 'string'
            || (settlement.winner !== 'local' && settlement.winner !== 'telegram')) return;
        questionManager?.handleSettlement(settlement);
    });

    pi.on('tool_call', async (event, ctx) => {
        if (event.toolName !== 'ask_question'
            || !ctx.hasUI
            || !pollEnabled
            || !coordinator?.isRunning()
            || !questionManager) return;
        const input = event.input as { question?: unknown; options?: unknown; multiSelect?: unknown };
        if (typeof input.question !== 'string' || !Array.isArray(input.options)) return;
        const options = normalizeAskQuestionOptions(input.options.filter((option): option is string => typeof option === 'string'));
        if (options.length === 0) return;
        await questionManager.sendQuestion({
            toolCallId: event.toolCallId,
            question: input.question,
            options,
            multiSelect: input.multiSelect === true,
        });
    });

    pi.on('tool_execution_end', async (event) => {
        if (event.toolName !== 'ask_question') return;
        await questionManager?.cancelQuestion(event.toolCallId);
    });

    pi.on('session_start', (_event, ctx) => {
        if (!ctx.hasUI || !bot || !token || !chatId || !pollEnabled || coordinator) return;

        let instance: TelegramCoordinator;
        const handleMessage = (message: {
            from?: { is_bot: boolean };
            chat: { id: number };
            text?: string;
            reply_to_message?: { message_id: number };
        }): void => {
            const text = message.text;
            if (!message.from || message.from.is_bot || !text?.trim() || !message.reply_to_message) return;
            instance.dispatchReply(message.chat.id, message.reply_to_message.message_id, text);
        };
        const handleCallbackQuery = (query: CallbackQuery): void => {
            const message = query.message;
            if (!message || query.from.is_bot || !query.data) return;
            instance.dispatchCallback(message.chat.id, message.message_id, {
                callbackQueryId: query.id,
                data: query.data,
                userId: query.from.id,
                userIsBot: query.from.is_bot,
            });
        };
        const handlePollingError = (error: Error): void => logPollingError(error);

        instance = new TelegramCoordinator({
            token,
            onLeaderStart: () => {
                bot.on('message', handleMessage);
                bot.on('callback_query', handleCallbackQuery);
                bot.on('polling_error', handlePollingError);
                void bot.startPolling().catch(logPollingError);
            },
            onLeaderStop: async () => {
                bot.removeListener('message', handleMessage);
                bot.removeListener('callback_query', handleCallbackQuery);
                bot.removeListener('polling_error', handlePollingError);
                if (bot.isPolling()) await bot.stopPolling({ cancel: true });
            },
            onReply: (text) => pi.sendUserMessage(text, { deliverAs: 'followUp' }),
            onCallback: async (callback) => {
                await questionManager?.handleCallback(callback);
            },
            onError: logPollingError,
        });
        coordinator = instance;
        questionManager = new TelegramQuestionManager({
            bot,
            chatId,
            coordinator: instance,
            events: pi.events,
            onError: (error) => logTelegramError('问答 API 失败', error),
        });
        instance.start();
    });

    pi.on('session_shutdown', async () => {
        const currentQuestions = questionManager;
        const current = coordinator;
        questionManager = undefined;
        coordinator = undefined;
        const coordinatorShutdown = current?.shutdown();
        const questionShutdown = currentQuestions?.shutdown();
        await Promise.all([coordinatorShutdown, questionShutdown]);
    });

    pi.on('agent_end', async (event, ctx) => {
        if (!ctx.hasUI) return;

        const projectName = basename(ctx.cwd) || ctx.cwd;
        const sessionName = pi.getSessionName();
        const notification = buildNotification(event.messages, projectName, sessionName);
        if (notification) await sendToTelegram(notification, coordinator);
    });
};
