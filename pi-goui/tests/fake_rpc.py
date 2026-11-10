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
    item = {'type': 'message', 'id': f'e{len(ENTRIES)}', 'parentId': ENTRIES[-1]['id'] if ENTRIES else None,
            'message': {'role': role, 'content': content, 'timestamp': len(ENTRIES) + 1, **extra}}
    ENTRIES.append(item)
    return item


entry('user', [{'type': 'text', 'text': 'PRECOMPACTION_USER_MARKER'}])
entry('assistant', [{'type': 'thinking', 'thinking': 'THINKING_VISIBLE_MARKER'},
                    {'type': 'text', 'text': 'PRECOMPACTION_ASSISTANT_MARKER'},
                    {'type': 'toolCall', 'id': 'tool-one', 'name': 'read', 'arguments': {'path': 'fixture.txt'}},
                    {'type': 'toolCall', 'id': 'tool-two', 'name': 'bash', 'arguments': {'command': 'fixture only'}}])
entry('toolResult', [{'type': 'text', 'text': 'Tool preview\nTOOL_DEEP_LINE\nCOPY_ME 中文\nLast tool line'}], toolCallId='tool-one', toolName='read')
entry('toolResult', [{'type': 'text', 'text': 'SECOND_TOOL_OUTPUT'}], toolCallId='tool-two', toolName='bash')
ENTRIES.append({'type': 'compaction', 'id': 'compact', 'parentId': ENTRIES[-1]['id'], 'summary': 'COMPACTION_BOUNDARY\n' + 'HIDDEN_COMPACTION_BODY\n' * 150, 'firstKeptEntryId': 'e1', 'tokensBefore': 1000})
entry('assistant', [{'type': 'text', 'text': '```text\n' + '\n'.join(f'SCROLL_LINE_{i}' for i in range(40)) + '\n```\n\nPOSTCOMPACTION_MARKER'}])


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
                message = action['message']
                entry(message['role'], message['content'], timestamp=message.get('timestamp', 9000))
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
            data = {'model': MODEL, 'thinkingLevel': THINKING, 'sessionId': str(PID), 'sessionName': f'Fixture-{PID}', 'isStreaming': BUSY, 'isCompacting': False, 'sessionFile': str(ROOT / f'{PID}.jsonl')}
        elif typ == 'get_entries':
            data = {'entries': list(ENTRIES), 'leafId': ENTRIES[-1]['id']}
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
            # Test-only synthetic acceptance; never executes the submitted text.
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
