#!/usr/bin/env python3
"""Linux real-PTY offline resume acceptance, Python standard library only.

python3 script/test-resume.py [--binary PATH] [--keep]
Temporary build, HOME/agent/cwd, native history and gated fake RPC are isolated.
Times cold first paint, a prewarmed sidebar click and a direct click on a
1200-turn history without the helpers' usual 150 ms input/100 ms polling delays.
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
spec = importlib.util.spec_from_file_location('shared_resume_helpers', ROOT / 'script/test-session-ownership.py')
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)
HISTORY = 'SAVED-HISTORY-BEFORE-CONNECTION'
BAD_STATUS = re.compile(r'\b(?:stopped|stoped)\b|identity pending', re.I)


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
        (root / 'agent/settings.json').write_text(json.dumps({
            'defaultProvider': 'test', 'defaultModel': 'resume-fixture', 'defaultThinkingLevel': 'low'}))
        (root / 'agent/models.json').write_text(json.dumps({'providers': {'test': {'models': [
            {'id': 'resume-fixture', 'name': 'Resume fixture', 'contextWindow': 200000}]}}}))
        self.session = root / 'agent/sessions/project/saved.jsonl'
        self.session.parent.mkdir(parents=True)
        header = {'type': 'session', 'version': 3, 'id': 'resume-saved',
                  'timestamp': '2026-01-01T00:00:00Z', 'cwd': str(root / 'cwd')}
        entries = [header,
                   {'type': 'session_info', 'id': 'info', 'parentId': None, 'name': 'Resume acceptance'},
                   {'type': 'model_change', 'id': 'model', 'parentId': 'info',
                    'provider': 'test', 'modelId': 'resume-fixture'},
                   {'type': 'thinking_level_change', 'id': 'thinking', 'parentId': 'model', 'thinkingLevel': 'high'},
                   {'type': 'message', 'id': 'old-user', 'parentId': 'thinking',
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
        self.wait(lambda: '↵ send' in terminal.text and re.search(r'\bidle\b', terminal.text),
                  name + ': blank local tab ready with idle status')
        self.wait(lambda: 'resume-fixture' in terminal.editor and 'low' in terminal.editor,
                  name + ': local default model and thinking visible before startup')
        assert not self.wire('start'), 'opening a blank tab started Pi'
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

    def preview_idle(self, terminal):
        self.watch = terminal
        self.wait(lambda: HISTORY in terminal.text, 'saved history visible before Pi ready')
        assert 'resume-fixture' in terminal.editor and 'high' in terminal.editor, 'saved model/thinking missing before startup'
        assert re.search(r'\bin\s+1\s+out\s+1\b', terminal.text), 'saved usage missing before startup'
        assert '2/200K' in terminal.text, 'saved context tokens and configured window missing before startup'
        assert not self.wire('start'), 'history preview started Pi'
        assert not (self.root / 'release-state').exists()
        assert not self.wire('released'), 'fixture was already released'
        assert not self.commands('prompt'), 'unexpected prompt before connection ready'
        assert self.session.read_bytes() == self.saved_bytes, 'preview modified native saved history'
        assert not BAD_STATUS.search(terminal.text) and 'Starting pi' not in terminal.text, 'misleading idle preview: ' + terminal.text
        self.pump(.35)
        assert all(not BAD_STATUS.search(f) for f in self.frames), 'transient starting/stopped during preview'
        self.dump()
        (self.root / 'gated-preview.txt').write_text(terminal.text)

    def release(self):
        (self.root / 'release-state').touch()

    @staticmethod
    def editor_row(terminal):
        lines = terminal.text.splitlines()
        # A full transcript can put its last bubble within the bottom nine
        # rows. Find the editor via its footer and contiguous content rows;
        # failure notices can replace the editor's top border.
        row = next((i for i in reversed(range(len(lines)))
                    if lines[i].startswith('│') and '↵ send' in lines[i]), None)
        if row is None:  # A partially received repaint is not evidence yet.
            return 0
        while row > 0 and lines[row - 1].startswith('│'):
            row -= 1
        return row

    @classmethod
    def user_bubbles(cls, terminal, text):
        transcript = '\n'.join(terminal.text.splitlines()[:cls.editor_row(terminal)])
        return len(re.findall(r'│\s*' + re.escape(text) + r'\s*│', transcript))

    def first_send(self, terminal):
        terminal.send('FIRST-INTENT')
        self.wait(lambda: 'FIRST-INTENT' in terminal.editor, 'typing during blocked get_state')
        started = time.monotonic()
        os.write(terminal.master, b'\r')
        while self.user_bubbles(terminal, 'FIRST-INTENT') != 1:
            if time.monotonic() - started > 3:
                raise AssertionError('ordinary user bubble missing before backend readiness: ' + terminal.text)
            self.pump(.005)
        echo_ms = round((time.monotonic() - started) * 1000, 1)
        editor = '\n'.join(terminal.text.splitlines()[self.editor_row(terminal):])
        assert 'FIRST-INTENT' not in editor, 'echo left a duplicate in the editor'
        assert not self.commands('prompt'), 'echo waited for actual prompt submission'
        assert self.session.read_bytes() == self.saved_bytes, 'echo changed native saved history'
        self.wait(lambda: bool(self.wire('gate')), 'explicit write reached the blocked get_state')
        self.wait(lambda: '1 turns · 0s' in terminal.editor, 'first turn and initial duration visible before blocked startup finishes')
        assert not re.search(r'\bworking\b', terminal.editor), 'submission flashed an intermediate working label'
        assert not any(label in terminal.text for label in ('待发送', '发送中', 'Starting pi', 'Loading current session'))
        terminal.send('LATER-LOCAL-DRAFT')
        self.wait(lambda: 'LATER-LOCAL-DRAFT' in terminal.editor, 'later draft while intent pending')
        assert self.user_bubbles(terminal, 'FIRST-INTENT') == 1, 'later typing erased or duplicated the echo'
        assert not self.commands('prompt'), 'prompt escaped readiness gate'
        native_file = pathlib.Path(self.wire('start')[-1]['session'])
        assert 'FIRST-INTENT' not in native_file.read_text(), 'display echo was persisted before dispatch'
        self.release()
        self.wait(lambda: len(self.commands('prompt')) == 1, 'deferred intent submitted once after ready')
        self.wait(lambda: 'RESULT-FIRST-INTENT' in terminal.text, 'deferred prompt result visible')
        self.wait(lambda: 'LATER-LOCAL-DRAFT' in terminal.editor, 'later draft survived authoritative refresh')
        self.pump(.8)
        prompts = self.commands('prompt')
        assert len(prompts) == 1 and prompts[0]['message'] == 'FIRST-INTENT', prompts
        assert self.user_bubbles(terminal, 'FIRST-INTENT') == 1, 'native confirmation duplicated or lost the user bubble'
        records = [json.loads(line) for line in native_file.read_text().splitlines() if line.strip()]
        users = [entry.get('message', {}) for entry in records if entry.get('message', {}).get('role') == 'user']
        assert sum(any(part.get('text') == 'FIRST-INTENT' for part in msg.get('content', [])) for msg in users) == 1
        assert all(not BAD_STATUS.search(f) for f in self.frames), 'misleading resume status after ready'
        print(json.dumps({'prompt_echo': terminal.name, 'visible_ms': echo_ms, 'prompt_blocked_until_release': True}), flush=True)

    def run_blank_echo(self):
        terminal = self.blank_ready('blank-echo')
        self.first_send(terminal)
        self.checkpoint('blank session: normal user bubble before gated startup, no local history write, one confirmed message')

    def run_initial(self):
        terminal = Terminal(self, 'initial-saved', saved=True)
        self.watch = terminal
        self.preview_idle(terminal)
        self.first_send(terminal)
        self.checkpoint('initial --session: idle native preview starts no Pi; explicit Enter waits for authoritative state and preserves later draft')

    def run_replace(self):
        sibling = self.blank_ready('other-live-blank')
        terminal = self.blank_ready('replace-blank')
        assert not self.wire('start'), 'blank sessions should be local views'
        self.select_history(terminal)
        self.preview_idle(terminal)
        self.first_send(terminal)
        assert sibling.p.poll() is None and '↵ send' in sibling.text and 'Idle' not in sibling.text
        assert len(self.wire('start')) == 1, 'history started more than one backend'
        self.checkpoint('empty native blank -> /resume: other blank stays local, only explicit history write starts Pi')

    def run_preserve(self):
        terminal = self.blank_ready('preserved-draft')
        terminal.send('ORIGINAL-BLANK-DRAFT')
        self.select_history(terminal, draft=True)
        self.preview_idle(terminal)
        terminal.send('HISTORY-LOCAL-DRAFT')
        assert not self.wire('start'), 'typing in a preview started Pi'
        terminal.send(b'\x1b[1;5A')  # Default app.session.previous (Ctrl+Up).
        self.wait(lambda: 'ORIGINAL-BLANK-DRAFT' in terminal.editor, 'original blank tab draft preserved')
        terminal.send(b'\x1b[1;5B')
        self.wait(lambda: 'HISTORY-LOCAL-DRAFT' in terminal.editor, 'resumed tab draft preserved')
        assert HISTORY in terminal.text
        assert not self.commands('prompt'), 'tab navigation sent a draft'
        assert not self.wire('start'), 'tab navigation started a backend'
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
        terminal.send(b'\x01\x0b')  # Clear only the fixture's draft before the send check.
        self.first_send(terminal)
        self.checkpoint('large native history: cold first paint and immediate user echo before backend readiness, confirmed once')

    def sidebar_target(self, terminal):
        def target():
            for row, line in enumerate(terminal.text.splitlines()):
                column = line.rfind('Resume acceptance')
                if column >= 100:  # The right sidebar, not a dialog or editor.
                    return column + 4, row + 1
            return None

        self.wait(lambda: target() is not None, 'large history sidebar row')
        return target()

    def run_many_turn_sidebar(self):
        # Unlike the prewarmed case below, click directly: measure parsing and
        # layout as well as delivery of the latest visible message to the PTY.
        records = self.session.read_text().splitlines()[:-1]
        parent = 'old-user'
        payload = 'synthetic read output. ' * 1024
        with self.session.open('w') as f:
            f.write('\n'.join(records) + '\n')

            def emit(key, message):
                nonlocal parent
                f.write(json.dumps({'type': 'message', 'id': key, 'parentId': parent,
                                    'message': message}, separators=(',', ':')) + '\n')
                parent = key

            for i in range(1200):
                emit(f'u{i}', self.message('user', f'Review **module {i}** in `module_{i}.go`.'))
                reply = self.message('assistant', f'## Review {i}\n\n- Checking the [source](https://example.invalid/{i}).')
                reply['content'].append({'type': 'toolCall', 'name': 'read', 'id': f't{i}', 'arguments': {'path': f'module_{i}.go'}})
                emit(f'a{i}', reply)
                emit(f'r{i}', {'role': 'toolResult', 'toolCallId': f't{i}', 'toolName': 'read',
                               'content': [{'type': 'text', 'text': payload}]})
            emit('end', self.message('assistant', HISTORY))
        self.saved_bytes = self.session.read_bytes()
        # Recents automatically preloads the first row even without a hover.
        # Put a different session first so the target really is cold on click.
        decoy = self.session.with_name('newer.jsonl')
        header = {**json.loads(records[0]), 'id': 'newer'}
        decoy.write_text(json.dumps(header) + '\n')
        stamp = self.session.stat().st_mtime + 1
        os.utime(decoy, (stamp, stamp))
        terminal = self.blank_ready('many-turn-sidebar')
        x, y = self.sidebar_target(terminal)
        started = time.monotonic()
        os.write(terminal.master, ('\x1b[<0;%d;%dM\x1b[<0;%d;%dm' % (x, y, x, y)).encode())
        self.first_paint(terminal, started, 8.0)
        assert not self.wire('start'), 'opening saved history started Pi'
        self.checkpoint('cold sidebar click paints latest of 1200 complete tool turns without hover or backend')

    def run_large_sidebar(self):
        self.large_history()
        terminal = self.blank_ready('large-sidebar')
        x, y = self.sidebar_target(terminal)
        os.write(terminal.master, ('\x1b[<35;%d;%dM' % (x, y)).encode())
        self.pump(1.0)  # Explicit prewarm interval; excluded from click-to-paint timing.
        assert not self.wire('start'), 'speculative preview started a Pi backend'
        started = time.monotonic()
        os.write(terminal.master, ('\x1b[<0;%d;%dM\x1b[<0;%d;%dm' % (x, y, x, y)).encode())
        self.first_paint(terminal, started, 1.0)
        terminal.send('LARGE-SIDEBAR-DRAFT')
        assert 'LARGE-SIDEBAR-DRAFT' in terminal.editor
        self.checkpoint('large prewarmed sidebar: real mouse click paints promptly without starting Pi during preload')

    def run_failure(self):
        (self.root / 'fail-state').touch()
        terminal = Terminal(self, 'failed-connection', saved=True)
        self.preview_idle(terminal)
        terminal.send('UNSENT-ON-FAILURE\r')
        self.wait(lambda: bool(self.wire('gate')), 'explicit write reaches failing connection')
        self.wait(lambda: self.user_bubbles(terminal, 'UNSENT-ON-FAILURE') == 1,
                  'local bubble visible while failing connection is still gated')
        assert self.session.read_bytes() == self.saved_bytes, 'local echo was written into native history'
        terminal.send('LATER-FAILURE-DRAFT')
        self.release()
        self.wait(lambda: '连接失败' in terminal.text, 'real backend failure is clearly reported')
        assert HISTORY in terminal.text, 'failure erased readable saved history'
        assert 'LATER-FAILURE-DRAFT' in terminal.editor, 'failure overwrote later draft'
        assert 'UNSENT-ON-FAILURE' in terminal.editor, 'failure lost the original input'
        assert self.user_bubbles(terminal, 'UNSENT-ON-FAILURE') == 0, 'failure retained an unconfirmed user bubble'
        assert self.session.read_bytes() == self.saved_bytes, 'failed startup changed native history'
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
        for name in ('blank_echo', 'initial', 'replace', 'preserve', 'failure', 'large_initial', 'large_sidebar', 'many_turn_sidebar'):
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
