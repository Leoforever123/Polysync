// Run after building dist/polysync-windows-amd64.exe (0.4.0) and .cache/polysync-upgrade-fixture.exe (0.4.1).
// Installs a separate test product, with its own registry key, shortcuts and workspace installation folder.
const fs=require('node:fs'),path=require('node:path'),net=require('node:net'),assert=require('node:assert/strict');
const{spawn,execFileSync}=require('node:child_process');const{once}=require('node:events');const{createHash}=require('node:crypto');
const root=path.resolve(__dirname,'..'),dir=fs.mkdtempSync(path.join(root,'.cache/msi-test-'));
const wix=path.join(root,'.cache/wix4/wix.exe'),msiexec=path.join(process.env.SystemRoot,'System32/msiexec.exe');
const installed=path.join(dir,'installed','polysync.exe'),dataDir=path.join(dir,'user-data'),cache=path.join(dir,'updates');fs.mkdirSync(cache,{recursive:true});
const packages=[path.join(dir,'old.msi'),path.join(dir,'new.msi')];let installedPackage='',parent,helper;
const sleep=ms=>new Promise(r=>setTimeout(r,ms));
function run(exe,args){return new Promise((resolve,reject)=>{const p=spawn(exe,args,{windowsHide:true,stdio:'inherit'});p.on('error',reject);p.on('exit',code=>resolve(code));});}
async function port(){const s=net.createServer();await new Promise(r=>s.listen(0,'127.0.0.1',r));const n=s.address().port;await new Promise(r=>s.close(r));return n;}
async function until(fn,label){for(let i=0;i<600;i++){const v=await fn();if(v)return v;await sleep(200);}throw Error('Timed out: '+label);}
function stopInstalled(){const script=`$target = '${installed.replaceAll("'","''")}'; Get-CimInstance Win32_Process -Filter "Name='polysync.exe'" | Where-Object { $_.ExecutablePath -eq $target } | ForEach-Object { Stop-Process -Id $_.ProcessId -ErrorAction Stop }`;execFileSync('powershell.exe',['-NoProfile','-Command',script],{windowsHide:true});}
(async()=>{console.log('MSI TEST DIRECTORY',dir);try{
 const existing=execFileSync('powershell.exe',['-NoProfile','-Command',"Test-Path 'HKCU:\\Software\\PolySync.PackagingTest'"],{windowsHide:true,encoding:'utf8'}).trim();assert.equal(existing,'False','Prior test installation exists; inspect before retrying.');
 for(let i=0;i<2;i++){const code=await run(wix,['build',path.join(root,'packaging/windows/PolySync.wxs'),'-arch','x64','-culture','zh-CN','-ext','WixToolset.UI.wixext','-d','Version=0.4.'+i,'-d','ProductName=PolySync Packaging Test','-d','InstallName=PolySync.PackagingTest','-d','UpgradeCode=06AF30D0-B78A-4B64-84BC-BEC1EE58D44D','-d','ApplicationGuid=AD1769BA-DAE5-4C1F-B87E-63349F74CA09','-d','Binary='+path.join(root,i?'.cache/polysync-upgrade-fixture.exe':'dist/polysync-windows-amd64.exe'),'-d','Notice='+path.join(root,'packaging/windows/notice.rtf'),'-o',packages[i]]);assert.equal(code,0,'MSI fixture build');}
 assert.equal(await run(msiexec,['/i',packages[0],'/qn','/norestart','INSTALLFOLDER='+path.dirname(installed),'/L*v',path.join(dir,'install.log')]),0);installedPackage=packages[0];assert.ok(fs.existsSync(installed));
 for(const name of ['LICENSE','THIRD_PARTY_NOTICES.txt']) assert.deepEqual(fs.readFileSync(path.join(path.dirname(installed),name)),fs.readFileSync(path.join(root,name)),'installed notice: '+name);
 const ui=await port(),tcp=await port(),base='http://127.0.0.1:'+ui,args=['-data-dir',dataDir,'-ui','127.0.0.1:'+ui,'-listen','127.0.0.1:'+tcp,'-tray=false','-open=false'];
 parent=spawn(installed,args,{windowsHide:true,stdio:'ignore'});await until(async()=>{try{return(await fetch(base+'/api/update')).ok;}catch{return false;}},'installed app');
 const configPath=path.join(dataDir,'config.json'),before=JSON.parse(fs.readFileSync(configPath,'utf8'));fs.writeFileSync(path.join(dataDir,'user-sentinel.txt'),'must survive uninstall');
 const helperExe=path.join(dir,'helper.exe');fs.copyFileSync(path.join(root,'dist/polysync-windows-amd64.exe'),helperExe);
 const jobPath=path.join(dir,'job.json');fs.writeFileSync(jobPath,JSON.stringify({parentPid:parent.pid,package:packages[1],digest:createHash('sha256').update(fs.readFileSync(packages[1])).digest('hex'),cache,installed,previous:installed,args}));
 helper=spawn(helperExe,['-apply-update',jobPath],{windowsHide:true,stdio:'ignore'});const helperDone=once(helper,'exit');await until(()=>fs.existsSync(jobPath+'.ready'),'helper ready');
 const parentDone=once(parent,'exit');parent.kill();await parentDone;const [helperCode]=await helperDone;installedPackage=packages[1];assert.equal(helperCode,0,'real update helper');
 await until(async()=>{try{const s=await(await fetch(base+'/api/update')).json();return s.current==='0.4.1'?s:false;}catch{return false;}},'updated app restarted');
 const installResult=JSON.parse(fs.readFileSync(path.join(cache,'last-result.json'),'utf8'));assert.equal(installResult.success,true);
 const after=JSON.parse(fs.readFileSync(configPath,'utf8'));assert.ok(before.deviceId);assert.ok(before.identityPrivateKey);assert.equal(after.deviceId,before.deviceId);assert.equal(after.identityPrivateKey,before.identityPrivateKey);assert.deepEqual(after.pairedDevices,before.pairedDevices);assert.deepEqual(after.shares,before.shares);
 stopInstalled();const downgrade=await run(msiexec,['/i',packages[0],'/qn','/norestart','INSTALLFOLDER='+path.dirname(installed),'/L*v',path.join(dir,'downgrade.log')]);assert.notEqual(downgrade,0,'downgrade must be rejected');
 assert.equal(await run(msiexec,['/x',packages[1],'/qn','/norestart','/L*v',path.join(dir,'uninstall.log')]),0);installedPackage='';assert.equal(fs.existsSync(installed),false);assert.ok(fs.existsSync(configPath));assert.equal(fs.readFileSync(path.join(dataDir,'user-sentinel.txt'),'utf8'),'must survive uninstall');
 console.log('MSI integration passed: per-user install, real helper upgrade/restart, identity/config retention, downgrade rejection, uninstall preserves data. Logs: '+dir);
 }finally{if(parent&&parent.exitCode===null)parent.kill();if(helper&&helper.exitCode===null)helper.kill();stopInstalled();if(installedPackage)await run(msiexec,['/x',installedPackage,'/qn','/norestart']);}})().catch(e=>{console.error(e);process.exitCode=1;});
