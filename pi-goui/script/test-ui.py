#!/usr/bin/env python3
"""Linux PTY acceptance, standard library only; all prompts go to a fake process.

python3 pi-goui/script/test-ui.py [--binary PATH] [--real-pi /path/to/pi]
A binary is built under a temporary directory if --binary is omitted.
"""
import argparse
import base64
import codecs
import fcntl
import json
import os
import pathlib
import re
import select
import signal
import struct
import subprocess
import tempfile
import zlib
import termios
import time
import unicodedata

ROOT = pathlib.Path(__file__).resolve().parents[1]


class Screen:
    """Small VT screen sufficient for Bubble Tea cursor/erase/SGR output."""
    def __init__(self, width, height):
        self.resize(width, height)
        self.pending = ''
        self.decoder = codecs.getincrementaldecoder('utf-8')('replace')

    def resize(self, width, height):
        self.w, self.h = width, height
        self.cells = [[' '] * width for _ in range(height)]
        self.x = self.y = 0
        self.wrap = True
        self.wrap_pending = False
        self.scroll_top, self.scroll_bottom = 0, height - 1

    def linefeed(self):
        self.wrap_pending = False
        if self.y == self.scroll_bottom:
            self.cells.pop(self.scroll_top)
            self.cells.insert(self.scroll_bottom, [' '] * self.w)
        else:
            self.y = min(self.h - 1, self.y + 1)

    def feed(self, data):
        s = self.pending + self.decoder.decode(data)
        self.pending = ''
        i = 0
        while i < len(s):
            c = s[i]
            if c == '\x1b':
                if i + 1 >= len(s):
                    break
                if s[i + 1] == '[':
                    match = re.match(r'\x1b\[([0-?]*)([ -/]*)([@-~])', s[i:])
                    if not match:
                        break
                    params, _, final = match.groups()
                    if params == '?7' and final in 'hl':
                        self.wrap = final == 'h'
                        self.wrap_pending = False
                    if final in 'HfABCDGd': self.wrap_pending = False
                    if not params.startswith(('?', '>', '<', '=')):
                        nums = [int(n) if n else 0 for n in params.split(';')]
                        n = nums[0] or 1
                        if final in 'Hf':
                            self.y = min(self.h - 1, n - 1)
                            self.x = min(self.w - 1, (nums[1] or 1) - 1) if len(nums) > 1 else 0
                        elif final == 'A': self.y = max(0, self.y - n)
                        elif final == 'B': self.y = min(self.h - 1, self.y + n)
                        elif final == 'C': self.x = min(self.w - 1, self.x + n)
                        elif final == 'D': self.x = max(0, self.x - n)
                        elif final == 'G': self.x = min(self.w - 1, n - 1)
                        elif final == 'd': self.y = min(self.h - 1, n - 1)
                        elif final == 'r':
                            self.scroll_top = min(self.h - 1, n - 1)
                            self.scroll_bottom = min(self.h - 1, (nums[1] or self.h) - 1) if len(nums)>1 else self.h - 1
                            self.x = self.y = 0
                            self.wrap_pending = False
                        elif final in 'ST':
                            for _ in range(min(n, self.scroll_bottom-self.scroll_top+1)):
                                if final == 'S':
                                    self.cells.pop(self.scroll_top); self.cells.insert(self.scroll_bottom, [' '] * self.w)
                                else:
                                    self.cells.pop(self.scroll_bottom); self.cells.insert(self.scroll_top, [' '] * self.w)
                        elif final == 'J':
                            if nums[0] in (2, 3): self.cells = [[' '] * self.w for _ in range(self.h)]
                            elif nums[0] == 0:
                                self.cells[self.y][self.x:] = [' '] * (self.w - self.x)
                                for row in range(self.y + 1, self.h): self.cells[row] = [' '] * self.w
                        elif final == 'K':
                            a, b = (0, self.w) if nums[0] == 2 else ((0, self.x + 1) if nums[0] == 1 else (self.x, self.w))
                            self.cells[self.y][a:b] = [' '] * (b - a)
                        elif final == 'X': self.cells[self.y][self.x:min(self.w, self.x+n)] = [' '] * min(n, self.w-self.x)
                    i += match.end()
                    continue
                if s[i + 1] in ']P_':
                    end = re.search(r'\x07|\x1b\\', s[i + 2:])
                    if not end: break
                    i += 2 + end.end()
                    continue
                i += 2
                continue
            if c == '\r': self.x = 0; self.wrap_pending = False
            elif c == '\n': self.linefeed()
            elif c == '\b': self.x = max(0, self.x - 1); self.wrap_pending = False
            elif c == '\t': self.x = min(self.w-1, (self.x//8+1)*8); self.wrap_pending = False
            elif c >= ' ' and not unicodedata.combining(c):
                width = 2 if unicodedata.east_asian_width(c) in 'WF' else 1
                if self.wrap_pending and self.wrap: self.x = 0; self.linefeed()
                self.cells[self.y][self.x] = c
                if width == 2 and self.x + 1 < self.w: self.cells[self.y][self.x + 1] = ''
                end = self.x + width
                self.x = min(self.w-1, end)
                self.wrap_pending = end >= self.w and self.wrap
            i += 1
        self.pending = s[i:]

    @property
    def text(self):
        return '\n'.join(''.join(row) for row in self.cells)


def alive(pid):
    try: os.kill(pid, 0); return True
    except ProcessLookupError: return False


class Harness:
    def __init__(self, binary, root):
        self.root = root
        self.master, self.slave = os.openpty()
        self.original = termios.tcgetattr(self.slave)
        self.screen = Screen(132, 36)
        self.raw = bytearray()
        self.resize(132, 36)
        self.env = os.environ.copy()
        self.env.update(HOME=str(root / 'home'), PI_CODING_AGENT_DIR=str(root / 'agent'),
                        PI_BIN=str(ROOT / 'tests/fake_rpc.py'), GOUI_TEST_DIR=str(root),
                        TERM='xterm-256color', COLORTERM='truecolor')
        for name in ('home', 'agent', 'cwd'): (root / name).mkdir()
        (root / 'agent/keybindings.json').write_text('{"app.session.resume":["ctrl+y"]}')
        # Private fake clipboard/viewer: never contact the user's desktop.
        helpers=root/'helpers'; helpers.mkdir()
        def chunk(kind, data):
            return struct.pack('!I',len(data))+kind+data+struct.pack('!I',zlib.crc32(kind+data))
        png=b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('!IIBBBBB',1,1,8,2,0,0,0))+chunk(b'IDAT',zlib.compress(b'\x00\xff\x00\x00'))+chunk(b'IEND',b'')
        (root/'fixture.png').write_bytes(png)
        (helpers/'wl-paste').write_text('#!/usr/bin/env python3\nimport os,pathlib,sys\nsys.stdout.buffer.write((pathlib.Path(os.environ["GOUI_TEST_DIR"])/"fixture.png").read_bytes())\n')
        (helpers/'xdg-open').write_text('#!/usr/bin/env python3\nimport os,pathlib,sys,json,base64\np=pathlib.Path(sys.argv[1])\nwith (pathlib.Path(os.environ["GOUI_TEST_DIR"])/"opened-images.jsonl").open("a") as f: f.write(json.dumps({"path":str(p),"data":base64.b64encode(p.read_bytes()).decode()})+"\\n")\n')
        for helper in helpers.iterdir(): helper.chmod(0o755)
        self.env.update(PATH=str(helpers)+os.pathsep+self.env.get('PATH',''),WAYLAND_DISPLAY='fixture')
        self.p = subprocess.Popen([str(binary), '--cwd', str(root / 'cwd')], env=self.env,
                                  stdin=self.slave, stdout=self.slave, stderr=self.slave, start_new_session=True)

    def resize(self, width, height):
        fcntl.ioctl(self.slave, termios.TIOCSWINSZ, struct.pack('HHHH', height, width, 0, 0))
        self.screen.resize(width, height)
        if hasattr(self, 'p'): os.kill(self.p.pid, signal.SIGWINCH)

    def pump(self, seconds=.12):
        end = time.monotonic() + seconds
        while time.monotonic() < end:
            readable, _, _ = select.select([self.master], [], [], max(0, end-time.monotonic()))
            if readable:
                try: data = os.read(self.master, 65536)
                except OSError: break
                if not data: break
                self.raw.extend(data); self.screen.feed(data)
                if b'\x1b[6n' in data: os.write(self.master, b'\x1b[1;1R')
                if b'\x1b[c' in data: os.write(self.master, b'\x1b[?1;2c')
                if b'\x1b[?u' in data: os.write(self.master, b'\x1b[?1u')

    def send(self, data):
        os.write(self.master, data.encode() if isinstance(data, str) else data)
        self.pump()

    def wait(self, predicate, description, timeout=6):
        end = time.monotonic() + timeout
        while time.monotonic() < end:
            self.pump()
            if predicate(): return
            if self.p.poll() is not None: break
        raise AssertionError(description + '\nSCREEN:\n' + self.screen.text)

    def visible(self, text): self.wait(lambda: text in self.screen.text, 'Expected visible: ' + text)
    def cell(self, text):
        for y, row in enumerate(self.screen.cells):
            for x in range(len(row)):
                if ''.join(row[x:]).startswith(text): return x, y
        raise AssertionError('visible text not found: ' + text + '\n' + self.screen.text)

    def click(self, text):
        x,y=self.cell(text)
        self.send(f'\x1b[<0;{x+1};{y+1}M\x1b[<0;{x+1};{y+1}m')

    def drag(self, text):
        x,y=self.cell(text)
        width=sum(0 if unicodedata.combining(c) else 2 if unicodedata.east_asian_width(c) in 'WF' else 1 for c in text)
        self.send(f'\x1b[<0;{x+1};{y+1}M\x1b[<32;{x+width};{y+1}M\x1b[<0;{x+width};{y+1}m')

    def wire(self):
        path = self.root / 'wire.jsonl'
        if not path.exists(): return []
        return [json.loads(line) for line in path.read_text().splitlines() if line]
    def pids(self): return [r['pid'] for r in self.wire() if r['kind'] == 'start']
    def commands(self, typ, pid=None):
        return [r for r in self.wire() if r['kind']=='command' and r['command']['type']==typ and (pid is None or r['pid']==pid)]
    def control(self, pid, event):
        with (self.root / f'control-{pid}.jsonl').open('a') as f: f.write(json.dumps(event)+'\n')
        self.pump()
    def click_session(self, index):
        x,y=self.cell('+ New session')
        self.send(f'\x1b[<0;{x+1};{y+3+2*index}M\x1b[<0;{x+1};{y+3+2*index}m')
    def slash(self, command): self.send(command + ' '); self.send(b'\r')

    def close(self):
        if self.p.poll() is None:
            self.p.terminate()
            try: self.p.wait(7)
            except subprocess.TimeoutExpired: self.p.kill(); self.p.wait()
        for pid in self.pids():
            if alive(pid):
                try: os.killpg(pid, signal.SIGKILL)
                except ProcessLookupError: pass
        os.close(self.master); os.close(self.slave)


def acceptance(h):
    h.visible('+ New session'); h.visible('STATS'); h.visible('SESSION')
    assert 'SESSIONS' not in h.screen.text
    assert 'Ready' not in h.screen.text
    assert not any(label in h.screen.text for label in ('FILES', 'GIT STATUS', 'THEMES', 'PLUGINS')), 'Unexpected sidebar section'
    first = h.pids()[0]
    h.visible('$0.12')
    h.click('fixture-model'); h.visible('Choose model'); h.visible('Message pi'); h.send(b'\x1b')
    h.wait(lambda: 'Choose model' not in h.screen.text, 'model popup cancellation')
    h.click('medium')
    h.wait(lambda: len(h.commands('cycle_thinking_level',first))==1, 'clicking thinking must cycle directly')
    h.send(b'\x1b[1;5H'); h.visible('PRECOMPACTION_USER_MARKER'); h.visible('PRECOMPACTION_ASSISTANT_MARKER')
    h.send(b'\x1b[1;5F'); h.visible('POSTCOMPACTION_MARKER')
    assert 'TOOL_DEEP_LINE' not in h.screen.text, 'Tool output must begin collapsed'
    h.send(b'\x1b[1;5H'); h.visible('2 calls'); h.click('2 calls')
    h.visible('fixture.txt'); assert 'TOOL_DEEP_LINE' not in h.screen.text, 'Group expansion must leave individual results folded'
    h.click('fixture.txt'); h.visible('TOOL_DEEP_LINE'); h.visible('read · result')
    h.drag('COPY_ME 中文')
    h.wait(lambda:any(base64.b64decode(value).decode('utf-8')=='COPY_ME 中文' for value in re.findall(rb'\x1b\]52;c;([A-Za-z0-9+/=]+)(?:\x07|\x1b\\)',bytes(h.raw))), 'Unicode mouse selection OSC52 copy')
    h.visible('Latest')
    assert re.search(r'\d+/\d+\s+\[↓ Latest\]',h.screen.text), 'Missing message ordinal before Latest'
    h.click('Latest'); h.visible('POSTCOMPACTION_MARKER')
    assert 'Latest' not in h.screen.text, 'Latest button must hide at bottom'
    h.send(b'\x1b[1;5H'); h.visible('PRECOMPACTION_USER_MARKER')
    assert 'HIDDEN_COMPACTION_BODY' not in h.screen.text, 'Compaction body must not be dumped into transcript'
    h.send(b'\x0f'); h.send(b'\x1b[1;5H'); h.visible('TOOL_DEEP_LINE')
    h.send(b'\x14'); h.pump(); assert 'THINKING_VISIBLE_MARKER' not in h.screen.text
    h.send(b'\x14'); h.visible('THINKING_VISIBLE_MARKER')
    assert len(h.commands('cycle_thinking_level'))==1, 'Ctrl+T must toggle, not cycle'
    h.send(b'\x1b[Z'); h.wait(lambda: len(h.commands('cycle_thinking_level'))==2, 'Shift+Tab must cycle thinking')
    h.send(b'\x0c'); h.visible('Choose model'); h.send(b'\x1b'); h.pump()
    h.send('line-one'); h.send(b'\x0a'); h.visible('line-one'); h.send('line-two'); h.visible('line-two')
    first_x,first_y=h.cell('line-one'); second_x,second_y=h.cell('line-two')
    assert second_y==first_y+1, 'Ctrl+J must add exactly one visible input row'
    assert h.screen.cells[first_y][99]=='│' and h.screen.cells[second_y][99]=='│', 'Input must align with main-area right edge'
    h.send(b'\x1b[13;2u'); h.send('line-three'); h.visible('line-one'); h.visible('line-three')
    assert not h.commands('prompt'), 'Ctrl+J / Shift+Enter must not submit'
    h.send(b'\r'); h.wait(lambda: len(h.commands('prompt'))==1, 'Enter should submit fake prompt')
    assert h.commands('prompt')[0]['command']['message']=='line-one\nline-two\nline-three'
    h.send('你照这个图改 '); h.send(b'\x16'); h.visible('[image1]')
    h.send('，然后参考这个图 '); h.send(b'\x16'); h.visible('[image2]'); h.send(b'\r')
    h.wait(lambda: len(h.commands('prompt'))==2, 'inline images not submitted')
    image_prompt=h.commands('prompt')[-1]['command']
    assert image_prompt['message']=='你照这个图改 [image1]，然后参考这个图 [image2]'
    assert len(image_prompt['images'])==2
    h.control(first, {'type':'message_end','message':{'role':'user','content':[{'type':'text','text':image_prompt['message']}]+image_prompt['images'],'timestamp':8900}})
    h.visible(image_prompt['message']); h.click('[image1]')
    opened=h.root/'opened-images.jsonl'
    h.wait(lambda: opened.exists() and bool(opened.read_text().strip()), 'saved image click did not launch fake viewer')
    previews=[json.loads(line) for line in opened.read_text().splitlines()]
    assert len(previews)==1 and base64.b64decode(previews[0]['data'])==(h.root/'fixture.png').read_bytes()
    h.slash('/new'); h.wait(lambda: len(h.pids())==2, '/new must start a second RPC process')
    second = h.pids()[1]; assert second != first
    h.visible(f'Fixture-{second}'); h.send('DRAFT_SECOND_MARKER')
    h.send(b'\x19'); h.visible('Sessions'); h.send(b'\r'); h.visible(f'Fixture-{first}')
    h.control(second, {'type':'agent_start'})
    h.control(second, {'type':'message_end','message':{'role':'assistant','content':[{'type':'text','text':'BACKGROUND_FINISHED_MARKER'}],'timestamp':9000}})
    h.control(second, {'type':'agent_settled'})
    assert alive(first) and alive(second), 'Switching must not terminate background sessions'
    assert 'BACKGROUND_FINISHED_MARKER' not in h.screen.text, 'Background messages leaked into active session'
    h.click_session(1); h.visible('DRAFT_SECOND_MARKER'); h.send(b'\x1b[1;5F'); h.visible('BACKGROUND_FINISHED_MARKER')
    h.control(first, {'type':'extension_ui_request','id':'confirm-first','method':'confirm','title':'CONFIRM_FIRST','message':'Confirm from background'})
    assert 'CONFIRM_FIRST' not in h.screen.text, 'Background dialog stole focus'
    h.click_session(0); h.visible('CONFIRM_FIRST'); h.send(b'\x1b[B'); h.send(b'\r')
    h.wait(lambda: any(r['command'].get('id')=='confirm-first' for r in h.commands('extension_ui_response',first)), 'Confirm response missing')
    responses = h.commands('extension_ui_response',first)
    assert responses[-1]['command'].get('confirmed') is True
    for method, value in [('select','Choice B'),('input','typed input'),('editor','edited text')]:
        request_id = method+'-first'
        e = {'type':'extension_ui_request','id':request_id,'method':method,'title':method.upper()+'_FIRST'}
        if method=='select': e['options']=['Choice A','Choice B']
        h.control(first,e); h.visible(method.upper()+'_FIRST')
        if method=='select': h.send(b'\x1b[B')
        else: h.send(value)
        h.send(b'\r')
        h.wait(lambda: any(r['command'].get('id')==request_id for r in h.commands('extension_ui_response',first)), method+' response missing')
        result = [r for r in h.commands('extension_ui_response',first) if r['command'].get('id')==request_id][-1]
        assert result['command'].get('value')==value, result
    assert not h.commands('extension_ui_response',second), 'Responses sent to wrong session'
    for width in (80,60,132):
        h.resize(width,36); h.pump(.4); h.visible('Message pi')
        assert ' π ' not in h.screen.text, 'Removed top-left session header reappeared'
        if width<96: assert 'STATS' not in h.screen.text, 'Narrow layout should hide sidebar'
    h.send(b'\x04'); h.visible(f'Fixture-{second}')
    h.wait(lambda: not alive(first), 'Ctrl+D did not close active RPC')
    assert h.p.poll() is None and alive(second), 'Ctrl+D stopped frontend or another session'
    h.visible('DRAFT_SECOND_MARKER'); h.send(b'\x03')
    h.slash('/quit'); h.wait(lambda: h.p.poll() is not None,'Quit failed',timeout=9)
    assert h.p.returncode == 0, h.p.returncode
    h.wait(lambda: not any(alive(pid) for pid in h.pids()), 'RPC children survived quit',timeout=4)
    assert b'\x1b[?1049l' in h.raw, 'Alternate screen not restored'
    assert termios.tcgetattr(h.slave)==h.original, 'TTY settings not restored'
    assert all(not pathlib.Path(p['path']).exists() for p in previews), 'preview temporary file leaked'
    assert all(r['home']==str(h.root/'home') for r in h.wire() if r['kind']=='start')
    print('PASS fake PTY: native keys, full history, grouped/framed tools, mouse copy, latest button, parallel sessions, scoped dialogs, resize, cleanup')


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',type=pathlib.Path)
    parser.add_argument('--real-pi',type=pathlib.Path)
    parser.add_argument('--real-only',action='store_true',help='Run only the read-only real RPC smoke (requires --real-pi)')
    args=parser.parse_args()
    if args.real_only and not args.real_pi: parser.error('--real-only requires --real-pi')
    with tempfile.TemporaryDirectory(prefix='pi-goui-acceptance-') as tmp:
        root=pathlib.Path(tmp); binary=args.binary.resolve() if args.binary else root/'pi-goui'
        if not args.binary: subprocess.run(['go','build','-o',str(binary),'./cmd/pi-goui'],cwd=ROOT,check=True)
        if not args.real_only:
            sandbox=root/'fake';sandbox.mkdir();h=Harness(binary,sandbox)
            try: acceptance(h)
            except BaseException:
                tail=[dict(pid=r['pid'],kind=r['kind'],type=r.get('command',r.get('event',{})).get('type')) for r in h.wire()[-16:]]
                print('Wire tail:',json.dumps(tail,ensure_ascii=False));raise
            finally: h.close()
        if args.real_pi:
            real=root/'real';real.mkdir();(real/'agent').mkdir();(real/'home').mkdir()
            env=os.environ.copy();env.update(HOME=str(real/'home'),PI_CODING_AGENT_DIR=str(real/'agent'),PI_BIN=str(ROOT/'tests/real_rpc_guard.py'),GOUI_REAL_PI=str(args.real_pi.resolve()),GOUI_REAL_LOG=str(real/'wire.jsonl'))
            result=subprocess.run([str(binary),'--cwd',str(real),'--check'],env=env,text=True,capture_output=True,timeout=25)
            assert result.returncode==0,(result.stdout,result.stderr)
            assert 'RPC OK' in result.stdout
            wire=[json.loads(line) for line in (real/'wire.jsonl').read_text().splitlines()]
            assert wire and all(r['allowed'] for r in wire)
            assert not list((real/'agent').glob('sessions/**/*.jsonl'))
            print('PASS real RPC whitelist:',result.stdout.strip())

if __name__=='__main__':main()
