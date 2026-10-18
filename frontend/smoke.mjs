import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
page.on('pageerror', e => errors.push(e.message));

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(1500);

// 1. autocomplete search
const input = page.locator('input[placeholder*="VOLV"]').first();
await input.fill('microsoft');
await page.waitForTimeout(1200);
const options = await page.locator('mat-option').allTextContents();
console.log('search results:', options.slice(0, 4));

// 2. pick first option
await page.locator('mat-option').first().click();
await page.waitForTimeout(5000);
const badge = await page.evaluate(() => document.body.innerText.match(/\[([A-Z0-9.^=-]+)\]/)?.[1] ?? 'NONE');
console.log('tracked symbol badge:', badge);
const kpi = await page.evaluate(() => document.body.innerText.match(/LATEST PRICE\s+([\d.]+)/)?.[1] ?? 'NONE');
console.log('live price:', kpi);

// 3. theme switch
const themeSelect = page.locator('.theme-field mat-select').first();
await themeSelect.click();
await page.waitForTimeout(400);
const themeOpts = await page.locator('mat-option').allTextContents();
console.log('themes:', themeOpts);
if (themeOpts.some(t => t.includes('Matrix'))) {
  await page.locator('mat-option', { hasText: 'Matrix' }).click();
  await page.waitForTimeout(600);
}
const bg = await page.evaluate(() => getComputedStyle(document.body).backgroundColor);
console.log('body bg after theme switch:', bg);

// 4. wrong ticker error handling
await input.fill('ZZZZZQQ');
await page.locator('button:has-text("Track Asset")').click();
await page.waitForTimeout(800);
const snack = await page.evaluate(() => document.querySelector('snack-bar-container, .mdc-snackbar__label')?.textContent ?? 'NO_SNACKBAR');
console.log('error snackbar:', snack.trim().slice(0, 120));

console.log('console errors:', errors.length, errors.slice(0,5));
await browser.close();
