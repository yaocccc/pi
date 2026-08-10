import {
    type ExtensionAPI,
    type ExtensionCommandContext,
} from '@earendil-works/pi-coding-agent';
import { Key, matchesKey, truncateToWidth, visibleWidth } from '@earendil-works/pi-tui';

const CODEX_PROVIDER = 'openai-codex';
const USAGE_URL = 'https://chatgpt.com/backend-api/wham/usage';
const TIMEOUT_MS = 15_000;

type RateLimitWindow = {
    label: string;
    group?: string;
    remaining: number;
    windowMinutes?: number;
    resetAt?: number;
};

type UsageData = {
    plan?: string;
    windows: RateLimitWindow[];
    credits?: string;
    resetCredits?: number;
};

class UsageError extends Error {}

const asObject = (value: unknown): Record<string, unknown> | undefined =>
    value && typeof value === 'object' && !Array.isArray(value)
        ? value as Record<string, unknown>
        : undefined;

const asString = (value: unknown): string | undefined => {
    if (typeof value !== 'string') return undefined;
    const sanitized = value
        .replace(/[\u0000-\u001F\u007F]/gu, '')
        .trim()
        .slice(0, 160);
    return sanitized || undefined;
};

const asNumber = (value: unknown): number | undefined => {
    if (typeof value === 'number' && Number.isFinite(value)) return value;
    if (typeof value === 'string' && value.trim()) {
        const number = Number(value);
        return Number.isFinite(number) ? number : undefined;
    }
    return undefined;
};

const clampPercent = (value: number): number => Math.min(100, Math.max(0, value));

const officialOrigin = (baseUrl: string | undefined): boolean => {
    try {
        return new URL(baseUrl ?? '').origin === 'https://chatgpt.com';
    } catch {
        return false;
    }
};

const authorizationFrom = (auth: {
    apiKey?: string;
    headers?: Record<string, string | null | undefined>;
}): string | undefined => {
    const authorization = Object.entries(auth.headers ?? {}).find(
        ([name]) => name.toLowerCase() === 'authorization',
    )?.[1];
    if (authorization && /^Bearer\s+\S+$/iu.test(authorization)) return authorization;
    return auth.apiKey ? `Bearer ${auth.apiKey}` : undefined;
};

const formatWindow = (minutes: number | undefined): string | undefined => {
    if (!minutes || !Number.isFinite(minutes) || minutes <= 0) return undefined;
    if (minutes % 10_080 === 0) return `${minutes / 10_080}w`;
    if (minutes % 1_440 === 0) return `${minutes / 1_440}d`;
    if (minutes % 60 === 0) return `${minutes / 60}h`;
    return `${minutes}m`;
};

const formatReset = (epochSeconds: number | undefined): string | undefined => {
    if (!epochSeconds) return undefined;
    const reset = new Date(epochSeconds * 1_000);
    if (Number.isNaN(reset.getTime())) return undefined;
    const time = `${reset.getHours().toString().padStart(2, '0')}:${reset
        .getMinutes()
        .toString()
        .padStart(2, '0')}`;
    return reset.toDateString() === new Date().toDateString()
        ? time
        : `${time} ${reset.getDate()} ${reset.toLocaleDateString(undefined, { month: 'short' })}`;
};

const addWindows = (
    windows: RateLimitWindow[],
    rateLimit: unknown,
    group?: string,
): void => {
    const details = asObject(rateLimit);
    if (!details) return;
    for (const [key, label] of [['primary_window', 'Primary'], ['secondary_window', 'Secondary']] as const) {
        const window = asObject(details[key]);
        const used = asNumber(window?.used_percent);
        if (used === undefined) continue;
        const seconds = asNumber(window?.limit_window_seconds);
        windows.push({
            label,
            group,
            remaining: 100 - clampPercent(used),
            ...(seconds && seconds > 0 ? { windowMinutes: Math.ceil(seconds / 60) } : {}),
            ...(asNumber(window?.reset_at) !== undefined ? { resetAt: asNumber(window?.reset_at) } : {}),
        });
    }
};

const parseUsage = (payload: unknown): UsageData => {
    const data = asObject(payload);
    if (!data) throw new UsageError('Codex usage returned an invalid response.');

    const windows: RateLimitWindow[] = [];
    addWindows(windows, data.rate_limit);
    for (const additional of Array.isArray(data.additional_rate_limits) ? data.additional_rate_limits : []) {
        const item = asObject(additional);
        if (!item) continue;
        const group = asString(item.limit_name) ?? asString(item.metered_feature);
        addWindows(windows, item.rate_limit, group);
    }

    const credits = asObject(data.credits);
    let creditText: string | undefined;
    if (credits?.has_credits === true) {
        if (credits.unlimited === true) creditText = 'unlimited';
        else {
            const balance = asNumber(credits.balance);
            creditText = balance === undefined ? 'available' : String(balance);
        }
    } else if (credits?.has_credits === false) {
        creditText = 'none';
    }

    const resetCredits = asNumber(asObject(data.rate_limit_reset_credits)?.available_count);
    if (windows.length === 0 && creditText === undefined && resetCredits === undefined) {
        throw new UsageError('Codex usage has no displayable limits.');
    }
    return {
        plan: asString(data.plan_type),
        windows,
        credits: creditText,
        ...(resetCredits !== undefined ? { resetCredits: Math.max(0, Math.floor(resetCredits)) } : {}),
    };
};

const displayLines = (usage: UsageData): string[] => {
    const lines = [`OpenAI Codex Usage${usage.plan ? ` · ${usage.plan}` : ''}`];
    let previousGroup: string | undefined;
    for (const window of usage.windows) {
        if (window.group !== previousGroup && window.group) lines.push(`${window.group}:`);
        previousGroup = window.group;
        const span = formatWindow(window.windowMinutes);
        const reset = formatReset(window.resetAt);
        lines.push(`${window.label}${span ? ` (${span})` : ''}: ${Math.round(window.remaining)}% left${reset ? ` · resets ${reset}` : ''}`);
    }
    if (usage.credits !== undefined) lines.push(`Credits: ${usage.credits}`);
    if (usage.resetCredits !== undefined) lines.push(`Reset credits: ${usage.resetCredits} available`);
    return lines;
};

const showUsage = async (ctx: ExtensionCommandContext, usage: UsageData): Promise<void> => {
    await ctx.ui.custom(
        (_tui, theme, _keybindings, done) => ({
            invalidate() {},
            handleInput(data: string) {
                if (matchesKey(data, Key.escape) || matchesKey(data, Key.ctrl('c')) || matchesKey(data, Key.enter)) {
                    done(undefined);
                }
            },
            render(width: number) {
                const inner = Math.max(1, width - 2);
                const pad = (text: string): string => {
                    const truncated = truncateToWidth(text, inner, '…');
                    return truncated + ' '.repeat(Math.max(0, inner - visibleWidth(truncated)));
                };
                const border = (text: string) => theme.fg('muted', text);
                const barWidth = Math.max(8, Math.min(28, inner - 28));
                const rows: string[] = [];
                let previousGroup: string | undefined;
                for (const window of usage.windows) {
                    if (window.group !== previousGroup && window.group) {
                        rows.push(theme.bold(theme.fg('muted', ` ${window.group}`)));
                    }
                    previousGroup = window.group;
                    const filled = Math.round((window.remaining / 100) * barWidth);
                    const color = window.remaining < 25 ? 'warning' : 'success';
                    const span = formatWindow(window.windowMinutes);
                    const reset = formatReset(window.resetAt);
                    const label = `${window.label}${span ? ` (${span})` : ''}`;
                    rows.push(
                        ` ${theme.fg(color, '■')} ${label.padEnd(17)} ${theme.fg(color, '█'.repeat(filled))}${theme.fg('dim', '░'.repeat(barWidth - filled))} ${Math.round(window.remaining).toString().padStart(3)}%${reset ? theme.fg('muted', `  ${reset}`) : ''}`,
                    );
                }
                if (usage.credits !== undefined) rows.push(` ${theme.fg('accent', '■')} Credits: ${usage.credits}`);
                if (usage.resetCredits !== undefined) rows.push(` ${theme.fg('accent', '■')} Reset credits: ${usage.resetCredits} available`);
                const title = theme.bold(theme.fg('accent', 'OpenAI Codex Usage'));
                const plan = usage.plan ? theme.fg('muted', ` ${usage.plan}`) : '';
                return [
                    border(`╭${'─'.repeat(inner)}╮`),
                    `${border('│')}${pad(` ${title}${plan}`)}${border('│')}`,
                    `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                    ...rows.map((row) => `${border('│')}${pad(row)}${border('│')}`),
                    `${border('├')}${border('─'.repeat(inner))}${border('┤')}`,
                    `${border('│')}${pad(theme.fg('dim', ' Enter / Esc to close'))}${border('│')}`,
                    border(`╰${'─'.repeat(inner)}╯`),
                ];
            },
        }),
        {
            overlay: true,
            overlayOptions: { anchor: 'center', width: 72, minWidth: 44, maxHeight: '90%', margin: 1 },
        },
    );
};

const queryUsage = async (ctx: ExtensionCommandContext): Promise<UsageData> => {
    const provider = ctx.modelRegistry.getProvider(CODEX_PROVIDER);
    if (provider?.baseUrl && !officialOrigin(provider.baseUrl)) {
        throw new UsageError('Codex usage is unavailable for a custom or proxy origin.');
    }
    if (ctx.model?.provider === CODEX_PROVIDER && !officialOrigin(ctx.model.baseUrl)) {
        throw new UsageError('Codex usage is unavailable for a custom or proxy origin.');
    }

    const providerAuth = await ctx.modelRegistry.getProviderAuth(CODEX_PROVIDER);
    if (providerAuth?.auth.baseUrl && !officialOrigin(providerAuth.auth.baseUrl)) {
        throw new UsageError('Codex usage is unavailable for a custom or proxy origin.');
    }

    const modelAuth = ctx.model?.provider === CODEX_PROVIDER
        ? await ctx.modelRegistry.getApiKeyAndHeaders(ctx.model)
        : undefined;
    if (modelAuth && !modelAuth.ok) throw new UsageError('OpenAI Codex authentication is unavailable.');
    const authorization = authorizationFrom(modelAuth?.ok ? modelAuth : {})
        ?? authorizationFrom(providerAuth?.auth ?? {});
    if (!authorization) throw new UsageError('OpenAI Codex authentication is unavailable.');

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), TIMEOUT_MS);
    try {
        const response = await fetch(USAGE_URL, {
            headers: {
                Authorization: authorization,
                'User-Agent': 'pi-usage',
            },
            signal: controller.signal,
        });
        if (!response.ok) throw new UsageError('Codex usage request was rejected.');
        try {
            const text = await response.text();
            if (Buffer.byteLength(text, 'utf8') > 64 * 1024) {
                throw new UsageError('Codex usage returned an oversized response.');
            }
            return parseUsage(JSON.parse(text) as unknown);
        } catch (error) {
            if (error instanceof UsageError) throw error;
            throw new UsageError('Codex usage returned an invalid response.');
        }
    } catch (error) {
        if (controller.signal.aborted) throw new UsageError('Codex usage request timed out after 15 seconds.');
        if (error instanceof UsageError) throw error;
        throw new UsageError('Could not retrieve Codex usage. Please try again.');
    } finally {
        clearTimeout(timer);
    }
};

export default function (pi: ExtensionAPI) {
    pi.registerCommand('usage', {
        description: '查看 OpenAI Codex 订阅用量',
        handler: async (_args, ctx) => {
            let usage: UsageData;
            ctx.ui.setStatus('usage', 'Loading Codex usage…');
            try {
                usage = await queryUsage(ctx);
            } catch (error) {
                const message = error instanceof UsageError
                    ? error.message
                    : 'Could not retrieve Codex usage. Please try again.';
                ctx.ui.notify(message, 'error');
                return;
            } finally {
                ctx.ui.setStatus('usage', undefined);
            }

            if (ctx.mode !== 'tui') {
                ctx.ui.notify(displayLines(usage).join('\n'), 'info');
                return;
            }
            await showUsage(ctx, usage);
        },
    });
}
