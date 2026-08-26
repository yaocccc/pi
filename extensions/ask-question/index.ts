import type { ExtensionAPI, ExtensionContext } from '@earendil-works/pi-coding-agent';
import { Editor, type EditorTheme, Key, matchesKey, Text, truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';
import { Type } from 'typebox';

interface AskQuestionDetails {
    question: string;
    options: string[];
    answer: string | string[] | null;
    multiSelect: boolean;
    wasCustom?: boolean;
    customAnswers?: string[];
}

interface AskQuestionResult {
    answers: string[];
    wasCustom?: boolean;
    customAnswers?: string[];
}

interface AskQuestionInput {
    question: string;
    options: string[];
    multiSelect?: boolean;
    label?: string;
}

interface QuestionnaireDetails {
    questions: Array<AskQuestionDetails & { label?: string }>;
    cancelled?: boolean;
}

type QuestionnaireAnswer = AskQuestionResult & { completed: boolean };

type DisplayOption = {
    label: string;
    isCustom?: boolean;
};

const TEXTAREA_RGB = '16;24;39';
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = '\x1b[49m';
const RESET_FG = '\x1b[39m';

const AskQuestionItemParams = Type.Object({
    question: Type.String({ description: '要展示给用户的问题。' }),
    options: Type.Array(Type.String({ description: '给用户选择的简短选项。建议 2-6 个。' }), {
        description: '可供用户选择的选项。工具会自动追加“自己输入”选项。',
        minItems: 1,
        maxItems: 8,
    }),
    multiSelect: Type.Optional(Type.Boolean({ description: '是否允许多选。true 时界面显示复选框，用户可勾选多项后提交。' })),
    label: Type.Optional(Type.String({ description: '导航标签中的简短名称；未提供时显示为“问题 1”等。' })),
});

// 不使用 Type.Union，以兼容 Google 工具 schema。
const AskQuestionParams = Type.Object({
    question: Type.Optional(Type.String({ description: '要展示给用户的问题。与 options 一起用于单个问题。' })),
    options: Type.Optional(Type.Array(Type.String({ description: '给用户选择的简短选项。建议 2-6 个。' }), {
        description: '单个问题的选项。工具会自动追加“自己输入”选项。',
        minItems: 1,
        maxItems: 8,
    })),
    multiSelect: Type.Optional(Type.Boolean({ description: '单个问题是否允许多选。' })),
    questions: Type.Optional(Type.Array(AskQuestionItemParams, {
        description: '一次展示的多个问题。多个问题会显示导航标签和最终提交页。',
        minItems: 1,
        maxItems: 8,
    })),
});

const normalizeOptions = (options: string[]): string[] => {
    const seen = new Set<string>();
    const result: string[] = [];

    for (const option of options) {
        const label = option.replace(/[\r\n\t]+/g, ' ').trim();
        if (!label || seen.has(label)) continue;
        seen.add(label);
        result.push(label);
    }

    return result.slice(0, 8);
};

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

const questionnaireDetails = (questions: AskQuestionInput[], answers: Array<QuestionnaireAnswer | undefined>): QuestionnaireDetails => ({
    questions: questions.map((question, index) => {
        const answer = answers[index];
        const options = normalizeOptions(question.options);
        const multiSelect = question.multiSelect === true;
        return {
            question: question.question,
            label: question.label,
            options,
            answer: answer?.completed ? (multiSelect ? answer.answers : (answer.answers[0] ?? '')) : null,
            multiSelect,
            wasCustom: answer?.wasCustom,
            customAnswers: answer?.customAnswers,
        };
    }),
});

const executeQuestionnaire = async (params: { questions: AskQuestionInput[] }, ctx: ExtensionContext) => {
    const questions = params.questions.map((question) => ({
        question: question.question,
        label: question.label?.replace(/[\r\n\t]+/g, ' ').trim() || undefined,
        options: normalizeOptions(question.options),
        multiSelect: question.multiSelect === true,
    }));

    if (!ctx.hasUI) {
        const details = questionnaireDetails(questions, []);
        return {
            content: [{ type: 'text' as const, text: `需要询问用户（${questions.length} 个问题）：\n${questions.map((q, i) => `${q.label || `问题 ${i + 1}`}：${q.question}\n${q.multiSelect ? '可多选：' : '选项：'}${q.options.join(' / ')}`).join('\n')}` }],
            details,
        };
    }

    const result = await ctx.ui.custom<{ answers: Array<QuestionnaireAnswer | undefined>; cancelled: boolean } | null>((tui, theme, _keybindings, done) => {
        let currentTab = 0;
        let inputMode = false;
        let warning: string | undefined;
        let cachedLines: string[] | undefined;
        let cachedWidth: number | undefined;
        const selectedIndices = questions.map(() => 0);
        const checked = questions.map(() => new Set<number>());
        const customAnswers = questions.map(() => [] as string[]);
        const answers: Array<QuestionnaireAnswer | undefined> = questions.map(() => undefined);

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
            cachedWidth = undefined;
            tui.requestRender();
        };
        const isSubmitTab = () => currentTab === questions.length;
        const currentQuestion = () => questions[currentTab];
        const displayOptions = (index: number): DisplayOption[] => [
            ...questions[index]!.options.map((label) => ({ label })),
            { label: '自己输入…', isCustom: true },
        ];
        const allAnswered = () => answers.every((answer) => answer?.completed);
        const labelFor = (index: number) => questions[index]!.label || `问题 ${index + 1}`;
        const nextTab = () => {
            currentTab = (currentTab + 1) % (questions.length + 1);
            warning = undefined;
            refresh();
        };
        const previousTab = () => {
            currentTab = (currentTab - 1 + questions.length + 1) % (questions.length + 1);
            warning = undefined;
            refresh();
        };
        const advanceAfterAnswer = () => {
            currentTab = currentTab < questions.length - 1 ? currentTab + 1 : questions.length;
            warning = undefined;
            refresh();
        };
        const openCustomInput = () => {
            inputMode = true;
            warning = undefined;
            editor.setText('');
            refresh();
        };
        const saveSingle = (answer: string, wasCustom: boolean) => {
            answers[currentTab] = { answers: [answer], wasCustom, completed: true };
            advanceAfterAnswer();
        };
        const saveMulti = () => {
            const selectedAnswers = Array.from(checked[currentTab]!).sort((a, b) => a - b)
                .map((index) => displayOptions(currentTab)[index]?.label)
                .filter((label): label is string => Boolean(label));
            const selected = [...selectedAnswers, ...customAnswers[currentTab]!];
            if (selected.length === 0) {
                warning = '请至少勾选一项，或选择“自己输入…”。';
                refresh();
                return;
            }
            answers[currentTab] = {
                answers: selected,
                customAnswers: [...customAnswers[currentTab]!],
                wasCustom: selectedAnswers.length === 0 && customAnswers[currentTab]!.length > 0,
                completed: true,
            };
            advanceAfterAnswer();
        };

        editor.onSubmit = (value) => {
            const answer = value.trim();
            if (!answer) {
                inputMode = false;
                editor.setText('');
                refresh();
                return;
            }
            const question = currentQuestion()!;
            if (question.multiSelect) {
                customAnswers[currentTab]!.push(answer);
                answers[currentTab] = undefined;
                inputMode = false;
                editor.setText('');
                refresh();
                return;
            }
            inputMode = false;
            editor.setText('');
            saveSingle(answer, true);
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
            if (matchesKey(data, Key.escape)) {
                done({ answers: [...answers], cancelled: true });
                return;
            }
            if (matchesKey(data, Key.left) || matchesKey(data, Key.shift('tab'))) {
                previousTab();
                return;
            }
            if (matchesKey(data, Key.right) || matchesKey(data, Key.tab)) {
                nextTab();
                return;
            }
            if (isSubmitTab()) {
                if (matchesKey(data, Key.enter)) {
                    if (allAnswered()) {
                        done({ answers: answers.map((answer) => answer!), cancelled: false });
                    } else {
                        const missing = questions.map((_question, index) => index).filter((index) => !answers[index]?.completed).map(labelFor);
                        warning = `尚未回答：${missing.join('、')}`;
                        refresh();
                    }
                }
                return;
            }

            const question = currentQuestion()!;
            const options = displayOptions(currentTab);
            const selectedIndex = selectedIndices[currentTab]!;
            if (matchesKey(data, Key.up)) {
                selectedIndices[currentTab] = Math.max(0, selectedIndex - 1);
                warning = undefined;
                refresh();
                return;
            }
            if (matchesKey(data, Key.down)) {
                selectedIndices[currentTab] = Math.min(options.length - 1, selectedIndex + 1);
                warning = undefined;
                refresh();
                return;
            }
            const option = options[selectedIndex];
            if (question.multiSelect && matchesKey(data, Key.space)) {
                if (option?.isCustom) openCustomInput();
                else if (option) {
                    if (checked[currentTab]!.has(selectedIndex)) checked[currentTab]!.delete(selectedIndex);
                    else checked[currentTab]!.add(selectedIndex);
                    answers[currentTab] = undefined;
                    warning = undefined;
                    refresh();
                }
                return;
            }
            if (matchesKey(data, Key.enter) || (!question.multiSelect && matchesKey(data, Key.space))) {
                if (option?.isCustom) {
                    openCustomInput();
                } else if (option) {
                    if (question.multiSelect) saveMulti();
                    else saveSingle(option.label, false);
                }
            }
        };

        const addLine = (lines: string[], width: number, text = '') => lines.push(textAreaBg(padToWidth(text, width)));
        const render = (width: number): string[] => {
            if (cachedLines && cachedWidth === width) return cachedLines;
            const lines: string[] = [];
            lines.push(halfBlockLine(width, 'top'));

            // Header 统一使用问卷面板的实色背景；当前项仅通过强调色和粗体区分。
            const tabs = questions.map((question, index) => {
                const active = currentTab === index;
                const status = answers[index]?.completed ? '■' : '□';
                const text = ` ${status} ${labelFor(index)} `;
                return active
                    ? theme.fg('accent', theme.bold(text))
                    : theme.fg(answers[index]?.completed ? 'success' : 'muted', text);
            });
            const submitText = ' ✓ 提交 ';
            tabs.push(currentTab === questions.length
                ? theme.fg('accent', theme.bold(submitText))
                : theme.fg(allAnswered() ? 'success' : 'dim', submitText));
            addLine(lines, width, tabs.join(theme.fg('dim', '│')));
            addLine(lines, width);

            if (isSubmitTab()) {
                addLine(lines, width, theme.fg('accent', ' 提交前确认'));
                addLine(lines, width);
                for (let index = 0; index < questions.length; index++) {
                    const answer = answers[index];
                    const value = answer?.completed ? formatList(answer.answers) : '尚未回答';
                    addLine(lines, width, theme.fg(answer?.completed ? 'text' : 'warning', `    ${labelFor(index)}：${value}`));
                }
                if (warning) addLine(lines, width, theme.fg('warning', `    ${warning}`));
            } else {
                const question = currentQuestion()!;
                const options = displayOptions(currentTab);
                const selectedIndex = selectedIndices[currentTab]!;
                addLine(lines, width, theme.fg('accent', ` ${labelFor(currentTab)}`) + theme.fg('text', ` ${question.question}`));
                if (question.multiSelect) addLine(lines, width, theme.fg('dim', ` 多选模式：已选 ${checked[currentTab]!.size + customAnswers[currentTab]!.length} 项`));
                addLine(lines, width);
                for (let index = 0; index < options.length; index++) {
                    const option = options[index]!;
                    const prefix = index === selectedIndex ? theme.fg('accent', '> ') : '  ';
                    const label = question.multiSelect
                        ? option.isCustom ? `[+] ${option.label}${inputMode ? ' ✎' : ''}` : `${checked[currentTab]!.has(index) ? '[✓]' : '[ ]'} ${option.label}`
                        : `${option.label}${option.isCustom && inputMode ? ' ✎' : ''}`;
                    addLine(lines, width, prefix + theme.fg(index === selectedIndex ? 'accent' : option.isCustom ? 'muted' : 'text', label));
                    if (question.multiSelect && option.isCustom) {
                        for (const answer of customAnswers[currentTab]!) addLine(lines, width, `     ${theme.fg('success', '[✓]')} ${theme.fg('text', answer)}`);
                    }
                }
                if (inputMode) {
                    addLine(lines, width);
                    addLine(lines, width, theme.fg('muted', ' 请输入你的答案：'));
                    for (const line of editor.render(Math.max(1, width - 2))) addLine(lines, width, ` ${line}`);
                }
                if (warning) addLine(lines, width, theme.fg('warning', ` ${warning}`));
            }
            addLine(lines, width);
            addLine(lines, width, inputMode
                ? theme.fg('dim', ' Enter 提交 • Esc 返回选项')
                : isSubmitTab()
                    ? theme.fg('dim', ' ←→ 返回问题 • Enter 提交 • Esc 取消')
                    : theme.fg('dim', currentQuestion()!.multiSelect ? ' ←→ 切换 • ↑↓ 选择 • 空格勾选 • Enter 确认 • Esc 取消' : ' ←→ 切换 • ↑↓ 选择 • Enter 确认 • Esc 取消'));
            lines.push(halfBlockLine(width, 'bottom'));
            cachedWidth = width;
            cachedLines = lines;
            return lines;
        };

        return {
            get focused() { return editor.focused; },
            set focused(value: boolean) { editor.focused = value; },
            render,
            invalidate: () => { cachedLines = undefined; cachedWidth = undefined; editor.invalidate(); },
            handleInput,
        };
    });

    const indexedAnswers = questions.map((_question, index) => result?.answers[index]);
    const details = questionnaireDetails(questions, indexedAnswers);
    details.cancelled = !result || result.cancelled;
    if (!result || result.cancelled) {
        return { content: [{ type: 'text' as const, text: '用户取消了问卷。' }], details };
    }
    const summary = details.questions.map((question, index) => `${question.label || `问题 ${index + 1}`}（${question.question}）：${Array.isArray(question.answer) ? formatList(question.answer) : question.answer}`).join('\n');
    return { content: [{ type: 'text' as const, text: `用户回答了 ${questions.length} 个问题：\n${summary}` }], details };
};

const askQuestion = (pi: ExtensionAPI) => {
    pi.registerTool({
        name: 'ask_question',
        label: '提问用户',
        description: '向用户提一个或多个问题，让用户从选项中选择、复选多项或自己输入。多个问题可逐题导航并在最后统一提交。需要用户决策、确认或补充信息时使用。',
        promptSnippet: '向用户提问，支持单选/多选及一次展示多个问题，并允许用户自己输入答案',
        promptGuidelines: [
            '当你需要用户决策、确认方案或补充信息才能继续时，必须调用 ask_question，而不要只在普通文本里提问。',
            '调用 ask_question 时，提供 2-6 个清晰、互斥或可并选的选项；如果不确定用户偏好，也给出“由 pi 自行判断后继续”之类的选项。',
            '如果问题本身允许用户同时选择多个答案，调用 ask_question 时必须设置 multiSelect: true，界面会显示复选框。',
            '需要连续收集多个相关决策时，可传入 questions 数组（每项包含 question、options、可选 multiSelect 和 label）；用户会逐题导航并在最后统一提交。单个问题继续使用原有 question 和 options 形状。',
            '不要为了回答用户提出的问题而调用 ask_question；只有你需要反问用户时才使用。',
        ],
        parameters: AskQuestionParams,

        async execute(_toolCallId, params, _signal, _onUpdate, ctx) {
            if (params.questions && params.questions.length > 0) {
                return executeQuestionnaire({ questions: params.questions as AskQuestionInput[] }, ctx);
            }

            if (typeof params.question !== 'string' || !Array.isArray(params.options)) {
                throw new Error('ask_question 需要提供 question 和 options，或提供非空的 questions 数组。');
            }

            const options = normalizeOptions(params.options);
            const multiSelect = params.multiSelect === true;

            if (!ctx.hasUI) {
                return {
                    content: [{ type: 'text', text: `需要询问用户：${params.question}\n${multiSelect ? '可多选：' : '选项：'}${options.join(' / ')}` }],
                    details: { question: params.question, options, answer: null, multiSelect } as AskQuestionDetails,
                };
            }

            const allOptions: DisplayOption[] = [...options.map((label) => ({ label })), { label: '自己输入…', isCustom: true }];

            const result = await ctx.ui.custom<AskQuestionResult | null>((tui, theme, _keybindings, done) => {
                let selectedIndex = 0;
                let inputMode = false;
                let warning: string | undefined;
                let cachedLines: string[] | undefined;
                let cachedWidth: number | undefined;
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
                    cachedWidth = undefined;
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

                    done({
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
                    done({ answers: [option.label], wasCustom: false });
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

                    done({ answers: [answer], wasCustom: true });
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
                        done(null);
                    }
                };

                const addLine = (lines: string[], width: number, text = '') => lines.push(textAreaBg(padToWidth(text, width)));

                const renderOptionLabel = (option: DisplayOption, index: number): string => {
                    if (!multiSelect) return `${option.label}${option.isCustom && inputMode ? ' ✎' : ''}`;
                    if (option.isCustom) return `[+] ${option.label}${inputMode ? ' ✎' : ''}`;
                    return `${checked.has(index) ? '[✓]' : '[ ]'} ${option.label}`;
                };

                const render = (width: number): string[] => {
                    if (cachedLines && cachedWidth === width) return cachedLines;

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
                                addLine(lines, width, `     ${theme.fg('success', '[✓]')} ${theme.fg('text', answer)}`);
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

                    cachedWidth = width;
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
                        cachedWidth = undefined;
                        editor.invalidate();
                    },
                    handleInput,
                };
            });

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
            const questionnaire = Array.isArray(args.questions) ? args.questions as AskQuestionInput[] : undefined;
            if (questionnaire && questionnaire.length > 0) {
                const labels = questionnaire.map((question, index) => question.label || `问题 ${index + 1}`).join('、');
                return new Text(
                    theme.fg('toolTitle', theme.bold('ask_question ')) +
                    theme.fg('muted', `问卷（${questionnaire.length} 题）`) +
                    theme.fg('dim', ` ${labels}`),
                    0,
                    0,
                );
            }
            const options = Array.isArray(args.options) ? normalizeOptions(args.options as string[]) : [];
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
            const questionnaire = result.details as QuestionnaireDetails | undefined;
            if (questionnaire?.questions) {
                if (questionnaire.cancelled) return new Text(theme.fg('warning', '已取消问卷'), 0, 0);
                const lines = questionnaire.questions.map((question, index) => {
                    const label = question.label || `问题 ${index + 1}`;
                    const answer = question.answer;
                    if (answer === null) return theme.fg('warning', `! ${label}：尚未回答`);
                    const value = Array.isArray(answer) ? formatList(answer) : answer;
                    return theme.fg('success', '✓ ') + theme.fg('muted', `${label}：`) + theme.fg('accent', value);
                });
                return new Text(lines.join('\n'), 0, 0);
            }
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

    pi.on('before_agent_start', (event, ctx) => {
        if (!ctx.hasUI) return undefined;
        return {
            systemPrompt:
                `${event.systemPrompt}\n\n` +
                '用户交互规则：如果你需要向用户反问、让用户做选择、确认方案或补充信息才能继续，必须调用 ask_question 工具。' +
                '不要只用普通文本提出需要用户回答的问题。ask_question 支持单选和多选；多选时设置 multiSelect: true，会展示复选框，并允许用户自己输入。' +
                '需要一次收集多个相关答案时，使用 questions 数组；每项包含 question、options、可选 multiSelect 和用于导航的可选 label，用户会逐题回答并在最终提交页确认。' +
                '如果你能基于现有信息继续完成任务，就不要提问。',
        };
    });
};

export default askQuestion;
