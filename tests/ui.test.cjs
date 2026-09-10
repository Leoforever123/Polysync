const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");
const script = fs.readFileSync(path.join(__dirname, "../internal/webui/static/app.js"), "utf8");

function app() {
  const nodes = new Map();
  const listeners = {};
  function node(selector) {
    if (!nodes.has(selector)) nodes.set(selector, {
      writes: 0, isConnected: true, style: {}, classList: { add() {}, remove() {}, toggle() {} },
      addEventListener() {}, contains() { return false; }, matches() { return false; },
      querySelector() { return null; }, querySelectorAll() { return []; },
      focus() { document.activeElement = this; },
      set innerHTML(value) { this.html = value; this.writes++; }, get innerHTML() { return this.html; }
    });
    return nodes.get(selector);
  }
  const document = { querySelector: node, activeElement: null, body: { style: {} },
    addEventListener(name, fn) { listeners[name] = fn; } };
  const context = vm.createContext({ document, console, setTimeout, clearTimeout, setInterval() {},
    requestAnimationFrame(fn) { fn(); }, fetch() { return new Promise(() => {}); } });
  vm.runInContext(script, context);
  return { context, document, node, listeners, run: code => vm.runInContext(code, context) };
}
test("unchanged polling keeps the same DOM and keyboard target", () => {
  const a = app();
  a.run('setContent("#share-list", "<button>同步</button>")');
  const list = a.node("#share-list");
  a.run('setContent("#share-list", "<button>同步</button>")');
  assert.equal(list.writes, 1);
  a.run('setContent("#share-list", "<button>同步完成</button>")');
  assert.equal(list.writes, 2);
});
test("polling does not overwrite an active input", () => {
  const a = app();
  const list = a.node("#share-list");
  a.document.activeElement = { matches() { return true; } };
  list.contains = () => true;
  a.run('setContent("#share-list", "new server data")');
  assert.equal(list.writes, 0);
});
test("folder names and paths are escaped and pending/syncing actions disabled", () => {
  const a = app();
  a.context.folder = { id: '"><script>', name: "<img src=x onerror=alert(1)>", path: "<path>", state: "pending", status: {} };
  const html = a.run("shareCard(folder)");
  assert.ok(!html.includes("<img"));
  assert.ok(html.includes("&lt;img"));
  assert.match(html, /sync-button" disabled/);
  a.context.folder.state = "active";
  a.context.folder.status.state = "syncing";
  assert.match(a.run("shareCard(folder)"), /sync-button" disabled/);
});
test("modal Escape restores focus and unlocks body scrolling", () => {
  const a = app();
  const trigger = a.node("#add-device");
  a.document.activeElement = trigger;
  a.run('openModal("<h2>添加设备</h2>", "nearby")');
  assert.equal(a.document.body.style.overflow, "hidden");
  assert.equal(a.node(".shell").inert, true);
  a.listeners.keydown({ key: "Escape", preventDefault() {} });
  assert.equal(a.document.body.style.overflow, "");
  assert.equal(a.node(".shell").inert, false);
  assert.equal(a.document.activeElement, trigger);
  assert.equal(a.run("state.modal"), null);
});
test("reduced motion disables decorative animation and transitions", () => {
  const css = fs.readFileSync(path.join(__dirname, "../internal/webui/static/app.css"), "utf8");
  assert.match(css, /@media \(prefers-reduced-motion: reduce\)/);
  assert.match(css, /animation: none !important; transition: none !important/);
});

const transfer = require("../internal/webui/static/transfer.js");
test("transfer selection limits include empty files and reject oversized batches", () => {
 assert.equal(transfer.transferSelectionError([{name:"empty",size:0}]), "");
 assert.match(transfer.transferSelectionError([]), /选择/);
 assert.match(transfer.transferSelectionError(Array.from({length:129},()=>({size:0}))), /128/);
 assert.match(transfer.transferSelectionError([{size:16*1024**3},{size:1}]), /16 GiB/);
 assert.equal(transfer.transferBytes(0), "0 B");
 assert.equal(transfer.transferBytes(1024), "1.0 KiB");
 assert.equal(transfer.transferIsActive("receiving"), true);
 assert.equal(transfer.transferIsActive("complete"), false);
});
test("unrelated forms are not intercepted by synchronization handlers", () => {
 const a = app();
 let prevented = false;
 a.listeners.submit({target:{id:"transfer-settings-form"},preventDefault(){prevented=true;}});
 assert.equal(prevented, false);
});
test("transfer filenames, peer names and paths are escaped", () => {
 const a = app();
 a.run('const module = {exports:{}};');
 // Load declarations, omitting browser initialization for this VM.
 a.run(fs.readFileSync(path.join(__dirname,"../internal/webui/static/transfer.js"),"utf8").split('if (typeof document !== "undefined")')[0]);
 a.context.task = {id:'test',peerName:'<script>bad</script>',direction:'receive',state:'complete',created:'2026-09-08T00:00:00Z',total:0,done:0,storageDir:'<path>',files:[{name:'<img onerror=bad>',size:0,state:'complete'}]};
 const html = a.run("transferCard(task)");
 assert.ok(!html.includes("<script>")); assert.ok(!html.includes("<img")); assert.ok(html.includes("&lt;path&gt;"));
});
