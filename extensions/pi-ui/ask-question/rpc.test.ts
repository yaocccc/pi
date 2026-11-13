import assert from 'node:assert/strict';
import test from 'node:test';
import { EventEmitter } from 'node:events';
import askQuestion from '../../ask-question/index.ts';
import { askQuestions } from './rpc.ts';
import { registerAskQuestionUI } from './index.ts';
import { nativeRenderers } from './native-ui.ts';

test('the adapter registers only presentation and leaves unrelated renderer resolution intact', () => {
    let resolver: any;
    registerAskQuestionUI({ events: new EventEmitter(), registerToolRenderer: (value: any) => { resolver = value; } } as any);
    const fallback = {};
    assert.equal(resolver('other', () => fallback), fallback);
    assert.equal(resolver('ask_question', () => { throw new Error('should resolve locally'); }), nativeRenderers);
});

for (const capability of ['PI_UI_PROTOCOL', 'PI_GOUI_ASK_QUESTION', 'PI_GOUI_PROTOCOL']) {
test(`${capability}: structured questionnaire RPC sends all questions and returns confirmed answers`, async () => {
    const previous = { PI_UI_PROTOCOL: process.env.PI_UI_PROTOCOL, PI_GOUI_ASK_QUESTION: process.env.PI_GOUI_ASK_QUESTION, PI_GOUI_PROTOCOL: process.env.PI_GOUI_PROTOCOL };
    for (const name of Object.keys(previous)) delete process.env[name];
    process.env[capability] = '1';
    try {
        let tool: any;
        const pi = { events: new EventEmitter(), registerTool: (value: any) => { tool = value; }, registerToolRenderer() {}, on() {} };
        askQuestion(pi as any);
        registerAskQuestionUI(pi as any);
        const questions = [
            { question: '语言', options: ['Go', 'TS'], multiSelect: true },
            { question: '模式', options: ['快', '慢'] },
        ];
        let calls = 0;
        const controller = new AbortController();
        const ctx = { hasUI: true, mode: 'rpc', ui: {
            input: async (title: string, placeholder: string, options?: { signal?: AbortSignal }) => {
                assert.ok(options?.signal instanceof AbortSignal);
                assert.equal(options?.signal.aborted, false);
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
        for (const invalid of ['null', '{"cancelled":false,"answers":[]}']) {
            ctx.ui.input = async () => invalid;
            await assert.rejects(tool.execute('rpc-invalid', { questions }, undefined, undefined, ctx), /Invalid questionnaire/);
        }
    } finally {
        for (const [key, value] of Object.entries(previous)) {
            if (value === undefined) delete process.env[key];
            else process.env[key] = value;
        }
    }
});
}

test('ordinary RPC keeps business choices distinct from the custom-input action', async (t) => {
    const previous = { PI_UI_PROTOCOL: process.env.PI_UI_PROTOCOL, PI_GOUI_ASK_QUESTION: process.env.PI_GOUI_ASK_QUESTION, PI_GOUI_PROTOCOL: process.env.PI_GOUI_PROTOCOL };
    for (const name of Object.keys(previous)) delete process.env[name];
    t.after(() => {
        for (const [key, value] of Object.entries(previous)) {
            if (value === undefined) delete process.env[key];
            else process.env[key] = value;
        }
    });
    const controller = new AbortController();
    for (const options of [['A', 'B'], ['自己输入…', '普通项'], ['自己输入…', '自己输入…（2）', '自己输入…（3）']]) {
        await t.test(JSON.stringify(options), async () => {
            let selectedIndex: number | undefined = 0;
            let inputResult: string | undefined = '  自定义答案  ';
            let inputCalls = 0;
            const ctx = { mode: 'rpc', ui: {
                select: async (title: string, choices: string[], dialog?: { signal?: AbortSignal }) => {
                    assert.equal(title, '选择');
                    assert.equal(dialog?.signal, controller.signal);
                    assert.deepEqual(choices.slice(0, -1), options);
                    assert.equal(new Set(choices).size, choices.length);
                    if (!options.includes('自己输入…')) assert.equal(choices.at(-1), '自己输入…');
                    return selectedIndex === undefined ? undefined : choices[selectedIndex];
                },
                input: async (title: string, placeholder: string, dialog?: { signal?: AbortSignal }) => {
                    assert.equal(title, '问题');
                    assert.equal(placeholder, '请输入你的答案');
                    assert.equal(dialog?.signal, controller.signal);
                    inputCalls++;
                    return inputResult;
                },
            } } as any;
            const ask = () => askQuestions([{ question: '问题', label: '选择', options }], false, ctx, controller.signal);
            for (selectedIndex = 0; selectedIndex < options.length; selectedIndex++) {
                assert.deepEqual(await ask(), { answers: [{ answers: [options[selectedIndex]], wasCustom: false, completed: true }], cancelled: false });
            }
            assert.equal(inputCalls, 0);
            // The final entry remains the action even when its usual label is a business value.
            selectedIndex = options.length;
            assert.deepEqual(await ask(), { answers: [{ answers: ['自定义答案'], wasCustom: true, customAnswers: ['自定义答案'], completed: true }], cancelled: false });
            assert.equal(inputCalls, 1);
            inputResult = undefined;
            assert.deepEqual(await ask(), { answers: [], cancelled: true });
            inputResult = '   ';
            assert.deepEqual(await ask(), { answers: [], cancelled: true });
            assert.equal(inputCalls, 3);
            selectedIndex = undefined;
            assert.deepEqual(await ask(), { answers: [], cancelled: true });
            assert.equal(inputCalls, 3);
        });
    }
});
