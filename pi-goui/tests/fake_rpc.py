#!/usr/bin/env python3
"""Deterministic pi JSONL fixture. No imports/calls to pi, models or tools."""
import json
import os
import pathlib
import sys
import threading
import time

ROOT = pathlib.Path(os.environ['GOUI_TEST_DIR'])
PID = os.getpid()
LOCK = threading.Lock()
STOP = threading.Event()
THINKING = 'medium'
BUSY = False
MODEL = {'id': 'fixture-model', 'name': 'Fixture model', 'provider': 'test', 'contextWindow': 200000}
ENTRIES = []
STATE_LOCK = threading.RLock()
SESSION_FILE = ROOT / f'{PID}.jsonl'
SESSION_ID = str(PID)
if '--session' in sys.argv:
    SESSION_FILE = pathlib.Path(sys.argv[sys.argv.index('--session') + 1]).resolve()
    if not SESSION_FILE.is_relative_to(ROOT.resolve()):
        raise ValueError('fixture sessions must stay inside GOUI_TEST_DIR')


def persist(item):
    with SESSION_FILE.open('a', encoding='utf-8') as target:
        target.write(json.dumps(item) + '\n')


def log(kind, **data):
    record = json.dumps(dict(kind=kind, pid=PID, **data)) + '\n'
    fd = os.open(ROOT / 'wire.jsonl', os.O_APPEND | os.O_CREAT | os.O_WRONLY, 0o600)
    try:
        os.write(fd, record.encode())
    finally:
        os.close(fd)


def emit(event):
    with LOCK:
        print(json.dumps(event), flush=True)
    log('event', event=event)


def entry(role, content, **extra):
    with STATE_LOCK:
        used = {item['id'] for item in ENTRIES}
        index = len(ENTRIES)
        while f'e{index}' in used:
            index += 1
        item = {'type': 'message', 'id': f'e{index}', 'parentId': ENTRIES[-1]['id'] if ENTRIES else None,
                'message': {'role': role, 'content': content, 'timestamp': index + 1, **extra}}
        persist(item)
        ENTRIES.append(item)
        return item


def initial_history():
    entry('user', [{'type': 'text', 'text': 'PRECOMPACTION_USER_MARKER'}])
    entry('assistant', [{'type': 'thinking', 'thinking': 'THINKING_VISIBLE_MARKER'},
                        {'type': 'text', 'text': 'PRECOMPACTION_ASSISTANT_MARKER'},
                        {'type': 'toolCall', 'id': 'tool-one', 'name': 'read', 'arguments': {'path': 'fixture.txt'}},
                        {'type': 'toolCall', 'id': 'tool-two', 'name': 'bash', 'arguments': {'command': 'fixture only'}}], stopReason='toolUse')
    entry('toolResult', [{'type': 'text', 'text': 'Tool preview\nTOOL_DEEP_LINE\nCOPY_ME 中文\nLast tool line'}], toolCallId='tool-one', toolName='read')
    entry('toolResult', [{'type': 'text', 'text': 'SECOND_TOOL_OUTPUT'}], toolCallId='tool-two', toolName='bash')
    compact = {'type': 'compaction', 'id': 'compact', 'parentId': ENTRIES[-1]['id'], 'summary': 'COMPACTION_BOUNDARY\n' + 'HIDDEN_COMPACTION_BODY\n' * 150, 'firstKeptEntryId': 'e1', 'tokensBefore': 1000}
    persist(compact)
    ENTRIES.append(compact)
    entry('assistant', [{'type': 'text', 'text': '```text\n' + '\n'.join(f'SCROLL_LINE_{i}' for i in range(40)) + '\n```\n\nPOSTCOMPACTION_MARKER'}], stopReason='stop')


if SESSION_FILE.exists():
    records = [json.loads(line) for line in SESSION_FILE.read_text().splitlines() if line.strip()]
    header, *ENTRIES = records
    SESSION_ID = header['id']
else:
    SESSION_FILE.parent.mkdir(parents=True, exist_ok=True)
    persist({'type': 'session', 'version': 3, 'id': SESSION_ID, 'cwd': os.getcwd(),
             'timestamp': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())})
    initial_history()


def controls():
    global BUSY
    path = ROOT / f'control-{PID}.jsonl'
    path.touch()
    with path.open() as source:
        while not STOP.is_set():
            line = source.readline()
            if not line:
                STOP.wait(.02)
                continue
            action = json.loads(line)
            if action.get('type') == 'agent_start':
                BUSY = True
            elif action.get('type') == 'agent_settled':
                BUSY = False
            elif action.get('type') == 'message_end':
                message = dict(action['message'])
                if message['role'] == 'assistant':
                    message.setdefault('stopReason', 'stop')
                action['message'] = message
                entry(message['role'], message['content'], **{key: value for key, value in message.items() if key not in ('role', 'content')})
            emit(action)


log('start', argv=sys.argv[1:], cwd=os.getcwd(), home=os.environ['HOME'])
threading.Thread(target=controls, daemon=True).start()
try:
    for line in sys.stdin:
        c = json.loads(line)
        log('command', command=c)
        typ = c['type']
        data = {}
        if typ == 'extension_ui_response':
            continue
        if typ == 'get_state':
            data = {'model': MODEL, 'thinkingLevel': THINKING, 'sessionId': SESSION_ID, 'sessionName': f'Fixture-{PID}', 'isStreaming': BUSY, 'isCompacting': False, 'sessionFile': str(SESSION_FILE)}
        elif typ == 'get_entries':
            with STATE_LOCK:
                data = {'entries': list(ENTRIES), 'leafId': ENTRIES[-1]['id'] if ENTRIES else None}
        elif typ == 'get_session_stats':
            data = {'tokens': {'input': 120, 'output': 80, 'total': 200}, 'cost': .125, 'contextUsage': {'tokens': 200, 'percent': .1, 'contextWindow': 200000}}
        elif typ == 'get_commands':
            data = {'commands': []}
        elif typ == 'get_available_models':
            data = {'models': [MODEL, dict(MODEL, id='fixture-second')]}
        elif typ == 'get_available_thinking_levels':
            data = {'levels': ['off', 'low', 'medium', 'high']}
        elif typ == 'set_thinking_level':
            THINKING = c['level']
            data = {'level': THINKING}
        elif typ == 'cycle_thinking_level':
            THINKING = 'high' if THINKING == 'medium' else 'medium'
            data = {'level': THINKING}
        elif typ == 'set_model':
            MODEL = dict(MODEL, id=c['modelId'])
        elif typ == 'cycle_model':
            MODEL = dict(MODEL, id='fixture-second' if MODEL['id'] == 'fixture-model' else 'fixture-model')
            data = {'model': MODEL, 'thinkingLevel': THINKING}
        elif typ == 'prompt':
            # Persist and publish a complete synthetic turn; never execute text.
            BUSY = True
            content = [{'type': 'text', 'text': c['message']}] + c.get('images', [])
            user = entry('user', content)
            emit({'type': 'agent_start'})
            emit({'type': 'message_end', 'message': user['message']})
            assistant = entry('assistant', [{'type': 'text', 'text': 'Fixture prompt completed'}], stopReason='stop')
            emit({'type': 'message_end', 'message': assistant['message']})
            BUSY = False
            emit({'type': 'agent_settled'})
            data = {'disposition': 'handled'}
        elif typ == 'abort':
            BUSY = False
            emit({'type': 'agent_settled'})
        elif typ == 'clear_queue':
            data = {'steering': [], 'followUp': []}
        else:
            emit({'type': 'response', 'id': c.get('id'), 'command': typ, 'success': False, 'error': 'Unsupported fake command: ' + typ})
            continue
        emit({'type': 'response', 'id': c.get('id'), 'command': typ, 'success': True, 'data': data})
finally:
    STOP.set()
    log('exit')
