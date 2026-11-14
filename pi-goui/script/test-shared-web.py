#!/usr/bin/env python3
"""Offline Linux desktop + real WebUI/WS shared-session acceptance.

Python standard library only. Builds both CLIs in an isolated temporary root
unless --desktop-binary and --web-binary are supplied. No real Pi/model runs.
Run: python3 script/test-shared-web.py [--keep]
The WebSocket client substitutes for a browser, not for the HTTP service or TUI.
"""
import argparse
import base64
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
import time
import urllib.parse
import urllib.request

# Import fixtures without creating bytecode alongside existing repository files.
sys.dont_write_bytecode = True
ROOT = pathlib.Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('shared_session_acceptance', ROOT / 'script/test-shared-session.py')
shared = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shared)


class WebTerminal:
    """Minimal RFC6455 loopback client with masking, ping/pong and VT replies."""
    def __init__(self, url, token, project):
        parsed = urllib.parse.urlsplit(url)
        self.sock = socket.create_connection((parsed.hostname, parsed.port), timeout=3)
        self.pending = bytearray()
        self.raw = bytearray()
        self.screen = shared.Screen(132, 36)
        self.events = []
        self.fragments = bytearray()
        self.fragment_kind = None
        key = base64.b64encode(os.urandom(16)).decode()
        path = '/api/terminal?' + urllib.parse.urlencode({'projectId': project, 'cols': 132, 'rows': 36})
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
                self.events.append(json.loads(payload))
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
        self.web_log = None
        self.url = None
        self.token = 'isolated-web-acceptance-token'

    def pump(self, seconds=.12):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            super().pump(min(.025, max(0, end - time.monotonic())))
            if self.ws is not None:
                self.ws.read()

    def add(self, name):
        return shared.Terminal(self, name)

    def starts(self):
        return self.wire('start')

    def assert_one_backend(self):
        starts = self.starts()
        assert len(starts) == 1, f'duplicate Pi backend starts: {starts}'
        return starts[0]['pid']

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
        self.ws = WebTerminal(self.url, self.token, info['projectId'])
        return self.ws

    def stop_web(self):
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

    def run(self):
        desktop = self.add('desktop-first')
        self.wait(lambda: len(self.starts()) == 1 and 'shared-fixture' in desktop.text, 'desktop ready')
        pid = self.assert_one_backend()
        initial = self.record()
        assert initial['PID'] == desktop.p.pid, 'desktop actual TUI is not coordinator'
        desktop.send('desktop-active\r')
        self.wait(lambda: len(self.commands('prompt')) == 1, 'desktop starts before Web exists')
        desktop.visible('STREAM-desktop-active')
        self.control(pid, 'progress')
        desktop.visible('3 turns · ↑51k ↓665')
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
        assert self.record()['PID'] == desktop.p.pid, 'HTTP-only process joined election'
        self.open_ws()
        self.wait(lambda: 'shared-fixture' in self.ws.text, 'Web-derived independent TUI loaded session')
        assert self.assert_one_backend() == pid
        self.checkpoint('real HTTP/WS default session and desktop use one Pi; HTTP service does not lead')
        self.wait(lambda: '3 turns · ↑51k ↓665' in self.ws.text and ' TPS · ' in self.ws.text,
                  'new Web-derived TUI inherits active working statistics')
        assert len(self.commands('prompt')) == 1, 'Web late join replayed the desktop prompt'
        self.control(pid, 'finish')
        self.wait(lambda: all('RESULT-desktop-active' in text and 'Esc interrupt' not in text
                             for text in (desktop.text, self.ws.text)), 'desktop-started run settles in both TUIs')
        self.checkpoint('Web started during a desktop run inherits working turns/tokens/TPS without replay')

        self.ws.send('web-turn\r')
        self.wait(lambda: len(self.commands('prompt')) == 2, 'Web TUI prompt submitted')
        self.wait(lambda: 'STREAM-web-turn' in desktop.text and 'STREAM-web-turn' in self.ws.text,
                  'streaming shared between desktop and Web TUI')
        self.ws.close()
        self.ws = None
        # Ctrl+D asks to stop an active task. Terminate this TUI instead;
        # the independent Web subscriber must keep the task/backend alive.
        desktop.p.send_signal(signal.SIGTERM)
        self.wait(lambda: desktop.p.poll() is not None, 'desktop TUI exits during active shared run')
        self.wait(lambda: self.record()['PID'] != initial['PID'], 'Web actual TUI elected after desktop exit')
        leader = self.record()['PID']
        assert leader not in (self.web.pid, desktop.p.pid), 'host or HTTP process became coordinator'
        child_cmd = pathlib.Path(f'/proc/{leader}/cmdline').read_bytes().split(b'\0')
        assert b'--internal-tui' in child_cmd, 'elected member is not a real Web-derived TUI'
        child_stat = pathlib.Path(f'/proc/{leader}/stat').read_text().rsplit(')', 1)[1].split()
        assert int(child_stat[1]) == self.web.pid, 'elected TUI is not the Web service child'
        assert shared.identity(leader) and shared.identity(pid)
        assert self.assert_one_backend() == pid
        assert len(self.commands('prompt')) == 2, 'election automatically replayed prompt'
        self.checkpoint('WS disconnect preserves active run; Web-derived actual TUI wins election without Pi restart/replay')

        self.control(pid, 'finish')
        self.open_ws()
        self.wait(lambda: 'RESULT-web-turn' in self.ws.text and 'Esc interrupt' not in self.ws.text,
                  'reconnected WS sees settled result after disconnected completion')
        assert self.assert_one_backend() == pid
        desktop2 = self.add('desktop-survivor')
        self.wait(lambda: 'RESULT-web-turn' in desktop2.text, 'new desktop attached original backend')
        self.checkpoint('completion while WS absent is preserved and later desktop joins original Pi')
        self.stop_web()
        self.wait(lambda: not shared.identity(leader), 'Web-derived TUI cleaned up with Web service')
        self.wait(lambda: self.record()['PID'] == desktop2.p.pid, 'desktop takes over after Web service exit')
        assert shared.identity(pid), 'Web shutdown killed backend still subscribed by desktop'
        assert self.assert_one_backend() == pid
        desktop2.send('after-web-exit\r')
        self.wait(lambda: len(self.commands('prompt')) == 3, 'surviving desktop can submit')
        self.control(pid, 'finish')
        self.wait(lambda: 'RESULT-after-web-exit' in desktop2.text and 'Esc interrupt' not in desktop2.text,
                  'desktop receives response after Web shutdown')
        assert [x['message'] for x in self.commands('prompt')] == ['desktop-active', 'web-turn', 'after-web-exit']
        self.checkpoint('Web shutdown cleans own TUI but leaves shared Pi and surviving desktop operational')
        desktop2.quit()
        self.wait(lambda: not shared.identity(pid), 'last TUI exit reclaims Pi', timeout=12)
        self.wait(lambda: any(x['kind'] == 'stop' for x in self.wire()), 'fake RPC graceful EOF')
        self.checkpoint('last subscriber departure reclaims shared backend')


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
        print(json.dumps({'result': 'passed', 'checks': suite.results, 'isolated_root': str(root)}))
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
