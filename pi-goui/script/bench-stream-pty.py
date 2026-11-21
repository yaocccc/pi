#!/usr/bin/env python3
"""Compare visible response over a real Linux PTY with an offline fake Pi.

Run: python3 script/bench-stream-pty.py BASELINE_BINARY CURRENT_BINARY
Reports nearest-rank p95. Resize first paint retains the old readable layout;
resize reflow waits for a transcript border at the new width.
"""
import argparse
import fcntl
import importlib.util
import json
import math
import os
import pathlib
import re
import signal
import statistics
import struct
import sys
import tempfile
import termios
import threading
import time

sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location(
    'ownership_fixture', pathlib.Path(__file__).with_name('test-session-ownership.py'))
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)

NUMBER = re.compile(r'section-(\d{4})')
BORDER = re.compile(r'╰─+╯')
WHEEL_UP = b'\x1b[<64;12;12M'
BOTTOM = b'\x1b[1;5F'
PAUSE = .125
WIDTHS = (150, 118)
HEIGHT = 42


def resize(term, width):
    term.screen.resize(width, HEIGHT)
    fcntl.ioctl(term.slave, termios.TIOCSWINSZ, struct.pack('HHHH', HEIGHT, width, 0, 0))
    os.kill(term.p.pid, signal.SIGWINCH)


def border(text):
    # At the tail of the reply the assistant frame is the last such border.
    hits = BORDER.findall(text)
    return len(hits[-1]) if hits else 0


def first_section(text):
    match = NUMBER.search(text)
    return int(match[1]) if match else None


def measure(suite, action, *conditions, timeout=8):
    start = time.perf_counter_ns()
    action()
    measurements = [None] * len(conditions)
    while (time.perf_counter_ns() - start) / 1e9 < timeout:
        suite.pump(.001)
        now = time.perf_counter_ns()
        for i, condition in enumerate(conditions):
            if measurements[i] is None and condition():
                measurements[i] = (now - start) / 1e6
        if None not in measurements:
            return measurements
    raise AssertionError(f'visible response timeout; observed (ms): {measurements}\n{suite.terminals[0].text}')


def trial(binary, mode, samples):
    with tempfile.TemporaryDirectory(prefix='goui-bench-stream-') as tmp:
        suite = fixture.Suite(pathlib.Path(tmp), binary)
        stop = threading.Event()
        stream = None
        try:
            term = fixture.Terminal(suite, mode)
            term.visible('SAVED-SESSION')
            term.send('pty-latency\r')
            term.visible('STREAM-pty-latency')
            prompts = [r for r in suite.wire('command') if r['command']['type'] == 'prompt']
            assert len(prompts) == 1 and prompts[0]['command']['message'] == 'pty-latency'
            pid = prompts[0]['pid']
            body = 'STREAM-pty-latency\n\n' + ''.join(
                f'section-{i:04} 中文 🧪 **Markdown** and `inline code`.\n\n' for i in range(1400))
            body += '```go\nfunc sample() {}\n```\n\n'
            body += '| left | right |\n| --- | --- |\n| 中文 | 🧪 |\n\nTAILREADY'
            current = {'role': 'assistant', 'timestamp': 100,
                       'content': [{'type': 'text', 'text': body}]}
            suite.control(pid, 'event', event={'type': 'message_update', 'message': current})
            term.visible('TAILREADY')
            lengths = {WIDTHS[0]: border(term.text)}
            assert lengths[WIDTHS[0]] > 0, 'assistant frame not visible at the tail'
            measure(suite, lambda: resize(term, WIDTHS[1]),
                    lambda: 'TAILREADY' in term.text and 0 < border(term.text) < lengths[WIDTHS[0]])
            lengths[WIDTHS[1]] = border(term.text)
            assert lengths[WIDTHS[1]] != lengths[WIDTHS[0]]

            state = {'body': body, 'updates': 0}

            def update(delta):
                state['body'] += delta
                event = {'type': 'message_update', 'assistantMessageEvent': {
                    'type': 'text_delta', 'contentIndex': 0, 'delta': delta}}
                if mode == 'snapshot+delta':
                    event['message'] = dict(current, content=[{'type': 'text', 'text': state['body']}])
                suite.control(pid, 'event', event=event)
                state['updates'] += 1

            def replay():
                while not stop.wait(PAUSE):
                    update('.')

            stream = threading.Thread(target=replay, daemon=True)
            stream.start()
            term.visible('TAILREADY.')
            data = {phase: [] for phase in ('input', 'wheel', 'resize+input', 'resize-reflow')}
            for i in range(samples):
                key = f'input{i:02}'
                data['input'].extend(measure(suite, lambda: os.write(term.master, b'\x15' + key.encode()),
                                             lambda: key in term.editor))
                before = first_section(term.text)
                assert before is not None, 'numbered paragraphs not visible at the tail'

                def scrolled_up():
                    section = first_section(term.text)
                    return section is not None and section < before

                data['wheel'].extend(measure(suite, lambda: os.write(term.master, WHEEL_UP), scrolled_up))
                measure(suite, lambda: os.write(term.master, BOTTOM), lambda: 'TAILREADY' in term.text)
                width = WIDTHS[i % 2]
                key = f'resize{i:02}'

                def resize_with_input():
                    resize(term, width)
                    os.write(term.master, b'\x15' + key.encode())

                first, reflow = measure(suite, resize_with_input, lambda: key in term.editor,
                                        lambda: border(term.text) == lengths[width] and 'TAILREADY' in term.text)
                data['resize+input'].append(first)
                data['resize-reflow'].append(reflow)
                suite.pump(.07)

            stop.set()
            stream.join()
            assert state['updates'] > 1, 'replay stopped before timing completed'
            update('\n\nENDOFREPLAY')
            term.visible('ENDOFREPLAY')
            suite.control(pid, 'finish')
            term.visible('RESULT-pty-latency')
            return {'mode': mode, 'samples': samples, 'update_interval_ms': PAUSE * 1000,
                    'replayed_updates': state['updates'], 'markdown_bytes': len(body.encode()),
                    'ms': {name: {'p50': round(statistics.median(values), 2),
                                  'p95': round(sorted(values)[math.ceil(.95 * samples) - 1], 2)}
                           for name, values in data.items()}}
        finally:
            stop.set()
            if stream is not None:
                stream.join()
            suite.cleanup()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binaries', type=lambda p: pathlib.Path(p).resolve(), nargs=2)
    parser.add_argument('--samples', type=int, default=12)
    args = parser.parse_args()
    if not 1 <= args.samples <= 32:
        parser.error('--samples must be between 1 and 32')
    if not all(path.is_file() and os.access(path, os.X_OK) for path in args.binaries):
        parser.error('both binaries must be executable files')
    for mode in ('snapshot+delta', 'delta'):
        for version, binary in zip(('before', 'after'), args.binaries, strict=True):
            print(json.dumps({'version': version, **trial(binary, mode, args.samples)}), flush=True)


if __name__ == '__main__':
    main()
