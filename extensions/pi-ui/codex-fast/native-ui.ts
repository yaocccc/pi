import type { ExtensionContext } from '@earendil-works/pi-coding-agent';

/** Native Pi and generic SDK RPC fallback; Go owns its own menu and labels. */
export async function selectNativeLevel(ctx: ExtensionContext, current: string, levels: readonly string[]): Promise<string | undefined> {
    const items = levels.map((level) => ({
        level,
        label: `${level}${level === current ? ' · 当前' : ''}`,
    }));
    const selected = await ctx.ui.select(
        `Codex 档位 · 当前 ${current} · 回车选择并保存 · Esc 退出`,
        items.map((item) => item.label),
    );
    return items.find((item) => item.label === selected)?.level;
}

export function notifyNativeUnavailable(ctx: ExtensionContext): void {
    ctx.ui.notify('Codex Fast 设置需要交互式 UI', 'warning');
}

export function notifyNativeSaved(ctx: ExtensionContext, level: string): void {
    ctx.ui.notify(`Codex 档位已设为 ${level}`, 'info');
}

export function notifyNativeError(ctx: ExtensionContext, error: unknown): void {
    ctx.ui.notify(`保存 Fast 配置失败: ${error}`, 'error');
}
