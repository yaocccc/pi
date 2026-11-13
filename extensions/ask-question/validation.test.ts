import assert from 'node:assert/strict';
import test from 'node:test';
import { validateQuestionResponse } from './validation.ts';

const single = [{ question: 'Pick', options: ['A', 'B'] }];
const response = (answer: Record<string, unknown>) => ({ answers: [{ completed: true, ...answer }], cancelled: false });

test('Question response validation rejects malformed, incomplete and fabricated answers', () => {
    for (const value of [null, [], {}, { cancelled: 'false' }, { cancelled: false, answers: [] },
        response({ answers: [] }), response({ answers: ['A'], completed: false }),
        response({ answers: [42] }), response({ answers: ['A', 'B'] }),
        response({ answers: ['unknown'] }), response({ answers: [' '] }),
        response({ answers: ['A'], customAnswers: ['unselected'] }),
        response({ answers: ['A'], customAnswers: 'A' }), response({ answers: ['A'], wasCustom: 'yes' }),
        response({ answers: ['A'], wasCustom: true }),
    ]) assert.throws(() => validateQuestionResponse(value, single), /Invalid questionnaire/);
    assert.throws(() => validateQuestionResponse(response({ answers: ['A', 'A'] }), [{ ...single[0]!, multiSelect: true }]), /Invalid questionnaire/);
});

test('Question response validation accepts mixed/custom answers and discards cancelled drafts', () => {
    assert.deepEqual(validateQuestionResponse({ cancelled: true, answers: [{ answers: ['unsubmitted'] }] }, single), { cancelled: true, answers: [] });
    const mixed = validateQuestionResponse(response({ answers: ['A', 'custom'], customAnswers: ['custom'], wasCustom: false }), [{ ...single[0]!, multiSelect: true }]);
    assert.deepEqual(mixed.answers[0], { answers: ['A', 'custom'], customAnswers: ['custom'], wasCustom: false, completed: true });
    assert.equal(validateQuestionResponse(response({ answers: ['custom'], customAnswers: ['custom'], wasCustom: true }), single).answers[0]!.wasCustom, true);
});
