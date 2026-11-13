"""Isolated protocol/history regression tests for the PTY fixture."""
import json
import os
import pathlib
import queue
import subprocess
import sys
import tempfile
import threading
import time
import unittest


FIXTURE = pathlib.Path(__file__).with_name('fake_rpc.py')


class Fixture:
    def __init__(self, root, session=None):
        self.root = root
        env = dict(os.environ, GOUI_TEST_DIR=str(root), HOME=str(root),
                   PI_CODING_AGENT_DIR=str(root / 'agent'))
        args = [sys.executable, str(FIXTURE)]
        if session is not None:
            args += ['--session', str(session)]
        self.process = subprocess.Popen(args, cwd=root, env=env, stdin=subprocess.PIPE,
                                        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.messages = queue.Queue()
        self.events = []
        self.sequence = 0
        self.reader = threading.Thread(target=self.read, daemon=True)
        self.reader.start()

    def read(self):
        for line in self.process.stdout:
            self.messages.put(json.loads(line))

    def call(self, kind, **data):
        self.sequence += 1
        request = dict(data, type=kind, id=str(self.sequence))
        self.process.stdin.write(json.dumps(request) + '\n')
        self.process.stdin.flush()
        while True:
            message = self.messages.get(timeout=5)
            if message.get('type') == 'response' and message.get('id') == request['id']:
                if not message['success']:
                    raise AssertionError(message)
                return message['data']
            self.events.append(message)

    def control(self, **event):
        with (self.root / f'control-{self.process.pid}.jsonl').open('a') as target:
            target.write(json.dumps(event) + '\n')

    def close(self):
        self.process.stdin.close()
        try:
            self.process.wait(timeout=5)
        finally:
            if self.process.poll() is None:
                self.process.kill()
                self.process.wait()
            self.reader.join(timeout=2)
            self.process.stdout.close()
            self.process.stderr.close()


class FakeRPCTest(unittest.TestCase):
    def test_persisted_history_prompt_and_reopen(self):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            fixture = Fixture(root)
            try:
                state = fixture.call('get_state')
                path = pathlib.Path(state['sessionFile'])
                initial = fixture.call('get_entries')
                records = [json.loads(line) for line in path.read_text().splitlines()]
                self.assertEqual(records[0]['type'], 'session')
                self.assertEqual(records[0]['version'], 3)
                self.assertEqual(records[0]['id'], state['sessionId'])
                self.assertEqual(records[1:], initial['entries'])
                self.assertEqual(records[-1]['message']['stopReason'], 'stop')
                parent = None
                for record in records[1:]:
                    self.assertEqual(record['parentId'], parent)
                    parent = record['id']
                image = dict(type='image', mimeType='image/png', data='fixture-bytes')
                fixture.call('prompt', message='synthetic request', images=[image])
                saved = fixture.call('get_entries')
                self.assertEqual(saved['entries'][-2]['message']['content'][1], image)
                self.assertEqual(saved['entries'][-1]['message']['stopReason'], 'stop')
                self.assertEqual([json.loads(line) for line in path.read_text().splitlines()][1:], saved['entries'])
                self.assertFalse(fixture.call('get_state')['isStreaming'])
                events = [event['type'] for event in fixture.events]
                self.assertLess(events.index('agent_start'), events.index('agent_settled'))
            finally:
                fixture.close()
            reopened = Fixture(root, path)
            try:
                self.assertEqual(reopened.call('get_state')['sessionId'], state['sessionId'])
                self.assertEqual(reopened.call('get_entries'), saved)
            finally:
                reopened.close()

    def test_controlled_unfinished_and_completed_turn(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = Fixture(pathlib.Path(directory))
            try:
                path = pathlib.Path(fixture.call('get_state')['sessionFile'])
                fixture.control(type='message_end', message=dict(role='user', content='controlled request'))
                fixture.control(type='agent_start')
                deadline = time.monotonic() + 5
                while not fixture.call('get_state')['isStreaming']:
                    self.assertLess(time.monotonic(), deadline)
                    time.sleep(.02)
                records = [json.loads(line) for line in path.read_text().splitlines()]
                self.assertEqual(records[-1]['message']['role'], 'user')
                fixture.control(type='message_end', message=dict(role='assistant', content='controlled result'))
                fixture.control(type='agent_settled')
                while fixture.call('get_state')['isStreaming']:
                    self.assertLess(time.monotonic(), deadline)
                    time.sleep(.02)
                records = [json.loads(line) for line in path.read_text().splitlines()]
                self.assertEqual(records[-1]['message']['stopReason'], 'stop')
                self.assertEqual(records[1:], fixture.call('get_entries')['entries'])
            finally:
                fixture.close()


if __name__ == '__main__':
    unittest.main()
