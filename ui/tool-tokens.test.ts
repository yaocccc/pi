import assert from 'node:assert/strict';
import test from 'node:test';
import { colorToolTokens } from './tool-tokens.ts';

test('token colors use inclusive thresholds and preserve background', () => {
    const theme = { fg: (color: string, text: string) => `<${color}>${text}</${color}>` };
    for (const count of [0, 4999]) assert.equal(colorToolTokens(count, 'value', theme), '<dim>value</dim>');
    for (const count of [5000, 9999]) {
        const colored = colorToolTokens(count, 'value', theme);
        assert.equal(colored, '\x1b[38;2;255;165;0mvalue\x1b[39m');
        assert.doesNotMatch(colored, /\x1b\[(?:0|49)m/);
    }
    for (const count of [10000, 10001, 50001]) assert.equal(colorToolTokens(count, 'value', theme), '<error>value</error>');
});
