const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../site/app.js'), 'utf8');

function card(platform, asset, kind) {
  const detail = { textContent: '' };
  return { dataset: { platform, asset, package: kind }, href: 'fallback/' + asset,
    classList: { add() {}, remove() {} }, querySelector: () => detail };
}

async function loadPage(navigator, cards, names, extras = [], commands = []) {
  const button = {}, label = {}, version = {}, picker = { addEventListener() {} };
  const ids = { '#recommended-download': button, '#recommended-label': label, '#download-platform': picker, '#platform-picker': {} };
  const groups = { '.download-card': cards, '[data-asset]': [...cards, ...extras], '[data-version]': [version], '[data-install]': commands };
  const context = { navigator, document: {
    querySelector: id => ids[id], querySelectorAll: selector => groups[selector] || [],
  }, fetch: async () => {
    if (!names) throw new Error('offline');
    return { ok: true, json: async () => ({ tag_name: 'v0.5.0', assets: names.map(name => ({name, browser_download_url: 'new/' + name})) }) };
  } };
  await vm.runInNewContext(source, context);
  return { button, label, version, picker };
}

const windows = 'polysync-windows-amd64';
for (const msi of [false, true]) test(`Windows offers ${msi ? 'the published MSI' : 'the available EXE'} from both buttons`, async () => {
  const win = card('windows', windows + '.msi');
  const names = [windows + '.exe'];
  if (msi) names.push(windows + '.msi');
  const {button, version} = await loadPage({platform: 'Win32'}, [win], names);
  assert.equal(win.href, 'new/' + names.at(-1));
  assert.equal(button.href, win.href);
  assert.equal(win.querySelector('small').textContent, msi ? 'x64 · MSI 安装包' : 'x64 · 免安装 EXE');
  assert.equal(version.textContent, 'v0.5.0');
});

test('Linux installation commands and links track release revisions, portable Windows keeps EXE', async () => {
  const cards = ['arch-x86_64', 'ubuntu-amd64', 'ubuntu-arm64'].map(kind => card('linux', 'old', kind));
  const portable = {dataset: {asset: windows + '.exe', portable: 'true'}};
  const command = {dataset: {install: 'arch-x86_64'}};
  const names = ['polysync-0.5.0-2-x86_64.pkg.tar.zst', 'polysync_0.5.0-2_amd64.deb', 'polysync_0.5.0-2_arm64.deb', windows + '.exe', windows + '.msi'];
  const {picker, label} = await loadPage({platform: 'Linux x86_64'}, cards, names, [portable], [command]);
  cards.forEach((item, i) => assert.equal(item.href, 'new/' + names[i]));
  assert.equal(command.textContent, 'sudo pacman -U ./' + names[0]);
  assert.equal(portable.href, 'new/' + windows + '.exe');
  assert.equal(picker.value, 'linux');
  assert.match(label.textContent, /Ubuntu、Arch/);
});

test('iPadOS, macOS, Android and offline Linux offer the relevant choice', async () => {
  for (const [device, choice, text] of [
    [{platform: 'MacIntel', maxTouchPoints: 5}, 'all', /iOS/],
    [{platform: 'MacIntel'}, 'mac', /Apple 芯片或 Intel/],
    [{platform: 'Linux armv8l', userAgent: 'Android'}, 'android', /Android/],
    [{userAgent: 'Linux'}, 'linux', /Ubuntu、Arch/],
  ]) {
    const {button, label, picker} = await loadPage(device, [card('android', 'app.apk')], undefined);
    assert.equal(button.href, '#downloads');
    assert.equal(picker.value, choice);
    assert.match(label.textContent, text);
  }
});

test('missing platform assets do not silently download an old desktop version', async () => {
  const win = card('windows', windows + '.msi');
  const {button} = await loadPage({platform: 'Win32'}, [win], ['SHA256SUMS.txt']);
  assert.equal(win.hidden, true);
  assert.equal(button.href, '#downloads');
});
