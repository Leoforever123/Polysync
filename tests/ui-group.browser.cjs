// Build the binary first and pass its absolute path in POLYSYNC_TEST_BINARY.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { once } = require("node:events");
const { chromium } = require("playwright");

async function port() {
  const listener = net.createServer();
  listener.listen(0, "127.0.0.1");
  await once(listener, "listening");
  const number = listener.address().port;
  await new Promise(resolve => listener.close(resolve));
  return number;
}

async function checkGroup() {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "polysync-ui-group-"));
  const processes = [];
  const errors = [];
  let browser;
  async function device(name) {
    const tcp = await port(), ui = await port();
    const address = "127.0.0.1:" + tcp;
    const child = spawn(process.env.POLYSYNC_TEST_BINARY, ["-data-dir", path.join(dir, name + "-data"), "-listen", address,
      "-ui", "127.0.0.1:" + ui, "-open=false", "-tray=false"], {stdio: "ignore"});
    processes.push(child);
    const base = "http://127.0.0.1:" + ui;
    const deadline = Date.now() + 15000;
    while (Date.now() < deadline) {
      try { if ((await fetch(base + "/api/status")).ok) break; } catch {}
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    const page = await browser.newPage();
    page.on("pageerror", error => errors.push(error.message));
    await page.goto(base);
    return {page, address, folder:path.join(dir, name + "-folder")};
  }
  async function pair(from, to) {
    await from.page.locator("#manual-address").fill(to.address);
    await from.page.locator("#manual-pair-form button").click();
    await to.page.locator(".approve-pair").click();
    await to.page.locator(".pair-code").waitFor();
    await from.page.locator("#pair-code-input").fill(await to.page.locator(".pair-code").innerText());
    await from.page.locator("#pair-form button[type=submit]").click();
    await from.page.locator("#target-device").waitFor();
  }
  async function accept(peer, role) {
    await peer.page.locator(".accept-invite").click();
    assert.match(await peer.page.locator(".invite-mode").innerText(), role);
    await peer.page.locator("#share-path").fill(peer.folder);
    await peer.page.locator("#accept-form button[type=submit]").click();
    await peer.page.locator("#accept-form").waitFor({state:"hidden"});
  }
  try {
    browser = await chromium.launch({channel:process.env.POLYSYNC_TEST_BROWSER_CHANNEL || "chrome"});
    const a = await device("A"), b = await device("B"), c = await device("C");
    for (const peer of [a,b,c]) {
      await fs.mkdir(peer.folder);
      const status = await (await peer.page.request.get(new URL("/api/status", peer.page.url()).href)).json();
      assert.deepEqual(status.device.addresses, [peer.address]);
    }
    await fs.writeFile(path.join(a.folder, "example.txt"), "same folder on all three");
    await a.page.locator("#empty-add").click();
    await pair(a,b);
    assert.equal(await a.page.locator("#invite-role").inputValue(), "collaborator");
    assert.equal(await a.page.locator("#tour-invite").isVisible(), false);
    assert.equal(await a.page.locator("#write-back-row").isVisible(), false);
    await a.page.locator("#share-path").fill(a.folder);
    await a.page.locator("#invite-form button[type=submit]").click();
    await accept(b, /共同编辑/);
    await a.page.locator(".invite-member").click();
    await pair(a,c);
    assert.equal(await a.page.locator("#share-path").count(), 0);
    assert.match(await a.page.locator("#role-note").innerText(), /本机关机/);
    await a.page.locator("#invite-role").selectOption("follower");
    assert.equal(await a.page.locator("#allow-write-back").isChecked(), false);
    await a.page.locator("#invite-form button[type=submit]").click();
    await accept(c, /仅接收/);
    await a.page.locator(".sync-button").click();
    const deadline = Date.now() + 20000;
    for (const peer of [b,c]) {
      let value;
      while (Date.now() < deadline) {
        try { value = await fs.readFile(path.join(peer.folder,"example.txt"),"utf8"); } catch {}
        if (value) break;
        await new Promise(resolve => setTimeout(resolve,150));
      }
      assert.equal(value, "same folder on all three");
    }
    assert.equal(await a.page.locator(".share-card").count(), 1);
    assert.equal(await c.page.locator(".invite-member").count(), 0);
    await fs.writeFile(path.join(c.folder, "local-only.txt"), "kept locally until consent");
    await a.page.locator(".edit-button").click();
    let follower = a.page.locator(".member-row").filter({hasText:"仅接收"});
    await follower.locator("summary").click();
    await follower.locator('[data-member-action="write"]').click();
    await follower.locator('[data-member-action="cancel"]').click();
    assert.match(await follower.locator("summary").innerText(), /仅接收/);
    await follower.locator('[data-member-action="write"]').click();
    await follower.locator('[data-member-action="confirm"]').click();
    follower = a.page.locator(".member-row").filter({hasText:"可回传"});
    await follower.locator("summary").waitFor();
    await a.page.keyboard.press("Escape");
    await a.page.locator(".sync-button").click();
    await c.page.locator(".review-writeback").click();
    await c.page.locator(".review-files").getByText("local-only.txt", {exact:false}).waitFor();
    await assert.rejects(fs.readFile(path.join(a.folder,"local-only.txt")), {code:"ENOENT"});
    await fs.writeFile(path.join(c.folder, "also-local.txt"), "changed during review");
    await c.page.locator("#confirm-writeback").click();
    await c.page.getByText("文件或授权已变化，请重新查看待回传内容").waitFor();
    await c.page.locator(".review-files").getByText("also-local.txt", {exact:false}).waitFor();
    await c.page.locator("#confirm-writeback").click();
    await c.page.locator("#modal-backdrop").waitFor({state:"hidden"});
    await c.page.locator(".sync-button").click();
    const deadlineBack = Date.now() + 15000;
    while (Date.now() < deadlineBack) {
      try { await fs.access(path.join(a.folder, "also-local.txt")); break; } catch {}
      await new Promise(resolve => setTimeout(resolve, 100));
    }
    assert.equal(await fs.readFile(path.join(a.folder,"also-local.txt"),"utf8"), "changed during review");
    await a.page.locator(".edit-button").click();
    await follower.locator("summary").click();
    await follower.locator('[data-member-action="remove"]').click();
    await follower.locator('[data-member-action="confirm"]').click();
    await follower.waitFor({state:"detached"});
    assert.equal(await a.page.locator(".member-row").count(), 1);
    assert.equal(await fs.readFile(path.join(c.folder,"example.txt"),"utf8"), "same folder on all three");
    assert.deepEqual(errors, []);
    console.log("Three loopback instances paired through the UI, joined one folder as collaborator and receive-only follower, and received identical bytes; local-only edits required a fresh preview and consent before write-back; disconnect retained the other member and all files.");
  } finally {
    if (browser) await browser.close();
    await Promise.all(processes.map(async child => {
      if (child.exitCode !== null || child.signalCode) return;
      const stopped = once(child, "exit");
      child.kill();
      await stopped;
    }));
    await fs.rm(dir, {recursive:true, force:true});
  }
}
checkGroup().catch(error => { console.error(error); process.exitCode = 1; });
