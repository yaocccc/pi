import type { KeybindingsManager } from '@earendil-works/pi-coding-agent';
import { Key, matchesKey, type TUI } from '@earendil-works/pi-tui';
import type { PlanDecision } from '../types/index.ts';
import { halfBgLine, padToWidth, textAreaBg, type PlanUiTheme } from './styles.ts';

interface PlanDecisionOption {
    value: PlanDecision;
    label: string;
}

const PLAN_DECISION_OPTIONS: PlanDecisionOption[] = [
    { value: 'execute', label: '确认执行' },
    { value: 'supplement', label: '补充' },
    { value: 'cancel', label: '取消' },
];

export const readPlanDecision = (ctx: any): Promise<PlanDecision | undefined> => ctx.ui.custom(
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
