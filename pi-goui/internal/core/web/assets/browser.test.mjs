// Actual Chromium + vendored xterm, with an isolated mock terminal transport.
// No Pi, model, provider or shell command is started by the browser.
import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import {spawn} from 'node:child_process';
import {mkdtemp, readFile, rm, access} from 'node:fs/promises';
import {constants} from 'node:fs';
import {tmpdir} from 'node:os';
import {fileURLToPath} from 'node:url';
import path from 'node:path';
const root = path.dirname(fileURLToPath(import.meta.url));
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
async function until(fn, label) { for(let i=0;i<120;i++){if(await fn())return;await delay(50);}throw Error('Timed out: '+label); }
class CDP {
  constructor(url) {
    this.next=0;this.pending=new Map();this.exceptions=[];this.socket=new WebSocket(url);
    this.ready=new Promise((resolve,reject)=>{this.socket.addEventListener('open',resolve,{once:true});this.socket.addEventListener('error',reject,{once:true});});
    this.socket.addEventListener('message',event=>{const m=JSON.parse(event.data);if(m.method==='Runtime.exceptionThrown')this.exceptions.push(m.params);if(m.id){const p=this.pending.get(m.id);this.pending.delete(m.id);m.error?p.reject(Error(JSON.stringify(m.error))):p.resolve(m.result);}});
  }
  async call(method,params={}){await this.ready;return new Promise((resolve,reject)=>{const id=++this.next;this.pending.set(id,{resolve,reject});this.socket.send(JSON.stringify({id,method,params}));});}
  async evaluate(expression){const r=await this.call('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true,userGesture:true});if(r.exceptionDetails)throw Error(JSON.stringify(r.exceptionDetails));return r.result?.value;}
  close(){this.socket.close();}
}
test('xterm: shared grid, activity, lifecycle, raw input, ANSI, reconnect and clipboard', {timeout:30000}, async t => {
  let executable=process.env.CHROME_BIN;
  if(executable)await access(executable,constants.X_OK);
  else {for(const name of ['chromium','chromium-browser','google-chrome']){for(const dir of (process.env.PATH||'').split(path.delimiter)){const p=path.join(dir,name);try{await access(p,constants.X_OK);executable=p;break;}catch{}}if(executable)break;}if(!executable){t.skip('Set CHROME_BIN to run actual Chromium');return;}}
  let authenticated=false,restarts=0,closes=0,sequence=0;
  const desktop={id:'main',name:'Desktop',desktop:true,cwd:'/projects/desktop',projectId:'desktop-project',status:'running'};
  const instances=new Map([['main',desktop]]),operations=[];
  let current=desktop,holdCreate=null,holdInfo=null,createWaiting=false,infoWaiting=false;
  const directories=Array.from({length:10},(_,i)=>`/projects/recent-${i}`);
  directories[9]='/projects/<img src=x onerror=alert(1)>';
  const server=http.createServer(async(req,res)=>{
    try {
      const url=new URL(req.url,'http://localhost');let raw='';for await(const part of req)raw+=part;const data=raw?JSON.parse(raw):{};
      const json=(value,status=200)=>{res.writeHead(status,{'Content-Type':'application/json'});res.end(JSON.stringify(value));};
      if(url.pathname==='/api/auth')return json({authenticated});
      if(url.pathname==='/api/login'){if(data.token!=='test-token')return json({error:'Invalid token'},401);authenticated=true;return json({ok:true});}
      if(url.pathname==='/api/logout'){authenticated=false;return json({ok:true});}
      if(url.pathname.startsWith('/api/')&&!authenticated)return json({error:'Please login'},401);
      if(url.pathname==='/api/projects')return json({...desktop,directories,defaultCwd:'/projects/default'});
      if(url.pathname==='/api/instances'&&req.method==='GET')return json({instances:Array.from(instances.values())});
      if(url.pathname==='/api/instances'&&req.method==='POST'){
        if(data.cwd==='invalid')return json({error:'Invalid directory'},400);
        if(holdCreate){createWaiting=true;await holdCreate;}
        const n=++sequence;current={id:'instance-'+n,name:n===1?'<img src=x onerror=alert(1)>':'Web '+n,desktop:false,cwd:data.cwd,projectId:'project-'+n,status:'idle'};
        instances.set(current.id,current);operations.push({type:'create',id:current.id});return json(current);
      }
      const scoped=url.pathname.match(/^\/api\/instances\/([^/]+)(?:\/api\/(.*))?$/);
      if(scoped){
        const id=decodeURIComponent(scoped[1]),selected=instances.get(id);
        if(!selected)return json({error:'Instance not found'},404);
        if(!scoped[2]&&req.method==='DELETE'){
          if(data.projectId!==selected.projectId)return json({error:'Project changed'},409);
          closes++;operations.push({type:'delete',id});
          if(id==='main')selected.projectId='';else instances.delete(id);
          return json({ok:true});
        }
        if(scoped[2]==='info'){if(holdInfo){infoWaiting=true;await holdInfo;}return json(selected);}
        if(scoped[2]==='projects/select'){
          if(data.projectId!==selected.projectId)return json({error:'Project changed'},409);
          selected.projectId||='desktop-selected';selected.cwd=data.cwd;operations.push({type:'select',id});return json(selected);
        }
        if(scoped[2]==='terminal/restart'){
          if(data.projectId!==selected.projectId)return json({error:'Project changed'},409);
          restarts++;operations.push({type:'restart',id});return json({ok:true});
        }
        return json({error:'not found'},404);
      }
      const name=url.pathname==='/'?'index.html':url.pathname.slice(1);
      if(!['index.html','app.js','style.css','vendor/xterm.js','vendor/xterm.css','vendor/addon-fit.js','vendor/addon-unicode-graphemes.js','fonts/jetbrains-mono-regular.woff2','fonts/jetbrains-mono-bold.woff2','fonts/jetbrains-mono-italic.woff2','fonts/jetbrains-mono-bold-italic.woff2','fonts/wqy-microhei-mono.woff2'].includes(name))return json({error:'not found'},404);
      res.writeHead(200,{'Content-Type':name.endsWith('.woff2')?'font/woff2':name.endsWith('.css')?'text/css':name.endsWith('.html')?'text/html':'text/javascript','Content-Security-Policy':"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; object-src 'none'"});res.end(await readFile(path.join(root,name)));
    }catch(error){res.writeHead(500);res.end(String(error));}
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const directory=await mkdtemp(path.join(tmpdir(),'pi-xterm-browser-'));
  const chrome=spawn(executable,['--headless','--no-sandbox','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${directory}`,'about:blank'],{stdio:['ignore','ignore','pipe']});
  let log='',cdp;chrome.stderr.on('data',d=>log+=d);
  try {
    await until(()=>log.includes('DevTools listening'),'Chromium');
    const debug=log.match(/DevTools listening on (ws:\/\/\S+)/)[1];
    const page=await(await fetch(new URL(debug).origin.replace('ws:','http:')+'/json/new?about:blank',{method:'PUT'})).json();
    cdp=new CDP(page.webSocketDebuggerUrl);await cdp.call('Page.enable');await cdp.call('Runtime.enable');
    await cdp.call('Page.addScriptToEvaluateOnNewDocument',{source:`
      window.transports=[];window.clipboardWrites=[];window.clipboardReads=0;
      Object.defineProperty(navigator,'clipboard',{configurable:true,value:{writeText:async text=>window.clipboardWrites.push(text),readText:async()=>{window.clipboardReads++;throw Error('denied');}}});
      window.WebSocket=class {
        static OPEN=1; constructor(url){this.url=String(url);this.active=true;this.readyState=1;this.bufferedAmount=0;this.sent=[];transports.push(this);setTimeout(()=>{this.onopen?.({});const u=new URL(this.url);this.control('ready',{cols:+u.searchParams.get('cols'),rows:+u.searchParams.get('rows')});},15);}
        send(data){this.sent.push(typeof data==='string'?data:Array.from(data));if(typeof data==='string'){const msg=JSON.parse(data);if(msg.type==='activity')this.active=true;if(msg.type==='activity'||msg.type==='resize'&&this.active)this.control('size',msg);if(msg.type==='ping'&&!window.dropPong)this.control('pong');}}
        control(type,fields={}){this.onmessage?.({data:JSON.stringify({...fields,type})});}
        output(text){this.onmessage?.({data:new TextEncoder().encode(text).buffer});}
        close(){this.readyState=3;this.onclose?.({});}
      };
      window.confirm=()=>true;
    `});
    await cdp.call('Page.navigate',{url:`http://127.0.0.1:${server.address().port}`});
    const evaluate=expression=>cdp.evaluate(expression), wait=(expr,label)=>until(()=>evaluate(expr),label);
    await wait(`document.querySelector('#login') && !document.querySelector('#login').hidden`,'login');
    await evaluate(`document.querySelector('#token').value='bad';document.querySelector('#login-form').requestSubmit()`);
    await wait(`document.querySelector('#login-error').textContent==='Invalid token'`,'invalid token');
    await evaluate(`document.querySelector('#token').value='test-token';document.querySelector('#login-form').requestSubmit()`);
    await wait(`!document.querySelector('#instances').hidden && !choosing && document.querySelector('#instance-list [data-desktop]')`,'instance selector');
    assert.equal(await evaluate(`transports.length`),0,'login must not automatically start or take over a TUI');
    assert.equal(await evaluate(`document.querySelector('#instance-list [data-desktop] strong').textContent`),'桌面共享实例');
    assert.equal(instances.size,1,'opening the instance list does not create an instance');
    instances.set('desktop-later',{...desktop,id:'desktop-later',name:'Later desktop'});
    await wait(`!!document.querySelector('#instance-list [data-instance-id="desktop-later"]')`,'automatically discovers a later desktop');
    assert.equal(await evaluate('transports.length'),0,'automatic discovery never takes control');
    instances.delete('desktop-later');
    await wait(`!document.querySelector('#instance-list [data-instance-id="desktop-later"]')`,'automatically removes a departed desktop');
    await evaluate(`document.querySelector('#new-instance').click()`);
    await wait(`!document.querySelector('#projects').hidden && document.querySelectorAll('#project-list button').length===10 && !choosing`,'recent directory selector');
    assert.equal(instances.size,1,'choosing a directory is still lazy');
    assert.equal(await evaluate(`document.querySelectorAll('#project-list img').length`),0,'directory paths are text, never markup');
    await evaluate(`document.querySelector('#project-list button').click()`);
    await wait(`typeof connected!=='undefined' && connected`,'terminal ready');
    assert.equal(await evaluate(`transports.length`),1);
    assert.equal(instances.size,2,'new instance coexists with desktop');
    assert.equal(desktop.projectId,'desktop-project','new instance cannot close or change the desktop');
    assert.equal(await evaluate(`new URL(transports[0].url).pathname`),'/api/instances/'+current.id+'/api/terminal','new terminal uses its own instance API');
    assert.equal(await evaluate(`document.querySelector('#terminal-menu').hidden`),true,'toolbar starts as one floating icon');
    const geometry=await evaluate(`({cols:terminal.cols,rows:terminal.rows,top:document.querySelector('#terminal').getBoundingClientRect().top,height:document.querySelector('#terminal').getBoundingClientRect().height})`);
    assert.equal(geometry.top,0,'no reserved header space');
    await evaluate(`document.querySelector('#menu-toggle').click()`);
    assert.equal(await evaluate(`document.querySelector('#menu-toggle').getAttribute('aria-expanded')`),'true');
    assert.deepEqual(await evaluate(`({cols:terminal.cols,rows:terminal.rows,top:document.querySelector('#terminal').getBoundingClientRect().top,height:document.querySelector('#terminal').getBoundingClientRect().height})`),geometry,'expanding overlay must not resize the PTY');
    await evaluate(`document.querySelector('#terminal').dispatchEvent(new PointerEvent('pointerdown',{bubbles:true}))`);
    assert.equal(await evaluate(`document.querySelector('#terminal-menu').hidden`),true,'outside tap closes menu');
    assert.equal(await evaluate(`terminal.options.theme.background`),'#222e32');
    assert.equal(await evaluate(`getComputedStyle(document.documentElement).backgroundColor`),'rgb(34, 46, 50)');
    assert.equal(await evaluate(`terminal.options.fontFamily`),await evaluate(`getComputedStyle(document.documentElement).fontFamily`));
    assert.match(await evaluate(`terminal.options.fontFamily`),/JetBrainsMono Nerd Font Mono/);
    assert.equal(await evaluate(`Array.from(document.fonts).filter(f=>f.status==='loaded').length`),5,'all embedded styles and CJK fallback loaded before terminal opens');
    const fontURLs=await evaluate(`performance.getEntriesByType('resource').filter(e=>e.name.endsWith('.woff2')).map(e=>new URL(e.name).pathname)`);
    assert.equal(fontURLs.length,5,'font resources are fetched from the server, not local OS fonts');
    assert(!await evaluate(`transports[0].url.includes('token')`));
    assert.equal(await evaluate(`terminal.unicode.activeVersion`),'15-graphemes');
    const wideRanges=JSON.parse(await readFile(path.join(root,'../testdata/unicode-wide-ranges.json'),'utf8'));
    const narrow=await evaluate(`(()=>{const providers=[];new UnicodeGraphemesAddon.UnicodeGraphemesAddon().activate({unicode:{register:p=>providers.push(p),activeVersion:'6'}});const missing=[];for(const [start,end] of ${JSON.stringify(wideRanges)})for(let c=start;c<=end;c++)if(((providers[1].charProperties(c,0)>>1)&3)!==2 && missing.length<20)missing.push(c);return missing;})()`);
    assert.deepEqual(narrow,[],'all Go TUI wide runes occupy two xterm columns');
    const widths=JSON.parse(await readFile(path.join(root,'../testdata/unicode-widths.json'),'utf8'));
    const misaligned=[];
    for (const {text,width} of widths) {
      const result=await evaluate(`new Promise(resolve=>terminal.write('\\x1b[2J\\x1b[H│'+${JSON.stringify(text)}+' '.repeat(${20}- ${width})+'│\\r\\n│'+' '.repeat(20)+'│',()=>resolve({top:terminal.buffer.active.getLine(0).getCell(21)?.getChars(),bottom:terminal.buffer.active.getLine(1).getCell(21)?.getChars()})))`);
      if (result.top!=='│' || result.bottom!=='│') misaligned.push({text,...result});
    }
    assert.deepEqual(misaligned,[],'TUI/xterm Emoji and CJK frame alignment');
    await evaluate(String.raw`transports[0].output('\x1b[2J\x1b[H\x1b[31mREAL TUI\x1b[0m\r\n第二行');`);
    await wait(`terminal.buffer.active.getLine(0)?.translateToString(true)==='REAL TUI'`,'ANSI terminal rendering');
    await cdp.call('Input.dispatchKeyEvent',{type:'keyDown',key:'a',code:'KeyA',text:'a',unmodifiedText:'a'});
    await cdp.call('Input.dispatchKeyEvent',{type:'keyUp',key:'a',code:'KeyA'});
    await cdp.call('Input.dispatchKeyEvent',{type:'keyDown',key:'Escape',code:'Escape',windowsVirtualKeyCode:27});
    await cdp.call('Input.dispatchKeyEvent',{type:'keyDown',key:'c',code:'KeyC',modifiers:2,windowsVirtualKeyCode:67});
    await wait(`transports[0].sent.some(v=>Array.isArray(v)&&v.includes(97)) && transports[0].sent.some(v=>Array.isArray(v)&&v.includes(27)) && transports[0].sent.some(v=>Array.isArray(v)&&v.includes(3))`,'raw keyboard/escape/Ctrl-C');
    await evaluate(`transports[0].output('\x1b[?1000h\x1b[?1006h');`);
    await delay(60);
    const point=await evaluate(`(()=>{const r=document.querySelector('.xterm-screen').getBoundingClientRect();return {x:r.left+120,y:r.top+80};})()`);
    await cdp.call('Input.dispatchMouseEvent',{type:'mousePressed',button:'left',clickCount:1,...point});
    await cdp.call('Input.dispatchMouseEvent',{type:'mouseReleased',button:'left',clickCount:1,...point});
    await wait(`transports[0].sent.some(v=>Array.isArray(v)&&new TextDecoder().decode(new Uint8Array(v)).startsWith('\x1b[<'))`,'raw mouse protocol');
    await evaluate(`transports[0].output('\x1b]52;c;'+btoa('explicit clipboard')+'\x07');`);
    await wait(`pendingCopy==='explicit clipboard'`,'OSC52 captured');assert.equal(await evaluate(`clipboardWrites.length`),0);
    await evaluate(`document.querySelector('#copy').click()`);await wait(`clipboardWrites[0]==='explicit clipboard'`,'explicit copy');
    await evaluate(`transports[0].output('\x1b[?2004h')`);
    await wait(`terminal.modes.bracketedPasteMode`,'bracketed paste mode parsed');
    await evaluate(`document.querySelector('#paste').click()`);
    await wait(`document.querySelector('#clipboard-dialog').open`,'HTTP/denied clipboard fallback');
    await evaluate(`document.querySelector('#clipboard-text').value='pasted text';document.querySelector('#clipboard-form').requestSubmit()`);
    await wait(`transports[0].sent.some(v=>Array.isArray(v)&&new TextDecoder().decode(new Uint8Array(v)).includes('pasted text'))`,'bracketed paste');
    assert(await evaluate(`transports[0].sent.some(v=>Array.isArray(v)&&new TextDecoder().decode(new Uint8Array(v)).includes('\x1b[200~'))`));
    await cdp.call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
    await wait(`desiredSize?.cols < 60 && terminal.cols===desiredSize.cols && terminal.rows===desiredSize.rows`,'mobile shared resize acknowledgement');
    assert(await evaluate(`transports[0].sent.some(v=>typeof v==='string'&&JSON.parse(v).type==='resize')`));
    assert.equal(await evaluate(`document.documentElement.scrollWidth<=innerWidth`),true);
    await cdp.call('Emulation.setTouchEmulationEnabled',{enabled:true,maxTouchPoints:1});
    await evaluate(`transports[0].output('\\x1b[2J\\x1b[HOUTPUT\\x1b['+(terminal.rows-4)+';1H'+['┌'+'─'.repeat(28)+'┐','│EDITOR'+' '.repeat(22)+'│','│'+' '.repeat(28)+'│','│MODEL CONTROL'+' '.repeat(15)+'│','└'+'─'.repeat(28)+'┘'].join('\\r\\n')+'\\x1b['+(terminal.rows-3)+';3H')`);
    await wait(`terminal.buffer.active.getLine(terminal.rows-4)?.translateToString(true).includes('EDITOR')`,'native editor frame');
    const tap=async(col,row)=>{
      const p=await evaluate(`(()=>{const r=terminal.element.querySelector('.xterm-screen').getBoundingClientRect();return {x:r.left+(${col}+.5)*r.width/terminal.cols,y:r.top+(${row}+.5)*r.height/terminal.rows};})()`);
      await cdp.call('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[p]});
      await cdp.call('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
    };
    await tap(15,0);
    assert.equal(await evaluate(`terminal.textarea.readOnly && terminal.textarea.inputMode==='none'`),true,'touching output must not open the keyboard');
    await tap(3,await evaluate(`terminal.rows-2`));
    assert.equal(await evaluate(`terminal.textarea.readOnly`),true,'footer controls are not input');
    await tap(3,await evaluate(`terminal.rows-4`));
    assert.equal(await evaluate(`!terminal.textarea.readOnly && terminal.textarea.inputMode==='text' && document.activeElement===terminal.textarea`),true,'only editor tap enables soft input');
    await tap(15,0);
    assert.equal(await evaluate(`terminal.textarea.readOnly`),true,'output tap closes the input gate again');
    // Real touch input must become coordinate-preserving terminal wheel events,
    // not local scrollback panning, keyboard focus or a click on finger release.
    const swipe=async(dx,dy)=>{
      const p=await evaluate(`(()=>{const r=terminal.element.querySelector('.xterm-screen').getBoundingClientRect();return {x:r.left+15.5*r.width/terminal.cols,y:r.top+8.5*r.height/terminal.rows};})()`);
      const start=await evaluate(`transports[0].sent.length`);
      await cdp.call('Input.dispatchTouchEvent',{type:'touchStart',touchPoints:[p]});
      for(let step=1;step<=3;step++)await cdp.call('Input.dispatchTouchEvent',{type:'touchMove',touchPoints:[{x:p.x+dx*step/3,y:p.y+dy*step/3}]});
      await cdp.call('Input.dispatchTouchEvent',{type:'touchEnd',touchPoints:[]});
      await delay(40);
      return evaluate(`new TextDecoder().decode(Uint8Array.from(transports[0].sent.slice(${start}).filter(Array.isArray).flat()))`);
    };
    await evaluate(`transports[0].output('\\x1b[?1000h\\x1b[?1006h')`);
    await wait(`terminal.modes.mouseTrackingMode==='vt200'`,'touch mouse reporting mode');
    assert.match(await swipe(0,-100),/^(?:\x1b\[<65;16;9M)+$/,'upward finger swipe scrolls down at the starting list coordinate without clicking');
    assert.match(await swipe(0,100),/^(?:\x1b\[<64;16;9M)+$/,'downward finger swipe scrolls up');
    assert.equal(await swipe(90,0),'','horizontal swipes must not scroll or click');
    assert.doesNotMatch(await swipe(0,3),/\x1b\[<6[45];/,'tap jitter must not become a scroll');
    assert.equal(await evaluate(`terminal.textarea.readOnly && terminal.textarea.inputMode==='none'`),true,'scrolling never enables soft input');
    await evaluate(`send(new TextEncoder().encode('x'.repeat(70000)))`);
    assert(await evaluate(`transports[0].sent.filter(Array.isArray).every(v=>v.length<=16384)`));
    await evaluate(`transports[0].close()`);await wait(`transports.length===2 && connected`,'automatic reconnect');
    await evaluate(`transports[1].control('taken_over');transports[1].close()`);await delay(1200);assert.equal(await evaluate(`transports.length`),2);
    await wait(`!choosing && !document.querySelector('#instances').hidden`,'legacy single-controller service returns home');
    await evaluate(`document.querySelector('#instance-list button[data-instance-id="${current.id}"]').click()`);await wait(`transports.length===3 && connected`,'explicit reconnect from instance list');
    await evaluate(`transports[2].control('exit');transports[2].close()`);await delay(1200);assert.equal(await evaluate(`transports.length`),3);
    await evaluate(`document.querySelector('#restart').click()`);await wait(`transports.length===4 && connected`,'explicit restart');assert.equal(restarts,1);
    // A slow parser cannot grow xterm's queue without bound; resume via replay.
    await evaluate(`terminal.write=()=>{};{const chunk='x'.repeat(4*1024*1024);transports[3].output(chunk);transports[3].output(chunk);transports[3].output(chunk);}`);
    assert.equal(await evaluate(`terminal===undefined`),true,'output overflow disconnects the old terminal');
    await wait(`transports.length===5 && connected`,'output overflow reconnect');
    // Clipboard permission may settle only after the user has logged out.
    await evaluate(`pendingCopy='private text';navigator.clipboard.writeText=()=>new Promise((resolve,reject)=>{window.finishCopy=resolve;window.rejectCopy=reject;});document.querySelector('#copy').click()`);
    await wait(`typeof rejectCopy==='function'`,'pending copy');
    await evaluate(`document.querySelector('#logout').click()`);await wait(`!document.querySelector('#login').hidden`,'logout');assert.equal(await evaluate(`terminal===undefined`),true);
    await evaluate(`rejectCopy(new Error('permission denied'))`);await delay(60);
    assert.equal(await evaluate(`document.querySelector('#clipboard-dialog').open`),false,'stale rejection must not reopen private text');
    assert.equal(await evaluate(`document.querySelector('#clipboard-text').value`),'');
    await evaluate(`document.querySelector('#token').value='test-token';document.querySelector('#login-form').requestSubmit()`);
    await wait(`!document.querySelector('#instances').hidden && !choosing`,'instance selector after login');
    assert.equal(await evaluate(`document.querySelectorAll('#instance-list img').length`),0,'instance labels are text, never markup');
    assert(await evaluate(`document.querySelector('#instance-list').textContent.includes('<img src=x onerror=alert(1)>')`));
    await evaluate(`document.querySelector('#instance-list button[data-instance-id="${current.id}"]').click()`);
    await wait(`transports.length===6 && connected`,'login after pending copy');
    await evaluate(`pendingCopy='private text';document.querySelector('#copy').click()`);
    await evaluate(`document.querySelector('#logout').click()`);await wait(`!document.querySelector('#login').hidden`,'second logout');
    await evaluate(`finishCopy()`);await delay(60);
    assert.notEqual(await evaluate(`document.querySelector('#status').textContent`),'已复制','stale success must not change UI');
    assert.equal(await evaluate(`clipboardReads`),1);
    await cdp.call('Page.navigate',{url:`http://127.0.0.1:${server.address().port}/?keep=ok&pk=test-token#anchor`});
    await wait(`typeof choosing!=='undefined' && !choosing && !document.querySelector('#instances').hidden && document.querySelector('#login').hidden`,'pk link automatic login to instance selector');
    assert.equal(await evaluate(`transports.length`),0,'pk login also waits for an explicit instance choice');
    assert.equal(await evaluate(`location.search`),'?keep=ok','URL token removed immediately');
    assert.equal(await evaluate(`location.hash`),'#anchor');
    assert.equal(await evaluate(`document.querySelector('#token').value`),'','token input cleared after successful link login');
    assert.equal(await evaluate(`linkToken`),null);
    const originalProject=current.projectId;
    await evaluate(`document.querySelector('#instance-list button[data-instance-id="${current.id}"]').click()`);
    await wait(`connected && transports.length===1`,'continue existing project');
    assert.equal(current.projectId,originalProject,'continuing the same project reuses the instance');
    await evaluate(`document.querySelector('#change-project').click()`);
    await wait(`!document.querySelector('#projects').hidden && !choosing`,'close and change directory');
    assert.equal(closes,1);
    assert.equal(await evaluate(`terminal===undefined`),true,'old terminal destroyed');
    await delay(1100);
    assert.equal(await evaluate(`transports.length`),1,'closing a project cannot automatically reconnect or restart it');
    await evaluate(`document.querySelector('#project-path').value='invalid';document.querySelector('#project-form').requestSubmit()`);
    await wait(`document.querySelector('#project-error').textContent==='Invalid directory'`,'invalid project remains on selection page');
    await evaluate(`document.querySelector('#project-path').value='/projects/new';document.querySelector('#project-form').requestSubmit()`);
    await wait(`connected && transports.length===2`,'open another project');
    assert.equal(current.cwd,'/projects/new');
    assert.equal(desktop.projectId,'desktop-project','closing/recreating an independent instance leaves desktop running');
    assert(operations.every(op=>op.id!=='main'),'new, restart and close are scoped away from desktop');
    assert.notEqual(current.projectId,originalProject);
    assert.equal(await evaluate(`new URL(transports[1].url).searchParams.get('projectId')`),current.projectId,'connection is bound to selected instance');
    await evaluate(`transports[1].control('project_changed')`);
    await wait(`!document.querySelector('#instances').hidden && !choosing`,'external project change returns to instance selector');
    await delay(1100);
    assert.equal(await evaluate(`transports.length`),2,'external close/change never reconnects blindly');
    // Late create/info responses must not reopen a terminal after navigation.
    let releaseCreate;holdCreate=new Promise(resolve=>releaseCreate=resolve);
    await evaluate(`document.querySelector('#new-instance').click()`);
    await wait(`!document.querySelector('#projects').hidden && !choosing`,'new instance directory picker');
    await evaluate(`document.querySelector('#project-path').value='/projects/slow';document.querySelector('#project-form').requestSubmit()`);
    await until(()=>createWaiting,'held instance creation');
    await evaluate(`void enter()`);
    await wait(`!document.querySelector('#instances').hidden && !choosing`,'navigation during creation');
    releaseCreate();holdCreate=null;await delay(80);
    assert.equal(await evaluate(`transports.length`),2,'late create cannot connect or steal control');
    assert.equal(await evaluate(`terminal===undefined && !document.querySelector('#instances').hidden`),true);
    assert.equal(desktop.projectId,'desktop-project');
    let releaseInfo;holdInfo=new Promise(resolve=>releaseInfo=resolve);
    await evaluate(`document.querySelector('#instance-list button[data-desktop]').click()`);
    await until(()=>infoWaiting,'held instance metadata');
    await evaluate(`document.querySelector('#instances-back').click()`);
    await wait(`!document.querySelector('#instances').hidden && !choosing`,'return to list keeps tasks');
    releaseInfo();holdInfo=null;await delay(80);
    assert.equal(await evaluate(`transports.length`),2,'late info cannot connect after returning to list');
    assert.equal(instances.has('main'),true);
    await evaluate(`document.querySelector('#instance-list button[data-desktop]').click()`);
    await wait(`connected && transports.length===3`,'explicit shared desktop takeover');
    assert.equal(await evaluate(`new URL(transports[2].url).pathname`),'/api/instances/main/api/terminal');
    assert.equal(await evaluate(`project.projectId`),'desktop-project');
    assert.equal(operations.filter(op=>op.type==='select').length,0,'existing instance opens directly without reselecting its directory');
    await evaluate(`document.querySelector('#instances-back').click()`);
    await wait(`!document.querySelector('#instances').hidden && !choosing`,'shared desktop remains after return');
    desktop.projectId='';desktop.status='idle';
    await evaluate(`document.querySelector('#instance-refresh').click()`);
    await wait(`!choosing`,'refresh idle desktop');
    await evaluate(`document.querySelector('#instance-list button[data-desktop]').click()`);
    await wait(`connected && transports.length===4`,'idle desktop initializes only on explicit choice');
    assert.equal(await evaluate(`project.projectId`),'desktop-selected');
    assert.deepEqual(operations.filter(op=>op.type==='select'),[{type:'select',id:'main'}]);
    instances.delete('main');
    await evaluate(`document.querySelector('#reconnect').click()`);
    await wait(`!document.querySelector('#instances').hidden && document.querySelector('#instance-error').textContent==='Instance not found' && !choosing`,'deleted instance returns 404 to list');
    await delay(1100);assert.equal(await evaluate(`transports.length`),4,'missing instance is never blindly reconnected');
    instances.set('main',desktop);
    await evaluate(`document.querySelector('#instance-refresh').click()`);
    await wait(`!choosing`,'restored desktop');
    await evaluate(`document.querySelector('#instance-list button[data-desktop]').click()`);
    await wait(`connected && transports.length===5`,'shared desktop selected again');
    desktop.projectId='externally-changed';
    await evaluate(`document.querySelector('#change-project').click()`);
    await wait(`!document.querySelector('#instances').hidden && document.querySelector('#instance-error').textContent==='Project changed' && !choosing`,'stale close returns 409 to list');
    assert.equal(desktop.projectId,'externally-changed','stale close does not stop changed desktop project');
    await evaluate(`document.querySelector('#instance-list button[data-desktop]').click()`);
    await wait(`connected && transports.length===6`,'shared lifecycle fixture');
    const localSize=await evaluate('({...desiredSize})');
    await evaluate(`transports.at(-1).active=false;transports.at(-1).control('size',{cols:100,rows:40})`);
    await wait('terminal.cols===100 && terminal.rows===40','remote active end controls grid');
    assert.deepEqual(await evaluate('({...desiredSize})'),localSize,'remote size never changes local desired dimensions');
    const activityCount=await evaluate(`transports.at(-1).sent.filter(x=>typeof x==='string'&&JSON.parse(x).type==='activity').length`);
    await evaluate(`transports.at(-1).output('background output');transports.at(-1).output('\\x1b[c')`);
    await delay(80);
    assert.equal(await evaluate(`transports.at(-1).sent.filter(x=>typeof x==='string'&&JSON.parse(x).type==='activity').length`),activityCount,'output and automatic terminal replies do not claim size');
    await evaluate(`terminal.textarea.dispatchEvent(new KeyboardEvent('keydown',{key:'Shift',bubbles:true}))`);
    await wait('terminal.cols===desiredSize.cols && terminal.rows===desiredSize.rows','local input restores local grid');
    const operationsBefore=operations.length;
    await evaluate(`Object.defineProperty(document,'hidden',{configurable:true,get:()=>true});document.dispatchEvent(new Event('visibilitychange'))`);
    assert.equal(await evaluate('connected'),false,'background page releases socket');
    assert.equal(operations.length,operationsBefore,'background page never stops server instance');
    await evaluate(`Object.defineProperty(document,'hidden',{configurable:true,get:()=>false});document.dispatchEvent(new Event('visibilitychange'))`);
    await wait('connected && transports.length===7','foreground resumes shared instance');
    await evaluate(`Object.defineProperty(navigator,'onLine',{configurable:true,get:()=>false});window.dispatchEvent(new Event('offline'))`);
    assert.equal(await evaluate('connected'),false);
    await evaluate(`Object.defineProperty(navigator,'onLine',{configurable:true,get:()=>true});window.dispatchEvent(new Event('online'))`);
    await wait('connected && transports.length===8','network restore reconnects same shared instance');
    await evaluate(`window.normalTimeout=window.setTimeout;window.setTimeout=(fn,ms,...args)=>normalTimeout(fn,ms===20000?20:ms===15000?50:ms,...args);window.dropPong=true;heartbeats(generation)`);
    await wait(`transports.length===9 && connected`,'half-open heartbeat recovers connection');
    await evaluate(`window.setTimeout=normalTimeout;window.dropPong=false;heartbeats(generation)`);
    assert.equal(operations.length,operationsBefore,'reconnect never creates or stops instance');
    await evaluate(`window.dispatchEvent(new PageTransitionEvent('pagehide'))`);
    assert.equal(await evaluate('connected'),false,'page exit releases local connection');
    assert.equal(operations.length,operationsBefore,'page exit preserves backend');
    await evaluate(`window.dispatchEvent(new PageTransitionEvent('pageshow',{persisted:true}))`);
    await wait(`!document.querySelector('#instances').hidden && !choosing`,'bfcache restore shows live instances');
    authenticated=false;
    await evaluate(`document.querySelector('#new-instance').click()`);
    await wait(`!document.querySelector('#login').hidden`,'401 returns to login');
    assert.equal(await evaluate(`transports.length`),9);
    assert.deepEqual(cdp.exceptions,[],'no uncaught browser errors');
    t.diagnostic('Actual vendored xterm exercised in Chromium; transport is mocked, no Pi/model execution.');
  } finally {
    cdp?.close();chrome.kill('SIGTERM');await Promise.race([new Promise(r=>chrome.once('exit',r)),delay(2000)]);if(chrome.exitCode===null)chrome.kill('SIGKILL');
    server.closeAllConnections();await new Promise(r=>server.close(r));await rm(directory,{recursive:true,force:true,maxRetries:5,retryDelay:100});
  }
});
