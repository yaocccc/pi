import type { ExtensionAPI } from '@earendil-works/pi-coding-agent';
import { Editor, type EditorTheme, Key, matchesKey, Text, truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';
import { Type } from 'typebox';
import {
    AskQuestionTelegramBridge,
    normalizeAskQuestionOptions,
    type AskQuestionResult,
} from './telegram-bridge.ts';

interface AskQuestionDetails {
    question: string;
    options: string[];
    answer: string | string[] | null;
    multiSelect: boolean;
    wasCustom?: boolean;
    customAnswers?: string[];
}

type DisplayOption = {
    label: string;
    isCustom?: boolean;
};

const TEXTAREA_RGB = '16;24;39';
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = '\x1b[49m';
const RESET_FG = '\x1b[39m';

const AskQuestionParams = Type.Object({
    question: Type.String({ description: '要展示给用户的问题。' }),
    options: Type.Array(Type.String({ description: '给用户选择的简短选项。建议 2-6 个。' }), {
        description: '可供用户选择的选项。工具会自动追加“自己输入”选项。',
        minItems: 1,
        maxItems: 8,
    }),
    multiSelect: Type.Optional(Type.Boolean({ description: '是否允许多选。true 时界面显示复选框，用户可勾选多项后提交。' })),
});

const formatList = (values: string[]): string => values.join('、');

const padToWidth = (text: string, width: number): string => {
    const safeWidth = Math.max(0, width);
    const truncated = truncateToWidth(text, safeWidth, '');
    return truncated + ' '.repeat(Math.max(0, safeWidth - visibleWidth(truncated)));
};

// Editor 光标内部可能用 \x1b[0m 重置样式；重置后重新补上背景色，保证整行都有 #101827 背景。
const textAreaBg = (text: string): string => TEXTAREA_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${TEXTAREA_BG}`) + RESET_BG;

const halfBlockLine = (width: number, position: 'top' | 'bottom'): string => TEXTAREA_FG + (position === 'top' ? '▄' : '▀').repeat(Math.max(0, width)) + RESET_FG;

// 让内嵌输入框的边框和 #101827 背景同色，视觉上与主输入框保持一致。
const invisibleBorder = (text: string): string => TEXTAREA_FG + text + RESET_FG;

const askQuestion = (pi: ExtensionAPI) => {
    const telegramBridge = new AskQuestionTelegramBridge(pi.events);

    pi.registerTool({
        name: 'ask_question',
        label: '提问用户',
        description: '向用户提一个问题，并让用户从选项中选择、复选多项或自己输入。需要用户决策、确认或补充信息时使用。',
        promptSnippet: '向用户提问，展示单选/多选选项，并允许用户自己输入答案',
        promptGuidelines: [
            '当你需要用户决策、确认方案或补充信息才能继续时，必须调用 ask_question，而不要只在普通文本里提问。',
            '调用 ask_question 时，提供 2-6 个清晰、互斥或可并选的选项；如果不确定用户偏好，也给出“由 pi 自行判断后继续”之类的选项。',
            '如果问题本身允许用户同时选择多个答案，调用 ask_question 时必须设置 multiSelect: true，界面会显示复选框。',
            '不要为了回答用户提出的问题而调用 ask_question；只有你需要反问用户时才使用。',
        ],
        parameters: AskQuestionParams,

        async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
            const options = normalizeAskQuestionOptions(params.options);
            const multiSelect = params.multiSelect === true;
            const noninteractiveResult = () => ({
                content: [{ type: 'text' as const, text: `需要询问用户：${params.question}\n${multiSelect ? '可多选：' : '选项：'}${options.join(' / ')}` }],
                details: { question: params.question, options, answer: null, multiSelect } as AskQuestionDetails,
            });

            if (!ctx.hasUI) return noninteractiveResult();

            let result: AskQuestionResult | null;
            if (ctx.mode !== 'tui') {
                const pendingTelegramAnswer = telegramBridge.wait(_toolCallId, _signal);
                if (!pendingTelegramAnswer) return noninteractiveResult();
                try {
                    result = await pendingTelegramAnswer;
                } finally {
                    telegramBridge.cancel(_toolCallId);
                }
            } else {
                const allOptions: DisplayOption[] = [...options.map((label) => ({ label })), { label: '自己输入…', isCustom: true }];
                let finishFromTelegram: ((answer: AskQuestionResult) => void) | undefined;
                const detachTelegram = telegramBridge.onAnswer(_toolCallId, (answer) => finishFromTelegram?.(answer));
                try {
                    result = await ctx.ui.custom<AskQuestionResult | null>((tui, theme, _keybindings, done) => {
                finishFromTelegram = done;
                const finishLocally = (answer: AskQuestionResult | null) => {
                    if (telegramBridge.settleLocally(_toolCallId)) done(answer);
                };
                let selectedIndex = 0;
                let inputMode = false;
                let warning: string | undefined;
                let cachedLines: string[] | undefined;
                const checked = new Set<number>();
                const customAnswers: string[] = [];

                const editorTheme: EditorTheme = {
                    borderColor: invisibleBorder,
                    selectList: {
                        selectedPrefix: (s) => theme.fg('accent', s),
                        selectedText: (s) => theme.fg('accent', s),
                        description: (s) => theme.fg('muted', s),
                        scrollInfo: (s) => theme.fg('dim', s),
                        noMatch: (s) => theme.fg('warning', s),
                    },
                };
                const editor = new Editor(tui, editorTheme);

                const refresh = () => {
                    cachedLines = undefined;
                    tui.requestRender();
                };

                const openCustomInput = () => {
                    inputMode = true;
                    warning = undefined;
                    editor.setText('');
                    refresh();
                };

                const finishMultiSelect = () => {
                    const selectedIndices = Array.from(checked).sort((a, b) => a - b);
                    const selectedAnswers = selectedIndices.map((index) => allOptions[index]?.label).filter((label): label is string => Boolean(label));
                    const answers = [...selectedAnswers, ...customAnswers];

                    if (answers.length === 0) {
                        warning = '请至少勾选一项，或选择“自己输入…”。';
                        refresh();
                        return;
                    }

                    finishLocally({
                        answers,
                        customAnswers: [...customAnswers],
                        wasCustom: selectedAnswers.length === 0 && customAnswers.length > 0,
                    });
                };

                const toggleOption = (index: number) => {
                    const option = allOptions[index];
                    if (!option) return;
                    warning = undefined;

                    if (option.isCustom) {
                        openCustomInput();
                        return;
                    }

                    if (checked.has(index)) checked.delete(index);
                    else checked.add(index);
                    refresh();
                };

                const submitOption = (index: number) => {
                    const option = allOptions[index];
                    if (!option) return;
                    if (option.isCustom) {
                        openCustomInput();
                        return;
                    }
                    finishLocally({ answers: [option.label], wasCustom: false });
                };

                editor.onSubmit = (value) => {
                    const answer = value.trim();
                    if (!answer) {
                        inputMode = false;
                        editor.setText('');
                        refresh();
                        return;
                    }

                    if (multiSelect) {
                        customAnswers.push(answer);
                        inputMode = false;
                        editor.setText('');
                        warning = undefined;
                        refresh();
                        return;
                    }

                    finishLocally({ answers: [answer], wasCustom: true });
                };

                const handleInput = (data: string) => {
                    if (inputMode) {
                        if (matchesKey(data, Key.escape)) {
                            inputMode = false;
                            editor.setText('');
                            refresh();
                            return;
                        }
                        editor.handleInput(data);
                        refresh();
                        return;
                    }

                    if (matchesKey(data, Key.up)) {
                        selectedIndex = Math.max(0, selectedIndex - 1);
                        warning = undefined;
                        refresh();
                        return;
                    }
                    if (matchesKey(data, Key.down)) {
                        selectedIndex = Math.min(allOptions.length - 1, selectedIndex + 1);
                        warning = undefined;
                        refresh();
                        return;
                    }
                    if (matchesKey(data, Key.space)) {
                        if (multiSelect) toggleOption(selectedIndex);
                        else submitOption(selectedIndex);
                        return;
                    }
                    if (matchesKey(data, Key.enter)) {
                        if (multiSelect) {
                            if (allOptions[selectedIndex]?.isCustom) openCustomInput();
                            else finishMultiSelect();
                        } else {
                            submitOption(selectedIndex);
                        }
                        return;
                    }
                    if (matchesKey(data, Key.escape)) {
                        finishLocally(null);
                    }
                };

                const addLine = (lines: string[], width: number, text = '') => lines.push(textAreaBg(padToWidth(text, width)));

                const renderOptionLabel = (option: DisplayOption, index: number): string => {
                    if (!multiSelect) return `${option.label}${option.isCustom && inputMode ? ' ✎' : ''}`;
                    if (option.isCustom) return `[+] ${option.label}${inputMode ? ' ✎' : ''}`;
                    return `${checked.has(index) ? '[x]' : '[ ]'} ${option.label}`;
                };

                const render = (width: number): string[] => {
                    if (cachedLines) return cachedLines;

                    const lines: string[] = [];
                    lines.push(halfBlockLine(width, 'top'));
                    addLine(lines, width, theme.fg('accent', ' ？') + theme.fg('text', ` ${params.question}`));
                    if (multiSelect) {
                        const count = checked.size + customAnswers.length;
                        addLine(lines, width, theme.fg('dim', ` 多选模式：已选 ${count} 项`));
                    }
                    addLine(lines, width);

                    for (let i = 0; i < allOptions.length; i++) {
                        const option = allOptions[i]!;
                        const selected = i === selectedIndex;
                        const prefix = selected ? theme.fg('accent', '> ') : '  ';
                        const color = selected ? 'accent' : option.isCustom ? 'muted' : 'text';
                        addLine(lines, width, prefix + theme.fg(color, renderOptionLabel(option, i)));

                        if (multiSelect && option.isCustom && customAnswers.length > 0) {
                            for (const answer of customAnswers) {
                                addLine(lines, width, `     ${theme.fg('success', '[x]')} ${theme.fg('text', answer)}`);
                            }
                        }
                    }

                    if (inputMode) {
                        addLine(lines, width);
                        addLine(lines, width, theme.fg('muted', ' 请输入你的答案：'));
                        for (const line of editor.render(Math.max(1, width - 2))) {
                            addLine(lines, width, ` ${line}`);
                        }
                    }

                    if (warning) {
                        addLine(lines, width);
                        addLine(lines, width, theme.fg('warning', ` ${warning}`));
                    }

                    addLine(lines, width);
                    addLine(
                        lines,
                        width,
                        inputMode
                            ? theme.fg('dim', ' Enter 提交 • Esc 返回选项')
                            : multiSelect
                              ? theme.fg('dim', ' ↑↓ 选择 • 空格勾选 • Enter 完成/自定义 • Esc 取消')
                              : theme.fg('dim', ' ↑↓ 选择 • Enter 确认 • Esc 取消'),
                    );
                    lines.push(halfBlockLine(width, 'bottom'));

                    cachedLines = lines;
                    return lines;
                };

                return {
                    get focused() {
                        return editor.focused;
                    },
                    set focused(value: boolean) {
                        editor.focused = value;
                    },
                    render,
                    invalidate: () => {
                        cachedLines = undefined;
                        editor.invalidate();
                    },
                    handleInput,
                };
                    });
                } finally {
                    finishFromTelegram = undefined;
                    detachTelegram();
                    telegramBridge.settleLocally(_toolCallId);
                    telegramBridge.release(_toolCallId);
                }
            }

            if (!result) {
                return {
                    content: [{ type: 'text', text: '用户取消了选择。' }],
                    details: { question: params.question, options, answer: null, multiSelect } as AskQuestionDetails,
                };
            }

            if (multiSelect) {
                return {
                    content: [{ type: 'text', text: `用户选择了 ${result.answers.length} 项：\n${formatList(result.answers)}` }],
                    details: {
                        question: params.question,
                        options,
                        answer: result.answers,
                        multiSelect: true,
                        wasCustom: result.wasCustom,
                        customAnswers: result.customAnswers,
                    } as AskQuestionDetails,
                };
            }

            const answer = result.answers[0] ?? '';
            if (result.wasCustom) {
                return {
                    content: [{ type: 'text', text: `用户输入：${answer}` }],
                    details: {
                        question: params.question,
                        options,
                        answer,
                        multiSelect: false,
                        wasCustom: true,
                    } as AskQuestionDetails,
                };
            }

            return {
                content: [{ type: 'text', text: `用户选择：${answer}` }],
                details: {
                    question: params.question,
                    options,
                    answer,
                    multiSelect: false,
                    wasCustom: false,
                } as AskQuestionDetails,
            };
        },

        renderCall(args, theme) {
            const options = Array.isArray(args.options) ? normalizeAskQuestionOptions(args.options as string[]) : [];
            const multiSelect = args.multiSelect === true;
            const prefix = multiSelect ? '[多选] ' : '';
            const optionSummary = [...options.map((option) => `${multiSelect ? '[ ] ' : ''}${option}`), '自己输入…'].join('  ');
            const text =
                theme.fg('toolTitle', theme.bold('ask_question ')) +
                theme.fg('muted', prefix + (args.question ?? '')) +
                (optionSummary ? `\n${theme.fg('dim', `  ${optionSummary}`)}` : '');
            return new Text(text, 0, 0);
        },

        renderResult(result, _options, theme) {
            const details = result.details as AskQuestionDetails | undefined;
            if (!details) {
                const first = result.content[0];
                return new Text(first?.type === 'text' ? first.text : '', 0, 0);
            }
            if (details.answer === null) {
                return new Text(theme.fg('warning', '已取消'), 0, 0);
            }
            if (Array.isArray(details.answer)) {
                const lines = details.answer.map((answer) => `${theme.fg('success', '✓ ')}${theme.fg('accent', answer)}`);
                return new Text(lines.join('\n'), 0, 0);
            }
            if (details.wasCustom) {
                return new Text(theme.fg('success', '✓ ') + theme.fg('muted', '自己输入：') + theme.fg('accent', details.answer), 0, 0);
            }
            return new Text(theme.fg('success', '✓ ') + theme.fg('accent', details.answer), 0, 0);
        },
    });

    pi.on('session_shutdown', () => {
        telegramBridge.dispose();
    });

    pi.on('before_agent_start', (event, ctx) => {
        if (!ctx.hasUI) return undefined;
        return {
            systemPrompt:
                `${event.systemPrompt}\n\n` +
                '用户交互规则：如果你需要向用户反问、让用户做选择、确认方案或补充信息才能继续，必须调用 ask_question 工具。' +
                '不要只用普通文本提出需要用户回答的问题。ask_question 支持单选和多选；多选时设置 multiSelect: true，会展示复选框，并允许用户自己输入。' +
                '如果你能基于现有信息继续完成任务，就不要提问。',
        };
    });
};

export default askQuestion;
