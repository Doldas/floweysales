// Manual check against a running engine: the stop-loss line spans the chart
// for every history range, not just the latest ticks.
import { chromium } from 'playwright';

const base = process.env.SMOKE_URL || 'http://localhost:8080/';
const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(e.message));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

await page.goto(new URL('#/markets', base).href, { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);

// Stop loss is drawn in #ef4444.
const countRed = () => page.evaluate(() => {
  const c = document.querySelector('.chart-container canvas');
  const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
  let n = 0;
  for (let i = 0; i < d.length; i += 4) if (d[i] > 180 && d[i + 1] < 100 && d[i + 2] > 40 && d[i + 2] < 140) n++;
  return n;
});

for (const range of ['', '1Y', 'MAX']) {
  if (range) {
    await page.locator('.chart-range').getByRole('button', { name: range, exact: true }).click();
    await page.waitForTimeout(2000);
  }
  const red = await countRed();
  console.log(`${range || 'boot'} stop-line red pixels:`, red, red > 150 ? 'SPANS CHART' : 'STUB ONLY?');
}

console.log('errors:', errors.length, errors.slice(0, 3));
await browser.close();
