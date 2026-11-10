import assert from 'node:assert/strict';
import test from 'node:test';
import { frameBorder, type FrameEdge, type FrameKind } from './frame.ts';

test('shared frame colors preserve the original default and input palette', () => {
    assert.equal(frameBorder('╭', 'topLeft'), '\x1b[38;2;120;181;239m╭\x1b[39m');
    assert.equal(frameBorder('│', 'left', 'input'), '\x1b[38;2;173;151;223m│\x1b[39m');
    assert.equal(frameBorder('─', 'top', 'tool'), '\x1b[38;2;113;137;159m─\x1b[39m');
    const kinds: FrameKind[] = ['user', 'assistant', 'tool', 'input'];
    const edges: FrameEdge[] = ['topLeft', 'top', 'topRight', 'right', 'bottomRight', 'bottom', 'bottomLeft', 'left'];
    for (const kind of kinds) for (const edge of edges) {
        const result = frameBorder('边', edge, kind);
        assert.match(result, /^\x1b\[38;2;\d+;\d+;\d+m边\x1b\[39m$/);
        assert.doesNotMatch(result, /\x1b\[(?:0|48|49)m/);
    }
});
