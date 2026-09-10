// Browser checks: npm install --prefix .cache/ui-tools --no-save playwright
// NODE_PATH=.cache/ui-tools/node_modules node tests/ui.browser.cjs
const { chromium } = require("playwright");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
(async () => {
 const browser = await chromium.launch({ channel: "chrome", headless: true });
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
  assert.equal(await page.locator("#modal-backdrop").isVisible(), false);
  assert.equal(await page.evaluate(() => document.activeElement.id), "add-device");
  await page.getByRole("button", { name: "添加同步", exact: true }).click();
  await page.getByRole("status").filter({ hasText: "请先配对一台设备" }).waitFor();
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
  let synced = false;
  await page.route("**/api/shares/documents/sync", async route => {
   assert.equal(route.request().method(), "POST");
   synced = true;
   await route.fulfill({ json: { ok: true } });
  });
  await page.locator('[data-share-id="documents"] .sync-button').click();
  await page.getByRole("status").filter({ hasText: "已开始同步" }).waitFor();
  assert.equal(synced, true);
  await page.unroute("**/api/status");
  await page.route("**/api/status", route => route.fulfill({ status: 503, json: { error: "服务已退出" } }));
  await page.waitForFunction(() => document.querySelector(".service-state").textContent.includes("服务离线"));
  assert.deepEqual(errors, []);
  console.log("Browser checks passed: desktop/mobile, empty/populated states, modal keyboard focus, polling stability, reduced motion, sync action and offline state.");
 } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
