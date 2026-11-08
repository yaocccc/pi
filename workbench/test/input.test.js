import test from 'node:test';
import assert from 'node:assert/strict';
import { InputRouter } from '../src/input.js';

test('SGR mouse reports split at every boundary are host-only; paste stays literal', async t => {
    const sent = [], events = [];
    const router = new InputRouter(data => sent.push(data), () => {}, event => events.push(event));
    t.after(() => router.dispose());
    for (const char of '\x1b[<0;4;3M\x1b[<0;4;3m') router.feed(char);
    assert.deepEqual(events, [{button:0,x:3,y:2,release:false},{button:0,x:3,y:2,release:true}]);
    router.feed('\x1b[<0;');
    await new Promise(resolve => setTimeout(resolve, 90));
    assert.deepEqual(sent, []);
    router.feed('50;4Mhello');
    assert.equal(sent.join(''), 'hello');
    const paste = '\x1b[200~\x1b[<0;4;3M\x1b[201~';
    router.feed(paste);
    assert.equal(sent.join(''), 'hello' + paste);
    assert.equal(events.length, 3);
});

function setup(t) {
    const sent = [], commands = [];
    const router = new InputRouter(data => sent.push(data), () => {});
    t.after(() => router.dispose());
    return { router, sent, commands };
}

test('all keyboard bytes including former host shortcuts reach child unchanged', t => {
    const { router, sent, commands } = setup(t);
    const value = 'hello\x1b[A\x07nworld\x07q\x07c\x07\x07';
    for (const char of value) router.feed(char);
    assert.deepEqual(commands, []);
    assert.equal(sent.join(''), value);
});

test('paste markers split at every byte boundary preserve embedded host shortcuts as text', t => {
    const { router, sent, commands } = setup(t);
    for (const char of '\x1b[200~pasted\x07q\x07n中文\x1b[201~') router.feed(char);
    assert.deepEqual(commands, []);
    assert.equal(sent.join(''), '\x1b[200~pasted\x07q\x07n中文\x1b[201~');
    assert.equal(router.pasting, false);
    router.feed('\x07n');
    assert.deepEqual(commands, []);
    assert.ok(sent.join('').endsWith('\x07n'));
});

test('a standalone escape is eventually forwarded, not lost to paste detection', async t => {
    const { router, sent } = setup(t);
    router.feed('\x1b');
    await new Promise(resolve => setTimeout(resolve, 90));
    assert.equal(sent.join(''), '\x1b');
});
