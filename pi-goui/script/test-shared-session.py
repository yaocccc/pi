#!/usr/bin/env python3
"""Linux real-CLI/PT Y shared-session acceptance; Python standard library only.

Run: python3 script/test-shared-session.py [--binary /path/to/pi-goui]
All HOME/agent/cwd/session/RPC data and the optional build live in a temporary
root. --keep preserves that root for failure investigation (never credentials).
"""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import select
import shlex
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time

sys.dont_write_bytecode = True
ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('goui_screen_fixture', ROOT / 'script/test-ui.py')
screen_module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(screen_module)
Screen = screen_module.Screen


def identity(pid):
    try:
        fields = pathlib.Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()
        return fields[19] if fields[0] != 'Z' else None
    except (FileNotFoundError, ProcessLookupError):
        return None


class Terminal:
    def __init__(self, suite, name, shared=False):
        self.suite, self.name = suite, name
        self.master, self.slave = os.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack('HHHH', 42, 150, 0, 0))
        self.screen, self.raw = Screen(150, 42), bytearray()
        args = [str(suite.binary), '--cwd', str(suite.root / 'cwd'), '--session', str(suite.session), '--', '--offline']
        if not shared:
            args.insert(1, '--share=false')
        self.p = subprocess.Popen(args, env=suite.env, stdin=self.slave, stdout=self.slave,
                                  stderr=self.slave, start_new_session=True)
        suite.terminals.append(self)
        suite.owned[self.p.pid] = identity(self.p.pid)

    def send(self, text):
        os.write(self.master, text.encode() if isinstance(text, str) else text)
        self.suite.pump(.15)

    @property
    def text(self):
        return self.screen.text

    @property
    def editor(self):
        return '\n'.join(self.text.splitlines()[-9:])

    @property
    def working(self):
        match = re.search(r'(\d+) turns · ↑([\d.kM]+) ↓([\d.kM]+) · ([\d.]+) TPS · (?:(\d+)m)?(\d+)s', self.text)
        if not match:
            return None
        turns, inputs, outputs, tps, minutes, seconds = match.groups()
        return {'turns': int(turns), 'input': inputs, 'output': outputs, 'tps': float(tps),
                'seconds': int(minutes or 0) * 60 + int(seconds)}

    def visible(self, text):
        self.suite.wait(lambda: text in self.text, f'{self.name}: visible {text!r}')

    def quit(self):
        # Ctrl+D quits the idle TUI without introducing a second application action.
        self.send(b'\x04')
        self.suite.wait(lambda: self.p.poll() is not None, f'{self.name}: graceful exit')
        assert self.p.returncode == 0, f'{self.name}: exit={self.p.returncode}'


class Suite:
    def __init__(self, root, binary):
        self.root, self.binary = root, binary
        self.terminals, self.owned, self.results = [], {}, []
        for name in ('home', 'agent', 'cwd', 'tmp', 'helpers'):
            (root / name).mkdir()
        domain = hashlib.sha256(os.fsencode((root / 'agent').resolve())).hexdigest()
        self.registry = (root / 'home').resolve() / '.pi' / 'goui-rpc' / domain
        self.session = root / 'session.jsonl'
        self.session.write_text(json.dumps({'type': 'session', 'version': 3, 'id': 'shared-e2e',
                                           'timestamp': '2026-01-01T00:00:00Z', 'cwd': str(root / 'cwd')}) + '\n')
        wrapper = root / 'helpers/fake-pi'
        wrapper.write_text('#!/bin/sh\nexec ' + shlex.quote(sys.executable) + ' ' +
                           shlex.quote(str(ROOT / 'tests/shared_rpc.py')) + ' "$@"\n')
        wrapper.chmod(0o700)
        self.env = os.environ.copy()
        for key in ('PI_SESSION_ID', 'PI_SESSION_FILE', 'TMUX', 'TMUX_PANE', 'STY', 'DISPLAY',
                    'WAYLAND_DISPLAY', 'SSH_AUTH_SOCK'):
            self.env.pop(key, None)
        self.env.update(HOME=str(root / 'home'), PI_CODING_AGENT_DIR=str(root / 'agent'),
                        PI_BIN=str(wrapper), GOUI_SHARED_TEST_ROOT=str(root), TMPDIR=str(root / 'tmp'),
                        XDG_CONFIG_HOME=str(root / 'home'), TERM='xterm-256color', COLORTERM='truecolor')

    def wire(self, kind=None):
        path = self.root / 'wire.jsonl'
        records = [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []
        return [r for r in records if kind is None or r['kind'] == kind]

    def commands(self, typ):
        return [r['command'] for r in self.wire('command') if r['command']['type'] == typ]

    def record(self):
        assert not (self.root / 'agent/goui-rpc').exists(), 'shared registry leaked into agent directory'
        try:
            return json.loads((self.registry / 'coordinator/endpoint.json').read_text())
        except FileNotFoundError:
            return {}

    def remember(self):
        # Only private, test-created registry records and fixture logs are used.
        for path in self.registry.glob('**/endpoint.json'):
            try:
                record = json.loads(path.read_text())
                pid = record.get('PID')
                if pid and pid not in self.owned:
                    cmdline = pathlib.Path(f'/proc/{pid}/cmdline').read_bytes()
                    if str(self.root).encode() in cmdline:
                        self.owned[pid] = identity(pid)
            except (FileNotFoundError, ProcessLookupError):
                pass
        for record in self.wire('start'):
            self.owned[record['pid']] = record['birth']

    def pump(self, seconds=.1):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            readers = {t.master: t for t in self.terminals}
            ready, _, _ = select.select(list(readers), [], [], min(.04, max(0, deadline-time.monotonic())))
            for fd in ready:
                terminal = readers[fd]
                try:
                    data = os.read(fd, 65536)
                except OSError:
                    continue
                terminal.raw.extend(data)
                terminal.screen.feed(data)
                for query, reply in ((b'\x1b[6n', b'\x1b[1;1R'), (b'\x1b[c', b'\x1b[?1;2c'),
                                     (b'\x1b[?u', b'\x1b[?1u')):
                    if query in data:
                        os.write(fd, reply)
            self.remember()

    def wait(self, predicate, description, timeout=12):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.pump()
            if predicate():
                return
        self.dump()
        raise AssertionError(description + '\n' + '\n'.join(t.name + ':\n' + t.text for t in self.terminals))

    def control(self, pid, typ, **fields):
        with (self.root / 'control.jsonl').open('a') as out:
            out.write(json.dumps({'pid': pid, 'type': typ, **fields}) + '\n')

    def event(self, pid, **event):
        self.control(pid, 'event', event={'type': 'extension_ui_request', **event})

    def checkpoint(self, name):
        self.results.append(name)
        print('PASS:', name, flush=True)

    def dump(self):
        for t in self.terminals:
            (self.root / f'{t.name}.screen.txt').write_text(t.text)
            (self.root / f'{t.name}.terminal.bin').write_bytes(t.raw)

    def cleanup(self):
        self.remember()
        self.dump()
        # Resume only our stopped children, then close them. Do not kill by name.
        for pid, birth in list(self.owned.items()):
            if birth and identity(pid) == birth:
                try:
                    os.kill(pid, signal.SIGCONT)
                    os.kill(pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
        time.sleep(.4)
        for pid, birth in list(self.owned.items()):
            if birth and identity(pid) == birth:
                try:
                    os.kill(pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        for t in self.terminals:
            try:
                t.p.wait(timeout=4)
            finally:
                os.close(t.master)
                os.close(t.slave)

    def run(self):
        a, b = Terminal(self, 'A'), Terminal(self, 'B')
        for t in (a, b):
            t.visible('shared-fixture')
        self.wait(lambda: len(self.wire('start')) == 1, 'one backend on concurrent cold start')
        pid = self.wire('start')[0]['pid']
        initial = self.record()
        assert initial['PID'] in (a.p.pid, b.p.pid), 'non-TUI coordinator'
        leader, survivor = (a, b) if initial['PID'] == a.p.pid else (b, a)
        self.checkpoint('concurrent real TUI cold start: exactly one fake Pi and an actual TUI coordinator')

        a.send('draft-A')
        b.send('draft-B')
        assert 'draft-A' in a.editor and 'draft-B' not in a.editor, a.editor
        assert 'draft-B' in b.editor and 'draft-A' not in b.editor, b.editor
        self.checkpoint('two actual TUI editors retain distinct unsent drafts')
        a.send('\r')
        for t in (a, b):
            t.visible('STREAM-draft-A')
            assert '1 working' in t.text, f'{t.name}: streaming without shared busy state'
        assert 'draft-B' in b.editor, 'shared stream overwrote B local editor'
        self.control(pid, 'finish')
        for t in (a, b):
            t.visible('RESULT-draft-A')
        self.wait(lambda: all('1 working' not in t.text for t in (a, b)), 'both TUIs leave busy state')
        assert 'draft-B' in b.editor, 'completed turn overwrote B local editor'
        self.checkpoint('independent drafts survive shared streaming and completed messages')
        b.send(b'\x15')
        self.wait(lambda: 'draft-B' not in b.editor, 'clear B draft')

        os.kill(leader.p.pid, signal.SIGSTOP)
        self.pump(.8)
        assert self.record()['PID'] == initial['PID'], 'SIGSTOP caused a second coordinator'
        assert self.record()['Socket'] == initial['Socket'], 'SIGSTOP changed coordinator generation'
        survivor.send('while-stopped\r')
        survivor.visible('STREAM-while-stopped')
        self.control(pid, 'finish')
        survivor.visible('RESULT-while-stopped')
        assert len(self.wire('start')) == 1
        os.kill(leader.p.pid, signal.SIGCONT)
        leader.visible('RESULT-while-stopped')
        self.checkpoint('SIGSTOP retains coordinator lock; survivor directly uses unchanged Pi')

        survivor.send('during-failover\r')
        survivor.visible('STREAM-during-failover')
        self.control(pid, 'progress')
        survivor.visible('3 turns · ↑51k ↓665')
        self.pump(2.1)  # Joining must not restart the original elapsed clock.
        os.kill(leader.p.pid, signal.SIGKILL)
        self.wait(lambda: leader.p.poll() is not None, 'leader killed')
        self.wait(lambda: self.record().get('PID') == survivor.p.pid and
                  self.record().get('Socket') != initial['Socket'], 'surviving actual TUI wins new term')
        # Join only after election, while the original run is still active.
        c = Terminal(self, 'C')
        c.visible('3 turns · ↑51k ↓665')
        def synchronized_progress():
            old, joined = survivor.working, c.working
            return (old is not None and joined is not None and
                    old['seconds'] == joined['seconds'] and old['seconds'] >= 2 and
                    abs(old['tps'] - joined['tps']) <= max(1, old['tps'] * .05))
        self.wait(synchronized_progress, 'late join inherits original elapsed/TPS, not a new clock')
        before = c.working['seconds']
        self.wait(lambda: c.working is not None and c.working['seconds'] > before,
                  'late join elapsed clock continues without new native events')
        assert len(self.commands('prompt')) == 3, 'late join replayed a prompt'
        self.control(pid, 'finish')
        for t in (survivor, c):
            t.visible('RESULT-during-failover')
        self.wait(lambda: all('1 working' not in t.text for t in (survivor, c)), 'shared run settles')
        frozen = survivor.working
        assert frozen == c.working and frozen['turns'] == 3 and frozen['output'] == '665', 'settled statistics differ'
        self.pump(1.2)
        assert survivor.working == c.working == frozen, 'settled elapsed/TPS did not freeze'
        self.checkpoint('late join during active run inherits turns/tokens/original elapsed/TPS; settles identically without replay')
        assert len(self.wire('start')) == 1 and identity(pid), 'failover restarted Pi'
        self.checkpoint('SIGKILL during active turn: new term, same live Pi, completion preserved')

        c.send('after-failover\r')
        for t in (survivor, c):
            t.visible('STREAM-after-failover')
            # The 21-character live body estimates 6 tokens until its final usage.
            t.visible('1 turns · ↑10 ↓6')
        self.control(pid, 'finish')
        for t in (survivor, c):
            t.visible('RESULT-after-failover')
        assert [r['message'] for r in self.commands('prompt')] == [
            'draft-A', 'while-stopped', 'during-failover', 'after-failover'], 'operation replay or loss'
        assert len(self.wire('start')) == 1
        survivor.quit()
        self.wait(lambda: self.record().get('PID') == c.p.pid, 'graceful coordinator handoff')
        assert identity(pid) and len(self.wire('start')) == 1
        self.checkpoint('late join and graceful leader exit retain history/Pi; four prompts occur exactly once')

        self.event(pid, id='status', method='setStatus', statusKey='acceptance', statusText='SHARED-STATUS')
        self.event(pid, id='widget', method='setWidget', widgetKey='acceptance', widgetLines=['SHARED-WIDGET'])
        self.event(pid, id='title', method='setTitle', title='SHARED-TITLE')
        c.visible('SHARED-STATUS')
        c.visible('SHARED-WIDGET')
        d = Terminal(self, 'D')
        d.visible('SHARED-STATUS')
        d.visible('SHARED-WIDGET')
        self.wait(lambda: b'SHARED-TITLE' in d.raw, 'late join window title')
        self.event(pid, id='question-1', method='select', title='SHARED-QUESTION', options=['Approve', 'Decline'])
        for t in (c, d):
            t.visible('SHARED-QUESTION')
        d.send('\r')
        self.wait(lambda: len(self.commands('extension_ui_response')) == 1, 'one questionnaire answer')
        self.wait(lambda: all('SHARED-QUESTION' not in t.text for t in (c, d)), 'resolved questionnaire closes on both TUIs')
        self.checkpoint('late join receives status/widget/title; successful first answer closes both questionnaires')

        c.quit()
        assert identity(pid), 'non-last subscriber stopped backend'
        d.quit()
        self.wait(lambda: identity(pid) is None, 'last subscriber reaps Pi')
        self.checkpoint('non-last departure preserves Pi; last departure reaps Pi')

        host = Terminal(self, 'desktop-host', shared=True)
        host.visible('RESULT-after-failover')
        self.wait(lambda: len(self.wire('start')) == 2, 'reopen after last departure')
        record = self.record()
        assert record['PID'] != host.p.pid, 'outer PTY host joined election'
        cmdline = pathlib.Path(f"/proc/{record['PID']}/cmdline").read_bytes()
        assert b'--internal-shared-tui' in cmdline, 'coordinator is not the hidden TUI'
        second_pid = self.wire('start')[-1]['pid']
        host.quit()
        self.wait(lambda: identity(second_pid) is None, 'desktop last subscriber reaps Pi')
        self.checkpoint('default desktop host reopens history; only hidden TUI participates; final Pi reaped')
        return self.results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path)
    parser.add_argument('--keep', action='store_true')
    args = parser.parse_args()
    if not sys.platform.startswith('linux'):
        parser.error('Linux /proc, PTY and signals are required; cross-platform acceptance is separate')
    temporary = tempfile.TemporaryDirectory(prefix='goui-shared-e2e-') if not args.keep else None
    root = pathlib.Path(temporary.name if temporary else tempfile.mkdtemp(prefix='goui-shared-e2e-'))
    suite = None
    try:
        binary = args.binary.resolve() if args.binary else root / 'pi-goui'
        if not args.binary:
            subprocess.run(['go', 'build', '-o', str(binary), './cmd/pi-goui'], cwd=ROOT, check=True)
        suite = Suite(root, binary)
        suite.run()
        print(json.dumps({'result': 'passed', 'checks': suite.results, 'isolated_root': str(root)}, ensure_ascii=False))
    except Exception:
        print('FAILED; isolated diagnostics:', root, '(use --keep to retain)', file=sys.stderr)
        raise
    finally:
        if suite:
            suite.cleanup()
        if temporary:
            temporary.cleanup()


if __name__ == '__main__':
    main()
