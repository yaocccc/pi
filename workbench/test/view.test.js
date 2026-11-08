import test from 'node:test';
import assert from 'node:assert/strict';
import stringWidth from 'string-width';
import { layout, sidebarAction, frame, SidebarViewport, ellipsize, NEW_SESSION_LABEL, SIDEBAR_WIDTH, SIDEBAR_COLORS, STATE_COLORS, toggleHit } from '../src/view.js';

const left = row => row.replace(/\x1b\[[0-9;]*m/g, '').split('│')[0];
const click = (x,y,button=0,release=false) => ({x,y,button,release});

test('unnamed sessions retain a blank name row beneath numbered status', () => {
    const size = layout(80, 10);
    const rows = frame({ sessions: [{ name: '', state: 'idle' }], selected: 0 }, size, 80, '', 0);
    assert.equal(left(rows[2]).trim(), '1 idle');
    assert.equal(left(rows[3]).trim(), '');
});

test('large lists scroll by complete cards and reveal selection on change or resize', () => {
    const viewport = new SidebarViewport();
    const size = layout(80, 10);
    const sessions = Array.from({length:100}, (_, i) => ({name:`Pi ${i+1}`, state:'idle'}));
    viewport.sync(100, 99, size.rows);
    assert.equal(viewport.offset, 97);
    const rows = frame({sessions, selected:99}, size, 80, '', viewport.offset);
    assert.equal(left(rows[6]).trim(), '100 idle');
    assert.equal(left(rows[7]).trim(), 'Pi 100');
    assert.equal(left(rows[9]).trim(), 'new          «');
    assert.match(rows[9], /^\x1b\[0m\x1b\[38;2;200;200;204m\x1b\[22m new +« /);
    assert.doesNotMatch(rows[9].split('│')[0], /\x1b\[1m|\x1b\[48;/);
    assert.match(rows[0], /\x1b\[1m.*Sessions/);
    assert.doesNotMatch(rows.join(''), /Pi Workbench|新建会话/);
    assert.match(frame({sessions, selected:99}, size, 80, '启动失败', viewport.offset)[1], /启动失败/);
    assert.equal(sidebarAction(size, 100, click(2,9)), 'n');
    for (const y of [6,7]) assert.equal(sidebarAction(size, 100, click(3,y), viewport.offset), '100');
    assert.equal(sidebarAction(size, 100, click(8,9), viewport.offset), null);
    viewport.scroll(-3); viewport.sync(100, 99, size.rows);
    assert.equal(viewport.offset, 94);
    viewport.sync(100, 99, size.rows, true);
    assert.equal(viewport.offset, 97);
    viewport.sync(100, 0, size.rows);
    assert.equal(viewport.offset, 0);
    viewport.scroll(1000);
    assert.equal(viewport.offset, 97);
    viewport.sync(100, 99, 20);
    assert.equal(viewport.offset, 92);
    viewport.sync(2, 1, 20);
    assert.equal(viewport.offset, 0);
    viewport.scroll(-1000); viewport.sync(0, 0, 2);
    assert.equal(viewport.offset, 0);
});

test('toggle targets are separate from cards and unavailable on narrow screens', () => {
    const open = layout(80, 11), closed = layout(80, 11, true);
    assert.equal(sidebarAction(open, 100, click(2, 10)), 'n');
    assert.equal(sidebarAction(open, 100, click(2, 9)), '4');
    for (const x of [12, 13, 14, 15]) assert.equal(toggleHit(open, 80, click(x, 10)), true);
    for (const x of [2, 11, 16]) assert.equal(toggleHit(open, 80, click(x, 10)), false);
    for (const x of [0, 1, 2, 3]) {
        assert.equal(toggleHit(closed, 80, click(x, 0), true), true);
        assert.equal(toggleHit(closed, 80, click(x, 1), true), false);
    }
    assert.equal(toggleHit(closed, 80, click(4, 1), true), false);
    assert.equal(toggleHit(closed, 80, click(2, 2), true), false);
    assert.equal(toggleHit(layout(60, 11, true), 60, click(2, 0), true), false);
    assert.equal(toggleHit(layout(80, 8, true), 80, click(2, 0), true), false);
    const viewport = new SidebarViewport();
    viewport.sync(20, 19, 11);
    assert.equal(viewport.offset, 16);
});

test('button blank rows are inert and both card rows target the same session', () => {
    const size = layout(110, 30);
    assert.equal(sidebarAction(size, 1, click(2,29)), 'n');
    for (const y of [0,1]) assert.equal(sidebarAction(size, 2, click(3,y)), null);
    for (const y of [2,3]) assert.equal(sidebarAction(size, 2, click(3,y)), '1');
    for (const y of [4,5]) assert.equal(sidebarAction(size, 2, click(3,y)), '2');
    for (const y of [6,7]) assert.equal(sidebarAction(layout(80,10), 5, click(3,y), 2), '5');
    for (const event of [click(SIDEBAR_WIDTH,3),click(50,3),click(3,7),click(3,8),click(3,3,64),click(3,3,0,true),click(3,30)]) {
        assert.equal(sidebarAction(size, 2, event), null);
    }
    assert.equal(sidebarAction(layout(60,30),2,click(3,3)), null);
    assert.equal(sidebarAction(layout(110,30,true),2,click(3,3)), null);
});

test('names ellipsize by grapheme width and state precedes the name with padding', () => {
    assert.equal(ellipsize('短名', 4), '短名');
    assert.equal(ellipsize('中文长会话名称', 5), '中文…');
    assert.equal(ellipsize('🧑‍💻开发任务', 3), '🧑‍💻…');
    assert.equal(ellipsize('éabc', 2), 'é…');
    assert.equal(ellipsize('long', 1), '…');
    assert.equal(ellipsize('long', 0), '');
    const rows = frame({sessions:[{name:'中文长会话名称超过宽度',state:'blocked'}],selected:0},layout(70,10),70,'');
    assert.equal(left(rows[2]).trim(), '1 等待回答');
    assert.equal(left(rows[3]).trim(), '中文长会话名…');
    for (const row of rows) {
        assert.equal(stringWidth(left(row)),16);
        assert.ok(left(row).startsWith(' ') && left(row).endsWith(' '));
    }
});

test('sidebar fits padded button and preserves minimum pane width', () => {
    const size = layout(70, 24);
    assert.equal(SIDEBAR_WIDTH, stringWidth(NEW_SESSION_LABEL) + 2);
    assert.deepEqual(size, {sidebar:16,left:17,cols:53,rows:24});
    assert.equal(layout(69,24).sidebar,0);
    const sessions = [{name:'very long session name',state:'blocked',message:'secret question summary'}];
    const rows = frame({sessions,selected:0},size,70,'');
    assert.equal(rows.length,24);
    assert.equal(left(rows[0]).trim(),'Sessions');
    assert.ok(rows[0].includes('\x1b[1m'));
    assert.equal(left(rows[1]).trim(),'');
    assert.equal(left(rows[3]).trim(),'very long ses…');
    assert.doesNotMatch(rows.join(''), /Ctrl\+G|复制选区|关闭当前会话|确认关闭|secret question summary/);
});

test('state palette and selected background are confined to the two card rows', () => {
    const sessions = Object.keys(STATE_COLORS).map(state => ({name:'a name',state}));
    const rows = frame({sessions,selected:1},layout(80,30),80,'');
    sessions.forEach((session, i) => {
        const first = 2 + i * 2;
        assert.ok(rows[first].includes(`\x1b[38;2;${STATE_COLORS[session.state]}m`));
        for (const y of [first,first+1]) {
            assert.equal(rows[y].includes(`\x1b[48;2;${SIDEBAR_COLORS.selected}m`), i === 1);
            assert.ok(rows[y].includes(`\x1b[0m\x1b[38;2;${SIDEBAR_COLORS.separator}m│\x1b[0m`));
            assert.ok(!rows[y].split('│')[1].includes('\x1b[48;'));
        }
    });
    const many = [];
    many.length = 1_000_000;
    many[999_999] = {name:'name',state:'blocked'};
    const big = frame({sessions:many,selected:999_999},layout(80,10),80,'',999_999);
    assert.equal(stringWidth(left(big[2])),16);
    assert.equal(left(big[2]).trim(), '1000… 等待回答');
});
