#!/usr/bin/env python3
"""Real PTY peer for tests. No Pi, provider, tools, shell or network."""
import os
import signal
import sys
import tty

log_path = os.environ['WEBUI_TEST_LOG']

def log(message):
    with open(log_path, 'a') as f:
        f.write(message + '\n')

def emit(message):
    os.write(1, message.encode())

def size(*_):
    width, height = os.get_terminal_size(0)
    emit(f'\r\nSIZE:{width}:{height}\r\n')

def stop(*_):
    log(f'STOP:{os.getpid()}')
    sys.exit(0)

# Model RPC and tool processes that leave the TUI's process group. They ignore
# both graceful termination and the controlling terminal's hangup.
if '--descendants' in sys.argv:
    ready, notify = os.pipe()
    child = os.fork()
    if child == 0:
        os.close(ready)
        os.setpgid(0, 0)
        signal.signal(signal.SIGTERM, signal.SIG_IGN)
        signal.signal(signal.SIGHUP, signal.SIG_IGN)
        signal.signal(signal.SIGCHLD, signal.SIG_IGN)
        log(f'CHILD:{os.getpid()}')
        if os.fork() == 0:
            os.setpgid(0, 0)
            log(f'GRANDCHILD:{os.getpid()}')
        os.write(notify, b'!')
        os.close(notify)
        while True:
            signal.pause()
    os.close(notify)
    while os.read(ready, 2):
        pass
    os.close(ready)
    signal.signal(signal.SIGCHLD, signal.SIG_IGN)

assert os.isatty(0) and os.isatty(1) and os.isatty(2)
tty.setraw(0)
signal.signal(signal.SIGWINCH, size)
signal.signal(signal.SIGTERM, signal.SIG_IGN if '--ignore-term' in sys.argv else stop)
log(f'START:{os.getpid()}')
emit(f'PID:{os.getpid()} TERM:{os.environ.get("TERM")} COLOR:{os.environ.get("COLORTERM")}\r\n')
size()
if '--exit-immediate' in sys.argv:
    sys.exit(7)
if '--no-read' in sys.argv:
    while True:
        signal.pause()
while True:
    data = os.read(0, 65536)
    if not data:
        break
    emit('INPUT:' + data.hex() + '\r\n')
    if data == b'quit':
        log(f'QUIT:{os.getpid()}')
        emit('FINAL OUTPUT\r\n')
        sys.exit(7)
    if data == b'size':
        size()
