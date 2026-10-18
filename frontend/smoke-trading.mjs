import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));
page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });

const check = (label, ok, extra = '') =>
  console.log(`${ok ? 'OK  ' : 'FAIL'} ${label}${extra ? ' :: ' + extra : ''}`);

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(3000);

// ---------------------------------------------------------------- desk shell
check('trading desk rendered', (await page.locator('.ledger-card').count()) > 0);

const tiles = await page.$$eval('.account-strip .tile', (els) =>
  els.map((e) => e.textContent.replace(/\s+/g, ' ').trim()));
check('account tiles', tiles.length >= 5, JSON.stringify(tiles));

const tabLabels = await page.$$eval('.tabs [role="tab"]', (els) =>
  els.map((e) => e.textContent.replace(/\s+/g, ' ').trim()));
console.log('tabs:', JSON.stringify(tabLabels));

// ----------------------------------------------------------- client validation
await page.locator('input[name="qty"]').fill('0');
await page.waitForTimeout(250);
const vErr = await page.locator('.ticket-error').first().textContent().catch(() => '');
check('validation error for qty 0', (await page.locator('.ticket-error').count()) > 0, (vErr || '').trim());

// ------------------------------------------------------------- market buy flow
await page.locator('input[name="qty"]').fill('10');
await page.getByRole('button', { name: /Review buy order/i }).click();
await page.waitForSelector('.review', { timeout: 5000 });
const reviewRows = await page.$$eval('.review-row', (els) =>
  els.map((e) => e.textContent.replace(/\s+/g, ' ').trim()));
check('review step lists order details', reviewRows.length >= 4, JSON.stringify(reviewRows).slice(0, 220));

await page.getByRole('button', { name: /Confirm buy/i }).click();
await page.waitForTimeout(2500);
check('ticket reset to compose', (await page.locator('.review').count()) === 0);

// ------------------------------------------------------------------- blotter
await page.getByRole('tab', { name: /History/i }).click();
await page.waitForTimeout(500);
const histRows = await page.$$eval('.desk-table tbody tr', (els) => els.length);
const firstHist = await page.locator('.desk-table tbody tr').first().textContent().catch(() => '');
check('history shows the execution', histRows >= 1 && /FILLED/.test(firstHist || ''),
  (firstHist || '').replace(/\s+/g, ' ').trim().slice(0, 140));

await page.getByRole('tab', { name: /Positions/i }).click();
await page.waitForTimeout(500);
const posRow = await page.locator('.desk-table tbody tr').first().textContent().catch(() => '');
check('position opened', !!posRow && /GIB/.test(posRow) && /75\.3000/.test(posRow),
  (posRow || '').replace(/\s+/g, ' ').trim().slice(0, 140));

// ------------------------------------------------------- resting limit + cancel
const markText = await page.locator('.panel-title ~ .mark .mark-value').textContent().catch(() => '');
const mark = parseFloat((markText || '').replace(/[^\d.]/g, '')) || 0;
console.log('ticket mark:', mark);

await page.locator('mat-select[name="orderType"]').click();
await page.getByRole('option', { name: 'Limit', exact: true }).click();
await page.waitForTimeout(300);
await page.locator('input[name="qty"]').fill('5');
await page.locator('input[name="limitPrice"]').fill('1.00');
await page.getByRole('button', { name: /Review buy order/i }).click();
await page.waitForSelector('.review', { timeout: 5000 });
await page.getByRole('button', { name: /Confirm buy/i }).click();
await page.waitForTimeout(2000);

await page.getByRole('tab', { name: /Open orders/i }).click();
await page.waitForTimeout(500);
const openRow = await page.locator('.desk-table tbody tr').first().textContent().catch(() => '');
check('limit order rests in open orders',
  !!openRow && /GIB/i.test(openRow) && /1\.0000/.test(openRow),
  (openRow || '').replace(/\s+/g, ' ').trim().slice(0, 140));

const openCount = await page.$$eval('.desk-table tbody tr', (els) => els.length);
if (openCount > 0) {
  await page.locator('.desk-table tbody tr .mini.cancel').first().click();
  await page.waitForTimeout(1500);
  const after = await page.$$eval('.desk-table tbody tr', (els) => els.length);
  check('cancel removes it from open orders', after < openCount, `${openCount} -> ${after}`);

  await page.getByRole('tab', { name: /History/i }).click();
  await page.waitForTimeout(500);
  const cancelled = await page.locator('.status-chip.status-cancelled').count();
  check('cancelled order in history', cancelled > 0, `cancelled chips=${cancelled}`);
}

// --------------------------------------------------- over-sized buy rejected
await page.getByRole('tab', { name: /Positions/i }).click();
await page.waitForTimeout(300);
await page.locator('input[name="qty"]').fill('99999999');
await page.waitForTimeout(300);
const bpErr = await page.locator('.ticket-error').first().textContent().catch(() => '');
check('buying power guard', (await page.locator('.ticket-error').count()) > 0, (bpErr || '').trim());

console.log('console errors:', errors.length, errors.slice(0, 3));
await browser.close();
