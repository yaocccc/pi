import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { basename } from 'node:path';
import TelegramBot from 'node-telegram-bot-api';
import { registerTelegramToolWatcher } from './tool-watcher.ts';

const TELEGRAM_MESSAGE_LIMIT = 3_900;

const token = process.env.PI_TG_TOKEN?.trim();
const chatId = process.env.PI_TG_CHAT?.trim();
const bot = token ? new TelegramBot(token, { polling: false }) : undefined;

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
    return characters.length > 20
        ? `${characters.slice(0, 20).join('')}…`
        : characters.join('');
};

const buildNotification = (
    messages: Message[],
    projectName: string,
    sessionName: string | undefined,
    firstSessionUserInput: string | undefined,
    assistantSectionTitle = '最终回复',
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
        '💬 *来自会话*',
        `> ${escapeMarkdownV2(projectName)} · ${escapeMarkdownV2(truncateSessionName(sessionLabel))}`,
        '',
        formatSection('👤', '用户输入', userInput || '（无文本输入）'),
        '',
        formatSection('🤖', assistantSectionTitle, finalOutput || '（无最终文本输出）'),
    ].join('\n');
};

const sendToTelegram = async (message: string): Promise<void> => {
    if (!bot || !chatId || !message.trim()) return;

    try {
        for (const chunk of splitTelegramMessage(message)) {
            await bot.sendMessage(chatId, chunk, { parse_mode: 'MarkdownV2' });
        }
    } catch (error) {
        // const detail = error instanceof Error ? error.message : String(error);
        // const sanitized = token ? detail.replaceAll(token, '[redacted]') : detail;
        // console.error(`[telegram] 发送消息失败: ${sanitized}`);
    }
};

export default (pi: ExtensionAPI) => {
    registerTelegramToolWatcher(pi, sendToTelegram, buildNotification);

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
        if (notification) await sendToTelegram(notification);
    });
};
