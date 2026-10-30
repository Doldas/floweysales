// Manual check against a running engine: quarterly statements for the active instrument.
import { chromium } from 'playwright';

const base = process.env.SMOKE_URL || 'http://localhost:8080/';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
const errors = [];
page.on('pageerror', e => errors.push(e.message));
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });

await page.goto(new URL('#/financials', base).href, { waitUntil: 'networkidle' });
await page.waitForTimeout(3000);

const title = await page.locator('.financials-card .card-title').first().textContent().catch(() => null);
console.log('card title:', title, title?.trim() === 'Quarterly financials' ? 'OK' : 'FAIL');
const rows = await page.locator('.financials-card .fin-table tbody tr').count();
console.log('report rows:', rows, rows >= 3 ? 'OK' : 'FAIL');
const first = await page.locator('.financials-card .fin-table tbody tr').first().locator('td').allTextContents().catch(() => []);
console.log('first row cells:', JSON.stringify(first.map(c => c.trim())));
console.log('row has numeric data:', first.some(c => /\d/.test(c)) ? 'OK' : 'FAIL');
console.log('trend bars:', await page.locator('.fin-trend .fin-bar').count());
console.log('note/error alerts:', await page.locator('.financials-card .alert').count());

console.log('console errors:', errors.length, errors.slice(0, 3));
await browser.close();
