import { CustomEditor, type KeybindingsManager } from '@earendil-works/pi-coding-agent';
import { type EditorTheme, type TUI, type TuiMouseEvent, type TuiMouseEventResult } from '@earendil-works/pi-tui';
import { frameBorder, padToWidth } from './utils.ts';
import { getWorkingMessageLine } from './working-message.ts';

export class TextAreaEditor extends CustomEditor {
    private frameInset = 0;
    private workingHeight = 0;
    private nativeWidth = 0;

    constructor(tui: TUI, theme: EditorTheme, keybindings: KeybindingsManager) {
        super(tui, theme, keybindings, { paddingX: 1 });
    }

    override render(width: number): string[] {
        this.workingHeight = this.nativeWidth = 0;
        if (width <= 0) return [];
        this.frameInset = width >= 8 ? 1 : 0;
        const innerWidth = width - this.frameInset * 2;
        // Native wrapping needs two content columns for wide graphemes.
        this.nativeWidth = Math.max(4, innerWidth);
        // Keep native border labels, then color each frame segment separately.
        const previousBorderColor = this.borderColor;
        this.borderColor = text => text;
        try {
            const lines = super.render(this.nativeWidth);
            const bottom = (this as unknown as { renderedVisibleLineCount: number }).renderedVisibleLineCount + 1;
            const left = frameBorder('│', 'left', 'input');
            const right = frameBorder('│', 'right', 'input');
            const rendered = lines.map((line, index) => {
                const body = padToWidth(line, innerWidth);
                if (index === 0) return this.frameInset
                    ? frameBorder('╭', 'topLeft', 'input') + frameBorder(body, 'top', 'input') + frameBorder('╮', 'topRight', 'input')
                    : frameBorder(body, 'top', 'input');
                if (index === bottom) return this.frameInset
                    ? frameBorder('╰', 'bottomLeft', 'input') + frameBorder(body, 'bottom', 'input') + frameBorder('╯', 'bottomRight', 'input')
                    : frameBorder(body, 'bottom', 'input');
                if (!this.frameInset) return body;
                if (index < bottom) return left + body + right;
                // Autocomplete remains below, outside the input frame.
                return ' ' + body + ' ';
            });
            const workingLine = getWorkingMessageLine();
            this.workingHeight = workingLine ? 1 : 0;
            return workingLine ? [padToWidth(` ${workingLine}`, width), ...rendered] : rendered;
        } finally {
            this.borderColor = previousBorderColor;
        }
    }

    override handleMouse(event: TuiMouseEvent): TuiMouseEventResult | undefined {
        const y = event.y - this.workingHeight;
        if (y < 0 || !this.nativeWidth) return;
        if (event.x < this.frameInset || event.x >= event.width - this.frameInset) {
            return event.type === 'click' && event.button === 'left' ? { handled: true, focus: true } : undefined;
        }
        return super.handleMouse({ ...event, x: event.x - this.frameInset, y,
            width: this.nativeWidth, height: event.height - this.workingHeight });
    }
}
