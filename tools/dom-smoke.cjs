// Browser-independent component tests using actual Go-generated reports.
// Usage: JSDOM_MODULE=/path/to/jsdom RELAYLAB_BIN=/path/to/relaylab node tools/dom-smoke.cjs
const { JSDOM, VirtualConsole } = require(process.env.JSDOM_MODULE || 'jsdom');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const { spawnSync } = require('node:child_process');

const pause = ms => new Promise(r => setTimeout(r, ms));
async function waitFor(fn) { for(let i=0;i<100;i++){if(fn())return;await pause(10)}throw Error('DOM condition timed out'); }

(async()=>{
  const temp=await fs.mkdtemp(path.join(os.tmpdir(),'relay-dom-'));
  const names=['shared_failure','independent','weak_alternative','wrong_labels','overload','lost_ack'];
  const reports={};
  for(const name of names){
    const dir=path.join(temp,name);
    const run=spawnSync(process.env.RELAYLAB_BIN||'./relaylab',['timeline','-preset',name,'-out',dir],{encoding:'utf8'});
    assert.equal(run.status,0,run.stderr);
    reports[name]=JSON.parse(await fs.readFile(path.join(dir,'report.json'),'utf8'));
  }
  const html=await fs.readFile('internal/web/assets/index.html','utf8');
  const js=await fs.readFile('internal/web/assets/app.js','utf8');
  const errors=[],downloads=[];
  const console=new VirtualConsole();console.on('jsdomError',e=>errors.push(e.message));
  const dom=new JSDOM(html,{url:'http://127.0.0.1:8080',pretendToBeVisual:true,runScripts:'outside-only',virtualConsole:console});
  const w=dom.window,d=w.document;
  try{
    w.Blob=global.Blob;
    w.URL.createObjectURL=blob=>{downloads.push(blob);return 'blob:download'};w.URL.revokeObjectURL=()=>{};
    w.HTMLAnchorElement.prototype.click=function(){};
    let failNext=false;
    w.fetch=async(url,options)=>{
      if(url==='/api/presets')return{ok:true,json:async()=>names.map(n=>reports[n].config)};
      if(url==='/api/run'){if(failNext){failNext=false;return{ok:false,text:async()=>'test failure'}}const c=JSON.parse(options.body).config;return{ok:true,json:async()=>reports[c.name]}}
      throw Error('unexpected fetch '+url);
    };
    w.eval(js);
    await waitFor(()=>!d.querySelector('#play').disabled);
    assert.equal(d.querySelectorAll('.preset').length,6);
    const seek=value=>{const el=d.querySelector('#time');el.value=value;el.dispatchEvent(new w.Event('input'))};
    seek(18000);
    const expected=reports.shared_failure.results.find(r=>r.policy==='risk_aware').frames.find(f=>f.time_ms===18000).stats.delivered;
    assert.equal(parseInt(d.querySelector('#right .metric-number').textContent),expected);
    for(const name of names){
      d.querySelector(`.preset[data-name="${name}"]`).click();await waitFor(()=>!d.querySelector('#build').disabled);
      seek(45000);
      assert.equal(d.querySelector('#error').hidden,true,d.querySelector('#error').textContent);
      assert.equal(d.querySelectorAll('#summary tbody tr').length,5);
      assert.equal(d.querySelectorAll('.topology svg').length,2);
      assert.ok(!d.querySelector('#comparison').textContent.includes('NaN'));
    }
    d.querySelector('#exportJSON').click();
    const exported=JSON.parse(await downloads.at(-1).text());assert.equal(exported.config.name,'lost_ack');
    const input=d.querySelector('#import');
    Object.defineProperty(input,'files',{configurable:true,value:[{size:100,text:async()=>JSON.stringify(reports.shared_failure)}]});
    input.dispatchEvent(new w.Event('change'));await waitFor(()=>d.querySelector('#scenarioName').textContent==='Общий отказ');
    assert.equal(d.querySelector('.preset.active').dataset.name,'shared_failure');
    d.querySelector('#step').click();assert.match(d.querySelector('#clock').textContent,/^1\.0/);
    d.querySelector('#play').click();await pause(90);d.querySelector('#play').click();
    assert.ok(parseFloat(d.querySelector('#clock').textContent)>1);
    Object.defineProperty(input,'files',{configurable:true,value:[{size:10,text:async()=>'{}'}]});input.dispatchEvent(new w.Event('change'));await waitFor(()=>!d.querySelector('#error').hidden);
    d.querySelector('#reset').click();assert.match(d.querySelector('#clock').textContent,/^0\.0/);
    failNext=true;d.querySelector('.preset[data-name="lost_ack"]').click();await waitFor(()=>!d.querySelector('#build').disabled);
    assert.equal(d.querySelector('#scenarioName').textContent,'Общий отказ');
    d.querySelector('#step').click();assert.match(d.querySelector('#clock').textContent,/^1\.0/);
    assert.deepEqual(errors,[]);
  }finally{w.close()}
  for(const name of ['shared_failure','lost_ack']){
    const output=path.join(temp,name+'.html');
    const run=spawnSync(process.env.RELAYLAB_BIN||'./relaylab',['offline','-preset',name,'-out',output],{encoding:'utf8'});
    assert.equal(run.status,0,run.stderr);
    const source=await fs.readFile(output,'utf8');
    const offline=new JSDOM(source,{url:'file:///demo.html',pretendToBeVisual:true,runScripts:'outside-only',virtualConsole:console});
    const ow=offline.window,od=ow.document;
    try{
      Object.assign(ow,{Blob:global.Blob,Response:global.Response,DecompressionStream:global.DecompressionStream});
      ow.eval(od.querySelector('body > script').textContent);
      await waitFor(()=>!od.querySelector('#play').disabled);
      assert.equal(od.querySelectorAll('.preset').length,1);
      assert.equal(od.querySelector('#scenarioName').textContent,reports[name].config.title);
      assert.equal(od.querySelector('#build').disabled,true);
      assert.equal(od.querySelector('#budget').disabled,true);
      od.querySelector('#step').click();assert.match(od.querySelector('#clock').textContent,/^1\.0/);
      assert.equal(od.querySelector('#error').hidden,true,od.querySelector('#error').textContent);
    }finally{ow.close()}
  }
  assert.deepEqual(errors,[]);
  await fs.rm(temp,{recursive:true,force:true});
  process.stdout.write('DOM OK: six Go reports, counters, playback, export/import, error recovery, two self-contained offline replays\n');
})().catch(e=>{process.stderr.write(e.stack+'\n');process.exit(1)});
