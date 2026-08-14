import type { TUI } from '@earendil-works/pi-tui';

export type MouseWheelDirection = -1 | 1;

type InputListenerResult = { consume?: boolean; data?: string } | undefined;
type WheelAwareTui = TUI & {
    mode?: 'regular' | 'fullscreen';
    shouldDeferViewportInputToOverlay?: () => boolean;
    handleViewportInput?: (data: string) => InputListenerResult;
};

type LegacyFullscreenHandler = {
    token: symbol;
    onWheel: (direction: MouseWheelDirection) => void;
};

type LegacyFullscreenState = {
    original: (data: string) => InputListenerResult;
    intercept: (data: string) => InputListenerResult;
    handlers: LegacyFullscreenHandler[];
};

type RegularMouseState = { users: number };

const ENABLE_MOUSE_WHEEL = '\x1b[?1000h\x1b[?1006h';
const DISABLE_MOUSE_WHEEL = '\x1b[?1006l\x1b[?1000l';
const legacyFullscreenStates = new WeakMap<TUI, LegacyFullscreenState>();
const regularMouseStates = new WeakMap<TUI, RegularMouseState>();

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
 * Enable wheel input for a focused popup.
 *
 * Current fullscreen pi versions natively defer wheel events to focused
 * overlays. Older versions consumed them in the viewport first, so the legacy
 * fallback installs one shared interceptor whose handler stack is safe even
 * when popups close out of order. Regular mode enables terminal mouse reporting
 * with reference-counted cleanup.
 */
export const enablePopupMouseWheel = (
    tui: TUI,
    onWheel: (direction: MouseWheelDirection) => void,
): (() => void) => {
    const wheelTui = tui as WheelAwareTui;
    const nativeOverlayWheel = wheelTui.mode === 'fullscreen'
        && typeof wheelTui.shouldDeferViewportInputToOverlay === 'function';

    if (nativeOverlayWheel) {
        // Repair a stale interceptor left by an older extension version, then
        // ensure wheel reporting is enabled without disabling it on popup close.
        const prototypeHandler = (Object.getPrototypeOf(wheelTui) as WheelAwareTui | null)?.handleViewportInput;
        if (typeof prototypeHandler === 'function' && wheelTui.handleViewportInput !== prototypeHandler) {
            wheelTui.handleViewportInput = prototypeHandler;
        }
        tui.terminal.write(ENABLE_MOUSE_WHEEL);
        return () => undefined;
    }

    const viewportHandler = wheelTui.handleViewportInput;
    if (wheelTui.mode === 'fullscreen' && typeof viewportHandler === 'function') {
        let state = legacyFullscreenStates.get(tui);
        if (!state) {
            const handlers: LegacyFullscreenHandler[] = [];
            const original = viewportHandler;
            const intercept = (data: string): InputListenerResult => {
                const direction = getMouseWheelDirection(data);
                const active = handlers.at(-1);
                if (direction !== undefined && active) {
                    active.onWheel(direction);
                    return { consume: true };
                }
                return original.call(wheelTui, data);
            };
            state = { original, intercept, handlers };
            legacyFullscreenStates.set(tui, state);
            wheelTui.handleViewportInput = intercept;
        }

        const token = Symbol('popup-mouse-wheel');
        state.handlers.push({ token, onWheel });
        let restored = false;
        return () => {
            if (restored) return;
            restored = true;
            const current = legacyFullscreenStates.get(tui);
            if (!current) return;
            const index = current.handlers.findIndex((handler) => handler.token === token);
            if (index >= 0) current.handlers.splice(index, 1);
            if (current.handlers.length > 0) return;
            if (wheelTui.handleViewportInput === current.intercept) {
                wheelTui.handleViewportInput = current.original;
            }
            legacyFullscreenStates.delete(tui);
        };
    }

    if (wheelTui.mode === 'fullscreen') {
        // Never disable reporting for fullscreen: the conversation viewport owns it.
        tui.terminal.write(ENABLE_MOUSE_WHEEL);
        return () => undefined;
    }

    let regularState = regularMouseStates.get(tui);
    if (!regularState) {
        regularState = { users: 0 };
        regularMouseStates.set(tui, regularState);
        tui.terminal.write(ENABLE_MOUSE_WHEEL);
    }
    regularState.users += 1;

    let restored = false;
    return () => {
        if (restored) return;
        restored = true;
        const current = regularMouseStates.get(tui);
        if (!current) return;
        current.users -= 1;
        if (current.users > 0) return;
        regularMouseStates.delete(tui);
        tui.terminal.write(DISABLE_MOUSE_WHEEL);
    };
};
