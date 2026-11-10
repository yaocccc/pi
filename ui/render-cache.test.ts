import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import test from 'node:test';
import { estimateLineBytes, RenderCache, RenderCacheBudget, renderCacheBudget, withRenderCachePass } from './render-cache.ts';

test('snapshots obey the budget and release their allocation on clear/replacement', () => {
    const budget = new RenderCacheBudget(10);
    const cache = new RenderCache<string>(budget);
    cache.set('first', 7);
    assert.equal(cache.get(), 'first');
    assert.equal(budget.usedBytes, 7);
    assert.equal(budget.size, 1);
    cache.set('second', 4);
    assert.equal(cache.get(), 'second');
    assert.equal(budget.usedBytes, 4);
    cache.clear();
    cache.clear();
    assert.equal(cache.get(), undefined);
    assert.equal(budget.usedBytes, 0);
    assert.equal(budget.size, 0);
});

test('oversize or invalid admissions do not evict other snapshots', () => {
    const budget = new RenderCacheBudget(10);
    const kept = new RenderCache<string>(budget);
    const rejected = new RenderCache<string>(budget);
    kept.set('kept', 10);
    for (const bytes of [11, Infinity, NaN, -1]) {
        rejected.set('not retained', bytes);
        assert.equal(rejected.get(), undefined);
        assert.equal(kept.get(), 'kept');
        assert.equal(budget.usedBytes, 10);
    }
    kept.set('too large replacement', 11);
    assert.equal(kept.get(), undefined, 'a rejected replacement must not return stale content');
    assert.equal(budget.usedBytes, 0);
});

test('full-history scans larger than the budget do not thrash within a pass', () => {
    const budget = new RenderCacheBudget(20);
    const caches = Array.from({ length: 6 }, () => new RenderCache<number>(budget));
    const misses = caches.map(() => 0);
    for (let pass = 0; pass < 4; pass++) budget.run(() => {
        caches.forEach((cache, index) => {
            if (cache.get() === undefined) {
                misses[index]++;
                cache.set(index, 10);
            }
            assert.ok(budget.usedBytes <= budget.maxBytes);
        });
    });
    assert.deepEqual(misses, [1, 1, 4, 4, 4, 4]);
    assert.equal(budget.size, 2);
});

test('later passes evict cold entries while retaining entries touched in this pass', () => {
    const budget = new RenderCacheBudget(20);
    const a = new RenderCache<string>(budget);
    const b = new RenderCache<string>(budget);
    const c = new RenderCache<string>(budget);
    budget.run(() => { a.set('a', 10); b.set('b', 10); });
    budget.run(() => {
        assert.equal(a.get(), 'a');
        c.set('c', 10);
        assert.equal(c.get(), 'c');
        assert.equal(a.get(), 'a');
        assert.equal(b.get(), undefined);
    });
    assert.equal(budget.usedBytes, 20);
});

test('failed admission does not partially evict cold entries', () => {
    const budget = new RenderCacheBudget(20);
    const cold = new RenderCache<string>(budget);
    const hot = new RenderCache<string>(budget);
    const rejected = new RenderCache<string>(budget);
    budget.run(() => { cold.set('cold', 5); hot.set('hot', 15); });
    budget.run(() => {
        hot.get();
        rejected.set('rejected', 10);
        assert.equal(rejected.get(), undefined);
        assert.equal(cold.get(), 'cold');
        assert.equal(hot.get(), 'hot');
    });
});

test('nested passes share protection and exceptions restore pass depth', () => {
    const budget = new RenderCacheBudget(10);
    const a = new RenderCache<string>(budget);
    const b = new RenderCache<string>(budget);
    assert.throws(() => budget.run(() => {
        a.set('a', 10);
        budget.run(() => b.set('b', 10));
        assert.equal(b.get(), undefined);
        throw new Error('render failed');
    }), /render failed/);
    assert.equal(budget.run(() => { b.set('b', 10); return 42; }), 42);
    assert.equal(a.get(), undefined);
    assert.equal(b.get(), 'b');
});

test('repeated hits charge protection once and clearing frees it within the same pass', () => {
    const budget = new RenderCacheBudget(20);
    const a = new RenderCache<string>(budget);
    const b = new RenderCache<string>(budget);
    a.set('a', 10);
    budget.run(() => {
        a.get();
        a.get();
        b.set('b', 10);
        assert.equal(b.get(), 'b');
        a.clear();
        a.set('replacement', 10);
        assert.equal(a.get(), 'replacement');
        budget.clear();
        b.set('whole budget', 20);
        assert.equal(b.get(), 'whole budget');
        assert.equal(budget.usedBytes, 20);
    });
});

test('outside a pass, admissions evict the least recently used entries', () => {
    const budget = new RenderCacheBudget(20);
    const a = new RenderCache<string>(budget);
    const b = new RenderCache<string>(budget);
    const c = new RenderCache<string>(budget);
    a.set('a', 10);
    b.set('b', 10);
    a.get();
    c.set('c', 10);
    assert.equal(a.get(), 'a');
    assert.equal(b.get(), undefined);
    assert.equal(c.get(), 'c');
});

test('clearing a budget clears all live slots and permits subsequent reuse', () => {
    const budget = new RenderCacheBudget(10);
    const a = new RenderCache<string>(budget);
    const b = new RenderCache<string>(budget);
    a.set('a', 4);
    b.set('b', 6);
    budget.clear();
    assert.equal(a.get(), undefined);
    assert.equal(b.get(), undefined);
    assert.equal(budget.usedBytes, 0);
    assert.equal(budget.size, 0);
    budget.clear();
    a.set('again', 10);
    assert.equal(a.get(), 'again');
    assert.equal(budget.usedBytes, 10);
});

test('zero-sized entries still have bounded metadata and invalid limits are rejected', () => {
    const budget = new RenderCacheBudget(1);
    const cache = new RenderCache<number>(budget);
    cache.set(1, 0);
    assert.equal(budget.usedBytes, 1);
    const disabled = new RenderCache<number>(new RenderCacheBudget(0));
    disabled.set(1, 0);
    assert.equal(disabled.get(), undefined);
    for (const limit of [-1, Infinity, NaN, 0.5]) assert.throws(() => new RenderCacheBudget(limit), RangeError);
});

test('default budget and helper use a shared synchronous render pass', () => {
    assert.equal(renderCacheBudget.maxBytes, 128 * 1024 * 1024);
    const a = new RenderCache<string>();
    const b = new RenderCache<string>();
    try {
        withRenderCachePass(() => {
            a.set('a', renderCacheBudget.maxBytes);
            withRenderCachePass(() => b.set('b', 1));
            assert.equal(b.get(), undefined);
        });
        assert.equal(a.get(), 'a');
    } finally { renderCacheBudget.clear(); }
});

test('line size estimate includes array/string allowances and UTF-16 code units', () => {
    assert.equal(estimateLineBytes([]), 24);
    assert.equal(estimateLineBytes(['a', '😀']), 24 + 2 * 32 + 6);
});

test('the budget index keeps neither abandoned slots nor their values alive', () => {
    const moduleUrl = new URL('./render-cache.ts', import.meta.url).href;
    const result = spawnSync(process.execPath, ['--expose-gc', '--experimental-transform-types', '--input-type=module', '-e', `
        import assert from 'node:assert/strict';
        import { setImmediate } from 'node:timers/promises';
        import { RenderCache, RenderCacheBudget } from ${JSON.stringify(moduleUrl)};
        const budget = new RenderCacheBudget(100);
        const refs = (() => {
            const cache = new RenderCache(budget);
            const value = { lines: ['retained only by the slot'] };
            cache.set(value, 100);
            return [new WeakRef(cache), new WeakRef(value)];
        })();
        for (let i = 0; i < 4; i++) { await setImmediate(); global.gc(); }
        assert.equal(refs[0].deref(), undefined);
        assert.equal(refs[1].deref(), undefined);
        assert.equal(budget.usedBytes, 0);
        assert.equal(budget.size, 0);
    `], { encoding: 'utf8', timeout: 10_000 });
    assert.equal(result.status, 0, result.stderr || String(result.error));
});
