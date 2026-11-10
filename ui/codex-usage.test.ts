import assert from 'node:assert/strict';
import test, { type TestContext } from 'node:test';
import type { ExtensionContext } from '@earendil-works/pi-coding-agent';
import { CODEX_USAGE_URL, CodexUsageBadge, formatCountdown, officialCodexOrigin, parseCodexUsage, queryCodexUsage, USAGE_REFRESH_MS, USAGE_TICK_MS, USAGE_TIMEOUT_MS } from './codex-usage.ts';

const NOW = 1_800_000_000_000;
const baseUrl = 'https://chatgpt.com/backend-api/codex';
const payload = (used = 23) => ({ rate_limit: {
    primary_window: { used_percent: 81, reset_at: NOW / 1000 + 1800, limit_window_seconds: 18000 },
    secondary_window: { used_percent: used, reset_at: NOW / 1000 + 5 * 86400 + 12 * 3600, limit_window_seconds: 604800 },
} });
const response = (used = 23) => new Response(JSON.stringify(payload(used)));
const flush = () => new Promise<void>((resolve) => setImmediate(resolve));

function context(options: { providerUrl?: string; modelUrl?: string; providerAuthUrl?: string; modelAuthUrl?: string; provider?: string; modelAuth?: object; providerAuth?: object | null } = {}): ExtensionContext {
    return {
        model: { provider: options.provider ?? 'openai-codex', id: 'codex', baseUrl: options.modelUrl ?? baseUrl },
        modelRegistry: {
            getProvider: () => ({ baseUrl: options.providerUrl ?? baseUrl }),
            getProviderAuth: async () => options.providerAuth === null ? undefined : options.providerAuth ?? ({ auth: { apiKey: 'provider-secret', baseUrl: options.providerAuthUrl } }),
            getApiKeyAndHeaders: async () => options.modelAuth ?? ({ ok: true, apiKey: 'model-secret', baseUrl: options.modelAuthUrl }),
        },
    } as unknown as ExtensionContext;
}

function trackerFixture(t: TestContext, ctx = context()) {
    let now = NOW;
    t.mock.method(Date, 'now', () => now);
    const intervals = new Map<NodeJS.Timeout, () => void>();
    t.mock.method(globalThis, 'setInterval', (callback: () => void, delay: number) => {
        assert.equal(delay, USAGE_TICK_MS);
        const handle = {} as NodeJS.Timeout;
        intervals.set(handle, callback);
        return handle;
    });
    t.mock.method(globalThis, 'clearInterval', (handle: NodeJS.Timeout) => {
        assert.ok(intervals.delete(handle), 'timer cleared only once');
    });
    let redraws = 0;
    const badge = new CodexUsageBadge(ctx, () => redraws++);
    t.after(() => badge.dispose());
    return { ctx, badge, intervals, redraws: () => redraws, advance: (ms: number) => { now += ms; }, tick: () => { for (const cb of [...intervals.values()]) cb(); } };
}

test('parsing prefers weekly then secondary, falls back to primary, clamps used rather than remaining', () => {
    assert.deepEqual(parseCodexUsage(payload(), NOW), { usedPercent: 23, resetAt: NOW + 5.5 * 86400000 });
    assert.equal(parseCodexUsage({ rate_limit: { secondary_window: { used_percent: '102', reset_at: '1800000300' } } }).usedPercent, 100);
    assert.equal(parseCodexUsage({ rate_limit: { primary_window: { used_percent: -2, reset_after_seconds: '300' } } }, NOW).usedPercent, 0);
    assert.deepEqual(parseCodexUsage({ rate_limit: { primary_window: { used_percent: 42.5, reset_after_seconds: 300 } } }, NOW), { usedPercent: 42.5, resetAt: NOW + 300000 });
    const reversed = payload();
    reversed.rate_limit.primary_window.limit_window_seconds = 604800;
    reversed.rate_limit.secondary_window.limit_window_seconds = 18000;
    assert.equal(parseCodexUsage(reversed).usedPercent, 81);
    reversed.rate_limit.secondary_window.limit_window_seconds = 604800;
    assert.equal(parseCodexUsage(reversed).usedPercent, 23, 'secondary wins a weekly tie');
    assert.equal(parseCodexUsage({ rate_limit: { primary_window: payload().rate_limit.primary_window, secondary_window: { used_percent: null } } }).usedPercent, 81);
});

test('malformed or absent top-level limits cannot produce a badge', () => {
    for (const value of [null, [], {}, { additional_rate_limits: [{ rate_limit: payload().rate_limit }] },
        { rate_limit: { secondary_window: { used_percent: 'NaN', reset_at: NOW / 1000 } } },
        { rate_limit: { secondary_window: { used_percent: true, reset_at: NOW / 1000 } } },
        { rate_limit: { secondary_window: { used_percent: 20, reset_at: 0 } } },
        { rate_limit: { secondary_window: { used_percent: 20, reset_at: 1e308 } } },
    ]) assert.throws(() => parseCodexUsage(value));
});

test('countdown uses compact day/hour and hour/minute units with minute rounding and expired floor', () => {
    for (const [ms, expected] of [[5.5 * 86400000, '5d12h'], [86400000, '1d0h'], [3660000, '1h1m'], [60000, '1m'], [1, '1m'], [0, '0m'], [-60000, '0m']] as const) {
        assert.equal(formatCountdown(NOW + ms, NOW), expected);
    }
});

test('query uses model bearer header on the fixed official endpoint without redirects or forwarded auth headers', async (t) => {
    t.mock.method(Date, 'now', () => NOW);
    const fetch = t.mock.method(globalThis, 'fetch', async (url: string, init: RequestInit) => {
        assert.equal(url, CODEX_USAGE_URL);
        assert.deepEqual(init.headers, { Authorization: 'Bearer header-secret', 'User-Agent': 'pi-usage' });
        assert.equal(init.redirect, 'error');
        assert.ok(init.signal);
        return response();
    });
    assert.equal((await queryCodexUsage(context({ modelAuth: { ok: true, apiKey: 'unused', headers: { aUtHoRiZaTiOn: 'Bearer header-secret', 'X-Proxy-Secret': 'must-not-forward' } } }))).usedPercent, 23);
    assert.equal(fetch.mock.callCount(), 1);
});

test('provider bearer fallback and API-key auth match the original command', async (t) => {
    const headers: unknown[] = [];
    t.mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => { headers.push(init.headers); return response(); });
    await queryCodexUsage(context({ modelAuth: { ok: true } }));
    await queryCodexUsage(context({ modelAuth: { ok: true }, providerAuth: { auth: { headers: { Authorization: 'Bearer provider-header' } } } }));
    assert.deepEqual(headers, [
        { Authorization: 'Bearer provider-secret', 'User-Agent': 'pi-usage' },
        { Authorization: 'Bearer provider-header', 'User-Agent': 'pi-usage' },
    ]);
});

test('every configured origin and unavailable auth blocks credential-bearing requests', async (t) => {
    const fetch = t.mock.method(globalThis, 'fetch', async () => { throw new Error('must not fetch'); });
    for (const options of [
        { provider: 'other' }, { providerUrl: 'https://proxy.example' }, { modelUrl: 'https://proxy.example' },
        { providerAuthUrl: 'https://proxy.example' }, { modelAuthUrl: 'https://proxy.example' },
        { modelAuth: { ok: false, error: 'secret-auth-error' } }, { modelAuth: { ok: true }, providerAuth: null },
    ]) await assert.rejects(queryCodexUsage(context(options)));
    assert.equal(fetch.mock.callCount(), 0);
    for (const url of ['http://chatgpt.com', 'https://chatgpt.com.evil.test', 'https://chatgpt.com:8443', 'bad', undefined]) assert.equal(officialCodexOrigin(url), false);
    assert.equal(officialCodexOrigin(baseUrl), true);
});

test('HTTP errors, malformed JSON, empty limits and oversized bodies reject', async (t) => {
    const responses = [new Response('', { status: 401 }), new Response('not json'), new Response('{}'), new Response(' '.repeat(65537))];
    t.mock.method(globalThis, 'fetch', async () => responses.shift()!);
    for (let i = 0; i < 4; i++) await assert.rejects(queryCodexUsage(context()));
});

test('bounded timeout includes stalled auth; resolving cancelled auth cannot subsequently fetch', async (t) => {
    let timeout: () => void = () => assert.fail('missing timeout');
    let cleared = 0;
    t.mock.method(globalThis, 'setTimeout', (callback: () => void, delay: number) => {
        assert.equal(delay, USAGE_TIMEOUT_MS);
        timeout = callback;
        return {} as NodeJS.Timeout;
    });
    t.mock.method(globalThis, 'clearTimeout', () => { cleared++; });
    let resolveAuth!: (value: any) => void;
    const ctx = context();
    t.mock.method(ctx.modelRegistry, 'getProviderAuth', () => new Promise((resolve) => { resolveAuth = resolve; }));
    const fetch = t.mock.method(globalThis, 'fetch', async () => response());
    const pending = queryCodexUsage(ctx);
    timeout();
    await assert.rejects(pending);
    resolveAuth({ auth: { apiKey: 'secret' } });
    await flush();
    assert.equal(fetch.mock.callCount(), 0);
    assert.equal(cleared, 1);
});

test('caller cancellation aborts fetch and pre-aborted calls never resolve auth', async (t) => {
    let signal: AbortSignal | undefined;
    t.mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
        signal = init.signal!;
        return new Promise<Response>(() => {});
    });
    const controller = new AbortController();
    const pending = queryCodexUsage(context(), controller.signal);
    await flush();
    controller.abort();
    await assert.rejects(pending);
    assert.equal(signal?.aborted, true);
    const ctx = context();
    const auth = t.mock.method(ctx.modelRegistry, 'getProviderAuth');
    await assert.rejects(queryCodexUsage(ctx, controller.signal));
    assert.equal(auth.mock.callCount(), 0);
});

test('cache is nonblocking, refreshes periodically and redraws the countdown without duplicate requests', async (t) => {
    let used = 23;
    const fetch = t.mock.method(globalThis, 'fetch', async () => response(used));
    const fixture = trackerFixture(t);
    assert.equal(fixture.badge.getBadge(), undefined, 'initial render does not wait for auth/network');
    await flush();
    assert.equal(fixture.badge.getBadge(), '[77% · 5d12h]');
    assert.equal(fixture.redraws(), 1);
    assert.equal(fixture.intervals.size, 1);
    fixture.tick();
    await flush();
    assert.equal(fetch.mock.callCount(), 1);
    fixture.advance(USAGE_REFRESH_MS);
    used = 35;
    fixture.tick();
    fixture.tick();
    await flush();
    assert.equal(fetch.mock.callCount(), 2);
    assert.equal(fixture.badge.getBadge(), '[65% · 5d11h]');
    assert.ok(fixture.redraws() >= 2);
});

test('errors or lost auth clear cached usage and retries can recover', async (t) => {
    t.mock.method(globalThis, 'fetch', async () => response());
    const fixture = trackerFixture(t);
    await flush();
    assert.ok(fixture.badge.getBadge());
    t.mock.method(fixture.ctx.modelRegistry, 'getApiKeyAndHeaders', async () => ({ ok: false, error: 'secret' }));
    fixture.advance(USAGE_REFRESH_MS);
    fixture.tick();
    await flush();
    assert.equal(fixture.badge.getBadge(), undefined);
    t.mock.method(fixture.ctx.modelRegistry, 'getApiKeyAndHeaders', async () => ({ ok: true, apiKey: 'secret' }));
    fixture.advance(USAGE_REFRESH_MS);
    fixture.tick();
    await flush();
    assert.ok(fixture.badge.getBadge());
    t.mock.method(globalThis, 'fetch', async () => { throw new Error('network-secret'); });
    fixture.advance(USAGE_REFRESH_MS);
    fixture.tick();
    await flush();
    assert.equal(fixture.badge.getBadge(), undefined);
});

test('model switches abort pending requests, ignore late results and refetch for the newly selected model', async (t) => {
    let resolveFirst!: (value: Response) => void;
    let firstSignal: AbortSignal | undefined;
    let calls = 0;
    t.mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
        if (++calls === 1) { firstSignal = init.signal!; return new Promise<Response>((resolve) => { resolveFirst = resolve; }); }
        return response(45);
    });
    const fixture = trackerFixture(t);
    await flush();
    fixture.badge.setContext(context({ provider: 'other' }));
    assert.equal(firstSignal?.aborted, true);
    assert.equal(fixture.intervals.size, 0);
    fixture.badge.setContext(context());
    await flush();
    assert.equal(fixture.badge.getBadge(), '[55% · 5d12h]');
    resolveFirst(response(99));
    await flush();
    assert.equal(fixture.badge.getBadge(), '[55% · 5d12h]');
    assert.equal(calls, 2);
});

test('render hides mutated models immediately, and timer reconciles model/auth origin changes', async (t) => {
    t.mock.method(globalThis, 'fetch', async () => response());
    const fixture = trackerFixture(t);
    await flush();
    assert.ok(fixture.badge.getBadge());
    (fixture.ctx.model as any).provider = 'other';
    assert.equal(fixture.badge.getBadge(), undefined);
    fixture.tick();
    assert.equal(fixture.intervals.size, 0);
    fixture.badge.setContext(context());
    await flush();
    assert.ok(fixture.badge.getBadge());
    const proxy = context({ providerUrl: 'https://proxy.example' });
    fixture.badge.setContext(proxy);
    assert.equal(fixture.badge.getBadge(), undefined);
    assert.equal(fixture.intervals.size, 0);
});

test('expired limits hide; disposal clears timers, cancels pending work and forbids late redraws', async (t) => {
    let resolveFetch!: (value: Response) => void;
    let signal: AbortSignal | undefined;
    t.mock.method(globalThis, 'fetch', async () => response());
    const fixture = trackerFixture(t);
    await flush();
    fixture.advance(6 * 86400000);
    assert.equal(fixture.badge.getBadge(), undefined);
    t.mock.method(globalThis, 'fetch', async (_url: unknown, init: RequestInit) => {
        signal = init.signal!;
        return new Promise<Response>((resolve) => { resolveFetch = resolve; });
    });
    fixture.badge.setContext(context());
    await flush();
    const redraws = fixture.redraws();
    fixture.badge.dispose();
    fixture.badge.dispose();
    assert.equal(signal?.aborted, true);
    assert.equal(fixture.intervals.size, 0);
    resolveFetch(response());
    await flush();
    fixture.badge.setContext(context());
    assert.equal(fixture.badge.getBadge(), undefined);
    assert.equal(fixture.redraws(), redraws);
});
