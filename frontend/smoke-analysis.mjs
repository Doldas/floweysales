import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(e.message));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

const group = (label) => page.locator('.indicator-toggles').locator(`mat-button-toggle`).filter({ hasText: label });
const painted = (n) => page.evaluate((idx) => {
  const c = document.querySelectorAll('canvas')[idx];
  if (!c) return -1;
  const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data;
  let cnt = 0;
  for (let i = 3; i < d.length; i += 4) if (d[i] > 0) cnt++;
  return cnt;
}, n);

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);

// 1. Default indicators checked (EMA21/50/200)
const checked = await page.$$eval('.indicator-toggles .mat-button-toggle-checked', els => els.map(e => e.textContent.trim()));
console.log('default checked:', checked.join(','));
console.log('defaults OK:', checked.includes('EMA 21') && checked.includes('EMA 50') && checked.includes('EMA 200'));

// 2. Insights cards rendered
const ins = await page.$$eval('.insight .insight-label', els => els.map(e => e.textContent.trim()));
console.log('insights:', ins.join(' | '));
console.log('insights count >= 6:', ins.length >= 6);

const main0 = await painted(0);
console.log('main canvas painted:', main0, main0 > 1000 ? 'OK' : 'FAIL');

// 3. Toggle RSI + MACD + Bollinger + Volume + VWAP on
for (const label of ['RSI 14', 'MACD', 'Bollinger', 'Volume', 'VWAP']) {
  await group(label).click();
  await page.waitForTimeout(500);
}
await page.waitForTimeout(1200);
const canvasCount = await page.$$eval('canvas', els => els.length);
console.log('canvases after RSI+MACD on:', canvasCount, canvasCount === 3 ? 'OK (main+rsi+macd)' : 'FAIL');

const rsiPx = await painted(1);
const macdPx = await painted(2);
console.log('RSI panel painted:', rsiPx, rsiPx > 500 ? 'OK' : 'FAIL');
console.log('MACD panel painted:', macdPx, macdPx > 500 ? 'OK' : 'FAIL');
console.log('main still painted:', (await painted(0)) > 1000 ? 'OK' : 'FAIL');
console.log('checked now:', (await page.$$eval('.indicator-toggles .mat-button-toggle-checked', els => els.map(e => e.textContent.trim()))).join(','));

// 4. Uncheck RSI → panel destroyed
await group('RSI 14').click();
await page.waitForTimeout(700);
const afterOff = await page.$$eval('canvas', els => els.length);
console.log('canvases after RSI off:', afterOff, afterOff === 2 ? 'OK (panel destroyed)' : 'FAIL');

// 5. Range switch keeps everything alive
await page.locator('.range-toggle').locator('mat-button-toggle', { hasText: '1Y' }).click();
await page.waitForTimeout(2200);
const insAfter = await page.$$eval('.insight .insight-label', els => els.map(e => e.textContent.trim()));
console.log('insights after 1Y:', insAfter.length, 'cards');
console.log('main painted after 1Y:', (await painted(0)) > 1000 ? 'OK' : 'FAIL');
console.log('MACD panel after 1Y:', (await painted(1)) > 500 ? 'OK' : 'FAIL');

// 6. Persistence: reload → flags kept
await page.reload({ waitUntil: 'networkidle' });
await page.waitForTimeout(2200);
const checkedAfter = await page.$$eval('.indicator-toggles .mat-button-toggle-checked', els => els.map(e => e.textContent.trim()));
console.log('after reload checked:', checkedAfter.join(','));
console.log('persisted:', checkedAfter.includes('MACD') && checkedAfter.includes('Bollinger') && checkedAfter.includes('VWAP'));

console.log('console errors:', errors.length, errors.slice(0, 5));
await browser.close();
