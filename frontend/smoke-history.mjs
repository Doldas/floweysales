// Manual check against a running engine: the chart is seeded from provider
// history on load, every range reloads it, and tracking a new symbol reseeds it.
import { chromium } from 'playwright';

const base = process.env.SMOKE_URL || 'http://localhost:8080/';
const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
page.on('pageerror', e => errors.push(e.message));

const painted = () => page.evaluate(() => {
  const c = document.querySelector('.chart-container canvas');
  if (!c) return -1;
  const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
  let n = 0;
  for (let i = 3; i < d.length; i += 4) if (d[i] > 0 && (d[i - 3] + d[i - 2] + d[i - 1]) > 30) n++;
  return n;
});

await page.goto(new URL('#/markets', base).href, { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);
const boot = await painted();
console.log('boot: painted canvas pixels > 0:', boot > 1000, `(${boot})`);
console.log('boot KPI price (from history seed):', await page.evaluate(() => document.body.innerText.match(/Latest price\s+([\d.]+)/i)?.[1] ?? 'NONE'));

for (const r of ['1D', '5D', '1Y', '3Y', '5Y', '10Y', 'MAX', '1M']) {
  await page.locator('.chart-range').getByRole('button', { name: r, exact: true }).click();
  await page.waitForTimeout(r === '1M' ? 1200 : 700);
}
console.log('range toggles clicked: OK');

await page.locator('.control-card input[role="combobox"]').fill('microsoft');
await page.waitForTimeout(1200);
await page.locator('.control-card [role="option"]').first().click();
await page.waitForTimeout(4000);
console.log('tracked:', (await page.locator('.quote-id h1').textContent())?.trim());
console.log('KPI after track:', await page.evaluate(() => document.body.innerText.match(/Latest price\s+([\d.]+)/i)?.[1] ?? 'NONE'));
const after = await painted();
console.log('after track: painted canvas pixels > 0:', after > 1000, `(${after})`);

console.log('console errors:', errors.length, errors.slice(0, 3));
await browser.close();
