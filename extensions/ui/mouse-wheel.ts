import type { TUI } from '@earendil-works/pi-tui';

export type MouseWheelDirection = -1 | 1;

type InputListenerResult = { consume?: boolean; data?: string } | undefined;
type WheelAwareTui = TUI & {
    handleViewportInput?: (data: string) => InputListenerResult;
};

const ENABLE_MOUSE_WHEEL = '\x1b[?1000h\x1b[?1006h';
const DISABLE_MOUSE_WHEEL = '\x1b[?1006l\x1b[?1000l';

export const getMouseWheelDirection = (data: string): MouseWheelDirection | undefined => {
    const sgr = /^\x1b\[<(\d+);\d+;\d+[Mm]$/.exec(data);
    if (sgr) {
        const button = Number.parseInt(sgr[1]!, 10);
        if ((button & 64) === 0) return undefined;
        const direction = button & 3;
        return direction === 0 ? -1 : direction === 1 ? 1 : undefined;
    }

    if (data.length === 6 && data.startsWith('\x1b[M')) {
        const button = data.charCodeAt(3) - 32;
        if ((button & 64) === 0) return undefined;
        const direction = button & 3;
        return direction === 0 ? -1 : direction === 1 ? 1 : undefined;
    }

    return undefined;
};

/**
 * Enable wheel input for a focused popup. Fullscreen pi consumes wheel input
 * in its viewport listener before an overlay receives handleInput(), so this
 * temporarily intercepts that private runtime hook. Regular mode does not
 * enable mouse reporting itself, so reporting is enabled only while open.
 */
export const enablePopupMouseWheel = (
    tui: TUI,
    onWheel: (direction: MouseWheelDirection) => void,
): (() => void) => {
    const wheelTui = tui as WheelAwareTui;
    const original = wheelTui.handleViewportInput;
    if (typeof original === 'function') {
        const intercept = (data: string): InputListenerResult => {
            const direction = getMouseWheelDirection(data);
            if (direction !== undefined) {
                onWheel(direction);
                return { consume: true };
            }
            return original.call(wheelTui, data);
        };

        wheelTui.handleViewportInput = intercept;
        return () => {
            if (wheelTui.handleViewportInput === intercept) {
                wheelTui.handleViewportInput = original;
            }
        };
    }

    tui.terminal.write(ENABLE_MOUSE_WHEEL);
    let restored = false;
    return () => {
        if (restored) return;
        restored = true;
        tui.terminal.write(DISABLE_MOUSE_WHEEL);
    };
};
