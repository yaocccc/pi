import { truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';

const TEXTAREA_RGB = '18;18;18';
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = '\x1b[49m';
const RESET_FG = '\x1b[39m';

export const formatTokenCount = (value: number | undefined): string => !Number.isFinite(value) ? '0' : value! >= 1_000_000 ? `${(value! / 1_000_000).toFixed(1)}M` : value! >= 1_000 ? `${Math.round(value! / 1_000)}k` : String(value);

export const formatElapsed = (ms: number): string => {
    const totalSeconds = Math.max(0, Math.floor(ms / 1000));
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return minutes > 0 ? `${minutes}m${seconds}s` : `${seconds}s`;
};

export const padToWidth = (text: string, width: number): string => {
    const truncated = truncateToWidth(text, width, '');
    return truncated + ' '.repeat(Math.max(0, width - visibleWidth(truncated)));
};

// Editor 光标内部会用 \x1b[0m 重置样式；重置后重新补上背景色，保证整行都有 #121212 背景。
export const textAreaBg = (text: string): string => TEXTAREA_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${TEXTAREA_BG}`) + RESET_BG;

// 顶部用下半块，底部用上半块，避免整行全高背景色块。
export const halfBlockLine = (width: number, position: 'top' | 'bottom', fg = TEXTAREA_FG): string => fg + (position === 'top' ? '▄' : '▀').repeat(Math.max(0, width)) + RESET_FG;

const foregroundFromBackground = (line: string): string | undefined => {
    const matches = [...line.matchAll(/\x1b\[48;(2;\d+;\d+;\d+|5;\d+)m/g)];
    const last = matches.at(-1);
    return last ? `\x1b[38;${last[1]}m` : undefined;
};

// 优先从内容行取色；有些主题/组件可能会让 padding 行和内容行的 ANSI 顺序不同。
export const foregroundFromRenderedBox = (lines: string[]): string | undefined => {
    const contentLine = lines.slice(1, -1).find((line) => foregroundFromBackground(line));
    return foregroundFromBackground(contentLine ?? lines.find((line) => foregroundFromBackground(line)) ?? '');
};

export const replaceVisibleContent = (line: string, content: string): string => (line.match(/^(?:\x1b\]133;[ABC]\x07)*/)?.[0] ?? '') + content;
