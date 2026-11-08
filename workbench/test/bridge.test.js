import test from 'node:test';
import assert from 'node:assert/strict';
import bridge from '../src/bridge.js';
import { Screen, parseMetadata } from '../src/screen.js';

test('opt-in bridge emits explicit name/pending snapshots, including idle renames, without prompt content', () => {
    const env = process.env.PI_WORKBENCH_BRIDGE;
    const write = process.stdout.write;
    const packets = [], hooks = new Map(), events = new Map();
    let name = '';
    const api = { getSessionName: () => name, on: (event, fn) => hooks.set(event, fn),
        events: { on: (event, fn) => events.set(event, fn) } };
    try {
        delete process.env.PI_WORKBENCH_BRIDGE;
        bridge(api);
        assert.equal(hooks.size, 0, 'normal Pi does not install the bridge');
        process.env.PI_WORKBENCH_BRIDGE = '1';
        process.stdout.write = value => { packets.push(String(value)); return true; };
        bridge(api);
        hooks.get('session_start')();
        name = 'Compacting context';
        hooks.get('session_info_changed')();
        events.get('ask-question:pending')({ pending: 2, question: 'never sent' });
        name = '空闲或等待时改名';
        hooks.get('session_info_changed')();
        events.get('ask-question:pending')({ pending: 1 });
        events.get('ask-question:pending')({ pending: 0 });
        const states = packets.map(packet => parseMetadata(packet.slice(6, -1)));
        assert.deepEqual(states, [
            { name: '', pending: 0 }, { name: 'Compacting context', pending: 0 },
            { name: 'Compacting context', pending: 2 }, { name: '空闲或等待时改名', pending: 2 },
            { name: '空闲或等待时改名', pending: 1 }, { name: '空闲或等待时改名', pending: 0 },
        ]);
        const length = packets.length;
        hooks.get('session_info_changed')();
        events.get('ask-question:pending')({ pending: -1 });
        hooks.get('session_shutdown')();
        hooks.get('session_info_changed')();
        assert.equal(packets.length, length);
    } finally {
        process.stdout.write = write;
        if (env === undefined) delete process.env.PI_WORKBENCH_BRIDGE;
        else process.env.PI_WORKBENCH_BRIDGE = env;
    }
});

test('metadata parser handles chunked transport, validates snapshots and strips terminal controls', async () => {
    const snapshots = [];
    const screen = new Screen(30, 5, () => {}, () => {}, () => {}, value => snapshots.push(value));
    try {
        const encoded = Buffer.from(JSON.stringify({ version: 1, name: '中文\x1b\n名称', pending: 2 })).toString('base64');
        const packet = '\x1b]777;pi-workbench;' + encoded + '\x07';
        for (const char of packet) screen.write(char);
        await screen.flush();
        assert.deepEqual(snapshots, [{ name: '中文名称', pending: 2 }]);
        for (const value of [null, {}, {version: 2,name:'x',pending:0}, {version:1,name:2,pending:0},
            {version:1,name:'x',pending:-1}, {version:1,name:'x',pending:0.1}]) {
            assert.equal(parseMetadata('pi-workbench;' + Buffer.from(JSON.stringify(value)).toString('base64')), undefined);
        }
        assert.equal(parseMetadata('something-else;bad'), undefined);
        assert.equal(parseMetadata('pi-workbench;' + 'a'.repeat(8192)), undefined);
    } finally { screen.dispose(); }
});
