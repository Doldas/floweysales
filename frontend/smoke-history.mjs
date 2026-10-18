import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
page.on('pageerror', e => errors.push(e.message));

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);

// Chart should already be seeded from 1M market history on boot.
let pts = await page.evaluate(() => (window.Chart?.getChart?.(document.querySelector('canvas'))?.data?.datasets?.[0]?.data?.length) ?? -1);
// Fallback: count via ng context is hard; use a proxy — chart canvas painted pixels.
const pixelCount = await page.evaluate(() => {
  const c = document.querySelector('canvas');
  if (!c) return -1;
  const ctx = c.getContext('2d');
  const d = ctx.getImageData(0, 0, c.width, c.height).data;
  let n = 0;
  for (let i = 3; i < d.length; i += 4) if (d[i] > 0 && (d[i-3]+d[i-2]+d[i-1]) > 30) n++;
  return n;
});
console.log('boot: painted canvas pixels > 0:', pixelCount > 1000, `(${pixelCount})`);

const kpi = await page.evaluate(() => document.body.innerText.match(/LATEST PRICE\s+([\d.]+)/)?.[1] ?? 'NONE');
console.log('boot KPI price (from history seed):', kpi);

// Range switch 1D → 5D → 1Y
for (const r of ['1D', '5D', '1Y', '3Y', '5Y', '10Y', 'MAX', '1M']) {
  await page.locator('mat-button-toggle:has-text("' + r + '")').click();
  await page.waitForTimeout(r === '1M' ? 1200 : 700);
}
console.log('range toggles clicked: OK');

// Track MSFT via autocomplete and verify the chart reseeds quickly.
const input = page.locator('input[placeholder*="VOLV"]');
await input.fill('microsoft');
await page.waitForTimeout(1200);
await page.locator('mat-option').first().click();
await page.waitForTimeout(4000);

const badge = await page.evaluate(() => document.body.innerText.match(/\[([A-Z0-9.^=-]+)\]/)?.[1] ?? 'NONE');
console.log('tracked badge:', badge);
const kpi2 = await page.evaluate(() => document.body.innerText.match(/LATEST PRICE\s+([\d.]+)/)?.[1] ?? 'NONE');
console.log('KPI after track:', kpi2);
const pixelCount2 = await page.evaluate(() => {
  const c = document.querySelector('canvas');
  const ctx = c.getContext('2d');
  const d = ctx.getImageData(0, 0, c.width, c.height).data;
  let n = 0;
  for (let i = 3; i < d.length; i += 4) if (d[i] > 0 && (d[i-3]+d[i-2]+d[i-1]) > 30) n++;
  return n;
});
console.log('after track: painted canvas pixels > 0:', pixelCount2 > 1000, `(${pixelCount2})`);

console.log('console errors:', errors.length, errors.slice(0, 3));
await browser.close();
