// Manual check against a running engine: study chips, RSI/MACD panels,
// insight cards and persistence of the chosen studies.
import { chromium } from 'playwright';

const base = process.env.SMOKE_URL || 'http://localhost:8080/';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
const errors = [];
page.on('pageerror', e => errors.push(e.message));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

const chip = label => page.locator('.studies .fs-chip', { hasText: label });
const enabled = () => page.$$eval('.studies .fs-chip[aria-pressed="true"]', els => els.map(e => e.textContent.trim()));
const canvases = () => page.$$eval('fse-trading-chart canvas', els => els.length);
const painted = idx => page.evaluate(i => {
  const c = document.querySelectorAll('fse-trading-chart canvas')[i];
  if (!c) return -1;
  const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
  let n = 0;
  for (let k = 3; k < d.length; k += 4) if (d[k] > 0) n++;
  return n;
}, idx);

await page.goto(new URL('#/markets', base).href, { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);

console.log('enabled studies:', (await enabled()).join(','));
const ins = await page.$$eval('.insight .insight-label', els => els.map(e => e.textContent.trim()));
console.log('insights:', ins.join(' | '), ins.length >= 6 ? 'OK' : 'FAIL');
console.log('main canvas painted:', (await painted(0)) > 1000 ? 'OK' : 'FAIL');

for (const label of ['RSI 14', 'MACD', 'Bollinger', 'Volume', 'VWAP']) {
  if (!(await enabled()).includes(label)) await chip(label).click();
  await page.waitForTimeout(400);
}
await page.waitForTimeout(1000);
console.log('canvases with RSI+MACD:', await canvases(), (await canvases()) === 3 ? 'OK' : 'FAIL');
console.log('RSI painted:', (await painted(1)) > 500 ? 'OK' : 'FAIL', '· MACD painted:', (await painted(2)) > 500 ? 'OK' : 'FAIL');

await chip('RSI 14').click();
await page.waitForTimeout(700);
console.log('canvases after RSI off:', await canvases(), (await canvases()) === 2 ? 'OK (panel destroyed)' : 'FAIL');

await page.locator('.chart-range').getByRole('button', { name: '1Y', exact: true }).click();
await page.waitForTimeout(2200);
console.log('insights after 1Y:', await page.locator('.insight').count(), 'cards');

await page.reload({ waitUntil: 'networkidle' });
await page.waitForTimeout(2200);
const kept = await enabled();
console.log('persisted after reload:', kept.includes('MACD') && kept.includes('Bollinger') && kept.includes('VWAP') ? 'OK' : 'FAIL', kept.join(','));

console.log('console errors:', errors.length, errors.slice(0, 5));
await browser.close();
