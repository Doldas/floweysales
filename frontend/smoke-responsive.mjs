// Responsive viewport smoke test on actual Chromium layout.
// Works against the compiled Angular SPA served at localhost:4173.
// Mock backend only, no trading, credentials or DB mutations.
import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { installMocks } from './smoke-fixtures.mjs';

const base = process.env.RESPONSIVE_URL || 'http://127.0.0.1:4173/';
const widths = [
  ['narrow phone', 320, 720],
  ['Galaxy S24', 360, 780],
  ['large phone', 384, 824],
  ['landscape/mobile', 600, 780],
  ['portrait tablet', 768, 1024],
  ['tablet', 1024, 768],
  ['desktop', 1440, 900],
];
const adminPages = ['', 'agent', 'flows', 'inbox', 'markets', 'plan', 'financials', 'portfolio', 'workspace', 'community', 'notifications', 'settings'];
const browser = await chromium.launch({ headless: true });
const failed = [];
let assertions = 0;

async function testViewport(name, width, height, role = 'SYS_ADMIN') {
  const page = await browser.newPage({
    viewport: { width, height },
    deviceScaleFactor: width <= 600 ? 3 : 1,
    isMobile: width <= 600,
    hasTouch: width <= 1024,
  });
  const errors = [];
  const financialRangeRequests = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('request', req => {
    const url = new URL(req.url());
    if (url.pathname === '/api/financials') financialRangeRequests.push({ from: url.searchParams.get('from_year'), to: url.searchParams.get('to_year') });
  });
  await installMocks(page, { role, permissions: [], email: 'responsive@example.test' });

  const check = (label, ok, details = '') => {
    assertions++;
    if (!ok) failed.push(`${name} ${width}px: ${label} ${details}`);
    console.log(`${ok ? 'PASS' : 'FAIL'} ${name} (${width}px): ${label} ${details}`);
  };
  const go = async path => {
    await page.goto(new URL('#/' + path, base).href, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector('#main-content > *:not(router-outlet)', { timeout: 20000 });
    await page.waitForTimeout(150);
  };
  const fits = async (selector, label = selector) => {
    const box = await page.locator(selector).first().boundingBox();
    check(`${label} fits horizontally`, !!box && box.x >= -2 && box.x + box.width <= width + 2, box ? `x=${box.x} w=${box.width}` : 'missing');
  };
  const noOverflow = async label => {
    const doc = await page.evaluate(() => ({ viewport: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
    check(`${label}: no horizontal page overflow`, doc.scroll <= doc.viewport + 2, `document=${doc.scroll}, viewport=${doc.viewport}`);
  };

  try {
    if (role === 'SYS_ADMIN') {
      for (const path of adminPages) {
        await go(path);
        await noOverflow(`#/${path}`);
      }

      // Shell: the topbar always fits; the sidebar is a drawer below 992px.
      await go('');
      await fits('.fs-topbar', 'topbar');
      const sidebar = await page.locator('.fs-sidebar').boundingBox();
      if (width < 992) {
        check('sidebar starts off-canvas', !!sidebar && sidebar.x + sidebar.width <= 1, JSON.stringify(sidebar));
        await page.getByRole('button', { name: 'Open navigation' }).click();
        await page.waitForTimeout(300);
        const open = await page.locator('.fs-sidebar').boundingBox();
        check('navigation drawer opens within viewport', !!open && open.x >= -1 && open.x + open.width <= width + 1, JSON.stringify(open));
        await page.locator('.fs-sidebar').getByRole('link', { name: 'Markets', exact: true }).click();
        await page.waitForTimeout(300);
        check('drawer closes after navigating', ((await page.locator('.fs-sidebar').boundingBox())?.x ?? 0) < 0);
      } else {
        check('sidebar visible', !!sidebar && sidebar.x >= 0 && sidebar.width > 200, JSON.stringify(sidebar));
      }

      // Markets keeps the instrument controls, key figures and chart.
      await go('markets');
      for (const selector of ['.control-card', '.grid', '.chart-card']) {
        check(`${selector} displayed`, await page.locator(selector).isVisible());
        await fits(selector);
      }
      const chart = await page.locator('.chart-container').boundingBox();
      if (width <= 700) check('mobile chart height fits', !!chart && chart.height <= 280, JSON.stringify(chart));

      await go('portfolio');
      check('.ledger-card displayed', await page.locator('.ledger-card').isVisible());
      await fits('.ledger-card');

      // The agent dock stays inside the viewport and is full screen on phones.
      await page.getByRole('button', { name: 'Toggle research agent' }).click();
      await page.locator('#fs-agent-dock.open').waitFor({ state: 'visible' });
      await page.waitForTimeout(300);
      const dock = await page.locator('#fs-agent-dock').boundingBox();
      check('agent dock remains within viewport', !!dock && dock.x >= -1 && dock.x + dock.width <= width + 1 && dock.y >= -1 && dock.y + dock.height <= height + 2, JSON.stringify(dock));
      if (width <= 575) check('mobile agent dock is full screen', !!dock && Math.abs(dock.width - width) <= 2);
      check('agent composer visible', await page.locator('#fs-agent-dock textarea').isVisible());
      await page.getByRole('button', { name: 'Close agent' }).click();
      await noOverflow('after closing the agent dock');
    } else {
      // USER: public-market research only.
      await go('');
      check('no paper portfolio for research accounts', await page.locator('.fs-sidebar').getByRole('link', { name: 'Paper portfolio' }).count() === 0);
      await go('portfolio');
      check('portfolio route redirects home', page.url().endsWith('#/'), page.url());
      check('no trading desk rendered', await page.locator('.ledger-card').count() === 0);

      await go('markets');
      await page.locator('.control-card input[role="combobox"]').fill('NVDA');
      await page.locator('.control-card [role="option"]').first().click();
      await page.locator('.chart-card').waitFor({ state: 'visible' });
      check('research chart loads from history', await page.locator('.chart-card').isVisible());
      await noOverflow('research markets');

      await go('financials');
      await page.locator('.research-container .fin-year-controls').getByRole('button', { name: 'Year range' }).click();
      const chosenYear = String(new Date().getFullYear() - 5);
      await page.locator('#fin-from').selectOption(chosenYear);
      await page.waitForTimeout(200);
      check('custom from_year/to_year sent to API',
        financialRangeRequests.some(q => q.from === chosenYear && q.to === String(new Date().getFullYear())),
        JSON.stringify(financialRangeRequests));
      check('mobile quarterly report cards visible', await page.locator('.fin-mobile-report').first().isVisible());
      await noOverflow('research financials');
    }
    check('no uncaught JavaScript errors', errors.length === 0, errors.join(' | '));
  } catch (e) {
    failed.push(`${name} ${width}px: ${e.stack || e.message}`);
  } finally {
    await page.close();
  }
}
try {
  for (const [name, width, height] of widths) await testViewport(name, width, height);
  await testViewport('Galaxy S24 USER', 360, 780, 'USER');
} finally {
  await browser.close();
}
console.log(`\nResponsive smoke checks: ${assertions - failed.length}/${assertions} assertions passed`);
assert.equal(failed.length, 0, failed.join('\n'));
