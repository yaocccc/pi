import type { ExtensionContext } from '@earendil-works/pi-coding-agent';

const PROVIDER = 'openai-codex';
export const CODEX_USAGE_URL = 'https://chatgpt.com/backend-api/wham/usage';
export const USAGE_TIMEOUT_MS = 15_000;
export const USAGE_REFRESH_MS = 10 * 60_000;
export const USAGE_TICK_MS = 30_000;

export type CodexUsage = { usedPercent: number; resetAt: number };

const object = (value: unknown): Record<string, unknown> | undefined =>
    value !== null && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
const number = (value: unknown): number | undefined => {
    if (typeof value !== 'number' && !(typeof value === 'string' && value.trim())) return undefined;
    const result = Number(value);
    return Number.isFinite(result) ? result : undefined;
};
export const officialCodexOrigin = (baseUrl: string | undefined): boolean => {
    try { return new URL(baseUrl ?? '').origin === 'https://chatgpt.com'; } catch { return false; }
};

export function parseCodexUsage(payload: unknown, now = Date.now()): CodexUsage {
    const rateLimit = object(object(payload)?.rate_limit);
    const windows = ['secondary_window', 'primary_window'].flatMap((key) => {
        const window = object(rateLimit?.[key]);
        const used = number(window?.used_percent);
        const resetAfter = number(window?.reset_after_seconds);
        const resetAt = number(window?.reset_at) ?? (resetAfter !== undefined && resetAfter >= 0 ? now / 1000 + resetAfter : undefined);
        if (used === undefined || resetAt === undefined || resetAt <= 0 || !Number.isFinite(resetAt * 1000)) return [];
        return [{ usedPercent: Math.min(100, Math.max(0, used)), resetAt: resetAt * 1000,
            weekly: (number(window?.limit_window_seconds) ?? 0) >= 604_800 }];
    });
    const selected = windows.find((window) => window.weekly) ?? windows[0];
    if (!selected) throw new Error('Invalid Codex usage');
    return { usedPercent: selected.usedPercent, resetAt: selected.resetAt };
}

export function formatCountdown(resetAt: number, now = Date.now()): string {
    const minutes = Math.max(0, Math.ceil((resetAt - now) / 60_000));
    const days = Math.floor(minutes / 1440);
    const hours = Math.floor((minutes % 1440) / 60);
    if (days) return `${days}d${hours}h`;
    if (hours) return `${hours}h${minutes % 60}m`;
    return `${minutes}m`;
}

const authorizationFrom = (auth: { apiKey?: string; headers?: Record<string, string | null | undefined> }): string | undefined => {
    const header = Object.entries(auth.headers ?? {}).find(([name]) => name.toLowerCase() === 'authorization')?.[1];
    if (header && /^Bearer\s+\S+$/iu.test(header)) return header;
    return auth.apiKey ? `Bearer ${auth.apiKey}` : undefined;
};

function eligible(ctx: ExtensionContext): boolean {
    if (ctx.model?.provider !== PROVIDER || !officialCodexOrigin(ctx.model.baseUrl)) return false;
    const provider = ctx.modelRegistry.getProvider(PROVIDER);
    return !provider?.baseUrl || officialCodexOrigin(provider.baseUrl);
}

/** Same registry/auth route as the former /usage command; never send credentials to a proxy or redirect. */
export async function queryCodexUsage(ctx: ExtensionContext, signal?: AbortSignal): Promise<CodexUsage> {
    const controller = new AbortController();
    const cancel = () => controller.abort();
    signal?.addEventListener('abort', cancel, { once: true });
    if (signal?.aborted) cancel();
    const timer = setTimeout(cancel, USAGE_TIMEOUT_MS);
    let rejectAbort: () => void = () => {};
    const aborted = new Promise<never>((_resolve, reject) => {
        rejectAbort = () => reject(new Error('Codex usage cancelled or timed out'));
        controller.signal.addEventListener('abort', rejectAbort, { once: true });
        if (controller.signal.aborted) rejectAbort();
    });
    const request = async (): Promise<CodexUsage> => {
        controller.signal.throwIfAborted();
        if (!eligible(ctx)) throw new Error('Codex usage unavailable');
        const model = ctx.model!;
        const providerAuth = await ctx.modelRegistry.getProviderAuth(PROVIDER);
        controller.signal.throwIfAborted();
        if (providerAuth?.auth.baseUrl && !officialCodexOrigin(providerAuth.auth.baseUrl)) throw new Error('Codex usage unavailable');
        const modelAuth = await ctx.modelRegistry.getApiKeyAndHeaders(model);
        controller.signal.throwIfAborted();
        if (!modelAuth.ok || (modelAuth.baseUrl && !officialCodexOrigin(modelAuth.baseUrl))) throw new Error('Codex auth unavailable');
        const authorization = authorizationFrom(modelAuth) ?? authorizationFrom(providerAuth?.auth ?? {});
        if (!authorization) throw new Error('Codex auth unavailable');
        const response = await fetch(CODEX_USAGE_URL, {
            headers: { Authorization: authorization, 'User-Agent': 'pi-usage' },
            redirect: 'error', signal: controller.signal,
        });
        if (!response.ok) throw new Error('Codex usage rejected');
        const text = await response.text();
        controller.signal.throwIfAborted();
        if (Buffer.byteLength(text, 'utf8') > 64 * 1024) throw new Error('Codex usage oversized');
        return parseCodexUsage(JSON.parse(text));
    };
    try { return await Promise.race([request(), aborted]); }
    finally {
        clearTimeout(timer);
        signal?.removeEventListener('abort', cancel);
        controller.signal.removeEventListener('abort', rejectAbort);
    }
}

const modelKey = (ctx: ExtensionContext): string => JSON.stringify([ctx.model?.provider, ctx.model?.id, ctx.model?.baseUrl]);

/** Footer-owned cache. Render only reads it; all auth/network work runs asynchronously. */
export class CodexUsageBadge {
    private usage: CodexUsage | undefined;
    private timer: ReturnType<typeof setInterval> | undefined;
    private request: AbortController | undefined;
    private generation = 0;
    private nextRefresh = 0;
    private key = '';
    private disposed = false;
    private lastBadge: string | undefined;

    constructor(private ctx: ExtensionContext, private redraw: () => void) { this.setContext(ctx); }

    setContext(ctx: ExtensionContext): void {
        if (this.disposed) return;
        this.stop();
        this.ctx = ctx;
        this.key = modelKey(ctx);
        this.usage = undefined;
        this.nextRefresh = 0;
        this.notify();
        if (!eligible(ctx)) return;
        this.timer = setInterval(() => this.tick(), USAGE_TICK_MS);
        this.timer.unref?.();
        this.tick();
    }

    getBadge(): string | undefined {
        if (this.disposed || this.key !== modelKey(this.ctx) || !eligible(this.ctx) || !this.usage || this.usage.resetAt <= Date.now()) return undefined;
        return `[${Math.round(100 - this.usage.usedPercent)}% · ${formatCountdown(this.usage.resetAt)}]`;
    }

    private notify(): void {
        const badge = this.getBadge();
        if (badge !== this.lastBadge) { this.lastBadge = badge; this.redraw(); }
    }

    private tick(): void {
        if (this.key !== modelKey(this.ctx) || !eligible(this.ctx)) { this.setContext(this.ctx); return; }
        this.notify();
        if (this.request || Date.now() < this.nextRefresh) return;
        const generation = this.generation;
        const controller = new AbortController();
        this.request = controller;
        this.nextRefresh = Date.now() + USAGE_REFRESH_MS;
        void queryCodexUsage(this.ctx, controller.signal).then((usage) => {
            if (generation === this.generation && !this.disposed) this.usage = usage;
        }, () => {
            if (generation === this.generation && !this.disposed) this.usage = undefined;
        }).finally(() => {
            if (generation !== this.generation || this.disposed) return;
            this.request = undefined;
            this.notify();
        });
    }

    private stop(): void {
        this.generation++;
        this.request?.abort();
        this.request = undefined;
        if (this.timer) clearInterval(this.timer);
        this.timer = undefined;
    }

    dispose(): void {
        if (this.disposed) return;
        this.disposed = true;
        this.stop();
        this.usage = undefined;
    }
}
