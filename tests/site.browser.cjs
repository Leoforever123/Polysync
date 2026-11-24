const { chromium } = require('playwright');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '..');
const server = http.createServer((req, res) => {
  const name = req.url === '/' ? 'index.html' : req.url.slice(1);
  if (!['index.html', 'app.js', 'styles.css', 'favicon.svg'].includes(name)) { res.writeHead(404).end(); return; }
  res.setHeader('Content-Type', name.endsWith('.js') ? 'text/javascript' : name.endsWith('.css') ? 'text/css' : name.endsWith('.svg') ? 'image/svg+xml' : 'text/html');
  res.end(fs.readFileSync(path.join(root, 'site', name)));
});
(async () => {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  let browser;
  try {
    browser = await chromium.launch({ channel: process.env.POLYSYNC_TEST_BROWSER_CHANNEL || 'chrome', headless: true });
    const page = await browser.newPage({ viewport: { width: 1365, height: 1000 } });
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.route('https://api.github.com/repos/Leoforever123/Polysync/releases/latest', route => route.fulfill({ json: {
      tag_name: 'v0.4.0', assets: ['polysync-windows-amd64.msi', 'polysync-windows-amd64.exe', 'polysync-0.4.0-1-x86_64.pkg.tar.zst', 'polysync_0.4.0-1_amd64.deb', 'polysync_0.4.0-1_arm64.deb'].map(name => ({name, browser_download_url: `https://github.com/Leoforever123/Polysync/releases/download/v0.4.0/${name}`})),
    } }));
    await page.goto(`http://127.0.0.1:${server.address().port}`);
    const card = page.locator('.download-card[data-asset="polysync-windows-amd64.msi"]');
    const picker = page.getByLabel('安装到');
    await picker.selectOption('windows');
    await card.waitFor();
    assert.match(await card.getAttribute('href'), /v0\.4\.0\/polysync-windows-amd64\.msi$/);
    assert.equal(await card.locator('small').textContent(), 'x64 · MSI 安装包');
    await card.focus();
    assert.equal(await card.evaluate(node => document.activeElement === node), true);
    await page.locator('summary').focus();
    await page.keyboard.press('Enter');
    assert.equal(await page.locator('details').evaluate(el => el.open), true);
    await picker.selectOption('linux');
    assert.equal(await card.isVisible(), false);
    const arch = page.locator('[data-package="arch-x86_64"]');
    assert.match(await arch.getAttribute('href'), /polysync-0.4.0-1-x86_64.pkg.tar.zst$/);
    assert.match(await page.locator('[data-install="arch-x86_64"]').innerText(), /sudo pacman -U/);
    assert.match(await page.locator('[data-portable="true"]').getAttribute('href'), /\.exe$/);
    await page.route('**/releases/download/**', route => route.fulfill({contentType: 'application/octet-stream', headers: {'Content-Disposition': 'attachment; filename="polysync-0.4.0-1-x86_64.pkg.tar.zst"'}, body: 'download fixture'}));
    await arch.focus();
    const [download] = await Promise.all([page.waitForEvent('download'), page.keyboard.press('Enter')]);
    assert.equal(download.suggestedFilename(), 'polysync-0.4.0-1-x86_64.pkg.tar.zst');
    const screenshots = path.join(root, '.cache/ui-review/release');
    fs.mkdirSync(screenshots, {recursive: true});
    await page.locator('#downloads').screenshot({path: path.join(screenshots, 'desktop.png')});
    await page.evaluate(() => scrollTo({top: 0, behavior: 'instant'}));
    await page.screenshot({path: path.join(screenshots, 'hero-light.png')});
    await page.setViewportSize({ width: 390, height: 844 });
    await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: 'dark' });
    assert.equal(await page.locator('html').evaluate(el => getComputedStyle(el).scrollBehavior), 'auto');
    assert.equal(await page.locator('html').evaluate(el => getComputedStyle(el).backgroundColor), 'rgb(17, 26, 21)');
    await page.screenshot({path: path.join(screenshots, 'hero-dark-narrow.png')});
    await picker.selectOption('windows');
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
    await card.scrollIntoViewIfNeeded();
    assert.equal(await card.isVisible(), true);
    await page.locator('#downloads').screenshot({path: path.join(screenshots, 'narrow.png')});
    await page.unroute('https://api.github.com/repos/Leoforever123/Polysync/releases/latest');
    await page.route('https://api.github.com/repos/Leoforever123/Polysync/releases/latest', route => route.abort());
    await page.reload();
    const fallback = await page.locator('[data-version]').textContent();
    assert.ok((await arch.getAttribute('href')).endsWith('/' + fallback + '/polysync-' + fallback.slice(1) + '-1-x86_64.pkg.tar.zst'));
    assert.deepEqual(errors, []);
    console.log('Site browser checks passed: Windows/Ubuntu/Arch downloads, keyboard download, desktop/narrow layout, reduced motion, API failure fallback. No new dialogs.');
  } finally { if (browser) await browser.close(); await new Promise(resolve => server.close(resolve)); }
})().catch(error => { console.error(error); process.exitCode = 1; server.close(); });
