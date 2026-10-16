const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const root=path.resolve(__dirname,'..'), out=path.join(root,'artifacts','native-acceptance');
const profile=fs.mkdtempSync(path.join(require('node:os').tmpdir(),'monitor-native-acceptance-'));
const go=require('./go.cjs');
const env={...process.env,MONITOR_NATIVE_FIXTURE:profile,MONITOR_NATIVE_CAPTURE:out};
function run(file,args,options={}){const r=spawnSync(file,args,{cwd:root,env,windowsHide:true,stdio:'inherit',...options});if(r.error)throw r.error;assert.equal(r.status,0,`${file} ${args.join(' ')} exited ${r.status}`);return r;}
try {
  // A failed run must never reuse screenshots or a report from an older build.
  assert.equal(path.dirname(out),path.join(root,'artifacts'));
  fs.rmSync(out,{recursive:true,force:true});
  run(go,['run','./cmd/fixture',profile]);fs.mkdirSync(out,{recursive:true});
  run(go,['test','-v','./...']);
  run(process.execPath,[path.join(root,'scripts','native.cjs'),'build']);
  const exe=path.join(root,'build','native','Codex Monitor Native.exe');
  run(exe,['--offline','--data',profile,'--home',profile,'--smoke','--smoke-widget','--capture-dir',out],{timeout:90000});
  const result=JSON.parse(fs.readFileSync(path.join(out,'result.json'),'utf8'));assert.equal(result.native,true);assert.equal(result.error,'');
  for(const key of ['insideWorkArea','captionVisible','resizable','movable','minimizable','maximizable','closable','moveWorked','resizeWorked','minimizeWorked','maximizeWorked','closeToTrayWorked'])assert.equal(result.window[key],true,`window ${key}`);
  assert.equal(result.page,4);assert.equal(result.rendered,4);assert.equal(result.widgetVerified,true);
  const calls=JSON.parse(fs.readFileSync(path.join(out,'calls-result.json'),'utf8'));
  assert.equal(calls.verified,true,'real native call view');assert.equal(calls.rows,50);assert.ok(calls.pagination.total>50);
  assert.ok(fs.statSync(path.join(out,'calls.png')).size>1000);
  const widget=JSON.parse(fs.readFileSync(path.join(out,'widget-result.json'),'utf8'));
  const switching=JSON.parse(fs.readFileSync(path.join(out,'switch-performance.json'),'utf8'));
  const appearance=JSON.parse(fs.readFileSync(path.join(out,'appearance-result.json'),'utf8'));
  assert.equal(appearance['history-ready'],true);
  assert.equal(appearance['history-dark'],true);
  for(const theme of ['light','dark']) {
    assert.equal(appearance[`theme-${theme}`],true,`native ${theme} appearance`);
    for(const group of ['general','accounts','prices','data']) {
      assert.equal(appearance[`settings-${group}-${theme}`],true);
      assert.ok(fs.statSync(path.join(out,`settings-${group}-${theme}.png`)).size>1000);
    }
  }
  assert.equal(switching.updaterConfigured,true,'signed native updater is configured');
  assert.equal(switching.rapidReverse,true,'rapid window reversal');
  for(const key of ['layered','fixedSize','rendered','perPixelAlpha','noBrowserChildren','orbHit','transparentHit','keyboardExpand','expiredUnknown','errorVisible','reducedMotion','retryRecovered','topmostToggle','hidePersisted','dragWorked','dragDidNotExpand','idleBuffersReleased','coldRestored'])assert.equal(widget[key],true,`widget ${key}`);
  for(const theme of ['light','dark'])assert.equal(widget[`theme-${theme}`],true,`widget ${theme} appearance`);
  for(const state of ['card','card-light','card-dark','hover','morph','orb','orb-dark','english-expired','disconnected','cold'])assert.ok(fs.statSync(path.join(out,`widget-${state}.png`)).size>1000);
  for(const page of ['overview','activity','history','quota','settings'])assert.ok(fs.statSync(path.join(out,`${page}.png`)).size>1000);
  const hashes=new Set(['overview','activity','history','quota','settings'].map(page=>require('node:crypto').createHash('sha256').update(fs.readFileSync(path.join(out,`${page}.png`))).digest('hex')));assert.equal(hashes.size,5,'each page must actually be redrawn');
  if(!result.reducedMotion)assert.notDeepEqual(fs.readFileSync(path.join(out,'history-enter.png')),fs.readFileSync(path.join(out,'history.png')),'page/lens motion must produce an intermediate frame');
  fs.writeFileSync(path.join(out,'acceptance.json'),JSON.stringify({passed:true,backend:'Go / modernc SQLite',validation:'Go tests + native pages + Win32 widget + window controls',nativeWindow:result.native,pages:5,window:result.window,widget,reducedMotion:result.reducedMotion,animatedPageFrames:!result.reducedMotion},null,2));
  console.log(`Native acceptance passed: ${out}`);
} finally {fs.rmSync(profile,{recursive:true,force:true,maxRetries:15,retryDelay:100});}
