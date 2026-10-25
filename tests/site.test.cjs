const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../site/app.js'), 'utf8');

for (const msi of [false, true]) test(`Windows download selects ${msi ? 'MSI when published' : 'existing EXE when MSI is absent'}`, async () => {
  const detail = { textContent: 'x64 · .exe' };
  const card = { dataset: { asset: 'polysync-windows-amd64.exe' }, href: 'old.exe', querySelector: () => detail };
  const hero = { dataset: { asset: 'polysync-windows-amd64.exe' }, href: 'old.exe', querySelector: () => null };
  const versions = [{ textContent: 'v0.3.0' }];
  const assets = [{ name: 'polysync-windows-amd64.exe', browser_download_url: 'new.exe' }];
  if (msi) assets.push({ name: 'polysync-windows-amd64.msi', browser_download_url: 'new.msi' });
  const context = vm.createContext({ navigator: { userAgent: '', platform: '' }, document: {
    querySelectorAll: selector => selector === '[data-asset]' ? [card, hero] : selector === '[data-version]' ? versions : [],
  }, fetch: async () => ({ ok: true, json: async () => ({ tag_name: 'v0.4.0', assets }) }) });
  vm.runInContext(source, context);
  await vm.runInContext('refreshRelease()', context);
  assert.equal(card.href, msi ? 'new.msi' : 'new.exe');
  assert.equal(hero.href, card.href);
  assert.equal(detail.textContent, msi ? 'x64 · MSI 安装包' : 'x64 · .exe');
  assert.equal(versions[0].textContent, 'v0.4.0');
});

test('new Linux releases update package links and installation commands; EXE stays portable', async () => {
  const cards = ['arch-x86_64', 'ubuntu-amd64', 'ubuntu-arm64'].map(kind => ({dataset: {package: kind, asset: 'old'}, href: 'old'}));
  const portable = {dataset: {asset: 'polysync-windows-amd64.exe', portable: 'true'}, href: 'old.exe'};
  const command = {dataset: {install: 'arch-x86_64'}, textContent: 'old'};
  const names = ['polysync-0.4.1-2-x86_64.pkg.tar.zst', 'polysync_0.4.1-2_amd64.deb', 'polysync_0.4.1-2_arm64.deb', 'polysync-windows-amd64.exe', 'polysync-windows-amd64.msi'];
  const context = vm.createContext({navigator: {userAgent: '', platform: ''}, document: {
    querySelectorAll: selector => selector === '[data-asset]' ? [...cards, portable] : selector === '[data-install]' ? [command] : [],
  }, fetch: async () => ({ok: true, json: async () => ({tag_name: 'v0.4.1', assets: names.map(name => ({name, browser_download_url: 'https://example.test/' + name}))})})});
  vm.runInContext(source, context);
  await vm.runInContext('refreshRelease()', context);
  cards.forEach((card, i) => assert.equal(card.href, 'https://example.test/' + names[i]));
  assert.equal(command.textContent, 'sudo pacman -U ./' + names[0]);
  assert.equal(portable.href, 'https://example.test/polysync-windows-amd64.exe');
});

test('Linux recommendation asks for distribution and keeps working when release API fails', async () => {
  const button = {href: 'old'}, label = {textContent: ''};
  const context = vm.createContext({navigator: {userAgent: 'Linux', platform: ''}, document: {
    querySelector: selector => selector === '#recommended-download' ? button : label,
    querySelectorAll: () => [],
  }, fetch: async () => { throw new Error('offline'); }});
  vm.runInContext(source, context);
  await vm.runInContext('refreshRelease()', context);
  assert.equal(button.href, '#downloads');
  assert.match(label.textContent, /Ubuntu、Arch/);
});
