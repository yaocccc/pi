import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { CustomEditor, getAgentDir, type ExtensionAPI, type KeybindingsManager } from '@earendil-works/pi-coding-agent';
import { CombinedAutocompleteProvider, Key, matchesKey, truncateToWidth, visibleWidth, type EditorTheme, type TUI } from '@earendil-works/pi-tui';
import { Type } from 'typebox';

// ---- 类型 ----
interface PlanTodo {
    title: string;
    goal: string;
    steps: string[];
    risks?: string[];
    acceptance: string[];
}
interface CheckResult {
    status: 'pass' | 'fail';
    reason?: string;
    improvements?: string[];
}

type PlanDecision = 'execute' | 'supplement' | 'cancel';

type PlanUiTheme = {
    fg(color: string, text: string): string;
    bold(text: string): string;
};

interface PlanDecisionOption {
    value: PlanDecision;
    label: string;
}

// ---- 常量 ----
const TEXTAREA_RGB = '16;24;39';
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = '\x1b[49m';
const RESET_FG = '\x1b[39m';

const PLAN_DECISION_OPTIONS: PlanDecisionOption[] = [
    { value: 'execute', label: '确认执行' },
    { value: 'supplement', label: '补充' },
    { value: 'cancel', label: '取消' },
];

const PLAN_SET_TODOS_PARAMETERS = Type.Object({
    todos: Type.Array(Type.Object({
        title: Type.String({ description: '小项标题' }),
        goal: Type.String({ description: '小项目标' }),
        steps: Type.Array(Type.String({ description: '执行步骤列表' }), { minItems: 1 }),
        risks: Type.Optional(Type.Array(Type.String({ description: '风险列表' }))),
        acceptance: Type.Array(Type.String({ description: '验收标准列表' }), { minItems: 1 }),
    }), { minItems: 1 }),
});

const PLAN_CHECK_RESULT_PARAMETERS = Type.Object({
    status: Type.Union([Type.Literal('pass'), Type.Literal('fail')], { description: 'pass 或 fail' }),
    reason: Type.Optional(Type.String({ description: '失败原因（fail 时必填）' })),
    improvements: Type.Optional(Type.Array(Type.String({ description: '改进措施' }))),
});

// ---- Prompts ----
const planPrompt = (task: string) =>
    `进入 /plan 工作流：请评估并拆分任务。\n\n用户原始需求：\n${task}\n\n要求：\n` +
    `1. 先判断目标是否清晰，如有缺失请列出假设。\n` +
    `2. 将任务拆分为可执行的小项，每个小项包含：标题、目标、执行步骤、风险、验收标准。\n` +
    `3. 展示完整计划后，最后必须追加「## 简短 Todos」，格式为「1. 标题」「2. 标题」「3. 标题」。\n` +
    `4. 简短 Todos 的标题必须与 plan_set_todos 提交的小项标题一致。\n` +
    `5. 展示完计划后，立即调用 plan_set_todos 工具提交所有小项，todos 内容必须与展示一致。\n` +
    `6. 暂时不要执行任务，只输出计划和提交 todos。`;      

const supplementPrompt = (feedback: string) =>
    `用户检查计划后提出补充意见：\n\n${feedback}\n\n请基于原始需求、上一版计划和补充意见，重新输出完整计划。\n\n` +
    `要求：\n1. 只输出补充后的计划，不要执行任务。\n` +
    `2. 展示完整计划后，最后必须追加「## 简短 Todos」。\n` +
    `3. 简短 Todos 的标题必须与 plan_set_todos 提交的小项标题一致。\n` +
    `4. 展示完计划后立即调用 plan_set_todos 重新提交所有小项。`;

const executeTodoPrompt = (todo: PlanTodo, index: number, total: number) =>
    `进入 /plan 执行阶段：请执行第 ${index}/${total} 个 todo。\n\n` +
    `标题：${todo.title}\n目标：${todo.goal}\n步骤：\n${todo.steps.map((s, i) => `${i + 1}. ${s}`).join('\n')}\n` +
    `验收标准：\n${todo.acceptance.map((a, i) => `${i + 1}. ${a}`).join('\n')}\n` +
    `\n要求：\n1. 只执行第 ${index} 个 todo，不要执行后续 todo。\n` +
    `2. 完成后说明做了什么、改了哪些文件、是否满足验收标准。`;      

const reviewTodoPrompt = (todo: PlanTodo, index: number, total: number) =>
    `进入 /plan 检查阶段：请复查第 ${index}/${total} 个 todo 的执行结果。\n\n` +
    `原始 todo：\n标题：${todo.title}\n目标：${todo.goal}\n验收标准：\n${todo.acceptance.map((a, i) => `${i + 1}. ${a}`).join('\n')}\n\n` +
    `要求：\n1. 对照 todo 要求检查是否完成。\n2. 检查是否误做了其他 todo。\n` +
    `3. 如果通过，调用 plan_check_result 工具，status 设为 "pass"。\n` +
    `4. 如果未通过，调用 plan_check_result 工具，status 设为 "fail"，并填写 reason 和 improvements。\n` +
    `5. 必须调用 plan_check_result 工具，且只能调用一次。`;      

const failurePrompt = (todo: PlanTodo, index: number, total: number, check?: CheckResult) =>
    `/plan 在第 ${index}/${total} 个 todo 处停止——"${todo.title}"。\n\n` +
    `失败原因：${check?.reason ?? '检查未返回有效结果，可能是模型未调用 plan_check_result 工具。'}\n` +
    `改进建议：${check?.improvements?.join('；') ?? '请在下一次 /plan 中补充更具体的需求或验收标准。'}\n\n` +
    `要求：\n1. 总结本次 /plan 已完成和未完成的部分。\n2. 不要继续执行后续 todo。`;      

const finalReviewPrompt =
    '进入 /plan 最终复查阶段：所有 todo 已执行完毕。请做总复查。\n\n要求：\n' +
    '1. 对照原始需求和计划检查是否全部完成。\n2. 指出遗漏、风险、潜在问题。\n' +
    '3. 如有必要，给出后续建议；不要继续修改。\n4. 最后给出结论：通过 / 需补充 / 需返工。';      

// ---- 通用工具 ----
const padToWidth = (text: string, width: number): string => {
    const safeWidth = Math.max(0, width);
    const truncated = truncateToWidth(text, safeWidth, '');
    return truncated + ' '.repeat(Math.max(0, safeWidth - visibleWidth(truncated)));
};

const textAreaBg = (text: string): string => TEXTAREA_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${TEXTAREA_BG}`) + RESET_BG;

const halfBgLine = (char: '▀' | '▄', width: number): string => TEXTAREA_FG + char.repeat(Math.max(0, width)) + RESET_FG;

let cachedFdPath: string | null | undefined;

const getFdPath = (): string | null => {
    if (cachedFdPath !== undefined) return cachedFdPath;

    const localFd = join(getAgentDir(), 'bin', process.platform === 'win32' ? 'fd.exe' : 'fd');
    if (existsSync(localFd)) {
        cachedFdPath = localFd;
        return cachedFdPath;
    }

    try {
        const output = execFileSync('sh', ['-lc', 'command -v fd || command -v fdfind || true'], {
            encoding: 'utf8',
            stdio: ['ignore', 'pipe', 'ignore'],
        }).trim();
        cachedFdPath = output || null;
    } catch {
        cachedFdPath = null;
    }
    return cachedFdPath;
};

const editorTheme = (theme: PlanUiTheme): EditorTheme => ({
    borderColor: (text) => theme.fg('borderMuted', text),
    selectList: {
        selectedPrefix: (text) => theme.fg('accent', text),
        selectedText: (text) => theme.fg('accent', text),
        description: (text) => theme.fg('muted', text),
        scrollInfo: (text) => theme.fg('dim', text),
        noMatch: (text) => theme.fg('warning', text),
    },
});

// ---- Plan 输入 UI ----
class PlanTextAreaEditor extends CustomEditor {
    private readonly bg = textAreaBg;
    private readonly blankBorder = (text: string) => this.bg(' '.repeat(visibleWidth(text)));
    private closed = false;

    constructor(
        tui: TUI,
        theme: EditorTheme,
        keybindings: KeybindingsManager,
        private done: (value: string | undefined) => void,
        private hint: string,
    ) {
        super(tui, { ...theme, borderColor: (text) => this.blankBorder(text) }, keybindings, { paddingX: 1 });
        this.onSubmit = (text) => this.close(text.trim() ? text : undefined);
        this.onEscape = () => this.close(undefined);
    }

    override render(width: number): string[] {
        const previousBorderColor = this.borderColor;
        this.borderColor = this.blankBorder;
        try {
            const lines = super.render(width);
            const textarea = lines.map((line, index) => {
                if (index === 0) return halfBgLine('▄', width);
                if (index === lines.length - 1) return halfBgLine('▀', width);
                return this.bg(padToWidth(line, width));
            });
            return [padToWidth(this.hint, width), ...textarea];
        } finally {
            this.borderColor = previousBorderColor;
        }
    }

    private close(value: string | undefined) {
        if (this.closed) return;
        this.closed = true;
        this.done(value);
    }
}

const readPlanText = (ctx: any, hint: string): Promise<string | undefined> => ctx.ui.custom(
    (tui: TUI, theme: PlanUiTheme, keybindings: KeybindingsManager, done: (value: string | undefined) => void) => {
        const editor = new PlanTextAreaEditor(tui, editorTheme(theme), keybindings, done, hint);
        editor.setAutocompleteProvider(new CombinedAutocompleteProvider([], ctx.cwd ?? process.cwd(), getFdPath()));
        return editor;
    },
);

const readPlanTask = (ctx: any): Promise<string | undefined> => readPlanText(ctx, 'Plan 模式：输入任务，Enter 提交，Esc 取消');
const readPlanSupplement = (ctx: any): Promise<string | undefined> => readPlanText(ctx, '补充');

// ---- 检查计划 UI ----
const readPlanDecision = (ctx: any): Promise<PlanDecision | undefined> => ctx.ui.custom(
    (tui: TUI, theme: PlanUiTheme, _keybindings: KeybindingsManager, done: (value: PlanDecision | undefined) => void) => {
        let selectedIndex = 0;
        let cachedLines: string[] | undefined;

        const refresh = () => {
            cachedLines = undefined;
            tui.requestRender();
        };

        const handleInput = (data: string) => {
            if (matchesKey(data, Key.up)) {
                selectedIndex = Math.max(0, selectedIndex - 1);
                refresh();
                return;
            }
            if (matchesKey(data, Key.down)) {
                selectedIndex = Math.min(PLAN_DECISION_OPTIONS.length - 1, selectedIndex + 1);
                refresh();
                return;
            }
            if (matchesKey(data, Key.enter) || matchesKey(data, Key.space)) {
                done(PLAN_DECISION_OPTIONS[selectedIndex]!.value);
                return;
            }
            if (matchesKey(data, Key.escape)) {
                done('cancel');
            }
        };

        const addLine = (lines: string[], width: number, text = '') => lines.push(textAreaBg(padToWidth(text, width)));

        const render = (width: number): string[] => {
            if (cachedLines) return cachedLines;

            const lines: string[] = [];
            lines.push(halfBgLine('▄', width));
            addLine(lines, width, theme.fg('accent', theme.bold(' 检查计划')));
            addLine(lines, width);

            for (let i = 0; i < PLAN_DECISION_OPTIONS.length; i++) {
                const option = PLAN_DECISION_OPTIONS[i]!;
                const selected = i === selectedIndex;
                const prefix = selected ? theme.fg('accent', '> ') : '  ';
                const color = selected ? 'accent' : option.value === 'cancel' ? 'muted' : 'text';
                addLine(lines, width, prefix + theme.fg(color, option.label));
            }

            addLine(lines, width);
            addLine(lines, width, theme.fg('dim', ' ↑↓ 选择 • Enter 确认 • Esc 取消'));
            lines.push(halfBgLine('▀', width));

            cachedLines = lines;
            return lines;
        };

        return {
            render,
            invalidate: () => {
                cachedLines = undefined;
            },
            handleInput,
        };
    },
);

// ---- Agent 交互 ----
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

const sendAndWait = async (
    pi: ExtensionAPI,
    ctx: { isIdle(): boolean; waitForIdle(): Promise<void> },
    content: string,
) => {
    pi.sendUserMessage(content, ctx.isIdle() ? undefined : { deliverAs: 'followUp' });
    for (let i = 0; i < 100 && ctx.isIdle(); i++) await sleep(20);
    await ctx.waitForIdle();
};

// ---- Extension：工具注册与工作流执行 ----
export default (pi: ExtensionAPI) => {
    let todos: PlanTodo[] = [];
    const state = { lastCheck: undefined as CheckResult | undefined };

    // ---- 工具：提交 todos ----
    pi.registerTool({
        name: 'plan_set_todos',
        label: '提交 Todos',
        description: '仅 /plan 模式使用：提交计划中的所有小项，供 plan extension 逐项执行。非 /plan 日常对话不要调用。',
        promptSnippet: '仅 /plan 模式使用：提交结构化 todos；非 /plan 日常对话不要调用',
        parameters: PLAN_SET_TODOS_PARAMETERS,
        async execute(_id, params) {
            todos = params.todos as PlanTodo[];
            return {
                content: [{ type: 'text', text: `已记录 ${todos.length} 个 todos。` }],
                details: { count: todos.length },
            };
        },
    });

    // ---- 工具：提交检查结果 ----
    pi.registerTool({
        name: 'plan_check_result',
        label: '提交检查结果',
        description: '仅 /plan 模式使用：提交当前 todo 的检查结论。status 为 "pass" 表示通过，"fail" 表示未通过。非 /plan 日常对话不要调用。',
        promptSnippet: '仅 /plan 模式使用：提交 todo 检查结果；非 /plan 日常对话不要调用',
        parameters: PLAN_CHECK_RESULT_PARAMETERS,
        async execute(_id, params) {
            state.lastCheck = params as CheckResult;
            const label = state.lastCheck!.status === 'pass' ? '通过' : '未通过';
            return {
                content: [{ type: 'text', text: `检查结论：${label}。` }],
                details: state.lastCheck,
            };
        },
    });

    // ---- 命令 ----
    pi.registerCommand('plan', {
        description: 'Plan 模式：使用当前配置模型规划、逐项执行、检查并最终复查',
        handler: async (args, ctx) => {
            // 1. 获取任务
            const task = (args as string).trim() || await readPlanTask(ctx);
            if (!task?.trim()) return ctx.ui.notify('已取消：无输入。', 'warning');

            // 2. 使用当前配置模型生成计划
            todos = [];
            state.lastCheck = undefined;
            ctx.ui.notify('正在规划……', 'info');
            await sendAndWait(pi, ctx, planPrompt(task));

            // 3. 检查计划
            while (true) {
                const choice = await readPlanDecision(ctx);
                if (!choice || choice === 'cancel') return ctx.ui.notify('已停止 /plan。', 'warning');
                if (choice === 'execute') break;

                const feedback = await readPlanSupplement(ctx);
                if (!feedback?.trim()) {
                    ctx.ui.notify('未输入内容。', 'warning');
                    continue;
                }

                todos = [];
                ctx.ui.notify('正在补充计划……', 'info');
                await sendAndWait(pi, ctx, supplementPrompt(feedback));
            }

            if (todos.length === 0) {
                ctx.ui.notify('模型未提交 todos。', 'error');
                return;
            }

            // 4. 使用同一配置模型逐项执行并检查
            for (const [i, todo] of todos.entries()) {
                const idx = i + 1;
                const total = todos.length;

                ctx.ui.notify(`正在执行 todo ${idx}/${total}：${todo.title}`, 'info');
                await sendAndWait(pi, ctx, executeTodoPrompt(todo, idx, total));

                state.lastCheck = undefined;
                ctx.ui.notify(`正在检查 todo ${idx}/${total}：${todo.title}`, 'info');
                await sendAndWait(pi, ctx, reviewTodoPrompt(todo, idx, total));

                // as 断言：TypeScript 无法追踪闭包内对象属性的 mutation
                if ((state.lastCheck as CheckResult | undefined)?.status !== 'pass') {
                    ctx.ui.notify(`❌ todo ${idx}/${total} 未通过，正在总结……`, 'warning');
                    await sendAndWait(pi, ctx, failurePrompt(todo, idx, total, state.lastCheck as CheckResult | undefined));
                    return;
                }

                ctx.ui.notify(`✅ todo ${idx}/${total} 通过。`, 'info');
            }

            // 5. 最终复查
            ctx.ui.notify('正在做最终总复查……', 'info');
            await sendAndWait(pi, ctx, finalReviewPrompt);
        },
    });
};
