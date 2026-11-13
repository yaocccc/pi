import type { ExtensionContext, ToolRenderers } from '@earendil-works/pi-coding-agent';
import { CURSOR_MARKER, Editor, type EditorTheme, Key, matchesKey, sliceByColumn, Text, truncateToWidth, visibleWidth, wrapTextWithAnsi } from '@earendil-works/pi-tui';
import { frameBorder } from './frame.ts';
import { customQuestion } from './dialog.ts';
import { normalizeOptions, formatList, type AskQuestionInput, type AskQuestionDetails, type QuestionnaireDetails, type QuestionnaireAnswer } from '../../ask-question/model.ts';

type DisplayOption = {
    label: string;
    isCustom?: boolean;
};

const padToWidth = (text: string, width: number): string => {
    const safeWidth = Math.max(0, width);
    const truncated = truncateToWidth(text, safeWidth, '');
    return truncated + ' '.repeat(Math.max(0, safeWidth - visibleWidth(truncated)));
};

const addQuestionHeading = (lines: string[], width: number, heading: string): void => {
    for (const line of wrapTextWithAnsi(heading, width)) lines.push(padToWidth(line, width));
};

// Both entry forms share selection, custom input and rendering. Only questionnaires
// expose navigation and require a final review; a single question submits immediately.
export const showQuestions = (questions: AskQuestionInput[], review: boolean, ctx: ExtensionContext, signal?: AbortSignal) =>
    customQuestion<{ answers: Array<QuestionnaireAnswer | undefined>; cancelled: boolean }>(ctx, signal, (tui, theme, _keybindings, done) => {
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
            borderColor: (text: string) => text,
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
        const switchTab = (direction: number) => {
            currentTab = (currentTab + direction + questions.length + 1) % (questions.length + 1);
            warning = undefined;
            refresh();
        };
        const advanceAfterAnswer = () => {
            if (!review) {
                done({ answers, cancelled: false });
                return;
            }
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
            answers[currentTab] = { answers: [answer], wasCustom, customAnswers: wasCustom ? [answer] : undefined, completed: true };
            advanceAfterAnswer();
        };
        const selectedValues = (questionIndex: number) => [...new Set([
            ...Array.from(checked[questionIndex]!).sort((a, b) => a - b)
                .map((index) => questions[questionIndex]!.options[index]!),
            ...customAnswers[questionIndex]!,
        ])];
        const saveMulti = () => {
            const selected = selectedValues(currentTab);
            if (selected.length === 0) {
                warning = '请至少勾选一项，或选择“自己输入…”。';
                refresh();
                return;
            }
            answers[currentTab] = {
                answers: selected,
                customAnswers: [...customAnswers[currentTab]!],
                wasCustom: customAnswers[currentTab]!.length > 0 && customAnswers[currentTab]!.length === selected.length,
                completed: true,
            };
            advanceAfterAnswer();
        };

        editor.onSubmit = (value) => {
            const answer = value.trim();
            inputMode = false;
            editor.setText('');
            if (answer && !currentQuestion()!.multiSelect) {
                saveSingle(answer, true);
                return;
            }
            if (answer) {
                if (!customAnswers[currentTab]!.includes(answer)) customAnswers[currentTab]!.push(answer);
                answers[currentTab] = undefined;
            }
            refresh();
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
            if (review && (matchesKey(data, Key.left) || matchesKey(data, Key.shift('tab')))) {
                switchTab(-1);
                return;
            }
            if (review && (matchesKey(data, Key.right) || matchesKey(data, Key.tab))) {
                switchTab(1);
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

        const addLine = (lines: string[], width: number, text = '') => lines.push(padToWidth(text, width));
        const render = (outerWidth: number): string[] => {
            if (cachedLines && cachedWidth === outerWidth) return cachedLines;
            if (outerWidth < 1) return [];
            const framed = outerWidth >= 3;
            const width = framed ? outerWidth - 2 : outerWidth;
            const lines: string[] = [];

            if (review) {
                // 当前项仅用强调色和粗体区分。
                const tabs = questions.map((_question, index) => {
                    const status = answers[index]?.completed ? '■' : '□';
                    const text = ` ${status} ${labelFor(index)} `;
                    return currentTab === index
                        ? theme.fg('accent', theme.bold(text))
                        : theme.fg(answers[index]?.completed ? 'success' : 'muted', text);
                });
                const submitText = ' ✓ 提交 ';
                tabs.push(isSubmitTab()
                    ? theme.fg('accent', theme.bold(submitText))
                    : theme.fg(allAnswered() ? 'success' : 'dim', submitText));
                addLine(lines, width, tabs.join(theme.fg('dim', '│')));
                addLine(lines, width);
            }

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
                addQuestionHeading(lines, width, theme.fg('accent', ` ${review ? labelFor(currentTab) : '？'}`) + theme.fg('text', ` ${question.question}`));
                if (question.multiSelect) addLine(lines, width, theme.fg('dim', ` 多选模式：已选 ${selectedValues(currentTab).length} 项`));
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
                    // Reserve a safe native width for wide graphemes, then clip tiny terminals.
                    // Only the outer panel supplies borders; the native content keeps its cursor.
                    for (const line of editor.render(Math.max(4, width - 1)).slice(1, -1)) {
                        let content = width > 1 ? ` ${line}` : line;
                        const marker = content.indexOf(CURSOR_MARKER);
                        const cursorColumn = marker < 0 ? -1 : visibleWidth(content.slice(0, marker));
                        if (cursorColumn >= width) content = sliceByColumn(content, cursorColumn - width + 1, width, true) + '\x1b[0m';
                        addLine(lines, width, content);
                    }
                }
                if (warning) {
                    if (!review) addLine(lines, width);
                    addLine(lines, width, theme.fg('warning', ` ${warning}`));
                }
            }
            addLine(lines, width);
            let help = ' Enter 提交 • Esc 返回选项';
            if (!inputMode) {
                if (isSubmitTab()) help = ' ←→ 返回问题 • Enter 提交 • Esc 取消';
                else {
                    const selection = currentQuestion()!.multiSelect ? ' • 空格勾选' : '';
                    const action = !review && currentQuestion()!.multiSelect ? '完成/自定义' : '确认';
                    help = `${review ? ' ←→ 切换 •' : ''} ↑↓ 选择${selection} • Enter ${action} • Esc 取消`;
                }
            }
            addLine(lines, width, theme.fg('dim', help));
            const border = (text: string, edge: Parameters<typeof frameBorder>[1]) => frameBorder(text, edge, 'input');
            cachedWidth = outerWidth;
            cachedLines = framed ? [
                border('╭', 'topLeft') + border('─'.repeat(width), 'top') + border('╮', 'topRight'),
                ...lines.map(line => border('│', 'left') + line + border('│', 'right')),
                border('╰', 'bottomLeft') + border('─'.repeat(width), 'bottom') + border('╯', 'bottomRight'),
            ] : lines;
            return cachedLines;
        };

        return {
            get focused() { return editor.focused; },
            set focused(value: boolean) { editor.focused = value; cachedLines = undefined; },
            render,
            invalidate: () => { cachedLines = undefined; cachedWidth = undefined; editor.invalidate(); },
            handleInput,
        };
    });


/** Native Pi rendering only; RPC clients render the structured tool details. */
export const nativeRenderers: ToolRenderers = {
        renderCall(input, theme) {
            const args = (input && typeof input === 'object' ? input : {}) as Partial<AskQuestionInput> & { questions?: AskQuestionInput[] };
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
                return new Text(theme.fg('warning', details.unavailable ? '尚未回答（未提供交互界面）' : '已取消'), 0, 0);
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
};
