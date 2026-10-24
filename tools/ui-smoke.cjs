// Optional browser test. The Go demo itself has no Node.js dependencies.
const { chromium } = process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES
  ? require(process.env.CODEX_PRIMARY_RUNTIME_NODE_MODULES + '/playwright')
  : require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');

(async () => {
  const out = process.env.QA_SCREENSHOT_DIR || path.join('results', 'ui-smoke');
  await fs.mkdir(out, { recursive: true });
  const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_BIN || undefined });
  try {
    const page = await browser.newPage({ viewport: { width: 1512, height: 1080 }, reducedMotion: 'reduce' });
    const errors = []; page.on('pageerror', e => errors.push(e.message));
    await page.goto(process.env.DEMO_URL || 'http://127.0.0.1:8080');
    await page.waitForFunction(() => !document.querySelector('#play').disabled);
    assert.equal(await page.locator('.preset').count(), 9);
    await page.locator('#time').evaluate(el => { el.value = '18000'; el.dispatchEvent(new Event('input')); });
    await page.screenshot({ path: path.join(out, 'desktop.png'), fullPage: true });
    const downloadPromise = page.waitForEvent('download');
    await page.locator('.final-results').evaluate(el => el.open = true);
    await page.locator('#exportJSON').click();
    const download = await downloadPromise;
    const reportPath = path.join(out, 'export.json'); await download.saveAs(reportPath);
    const report = JSON.parse(await fs.readFile(reportPath, 'utf8'));
    assert.equal(report.version, 'timeline-v3'); assert.equal(report.results.length, 6);
    const baseline = report.results.find(r => r.policy === 'availability');
    const risk = report.results.find(r => r.policy === 'risk_aware');
    assert.ok(risk.stats.delivered >= baseline.stats.delivered);
    assert.ok(risk.frames.every(f => f.stats.generated === f.stats.delivered + f.stats.expired + f.stats.rejected + f.stats.pending));
    for (const name of ['wrong_labels', 'overload', 'lost_ack', 'independent', 'weak_alternative','changing_links','deadline_pressure','unknown_topology']) {
      await page.locator(`.preset[data-name="${name}"]`).click();
      await page.waitForFunction(() => !document.querySelector('#build').disabled && !document.querySelector('#play').disabled);
      assert.equal(await page.locator('#error').isVisible(), false);
      await page.locator('#time').evaluate(el => { el.value = el.max; el.dispatchEvent(new Event('input')); });
      assert.equal(await page.locator('#summary tbody tr').count(), 6);
    }
    await page.locator('#import').setInputFiles(reportPath);
    await page.waitForFunction(() => document.querySelector('#scenarioName').textContent === 'Общий отказ');
    await page.locator('#step').click();
    assert.match(await page.locator('#clock').textContent(), /^1\.0/);
    await page.locator('#play').click();
    await page.waitForTimeout(350);
    await page.locator('#play').click();
    assert.equal(await page.locator('#error').isVisible(), false);
    await page.setViewportSize({ width: 390, height: 844 });
    await page.locator('#time').evaluate(el => { el.value = '18000'; el.dispatchEvent(new Event('input')); });
    await page.screenshot({ path: path.join(out, 'mobile.png'), fullPage: true });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'horizontal overflow on mobile');
    assert.deepEqual(errors, []);
    console.log('UI OK: nine presets, replay, export/import, mobile, no JS errors');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exit(1); });
