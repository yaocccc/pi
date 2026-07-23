import { CustomEditor, UserMessageComponent, type ExtensionAPI, type ExtensionContext, type KeybindingsManager } from '@earendil-works/pi-coding-agent';
import { Box, Text, truncateToWidth, visibleWidth, type Component, type TUI, type EditorTheme } from '@earendil-works/pi-tui';

const TEXTAREA_RGB = '18;18;18';
const TEXTAREA_BG = `\x1b[48;2;${TEXTAREA_RGB}m`;
const TEXTAREA_FG = `\x1b[38;2;${TEXTAREA_RGB}m`;
const RESET_BG = '\x1b[49m';
const RESET_FG = '\x1b[39m';

type TokenUsage = {
    input?: number;
    output?: number;
};

const formatTokenCount = (value: number | undefined): string => !Number.isFinite(value) ? '0' : value! >= 1_000_000 ? `${(value! / 1_000_000).toFixed(1)}M` : value! >= 1_000 ? `${Math.round(value! / 1_000)}k` : String(value);

const formatElapsed = (ms: number): string => {
    const totalSeconds = Math.max(0, Math.floor(ms / 1000));
    const minutes = Math.floor(totalSeconds / 60);
    const seconds = totalSeconds % 60;
    return minutes > 0 ? `${minutes}m${seconds}s` : `${seconds}s`;
};

const workingMessage = (startedAt: number | undefined, usage: TokenUsage = {}): string => {
    const input = usage.input ?? 0;
    const output = usage.output ?? 0;
    const parts = [
        ...(input > 0 ? [`↑${formatTokenCount(input)}`] : []),
        ...(output > 0 ? [`↓${formatTokenCount(output)}`] : []),
        formatElapsed(startedAt ? Date.now() - startedAt : 0),
    ];
    return `working...(${parts.join(' ')})`;
};

const applyWorkingMessage = (ctx: ExtensionContext, startedAt?: number, usage?: TokenUsage): void => ctx.ui.setWorkingMessage(workingMessage(startedAt, usage));

const padToWidth = (text: string, width: number): string => {
    const truncated = truncateToWidth(text, width, '');
    return truncated + ' '.repeat(Math.max(0, width - visibleWidth(truncated)));
};

// Editor 光标内部会用 \x1b[0m 重置样式；重置后重新补上背景色，保证整行都有 #121212 背景。
const textAreaBg = (text: string): string => TEXTAREA_BG + text.replace(/\x1b\[0m/g, `\x1b[0m${TEXTAREA_BG}`) + RESET_BG;

// 顶部用下半块，底部用上半块，避免整行全高背景色块。
const halfBlockLine = (width: number, position: 'top' | 'bottom', fg = TEXTAREA_FG): string => fg + (position === 'top' ? '▄' : '▀').repeat(Math.max(0, width)) + RESET_FG;

const foregroundFromBackground = (line: string): string | undefined => {
    const matches = [...line.matchAll(/\x1b\[48;(2;\d+;\d+;\d+|5;\d+)m/g)];
    const last = matches.at(-1);
    return last ? `\x1b[38;${last[1]}m` : undefined;
};

// 优先从内容行取色；有些主题/组件可能会让 padding 行和内容行的 ANSI 顺序不同。
const foregroundFromRenderedBox = (lines: string[]): string | undefined => {
    const contentLine = lines.slice(1, -1).find((line) => foregroundFromBackground(line));
    return foregroundFromBackground(contentLine ?? lines.find((line) => foregroundFromBackground(line)) ?? '');
};

const replaceVisibleContent = (line: string, content: string): string => (line.match(/^(?:\x1b\]133;[ABC]\x07)*/)?.[0] ?? '') + content;

let paddedBackgroundHalfBlockPatched = false;
let userMessageHalfBlockPatched = false;

type RenderablePrototype = {
    render(width: number): string[];
};

const patchPaddedBackgroundComponent = (prototype: RenderablePrototype): void => {
    const originalRender = prototype.render;

    prototype.render = function patchedPaddedBackgroundRender(this: { paddingY?: number; bgFn?: unknown; customBgFn?: unknown }, width: number): string[] {
        const lines = originalRender.call(this, width);
        const hasComponentBg = typeof this.bgFn === 'function' || typeof this.customBgFn === 'function';
        if (!hasComponentBg || lines.length < 2 || (typeof this.paddingY === 'number' && this.paddingY <= 0)) return lines;

        const fg = foregroundFromRenderedBox(lines);
        if (!fg) return lines;

        const patched = [...lines];
        patched[0] = replaceVisibleContent(patched[0]!, halfBlockLine(width, 'top', fg));
        patched[patched.length - 1] = replaceVisibleContent(patched[patched.length - 1]!, halfBlockLine(width, 'bottom', fg));
        return patched;
    };
};

const patchPaddedBackgroundHalfBlocks = (): void => {
    if (paddedBackgroundHalfBlockPatched) return;
    paddedBackgroundHalfBlockPatched = true;

    // Box 覆盖工具调用、custom message、compact/branch/skill 等带背景色块的组件。
    patchPaddedBackgroundComponent(Box.prototype as unknown as RenderablePrototype);
    // Text 覆盖少量直接用 Text + customBgFn 渲染背景的组件兜底。
    patchPaddedBackgroundComponent(Text.prototype as unknown as RenderablePrototype);
};

const patchUserMessageHalfBlocks = (): void => {
    if (userMessageHalfBlockPatched) return;
    userMessageHalfBlockPatched = true;

    const prototype = UserMessageComponent.prototype as UserMessageComponent & { render(width: number): string[] };
    const originalRender = prototype.render;

    prototype.render = function patchedUserMessageRender(this: UserMessageComponent, width: number): string[] {
        const lines = originalRender.call(this, width);
        if (lines.length === 0) return lines;

        const fg = foregroundFromRenderedBox(lines) ?? TEXTAREA_FG;
        const patched = [...lines];

        patched[0] = replaceVisibleContent(patched[0]!, halfBlockLine(width, 'top', fg));
        patched[patched.length - 1] = replaceVisibleContent(patched[patched.length - 1]!, halfBlockLine(width, 'bottom', fg));
        return patched;
    };
};

class StartupHeader implements Component {
    constructor(private theme: any) {}

    render(width: number): string[] {
        const logo = [
            '██████╗ ██╗',
            '██╔══██╗██║',
            '██████╔╝██║',
            '██╔═══╝ ██║',
            '██║     ██║',
            '╚═╝     ╚═╝',
        ];
        return [
            '',
            '',
            ...logo.map((text) => this.theme.fg('accent', truncateToWidth(`    ${text}`, width, ''))),
            this.theme.fg('dim', truncateToWidth('    coding agent', width, '')),
        ];
    }

    invalidate() {}
}

class NoCostFooter implements Component {
    constructor(
        private ctx: ExtensionContext,
        private theme: any,
        private footerData: { getGitBranch(): string | null; getExtensionStatuses(): ReadonlyMap<string, string>; getAvailableProviderCount(): number },
    ) {}

    render(width: number): string[] {
        const lines: string[] = [];
        const home = process.env.HOME || process.env.USERPROFILE;
        const branch = this.footerData.getGitBranch();
        let pwd = this.ctx.cwd;
        if (home && pwd.startsWith(home)) pwd = `~${pwd.slice(home.length)}`;
        if (branch) pwd += ` (${branch})`;
        lines.push(truncateToWidth(this.theme.fg('dim', pwd), width, this.theme.fg('dim', '...')));

        const entries = this.ctx.sessionManager.getEntries() as any[];
        let input = 0, output = 0, cacheRead = 0, cacheWrite = 0;
        for (const entry of entries) {
            const msg = entry.type === 'message' ? entry.message : undefined;
            if (msg?.role !== 'assistant' || !msg.usage) continue;
            input += msg.usage.input ?? 0;
            output += msg.usage.output ?? 0;
            cacheRead += msg.usage.cacheRead ?? 0;
            cacheWrite += msg.usage.cacheWrite ?? 0;
        }

        const parts = [];
        if (input) parts.push(`↑${formatTokenCount(input)}`);
        if (output) parts.push(`↓${formatTokenCount(output)}`);
        if (cacheRead) parts.push(`R${formatTokenCount(cacheRead)}`);
        if (cacheWrite) parts.push(`W${formatTokenCount(cacheWrite)}`);

        const usage = this.ctx.getContextUsage?.();
        if (usage) parts.push(`${usage.percent?.toFixed?.(1) ?? '?'}%/${formatTokenCount(usage.contextWindow)}`);

        const model = this.ctx.model as any;
        const modelText = model
            ? `${this.footerData.getAvailableProviderCount() > 1 ? `(${model.provider}) ` : ''}${model.id}`
            : 'no-model';
        const leftText = parts.join(' ');
        const left = this.theme.fg('dim', leftText);
        const right = this.theme.fg('dim', modelText);
        const padding = Math.max(2, width - visibleWidth(leftText) - visibleWidth(modelText));
        lines.push(truncateToWidth(left + ' '.repeat(padding) + right, width));

        const statuses = Array.from(this.footerData.getExtensionStatuses().values()).map((s) => s.replace(/[\r\n\t]/g, ' ').trim()).filter(Boolean);
        if (statuses.length) lines.push(truncateToWidth(this.theme.fg('dim', statuses.join(' ')), width, this.theme.fg('dim', '...')));
        return lines;
    }

    invalidate() {}
}

class TextAreaEditor extends CustomEditor {
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

export default (pi: ExtensionAPI) => {
    patchPaddedBackgroundHalfBlocks();
    patchUserMessageHalfBlocks();

    let startedAt: number | undefined;
    let timer: NodeJS.Timeout | undefined;
    let latestUsage: TokenUsage = {};

    const refreshWorkingMessage = (ctx: ExtensionContext) => applyWorkingMessage(ctx, startedAt, latestUsage);

    pi.on('session_start', (_event, ctx) => {
        refreshWorkingMessage(ctx);
        ctx.ui.setHeader((_tui, theme) => new StartupHeader(theme));
        ctx.ui.setEditorComponent((tui, theme, keybindings) => new TextAreaEditor(tui, theme, keybindings));
        ctx.ui.setFooter((_tui, theme, footerData) => new NoCostFooter(ctx, theme, footerData));
    });

    pi.on('agent_start', (_event, ctx) => {
        if (timer) clearInterval(timer);
        startedAt = Date.now();
        latestUsage = {};
        refreshWorkingMessage(ctx);
        timer = setInterval(() => refreshWorkingMessage(ctx), 1000);
    });

    pi.on('message_end', (event, ctx) => {
        if (event.message.role !== 'assistant') return;
        latestUsage = {
            input: event.message.usage?.input,
            output: event.message.usage?.output,
        };
        refreshWorkingMessage(ctx);
    });

    pi.on('agent_end', (_event, ctx) => {
        if (timer) clearInterval(timer);
        timer = undefined;
        refreshWorkingMessage(ctx);
    });

    pi.on('session_shutdown', () => {
        if (timer) clearInterval(timer);
        timer = undefined;
    });
};
