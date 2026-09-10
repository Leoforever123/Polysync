// Uses isolated temporary instances; never touches the user's sync folders.
// Build .cache/polysync-transfer-test.exe, then run with NODE_PATH pointing to Playwright.
const { chromium } = require("playwright");
const { spawn, execFileSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");
const net = require("node:net");
const assert = require("node:assert/strict");
const root = path.resolve(__dirname, "..");
const binary = process.env.POLYSYNC_TEST_BINARY || path.join(root, ".cache/polysync-transfer-test.exe");
const output = path.join(root, ".cache/ui-review");
const children = [];
async function freePort() { const s = net.createServer(); await new Promise(resolve => s.listen(0, "127.0.0.1", resolve)); const port = s.address().port; await new Promise(resolve => s.close(resolve)); return port; }
async function request(base, endpoint, data, method = "POST") {
 const result = await fetch(base + endpoint, { method, headers: { "Content-Type": "application/json" }, ...(method === "GET" ? {} : { body: JSON.stringify(data || {}) }) });
 const json = await result.json(); assert.ok(result.ok, JSON.stringify(json)); return json;
}
async function until(fn, description) {
 const deadline = Date.now() + 15000;
 while (Date.now() < deadline) { const value = await fn(); if (value) return value; await new Promise(resolve => setTimeout(resolve, 100)); }
 throw new Error("Timed out: " + description);
}
async function instance(name) {
 const tcp = await freePort(), ui = await freePort();
 const dir = fs.mkdtempSync(path.join(root, ".cache/transfer-browser-" + name + "-"));
 const child = spawn(binary, ["-data-dir", dir, "-listen", "127.0.0.1:" + tcp, "-ui", "127.0.0.1:" + ui, "-open=false", "-tray=false"], { windowsHide: true, stdio: "ignore" });
 children.push(child);
 const base = "http://127.0.0.1:" + ui;
 await until(async () => { try { return (await fetch(base + "/api/status")).ok; } catch { return false; } }, "instance startup");
 return { base, tcp, dir };
}
(async () => {
 fs.mkdirSync(output, { recursive: true });
 let browser;
 try {
  const a = await instance("a"), b = await instance("b");
  // Preserve the existing synchronization UI regression suite.
  execFileSync(process.execPath, [path.join(__dirname, "ui.browser.cjs")], { env: { ...process.env, POLYSYNC_TEST_URL: a.base }, stdio: "inherit", windowsHide: true });
  browser = await chromium.launch({ channel: "chrome", headless: true });
  const page = await browser.newPage({ viewport: { width: 1365, height: 1050 } });
  const errors = []; page.on("pageerror", e => errors.push(e.message));
  await page.goto(a.base);
  await page.locator("#tour-start").click();
  for (let i = 1; i <= 8; i++) {
   assert.equal(await page.locator("#tour-count").textContent(), i + " / 8");
   const rect = await page.locator("#tour-bubble").boundingBox();
   assert.ok(rect.x >= 0 && rect.y >= 0 && rect.x + rect.width <= 1365 && rect.y + rect.height <= 1050);
   if (i === 2) { await page.locator("#tour-prev").click(); assert.equal(await page.locator("#tour-count").textContent(), "1 / 8"); await page.locator("#tour-next").click(); }
   if (i === 1) await page.screenshot({ path: path.join(output, "tour-desktop.png") });
   await page.locator("#tour-next").click();
  }
  assert.equal(await page.locator("#tour-layer").isVisible(), false);
  await page.reload(); assert.equal(await page.locator("#tour-invite").isVisible(), false);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.locator("#help-tour").click();
  await page.keyboard.press("Escape");
  assert.equal(await page.evaluate(() => document.activeElement.id), "help-tour");
  await page.locator("#help-tour").click();
  await page.locator("#tour-skip").focus(); await page.keyboard.press("Shift+Tab");
  assert.equal(await page.evaluate(() => document.activeElement.id), "tour-next");
  for (let i = 1; i <= 8; i++) {
   const rect = await page.locator("#tour-bubble").boundingBox();
   assert.ok(rect.x >= 0 && rect.y >= 0 && rect.x + rect.width <= 390 && rect.y + rect.height <= 844);
   if (i === 3) await page.screenshot({ path: path.join(output, "tour-mobile.png") });
   await page.locator("#tour-next").click();
  }
  await page.locator('[data-workspace="transfer"]').click();
  await page.getByRole("heading", { name: "文件到了，就在这里。" }).waitFor();
  await page.screenshot({ path: path.join(output, "transfer-mobile-empty.png"), fullPage: true });
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await page.setViewportSize({ width: 1365, height: 1050 });
  await page.screenshot({ path: path.join(output, "transfer-desktop-empty.png"), fullPage: true });
  await page.locator("#transfer-settings").click();
  const originalDirectory = await page.locator("#transfer-directory").inputValue();
  await page.locator("#transfer-auto").uncheck();
  await page.getByRole("button", { name: "保存设置", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal((await request(a.base, "/api/transfers", null, "GET")).settings.autoReceive, false);
  await page.locator("#transfer-settings").click();
  await page.locator("#transfer-auto").check();
  await page.getByRole("button", { name: "保存设置", exact: true }).click();
  await page.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal((await request(a.base, "/api/transfers", null, "GET")).settings.directory, originalDirectory);
  // Pair real instances using the existing confirmation protocol.
  const pair = await request(a.base, "/api/pair/start", { address: "127.0.0.1:" + b.tcp });
  const incoming = await until(async () => (await request(b.base, "/api/status", null, "GET")).pairingRequests[0], "pair request");
  await request(b.base, "/api/pair/requests/" + incoming.id + "/approve");
  const approved = await until(async () => { const p = (await request(b.base, "/api/status", null, "GET")).pairingRequests[0]; return p?.code ? p : null; }, "pair code");
  await request(a.base, "/api/pair/confirm", { sessionId: pair.sessionId, code: approved.code });
  const target = (await request(b.base, "/api/status", null, "GET")).device.id;
  await page.waitForFunction(id => !!document.querySelector('#transfer-peer option[value="' + id + '"]'), target);
  await page.locator("#transfer-peer").selectOption(target);
  await page.locator("#transfer-files").setInputFiles([{ name: "随传测试.txt", mimeType: "text/plain", buffer: Buffer.from("真实浏览器 → TLS → 自动接收") }, { name: "empty.txt", mimeType: "text/plain", buffer: Buffer.alloc(0) }]);
  await page.locator("#transfer-send").click();
  await page.getByText("已交给后台，现在可以关闭页面。", { exact: true }).waitFor();
  await page.close();
  const received = await until(async () => (await request(b.base, "/api/transfers", null, "GET")).tasks.find(task => task.state === "complete"), "automatic receive without receiver page");
  assert.equal(received.files.length, 2);
  assert.equal(fs.readFileSync(path.join(received.storageDir, received.files[0].blob), "utf8"), "真实浏览器 → TLS → 自动接收");
  assert.equal(fs.statSync(path.join(received.storageDir, received.files[1].blob)).size, 0);
  assert.equal((await request(b.base, "/api/status", null, "GET")).shares.length, 0);
  const inbox = await browser.newPage({ viewport: { width: 1365, height: 1050 } });
  inbox.on("pageerror", e => errors.push(e.message));
  await inbox.goto(b.base + "#transfer/task/" + received.id);
  await inbox.locator('[data-transfer-action="save"]').first().waitFor();
  await inbox.screenshot({ path: path.join(output, "transfer-desktop-received.png"), fullPage: true });
  const focused = inbox.locator('[data-transfer-action="save"]').first();
  await focused.focus(); await focused.evaluate(node => node.identityCheck = true);
  await inbox.waitForTimeout(2700);
  assert.equal(await focused.evaluate(node => node.identityCheck), true);
  // Verify save/open API dispatch without bringing native dialogs into headless Chrome.
  let saved = false;
  await inbox.route("**/api/transfers/*/files/0/save", async route => { assert.equal(route.request().method(), "POST"); saved = true; await route.fulfill({ json: { path: "D:\\Saved\\随传测试.txt", cancelled: false } }); });
  await focused.click();
  assert.equal(saved, true);
  await inbox.setViewportSize({ width: 390, height: 844 });
  await inbox.screenshot({ path: path.join(output, "transfer-mobile-received.png"), fullPage: true });
  assert.equal(await inbox.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
  await inbox.emulateMedia({ reducedMotion: "reduce" });
  assert.equal(await inbox.locator(".transfer-task").evaluate(node => getComputedStyle(node).animationName), "none");
  await inbox.locator('[data-transfer-action="remove"]').click();
  assert.equal(await inbox.locator("#transfer-delete-files").isChecked(), false);
  await inbox.locator("#transfer-remove-form").getByRole("button", { name: "移除记录", exact: true }).click();
  await inbox.getByRole("dialog").waitFor({ state: "hidden" });
  assert.equal(fs.existsSync(path.join(received.storageDir, received.files[0].blob)), true);
  assert.equal((await request(b.base, "/api/transfers", null, "GET")).tasks.length, 0);
  assert.deepEqual(errors, []);
  console.log("Transfer browser checks passed: tutorial all steps/replay/skip/keyboard/mobile, real paired TLS delivery, empty file, receiving without page, settings persistence, save dispatch, focus stability, remove record preserves file, reduced motion.");
 } finally { if (browser) await browser.close(); for (const child of children) child.kill(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
