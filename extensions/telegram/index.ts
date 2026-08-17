import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { basename } from 'node:path';
import {
    ASK_QUESTION_SETTLED_EVENT,
    normalizeAskQuestionOptions,
    type TelegramQuestionSettlement,
} from '../ask-question/telegram-bridge.ts';
import { TelegramClient } from './client.ts';
import { TelegramQuestionManager } from './question.ts';

const TELEGRAM_MESSAGE_LIMIT = 3_900;
const pollEnabled = /^(?:1|true|yes|on)$/iu.test(process.env.PI_TG_POLL?.trim() ?? '');

const token = process.env.PI_TG_TOKEN?.trim();
const chatId = process.env.PI_TG_CHAT?.trim();

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
    firstSessionUserInput: string | undefined,
): string | undefined => {
    const userMessage = [...messages].reverse().find((message) => message.role === 'user');
    const assistantMessage = [...messages].reverse().find((message) => message.role === 'assistant');
    if (!userMessage || !assistantMessage) return undefined;

    const firstUserInput = firstSessionUserInput?.trim().replace(/\s+/gu, ' ') ?? '';
    const userInput = textFromContent(userMessage.content, true).trim();
    const finalOutput = textFromContent(assistantMessage.content).trim();
    if (!userInput && !finalOutput) return undefined;

    const namedSession = sessionName?.trim();
    const sessionLabel = namedSession && namedSession !== '未命名会话'
        ? namedSession
        : firstUserInput || '临时会话';
    return [
        `📁 *来自项目* · ${escapeMarkdownV2(projectName)}`,
        `💬 *来自会话* · ${escapeMarkdownV2(truncateSessionName(sessionLabel))}`,
        '',
        formatSection('👤', '用户输入', userInput || '（无文本输入）'),
        '',
        formatSection('🤖', '最终回复', finalOutput || '（无最终文本输出）'),
    ].join('\n');
};

const sendToTelegram = async (
    message: string,
    client?: TelegramClient,
): Promise<void> => {
    if (!client || !chatId || !message.trim()) return;

    try {
        for (const chunk of splitTelegramMessage(message)) {
            const sent = await client.sendMessage(chatId, chunk, { parse_mode: 'MarkdownV2' });
            client.registerRoute(sent.chat.id, sent.message_id);
        }
    } catch (error) {
        const detail = error instanceof Error ? error.message : String(error);
        const sanitized = token ? detail.replaceAll(token, '[redacted]') : detail;
        console.error(`[telegram] 发送消息失败: ${sanitized}`);
    }
};

export default (pi: ExtensionAPI) => {
    let client: TelegramClient | undefined;
    let questionManager: TelegramQuestionManager | undefined;

    const logTelegramError = (label: string, error: unknown): void => {
        const detail = error instanceof Error ? error.message : String(error);
        const sanitized = token ? detail.replaceAll(token, '[redacted]') : detail;
        console.error(`[telegram] ${label}: ${sanitized}`);
    };
    const logPollingError = (error: unknown): void => logTelegramError('轮询失败', error);

    const disposeQuestionSettlement = pi.events.on(ASK_QUESTION_SETTLED_EVENT, (data) => {
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
            || !client
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
        if (!ctx.hasUI || !token || !chatId || client) return;

        const instance = new TelegramClient({
            token,
            poll: pollEnabled,
            onReply: (text) => pi.sendUserMessage(text, { deliverAs: 'followUp' }),
            onCallback: async (callback) => {
                const manager = questionManager;
                if (!manager) throw new Error('Telegram question manager unavailable');
                await manager.handleCallback(callback);
            },
            onError: logPollingError,
        });
        client = instance;
        if (pollEnabled) {
            questionManager = new TelegramQuestionManager({
                bot: instance,
                chatId,
                routes: instance,
                events: pi.events,
                onError: (error) => logTelegramError('问答 API 失败', error),
            });
        }
        instance.start();
    });

    pi.on('session_shutdown', async (event) => {
        disposeQuestionSettlement();
        const currentQuestions = questionManager;
        const current = client;
        await currentQuestions?.shutdown();
        questionManager = undefined;
        await current?.shutdown({ preserveRoutes: event.reason === 'reload' });
        client = undefined;
    });

    pi.on('agent_end', async (event, ctx) => {
        if (!ctx.hasUI) return;

        const projectName = basename(ctx.cwd) || ctx.cwd;
        const sessionName = pi.getSessionName();
        const firstSessionUserMessage = ctx.sessionManager.getBranch().find(
            (entry) => entry.type === 'message' && entry.message.role === 'user',
        );
        const firstSessionUserInput = firstSessionUserMessage?.type === 'message'
            ? textFromContent((firstSessionUserMessage.message as Message).content, true)
            : undefined;
        const notification = buildNotification(
            event.messages,
            projectName,
            sessionName,
            firstSessionUserInput,
        );
        if (notification) await sendToTelegram(notification, client);
    });
};
