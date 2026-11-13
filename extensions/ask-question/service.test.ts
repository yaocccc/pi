import assert from 'node:assert/strict';
import { EventEmitter } from 'node:events';
import { readFileSync, readdirSync } from 'node:fs';
import test from 'node:test';
import askQuestion from './index.ts';
import { ASK_QUESTION_INTERACTION, ASK_QUESTION_SERVICE, type AskQuestionService, type QuestionInteractionPort } from './api.ts';
import { getCapability, registerCapability } from '../shared/capabilities.ts';

const question = { question: 'Choose?', options: ['A', 'B'] };
const submission = { cancelled: false, answers: [{ answers: ['A'], completed: true, wasCustom: false }] };
const flush = async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); };
function harness(port?: QuestionInteractionPort, adapterFirst = false) {
    let tool: any;
    const handlers = new Map<string, Array<() => unknown>>();
    const pi = {
        events: new EventEmitter(), registerTool: (value: any) => { tool = value; },
        on(name: string, handler: () => unknown) { handlers.set(name, [...handlers.get(name) ?? [], handler]); },
    };
    if (adapterFirst && port) registerCapability(pi as any, ASK_QUESTION_INTERACTION, port);
    askQuestion(pi as any);
    if (!adapterFirst && port) registerCapability(pi as any, ASK_QUESTION_INTERACTION, port);
    const context = { hasUI: false, mode: 'rpc', ui: new Proxy({}, { get() { throw new Error('capability accessed host UI'); } }) } as any;
    const service = getCapability<AskQuestionService>(pi as any, ASK_QUESTION_SERVICE)!;
    return {
        pi, tool, context, service,
        ask: (signal?: AbortSignal) => service.ask({ questions: [question], review: false, context, signal }),
        call: (params: unknown = question) => tool.execute('question', params, undefined, undefined, context),
        event: async (name: string) => { for (const handler of handlers.get(name) ?? []) await handler(); },
    };
}

test('without a UI adapter, tools expose unanswered data rather than claiming cancellation', async () => {
    const h = harness();
    h.context.hasUI = true;
    const single = await h.call();
    assert.equal(single.details.answer, null);
    assert.equal(single.details.unavailable, true);
    assert.equal(single.details.cancelled, undefined);
    assert.doesNotMatch(single.content[0].text, /取消/);
    const grouped = await h.call({ questions: [question] });
    assert.equal(grouped.details.questions[0].answer, null);
    assert.equal(grouped.details.unavailable, true);
    assert.equal(grouped.details.cancelled, undefined);
    assert.equal(h.service.pending(), 0);
    assert.equal(h.tool.renderCall, undefined);
    assert.equal(h.tool.renderResult, undefined);
});

for (const adapterFirst of [false, true]) {
    test(`an independent frontend port works regardless of load order (${adapterFirst})`, async () => {
        const h = harness({ supports: () => true, ask: async () => submission }, adapterFirst);
        assert.equal((await h.call()).details.answer, 'A');
        assert.equal((await h.ask())?.answers[0]?.answers[0], 'A');
        assert.equal(h.service.pending(), 0);
    });
}

test('every port response is validated; ports cannot mutate authoritative questions', async () => {
    let value: unknown = submission;
    let mutate = false;
    const h = harness({ supports: () => true, ask: async ({ questions }) => {
        if (mutate) questions[0]!.options.push('fabricated');
        return value;
    } });
    for (value of [null, undefined, { cancelled: false, answers: [] }, { cancelled: false, answers: [{ answers: ['fabricated'], completed: true }] }]) {
        await assert.rejects(h.ask(), /Invalid questionnaire/);
        assert.equal(h.service.pending(), 0);
    }
    mutate = true;
    await assert.rejects(h.ask(), /Invalid questionnaire/);
    assert.deepEqual(question.options, ['A', 'B']);
    value = { cancelled: true, answers: [{ answers: ['draft'] }] };
    assert.deepEqual(await h.ask(), { cancelled: true, answers: [] });
    value = submission;
    assert.equal((await h.ask())?.cancelled, false);
});

test('capabilities and pending request counts are isolated per event bus', async () => {
    let finish!: (value: unknown) => void;
    const first = harness({ supports: () => true, ask: async () => new Promise(resolve => { finish = resolve; }) });
    const second = harness();
    const one = first.ask();
    await flush();
    assert.equal(first.service.pending(), 1);
    assert.equal(second.service.pending(), 0);
    assert.equal(await second.ask(), undefined);
    finish(submission);
    await one;
    assert.equal(first.service.pending(), 0);
});

test('core serializes ports and queued abort does not let a successor overtake the owner', async () => {
    const finishes: Array<(value: unknown) => void> = [];
    const counts: number[] = [];
    const h = harness({ supports: () => true, ask: async () => new Promise(resolve => finishes.push(resolve)) });
    h.pi.events.on('ask-question:pending', ({ pending }) => counts.push(pending));
    const first = h.ask();
    const controller = new AbortController();
    const queued = h.ask(controller.signal);
    const rejected = assert.rejects(queued, /queued/);
    const third = h.ask();
    await flush();
    assert.equal(finishes.length, 1);
    assert.equal(h.service.pending(), 3);
    controller.abort(new Error('queued'));
    await rejected;
    assert.equal(finishes.length, 1);
    finishes[0]!(submission);
    await first; await flush();
    assert.equal(finishes.length, 2);
    finishes[1]!(submission);
    await third;
    assert.deepEqual(counts, [1, 2, 3, 2, 1, 0]);
});

for (const event of ['session_shutdown', 'session_tree', 'session_start']) {
    test(`${event} clears active and queued ports even when the active port never resolves`, async () => {
        let opened = 0;
        let activeSignal: AbortSignal | undefined;
        const h = harness({ supports: () => true, ask: async ({ signal }) => {
            opened++; activeSignal = signal;
            return new Promise(() => {});
        } });
        const calls = [h.ask(), h.ask(), h.ask()];
        const rejected = calls.map(call => assert.rejects(call, /关闭/));
        await flush();
        assert.equal(opened, 1);
        await h.event(event);
        await Promise.all(rejected);
        assert.equal(h.service.pending(), 0);
        assert.equal(opened, 1);
        assert.equal(activeSignal?.aborted, true);
    });
}

test('pre-aborted requests never enter a port or report pending', async () => {
    let opens = 0;
    const h = harness({ supports: () => true, ask: async () => { opens++; return submission; } });
    const controller = new AbortController(); controller.abort(new Error('pre-abort'));
    await assert.rejects(h.ask(controller.signal), /pre-abort/);
    assert.equal(opens, 0);
    assert.equal(h.service.pending(), 0);
});

test('the capability has no renderer, terminal, native adapter or host UI dependency', () => {
    for (const name of readdirSync(new URL('.', import.meta.url)).filter(name => name.endsWith('.ts') && !name.endsWith('.test.ts'))) {
        const source = readFileSync(new URL(name, import.meta.url), 'utf8');
        assert.doesNotMatch(source, /pi-tui|pi-ui\/|ctx\.ui|context\.ui|renderCall|renderResult|isGoUI|PI_GOUI/, name);
    }
});
