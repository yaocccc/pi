#!/usr/bin/env node
import { spawn } from 'node:child_process';

process.stdin.setRawMode(true);
process.stdin.setEncoding('utf8');
process.stdin.resume();
let tick = 0, input = '';
const status = value => process.stdout.write(`\x1b]7501;state=${value}\x07`);
process.stdout.write(`\x1b[?1049h\x1b[2J\x1b[HPID ${process.pid} 中文\x1b[2;1HINPUT `);
status('working');
if (process.argv.includes('--mouse-test')) {
    process.stdout.write('\x1b[?1003h\x1b[?1006h');
    setInterval(() => process.stdout.write('\x1b]52;c;RVZJTA==\x07'), 50);
}
setInterval(() => process.stdout.write(`\x1b[3;1HTICK ${++tick}\x1b[K`), 40);
process.on('SIGWINCH', () => process.stdout.write(`\x1b[4;1HSIZE ${process.stdout.columns}x${process.stdout.rows}\x1b[K`));
process.stdin.on('data', data => {
    input += data;
    if (process.argv.includes('--mouse-test')) {
        for (const event of data.matchAll(/\x1b\[<\d+;\d+;\d+[Mm]/g)) {
            process.stdout.write(`\x1b[6;1HMOUSE ${Buffer.from(event[0]).toString('hex')}\x1b[K`);
        }
    }
    process.stdout.write(`\x1b[2;1HINPUT ${Buffer.from(input).toString('hex')}\x1b[K`);
    if (data === 'quit') process.exit(0);
    if (data.includes('b')) status('blocked');
    if (data.includes('i')) status('idle');
    if (data.includes('e')) status('error');
    if (data.includes('d')) {
        const child = spawn(process.execPath, ['-e', 'setInterval(()=>{},1000)'], { detached: true, stdio: 'ignore' });
        process.stdout.write(`\x1b[5;1HDESCENDANT ${child.pid}\x1b[K`);
    }
});
