#!/usr/bin/env python3
"""Read-only RPC proxy: no prompt, compaction, shell, or tool commands can pass."""
import json
import os
import subprocess
import sys
import threading

ALLOWED = frozenset({'get_state', 'get_entries', 'get_session_stats', 'get_commands',
                     'get_available_models', 'get_available_thinking_levels'})
p = subprocess.Popen([os.environ['GOUI_REAL_PI'], '--mode', 'rpc', '--no-session', '--no-extensions'],
                     stdin=subprocess.PIPE, stdout=subprocess.PIPE)

def output():
    for line in p.stdout:
        sys.stdout.buffer.write(line)
        sys.stdout.buffer.flush()

thread = threading.Thread(target=output, daemon=True)
thread.start()
try:
    for line in sys.stdin.buffer:
        command = json.loads(line)
        allowed = command.get('type') in ALLOWED
        with open(os.environ['GOUI_REAL_LOG'], 'a') as log:
            log.write(json.dumps({'allowed': allowed, 'command': command}) + '\n')
        if not allowed:
            print(json.dumps({'type': 'response', 'id': command.get('id'), 'command': command.get('type'),
                              'success': False, 'error': 'Acceptance test forbids this command'}), flush=True)
            continue
        p.stdin.write(line)
        p.stdin.flush()
finally:
    p.stdin.close()
    try:
        p.wait(timeout=5)
    except subprocess.TimeoutExpired:
        p.kill()
        p.wait()
    thread.join(timeout=2)
