import test from 'node:test';
import assert from 'node:assert/strict';
import { Screen } from '../src/screen.js';
import { PaneMouse, mouseReport } from '../src/mouse.js';
import { layout } from '../src/view.js';

async function pane(t, mode = 1003) {
    const writes = [];
    const screen = new Screen(73, 18, () => {}, () => {}, () => {});
    t.after(() => screen.dispose());
    screen.write(`\x1b[?${mode}h\x1b[?1006h中文abc`);
    await screen.flush();
    return { screen, writes, write: data => { if (data) writes.push(data); } };
}
const event = (button, x, y, release = false) => ({ button, x, y, release });

test('negotiated mouse modes filter hover, dragging, release, and disabled reporting', async t => {
    const session = await pane(t);
    assert.equal(mouseReport(session, event(35, 2, 3)), '\x1b[<35;3;4M');
    session.screen.write('\x1b[?1002h'); await session.screen.flush();
    assert.equal(mouseReport(session, event(35, 2, 3)), '');
    assert.equal(mouseReport(session, event(32, 2, 3)), '\x1b[<32;3;4M');
    session.screen.write('\x1b[?1000h'); await session.screen.flush();
    assert.equal(mouseReport(session, event(32, 2, 3)), '');
    assert.equal(mouseReport(session, event(64, 2, 3)), '\x1b[<64;3;4M');
    assert.equal(mouseReport(session, event(0, 2, 3, true)), '\x1b[<0;3;4m');
    session.screen.write('\x1b[?1000l'); await session.screen.flush();
    assert.equal(mouseReport(session, event(0, 2, 3)), '');
    session.screen.write('\x1b[?9h'); await session.screen.flush();
    assert.equal(mouseReport(session, event(0, 2, 3, true)), '');
    assert.equal(mouseReport(session, event(64, 2, 3)), '');
    session.screen.write('\x1b[?1006l'); await session.screen.flush();
    assert.equal(mouseReport(session, event(0, 2, 3)), '\x1b[M #$');
});

test('pane coordinates, wheel, captured outside release and sidebar isolation', async t => {
    const session = await pane(t), mouse = new PaneMouse(), size = layout(100, 20);
    assert.equal(mouse.handle(event(0, 2, 2), session, size), false);
    assert.equal(mouse.handle(event(0, 17, 1), session, size), true);
    mouse.handle(event(32, 19, 2), session, size);
    mouse.handle(event(0, 5, 30, true), session, size);
    assert.deepEqual(session.writes, ['\x1b[<0;1;2M', '\x1b[<32;3;3M', '\x1b[<0;1;20m']);
    assert.equal(mouse.capture, undefined);
    mouse.handle(event(64, 99, 17), session, size);
    assert.equal(session.writes.at(-1), '\x1b[<64;83;18M');
    mouse.handle(event(0, 2, 1), session, layout(67, 20));
    assert.equal(session.writes.at(-1), '\x1b[<0;3;2M');
    mouse.reset();
    assert.equal(session.writes.at(-1), '\x1b[<0;3;2m');
});

test('switching during a drag releases the old pane without sending its selection to the new pane', async t => {
    const first = await pane(t), second = await pane(t), mouse = new PaneMouse(), size = layout(100, 20);
    mouse.handle(event(0, 30, 2), first, size);
    mouse.handle(event(35, 31, 3), second, size);
    assert.equal(first.writes.at(-1), '\x1b[<0;14;3m');
    assert.equal(mouse.capture, undefined);
});
