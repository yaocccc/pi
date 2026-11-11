import assert from 'node:assert/strict';
import test from 'node:test';
import askQuestion from './index.ts';
import { validateGoResponse } from './rpc.ts';

for (const capability of ['PI_GOUI_ASK_QUESTION', 'PI_GOUI_PROTOCOL']) {
test(`${capability}: Go questionnaire RPC sends all questions and returns confirmed answers`, async () => {
    const previous = { PI_GOUI_ASK_QUESTION: process.env.PI_GOUI_ASK_QUESTION, PI_GOUI_PROTOCOL: process.env.PI_GOUI_PROTOCOL };
    delete process.env.PI_GOUI_ASK_QUESTION;
    delete process.env.PI_GOUI_PROTOCOL;
    process.env[capability] = '1';
    try {
        let tool: any;
        askQuestion({ registerTool: (value: any) => { tool = value; }, on() {} } as any);
        const questions = [
            { question: '语言', options: ['Go', 'TS'], multiSelect: true },
            { question: '模式', options: ['快', '慢'] },
        ];
        let calls = 0;
        const controller = new AbortController();
        const ctx = { hasUI: true, mode: 'rpc', ui: {
            input: async (title: string, placeholder: string, options?: { signal?: AbortSignal }) => {
                assert.equal(options?.signal, controller.signal);
                calls++;
                assert.equal(title, 'pi-goui:ask-question:v1');
                const payload = JSON.parse(placeholder);
                assert.equal(payload.questions.length, 2);
                assert.equal(payload.questions[0].multiSelect, true);
                assert.equal(payload.review, true);
                return JSON.stringify({ answers: [
                    { answers: ['Go', 'TS'], completed: true, wasCustom: false },
                    { answers: ['快'], completed: true, wasCustom: false },
                ], cancelled: false });
            },
        } };
        const result = await tool.execute('rpc-test', { questions }, controller.signal, undefined, ctx);
        assert.equal(calls, 1);
        assert.deepEqual(result.details.questions[0].answer, ['Go', 'TS']);
        assert.equal(result.details.questions[1].answer, '快');
        ctx.ui.input = async () => undefined as any;
        const cancelled = await tool.execute('rpc-cancel', { questions }, undefined, undefined, ctx);
        assert.equal(cancelled.details.cancelled, true);
        ctx.ui.input = async () => JSON.stringify({ cancelled: true, answers: [{ answers: ['draft'] }] });
        const partial = await tool.execute('rpc-partial-cancel', { questions }, undefined, undefined, ctx);
        assert.equal(partial.details.cancelled, true);
        assert.deepEqual(partial.details.questions.map((question: any) => question.answer), [null, null]);
        ctx.ui.input = async () => JSON.stringify({ cancelled: false, answers: [] });
        await assert.rejects(tool.execute('rpc-invalid', { questions }, undefined, undefined, ctx), /Invalid Go UI/);
    } finally {
        for (const [key, value] of Object.entries(previous)) {
            if (value === undefined) delete process.env[key];
            else process.env[key] = value;
        }
    }
});
}

const single = [{ question: 'Pick', options: ['A', 'B'] }];
const response = (answer: Record<string, unknown>) => ({ answers: [{ completed: true, ...answer }], cancelled: false });

test('Go response validation rejects malformed, incomplete and fabricated answers', () => {
    for (const value of [null, [], {}, { cancelled: 'false' }, { cancelled: false, answers: [] },
        response({ answers: [] }), response({ answers: ['A'], completed: false }),
        response({ answers: [42] }), response({ answers: ['A', 'B'] }),
        response({ answers: ['unknown'] }), response({ answers: [' '] }),
        response({ answers: ['A'], customAnswers: ['unselected'] }),
        response({ answers: ['A'], customAnswers: 'A' }), response({ answers: ['A'], wasCustom: 'yes' }),
        response({ answers: ['A'], wasCustom: true }),
    ]) assert.throws(() => validateGoResponse(value, single), /Invalid Go UI/);
    assert.throws(() => validateGoResponse(response({ answers: ['A', 'A'] }), [{ ...single[0]!, multiSelect: true }]), /Invalid Go UI/);
});

test('Go response validation accepts mixed/custom answers and discards cancelled drafts', () => {
    assert.deepEqual(validateGoResponse({ cancelled: true, answers: [{ answers: ['unsubmitted'] }] }, single), { cancelled: true, answers: [] });
    const mixed = validateGoResponse(response({ answers: ['A', 'custom'], customAnswers: ['custom'], wasCustom: false }), [{ ...single[0]!, multiSelect: true }]);
    assert.deepEqual(mixed.answers[0], { answers: ['A', 'custom'], customAnswers: ['custom'], wasCustom: false, completed: true });
    assert.equal(validateGoResponse(response({ answers: ['custom'], customAnswers: ['custom'], wasCustom: true }), single).answers[0]!.wasCustom, true);
});
