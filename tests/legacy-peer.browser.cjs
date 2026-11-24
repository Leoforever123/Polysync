// Supply POLYSYNC_TEST_BINARY and POLYSYNC_LEGACY_BINARY (the published v0.4.0 client).
const assert = require("node:assert/strict");
const {spawn, execFileSync} = require("node:child_process");
const {once} = require("node:events");
const fs = require("node:fs/promises");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");
const {setTimeout: delay} = require("node:timers/promises");
const {chromium} = require("playwright");

const address = port => "127.0.0.1:" + port;

async function bindPort(port = 0) {
  const socket = net.createServer().listen(port, "127.0.0.1");
  await once(socket, "listening");
  const number = socket.address().port;
  await new Promise((resolve, reject) => socket.close(err => err ? reject(err) : resolve()));
  return number;
}

async function waitFor(check, description) {
  for (let n = 0; n < 100; n++) {
    if (await check()) return;
    await delay(100);
  }
  throw new Error("Timed out: " + description);
}

async function waitBytes(dir, name, value) {
  await waitFor(async () => {
    try { return (await fs.readFile(path.join(dir, name), "utf8")) === value; }
    catch (err) { if (err.code === "ENOENT") return false; throw err; }
  }, "expected " + name + " in peer folder");
}

async function start(device) {
  device.proc = spawn(device.exe, ["-listen", address(device.tcp), "-ui", address(device.http),
    "-open=false", "-tray=false", "-data-dir", device.data], {stdio: "ignore"});
  await once(device.proc, "spawn");
  await waitFor(async () => {
    if (device.proc.exitCode !== null) throw new Error("Early exit from " + device.exe);
    try { return (await fetch(device.base + "/api/status")).ok; } catch { return false; }
  }, "service startup");
}

async function stop(device) {
  if (!device.proc || device.proc.exitCode !== null || device.proc.signalCode !== null) return;
  const exited = once(device.proc, "exit");
  device.proc.kill("SIGTERM");
  const [code] = await exited;
  assert.equal(code, 0, "process should shut down cleanly");
  await bindPort(device.tcp);
  await bindPort(device.http);
}

async function run() {
  const legacy = process.env.POLYSYNC_LEGACY_BINARY;
  const current = process.env.POLYSYNC_TEST_BINARY;
  assert.ok(legacy && current, "set POLYSYNC_LEGACY_BINARY and POLYSYNC_TEST_BINARY");
  assert.match(execFileSync(legacy, ["-version"], {encoding: "utf8"}), /PolySync 0\.4\.0\b/);
  const temp = await fs.mkdtemp(path.join(os.tmpdir(), "polysync-legacy-peer-"));
  const devices = [];
  let browser;
  try {
    browser = await chromium.launch({channel: process.env.POLYSYNC_TEST_BROWSER_CHANNEL || "chrome"});
    const jsErrors = [];
    for (const [name, exe] of [["current", current], ["legacy", legacy]]) {
      const tcp = await bindPort(), http = await bindPort();
      const device = {exe, tcp, http, base: "http://" + address(http), data: path.join(temp, name + "-data"),
        folder: path.join(temp, name + "-folder"), page: await browser.newPage()};
      devices.push(device);
      device.page.on("pageerror", e => jsErrors.push(e.message));
      await fs.mkdir(device.folder);
      await start(device);
      await device.page.goto(device.base);
    }
    const [a, b] = devices;
    await a.page.locator("#empty-add").click();
    await a.page.locator("#manual-address").fill(address(b.tcp));
    await a.page.locator("#manual-pair-form button").click();
    await b.page.locator(".approve-pair").click();
    await a.page.locator("#pair-code-input").fill(await b.page.locator(".pair-code").innerText());
    await a.page.locator("#pair-form button[type=submit]").click();
    assert.equal(await a.page.locator("#invite-role").inputValue(), "collaborator");
    await fs.writeFile(path.join(a.folder, "out.txt"), "from the new device");
    await a.page.locator("#share-path").fill(a.folder);
    await a.page.locator("#invite-form button[type=submit]").click();
    await b.page.locator(".accept-invite").click();
    await b.page.locator("#share-path").fill(b.folder);
    await b.page.locator("#accept-form button[type=submit]").click();
    await a.page.locator(".sync-button:enabled").click();
    await waitBytes(b.folder, "out.txt", "from the new device");

    await fs.writeFile(path.join(b.folder, "back.txt"), "from the old device");
    await b.page.locator(".sync-button:enabled").click();
    await waitBytes(a.folder, "back.txt", "from the old device");
    console.log("Real v0.4.0 accepted the default v0.5 invitation and both directions delivered matching bytes.");

    const getStatus = async device => (await fetch(device.base + "/api/status")).json();
    const before = await getStatus(a);
    await stop(a);
    await start(a);
    await a.page.reload();
    const after = await getStatus(a);
    assert.deepEqual(after.pairedDevices.map(device => device.id), before.pairedDevices.map(device => device.id));
    assert.equal(after.shares[0].id, before.shares[0].id);
    await fs.writeFile(path.join(a.folder, "after.txt"), "still paired");
    await a.page.locator(".sync-button:enabled").click();
    await waitBytes(b.folder, "after.txt", "still paired");
    console.log("SIGTERM released both loopback ports; restart on the same ports retained the pair and synchronized again.");

    const follower = path.join(temp, "read-only");
    await fs.mkdir(follower);
    await fs.writeFile(path.join(follower, "private.txt"), "private");
    await a.page.locator("#add-share").click();
    await a.page.locator("#target-device").selectOption(after.pairedDevices[0].id);
    await a.page.locator("#invite-role").selectOption("follower");
    await a.page.locator("#share-path").fill(follower);
    await a.page.locator("#invite-form button[type=submit]").click();
    const failure = a.page.locator("#invite-form .form-error");
    await waitFor(async () => /升级|更新/.test(await failure.innerText()), "upgrade guidance for an old read-only peer");
    assert.equal(await a.page.locator("#invite-role").inputValue(), "follower");
    const oldStatus = await getStatus(b);
    assert.equal((oldStatus.shareInvitations || []).length, 0);
    assert.equal(oldStatus.shares.length, 1);
    const pending = (await getStatus(a)).shares.find(share => share.path === follower);
    assert.equal(pending.members[0].role, "follower");
    assert.equal(pending.members[0].allowWriteBack || false, false);
    assert.deepEqual(jsErrors, []);
    console.log("Read-only invitation required an upgrade, retained the grant and created no invitation on v0.4.0.");
  } finally {
    if (browser) await browser.close();
    try { await Promise.all(devices.map(stop)); }
    finally { await fs.rm(temp, {recursive: true, force: true}); }
  }
}
run().catch(err => { console.error(err); process.exitCode = 1; });
