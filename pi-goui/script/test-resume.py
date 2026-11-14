#!/usr/bin/env python3
"""Linux real-PTY offline resume acceptance, Python standard library only.

python3 script/test-resume.py [--binary PATH] [--keep]
Temporary build, HOME/agent/cwd, native history and gated fake RPC are isolated.
Includes a roughly 16 MiB native history: cold first paint and prewarmed sidebar
click are timed without the helpers' usual 150 ms input/100 ms polling delays.
No real Pi, credentials, model calls or production resources are used.
"""
import argparse
import fcntl
import importlib.util
import json
import os
import pathlib
import re
import shlex
import shutil
import struct
import subprocess
import sys
import tempfile
import termios
import time

sys.dont_write_bytecode = True
ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('shared_resume_helpers', ROOT / 'script/test-shared-session.py')
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)
HISTORY = 'SAVED-HISTORY-BEFORE-CONNECTION'
BAD_STATUS = re.compile(r'\b(?:starting|stopped|stoped)\b|identity pending', re.I)


class Terminal(shared.Terminal):
    def __init__(self, suite, name, saved=False):
        self.suite, self.name = suite, name
        self.master, self.slave = os.openpty()
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack('HHHH', 42, 150, 0, 0))
        self.screen, self.raw = shared.Screen(150, 42), bytearray()
        args = [str(suite.binary), '--share=false', '--cwd', str(suite.root / 'cwd')]
        if saved:
            args += ['--session', str(suite.session)]
        args += ['--', '--offline']
        self.p = subprocess.Popen(args, env=suite.env, cwd=suite.root / 'cwd', stdin=self.slave,
                                  stdout=self.slave, stderr=self.slave, start_new_session=True)
        suite.terminals.append(self)
        suite.owned[self.p.pid] = shared.identity(self.p.pid)


class Suite(shared.Suite):
    def __init__(self, root, binary):
        super().__init__(root, binary)
        self.env['PI_OFFLINE'] = '1'
        (root / 'agent/keybindings.json').write_text(json.dumps({'app.session.resume': ['ctrl+y']}))
        self.session = root / 'agent/sessions/project/saved.jsonl'
        self.session.parent.mkdir(parents=True)
        header = {'type': 'session', 'version': 3, 'id': 'resume-saved',
                  'timestamp': '2026-01-01T00:00:00Z', 'cwd': str(root / 'cwd')}
        entries = [header,
                   {'type': 'session_info', 'id': 'info', 'parentId': None, 'name': 'Resume acceptance'},
                   {'type': 'message', 'id': 'old-user', 'parentId': 'info',
                    'message': self.message('user', 'NATIVE-SAVED-QUESTION')},
                   {'type': 'message', 'id': 'old-answer', 'parentId': 'old-user',
                    'message': self.message('assistant', HISTORY)}]
        self.session.write_text(''.join(json.dumps(e) + '\n' for e in entries))
        self.saved_bytes = self.session.read_bytes()
        wrapper = root / 'helpers/fake-pi'
        wrapper.write_text('#!/bin/sh\nexec ' + shlex.quote(sys.executable) + ' ' +
                           shlex.quote(str(ROOT / 'tests/resume_rpc.py')) + ' "$@"\n')
        self.watch = None
        self.frames = []

    @staticmethod
    def message(role, text):
        return {'role': role, 'content': [{'type': 'text', 'text': text}], 'timestamp': 1700000000000,
                'provider': 'test', 'model': 'resume-fixture', 'stopReason': 'stop',
                'usage': {'input': 1, 'output': 1, 'totalTokens': 2, 'cost': {'total': 0}}}

    def wire(self, kind=None):
        # A fixture can be between append/write and close while the PTY is polled.
        path = self.root / 'wire.jsonl'
        records = []
        if path.exists():
            raw = path.read_text()
            lines = raw.splitlines()
            if raw and not raw.endswith('\n'):
                lines = lines[:-1]  # Only the final incomplete append may be ignored.
            records = [json.loads(line) for line in lines]
        return [r for r in records if kind is None or r['kind'] == kind]

    def pump(self, seconds=.1):
        super().pump(seconds)
        if self.watch is not None:
            self.frames.append(self.watch.text)

    def dump(self):
        super().dump()
        (self.root / 'observed-frames.json').write_text(json.dumps(self.frames, ensure_ascii=False))

    def blank_ready(self, name):
        terminal = Terminal(self, name)
        self.wait(lambda: 'resume-fixture' in terminal.text, name + ': blank backend ready')
        self.wait(lambda: not BAD_STATUS.search(terminal.text), name + ': startup settled')
        starts = [r for r in self.wire('start') if not r['saved']]
        assert starts, 'blank fixture did not start'
        assert all(not pathlib.Path(r['session']).exists() for r in starts), 'blank path unexpectedly persisted'
        return terminal

    def select_history(self, terminal, draft=False):
        if draft:
            terminal.send(b'\x19')  # User-configured resume shortcut; draft stays untouched.
        else:
            terminal.send('/resume\r')
        self.wait(lambda: 'Resume acceptance' in terminal.text and 'escape cancel' in terminal.text,
                  'native /resume selector visible')
        terminal.send('Resume acceptance')
        terminal.send('\r')

    def preview_gated(self, terminal):
        self.watch = terminal
        self.wait(lambda: HISTORY in terminal.text, 'saved history visible before Pi ready')
        self.wait(lambda: bool(self.wire('gate')), 'saved get_state reached external gate')
        assert not (self.root / 'release-state').exists()
        assert not self.wire('released'), 'fixture was already released'
        assert not self.commands('prompt'), 'unexpected prompt before connection ready'
        assert self.session.read_bytes() == self.saved_bytes, 'preview modified native saved history'
        assert not BAD_STATUS.search(terminal.text), 'misleading resume status: ' + terminal.text
        self.pump(.35)
        assert all(not BAD_STATUS.search(f) for f in self.frames), 'transient starting/stopped during preview'
        self.dump()
        (self.root / 'gated-preview.txt').write_text(terminal.text)

    def release(self):
        (self.root / 'release-state').touch()

    def first_send(self, terminal):
        terminal.send('FIRST-INTENT')
        self.wait(lambda: 'FIRST-INTENT' in terminal.editor, 'typing during blocked get_state')
        terminal.send('\r')
        terminal.send('LATER-LOCAL-DRAFT')
        self.wait(lambda: 'LATER-LOCAL-DRAFT' in terminal.editor, 'later draft while intent pending')
        assert not self.commands('prompt'), 'prompt escaped readiness gate'
        self.release()
        self.wait(lambda: len(self.commands('prompt')) == 1, 'deferred intent submitted once after ready')
        self.wait(lambda: 'RESULT-FIRST-INTENT' in terminal.text, 'deferred prompt result visible')
        self.wait(lambda: 'LATER-LOCAL-DRAFT' in terminal.editor, 'later draft survived authoritative refresh')
        self.pump(.8)
        prompts = self.commands('prompt')
        assert len(prompts) == 1 and prompts[0]['message'] == 'FIRST-INTENT', prompts
        assert all(not BAD_STATUS.search(f) for f in self.frames), 'misleading resume status after ready'

    def run_initial(self):
        terminal = Terminal(self, 'initial-saved', saved=True)
        self.watch = terminal
        self.preview_gated(terminal)
        self.first_send(terminal)
        self.checkpoint('initial --session: native history precedes blocked get_state; editing/Enter retained once with later draft')

    def run_replace(self):
        sibling = self.blank_ready('other-live-blank')
        terminal = self.blank_ready('replace-blank')
        assert len(self.wire('start')) == 2, 'blank sessions must be independent'
        self.select_history(terminal)
        self.preview_gated(terminal)
        self.first_send(terminal)
        assert sibling.p.poll() is None and 'resume-fixture' in sibling.text
        assert len(self.wire('start')) == 3, 'history started more than one backend'
        self.checkpoint('empty native blank -> /resume: other unpersisted blank stays live, one history backend, no identity conflict')

    def run_preserve(self):
        terminal = self.blank_ready('preserved-draft')
        terminal.send('ORIGINAL-BLANK-DRAFT')
        self.select_history(terminal, draft=True)
        self.preview_gated(terminal)
        terminal.send('HISTORY-LOCAL-DRAFT')
        self.release()
        self.wait(lambda: any(r['saved'] and r['command']['type'] == 'get_commands'
                              for r in self.wire('command')), 'new history authoritative snapshot fetched')
        terminal.send(b'\x1b[1;5A')  # Default app.session.previous (Ctrl+Up).
        self.wait(lambda: 'ORIGINAL-BLANK-DRAFT' in terminal.editor, 'original blank tab draft preserved')
        terminal.send(b'\x1b[1;5B')
        self.wait(lambda: 'HISTORY-LOCAL-DRAFT' in terminal.editor, 'resumed tab draft preserved')
        assert HISTORY in terminal.text
        assert not self.commands('prompt'), 'tab navigation sent a draft'
        assert len(self.wire('start')) == 2, 'preserved blank plus history should use two backends'
        self.checkpoint('resume from a drafted blank opens a second tab; both local drafts survive tab navigation')

    def large_history(self):
        records = [json.loads(line) for line in self.session.read_text().splitlines()][:2]
        parent = records[-1]['id']
        payload = 'opaque native tool detail ' * 4096
        for i in range(160):
            call = 'large-call-%d' % i
            messages = [self.message('user', 'Historical request %d' % i),
                        {**self.message('assistant', ''), 'content': [
                            {'type': 'toolCall', 'id': call, 'name': 'bash',
                             'arguments': {'command': 'printf fixture'}}]},
                        {'role': 'toolResult', 'toolCallId': call, 'toolName': 'bash',
                         'content': [{'type': 'text', 'text': 'completed %d' % i}],
                         'details': {'output': payload}, 'isError': False}]
            for message in messages:
                key = 'large-%d' % len(records)
                records.append({'type': 'message', 'id': key, 'parentId': parent, 'message': message})
                parent = key
        records.append({'type': 'message', 'id': 'large-latest', 'parentId': parent,
                        'message': self.message('assistant', HISTORY)})
        self.session.write_text(''.join(json.dumps(r, separators=(',', ':')) + '\n' for r in records))
        self.saved_bytes = self.session.read_bytes()
        assert len(self.saved_bytes) > 15 * 1024 * 1024

    def first_paint(self, terminal, started, budget):
        deadline = started + budget
        while HISTORY not in terminal.text and time.monotonic() < deadline:
            self.pump(.005)
        elapsed = time.monotonic() - started
        assert HISTORY in terminal.text, 'large history first paint exceeded %.1fs' % budget
        assert not BAD_STATUS.search(terminal.text)
        assert not (self.root / 'release-state').exists(), 'first paint waited for backend release'
        assert not self.commands('prompt'), 'preview submitted a prompt'
        assert self.session.read_bytes() == self.saved_bytes, 'preview changed native history'
        print(json.dumps({'scenario': self.root.name, 'history_bytes': len(self.saved_bytes),
                          'first_paint_ms': round(elapsed * 1000, 1)}), flush=True)

    def run_large_initial(self):
        self.large_history()
        started = time.monotonic()
        terminal = Terminal(self, 'large-cold', saved=True)
        self.first_paint(terminal, started, 3.0)
        terminal.send('LARGE-COLD-DRAFT')
        assert 'LARGE-COLD-DRAFT' in terminal.editor
        self.checkpoint('large native history: cold first paint before backend readiness, editing remains available')

    def run_large_sidebar(self):
        self.large_history()
        terminal = self.blank_ready('large-sidebar')

        def target():
            for row, line in enumerate(terminal.text.splitlines()):
                column = line.rfind('Resume acceptance')
                if column >= 100:  # The right sidebar, not a dialog or editor.
                    return column + 4, row + 1
            return None

        self.wait(lambda: target() is not None, 'large history sidebar row')
        x, y = target()
        os.write(terminal.master, ('\x1b[<35;%d;%dM' % (x, y)).encode())
        self.pump(1.0)  # Explicit prewarm interval; excluded from click-to-paint timing.
        assert len(self.wire('start')) == 1, 'speculative preview started a Pi backend'
        started = time.monotonic()
        os.write(terminal.master, ('\x1b[<0;%d;%dM\x1b[<0;%d;%dm' % (x, y, x, y)).encode())
        self.first_paint(terminal, started, 1.0)
        terminal.send('LARGE-SIDEBAR-DRAFT')
        assert 'LARGE-SIDEBAR-DRAFT' in terminal.editor
        self.checkpoint('large prewarmed sidebar: real mouse click paints promptly without starting Pi during preload')

    def run_failure(self):
        (self.root / 'fail-state').touch()
        terminal = Terminal(self, 'failed-connection', saved=True)
        self.preview_gated(terminal)
        terminal.send('UNSENT-ON-FAILURE\r')
        terminal.send('LATER-FAILURE-DRAFT')
        self.release()
        self.wait(lambda: '连接失败' in terminal.text, 'real backend failure is clearly reported')
        assert HISTORY in terminal.text, 'failure erased readable saved history'
        assert 'LATER-FAILURE-DRAFT' in terminal.editor, 'failure overwrote later draft'
        assert not self.commands('prompt'), 'failed connection sent or retried deferred intent'
        assert not BAD_STATUS.search(terminal.text), 'failure incorrectly labelled stopped'
        self.checkpoint('backend failure retains readable native history and local draft, reports connection failure, never submits')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=pathlib.Path)
    parser.add_argument('--keep', action='store_true')
    args = parser.parse_args()
    if not sys.platform.startswith('linux'):
        parser.error('Linux /proc process identity checks required')
    root = pathlib.Path(tempfile.mkdtemp(prefix='goui-resume-e2e-'))
    failed = False
    try:
        binary = args.binary.resolve() if args.binary else root / 'pi-goui'
        if not args.binary:
            subprocess.run(['go', 'build', '-o', str(binary), './cmd/pi-goui'], cwd=ROOT, check=True)
        checks = []
        for name in ('initial', 'replace', 'preserve', 'failure', 'large_initial', 'large_sidebar'):
            folder = root / name
            folder.mkdir()
            suite = Suite(folder, binary)
            try:
                getattr(suite, 'run_' + name)()
                checks += suite.results
            finally:
                suite.cleanup()
        print(json.dumps({'result': 'passed', 'checks': checks, 'isolated_root': str(root)}, ensure_ascii=False))
    except Exception:
        failed = True
        print('FAILED; isolated diagnostics retained:', root, file=sys.stderr)
        raise
    finally:
        if not args.keep and not failed:
            shutil.rmtree(root)


if __name__ == '__main__':
    main()
