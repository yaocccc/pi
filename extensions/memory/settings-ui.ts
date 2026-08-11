import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import {
    writeMemorySettings,
    type MemoryResultDisplay,
    type MemorySettings,
    type MemoryThinking,
} from './settings';

type MenuKey =
    | 'maxMemories'
    | 'auto'
    | 'model'
    | 'thinking'
    | 'resultDisplay'
    | 'includeToolMessages'
    | 'includeThinking'
    | 'save'
    | 'cancel';

type Choice<T> = { label: string; value: T };

const THINKING_CHOICES: Choice<MemoryThinking>[] = [
    { label: '跟随当前会话', value: 'auto' },
    { label: '关闭', value: 'off' },
    { label: 'Minimal', value: 'minimal' },
    { label: 'Low', value: 'low' },
    { label: 'Medium', value: 'medium' },
    { label: 'High', value: 'high' },
    { label: 'XHigh', value: 'xhigh' },
    { label: 'Max', value: 'max' },
];

const RESULT_DISPLAY_CHOICES: Choice<MemoryResultDisplay>[] = [
    { label: '写入当前会话', value: 'message' },
    { label: '居中弹窗', value: 'popup' },
    { label: '不通知', value: 'none' },
];

const displayName = (value: MemoryResultDisplay): string => RESULT_DISPLAY_CHOICES.find((choice) => choice.value === value)?.label ?? value;
const enabledName = (value: boolean): string => value ? '开启' : '关闭';
const includedName = (value: boolean): string => value ? '包含' : '排除';

const choose = async <T extends string>(
    ctx: ExtensionContext,
    title: string,
    choices: Choice<T>[],
    current: T,
): Promise<T> => {
    const options = choices.map((choice) => `${choice.value === current ? '●' : '○'} ${choice.label}  ·  ${choice.value}`);
    const selected = await ctx.ui.select(title, options);
    if (!selected) return current;
    return choices[options.indexOf(selected)]?.value ?? current;
};

const chooseBoolean = async (
    ctx: ExtensionContext,
    title: string,
    current: boolean,
): Promise<boolean> => {
    const choices: Choice<'true' | 'false'>[] = [
        { label: '开启', value: 'true' },
        { label: '关闭', value: 'false' },
    ];
    return (await choose(ctx, title, choices, String(current) as 'true' | 'false')) === 'true';
};

const chooseModel = async (ctx: ExtensionContext, current: string): Promise<string> => {
    const models = ctx.scopedModels.length
        ? ctx.scopedModels.map((entry) => entry.model)
        : await ctx.modelRegistry.getAvailable();
    const available = [...new Set(models.map((model) => `${model.provider}/${model.id}`))].sort((a, b) => a.localeCompare(b));
    if (current !== 'auto' && !available.includes(current)) available.unshift(current);

    const choices: Choice<string>[] = [
        { label: '跟随当前会话', value: 'auto' },
        ...available.map((value) => ({ label: value, value })),
    ];
    const options = [
        ...choices.map((choice) => `${choice.value === current ? '●' : '○'} ${choice.label}`),
        '手动输入 provider/model…',
    ];
    const selected = await ctx.ui.select('总结模型', options);
    if (!selected) return current;
    if (selected === options.at(-1)) {
        const entered = (await ctx.ui.input('总结模型', `provider/model，当前：${current}`))?.trim();
        if (!entered) return current;
        const separator = entered.indexOf('/');
        if (separator <= 0 || separator === entered.length - 1) {
            ctx.ui.notify('模型格式应为 provider/model', 'warning');
            return current;
        }
        return entered;
    }
    return choices[options.indexOf(selected)]?.value ?? current;
};

const menuItems = (settings: MemorySettings): Array<{ key: MenuKey; label: string }> => [
    { key: 'maxMemories', label: `记忆数量上限 · ${settings.maxMemories}` },
    { key: 'auto', label: `自动总结 · ${enabledName(settings.summarize.auto)}` },
    { key: 'model', label: `总结模型 · ${settings.summarize.model}` },
    { key: 'thinking', label: `Thinking · ${settings.summarize.thinking}` },
    { key: 'resultDisplay', label: `结果通知 · ${displayName(settings.summarize.resultDisplay)}` },
    { key: 'includeToolMessages', label: `Tool Messages · ${includedName(settings.summarize.includeToolMessages)}` },
    { key: 'includeThinking', label: `Thinking 内容 · ${includedName(settings.summarize.includeThinking)}` },
    { key: 'save', label: '保存并退出' },
    { key: 'cancel', label: '取消' },
];

export const configureMemorySettings = async (
    ctx: ExtensionContext,
    current: MemorySettings,
): Promise<MemorySettings | undefined> => {
    const draft = structuredClone(current);
    const original = JSON.stringify(current);

    while (true) {
        const dirty = JSON.stringify(draft) !== original;
        const items = menuItems(draft);
        const selected = await ctx.ui.select(`Memory 设置${dirty ? '  ·  未保存' : ''}`, items.map((item) => item.label));
        if (!selected) {
            if (!dirty || await ctx.ui.confirm('放弃更改？', '当前修改尚未保存。')) return undefined;
            continue;
        }
        const key = items.find((item) => item.label === selected)?.key;

        if (key === 'cancel') {
            if (!dirty || await ctx.ui.confirm('放弃更改？', '当前修改尚未保存。')) return undefined;
            continue;
        }
        if (key === 'save') {
            await writeMemorySettings(draft);
            return draft;
        }
        if (key === 'maxMemories') {
            const entered = (await ctx.ui.input('记忆数量上限', `正整数，当前：${draft.maxMemories}`))?.trim();
            if (!entered) continue;
            const value = Number(entered);
            if (!Number.isInteger(value) || value <= 0) {
                ctx.ui.notify('记忆数量上限必须是正整数', 'warning');
                continue;
            }
            draft.maxMemories = value;
        } else if (key === 'auto') {
            draft.summarize.auto = await chooseBoolean(ctx, '自动总结', draft.summarize.auto);
        } else if (key === 'model') {
            draft.summarize.model = await chooseModel(ctx, draft.summarize.model);
        } else if (key === 'thinking') {
            draft.summarize.thinking = await choose(ctx, '总结 Thinking', THINKING_CHOICES, draft.summarize.thinking);
        } else if (key === 'resultDisplay') {
            draft.summarize.resultDisplay = await choose(ctx, '结果通知', RESULT_DISPLAY_CHOICES, draft.summarize.resultDisplay);
        } else if (key === 'includeToolMessages') {
            draft.summarize.includeToolMessages = await chooseBoolean(
                ctx,
                '总结上下文包含 Tool Call 和 Tool Result（Memory Tools 始终排除）',
                draft.summarize.includeToolMessages,
            );
        } else if (key === 'includeThinking') {
            draft.summarize.includeThinking = await chooseBoolean(ctx, '总结上下文包含 Thinking', draft.summarize.includeThinking);
        }
    }
};
