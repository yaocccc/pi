import assert from 'node:assert/strict';
import test from 'node:test';
import type { ExtensionAPI, ExtensionContext, ToolResultEvent, ToolResultEventResult } from '@earendil-works/pi-coding-agent';
import extension from './index.ts';

type Handler = (event: ToolResultEvent, ctx: ExtensionContext) => Promise<ToolResultEventResult | void>;

function setup() {
    let handler: Handler | undefined;
    extension({
        on: (name: string, callback: Handler) => {
            assert.equal(name, 'tool_result');
            handler = callback;
            return () => {};
        },
        events: { emit: () => {} },
    } as unknown as ExtensionAPI);
    assert.ok(handler);
    const run = handler;
    return async (text: string, overrides: Partial<ToolResultEvent> = {}) => {
        const event: ToolResultEvent = {
            type: 'tool_result', toolCallId: 'offline-fixture', toolName: 'bash',
            input: {}, content: [{ type: 'text', text }], details: undefined, isError: false,
            ...overrides,
        };
        const original = structuredClone(event);
        const result = await run(event, {} as ExtensionContext);
        assert.deepEqual(event, original, 'filter must not mutate the source event');
        const content = result?.content ?? event.content;
        const block = content[0];
        assert.equal(block?.type, 'text');
        if (block?.type !== 'text') throw new Error('Expected a text result');
        return { text: block.text, result };
    };
}

const hex = 'a1'.repeat(32);

test('all tools mask bare and prefixed 32-byte hex values, including public hashes', async () => {
    const filter = setup();
    for (const toolName of ['read', 'bash', 'web_fetch', 'custom_tool', 'codemode']) {
        for (const value of [hex, hex.toUpperCase()]) {
            const text = [
                `transaction_hash=0x${value}`, `block_hash=0X${value}`, `topic=0x${value}`,
                `bytes32=0x${value}`, `SHA-256=${value}`, JSON.stringify({ transactionHash: `0x${value}` }),
                value, `0x${value}`,
            ].join('\n');
            const expected = [
                'transaction_hash=[WEB3_PRIVATE_KEY_REDACTED]', 'block_hash=[WEB3_PRIVATE_KEY_REDACTED]',
                'topic=[WEB3_PRIVATE_KEY_REDACTED]', 'bytes32=[WEB3_PRIVATE_KEY_REDACTED]',
                'SHA-256=[WEB3_PRIVATE_KEY_REDACTED]', '{"transactionHash":"[WEB3_PRIVATE_KEY_REDACTED]"}',
                '[WEB3_PRIVATE_KEY_REDACTED]', '[WEB3_PRIVATE_KEY_REDACTED]',
            ].join('\n');
            assert.equal((await filter(text, { toolName })).text, expected);
        }
    }
});

test('tool_result masks explicitly private hex keys, including JSON, and adjacent hashes', async () => {
    const filter = setup();
    const inputs = [
        [`private_key=0x${hex}`, 'private_key=[WEB3_PRIVATE_KEY_REDACTED]'],
        [`wallet_key: '${hex}'`, "wallet_key: '[WEB3_PRIVATE_KEY_REDACTED]'"],
        [`evm_private_key = "0x${hex}"`, 'evm_private_key = "[WEB3_PRIVATE_KEY_REDACTED]"'],
        [JSON.stringify({ privateKey: `0x${hex}` }), '{"privateKey":"[WEB3_PRIVATE_KEY_REDACTED]"}'],
        [`'wallet-key': '${hex}'`, "'wallet-key': '[WEB3_PRIVATE_KEY_REDACTED]'"],
    ];
    for (const [input, expected] of inputs) {
        const output = await filter(`${input}\ntransaction_hash=0x${hex}`);
        assert.equal(output.text, `${expected}\ntransaction_hash=[WEB3_PRIVATE_KEY_REDACTED]`);
    }
});

test('tool_result masks database passwords while retaining the URI address and separators', async () => {
    const filter = setup();
    // MongoDB/PostgreSQL use three capture groups; MySQL/Redis use two.
    for (const scheme of ['mysql', 'redis', 'mongodb', 'postgresql', 'postgres']) {
        const input = `${scheme}://user:fixture-password@localhost:6379/0?option=yes`;
        const output = await filter(input);
        assert.equal(output.text, `${scheme}://user:[REDACTED]@localhost:6379/0?option=yes`);
        assert.ok(!output.text.includes('$3'));
    }
});

test('tool_result keeps representative token, password and PEM protections, including failed results', async () => {
    const filter = setup();
    const text = [
        `api_key=sk-${'z'.repeat(24)}`, 'token=fixture-token', 'password="fixture-password"',
        'Authorization: Bearer fixture-bearer-token-for-test',
        '-----BEGIN RSA PRIVATE KEY-----\nfixture-not-a-real-key\n-----END RSA PRIVATE KEY-----',
    ].join('\n');
    const output = await filter(text);
    for (const secret of [`sk-${'z'.repeat(24)}`, 'fixture-token', 'fixture-password', 'fixture-bearer-token-for-test', 'fixture-not-a-real-key']) {
        assert.ok(!output.text.includes(secret), `must redact ${secret}`);
    }
    assert.ok(output.text.includes('[PRIVATE_KEY_REDACTED]'));
    assert.equal((await filter(text, { isError: true })).text, output.text);
});

test('hex protection applies to failed tools and example files without hiding the entire example', async () => {
    const filter = setup();
    const text = `example=0x${hex}`;
    const expected = 'example=[WEB3_PRIVATE_KEY_REDACTED]';
    assert.equal((await filter(text, { isError: true })).text, expected);
    assert.equal((await filter(text, { toolName: 'read', input: { path: '.env.example' } })).text, expected);
    assert.equal((await filter('ordinary example', { toolName: 'read', input: { path: '.env.example' } })).text, 'ordinary example');
});

test('only exact 64-digit hex tokens are masked', async () => {
    const filter = setup();
    for (const text of ['a'.repeat(63), 'a'.repeat(65), `0x${'a'.repeat(65)}`, `id_${hex}`]) {
        assert.equal((await filter(text)).text, text);
    }
});

test('extension has no user-message hook and leaves user hex unchanged', () => {
    const callbacks = new Map<string, unknown>();
    extension({
        on(name: string, callback: unknown) { callbacks.set(name, callback); return () => {}; },
        events: { emit: () => {} },
    } as unknown as ExtensionAPI);
    assert.deepEqual([...callbacks.keys()], ['tool_result']);
    const message = { role: 'user', content: `private_key=0x${hex}` };
    const original = structuredClone(message);
    for (const name of ['input', 'before_agent_start', 'context']) {
        assert.equal(callbacks.has(name), false, `no ${name} hook may transform user input`);
    }
    assert.deepEqual(message, original);
});
