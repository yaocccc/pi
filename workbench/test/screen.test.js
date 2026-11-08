import test from 'node:test';
import assert from 'node:assert/strict';
import stringWidth from 'string-width';
import { Screen, parseStatus, safeText } from '../src/screen.js';
import { fit, layout, Painter, expandPill } from '../src/view.js';

const plain = value => value.replace(/\x1b\[[0-9;]*m/g, '');

test('floating pill overlays only one cell and restores underlying row when removed', async t => {
    const screen = new Screen(20, 4, () => {}, () => {}, () => {});
    t.after(() => screen.dispose());
    const painter = new Painter();
    const lines = ['abcdefghij中文klmnop', 'second row'];
    const cursor = {x:0,y:0,visible:true};
    assert.equal(expandPill(), '\x1b[0m\x1b[38;2;255;255;255m»\x1b[0m');
    screen.write(painter.paint(lines, cursor, expandPill()));
    await screen.flush();
    assert.ok(plain(screen.lines()[0]).startsWith('»bcdefghij中文'));
    assert.doesNotMatch(painter.paint(lines, cursor, expandPill()), /\x1b\[\?25h/);
    screen.write(painter.paint(lines, cursor));
    await screen.flush();
    assert.ok(plain(screen.lines()[0]).startsWith('abcdefghij中文'));
    assert.ok(plain(screen.lines()[1]).startsWith('second row'));
});

test('OSC status validates states, decodes msg, strips controls, and ignores unrelated records', () => {
    assert.equal(parseStatus('?'), undefined);
    assert.equal(parseStatus('state=unexpected'), undefined);
    assert.deepEqual(parseStatus('state=blocked:kind=question:msg=' + Buffer.from('等待\x1b[2J').toString('base64')),
        { state: 'blocked', message: '等待[2J' });
    assert.equal(safeText('x\u202ey\n\x1b'), 'xy');
});

test('real headless parser handles split OSC, modes, cursor and local terminal replies', async t => {
    const replies = [], statuses = [];
    const screen = new Screen(20, 4, value => replies.push(value), value => statuses.push(value), () => {});
    t.after(() => screen.dispose());
    screen.write('\x1b]7501;sta');
    screen.write('te=working\x1b');
    screen.write('\\\x1b]7501;?\x07\x1b[6n');
    screen.write('\x1b[?25l\x1b[2;3H\x1b[38;2;17;34;51m中文 e\u0301');
    await screen.flush();
    assert.deepEqual(statuses, [{ state: 'working', message: '' }]);
    assert.ok(replies.includes('\x1b]7501;?\x1b\\'));
    assert.ok(replies.some(value => /\x1b\[\d+;\d+R/.test(value)));
    assert.equal(screen.cursorVisible, false);
    assert.match(screen.lines()[1], /38;2;17;34;51/);
    assert.match(plain(screen.lines()[1]), /^  中文 é/);
    for (const line of screen.lines()) assert.equal(stringWidth(line), 20);
    screen.write('\x1b[?1049hALT\x1b[?25h');
    await screen.flush();
    assert.equal(screen.cursorVisible, true);
    assert.equal(screen.terminal.buffer.active.type, 'alternate');
    screen.write('\x1b[?1049l');
    await screen.flush();
    assert.match(plain(screen.lines()[1]), /中文/);
    screen.resize(10, 3);
    assert.equal(screen.lines().length, 3);
    for (const line of screen.lines()) assert.equal(stringWidth(line), 10);
});

test('child control sequences are not forwarded to host (clipboard, title, clear, OSC)', async t => {
    const screen = new Screen(20, 3, () => {}, () => {}, () => {});
    t.after(() => screen.dispose());
    screen.write('\x1b]52;c;ZXhmaWw=\x07\x1b]0;TITLE\x07\x1b[2JVISIBLE');
    await screen.flush();
    const content = screen.lines().join('\n');
    assert.match(content, /VISIBLE/);
    assert.doesNotMatch(content, /TITLE|52;|ZXhmaWw|\x1b\[2J/);
});

test('layout hides sidebar on narrow windows; safe labels fit wide Unicode and escape text', () => {
    assert.deepEqual(layout(100, 30), { sidebar: 16, left: 17, cols: 83, rows: 30 });
    assert.equal(layout(67, 30).left, 0);
    assert.equal(layout(100, 8).left, 0);
    assert.equal(layout(100, 30, true).left, 0);
    assert.equal(layout(1, 1).cols, 2);
    for (let width = 0; width < 20; width++) assert.equal(stringWidth(fit('中文é🧑‍💻\x1b[31m', width)), width);
});

test('painter preserves last-column borders, footer and wide glyphs with host autowrap disabled', async t => {
    const host = new Screen(10, 4, () => {}, () => {}, () => {});
    t.after(() => host.dispose());
    const painter = new Painter(), cursor = { x: 0, y: 3, visible: false };
    host.write('\x1b[?7l');
    host.write(painter.paint(['╭────────╮', '│ 中文   │', '│       中', '123456789Z'], cursor));
    await host.flush();
    assert.deepEqual(host.lines().map(plain), ['╭────────╮', '│ 中文   │', '│       中', '123456789Z']);
    host.write(painter.paint(['short', '│ 中文   │', '│       中', '123456789Z'], cursor));
    await host.flush();
    assert.equal(plain(host.lines()[0]), 'short     ');
});

test('painter only updates changed rows and restores the translated cursor', () => {
    const painter = new Painter();
    const cursor = { x: 30, y: 2, visible: true };
    const first = painter.paint(['left | right', 'footer'], cursor);
    assert.match(first, /left \| right/);
    assert.match(first, /\x1b\[3;31H\x1b\[\?25h$/);
    assert.doesNotMatch(painter.paint(['left | right', 'footer'], cursor), /footer/);
    painter.invalidate();
    assert.match(painter.paint(['left | right', 'footer'], cursor), /footer/);
});
