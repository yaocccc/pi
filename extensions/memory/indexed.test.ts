import assert from 'node:assert/strict';
import test from 'node:test';
import { indexEntryFromCommit } from './indexed.ts';
import type { CommitMemory } from './types.ts';

for (const updated of ['2026-01-02', undefined, 'absent'] as const) {
    test(`commit projection preserves every index field and its order (updated: ${updated})`, () => {
        const memory: CommitMemory = {
            title: 'Detail-only title',
            heading: ' 0007 Heading ',
            file: 'memories/example.md',
            type: 'decision',
            project: ' Mixed-Case ',
            tags: [' second ', 'first', 'first'],
            keywords: ['Alpha', ' beta '],
            summary: ` ${'long summary '.repeat(15)} `,
            whenToUse: '',
            constraints: ['one', 'two', 'three', 'four'],
            content: 'Detail-only content',
            evidence: 'Detail-only evidence',
            ...(updated === 'absent' ? {} : { updated }),
        };
        const before = structuredClone(memory);
        const entry = indexEntryFromCommit(memory);

        assert.deepEqual(entry, {
            heading: ' 0007 Heading ',
            file: 'memories/example.md',
            type: 'decision',
            project: ' Mixed-Case ',
            tags: [' second ', 'first', 'first'],
            keywords: ['Alpha', ' beta '],
            summary: ` ${'long summary '.repeat(15)} `,
            whenToUse: '',
            constraints: ['one', 'two', 'three', 'four'],
            updated: updated === 'absent' ? undefined : updated,
        });
        assert.deepEqual(Object.keys(entry), [
            'heading', 'file', 'type', 'project', 'tags', 'keywords',
            'summary', 'whenToUse', 'constraints', 'updated',
        ]);
        assert.equal(Object.hasOwn(entry, 'updated'), true);
        assert.notEqual(entry, memory);
        for (const field of ['tags', 'keywords', 'constraints'] as const) {
            assert.equal(entry[field], memory[field], `${field} retains its original array reference`);
        }
        assert.deepEqual(memory, before, 'projection does not mutate or normalize the commit');
    });
}
