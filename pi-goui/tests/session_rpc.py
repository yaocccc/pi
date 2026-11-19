#!/usr/bin/env python3
"""Offline JSONL Pi fixture for script/test-session-ownership.py; no model/tool calls."""
import json
import os
import pathlib
import sys
import threading
import time

ROOT = pathlib.Path(os.environ['GOUI_SESSION_TEST_ROOT'])
SESSION = pathlib.Path(sys.argv[sys.argv.index('--session') + 1])
PID = os.getpid()
LOCK = threading.RLock()
STOP = threading.Event()
BUSY = False
PENDING = None
CURRENT = None
KEEP_ALIVE = False
# Opt-in synthetic extension for the instance Mode scenario only.
MODE_PATH = ROOT / 'agent' / 'codex-fast.json'
MODE = json.loads(MODE_PATH.read_text())['level'] if MODE_PATH.exists() else None
MODEL = {'id': 'session-fixture', 'name': 'Session fixture', 'provider': 'test', 'contextWindow': 200000}
ENTRIES = [json.loads(s) for s in SESSION.read_text().splitlines() if s.strip()] if SESSION.exists() else []
ENTRIES = [e for e in ENTRIES if e.get('type') != 'session']


def log(kind, **fields):
    with LOCK, (ROOT / 'wire.jsonl').open('a') as out:
        out.write(json.dumps({'kind': kind, 'pid': PID, **fields}) + '\n')


def emit(event):
    with LOCK:
        print(json.dumps(event), flush=True)


def message(role, text):
    return {'role': role, 'content': [{'type': 'text', 'text': text}], 'timestamp': int(time.time() * 1000),
            'provider': 'test', 'model': MODEL['id'], 'stopReason': 'stop',
            'usage': {'input': 10, 'output': 5, 'totalTokens': 15, 'cost': {'total': 0}}}


def append_message(msg):
    entry = {'type': 'message', 'id': str(len(ENTRIES) + 1),
             'parentId': ENTRIES[-1]['id'] if ENTRIES else None, 'message': msg}
    ENTRIES.append(entry)
    SESSION.parent.mkdir(parents=True, exist_ok=True)
    if not SESSION.exists() or SESSION.stat().st_size == 0:
        SESSION.write_text(json.dumps({'type': 'session', 'version': 3, 'id': 'ownership-e2e',
                                       'timestamp': '2026-01-01T00:00:00Z', 'cwd': str(ROOT / 'cwd')}) + '\n')
    with SESSION.open('a') as out:
        out.write(json.dumps(entry) + '\n')


def complete():
    global BUSY, PENDING, CURRENT
    if PENDING is None:
        return
    request = PENDING
    text = 'RESULT-' + request['message']
    # A native assistant keeps its timestamp/identity from start to final.
    msg = dict(CURRENT)
    msg['content'] = [{'type': 'text', 'text': text}]
    append_message(msg)
    emit({'type': 'message_end', 'message': msg})
    BUSY = False
    emit({'type': 'agent_end', 'messages': [msg]})
    emit({'type': 'agent_settled'})
    emit({'type': 'response', 'command': 'prompt', 'id': request['id'], 'success': True})
    PENDING = CURRENT = None


def advance_progress():
    """Two completed turns plus a live third turn: 51k input, 665 output."""
    global CURRENT
    assert PENDING is not None and CURRENT is not None
    for turn, output in enumerate((200, 400)):
        CURRENT['content'] = [{'type': 'text', 'text': f'TURN-{turn + 1}-' + PENDING['message']}]
        CURRENT['usage'] = {'input': 51000, 'output': output, 'totalTokens': 51000 + output}
        append_message(CURRENT)
        emit({'type': 'message_end', 'message': CURRENT})
        emit({'type': 'turn_start', 'turnIndex': turn + 1})
        CURRENT = message('assistant', 'STREAM-' + PENDING['message'])
        CURRENT['usage'] = {'input': 51000, 'output': 65 if turn == 1 else 0}
        emit({'type': 'message_start', 'message': CURRENT})
    log('progress')


def control():
    global KEEP_ALIVE
    path = ROOT / 'control.jsonl'
    offset = 0
    while not STOP.wait(.025):
        if not path.exists():
            continue
        with path.open() as source:
            source.seek(offset)
            lines = source.readlines()
            offset = source.tell()
        for line in lines:
            cmd = json.loads(line)
            if cmd.get('pid') != PID:
                continue
            with LOCK:
                if cmd['type'] == 'finish':
                    complete()
                elif cmd['type'] == 'progress':
                    advance_progress()
                elif cmd['type'] == 'event':
                    emit(cmd['event'])
                elif cmd['type'] == 'keep_alive_after_eof':
                    KEEP_ALIVE = True
                    log('keep_alive')


log('loaded', texts=[part.get('text', '') for e in ENTRIES for part in e.get('message', {}).get('content', []) if isinstance(part, dict)])
log('start', session=str(SESSION), birth=pathlib.Path('/proc/self/stat').read_text().rsplit(')', 1)[1].split()[19])
threading.Thread(target=control, daemon=True).start()
try:
    for line in sys.stdin:
        req = json.loads(line)
        with LOCK:
            log('command', command=req)
            typ = req['type']
            data = {}
            if typ == 'get_state':
                data = {'sessionId': 'ownership-e2e', 'sessionFile': str(SESSION), 'sessionName': 'Ownership acceptance',
                        'model': MODEL, 'thinkingLevel': 'medium', 'isStreaming': BUSY,
                        'isCompacting': False, 'pendingMessageCount': 0,
                        'steeringMessages': [], 'followUpMessages': [], 'messageCount': len(ENTRIES)}
            elif typ == 'get_entries':
                data = {'entries': ENTRIES, 'leafId': ENTRIES[-1]['id'] if ENTRIES else None}
            elif typ == 'get_messages':
                data = {'messages': [e['message'] for e in ENTRIES if e['type'] == 'message']}
            elif typ == 'get_session_stats':
                data = {'sessionId': 'ownership-e2e', 'sessionFile': str(SESSION), 'totalMessages': len(ENTRIES),
                        'tokens': {'input': 10, 'output': 5, 'total': 15}, 'cost': 0}
            elif typ == 'get_available_models':
                data = {'models': [MODEL]}
            elif typ == 'get_commands':
                data = {'commands': [{'name': 'pi-ui-rpc', 'source': 'extension'}] if MODE else []}
            elif MODE and typ == 'prompt' and req['message'].startswith('/pi-ui-rpc '):
                call = json.loads(req['message'].split(' ', 1)[1])
                method = call['method']
                if method == 'session.activity':
                    result = {'version': 1, 'idle': not BUSY, 'reasons': ['busy'] if BUSY else []}
                else:
                    assert method in ('codex-fast.get', 'codex-fast.apply', 'codex-fast.set'), method
                    if method != 'codex-fast.get':
                        MODE = call['params']['level']
                        if method == 'codex-fast.set':
                            MODE_PATH.write_text(json.dumps({'level': MODE}))
                    result = {'level': MODE, 'levels': ['normal', 'fast', 'ultrafast']}
                emit({'type': 'extension_ui_request', 'method': 'notify',
                      'message': 'pi-ui:rpc:v1:' + json.dumps({'id': call['id'], 'result': result})})
            elif typ == 'extension_ui_response':
                continue
            elif typ == 'prompt':
                assert PENDING is None, 'test sent overlapping prompts'
                BUSY = True
                PENDING = req
                if MODE:
                    log('mode_at_prompt', level=MODE)
                emit({'type': 'agent_start'})
                msg = message('user', req['message'])
                append_message(msg)
                emit({'type': 'message_end', 'message': msg})
                partial = CURRENT = message('assistant', 'STREAM-' + req['message'])
                emit({'type': 'message_start', 'message': partial})
                emit({'type': 'message_update', 'message': partial,
                      'assistantMessageEvent': {'type': 'text_delta', 'contentIndex': 0, 'delta': 'STREAM-' + req['message']}})
                continue
            elif typ == 'abort':
                complete()
            emit({'type': 'response', 'command': typ, 'id': req.get('id'), 'success': True, 'data': data})
finally:
    if KEEP_ALIVE:
        log('orphan')
        while not STOP.wait(.1):
            pass
    STOP.set()
    log('stop')
