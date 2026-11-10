import assert from 'node:assert/strict';
import test from 'node:test';
import askQuestion from './index.ts';

test('Go questionnaire RPC sends all questions and returns confirmed answers', async () => {
    const previous = process.env.PI_GOUI_ASK_QUESTION;
    process.env.PI_GOUI_ASK_QUESTION = '1';
    try {
        let tool: any;
        askQuestion({ registerTool: (value: any) => { tool = value; }, on() {} } as any);
        const questions = [
            { question: '语言', options: ['Go', 'TS'], multiSelect: true },
            { question: '模式', options: ['快', '慢'] },
        ];
        let calls = 0;
        const ctx = { hasUI: true, mode: 'rpc', ui: {
            input: async (title: string, placeholder: string) => {
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
        const result = await tool.execute('rpc-test', { questions }, undefined, undefined, ctx);
        assert.equal(calls, 1);
        assert.deepEqual(result.details.questions[0].answer, ['Go', 'TS']);
        assert.equal(result.details.questions[1].answer, '快');
        ctx.ui.input = async () => undefined as any;
        const cancelled = await tool.execute('rpc-cancel', { questions }, undefined, undefined, ctx);
        assert.equal(cancelled.details.cancelled, true);
    } finally {
        if (previous === undefined) delete process.env.PI_GOUI_ASK_QUESTION;
        else process.env.PI_GOUI_ASK_QUESTION = previous;
    }
});
