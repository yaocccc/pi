#!/usr/bin/env python3
"""Offline native-RPC fixture for test-resume.py; gates only saved get_state.

All state, process identities and wire records belong to GOUI_SHARED_TEST_ROOT.
No network, credentials, real Pi, tools or model calls are used.
"""
import json
import os
import pathlib
import sys
import time

ROOT = pathlib.Path(os.environ['GOUI_SHARED_TEST_ROOT'])
PID = os.getpid()
SAVED = '--session' in sys.argv
SESSION = (pathlib.Path(sys.argv[sys.argv.index('--session') + 1]) if SAVED else
           ROOT / 'agent/sessions/project' / ('unwritten-%s.jsonl' % PID))
HEADER = {'type': 'session', 'version': 3, 'id': 'resume-saved' if SAVED else 'blank-%s' % PID,
          'timestamp': '2026-01-01T00:00:00Z', 'cwd': str(ROOT / 'cwd')}
ENTRIES = []
if SESSION.exists():
    records = [json.loads(s) for s in SESSION.read_text().splitlines() if s.strip()]
    HEADER = records[0]
    ENTRIES = records[1:]
MODEL = {'id': 'resume-fixture', 'name': 'Resume fixture', 'provider': 'test', 'contextWindow': 200000}


def log(kind, **fields):
    with (ROOT / 'wire.jsonl').open('a') as out:
        out.write(json.dumps({'kind': kind, 'pid': PID, 'saved': SAVED, 'time': time.monotonic(), **fields}) + '\n')


def emit(event):
    print(json.dumps(event), flush=True)


def message(role, text):
    return {'role': role, 'content': [{'type': 'text', 'text': text}], 'timestamp': int(time.time() * 1000),
            'provider': 'test', 'model': MODEL['id'], 'stopReason': 'stop',
            'usage': {'input': 1, 'output': 1, 'totalTokens': 2, 'cost': {'total': 0}}}


def append_message(msg):
    entry = {'type': 'message', 'id': 'msg-%d' % len(ENTRIES),
             'parentId': ENTRIES[-1]['id'] if ENTRIES else None, 'message': msg}
    ENTRIES.append(entry)
    if not SESSION.exists():
        SESSION.parent.mkdir(parents=True, exist_ok=True)
        SESSION.write_text(json.dumps(HEADER) + '\n')
    with SESSION.open('a') as out:
        out.write(json.dumps(entry) + '\n')


log('start', birth=pathlib.Path('/proc/self/stat').read_text().rsplit(')', 1)[1].split()[19], session=str(SESSION))
try:
    for line in sys.stdin:
        req = json.loads(line)
        typ = req['type']
        log('command', command=req)
        data = {}
        if typ == 'get_state':
            if SAVED and not (ROOT / 'release-state').exists():
                log('gate')
                deadline = time.monotonic() + 40
                while not (ROOT / 'release-state').exists():
                    if time.monotonic() > deadline:
                        raise RuntimeError('test did not release saved get_state')
                    time.sleep(.02)
                log('released')
            if SAVED and (ROOT / 'fail-state').exists():
                log('deliberate-failure')
                sys.exit(17)
            data = {'sessionId': HEADER['id'], 'sessionFile': str(SESSION),
                    'sessionName': 'Resume acceptance' if SAVED else 'Blank acceptance',
                    'model': MODEL, 'thinkingLevel': 'medium', 'isStreaming': False,
                    'steeringMessages': [], 'followUpMessages': [],
                    'messageCount': sum(e['type'] == 'message' for e in ENTRIES)}
        elif typ == 'get_entries':
            data = {'entries': ENTRIES, 'leafId': ENTRIES[-1]['id'] if ENTRIES else None}
        elif typ == 'get_messages':
            data = {'messages': [e['message'] for e in ENTRIES if e['type'] == 'message']}
        elif typ == 'get_session_stats':
            data = {'sessionId': HEADER['id'], 'sessionFile': str(SESSION), 'totalMessages': len(ENTRIES),
                    'tokens': {'input': 1, 'output': 1, 'total': 2}, 'cost': 0}
        elif typ == 'get_commands':
            data = {'commands': []}
        elif typ == 'get_available_models':
            data = {'models': [MODEL]}
        elif typ == 'prompt':
            emit({'type': 'agent_start'})
            user = message('user', req['message'])
            append_message(user)
            emit({'type': 'message_end', 'message': user})
            assistant = message('assistant', 'RESULT-' + req['message'])
            append_message(assistant)
            emit({'type': 'message_start', 'message': assistant})
            emit({'type': 'message_end', 'message': assistant})
            emit({'type': 'agent_end', 'messages': [assistant]})
            emit({'type': 'agent_settled'})
        elif typ == 'extension_ui_response':
            continue
        emit({'type': 'response', 'command': typ, 'id': req.get('id'), 'success': True, 'data': data})
finally:
    log('stop')
