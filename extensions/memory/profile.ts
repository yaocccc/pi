import type { Message } from '@earendil-works/pi-ai';
import { completeSimple } from '@earendil-works/pi-ai/compat';
import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { readFile } from 'node:fs/promises';
import { DEFAULT_MEMORY, MAX_INPUT_CHARS, MAX_MEMORY_CHARS, MEMORY_PATH } from './constants';
import { assistantText, clamp, saveText } from './utils';

const updatePrompt = (current: string, input: string): string =>
    `你是用户画像子 agent。根据用户最新消息更新 memory.md，输出完整 Markdown。\n\n` +
    `规则：\n` +
    `1. 只记录长期稳定可复用的信息：用户画像、偏好、工作环境、交互习惯、明确禁忌。\n` +
    `2. 不记录临时任务、日志、密钥或敏感信息；不要凭空推断。\n` +
    `3. 冲突信息以最近明确说法为准，或放入待确认。\n` +
    `4. 总长度不超过 ${MAX_MEMORY_CHARS} 字符，每节最多 6 条。\n` +
    `5. 保持 # Memory、用户画像、偏好、工作环境、交互习惯、待确认 结构。\n` +
    `6. 无需更新则原样输出 current_memory；只输出 Markdown。\n\n` +
    `<current_memory>\n${current}\n</current_memory>\n\n` +
    `<user_message>\n${clamp(input, MAX_INPUT_CHARS)}\n</user_message>`;

const autoInjectPrompt = (memory: string): string =>
    `用户长期记忆来自 ${MEMORY_PATH}。请静默参考；` +
    `若与用户当前明确指示冲突，以当前指示为准。` +
    `不要主动提及已载入记忆，除非用户询问。\n\n` +
    `<memory>\n${memory}\n</memory>`;

const cleanMemory = (output: string): string | undefined => {
    const text = output.trim().replace(/^```(?:markdown|md)?\s*\n([\s\S]*?)\n```$/i, '$1').trim();
    if (!text.startsWith('# Memory')) return undefined;
    if (text.length > MAX_MEMORY_CHARS + 500) return undefined;
    return text;
};

export const readMemory = async (): Promise<string> => {
    try {
        return (await readFile(MEMORY_PATH, 'utf8')).trim() || DEFAULT_MEMORY.trim();
    } catch (e) {
        if ((e as NodeJS.ErrnoException).code === 'ENOENT') return DEFAULT_MEMORY.trim();
        throw e;
    }
};

export const saveMemory = async (content: string) => saveText(MEMORY_PATH, content);

export const updateMemory = async (input: string, ctx: ExtensionContext): Promise<boolean> => {
    if (!input.trim() || !ctx.model) return false;
    const auth = await ctx.modelRegistry.getApiKeyAndHeaders(ctx.model);
    if (!auth.ok || !auth.apiKey) return false;

    const current = await readMemory();
    const messages: Message[] = [{ role: 'user', content: [{ type: 'text', text: updatePrompt(current, input) }], timestamp: Date.now() }];
    const res = await completeSimple(ctx.model, { messages }, {
        apiKey: auth.apiKey,
        headers: auth.headers,
        maxTokens: 2_000,
        reasoning: 'low',
    });
    if (res.stopReason === 'error') return false;

    const next = cleanMemory(assistantText(res));
    if (!next || next === current.trim()) return false;
    await saveMemory(next);
    return true;
};

export const memoryPrompt = async (): Promise<string | undefined> => {
    const memory = await readMemory();
    if (memory === DEFAULT_MEMORY.trim()) return undefined;
    return autoInjectPrompt(memory);
};
