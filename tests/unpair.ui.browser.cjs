// Local fixture server only; no production devices or folders are used.
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), http = require("node:http");
const root = path.resolve(__dirname, ".."), staticDir = path.join(root, "internal/webui/static");
const output = path.join(root, ".cache/ui-review/unpair");
const peer = {id:"peer",name:'测试电脑 <设备>',online:false,publicKey:'test-key'};
const status = {protocolVersion:2,device:{id:"local",name:"本机",platform:"windows",addresses:["127.0.0.1:45123"]},shares:[],activities:[],pairedDevices:[],nearbyDevices:[],pairingRequests:[],shareInvitations:[],conflicts:[]};
let deletes = 0, busy = false;
const server = http.createServer((req,res) => {
  const json = (value, code=200) => {res.writeHead(code, {"Content-Type":"application/json"});res.end(JSON.stringify(value));};
  if(req.url === "/api/status") return json(status);
  if(req.url === "/api/transfers") return json({tasks:[],settings:{autoReceive:true}});
  if(req.url === "/api/paired-devices/peer" && req.method === "DELETE") {
    deletes++;
    if(busy) return json({error:"仍有设备操作或文件任务正在进行，请完成或取消任务后重试"},409);
    status.pairedDevices=[];
    status.shares.forEach(s => {s.state="unpaired";s.autoSync=false;});
    return json({ok:true});
  }
  const file = req.url === "/" ? "index.html" : req.url.slice(1);
  if(!["index.html","app.js","app.css","transfer.js","transfer.css","tour.js","update.js"].includes(file)) {res.writeHead(404);res.end();return;}
  res.writeHead(200,{"Content-Type":file.endsWith(".js")?"text/javascript":file.endsWith(".css")?"text/css":"text/html"});
  res.end(fs.readFileSync(path.join(staticDir,file)));
});
(async () => {
  fs.mkdirSync(output,{recursive:true});
  await new Promise(resolve=>server.listen(0,"127.0.0.1",resolve));
  let browser;
  try {
    browser = await chromium.launch({channel:"chrome",headless:true});
    const page = await browser.newPage({viewport:{width:1365,height:1000}});
    const errors=[];page.on("pageerror",e=>errors.push(e.message));
    await page.addInitScript(()=>localStorage.setItem("polysync-tour-v1","done"));
    await page.goto(`http://127.0.0.1:${server.address().port}`);
    await page.getByRole("heading",{name:"正在发现附近设备"}).waitFor();
    assert.equal(await page.locator('.unpair-device').count(),0);
    await page.screenshot({animations:"disabled",path:path.join(output,"desktop-empty.png"),fullPage:true});
    status.pairedDevices=[peer];
    status.shares=[{id:"workspace",name:"本地资料",path:"C:\\Test\\Docs",peerDeviceId:"peer",state:"active",autoSync:true,status:{state:"idle"}}];
    await page.evaluate(()=>refresh());
    const button=page.locator('.unpair-device');
    await button.focus();
    await button.evaluate(node=>node.identity="kept");
    peer.online=true;
    await page.evaluate(()=>refresh());
    assert.equal(await button.evaluate(node=>node.identity),"kept");
    assert.equal(await button.evaluate(node=>document.activeElement===node),true);
    await page.keyboard.press("Enter");
    await page.getByRole("heading",{name:"取消配对",exact:true}).waitFor();
    await page.waitForFunction(()=>document.activeElement.textContent==="保留配对");
    assert.match(await page.locator('#modal-content').innerText(),/对方需单独取消配对/);
    assert.equal(await page.locator('#modal-content img').count(),0);
    await page.screenshot({animations:"disabled",path:path.join(output,"desktop-confirm.png"),fullPage:true});
    await page.keyboard.press("Escape");
    assert.equal(deletes,0);
    assert.equal(await button.evaluate(node=>document.activeElement===node),true);
    await button.click();
    await page.getByRole("button",{name:"保留配对",exact:true}).click();
    assert.equal(deletes,0);
    await page.setViewportSize({width:390,height:844});
    await page.emulateMedia({reducedMotion:"reduce"});
    await button.click();
    await page.waitForFunction(()=>document.activeElement.textContent==="保留配对");
    await page.locator('#close-modal').focus();
    await page.keyboard.press("Shift+Tab");
    assert.equal(await page.evaluate(()=>document.activeElement.type),"submit");
    busy=true;
    await page.keyboard.press("Enter");
    await page.locator('#unpair-error').filter({hasText:"仍有设备操作"}).waitFor();
    assert.equal(await page.locator('#unpair-form button[type=submit]').isEnabled(),true);
    assert.equal(status.pairedDevices.length,1);
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
    assert.equal(await page.locator('.hero').evaluate(n=>getComputedStyle(n).animationName),"none");
    await page.screenshot({animations:"disabled",path:path.join(output,"narrow-busy.png"),fullPage:true});
    busy=false;
    await page.locator('#unpair-form button[type=submit]').click();
    await page.waitForFunction(()=>!document.querySelector('.unpair-device'));
    assert.equal(deletes,2);
    assert.equal(await page.locator('[data-share-id="workspace"] .sync-button').isDisabled(),true);
    assert.match(await page.locator('[data-share-id="workspace"]').innerText(),/已取消配对/);
    assert.equal(await page.evaluate(()=>document.activeElement.id),"add-device");
    await page.screenshot({animations:"disabled",path:path.join(output,"narrow-unpaired.png"),fullPage:true});
    assert.deepEqual(errors,[]);
    console.log("Unpair browser checks passed: desktop/narrow, empty, confirmation/cancel, keyboard/focus, busy retry, retained workspace, reduced motion and escaping.");
  } finally { if(browser) await browser.close(); await new Promise(resolve=>server.close(resolve)); }
})().catch(error=>{console.error(error);process.exitCode=1;});
