import { execFileSync } from 'node:child_process';
import { existsSync } from 'node:fs';
import { join } from 'node:path';
import { getAgentDir } from '@earendil-works/pi-coding-agent';
import { truncateToWidth, visibleWidth, type EditorTheme } from '@earendil-works/pi-tui';

export type PlanUiTheme = {
    fg(color: string, text: string): string;
    bold(text: string): string;
};

const TEXTAREA_RGB = '16;24;39';
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = '\x1b[49m';
const RESET_FG = '\x1b[39m';

let cachedFdPath: string | null | undefined;

export const padToWidth = (text: string, width: number): string => {
    const safeWidth = Math.max(0, width);
    const truncated = truncateToWidth(text, safeWidth, '');
    return truncated + ' '.repeat(Math.max(0, safeWidth - visibleWidth(truncated)));
};

export const textAreaBg = (text: string): string => TEXTAREA_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${TEXTAREA_BG}`) + RESET_BG;

export const halfBgLine = (char: '▀' | '▄', width: number): string => TEXTAREA_FG + char.repeat(Math.max(0, width)) + RESET_FG;

export const getFdPath = (): string | null => {
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

export const editorTheme = (theme: PlanUiTheme): EditorTheme => ({
    borderColor: (text) => theme.fg('borderMuted', text),
    selectList: {
        selectedPrefix: (text) => theme.fg('accent', text),
        selectedText: (text) => theme.fg('accent', text),
        description: (text) => theme.fg('muted', text),
        scrollInfo: (text) => theme.fg('dim', text),
        noMatch: (text) => theme.fg('warning', text),
    },
});
