import { CustomEditor, type KeybindingsManager } from '@earendil-works/pi-coding-agent';
import { visibleWidth, type EditorTheme, type TUI } from '@earendil-works/pi-tui';
import { halfBlockLine, padToWidth, textAreaBg } from './utils.ts';

export class TextAreaEditor extends CustomEditor {
    private readonly bg: (text: string) => string;
    private readonly blankBorder: (text: string) => string;

    constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager) {
        const bg = textAreaBg;
        const blankBorder = (text: string) => bg(' '.repeat(visibleWidth(text)));

        super(tui, { ...theme, borderColor: blankBorder }, keybindings, { paddingX: 1 });

        this.bg = bg;
        this.blankBorder = blankBorder;
    }

    override render(width: number): string[] {
        // pi 在接入自定义 editor 后会复制默认 borderColor；这里渲染时再临时改回“无边框”。
        const previousBorderColor = this.borderColor;
        this.borderColor = this.blankBorder;
        try {
            const lines = super.render(width);
            return lines.map((line, index) => {
                if (index === 0) return halfBlockLine(width, 'top');
                if (index === lines.length - 1) return halfBlockLine(width, 'bottom');
                return this.bg(padToWidth(line, width));
            });
        } finally {
            this.borderColor = previousBorderColor;
        }
    }
}
