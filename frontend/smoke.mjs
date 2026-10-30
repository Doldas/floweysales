// Manual smoke test against a running engine (make run): boot, ticker search,
// tracking, theme switch and the unknown-ticker toast.
import { chromium } from 'playwright';

const base = process.env.SMOKE_URL || 'http://localhost:8080/';
const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
page.on('pageerror', e => errors.push(e.message));

await page.goto(new URL('#/markets', base).href, { waitUntil: 'networkidle' });
await page.waitForTimeout(1500);

// 1. autocomplete search
const input = page.locator('.control-card input[role="combobox"]');
await input.fill('microsoft');
await page.waitForTimeout(1200);
const options = await page.locator('.control-card [role="option"]').allTextContents();
console.log('search results:', options.slice(0, 4).map(o => o.trim()));

// 2. pick first option
await page.locator('.control-card [role="option"]').first().click();
await page.waitForTimeout(5000);
console.log('active instrument:', (await page.locator('.quote-id h1').textContent())?.trim());
const kpi = await page.evaluate(() => document.body.innerText.match(/Latest price\s+([\d.]+)/i)?.[1] ?? 'NONE');
console.log('live price:', kpi);

// 3. theme switch
const before = await page.evaluate(() => document.documentElement.dataset.bsTheme);
await page.getByRole('button', { name: /Switch to (light|dark) theme/ }).click();
const after = await page.evaluate(() => document.documentElement.dataset.bsTheme);
console.log('theme switch:', before, '→', after, before !== after ? 'OK' : 'FAIL');

// 4. wrong ticker error handling
await input.fill('ZZZZZQQ');
await input.press('Enter');
await page.waitForTimeout(1200);
const toast = await page.locator('.fs-toast').first().textContent().catch(() => 'NO_TOAST');
console.log('error toast:', (toast ?? '').trim().slice(0, 120));

console.log('console errors:', errors.length, errors.slice(0, 5));
await browser.close();
