import { clampThinkingLevel } from '@earendil-works/pi-ai/compat';
import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { mkdir, readFile, rename, unlink, writeFile } from 'node:fs/promises';
import { dirname } from 'node:path';
import { MEMORY_SETTINGS_PATH } from './constants';
import { asObj } from './utils';

export type MemoryThinking = 'auto' | 'off' | 'minimal' | 'low' | 'medium' | 'high' | 'xhigh' | 'max';
export type MemoryResultDisplay = 'message' | 'popup' | 'none';

export type MemorySettings = {
    maxMemories: number;
    summarize: {
        auto: boolean;
        model: string;
        thinking: MemoryThinking;
        resultDisplay: MemoryResultDisplay;
        includeToolMessages: boolean;
        includeThinking: boolean;
    };
};

export const DEFAULT_MEMORY_SETTINGS: MemorySettings = {
    maxMemories: 100,
    summarize: {
        auto: true,
        model: 'auto',
        thinking: 'auto',
        resultDisplay: 'popup',
        includeToolMessages: false,
        includeThinking: false,
    },
};

const THINKING_VALUES = new Set<MemoryThinking>(['auto', 'off', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max']);
const RESULT_DISPLAY_VALUES = new Set<MemoryResultDisplay>(['message', 'popup', 'none']);
const boolOr = (value: unknown, fallback: boolean): boolean => typeof value === 'boolean' ? value : fallback;
const stringOr = (value: unknown, fallback: string): string => typeof value === 'string' && value.trim() ? value.trim() : fallback;
const maxMemoriesOf = (value: unknown): number => typeof value === 'number' && Number.isInteger(value) && value > 0
    ? value
    : DEFAULT_MEMORY_SETTINGS.maxMemories;

export const readMemorySettings = async (): Promise<MemorySettings> => {
    try {
        const root = asObj(JSON.parse(await readFile(MEMORY_SETTINGS_PATH, 'utf8')));
        const summarize = asObj(root?.summarize);
        const thinking = stringOr(summarize?.thinking, DEFAULT_MEMORY_SETTINGS.summarize.thinking) as MemoryThinking;
        const resultDisplay = stringOr(summarize?.resultDisplay, DEFAULT_MEMORY_SETTINGS.summarize.resultDisplay) as MemoryResultDisplay;
        return {
            maxMemories: maxMemoriesOf(root?.maxMemories),
            summarize: {
                auto: boolOr(summarize?.auto, DEFAULT_MEMORY_SETTINGS.summarize.auto),
                model: stringOr(summarize?.model, DEFAULT_MEMORY_SETTINGS.summarize.model),
                thinking: THINKING_VALUES.has(thinking) ? thinking : DEFAULT_MEMORY_SETTINGS.summarize.thinking,
                resultDisplay: RESULT_DISPLAY_VALUES.has(resultDisplay) ? resultDisplay : DEFAULT_MEMORY_SETTINGS.summarize.resultDisplay,
                includeToolMessages: boolOr(
                    summarize?.includeToolMessages,
                    boolOr(summarize?.includeToolResults, DEFAULT_MEMORY_SETTINGS.summarize.includeToolMessages),
                ),
                includeThinking: boolOr(summarize?.includeThinking, DEFAULT_MEMORY_SETTINGS.summarize.includeThinking),
            },
        };
    } catch {
        return {
            maxMemories: DEFAULT_MEMORY_SETTINGS.maxMemories,
            summarize: { ...DEFAULT_MEMORY_SETTINGS.summarize },
        };
    }
};

export const writeMemorySettings = async (settings: MemorySettings): Promise<void> => {
    await mkdir(dirname(MEMORY_SETTINGS_PATH), { recursive: true });
    const tempPath = `${MEMORY_SETTINGS_PATH}.tmp-${process.pid}-${Date.now()}`;
    const serialized = `${JSON.stringify({ $schemaVersion: 1, ...settings }, null, 2)}\n`;
    try {
        await writeFile(tempPath, serialized, { encoding: 'utf8', mode: 0o600 });
        await rename(tempPath, MEMORY_SETTINGS_PATH);
    } catch (error) {
        await unlink(tempPath).catch(() => undefined);
        throw error;
    }
};

export const resolveSummaryModel = (ctx: ExtensionContext, settings: MemorySettings): NonNullable<ExtensionContext['model']> | undefined => {
    if (settings.summarize.model === 'auto') return ctx.model;
    const separator = settings.summarize.model.indexOf('/');
    if (separator <= 0 || separator === settings.summarize.model.length - 1) return ctx.model;
    const provider = settings.summarize.model.slice(0, separator);
    const modelId = settings.summarize.model.slice(separator + 1);
    return ctx.modelRegistry.find(provider, modelId) ?? ctx.model;
};

export const resolveSummaryThinking = (
    ctx: ExtensionContext,
    settings: MemorySettings,
    model: NonNullable<ExtensionContext['model']>,
) => {
    const level = clampThinkingLevel(model, settings.summarize.thinking === 'auto' ? (ctx.thinkingLevel ?? 'medium') : settings.summarize.thinking);
    return level === 'off' ? undefined : level;
};
