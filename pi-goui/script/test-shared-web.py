#!/usr/bin/env python3
"""Offline Linux desktop + WebUI/WS ownership and terminal-mirror acceptance.

Python standard library only. Builds both CLIs in an isolated temporary root
unless --desktop-binary and --web-binary are supplied. No real Pi/model runs.
Run: python3 script/test-shared-web.py [--keep]
The WebSocket client substitutes for a browser, not for the HTTP service or TUI.
"""
import argparse
import base64
import fcntl
import hashlib
import importlib.util
import json
import os
import pathlib
import re
import select
import shutil
import signal
import socket
import struct
import subprocess
import sys
import tempfile
import termios
import time
import urllib.parse
import urllib.request

# Import fixtures without creating bytecode alongside existing repository files.
sys.dont_write_bytecode = True
ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('shared_session_acceptance', ROOT / 'script/test-session-ownership.py')
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)


class WebTerminal:
    """Minimal RFC6455 loopback client with masking, ping/pong and VT replies."""
    def __init__(self, url, token, project, instance=None, cols=132, rows=36):
        parsed = urllib.parse.urlsplit(url)
        self.sock = socket.create_connection((parsed.hostname, parsed.port), timeout=3)
        self.pending = bytearray()
        self.raw = bytearray()
        self.screen = shared.Screen(cols, rows)
        self.events = []
        self.fragments = bytearray()
        self.fragment_kind = None
        key = base64.b64encode(os.urandom(16)).decode()
        prefix = '/api/instances/' + urllib.parse.quote(instance, safe='') if instance else ''
        path = prefix + '/api/terminal?' + urllib.parse.urlencode({'projectId': project, 'cols': cols, 'rows': rows})
        request = (f'GET {path} HTTP/1.1\r\nHost: {parsed.netloc}\r\n'
                   f'Origin: {url}\r\nAuthorization: Bearer {token}\r\n'
                   'Upgrade: websocket\r\nConnection: Upgrade\r\n'
                   f'Sec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\n\r\n')
        self.sock.sendall(request.encode())
        header = bytearray()
        while b'\r\n\r\n' not in header:
            data = self.sock.recv(4096)
            if not data:
                raise AssertionError('WebSocket closed during upgrade')
            header.extend(data)
            assert len(header) < 65536, 'oversized HTTP upgrade'
        headers, tail = bytes(header).split(b'\r\n\r\n', 1)
        assert headers.startswith(b'HTTP/1.1 101 '), headers.decode(errors='replace')
        accept = base64.b64encode(hashlib.sha1((key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').encode()).digest())
        fields = {key.lower(): value.strip() for key, value in
                  (line.split(b':', 1) for line in headers.split(b'\r\n')[1:])}
        assert fields[b'sec-websocket-accept'] == accept, 'invalid WS handshake'
        self.pending.extend(tail)
        self.sock.setblocking(False)

    @property
    def text(self):
        return self.screen.text

    def send(self, data, opcode=2):
        if isinstance(data, str):
            data = data.encode()
        mask = os.urandom(4)
        size = len(data)
        head = bytes([0x80 | opcode])
        if size < 126:
            head += bytes([0x80 | size])
        elif size < 65536:
            head += bytes([0x80 | 126]) + struct.pack('!H', size)
        else:
            head += bytes([0x80 | 127]) + struct.pack('!Q', size)
        self.sock.sendall(head + mask + bytes(b ^ mask[i % 4] for i, b in enumerate(data)))

    def control(self, typ, **fields):
        self.send(json.dumps({'type': typ, **fields}), opcode=1)

    def read(self):
        if select.select([self.sock], [], [], 0)[0]:
            data = self.sock.recv(65536)
            assert data, 'WebSocket unexpectedly closed'
            self.pending.extend(data)
        while len(self.pending) >= 2:
            first, second = self.pending[:2]
            opcode, size, offset = first & 15, second & 127, 2
            assert not second & 128, 'server sent masked WS frame'
            if size in (126, 127):
                width = 2 if size == 126 else 8
                if len(self.pending) < offset + width:
                    return
                size = int.from_bytes(self.pending[offset:offset + width], 'big')
                offset += width
            assert size <= 16 << 20, 'unbounded WS frame'
            if len(self.pending) < offset + size:
                return
            payload = bytes(self.pending[offset:offset + size])
            del self.pending[:offset + size]
            if opcode == 9:
                self.send(payload, 10)
                continue
            if opcode == 10:
                continue
            assert opcode != 8, 'server closed WS: ' + repr(payload)
            if opcode:
                assert self.fragment_kind is None, 'overlapping fragmented messages'
                self.fragment_kind = opcode
            self.fragments.extend(payload)
            if not first & 128:
                continue
            payload = bytes(self.fragments)
            opcode = self.fragment_kind
            self.fragments.clear()
            self.fragment_kind = None
            if opcode == 1:
                event = json.loads(payload)
                self.events.append(event)
                if event.get('type') == 'size':
                    cols, rows = event['cols'], event['rows']
                    if (cols, rows) != (self.screen.w, self.screen.h):
                        self.screen.resize(cols, rows)
            else:
                assert opcode == 2, 'unexpected WS opcode'
                self.raw.extend(payload)
                self.screen.feed(payload)
                if b'\x1b[6n' in payload:
                    self.send(b'\x1b[1;1R')
                if b'\x1b[c' in payload:
                    self.send(b'\x1b[?1;2c')
                if b'\x1b[?u' in payload:
                    self.send(b'\x1b[?1u')

    def close(self):
        if self.sock is not None:
            self.sock.close()
            self.sock = None


class WebSuite(shared.Suite):
    def __init__(self, root, desktop, web):
        super().__init__(root, desktop)
        self.web_binary = web
        self.agent, self.cwd = root / 'agent', root / 'cwd'
        self.fake = root / 'helpers/fake-pi'
        self.env['PI_OFFLINE'] = '1'
        self.web = None
        self.ws = None
        self.mirrors = []
        self.project_id = None
        self.web_log = None
        self.url = None
        self.token = 'isolated-web-acceptance-token'

    def pump(self, seconds=.12):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            super().pump(min(.025, max(0, end - time.monotonic())))
            if self.ws is not None:
                self.ws.read()
            for mirror in self.mirrors:
                mirror.read()

    def add(self, name):
        return shared.Terminal(self, name)

    def starts(self):
        return self.wire('start')

    def request(self, path, body=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.url + path, data=data,
                                         headers={'Authorization': 'Bearer ' + self.token,
                                                  'Content-Type': 'application/json'})
        # Explicitly bypass environment HTTP proxies, even for loopback.
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=2) as response:
            return json.load(response)

    def open_ws(self):
        info = self.request('/api/info')
        if not info['projectId']:
            info = self.request('/api/projects/select', {'cwd': info['cwd'], 'projectId': ''})
        self.project_id = info['projectId']
        self.ws = WebTerminal(self.url, self.token, self.project_id)
        return self.ws

    def stop_web(self):
        for mirror in self.mirrors:
            mirror.close()
        self.mirrors.clear()
        if self.ws is not None:
            self.ws.close()
            self.ws = None
        if self.web is not None and self.web.poll() is None:
            self.web.send_signal(signal.SIGTERM)
            try:
                self.web.wait(timeout=12)
            except subprocess.TimeoutExpired:
                self.web.kill()
                self.web.wait(timeout=5)
                raise AssertionError('WebUI failed graceful shutdown')
            assert self.web.returncode == 0, f'WebUI exit {self.web.returncode}'

    def dump(self):
        super().dump()
        if self.ws is not None:
            (self.root / 'web.screen.txt').write_text(self.ws.text)
            (self.root / 'web.terminal.bin').write_bytes(self.ws.raw)
            (self.root / 'web.controls.json').write_text(json.dumps(self.ws.events))

    def cleanup(self):
        try:
            self.dump()
            self.stop_web()
        finally:
            if self.web_log is not None:
                self.web_log.close()
            super().cleanup()

    def start_web(self):
        self.web_log = (self.root / 'web.log').open('wb')
        self.web = subprocess.Popen([
            str(self.web_binary), '--listen', '127.0.0.1:0', '--key', self.token,
            '--pi', str(self.fake), '--cwd', str(self.cwd), '--agent-dir', str(self.agent),
            '--session', str(self.session)], env=self.env, cwd=self.cwd,
            stdout=self.web_log, stderr=subprocess.STDOUT, start_new_session=True)

        def listening():
            content = (self.root / 'web.log').read_text(errors='replace')
            match = re.search(r'Pi WebUI: (http://127\.0\.0\.1:\d+)', content)
            if match:
                self.url = match.group(1)
                return True
            assert self.web.poll() is None, 'WebUI exited before listening'
            return False

        self.wait(listening, 'real HTTP server listening')

    def run_desktop_mirror(self):
        # Unlike run(), this is the desktop discovery route, not a Web-owned TUI.
        desktop = shared.Terminal(self, 'shared-desktop', shared=True)
        desktop.visible('SAVED-SESSION')
        desktop.idle()

        def tui_processes():
            found = {}
            for proc in pathlib.Path('/proc').iterdir():
                if not proc.name.isdigit():
                    continue
                try:
                    if os.readlink(proc / 'exe') == str(self.binary.resolve()):
                        pid = int(proc.name)
                        found[pid] = shared.identity(pid)
                except (FileNotFoundError, PermissionError, ProcessLookupError):
                    pass
            return found

        original = tui_processes()
        assert len(original) == 2 and desktop.p.pid in original, original
        tui = next(pid for pid in original if pid != desktop.p.pid)
        tty = os.readlink(f'/proc/{tui}/fd/0')

        def dimensions():
            fd = os.open(f'/proc/{tui}/fd/0', os.O_RDONLY | os.O_NOCTTY)
            try:
                rows, cols, _, _ = struct.unpack('HHHH', fcntl.ioctl(fd, termios.TIOCGWINSZ, b'\0' * 8))
                return cols, rows
            finally:
                os.close(fd)

        desktop.send('desktop-question-task\r')
        desktop.visible('STREAM-desktop-question-task')
        self.wait(lambda: len(self.starts()) == 1, 'desktop task starts exactly one Pi')
        writer = self.starts()[0]['pid']
        writer_parent = int(pathlib.Path(f'/proc/{writer}/stat').read_text().rsplit(')', 1)[1].split()[1])
        assert writer_parent == tui, 'fixture is not the original desktop TUI child'
        desktop.send('SHARED-DESKTOP-DRAFT')
        self.event(writer, id='desktop-question-one', method='input', title='pi-goui:ask-question:v1',
                   placeholder=json.dumps({'questions': [{'question': 'DESKTOP-FIRST-QUESTION',
                       'options': ['DESKTOP-CHOICE', 'PHONE-CHOICE'], 'multiSelect': True}]}))
        desktop.visible('DESKTOP-FIRST-QUESTION')
        desktop.send(' ')
        desktop.visible('[✓] DESKTOP-CHOICE')
        assert not self.commands('extension_ui_response')
        self.start_web()
        found = []

        def discover():
            found[:] = [item for item in self.request('/api/instances')['instances'] if item['desktop']]
            return bool(found)

        self.wait(discover, 'desktop sharing instance discovered after original question opens')
        assert len(found) == 1 and found[0]['id'].startswith('desktop-'), found
        instance = found[0]['id']
        info = self.request('/api/instances/' + instance + '/api/info')
        project = info['projectId']
        assert project, 'discovered desktop lost its existing project'

        def connect():
            self.ws = WebTerminal(self.url, self.token, project, instance=instance, cols=96, rows=28)
            self.wait(lambda: bool(self.ws.events) and dimensions() == (96, 28), 'phone connects to desktop PTY')

        def unchanged():
            assert tui_processes() == original, 'desktop sharing created or replaced a TUI'
            assert len(self.starts()) == 1 and self.starts()[0]['pid'] == writer, 'desktop sharing restarted Pi'
            assert os.readlink(f'/proc/{tui}/fd/0') == tty, 'desktop sharing replaced the PTY'
            assert shared.identity(writer) == self.starts()[0]['birth'], 'original writer stopped'
            assert len(self.commands('prompt')) == 1, 'desktop mirror replayed a prompt'

        connect()
        self.wait(lambda: 'DESKTOP-FIRST-QUESTION' in self.ws.text and '[✓] DESKTOP-CHOICE' in self.ws.text,
                  'phone sees the original ask_question and desktop selection without confirmation')
        unchanged()
        assert 'Read-only' not in self.ws.text and desktop.p.poll() is None
        self.ws.send('\x1b[B ')
        self.wait(lambda: all('[✓] PHONE-CHOICE' in view.text for view in (desktop, self.ws)),
                  'phone selection updates the original question on both displays')
        desktop.send('\x1b[A')
        self.wait(lambda: dimensions() == (150, 42), 'desktop input reclaims desktop dimensions')
        self.ws.control('resize', cols=72, rows=25)
        self.ws.control('ping')
        self.control(writer, 'progress')
        self.wait(lambda: any(event.get('type') == 'pong' for event in self.ws.events)
                  and bool(self.wire('progress')), 'passive resize, heartbeat and task output processed')
        assert dimensions() == (150, 42), 'passive phone traffic stole desktop dimensions'
        self.ws.control('activity', cols=72, rows=25)
        self.ws.send('\x1b[B')
        self.wait(lambda: dimensions() == (72, 25), 'phone interaction takes its current dimensions')
        fcntl.ioctl(desktop.slave, termios.TIOCSWINSZ, struct.pack('HHHH', 40, 142, 0, 0))
        os.kill(desktop.p.pid, signal.SIGWINCH)
        self.pump(.3)
        assert dimensions() == (72, 25), 'inactive desktop window resize stole phone dimensions'
        desktop.send('\x1b[A')
        self.wait(lambda: dimensions() == (142, 40), 'desktop input uses its updated passive window dimensions')
        self.ws.control('activity', cols=72, rows=25)
        self.ws.send('\r')
        self.wait(lambda: len(self.commands('extension_ui_response')) == 1, 'phone answers original ask_question once')
        answer = self.commands('extension_ui_response')[0]
        assert answer['id'] == 'desktop-question-one', answer
        value = json.loads(answer['value'])
        assert value['cancelled'] is False and value['answers'] == [{
            'answers': ['DESKTOP-CHOICE', 'PHONE-CHOICE'], 'wasCustom': False, 'completed': True}], value
        self.wait(lambda: all('DESKTOP-FIRST-QUESTION' not in view.text and 'SHARED-DESKTOP-DRAFT' in view.text
                              for view in (desktop, self.ws)), 'both displays dismiss question and keep original draft')
        unchanged()
        self.checkpoint('desktop-first ask_question is answered once by late phone on the same TUI/Pi/PTY; selection and draft survive bidirectional sizing')

        self.control(writer, 'event', event={'type': 'tool_execution_start', 'toolCallId': 'shared-read',
                     'toolName': 'read', 'args': {'path': 'desktop-shared-tool.txt'}})
        self.control(writer, 'event', event={'type': 'tool_execution_end', 'toolCallId': 'shared-read',
                     'toolName': 'read', 'isError': False,
                     'result': {'content': [{'type': 'text', 'text': 'DESKTOP-SHARED-TOOL-RESULT'}]}})
        self.wait(lambda: all('desktop-shared-tool.txt' in view.text for view in (desktop, self.ws)),
                  'same native tool card appears on desktop and phone')
        unchanged()
        self.event(writer, id='desktop-question-two', method='input', title='pi-goui:ask-question:v1',
                   placeholder=json.dumps({'questions': [{'question': 'RECONNECT-QUESTION',
                       'options': ['KEEP-THIS-ANSWER', 'OTHER-ANSWER'], 'multiSelect': True}]}))
        self.wait(lambda: 'RECONNECT-QUESTION' in self.ws.text, 'second question opens before disconnect')
        self.ws.send(' ')
        self.wait(lambda: '[✓] KEEP-THIS-ANSWER' in self.ws.text, 'phone makes an unsubmitted selection')
        self.ws.close()
        self.ws = None
        desktop.visible('RECONNECT-QUESTION')
        unchanged()
        assert len(self.commands('extension_ui_response')) == 1
        connect()
        self.wait(lambda: 'RECONNECT-QUESTION' in self.ws.text and '[✓] KEEP-THIS-ANSWER' in self.ws.text,
                  'reconnected phone restores original pending questionnaire selection')
        self.pump(.2)
        assert len(self.commands('extension_ui_response')) == 1, 'reconnect automatically answered a question'
        self.ws.send('\r')
        self.wait(lambda: len(self.commands('extension_ui_response')) == 2, 'reconnected phone submits second question once')
        second_answer = self.commands('extension_ui_response')[1]
        assert second_answer['id'] == 'desktop-question-two'
        assert json.loads(second_answer['value'])['answers'][0]['answers'] == ['KEEP-THIS-ANSWER']
        self.control(writer, 'progress')
        self.wait(lambda: len(self.wire('progress')) == 2, 'original task continues after phone answers')
        unchanged()
        self.ws.close()
        self.ws = None
        self.control(writer, 'finish')
        desktop.visible('RESULT-desktop-question-task')
        self.wait(lambda: shared.identity(writer) is None, 'task finishes while phone is disconnected')
        connect()
        self.wait(lambda: 'RESULT-desktop-question-task' in self.ws.text and 'SHARED-DESKTOP-DRAFT' in self.ws.text,
                  'phone reconnect restores completed current task and unsent draft')
        assert tui_processes() == original and len(self.starts()) == 1
        assert len(self.commands('prompt')) == 1 and len(self.commands('extension_ui_response')) == 2
        self.stop_web()
        assert tui_processes() == original and desktop.p.poll() is None
        desktop.send('-AFTER-WEB')
        desktop.visible('SHARED-DESKTOP-DRAFT-AFTER-WEB')
        desktop.send(b'\x01\x0b')  # Explicit teardown: discard the already-verified unsent draft.
        desktop.quit()
        self.checkpoint('phone disconnect/reconnect preserves pending ask_question, original task and draft without resubmission; Web shutdown leaves desktop alive')

    def run(self):
        desktop = self.add('desktop')
        desktop.visible('SAVED-SESSION')
        desktop.idle()
        assert not self.starts(), 'desktop preview started Pi'
        desktop.send('desktop-active\r')
        desktop.visible('STREAM-desktop-active')
        self.wait(lambda: len(self.starts()) == 1, 'desktop starts its explicit write')
        first = self.starts()[0]['pid']
        self.start_web()
        self.open_ws()
        self.wait(lambda: 'Read-only' in self.ws.text and 'SAVED-SESSION' in self.ws.text,
                  'independent Web TUI observes the desktop session as read-only')
        assert len(self.starts()) == 1 and len(self.commands('prompt')) == 1
        assert 'STREAM-desktop-active' not in self.ws.text
        self.checkpoint('real HTTP/WS independent TUI opens read-only while desktop holds the session')
        self.ws.close()
        self.ws = None
        assert shared.identity(first), 'viewer disconnect stopped desktop writer'
        self.control(first, 'finish')
        desktop.visible('RESULT-desktop-active')
        desktop.idle()
        self.wait(lambda: not shared.identity(first), 'desktop writer released at idle')
        self.open_ws()
        self.wait(lambda: 'RESULT-desktop-active' in self.ws.text and shared.idle_screen(self.ws.text),
                  'Web reconnect reads latest persisted result after idle')
        assert len(self.starts()) == 1
        self.checkpoint('read-only Web disconnect is harmless; reconnect refreshes saved completion without Pi startup')

        self.ws.send('web-turn\r')
        self.wait(lambda: len(self.starts()) == 2 and 'STREAM-web-turn' in self.ws.text,
                  'Web TUI acquires idle session and starts fresh Pi')
        second = self.starts()[-1]['pid']
        desktop.visible('Read-only')
        mirror = WebTerminal(self.url, self.token, self.project_id)
        self.mirrors.append(mirror)
        self.wait(lambda: 'STREAM-web-turn' in mirror.text, 'second browser mirrors the same TUI live output')
        self.event(second, id='web-question', method='select', title='WEB-OWNER-QUESTION', options=['Approve', 'Decline'])
        self.wait(lambda: all('WEB-OWNER-QUESTION' in item.text for item in (self.ws, mirror)),
                  'two browsers mirror one owner questionnaire')
        assert 'WEB-OWNER-QUESTION' not in desktop.text
        mirror.send('\r')
        self.wait(lambda: len(self.commands('extension_ui_response')) == 1, 'mirror answers through the owning TUI once')
        self.wait(lambda: all('WEB-OWNER-QUESTION' not in item.text for item in (self.ws, mirror)),
                  'one TUI answer updates both mirrors')
        self.checkpoint('two browsers retain live mirroring and shared interaction within one TUI')
        mirror.close()
        self.mirrors.clear()
        self.ws.close()
        self.ws = None
        self.pump(.3)
        assert shared.identity(second), 'last browser disconnect stopped its hidden TUI task'
        self.control(second, 'finish')
        self.wait(lambda: not shared.identity(second), 'Web owner releases writer after disconnected completion')
        self.open_ws()
        self.wait(lambda: 'RESULT-web-turn' in self.ws.text and shared.idle_screen(self.ws.text),
                  'Web reconnect restores completion and idle')
        desktop.visible('RESULT-web-turn')
        self.checkpoint('all browser connections may leave while their TUI finishes and releases the session')

        self.ws.send('web-orphan\r')
        self.wait(lambda: len(self.starts()) == 3 and 'STREAM-web-orphan' in self.ws.text,
                  'hosted TUI starts the crash fixture')
        writer = self.starts()[-1]['pid']
        self.control(writer, 'keep_alive_after_eof')
        self.wait(lambda: any(r['pid'] == writer for r in self.wire('keep_alive')), 'orphan mode armed')
        self.ws.close()
        self.ws = None

        def parent(pid):
            return int(pathlib.Path(f'/proc/{pid}/stat').read_text().rsplit(')', 1)[1].split()[1])

        tui = parent(writer)
        assert parent(tui) == self.web.pid and os.getsid(writer) == tui, 'not the isolated hosted TUI'
        locks = self.registry / 'locks'
        lock_ids = {p.name: p.stat().st_ino for p in locks.glob('*.lock')}
        assert lock_ids
        os.kill(tui, signal.SIGKILL)
        self.wait(lambda: not shared.identity(tui) and any(r['pid'] == writer for r in self.wire('orphan')),
                  'RPC survives death of hosted TUI')
        desktop.visible('Read-only')
        assert shared.identity(writer)
        assert self.request('/api/terminal/restart', {'projectId': self.project_id})['ok']
        self.wait(lambda: not shared.identity(writer), 'restart reclaims surviving RPC')
        assert lock_ids == {p.name: p.stat().st_ino for p in locks.glob('*.lock')}
        self.checkpoint('scoped restart after hosted TUI death reclaims its surviving RPC without replacing locks')

        self.stop_web()
        assert desktop.p.poll() is None, 'Web shutdown killed independent desktop'
        desktop.idle()
        desktop.send('after-web-exit\r')
        desktop.visible('STREAM-after-web-exit')
        self.wait(lambda: len(self.starts()) == 4, 'desktop acquires after Web shutdown')
        third = self.starts()[-1]['pid']
        self.control(third, 'finish')
        desktop.visible('RESULT-after-web-exit')
        desktop.idle()
        self.wait(lambda: not shared.identity(third), 'final idle writer exits')
        assert [x['message'] for x in self.commands('prompt')] == ['desktop-active', 'web-turn', 'web-orphan', 'after-web-exit']
        desktop.quit()
        self.checkpoint('Web shutdown is isolated; desktop can acquire and finish another execution cycle')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--desktop-binary', type=pathlib.Path)
    parser.add_argument('--web-binary', type=pathlib.Path)
    parser.add_argument('--keep', action='store_true')
    args = parser.parse_args()
    if not sys.platform.startswith('linux'):
        parser.error('Linux /proc process-identity checks required')
    root = pathlib.Path(tempfile.mkdtemp(prefix='goui-shared-web-'))
    suite = None
    try:
        binaries = []
        for supplied, name in [(args.desktop_binary, 'pi-goui'), (args.web_binary, 'webui')]:
            binary = supplied.resolve() if supplied else root / name
            if not supplied:
                subprocess.run(['go', 'build', '-o', str(binary), './cmd/' + name], cwd=ROOT, check=True)
            binaries.append(binary)
        suite = WebSuite(root, *binaries)
        suite.run()
        checks = list(suite.results)
        suite.cleanup()
        suite = None
        mirror_root = root / 'desktop-mirror'
        mirror_root.mkdir()
        suite = WebSuite(mirror_root, *binaries)
        suite.run_desktop_mirror()
        checks.extend(suite.results)
        print(json.dumps({'result': 'passed', 'checks': checks, 'isolated_root': str(root)}))
    except Exception:
        print('FAILED; isolated diagnostics:', root, '(--keep retains them)', file=sys.stderr)
        raise
    finally:
        try:
            if suite is not None:
                suite.cleanup()
        finally:
            if not args.keep:
                shutil.rmtree(root)


if __name__ == '__main__':
    main()
