'use strict';
const $ = id => document.getElementById(id);
const encoder = new TextEncoder();
const loginURL = new URL(location.href);
let linkToken = loginURL.searchParams.get('pk');
if (loginURL.searchParams.has('pk')) {
  loginURL.searchParams.delete('pk');
  history.replaceState(null, '', loginURL.pathname + loginURL.search + loginURL.hash);
}
let terminal, fit, socket, generation = 0, retryTimer, resizeTimer, attempts = 0;
let heartbeatTimer, heartbeatDeadline, desiredSize, requestedSize;
let connected = false, automatic = false, pendingCopy = '', clipboardOwner = 0;
let touchDriven = false, cursorVisible = true;
let project = null, choosing = false, instanceTimer;
const touchUI = () => touchDriven || matchMedia('(pointer: coarse)').matches;
function keyboardInput(enabled) {
  const area = terminal?.textarea;
  if (!area) return;
  area.readOnly = !enabled; area.inputMode = enabled ? 'text' : 'none';
  if (enabled) terminal.focus(); else terminal.blur();
}
function inputTap(event) {
  if (!terminal || !cursorVisible || !event.target.closest('#terminal')) return false;
  const screen = terminal.element.querySelector('.xterm-screen').getBoundingClientRect();
  const x = Math.floor((event.clientX - screen.left) * terminal.cols / screen.width);
  const y = Math.floor((event.clientY - screen.top) * terminal.rows / screen.height);
  const buffer = terminal.buffer.active;
  if (x < 0 || x >= terminal.cols || y < 0 || y >= terminal.rows || buffer.viewportY !== buffer.baseY) return false;
  const cell = (col, row) => buffer.getLine(buffer.baseY + row)?.getCell(col)?.getChars() || '';
  const edge = (col, row) => /[│┃]/u.test(cell(col, row));
  const cy = buffer.cursorY;
  let left = buffer.cursorX, right = buffer.cursorX;
  while (left > 0 && !edge(left, cy)) left--;
  while (right < terminal.cols - 1 && !edge(right, cy)) right++;
  if (x <= left || x >= right) return false;
  let top = cy, bottom = cy;
  while (top > 0 && edge(left, top) && edge(right, top)) top--;
  while (bottom < terminal.rows - 1 && edge(left, bottom) && edge(right, bottom)) bottom++;
  // The native bottom editor includes a spacer/footer: these are controls,
  // not editable rows. Popups only open the keyboard on their cursor row.
  if (left === 0 && bottom === terminal.rows - 1 && /[┌╭]/u.test(cell(left, top)) && /[┘╯]/u.test(cell(right, bottom))) {
    return y > top && y <= bottom - 3;
  }
  return y === cy;
}
function touchScroll(term) {
  const screen = term.element.querySelector('.xterm-screen');
  let gesture;
  const options = {capture: true, passive: false};
  term.element.addEventListener('touchstart', event => {
    gesture = undefined;
    if (!connected || terminal !== term || event.touches.length !== 1) return;
    const point = event.touches[0];
    if (inputTap({target: event.target, clientX: point.clientX, clientY: point.clientY})) return;
    gesture = {id: point.identifier, x: point.clientX, y: point.clientY, lastY: point.clientY, remainder: 0, axis: ''};
    event.stopPropagation(); // Do not also pan xterm's browser scrollback.
  }, options);
  term.element.addEventListener('touchmove', event => {
    if (!gesture) return;
    if (!connected || terminal !== term || event.touches.length !== 1) { gesture = undefined; return; }
    const point = event.touches[0];
    if (point.identifier !== gesture.id) return;
    event.stopPropagation();
    const dx = Math.abs(point.clientX - gesture.x), dy = Math.abs(point.clientY - gesture.y);
    if (!gesture.axis && Math.max(dx, dy) < 8) return;
    gesture.axis ||= dy >= dx ? 'y' : 'x';
    event.preventDefault();
    if (gesture.axis !== 'y') return;
    keyboardInput(false); activity();
    const height = screen.getBoundingClientRect().height / term.rows;
    if (height <= 0) return;
    gesture.remainder += gesture.lastY - point.clientY;
    gesture.lastY = point.clientY;
    const lines = Math.trunc(gesture.remainder / height);
    gesture.remainder -= lines * height;
    // Use xterm's own mouse encoding, mode handling and coordinate mapping.
    // Keep the starting hit target even if the finger leaves its list/popup.
    for (let i = 0; i < Math.min(Math.abs(lines), 20); i++) {
      screen.dispatchEvent(new WheelEvent('wheel', {bubbles: true, cancelable: true,
        clientX: gesture.x, clientY: gesture.y, deltaMode: 1, deltaY: Math.sign(lines)}));
    }
  }, options);
  const finish = event => {
    if (!gesture) return;
    event.stopPropagation();
    if (gesture.axis) event.preventDefault(); // A swipe must not become a click.
    gesture = undefined;
  };
  term.element.addEventListener('touchend', finish, options);
  term.element.addEventListener('touchcancel', finish, options);
}
function showMenu(open) {
  $('terminal-menu').hidden = !open;
  $('menu-toggle').setAttribute('aria-expanded', String(open));
}
$('menu-toggle').onclick = () => showMenu($('terminal-menu').hidden);
document.addEventListener('pointerdown', event => {
  if (!event.target.closest('#terminal-menu, #menu-toggle')) showMenu(false);
  if (event.pointerType === 'touch') touchDriven = true;
  if (terminal && touchUI()) keyboardInput(inputTap(event));
}, {capture: true});
const status = message => { $('status').textContent = message; $('menu-toggle').title = '终端菜单 · ' + message; };
document.addEventListener('keydown', event => {
  if (event.key === 'Escape' && !$('terminal-menu').hidden) {
    showMenu(false); event.preventDefault(); event.stopPropagation();
    if (!touchUI()) terminal?.focus();
  }
}, {capture: true});
const instancePath = (selected, path) => `instances/${encodeURIComponent(selected.instanceId || selected.id)}/api/${path}`;
async function api(path, body, method = body === undefined ? 'GET' : 'POST') {
  const response = await fetch('/api/' + path, {signal: AbortSignal.timeout(15000), ...(body === undefined ? {method, cache: 'no-store'} : {
    method, headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)
  })});
  const value = await response.json();
  if (!response.ok) { const error = new Error(value.error || `HTTP ${response.status}`); error.status = response.status; throw error; }
  return value;
}
function disconnect() {
  generation++; connected = false; clearTimeout(retryTimer); clearTimeout(resizeTimer); clearTimeout(instanceTimer);
  clearTimeout(heartbeatTimer); clearTimeout(heartbeatDeadline); desiredSize = requestedSize = null;
  if (socket) { socket.onclose = socket.onmessage = socket.onopen = socket.onerror = null; socket.close(); socket = null; }
  terminal?.dispose(); terminal = fit = undefined;
  $('terminal').replaceChildren();
}
function showLogin(message = '') {
  automatic = false; disconnect(); pendingCopy = ''; showMenu(false);
  $('clipboard-dialog').close(); $('clipboard-text').value = '';
  project = null; $('instances').hidden = $('projects').hidden = true;
  $('app').hidden = true; $('login').hidden = false; $('login-error').textContent = message; if (!touchUI()) $('token').focus();
}
function send(bytes) {
  if (!connected || socket?.readyState !== WebSocket.OPEN) { status('尚未连接，输入未发送'); return; }
  // Bounded WebSocket frames, including large bracketed pastes. UTF-8 may span
  // frames: the PTY consumes the ordered byte stream, not independent strings.
  if (socket.bufferedAmount + bytes.length > 1024 * 1024) { status('发送缓冲已满，请稍后重试'); return; }
  for (let i = 0; i < bytes.length; i += 16384) socket.send(bytes.subarray(i, i + 16384));
}
function resize() {
  if (!terminal || !fit) return;
  const size = fit.proposeDimensions();
  if (!size) return;
  const cols = Math.max(20, Math.min(500, size.cols)), rows = Math.max(5, Math.min(200, size.rows));
  desiredSize = {cols, rows};
  if (!connected) terminal.resize(cols, rows);
  if (socket?.readyState === WebSocket.OPEN && (requestedSize?.cols !== cols || requestedSize?.rows !== rows)) {
    requestedSize = {cols, rows}; socket.send(JSON.stringify({type: 'resize', cols, rows}));
  }
}
function sharedSize(size) {
  if (!terminal || !Number.isInteger(size.cols) || !Number.isInteger(size.rows) || size.cols < 20 || size.cols > 500 || size.rows < 5 || size.rows > 200) return;
  terminal.resize(size.cols, size.rows);
  const screen = terminal.element.querySelector('.xterm-screen');
  terminal.element.style.width = screen.style.width;
  terminal.element.style.height = screen.style.height;
  if (terminal.buffer.active.type === 'alternate') terminal.scrollToBottom();
}
function activity() {
  if (!connected || socket?.readyState !== WebSocket.OPEN) return;
  resize();
  if (desiredSize) socket.send(JSON.stringify({type: 'activity', ...desiredSize}));
}
function heartbeats(owner) {
  clearTimeout(heartbeatTimer); clearTimeout(heartbeatDeadline);
  heartbeatTimer = setTimeout(() => {
    if (owner !== generation || !connected) return;
    heartbeatDeadline = setTimeout(() => {
      if (owner === generation && automatic) { disconnect(); scheduleReconnect(generation); }
    }, 15000);
    socket.send(JSON.stringify({type: 'ping'}));
  }, 20000);
}
function makeTerminal() {
  terminal = new Terminal({fontFamily: getComputedStyle(document.documentElement).fontFamily, fontSize: 14,
    cursorBlink: true, scrollback: 3000, allowProposedApi: true, theme: {background: getComputedStyle(document.documentElement).getPropertyValue('--page-bg').trim(), foreground: '#dddddd'},
    linkHandler: {activate() { /* Terminal output cannot open browser URLs. */ }}
  });
  terminal.loadAddon(new UnicodeGraphemesAddon.UnicodeGraphemesAddon());
  fit = new FitAddon.FitAddon(); terminal.loadAddon(fit); terminal.open($('terminal')); resize();
  cursorVisible = true;
  touchScroll(terminal);
  if (touchUI()) keyboardInput(false);
  for (const [final, visible] of [['h', true], ['l', false]]) {
    terminal.parser.registerCsiHandler({prefix: '?', final}, params => {
      if (params.includes(25)) cursorVisible = visible;
      return false; // Observe cursor visibility without consuming the VT mode.
    });
  }
  let lastPointerActivity = 0;
  terminal.element.addEventListener('pointermove', () => {
    if (performance.now() - lastPointerActivity > 150) { lastPointerActivity = performance.now(); activity(); }
  }, {capture: true, passive: true});
  for (const type of ['keydown', 'pointerdown', 'wheel', 'touchstart', 'beforeinput', 'paste']) {
    terminal.element.addEventListener(type, activity, {capture: true, passive: true});
  }
  terminal.onData(data => send(encoder.encode(data)));
  terminal.onBinary(data => send(Uint8Array.from(data, char => char.charCodeAt(0))));
  terminal.attachCustomKeyEventHandler(event => {
    if ((event.ctrlKey || event.metaKey) && event.shiftKey && ['c', 'v'].includes(event.key.toLowerCase())) {
      if (event.type === 'keydown') { event.preventDefault(); void (event.key.toLowerCase() === 'c' ? copy() : paste()); }
      return false;
    }
    return true;
  });
  // Never answer OSC52 clipboard read requests or write the browser clipboard
  // automatically. A subsequent explicit Copy action may use this text.
  terminal.parser.registerOscHandler(52, payload => {
    const data = payload.slice(payload.indexOf(';') + 1);
    if (data !== '?' && data.length <= 1024 * 1024) {
      try { pendingCopy = new TextDecoder('utf-8', {fatal: true}).decode(Uint8Array.from(atob(data), c => c.charCodeAt(0))); status('TUI 已复制文本，点击“复制”写入浏览器剪贴板'); } catch { /* Invalid clipboard output is ignored. */ }
    }
    return true;
  });
}
async function connect() {
  if (!project) { await enter(); return; }
  const selected = project;
  automatic = true; disconnect(); const owner = generation;
  if (document.hidden || !navigator.onLine) { status('连接已暂停，任务继续运行'); return; }
  status('连接中');
  try {
    if (!(await api('auth')).authenticated) { if (owner === generation) showLogin('请重新登录'); return; }
    if (owner !== generation) return;
    const info = await api(instancePath(selected, 'info'));
    if (owner !== generation) return;
    if (info.projectId !== selected.projectId) { await enter('实例已改变，请重新选择'); return; }
    // xterm caches cell metrics and glyphs: finish loading every font style
    // before opening it, rather than fitting with a device-dependent fallback.
    const family = getComputedStyle(document.documentElement).fontFamily;
    await Promise.all(['400', '700', 'italic 400', 'italic 700'].map(style => document.fonts.load(`${style} 14px ${family}`, 'M中\ue0b0')));
    if (owner !== generation) return;
    makeTerminal();
    let pendingOutput = 0;
    const url = new URL('/api/' + instancePath(selected, 'terminal'), location.href); url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    url.searchParams.set('projectId', selected.projectId);
    url.searchParams.set('cols', terminal.cols); url.searchParams.set('rows', terminal.rows);
    const ws = socket = new WebSocket(url); ws.binaryType = 'arraybuffer';
    heartbeatDeadline = setTimeout(() => { if (owner === generation && automatic) { disconnect(); scheduleReconnect(generation); } }, 15000);
    ws.onmessage = event => {
      if (owner !== generation) return;
      if (event.data instanceof ArrayBuffer) {
        const size = event.data.byteLength;
        // Include the 4 MiB replay while bounding xterm's asynchronous parser
        // queue on slow/background tabs. Reconnect repaints from the live TUI.
        if (pendingOutput + size > 8 * 1024 * 1024) { disconnect(); scheduleReconnect(generation); return; }
        pendingOutput += size;
        const term = terminal;
        term.write(new Uint8Array(event.data), () => {
          pendingOutput -= size;
          // Grid changes during replay can leave xterm's alternate viewport
          // above its live cursor. Native TUI scrolling lives in the PTY, not
          // in browser scrollback: keep the live alternate screen visible.
          if (owner === generation && terminal === term && term.buffer.active.type === 'alternate') term.scrollToBottom();
        });
        return;
      }
      let message; try { message = JSON.parse(event.data); } catch { status('无效的终端控制消息'); return; }
      if (message.type === 'ready') { connected = true; attempts = 0; sharedSize(message); heartbeats(owner); status('已连接 · 多端同步'); if (!touchUI()) terminal.focus(); }
      if (message.type === 'size') {
        const term = terminal;
        term.write('', () => { if (owner === generation && terminal === term) sharedSize(message); });
      }
      if (message.type === 'pong') heartbeats(owner);
      if (message.type === 'exit') {
        automatic = false; connected = false; clearTimeout(heartbeatTimer); clearTimeout(heartbeatDeadline);
        status(message.message || 'TUI 已退出；点击“重启 TUI”重新启动');
      }
      if (message.type === 'taken_over') void enter('旧服务已切换控制端，请重新选择实例');
      if (message.type === 'restart') { connected = false; status('TUI 已重启，重新连接中'); }
      if (message.type === 'project_closed' || message.type === 'project_changed') void enter();
    };
    ws.onclose = () => {
      if (owner !== generation) return;
      connected = false;
      if (automatic) scheduleReconnect(owner);
    };
    ws.onerror = () => { if (owner === generation && automatic) status('连接失败，正在重试'); };
  } catch (error) {
    if (owner !== generation || !automatic) return;
    if (error.status === 401) showLogin('请重新登录');
    else if (error.status === 404 || error.status === 409) await enter(error.message);
    else { status(error.message); scheduleReconnect(owner); }
  }
}
function scheduleReconnect(owner) {
  clearTimeout(heartbeatTimer); clearTimeout(heartbeatDeadline);
  if (document.hidden || !navigator.onLine) { status('连接已暂停，任务继续运行'); return; }
  status('连接断开，正在重连（任务继续运行）');
  retryTimer = setTimeout(() => { if (owner === generation && automatic) void connect(); }, Math.min(1000 * 2 ** Math.min(attempts++, 4), 15000));
}
function projectBusy(busy) {
  choosing = busy;
  for (const button of document.querySelectorAll('#instances button, #projects button')) button.disabled = busy;
}
function picker(id) {
  automatic = false; disconnect(); project = null; showMenu(false);
  $('clipboard-dialog').close(); $('clipboard-text').value = ''; pendingCopy = '';
  for (const page of ['login', 'app', 'instances', 'projects']) $(page).hidden = page !== id;
  $('token').value = ''; projectBusy(true);
  return generation;
}
async function enter(message = '') {
  const owner = picker('instances');
  $('instance-list').replaceChildren(); $('instance-error').textContent = message;
  try {
    const list = await api('instances');
    if (owner !== generation) return;
    renderInstances(list.instances);
  } catch (error) {
    if (owner === generation) {
      if (error.status === 401) showLogin('请重新登录');
      else $('instance-error').textContent = error.message;
    }
  } finally {
    if (owner === generation) { projectBusy(false); scheduleInstances(owner); }
  }
}
function renderInstances(instances) {
  const snapshot = JSON.stringify(instances);
  if ($('instance-list').dataset.snapshot === snapshot && $('instance-list').childElementCount === instances.length) return;
  $('instance-list').dataset.snapshot = snapshot;
  const focused = document.activeElement?.dataset.instanceId;
  $('instance-list').replaceChildren();
  for (const instance of instances) {
      const item = document.createElement('li'), button = document.createElement('button');
      const name = document.createElement('strong'), path = document.createElement('span');
      name.textContent = instance.desktop ? '桌面共享实例' : (instance.name || '网页实例');
      path.className = 'project-path'; path.textContent = instance.cwd || '尚未选择项目';
      button.type = 'button'; button.dataset.instanceId = instance.id;
      if (instance.desktop) button.setAttribute('data-desktop', '');
      if (instance.status === 'exited') name.textContent += ' · 已退出';
      button.append(name, path); button.onclick = () => void openInstance(instance);
      item.append(button); $('instance-list').append(item);
      if (focused === instance.id) button.focus();
  }
}
function scheduleInstances(owner) {
  clearTimeout(instanceTimer);
  if (owner !== generation || $('instances').hidden) return;
  instanceTimer = setTimeout(async () => {
    try {
      if (choosing || document.hidden) return;
      const list = await api('instances');
      if (owner === generation && !choosing && !$('instances').hidden) renderInstances(list.instances);
    } catch (error) {
      if (owner === generation && !choosing) {
        if (error.status === 401) showLogin('请重新登录');
        else $('instance-error').textContent = error.message;
      }
    } finally { scheduleInstances(owner); }
  }, 2000);
}
async function enterProjects() {
  const owner = picker('projects');
  $('project-list').replaceChildren(); $('project-error').textContent = '';
  try {
    const list = await api('projects');
    if (owner !== generation) return;
    $('project-path').value = list.defaultCwd || list.cwd;
    $('project-error').textContent = list.warning || '';
    for (const cwd of list.directories.slice(0, 10)) {
      const item = document.createElement('li'), button = document.createElement('button');
      const name = document.createElement('strong'), path = document.createElement('span');
      name.textContent = cwd.split(/[\\/]/).filter(Boolean).pop() || cwd;
      path.className = 'project-path'; path.textContent = cwd;
      button.type = 'button'; button.title = cwd;
      button.append(name, path); button.onclick = () => void openProject(cwd);
      item.append(button); $('project-list').append(item);
    }
  } catch (error) {
    if (owner === generation) {
      if (error.status === 401) showLogin('请重新登录');
      else $('project-error').textContent = error.message;
    }
  } finally { if (owner === generation) projectBusy(false); }
}
async function startProject(selected) {
  projectBusy(false); project = selected;
  $('instances').hidden = $('projects').hidden = true; $('app').hidden = false;
  $('restart').disabled = $('change-project').disabled = false;
  attempts = 0; await connect();
}
async function openInstance(instance) {
  if (choosing) return;
  const owner = generation;
  projectBusy(true); $('instance-error').textContent = '';
  try {
    const selected = instance.projectId ? instance : await api(instancePath(instance, 'projects/select'), {cwd: instance.cwd, projectId: ''});
    if (owner !== generation) return;
    await startProject({...selected, instanceId: instance.id});
  } catch (error) {
    if (owner === generation) {
      if (error.status === 401) showLogin('请重新登录');
      else if (error.status === 404 || error.status === 409) await enter(error.message);
      else $('instance-error').textContent = error.message;
    }
  } finally { if (owner === generation) projectBusy(false); }
}
async function openProject(cwd) {
  if (choosing) return;
  const owner = generation;
  projectBusy(true); $('project-error').textContent = '';
  try {
    const selected = await api('instances', {cwd});
    if (owner !== generation) return;
    await startProject({...selected, instanceId: selected.id});
  } catch (error) {
    if (owner === generation) {
      if (error.status === 401) showLogin('请重新登录');
      else $('project-error').textContent = error.message;
    }
  } finally { if (owner === generation) projectBusy(false); }
}
$('project-form').onsubmit = event => { event.preventDefault(); void openProject($('project-path').value); };
$('new-instance').onclick = $('project-refresh').onclick = () => void enterProjects();
$('instance-refresh').onclick = $('project-back').onclick = $('instances-back').onclick = () => void enter();
function clipboardDialog(text, copying) {
  clipboardOwner = generation;
  $('clipboard-label').textContent = copying ? '复制文本' : '粘贴文本';
  $('clipboard-text').value = text; $('clipboard-text').readOnly = copying;
  $('clipboard-submit').hidden = copying;
  $('clipboard-help').textContent = copying ? '请按 Ctrl/⌘+C 或使用系统复制。' : '此连接无法直接读取剪贴板。请在此按 Ctrl/⌘+V 或长按粘贴，再发送。';
  $('clipboard-dialog').showModal(); if (!touchUI()) $('clipboard-text').focus(); if (copying) $('clipboard-text').select();
}
async function copy() {
  const text = terminal?.getSelection() || pendingCopy;
  if (!text) { status('先选择终端文本，或在 TUI 内执行复制'); return; }
  const owner = generation;
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable');
    await navigator.clipboard.writeText(text);
    if (owner === generation) status('已复制');
  } catch {
    if (owner !== generation) return;
    clipboardDialog(text, true);
    if (document.execCommand('copy')) { $('clipboard-dialog').close(); status('已复制'); terminal?.focus(); }
  }
}
async function paste() {
  if (!connected) { status('连接后才能粘贴'); return; }
  const owner = generation;
  try {
    if (!navigator.clipboard?.readText) throw new Error('clipboard unavailable');
    const text = await navigator.clipboard.readText();
    if (owner === generation && connected) { activity(); terminal.paste(text); }
  } catch { if (owner === generation) clipboardDialog('', false); }
}
$('clipboard-form').addEventListener('submit', event => {
  event.preventDefault();
  if (clipboardOwner !== generation || !connected) { $('clipboard-help').textContent = '连接已改变，请关闭后重新粘贴。'; return; }
  activity(); terminal.paste($('clipboard-text').value); $('clipboard-dialog').close(); $('clipboard-text').value = ''; terminal.focus();
});
$('clipboard-close').onclick = () => { $('clipboard-dialog').close(); $('clipboard-text').value = ''; terminal?.focus(); };
$('login-form').addEventListener('submit', async event => {
  event.preventDefault(); const button = event.submitter; if (button) button.disabled = true;
  try { await api('login', {token: $('token').value}); await enter(); } catch (error) { $('login-error').textContent = error.message; }
  finally { if (button) button.disabled = false; }
});
$('reconnect').onclick = () => { attempts = 0; void connect(); };
$('copy').onclick = () => void copy(); $('paste').onclick = () => void paste();
$('restart').onclick = async () => {
  if (!project || !confirm('重启会停止当前实例的全部会话，并影响所有连接端。确定继续？')) return;
  const selected = project;
  automatic = false; disconnect(); status('正在重启 TUI'); $('restart').disabled = true;
  const owner = generation;
  try {
    await api(instancePath(selected, 'terminal/restart'), {projectId: selected.projectId});
    if (owner === generation) { attempts = 0; await connect(); }
  } catch (error) {
    if (owner === generation) {
      if (error.status === 401) showLogin('请重新登录');
      else if (error.status === 404 || error.status === 409) await enter(error.message);
      else status(error.message);
    }
  } finally { if (project === selected) $('restart').disabled = false; }
};
$('change-project').onclick = async () => {
  if (!project || !confirm('关闭当前实例及全部会话会影响所有连接端，然后为新实例选择目录。其他实例不受影响。确定继续？')) return;
  const selected = project;
  automatic = false; disconnect(); $('change-project').disabled = true;
  const owner = generation;
  try {
    await api('instances/' + encodeURIComponent(selected.instanceId), {projectId: selected.projectId}, 'DELETE');
    if (owner === generation) await enterProjects();
  } catch (error) {
    if (owner === generation) {
      if (error.status === 401) showLogin('请重新登录');
      else await enter(error.message);
    }
  } finally { if (project === selected) $('change-project').disabled = false; }
};
$('instance-logout').onclick = $('project-logout').onclick = $('logout').onclick = async () => {
  showLogin(); const owner = generation;
  try { await api('logout', {}); } catch (error) {
    if (owner === generation) $('login-error').textContent = error.message;
  }
};
const observer = new ResizeObserver(() => { clearTimeout(resizeTimer); resizeTimer = setTimeout(resize, 60); });
observer.observe($('terminal'));
function pauseConnection() {
  if (!project || !automatic) return;
  disconnect(); status('连接已暂停，任务继续运行');
}
function resumeConnection() {
  if (document.hidden || !navigator.onLine) return;
  if (project && automatic && !socket) void connect();
  else if (!$('instances').hidden) void enter();
}
document.addEventListener('visibilitychange', () => { if (document.hidden) pauseConnection(); else resumeConnection(); });
window.addEventListener('offline', pauseConnection);
window.addEventListener('online', resumeConnection);
window.addEventListener('pagehide', () => { automatic = false; disconnect(); observer.disconnect(); });
window.addEventListener('pageshow', event => { if (event.persisted) { observer.observe($('terminal')); void startup(); } });
async function startup() {
  const token = linkToken; linkToken = null;
  try {
    if (token !== null) {
      $('token').value = token;
      await api('login', {token});
      await enter();
    } else if ((await api('auth')).authenticated) await enter();
    else showLogin();
  } catch (error) { showLogin(error.message); }
}
void startup();
