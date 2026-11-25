// Browser checks: npm install --prefix .cache/ui-tools --no-save playwright
// NODE_PATH=.cache/ui-tools/node_modules node tests/ui.browser.cjs
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
(async () => {
 const browser = await chromium.launch({ channel: process.env.POLYSYNC_TEST_BROWSER_CHANNEL || "chrome", headless: true });
 const base = process.env.POLYSYNC_TEST_URL || "http://127.0.0.1:45324";
 const output = path.join(__dirname, "../.cache/ui-review");
 fs.mkdirSync(output, { recursive: true });
 try {
  const page = await browser.newPage({ viewport: { width: 1365, height: 1000 } });
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.goto(base);
  await page.getByRole("heading", { name: "从第一个同步文件夹开始" }).waitFor();
  await page.screenshot({ animations: "disabled", path: path.join(output, "desktop-empty.png"), fullPage: true });
  assert.match(await page.locator("#sync-workspace h1").evaluate(node => getComputedStyle(node).fontFamily), /Microsoft YaHei/);
  await page.getByRole("button", { name: "添加设备", exact: true }).click();
  await page.getByRole("dialog").waitFor();
  await page.waitForFunction(() => document.activeElement.id === "manual-address");
  await page.locator("#close-modal").focus();
  await page.keyboard.press("Shift+Tab");
  assert.notEqual(await page.evaluate(() => document.activeElement.id), "close-modal");
  assert.equal(await page.evaluate(() => !!document.activeElement.closest(".modal")), true);
  await page.screenshot({ animations: "disabled", path: path.join(output, "dialog.png") });
  await page.keyboard.press("Escape");
  await page.locator("#modal-backdrop").waitFor({state:"hidden"});
  assert.equal(await page.evaluate(() => document.activeElement.id), "add-device");
  assert.equal(await page.locator("#add-share").isVisible(), false);
  assert.equal(await page.locator("#help-tour").isVisible(), false);
  await page.locator("#empty-add").click();
  await page.getByText("先连接另一台设备，配对后继续添加文件夹。").waitFor();
  await page.route("**/api/pair/start", route => route.fulfill({status:400, json:{error:"设备地址不可达"}}));
  await page.locator("#manual-address").fill("127.0.0.1:99");
  await page.locator("#manual-pair-form button").click();
  await page.getByRole("alert").filter({hasText:"设备地址不可达"}).waitFor();
  assert.equal(await page.locator("#manual-pair-form button").isEnabled(), true);
  assert.equal(await page.locator("#manual-address").inputValue(), "127.0.0.1:99");
  await page.keyboard.press("Escape");
  await page.locator("#modal-backdrop").waitFor({state:"hidden"});
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ animations: "disabled", path: path.join(output, "mobile-empty.png"), fullPage: true });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.emulateMedia({ reducedMotion: "reduce" });
  assert.equal(await page.locator(".hero").evaluate(node => getComputedStyle(node).animationName), "none");

  const fixture = {
   protocolVersion: 2, trayEnabled: true,
   device: { id: "test", name: "我的工作电脑", platform: "windows", addresses: ["192.168.1.10:45123"] },
   shares: [
    { id: "documents", name: "工作文档", path: "C:\\Users\\Demo\\Documents", state: "active", autoSync: true, intervalSeconds: 30,
      status: { state: "synced", lastSync: "2026-09-05T20:00:00+08:00" } },
    { id: "photos", name: "旅行照片", path: "D:\\Photos\\2026", state: "active", autoSync: true, intervalSeconds: 30,
      status: { state: "syncing", message: "正在接收 3 个文件…" } },
    { id: "pending", name: "等待接受的文件夹", path: "D:\\Projects", state: "pending", status: {} }
   ],
   pairedDevices: [{ id: "peer", name: "MacBook Air", online: true, publicKey: "test-public-key", lastSeen: "2026-09-05T20:00:00+08:00" }],
   nearbyDevices: [], pairingRequests: [], shareInvitations: [], conflicts: [],
   activities: [{ level: "success", message: "工作文档同步完成 · 2 个文件", time: "2026-09-05T20:00:00+08:00" }]
  };
  await page.route("**/api/status", route => route.fulfill({ json: fixture }));
  await page.setViewportSize({ width: 1365, height: 1000 });
  await page.goto(base);
  await page.locator('[data-share-id="documents"]').waitFor();
  assert.equal(await page.locator('[data-share-id="pending"] .sync-button').isDisabled(), true);
  assert.equal(await page.locator('[data-share-id="photos"] .sync-button').isDisabled(), true);
  await page.locator('[data-share-id="documents"]').evaluate(node => node.testIdentity = true);
  await page.waitForTimeout(2700);
  assert.equal(await page.locator('[data-share-id="documents"]').evaluate(node => node.testIdentity), true);
  await page.screenshot({ animations: "disabled", path: path.join(output, "desktop-folders.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ animations: "disabled", path: path.join(output, "mobile-folders.png"), fullPage: true });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.locator("#add-share").click();
  await page.locator("#share-path").fill("/tmp/draft");
  await page.locator("#invite-role").selectOption("follower");
  await page.locator("#allow-write-back").check();
  await page.locator("#invite-pair").click();
  await page.keyboard.press("Escape");
  assert.equal(await page.locator("#share-path").inputValue(), "/tmp/draft");
  assert.equal(await page.locator("#invite-role").inputValue(), "follower");
  assert.equal(await page.locator("#allow-write-back").isChecked(), true);
  await page.keyboard.press("Escape");
  let synced = false;
  await page.route("**/api/shares/documents/sync", async route => {
   assert.equal(route.request().method(), "POST");
   synced = true;
   await route.fulfill({ json: { ok: true } });
  });
  await page.locator('[data-share-id="documents"] .sync-button').click();
  await page.getByRole("status").filter({ hasText: "已请求同步" }).waitFor();
  assert.equal(synced, true);
  const folder = page.locator('[data-share-id="documents"]');
  const manual = folder.locator(".sync-button");
  const priorStatus = fixture.shares[0].status;
  const pause = "当前路径上的磁盘已更换。请接回原磁盘，文件夹将恢复同步。";
  const originalStatus = await folder.locator(".status").elementHandle();
  await manual.focus();
  await page.emulateMedia({ colorScheme: "dark", reducedMotion: "reduce" });
  fixture.shares[0].status = { ...priorStatus, state: "paused", message: pause };
  await folder.getByText(pause, {exact:true}).waitFor();
  assert.equal(await manual.isDisabled(), true);
  assert.equal(await manual.textContent(), "已暂停");
  assert.equal(await manual.evaluate(node => node.disabled), false);
  assert.equal(await folder.locator(".last-success").isVisible(), true);
  assert.equal(await manual.evaluate(node => document.activeElement === node), true);
  assert.equal(await folder.locator(".auto-toggle").isChecked(), true);
  assert.equal(await folder.locator(".status span").evaluate(node => node.scrollHeight <= node.clientHeight), true);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  assert.equal(await page.locator('[data-share-id="photos"] .sync-button').innerText(), "同步中");
  synced = false;
  await page.keyboard.press("Enter");
  fixture.shares[0].status = priorStatus;
  await folder.getByText(/已同步/).waitFor();
  assert.equal(await manual.isEnabled(), true);
  assert.equal(await manual.evaluate(node => document.activeElement === node), true);
  assert.equal(synced, false);
  await page.unroute("**/api/shares/documents/sync");
  await page.route("**/api/shares/documents/sync", async route => {
    fixture.shares[0].status = {...priorStatus, state:"waiting", message:"对端的文件夹已暂停，正在等待对端接回原盘。"};
    await route.fulfill({status:409, json:{error:"对端的文件夹已暂停，正在等待对端接回原盘。"}});
  });
  await page.keyboard.press("Enter");
  await folder.locator(".status").getByText(/对端的文件夹已暂停/).waitFor();
  await folder.getByText(/对端的文件夹已暂停/, {exact:false}).waitFor();
  assert.equal(await manual.isEnabled(), true);
  assert.equal(await manual.getAttribute("aria-describedby"), await folder.locator(".status").getAttribute("id"));
  await page.locator("#help-tour").focus();
  fixture.shares[0].status = priorStatus;
  await folder.locator(".status.synced").waitFor();
  assert.equal(await folder.locator(".status").evaluate((node, original) => node === original, originalStatus), true);
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  const conflict = {id:"meeting", path:"会议纪要.txt", status:"pending", kind:"modify-modify", localDevice:"本机", remoteDevice:"笔记本"};
  fixture.conflicts = [conflict];
  await page.route("**/api/conflicts/meeting", route => route.fulfill({json:{conflict, text:true, local:"初稿", remote:"定稿", merged:"初稿", base:""}}));
  await page.locator(".resolve-conflict").click();
  await page.getByText("没有可核对的共同版本，请比较两台设备的内容。").waitFor();
  assert.equal(await page.locator(".merge-grid textarea").count(), 2);
  assert.equal(await page.getByLabel("本机", {exact:true}).inputValue(), "初稿");
  await page.keyboard.press("Escape");
  conflict.baseHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";
  await page.locator(".resolve-conflict").click();
  await page.getByLabel("共同旧版本").waitFor();
  assert.equal(await page.getByLabel("共同旧版本").inputValue(), "");
  assert.equal(await page.locator(".merge-grid textarea").count(), 3);
  await page.keyboard.press("Escape");
  await page.unroute("**/api/status");
  await page.route("**/api/status", route => route.fulfill({ status: 503, json: { error: "服务已退出" } }));
  await page.waitForFunction(() => document.querySelector(".service-state").textContent.includes("服务离线"));
  assert.deepEqual(errors, []);
  console.log("Browser checks passed: desktop/mobile, empty/populated states, modal keyboard focus, polling stability, reduced motion, sync action and offline state.");
 } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
