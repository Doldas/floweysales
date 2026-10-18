import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('pageerror', e => errors.push(e.message));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(3000);

const title = await page.locator('.financials-card mat-card-title').first().textContent().catch(() => null);
console.log('card title:', title, title?.trim() === 'Quarterly Financials' ? 'OK' : 'FAIL');

const source = await page.locator('.financials-card .fin-source').first().textContent().catch(() => '');
console.log('source badge:', source.trim());

const rows = await page.$$eval('.financials-card tbody tr, .financials-card .mat-mdc-row', els => els.length);
console.log('report rows:', rows, rows >= 3 ? 'OK' : 'FAIL');

const first = await page.evaluate(() => {
  const row = document.querySelector('.financials-card .mat-mdc-row');
  if (!row) return null;
  const cells = [...row.querySelectorAll('.mat-mdc-cell, td')].map(c => c.textContent.trim());
  return cells;
});
console.log('first row cells:', JSON.stringify(first));

const hasData = first && first.some(c => /\d/.test(c));
console.log('row has numeric data:', hasData ? 'OK' : 'FAIL');

const noteOrError = await page.locator('.financials-card .fin-note, .financials-card .fin-error').count();
console.log('note/error boxes:', noteOrError);

console.log('console errors:', errors.length, errors.slice(0, 3));
await browser.close();
