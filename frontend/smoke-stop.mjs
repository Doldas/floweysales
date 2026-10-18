import { chromium } from 'playwright';
const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(e.message));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);

const countRed = () => page.evaluate(() => {
  const c = document.querySelector('canvas');
  const ctx = c.getContext('2d');
  const d = ctx.getImageData(0, 0, c.width, c.height).data;
  let n = 0;
  for (let i = 0; i < d.length; i += 4) {
    if (d[i] > 180 && d[i+1] < 100 && d[i+2] > 40 && d[i+2] < 140) n++;
  }
  return n;
});

const boot = await countRed();
console.log('boot stop-line red pixels:', boot, boot > 150 ? 'SPANS CHART' : 'STUB ONLY?');

await page.locator('mat-button-toggle:has-text("1Y")').click();
await page.waitForTimeout(2000);
const after1Y = await countRed();
console.log('after 1Y switch red pixels:', after1Y, after1Y > 150 ? 'SPANS CHART' : 'STUB ONLY?');

await page.locator('mat-button-toggle:has-text("MAX")').click();
await page.waitForTimeout(2000);
const afterMax = await countRed();
console.log('after MAX switch red pixels:', afterMax, afterMax > 150 ? 'SPANS CHART' : 'STUB ONLY?');

console.log('errors:', errors.length, errors.slice(0,3));
await browser.close();
