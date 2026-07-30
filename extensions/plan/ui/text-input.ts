import { CustomEditor, type KeybindingsManager } from '@earendil-works/pi-coding-agent';
import { CombinedAutocompleteProvider, visibleWidth, type EditorTheme, type TUI } from '@earendil-works/pi-tui';
import { editorTheme, getFdPath, halfBgLine, padToWidth, textAreaBg, type PlanUiTheme } from './styles.ts';

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
            const showingAutocomplete = this.isShowingAutocomplete();
            const textarea = lines.map((line, index) => {
                if (index === 0) return halfBgLine('▄', width);
                if (!showingAutocomplete && index === lines.length - 1) return halfBgLine('▀', width);
                return this.bg(padToWidth(line, width));
            });
            if (showingAutocomplete) textarea.push(halfBgLine('▀', width));
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

export const readPlanTask = (ctx: any): Promise<string | undefined> => readPlanText(ctx, 'Plan 模式：输入任务，Enter 提交，Esc 取消');
export const readPlanSupplement = (ctx: any): Promise<string | undefined> => readPlanText(ctx, '补充');
