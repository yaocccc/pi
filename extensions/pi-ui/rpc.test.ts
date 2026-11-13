import assert from 'node:assert/strict';
import test from 'node:test';
import { createEventBus } from '@earendil-works/pi-coding-agent';
import { registerExtensionRpc, registerRpcMethod, EXTENSION_RPC_PREFIX } from './rpc.ts';

function harness() {
    let command: any;
    const events = createEventBus();
    const pi = { events, registerCommand(name: string, value: unknown) { assert.equal(name, 'pi-ui-rpc'); command = value; } } as any;
    registerExtensionRpc(pi);
    const replies: any[] = [];
    const ctx = { hasUI: true, mode: 'rpc', ui: { notify(message: string, level: string) {
        assert.equal(level, 'info');
        assert.ok(message.startsWith(EXTENSION_RPC_PREFIX));
        replies.push(JSON.parse(message.slice(EXTENSION_RPC_PREFIX.length)));
    } } } as any;
    return { pi, ctx, replies, invoke: (value: unknown) => command.handler(JSON.stringify(value), ctx), command };
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
