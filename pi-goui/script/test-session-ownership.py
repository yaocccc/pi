#!/usr/bin/env python3
"""Linux real-CLI/PTY exclusive-session acceptance; Python standard library only.

Run: python3 script/test-session-ownership.py [--binary /path/to/pi-goui]
All HOME/agent/cwd/session/RPC data and the optional build live in a temporary
root. --keep preserves that root for failure investigation (never credentials).
"""
import argparse
import fcntl
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


def idle_screen(text):
    return '↵ send' in text and not any(label in text for label in (
        'Read-only', 'Finishing session', 'Starting pi'))


class Terminal:
    def __init__(self, suite, name, shared=False, saved=True):
        self.suite, self.name = suite, name
        self.master, self.slave = os.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack('HHHH', 42, 150, 0, 0))
        self.screen, self.raw = Screen(150, 42), bytearray()
        args = [str(suite.binary), '--cwd', str(suite.root / 'cwd')]
        if saved:
            args += ['--session', str(suite.session)]
        args += ['--', '--offline']
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

    def idle(self):
        self.suite.wait(lambda: idle_screen(self.text) and all(
            identity(record['pid']) != record['birth'] for record in self.suite.wire('start')),
            self.name + ': writer exited and editable UI restored')

    def quit(self):
        # A read-only draft stays intact until this explicit close confirmation.
        self.send(b'\x04')
        self.suite.wait(lambda: self.p.poll() is not None or 'Close session' in self.text,
                        f'{self.name}: exit or draft confirmation')
        if self.p.poll() is None:
            self.send(b'\x1b[B\r')
        self.suite.wait(lambda: self.p.poll() is not None, f'{self.name}: graceful exit')
        assert self.p.returncode == 0, f'{self.name}: exit={self.p.returncode}'


class Suite:
    def __init__(self, root, binary):
        self.root, self.binary = root, binary
        self.terminals, self.owned, self.results = [], {}, []
        for name in ('home', 'agent', 'cwd', 'tmp', 'helpers'):
            (root / name).mkdir()
        self.registry = (root / 'home').resolve() / '.pi' / 'goui-locks'
        self.session = root / 'session.jsonl'
        self.session.write_text(json.dumps({'type': 'session', 'version': 3, 'id': 'ownership-e2e',
                                           'timestamp': '2026-01-01T00:00:00Z', 'cwd': str(root / 'cwd')}) + '\n' + json.dumps({
                                               'type': 'message', 'id': 'seed', 'parentId': None,
                                               'message': {'role': 'assistant', 'content': [{'type': 'text', 'text': 'SAVED-SESSION'}],
                                                           'timestamp': 1700000000000, 'stopReason': 'stop'}}) + '\n')
        wrapper = root / 'helpers/fake-pi'
        wrapper.write_text('#!/bin/sh\nexec ' + shlex.quote(sys.executable) + ' ' +
                           shlex.quote(str(ROOT / 'tests/session_rpc.py')) + ' "$@"\n')
        wrapper.chmod(0o700)
        self.env = os.environ.copy()
        for key in ('PI_SESSION_ID', 'PI_SESSION_FILE', 'TMUX', 'TMUX_PANE', 'STY', 'DISPLAY',
                    'WAYLAND_DISPLAY', 'SSH_AUTH_SOCK'):
            self.env.pop(key, None)
        self.env.update(HOME=str(root / 'home'), PI_CODING_AGENT_DIR=str(root / 'agent'),
                        PI_BIN=str(wrapper), GOUI_SESSION_TEST_ROOT=str(root), PI_OFFLINE='1', TMPDIR=str(root / 'tmp'),
                        XDG_CONFIG_HOME=str(root / 'home'), TERM='xterm-256color', COLORTERM='truecolor')

    def wire(self, kind=None):
        path = self.root / 'wire.jsonl'
        records = [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []
        return [r for r in records if kind is None or r['kind'] == kind]

    def commands(self, typ):
        return [r['command'] for r in self.wire('command') if r['command']['type'] == typ]

    def remember(self):
        # Only fixture logs from this private test root identify owned children.
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

    def run_fresh(self):
        owner = Terminal(self, 'fresh-owner', saved=False)
        owner.idle()
        assert not self.wire('start'), 'blank tab eagerly started Pi'
        owner.send('new-first\r')
        owner.visible('STREAM-new-first')
        self.wait(lambda: len(self.wire('start')) == 1, 'new session starts one writer')
        start = self.wire('start')[0]
        self.session = pathlib.Path(start['session'])
        assert self.session.exists(), 'first user message did not persist at the preselected path'
        before = self.session.read_bytes()
        viewer = Terminal(self, 'fresh-viewer')
        viewer.visible('Read-only')
        assert len(self.wire('start')) == 1, 'newly persisted session admitted a second writer'
        self.control(start['pid'], 'finish')
        for terminal in (owner, viewer):
            terminal.visible('RESULT-new-first')
            terminal.idle()
        self.wait(lambda: not identity(start['pid']), 'first fresh writer exited')
        assert self.session.read_bytes().startswith(before), 'session identity changed on first persistence'
        viewer.send('new-second\r')
        viewer.visible('STREAM-new-second')
        self.wait(lambda: len(self.wire('start')) == 2, 'fresh session handed off for second run')
        second = self.wire('start')[-1]
        assert second['session'] == str(self.session), 'handoff changed the native history path'
        loaded = [r for r in self.wire('loaded') if r['pid'] == second['pid']]
        assert loaded and 'RESULT-new-first' in loaded[-1]['texts']
        self.control(second['pid'], 'finish')
        for terminal in (owner, viewer):
            terminal.visible('RESULT-new-second')
            terminal.idle()
        self.wait(lambda: not identity(second['pid']), 'second fresh writer exited')
        assert [r['message'] for r in self.commands('prompt')] == ['new-first', 'new-second']
        owner.quit()
        viewer.quit()
        self.checkpoint('new session locks its preselected path before first write and hands off the same persisted history')

    def run(self):
        a, b = Terminal(self, 'A'), Terminal(self, 'B')
        for terminal in (a, b):
            terminal.visible('SAVED-SESSION')
            terminal.idle()
        self.pump(.4)
        assert not self.wire('start'), 'opening a session started Pi'
        assert not self.registry.exists(), 'read-only browsing created lock metadata'
        self.checkpoint('two independent TUIs browse history without starting Pi or acquiring a lock')

        a.send('draft-A')
        b.send('draft-B')
        assert 'draft-A' in a.editor and 'draft-B' not in a.editor, a.editor
        assert 'draft-B' in b.editor and 'draft-A' not in b.editor, b.editor
        a.send('\r')
        a.visible('STREAM-draft-A')
        self.wait(lambda: len(self.wire('start')) == 1, 'one backend for the first explicit write')
        first = self.wire('start')[0]['pid']
        b.visible('working · held by another instance')
        b.visible('Read-only')
        b.visible(f'PID {a.p.pid}')
        assert 'draft-B' in b.editor, 'read-only polling overwrote the local draft'
        assert 'STREAM-draft-A' not in b.text, 'independent viewer received a live stream'
        b.send('\r')
        self.pump(.3)
        assert len(self.commands('prompt')) == 1, 'read-only Enter forwarded or queued a prompt'
        assert 'draft-B' in b.editor, 'read-only Enter consumed the draft'
        self.checkpoint('busy session shows its holder, refuses foreign writes, and preserves independent drafts')

        # Type while A is still working so its completion row remains visible;
        # editing after completion would dismiss it and mask a handoff regression.
        a.send('draft-after-handoff')
        self.control(first, 'finish')
        for terminal in (a, b):
            terminal.visible('RESULT-draft-A')
            terminal.idle()
        self.wait(lambda: identity(first) is None, 'idle writer exits while both TUIs remain open')
        assert len(self.commands('prompt')) == 1, 'idle transition automatically submitted a foreign draft'
        assert 'draft-B' in b.editor
        assert a.p.poll() is None and b.p.poll() is None
        self.checkpoint('idle closes Pi and releases ownership; persisted result reaches the viewer without auto-submit')

        assert re.search(r'✓ \d+ turns', a.text), 'first owner did not retain its own completed working row'
        b.send('\r')
        b.visible('STREAM-draft-B')
        self.wait(lambda: len(self.wire('start')) == 2, 'second instance starts a fresh writer after idle')
        second = self.wire('start')[-1]['pid']
        assert second != first
        loaded = [r for r in self.wire('loaded') if r['pid'] == second]
        assert loaded and 'RESULT-draft-A' in loaded[-1]['texts'], 'handoff reused stale in-memory history'
        a.visible('Read-only')
        os.kill(b.p.pid, signal.SIGSTOP)
        self.pump(2.3)
        a.send('\r')
        assert len(self.wire('start')) == 2, 'SIGSTOP allowed a second writer'
        assert len(self.commands('prompt')) == 2, 'SIGSTOP replayed or accepted a foreign prompt'
        assert 'draft-after-handoff' in a.editor
        os.kill(b.p.pid, signal.SIGCONT)
        self.checkpoint('fresh writer reloads current history; SIGSTOP never permits timeout takeover')

        c = Terminal(self, 'C')
        c.visible('RESULT-draft-A')
        c.visible('Read-only')
        self.event(second, id='question-1', method='select', title='OWNER-QUESTION', options=['Approve', 'Decline'])
        b.visible('OWNER-QUESTION')
        assert 'OWNER-QUESTION' not in a.text and 'OWNER-QUESTION' not in c.text
        b.send('\r')
        self.wait(lambda: len(self.commands('extension_ui_response')) == 1, 'owner answers the question once')
        self.control(second, 'finish')
        for terminal in (a, b, c):
            terminal.visible('RESULT-draft-B')
            terminal.idle()
        self.wait(lambda: identity(second) is None, 'second idle writer exits')
        assert a.working is None, 'previous owner restored stale completed working statistics after handoff'
        assert not re.search(r'\bidle\b', a.editor), 'idle appeared in the previous owner working message'
        assert re.search(r'\bidle\b', a.text), 'previous owner sidebar lost its idle status'
        assert re.search(r'✓ \d+ turns', b.text), 'executing owner lost its own completion row'
        self.checkpoint('foreign writer handoff permanently clears the previous owner completion row without hiding the new owner result')
        assert 'draft-after-handoff' in a.editor
        assert len(self.commands('prompt')) == 2
        self.checkpoint('late viewers read persisted history; questions belong only to the executing TUI')

        b.send('must-not-run')
        a.send('\r')
        a.visible('STREAM-draft-after-handoff')
        self.wait(lambda: len(self.wire('start')) == 3, 'owner starts next execution cycle')
        third = self.wire('start')[-1]['pid']
        self.control(third, 'keep_alive_after_eof')
        self.wait(lambda: any(r['pid'] == third for r in self.wire('keep_alive')), 'fixture armed for parent death')
        claim_path, = (self.registry / 'claims').glob('*.json')
        old_token = json.loads(claim_path.read_text())['Token']
        lock_inodes = {path.name: (path.stat().st_dev, path.stat().st_ino)
                       for path in (self.registry / 'locks').glob('*.lock')}
        os.kill(a.p.pid, signal.SIGKILL)
        self.wait(lambda: a.p.poll() is not None, 'owning TUI killed')
        self.wait(lambda: any(r['pid'] == third for r in self.wire('orphan')), 'writer survives parent EOF')
        assert identity(third), 'orphan fixture did not stay alive'
        b.visible('Read-only')
        b.send('\r')
        self.pump(2.3)
        assert len(self.wire('start')) == 3 and len(self.commands('prompt')) == 3
        assert 'must-not-run' in b.editor
        self.checkpoint('parent SIGKILL leaves a surviving writer protected; another TUI cannot steal ownership')
        os.kill(third, signal.SIGKILL)
        self.wait(lambda: identity(third) is None, 'orphan fixture stopped')
        for terminal in (b, c):
            terminal.idle()
        assert not claim_path.exists(), 'dead writer left a stale Claim after recovery'
        assert len(self.wire('start')) == 3, 'recovery automatically started a writer'
        assert len(self.commands('prompt')) == 3, 'interrupted operation or old draft was automatically replayed'
        assert 'must-not-run' in b.editor, 'automatic recovery consumed the retained draft'
        assert lock_inodes == {path.name: (path.stat().st_dev, path.stat().st_ino)
                               for path in (self.registry / 'locks').glob('*.lock')}, 'recovery replaced a lock inode'
        self.checkpoint('orphan Pi exit automatically clears its stale Claim and restores idle without resending')

        b.send(b'\x01\x0b')
        b.send('recovery-B')
        c.send('recovery-C')
        # Both explicit submissions race before either viewer sees the new owner.
        os.write(b.master, b'\r')
        os.write(c.master, b'\r')
        self.wait(lambda: len(self.wire('start')) == 4 and len(self.commands('prompt')) == 4,
                  'two recovering TUIs admit exactly one new writer')
        fourth = self.wire('start')[-1]['pid']
        message = self.commands('prompt')[-1]['message']
        assert message in ('recovery-B', 'recovery-C'), message
        winner, loser = (b, c) if message == 'recovery-B' else (c, b)
        losing_draft = 'recovery-C' if loser is c else 'recovery-B'
        winner.visible('STREAM-' + message)
        loser.visible('Read-only')
        assert losing_draft in loser.editor, 'losing contender consumed its unsent draft'
        claim = json.loads(claim_path.read_text())
        assert claim['Token'] != old_token and claim['OwnerPID'] == winner.p.pid
        assert claim['LauncherPID'] == fourth, 'concurrent recovery damaged the new writer Claim'
        self.pump(1.2)
        assert len(self.wire('start')) == 4 and len(self.commands('prompt')) == 4
        assert json.loads(claim_path.read_text())['Token'] == claim['Token'], 'polling removed the winner Claim'
        self.control(fourth, 'finish')
        for terminal in (b, c):
            terminal.visible('RESULT-' + message)
            terminal.idle()
        self.pump(1.2)
        assert len(self.commands('prompt')) == 4 and losing_draft in loser.editor
        assert lock_inodes == {path.name: (path.stat().st_dev, path.stat().st_ino)
                               for path in (self.registry / 'locks').glob('*.lock')}
        self.checkpoint('simultaneous recovery submissions admit one writer, preserve the loser draft and keep the new Claim safe')
        # Editable drafts intentionally block Ctrl-D; explicitly clear only after
        # verifying that recovery and the losing submission both preserved it.
        loser.send(b'\x03')
        b.quit()
        c.quit()
        assert [r['message'] for r in self.commands('prompt')] == ['draft-A', 'draft-B', 'draft-after-handoff', message]
        return self.results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path)
    parser.add_argument('--keep', action='store_true')
    args = parser.parse_args()
    if not sys.platform.startswith('linux'):
        parser.error('Linux /proc, PTY and signals are required; cross-platform acceptance is separate')
    temporary = tempfile.TemporaryDirectory(prefix='goui-ownership-e2e-') if not args.keep else None
    root = pathlib.Path(temporary.name if temporary else tempfile.mkdtemp(prefix='goui-ownership-e2e-'))
    suite = None
    try:
        binary = args.binary.resolve() if args.binary else root / 'pi-goui'
        if not args.binary:
            subprocess.run(['go', 'build', '-o', str(binary), './cmd/pi-goui'], cwd=ROOT, check=True)
        checks = []
        for name, method in [('ownership', 'run'), ('fresh', 'run_fresh')]:
            folder = root / name
            folder.mkdir()
            suite = Suite(folder, binary)
            try:
                getattr(suite, method)()
                checks += suite.results
            finally:
                suite.cleanup()
                suite = None
        print(json.dumps({'result': 'passed', 'checks': checks, 'isolated_root': str(root)}, ensure_ascii=False))
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
