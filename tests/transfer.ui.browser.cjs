// Independent browser UI checks using controlled API fixtures.
// Run with NODE_PATH pointing to Playwright; all files are served from the repository.
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), http = require("node:http");
const { execFileSync } = require("node:child_process");
const root = path.resolve(__dirname, ".."), staticDir = path.join(root, "internal/webui/static"), output = path.join(root, ".cache/ui-review");
const status = { protocolVersion:2,trayEnabled:true,device:{id:"local",name:"我的电脑",platform:"windows",addresses:["127.0.0.1:45123"]},shares:[],activities:[],pairedDevices:[],nearbyDevices:[],pairingRequests:[],shareInvitations:[],conflicts:[] };
let settings = { autoReceive: true, directory: "D:\\PolySync Inbox" }, tasks=[];
const server = http.createServer((req,res) => {
 if (req.url==="/api/status" || req.url==="/api/transfers") {res.setHeader("Content-Type","application/json");res.end(JSON.stringify(req.url==="/api/status"?status:{settings,tasks}));return;}
 const name = req.url==="/"?"index.html":req.url.slice(1);
 if (!["index.html","app.js","transfer.js","tour.js","update.js","app.css","transfer.css"].includes(name)) {res.writeHead(404);res.end();return;}
 res.setHeader("Content-Type",name.endsWith(".css")?"text/css":name.endsWith(".js")?"text/javascript":"text/html");res.end(fs.readFileSync(path.join(staticDir,name)));
});
(async()=>{
 await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
 const base="http://127.0.0.1:"+server.address().port;
 const browser=await chromium.launch({channel:process.env.POLYSYNC_TEST_BROWSER_CHANNEL || "chrome",headless:true});
 fs.mkdirSync(output,{recursive:true});
 try {
  // A child runner cannot use this same Node server synchronously; launch asynchronously.
  const {spawn}=require("node:child_process");
  await new Promise((resolve,reject)=>{const c=spawn(process.execPath,[path.join(__dirname,"ui.browser.cjs")],{env:{...process.env,POLYSYNC_TEST_URL:base},windowsHide:true,stdio:"inherit"});c.on("error",reject);c.on("exit",code=>code===0?resolve():reject(new Error("Existing UI suite failed: "+code)));});
  const page=await browser.newPage({viewport:{width:1365,height:1050}});
  const errors=[];page.on("pageerror",error=>errors.push(error.message));
  await page.goto(base);await page.locator("#tour-start").click();
  for(let i=1;i<=8;i++){
   assert.equal(await page.locator("#tour-count").textContent(),i+" / 8");
   const r=await page.locator("#tour-bubble").boundingBox();assert.ok(r.x>=0&&r.y>=0&&r.x+r.width<=1365&&r.y+r.height<=1050);
   if(i===2){await page.locator("#tour-prev").click();assert.equal(await page.locator("#tour-count").textContent(),"1 / 8");await page.locator("#tour-next").click();}
   if(i===4) assert.match(await page.locator('#tour-copy').textContent(), /发送邀请/);
   if(i===5) assert.match(await page.locator('#tour-copy').textContent(), /接受并同步/);
   if(i===6) assert.match(await page.locator('#tour-copy').textContent(), /立即同步/);
   if(i===1) assert.match(await page.locator('#tour-spotlight').evaluate(n=>getComputedStyle(n).boxShadow), /0\.38/);
   if(i===4) await page.screenshot({path:path.join(output,'tour-sync-desktop.png')});
   if(i===1)await page.screenshot({path:path.join(output,"tour-desktop.png")});
   await page.locator("#tour-next").click();
  }
  await page.reload();assert.equal(await page.locator("#tour-invite").isVisible(),false);
  await page.setViewportSize({width:390,height:844});await page.locator("#help-tour").click();
  await page.keyboard.press("Escape");assert.equal(await page.evaluate(()=>document.activeElement.id),"help-tour");
  await page.locator("#help-tour").click();await page.locator("#tour-skip").focus();await page.keyboard.press("Shift+Tab");assert.equal(await page.evaluate(()=>document.activeElement.id),"tour-next");
  for(let i=1;i<=8;i++){
   const r=await page.locator("#tour-bubble").boundingBox();assert.ok(r.x>=0&&r.y>=0&&r.x+r.width<=390&&r.y+r.height<=844);
   if(i===3)await page.screenshot({path:path.join(output,"tour-mobile.png")});
   await page.locator("#tour-next").click();
  }
  status.trayEnabled=false;
  await page.reload(); await page.locator('#help-tour').click();
  for(let i=1;i<8;i++) await page.locator('#tour-next').click();
  assert.match(await page.locator('#tour-copy').textContent(), /Ctrl\+C/);
  await page.emulateMedia({reducedMotion:'reduce'});
  assert.equal(await page.locator('#tour-bubble').evaluate(n=>getComputedStyle(n).animationName),'none');
  await page.locator('#tour-next').click();
  await page.locator('[data-workspace="transfer"]').click();
  await page.getByRole("heading",{name:"文件到了，就在这里。"}).waitFor();
  await page.screenshot({path:path.join(output,"transfer-mobile-empty.png"),fullPage:true});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.setViewportSize({width:1365,height:1050});await page.screenshot({path:path.join(output,"transfer-desktop-empty.png"),fullPage:true});
  await page.route("**/api/transfer-settings",async route=>{
   assert.equal(route.request().method(),"PUT");settings=route.request().postDataJSON();await route.fulfill({json:settings});
  });
  await page.locator("#transfer-settings").click();await page.locator("#transfer-auto").uncheck();
  await page.locator("#transfer-directory").fill("E:\\Receipts");
  await page.getByRole("button",{name:"保存设置",exact:true}).click();await page.getByRole("dialog").waitFor({state:"hidden"});
  assert.equal(settings.autoReceive,false);assert.equal(settings.directory,"E:\\Receipts");
  await page.locator("#transfer-settings").click();assert.equal(await page.locator("#transfer-directory").inputValue(),settings.directory);
  await page.evaluate(() => startTour());
  assert.equal(await page.locator("#tour-layer").isVisible(), false);
  assert.equal(await page.locator("#transfer-directory").inputValue(), settings.directory);
  await page.keyboard.press("Escape"); await page.locator("#tour-bubble").waitFor();
  await page.locator("#tour-skip").click(); await page.locator('[data-workspace="transfer"]').click();
  status.pairedDevices=[{id:"peer",name:"另一台电脑",online:true,publicKey:"test"}];
  await page.waitForFunction(()=>!!document.querySelector('#transfer-peer option[value="peer"]'));
  await page.locator("#transfer-peer").selectOption("peer");
  await page.locator("#transfer-files").setInputFiles([{name:"sample.txt",mimeType:"text/plain",buffer:Buffer.from("browser upload")},{name:"empty.txt",mimeType:"text/plain",buffer:Buffer.alloc(0)}]);
  await page.locator('[data-remove-file="1"]').click();assert.equal(await page.locator("#transfer-selection li").count(),1);
  await page.locator("#transfer-files").setInputFiles([{name:"sample.txt",mimeType:"text/plain",buffer:Buffer.from("browser upload")}]);
  assert.equal(await page.locator("#transfer-selection li").count(),2);
  await page.locator('[data-remove-file="1"]').click();
  let uploaded=false;
  await page.route("**/api/transfers",async route=>{
   if(route.request().method()==="POST"){const data=route.request().postDataJSON();assert.equal(data.peerId,"peer");assert.equal(data.files[0].name,"sample.txt");await route.fulfill({status:201,json:{id:"new-task"}});}else await route.continue();
  });
  await page.route("**/api/transfers/new-task/upload",async route=>{
   assert.equal(route.request().method(),"PUT");assert.match(route.request().headers()["content-type"],/multipart\/form-data/);
   assert.match(route.request().postDataBuffer().toString(),/browser upload/);uploaded=true;await route.fulfill({status:202,json:{id:"new-task"}});
  });
  await page.locator("#transfer-send").click();await page.getByText("已交给后台，现在可以关闭页面。",{exact:true}).waitFor();assert.equal(uploaded,true);
  tasks=[{id:"received",peerId:"peer",peerName:"另一台电脑",direction:"receive",state:"complete",created:"2026-09-08T10:00:00+08:00",total:13,done:13,storageDir:"E:\\Receipts\\polysync-received",files:[{name:"设计资料.txt",size:13,state:"complete"},{name:"empty.txt",size:0,state:"complete"}]}];
  await page.locator('[data-transfer-id="received"]').waitFor();
  await page.locator('[data-transfer-action="save"]').first().focus();
  await page.locator('[data-transfer-id="received"]').evaluate(node=>node.testIdentity=true);
  await page.waitForTimeout(2700);
  assert.equal(await page.locator('[data-transfer-id="received"]').evaluate(node=>node.testIdentity),true);
  await page.screenshot({path:path.join(output,"transfer-desktop-received.png"),fullPage:true});
  let saved=false;
  await page.route("**/api/transfers/received/files/0/save",async route=>{saved=true;await route.fulfill({json:{path:"E:\\Saved\\设计资料.txt",cancelled:false}});});
  await page.locator('[data-transfer-action="save"]').first().click();assert.equal(saved,true);
  await page.setViewportSize({width:390,height:844});await page.screenshot({path:path.join(output,"transfer-mobile-received.png"),fullPage:true});assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  await page.emulateMedia({reducedMotion:"reduce"});assert.equal(await page.locator(".transfer-task").evaluate(node=>getComputedStyle(node).animationName),"none");
  let removed=false;
  await page.route("**/api/transfers/received?files=false",async route=>{assert.equal(route.request().method(),"DELETE");removed=true;tasks=[];await route.fulfill({json:{ok:true}});});
  await page.locator('[data-transfer-action="remove"]').click();assert.equal(await page.locator("#transfer-delete-files").isChecked(),false);
  await page.locator("#transfer-remove-form").getByRole("button",{name:"移除记录",exact:true}).click();await page.getByRole("dialog").waitFor({state:"hidden"});assert.equal(removed,true);

  let updateState={current:'0.4.0',phase:'idle',message:'点击检查更新',supported:true,available:false,token:'fixture-token'};
  let updateReads=0,installAttempts=0;
  await page.route('**/api/update',async route=>{updateReads++;await route.fulfill({json:updateState});});
  await page.route('**/api/update/*',async route=>{
   assert.equal(route.request().headers()['x-polysync-update-token'],'fixture-token');
   const action=route.request().url().split('/').at(-1);
   if(action==='check')updateState={...updateState,phase:'available',latest:'0.4.1',available:true,message:'发现新版本'};
   if(action==='download')updateState={...updateState,phase:'ready',downloaded:100,total:100,message:'下载和校验完成'};
   if(action==='install'){installAttempts++;if(installAttempts===1){updateState.message='仍有同步正在进行，请完成后再安装';await route.fulfill({status:409,json:{error:updateState.message}});return;}updateState={...updateState,phase:'installing',message:'正在重启'};}
   await route.fulfill({json:updateState});
  });
  await page.setViewportSize({width:1365,height:1050});
  await page.locator('#open-update').click();await page.locator('#update-check').waitFor();
  await page.waitForFunction(()=>!document.querySelector('#update-check').disabled);
  assert.equal(await page.locator('#update-install').isDisabled(),true);
  await page.locator('#update-check').click();await page.getByText('发现新版本',{exact:true}).waitFor();
  await page.locator('#update-download').focus();await page.waitForTimeout(1700);
  assert.equal(await page.evaluate(()=>document.activeElement.id),'update-download');
  await page.screenshot({path:path.join(output,'update-desktop.png')});
  await page.locator('#update-download').click();await page.waitForFunction(()=>!document.querySelector('#update-install').disabled);
  await page.locator('#update-install').click();await page.getByText('仍有同步正在进行，请完成后再安装',{exact:true}).waitFor();
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
  assert.equal(await page.locator('#update-install').evaluate(node=>getComputedStyle(node).whiteSpace),'nowrap');
  const checkBox=await page.locator('#update-check').boundingBox(),installBox=await page.locator('#update-install').boundingBox();
  assert.ok(installBox.y>checkBox.y+checkBox.height,'narrow screen update buttons stack vertically');
  await page.screenshot({path:path.join(output,'update-mobile.png')});
  await page.waitForFunction(()=>!document.querySelector('#update-install').disabled);
  await page.locator('#update-install').click();await page.getByText('正在重启',{exact:true}).waitFor();
  await page.keyboard.press('Escape');assert.equal(await page.evaluate(()=>document.activeElement.id),'open-update');
  const readsAfterClose=updateReads;await page.waitForTimeout(1900);assert.equal(updateReads,readsAfterClose);
  const longPage = await browser.newPage({viewport:{width:1024,height:780}, colorScheme:"dark"});
  longPage.on("pageerror", error => errors.push(error.message));
  const item = {peerId:"peer", peerName:"工作电脑", direction:"send", created:"2026-09-08T10:00:00Z",
    total:1000, done:1000, state:"complete", storageDir:"/tmp/polysync", files:[{name:"报告.txt", size:1000,state:"complete"}]};
  tasks = Array.from({length:4000}, (_, i) => ({...item, id:"history-" + i}));
  tasks[0] = {...tasks[0], state:"sending", done:0};
  let reads = 0;
  await longPage.route("**/api/transfers", async route => {
    reads++;
    tasks[0].done = Math.min(990, tasks[0].done + 50);
    await route.fulfill({json:{settings,tasks}});
  });
  await longPage.goto(base + "/#transfer");
  await longPage.locator("#transfer-task-history-0 progress").waitFor();
  assert.equal(await longPage.locator(".transfer-task").count(), 24);
  const cancel = longPage.locator("#transfer-task-history-0 [data-transfer-action=cancel]");
  await cancel.focus();
  const amount = await longPage.locator("#transfer-task-history-0 progress").evaluate(n => n.value);
  await longPage.evaluate(() => refreshTransfers());
  assert.ok(await longPage.locator("#transfer-task-history-0 progress").evaluate(n => n.value) > amount);
  assert.equal(await cancel.evaluate(n => n === document.activeElement), true);
  tasks[0].state = "complete";
  await longPage.evaluate(() => refreshTransfers());
  assert.equal(await longPage.locator("#transfer-task-history-0 progress").count(), 0);
  assert.equal(await longPage.locator("#transfer-task-history-0").evaluate(n => n === document.activeElement), true);
  await longPage.locator("#transfer-more").click();
  assert.equal(await longPage.locator(".transfer-task").count(), 48);
  await longPage.goto(base + "/#transfer/task/history-3999");
  await longPage.locator("#transfer-task-history-3999:focus").waitFor();
  assert.ok(await longPage.locator(".transfer-task").count() <= 49);
  await longPage.emulateMedia({reducedMotion:"no-preference"});
  const samples = await longPage.evaluate(async () => {
    const samples = [];
    let last;
    for (let i = 0; i < 151; i++) {
      const current = await new Promise(resolve => requestAnimationFrame(resolve));
      if (last !== undefined) samples.push(current - last);
      last = current;
      window.scrollBy(0, -4);
      if (i === 40 || i === 100) void refreshTransfers();
    }
    samples.sort((a,b) => a-b);
    return {frames:samples.length, medianMs:samples[75], p95Ms:samples[142], above33ms:samples.filter(n=>n>33.34).length};
  });
  console.log("4,000-record dark-mode Chromium scrolling with two requested refreshes:", JSON.stringify(samples));
  await longPage.setViewportSize({width:320,height:690});
  await longPage.emulateMedia({reducedMotion:"reduce"});
  assert.equal(await longPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth),true);
  await longPage.locator('[data-workspace="sync"]').click();
  await longPage.screenshot({path:path.join(output,"dark-narrow-sync.png"),animations:"disabled",fullPage:true});
  const before = reads;
  await longPage.waitForTimeout(2700);
  assert.equal(reads, before);
  await longPage.close();
  assert.deepEqual(errors,[]);
  console.log("Transfer UI checks passed: all tutorial steps, replay/skip/focus, desktop/mobile, settings, multipart upload, received files, save dispatch, polling focus, removal, reduced motion.");
 }finally{await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(error=>{console.error(error);process.exitCode=1;server.close();});
