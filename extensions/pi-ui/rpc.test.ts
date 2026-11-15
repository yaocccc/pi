import assert from 'node:assert/strict';
import test from 'node:test';
import { createEventBus } from '@earendil-works/pi-coding-agent';
import { registerExtensionRpc, registerRpcMethod, EXTENSION_RPC_PREFIX } from './rpc.ts';
import { registerSessionActivity } from '../shared/capabilities.ts';

function harness(lifecycle = true) {
    let command: any;
    const events = createEventBus();
    const handlers = new Map<string, () => void>();
    const pi = { events, on: lifecycle ? (name: string, handler: () => void) => handlers.set(name, handler) : undefined, registerCommand(name: string, value: unknown) { assert.equal(name, 'pi-ui-rpc'); command = value; } } as any;
    registerExtensionRpc(pi);
    const replies: any[] = [];
    const ctx = { hasUI: true, mode: 'rpc', isIdle: () => true, hasPendingMessages: () => false, waitForIdle: async () => {}, ui: { notify(message: string, level: string) {
        assert.equal(level, 'info');
        assert.ok(message.startsWith(EXTENSION_RPC_PREFIX));
        replies.push(JSON.parse(message.slice(EXTENSION_RPC_PREFIX.length)));
    } } } as any;
    return { pi, ctx, replies, invoke: (value: unknown) => command.handler(JSON.stringify(value), ctx), command, handlers };
}

test('optional bridge routes explicit methods independent of registration order and unregisters', async () => {
    const h = harness();
    await h.invoke({ id: 'one', method: 'example.get' });
    assert.equal(h.replies.pop().error.code, 'unavailable');
    const remove = registerRpcMethod(h.pi, 'example.get', (params, ctx) => {
        assert.equal(ctx, h.ctx); return params;
    });
    await h.invoke({ id: 'two', method: 'example.get', params: { value: 42 } });
    assert.deepEqual(h.replies.pop(), { id: 'two', result: { value: 42 } });
    remove();
    await h.invoke({ id: 'three', method: 'example.get' });
    assert.equal(h.replies.pop().error.code, 'unavailable');
});

test('bridge validates envelopes, reports structured failures and remains bus-local', async () => {
    const h = harness(), other = harness();
    registerRpcMethod(h.pi, 'example.fail', () => { throw Object.assign(new Error('bad choice'), { code: 'invalid-choice' }); });
    await h.invoke({ id: 'one', method: 'example.fail' });
    assert.deepEqual(h.replies.pop(), { id: 'one', error: { code: 'invalid-choice', message: 'bad choice' } });
    await other.invoke({ id: 'one', method: 'example.fail' });
    assert.equal(other.replies.pop().error.code, 'unavailable');
    for (const input of [null, [], {}, { id: 1, method: 'example.fail' }, { id: 'two', method: 5 }]) {
        await h.invoke(input); assert.equal(h.replies.pop().error.code, 'invalid-request');
    }
    await h.command.handler('{', h.ctx);
    assert.equal(h.replies.pop().error.code, 'extension-error');
});

test('oversized escaped replies return bounded errors and leave the bridge usable', async () => {
    const h = harness();
    // Raw JSON fits the receiver limit, but embedding it in notify.message
    // escapes it again. Limit the final encoding, not just the inner response.
    registerRpcMethod(h.pi, 'example.large', () => '"'.repeat(9 * 1024 * 1024));
    await h.invoke({ id: 'large', method: 'example.large' });
    const failure = h.replies.pop();
    assert.equal(failure.id, 'large');
    assert.equal(failure.error.code, 'response-too-large');
    assert.ok(Buffer.byteLength(JSON.stringify(failure)) < 1024);
    registerRpcMethod(h.pi, 'example.small', () => ({ ready: true }));
    await h.invoke({ id: 'next', method: 'example.small' });
    assert.deepEqual(h.replies.pop(), { id: 'next', result: { ready: true } });
});

test('bridge does not run capability methods outside RPC', async () => {
    const h = harness();
    registerRpcMethod(h.pi, 'example.write', () => { throw new Error('must not run'); });
    h.ctx.mode = 'tui';
    await h.invoke({ id: 'one', method: 'example.write' });
    assert.deepEqual(h.replies, []);
});

test('activity priming keeps the session occupied through deferred idle completion without accumulating waiters', async () => {
    const h = harness();
    let idle = true, pending = false, waits = 0, done!: () => void;
    let completion = new Promise<void>((resolve) => { done = resolve; });
    h.ctx.isIdle = () => idle;
    h.ctx.hasPendingMessages = () => pending;
    h.ctx.waitForIdle = () => { waits++; return completion; };
    const probe = async () => {
        await h.invoke({ id: 'activity', method: 'session.activity' });
        const reply = h.replies.pop();
        assert.equal(reply.error, undefined);
        return reply.result;
    };
    assert.deepEqual(await probe(), { version: 1, idle: true, reasons: [] });
    assert.equal(waits, 0, 'priming never creates an idle waiter');
    idle = false;
    h.handlers.get('agent_start')!();
    idle = true; // SDK has emitted agent_settled, but deferred work has not returned.
    for (let i = 0; i < 5; i++) assert.equal((await probe()).idle, false);
    assert.equal(waits, 1, 'only the observed run holds an idle waiter');
    done();
    await completion;
    assert.equal((await probe()).idle, true);
    pending = true;
    assert.match((await probe()).reasons.join(' '), /queued/);
    pending = false;
    let active = true;
    registerSessionActivity(h.pi, 'Worker', () => active ? 'cleaning up' : undefined);
    assert.deepEqual((await probe()).reasons, ['Worker: cleaning up']);
    active = false;
    assert.equal((await probe()).idle, true);

    completion = new Promise<void>((resolve) => { done = resolve; });
    idle = false;
    h.handlers.get('agent_start')!();
    h.handlers.get('session_start')!();
    idle = true;
    assert.equal((await probe()).idle, true, 'a new session owns a fresh idle generation');
    done();
    await completion;
    assert.equal((await probe()).idle, true, 'late old completion cannot change the new session');
});

test('activity query fails closed on unobserved runs and missing SDK idle support', async () => {
    const h = harness();
    h.handlers.get('agent_start')!();
    await h.invoke({ id: 'late', method: 'session.activity' });
    assert.match(h.replies.pop().result.reasons.join(' '), /not observed/);
    h.ctx.waitForIdle = undefined;
    await h.invoke({ id: 'old-sdk', method: 'session.activity' });
    assert.equal(h.replies.pop().error.code, 'activity-unavailable');
    const noLifecycle = harness(false);
    await noLifecycle.invoke({ id: 'missing-lifecycle', method: 'session.activity' });
    assert.equal(noLifecycle.replies.pop().error.code, 'activity-unavailable');
});
