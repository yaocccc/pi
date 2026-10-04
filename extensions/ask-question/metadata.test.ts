import assert from 'node:assert/strict';
import test from 'node:test';
import askQuestion from './index.ts';

function metadata() {
    let tool: any;
    const events: string[] = [];
    askQuestion({
        registerTool: (value: any) => { tool = value; },
        on: (event: string) => { events.push(event); },
    } as any);
    return { tool, events };
}

function descriptions(schema: any): string[] {
    if (!schema || typeof schema !== 'object') return [];
    return Object.entries(schema).flatMap(([key, value]) =>
        key === 'description' && typeof value === 'string' ? [value] : descriptions(value));
}

test('compact guidance retains essential rules without duplicate system injection', () => {
    const { tool, events } = metadata();
    assert.ok(!events.includes('before_agent_start'));
    const guidelines = tool.promptGuidelines.join('\n');
    for (const rule of ['ask_question', '必须', '能继续则不问', '2-6', 'multiSelect: true', 'questions']) {
        assert.ok(guidelines.includes(rule), `missing rule: ${rule}`);
    }
    const text = [tool.description, tool.promptSnippet, guidelines, ...descriptions(tool.parameters)].join('\n');
    assert.ok(text.length <= 300, `model-facing text exceeds budget: ${text.length}`);
});

test('single and grouped question schemas retain types and bounds', () => {
    const { tool } = metadata();
    const root = tool.parameters;
    assert.equal(root.type, 'object');
    assert.equal(root.anyOf, undefined);
    assert.deepEqual(root.required ?? [], []);
    const item = root.properties.questions.items;
    assert.deepEqual(item.required, ['question', 'options']);
    for (const schema of [root, item]) {
        assert.equal(schema.properties.question.type, 'string');
        assert.equal(schema.properties.multiSelect.type, 'boolean');
        const options = schema.properties.options;
        assert.equal(options.type, 'array');
        assert.equal(options.items.type, 'string');
        assert.equal(options.minItems, 1);
        assert.equal(options.maxItems, 8);
    }
    assert.equal(item.properties.label.type, 'string');
    assert.equal(root.properties.questions.type, 'array');
    assert.equal(root.properties.questions.minItems, 1);
    assert.equal(root.properties.questions.maxItems, 8);
});
