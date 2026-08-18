import type { ExtensionAPI, ExtensionContext } from '@earendil-works/pi-coding-agent';
import { basename } from 'node:path';

export type TelegramToolNotificationSender = (message: string) => Promise<void>;

export type TelegramNotificationMessage = {
    role: string;
    content?: unknown;
};

export type TelegramNotificationBuilder = (
    messages: TelegramNotificationMessage[],
    projectName: string,
    sessionName: string | undefined,
    firstSessionUserInput: string | undefined,
    assistantSectionTitle?: string,
) => string | undefined;

type AskQuestionInput = {
    question?: unknown;
    options?: unknown;
    multiSelect?: unknown;
};

type SessionMessage = {
    role?: unknown;
    content?: unknown;
};

const MAX_QUESTION_CHARACTERS = 1_000;
const MAX_OPTION_CHARACTERS = 300;

const truncate = (text: string, maximum: number): string => {
    const characters = Array.from(text.trim().replace(/\s+/gu, ' '));
    return characters.length > maximum
        ? `${characters.slice(0, maximum).join('')}…`
        : characters.join('');
};

const messageContent = (entry: unknown): unknown => {
    if (!entry || typeof entry !== 'object' || !('type' in entry) || !('message' in entry)) return undefined;
    if ((entry as { type?: unknown }).type !== 'message') return undefined;
    return ((entry as { message: SessionMessage }).message).content;
};

const findUserContent = (ctx: ExtensionContext, fromEnd: boolean): unknown => {
    const branch = ctx.sessionManager.getBranch();
    const entries = fromEnd ? [...branch].reverse() : branch;
    const entry = entries.find((item) => item.type === 'message' && item.message.role === 'user');
    return messageContent(entry);
};

const textFromContent = (content: unknown): string => {
    if (typeof content === 'string') return content;
    if (!Array.isArray(content)) return '';
    return content.flatMap((block): string[] => {
        if (!block || typeof block !== 'object') return [];
        const item = block as Record<string, unknown>;
        return item.type === 'text' && typeof item.text === 'string' ? [item.text] : [];
    }).join('\n');
};

export const buildAskQuestionMessage = (input: AskQuestionInput): string | undefined => {
    if (typeof input.question !== 'string' || !input.question.trim()) return undefined;

    const options = Array.isArray(input.options)
        ? input.options
            .filter((option): option is string => typeof option === 'string' && Boolean(option.trim()))
            .map((option) => truncate(option, MAX_OPTION_CHARACTERS))
        : [];
    const lines = [
        `❓ ${truncate(input.question, MAX_QUESTION_CHARACTERS)}`,
        ...(options.length > 0
            ? [
                '',
                input.multiSelect === true ? '可多选：' : '请选择一项：',
                ...options.map((option, index) => `${index + 1}. ${option}`),
            ]
            : []),
    ];
    return lines.join('\n');
};

/** Observes ask_question and sends it through the same notification template as agent_end. */
export const registerTelegramToolWatcher = (
    pi: ExtensionAPI,
    sendNotification: TelegramToolNotificationSender,
    buildNotification: TelegramNotificationBuilder,
): void => {
    pi.on('tool_call', (event, ctx) => {
        if (!ctx.hasUI || event.toolName !== 'ask_question') return;
        const questionMessage = buildAskQuestionMessage(event.input as AskQuestionInput);
        if (!questionMessage) return;

        const latestUserContent = findUserContent(ctx, true);
        const firstUserContent = findUserContent(ctx, false);
        const notification = buildNotification(
            [
                { role: 'user', content: latestUserContent ?? '（无文本输入）' },
                { role: 'assistant', content: questionMessage },
            ],
            basename(ctx.cwd) || ctx.cwd,
            pi.getSessionName(),
            textFromContent(firstUserContent),
            '等待用户回复',
        );
        if (!notification) return;
        void sendNotification(notification).catch(() => undefined);
    });
};
