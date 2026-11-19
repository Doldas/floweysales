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
const adminPages = ['', 'agent', 'flows', 'inbox', 'markets', 'collections', 'plan', 'financials', 'portfolio', 'workspace', 'community', 'notifications', 'settings'];
const browser = await chromium.launch({ headless: true });
const failed = [];
let assertions = 0;

async function testViewport(name, width, height, role = 'SYS_ADMIN') {
  // The service worker is covered by its own check; blocking it keeps the mocked API in charge.
  const page = await browser.newPage({
    serviceWorkers: 'block',
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
  const fits = async (locator, label) => {
    const box = await locator.first().boundingBox();
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

      // Installable app: the manifest is linked, served and names its icons.
      await go('');
      const manifest = await page.evaluate(async () => {
        const link = document.querySelector('link[rel="manifest"]');
        if (!link) return null;
        const res = await fetch(link.href);
        return res.ok ? res.json() : null;
      });
      check('web app manifest is linked and describes an installable app', !!manifest && manifest.display === 'standalone' && manifest.start_url === '/' &&
        manifest.icons.some(i => i.sizes === '512x512' && i.purpose === 'maskable') && manifest.icons.some(i => i.sizes === '192x192'), JSON.stringify(manifest)?.slice(0, 120));

      // Shell: the topbar always fits; the sidebar is a drawer below 992px.
      await go('');
      await fits(page.locator('.fs-topbar'), 'topbar');
      const sidebar = page.locator('.fs-sidebar');
      if (width < 992) {
        check('navigation drawer starts hidden', !(await sidebar.isVisible()));
        await page.getByRole('button', { name: 'Open navigation' }).click();
        await page.waitForTimeout(300);
        const open = await sidebar.boundingBox();
        check('navigation drawer opens within viewport', !!open && open.x >= -1 && open.x + open.width <= width + 1, JSON.stringify(open));
        await sidebar.getByRole('link', { name: 'Markets', exact: true }).click();
        await sidebar.waitFor({ state: 'hidden', timeout: 5000 });
        check('drawer closes after navigating', !(await sidebar.isVisible()));
      } else {
        const box = await sidebar.boundingBox();
        check('sidebar visible', !!box && box.x >= 0 && box.width > 200, JSON.stringify(box));
      }

      // Markets prioritizes the instrument chart. Key figures open on demand on phones.
      await go('markets');
      const controls = page.getByRole('region', { name: 'Instrument and trackers' });
      const priceChart = page.getByRole('region', { name: 'Price chart', exact: true });
      await priceChart.waitFor({ state: 'visible', timeout: 5000 });
      for (const [locator, label] of [[controls, 'instrument controls'], [priceChart, 'price chart']]) {
        check(`${label} displayed`, await locator.isVisible());
        await fits(locator, label);
      }
      const figures = page.getByRole('region', { name: 'Key figures', exact: true });
      if (width <= 700) {
        const chart = await priceChart.locator('.chart-container').boundingBox();
        check('mobile chart fits and is visible without scrolling', !!chart && chart.height <= 280 && chart.y < height && chart.y + chart.height > 0, JSON.stringify(chart));
        const toggle = page.getByRole('button', { name: 'Key figures', exact: true });
        check('mobile key figures start folded', (await toggle.getAttribute('aria-expanded')) === 'false' && !(await figures.isVisible()));
        await toggle.click();
      }
      await figures.waitFor({ state: 'visible', timeout: 5000 });
      check('key figures displayed', await figures.isVisible());
      await fits(figures, 'key figures');

      await go('portfolio');
      const ledger = page.locator('.ledger-card');
      check('ledger card displayed', await ledger.isVisible());
      await fits(ledger, 'ledger card');

      // The agent dock stays inside the viewport and is full screen on phones.
      await page.getByRole('button', { name: 'Toggle research agent' }).click();
      await page.locator('#fs-agent-dock.open').waitFor({ state: 'visible' });
      await page.waitForTimeout(300);
      const dock = await page.locator('#fs-agent-dock').boundingBox();
      check('agent dock remains within viewport', !!dock && dock.x >= -1 && dock.x + dock.width <= width + 1 && dock.y >= -1 && dock.y + dock.height <= height + 2, JSON.stringify(dock));
      if (width <= 575) check('mobile agent dock is full screen', !!dock && Math.abs(dock.width - width) <= 2);
      check('agent composer visible', await page.locator('#fs-agent-dock textarea[aria-label="Message the research agent"]').isVisible());
      await page.getByRole('button', { name: 'Close agent' }).click();
      await noOverflow('after closing the agent dock');
    } else {
      // USER: public-market research only.
      await go('');
      check('no paper portfolio for research accounts', await page.locator('.fs-sidebar a[href="#/portfolio"]').count() === 0);
      await go('portfolio');
      check('portfolio route redirects home', page.url().endsWith('#/'), page.url());
      check('no trading desk rendered', await page.locator('.ledger-card').count() === 0);

      await go('markets');
      await page.locator('.control-card input[role="combobox"]').fill('NVDA');
      await page.locator('.control-card [role="option"]').first().click();
      const priceChart = page.getByRole('region', { name: 'Price chart', exact: true });
      const newestBar = priceChart.getByTestId('chart-meta').getByText(/UTC/);
      await newestBar.waitFor({ state: 'visible', timeout: 5000 });
      check('research chart loads from history', await newestBar.isVisible());
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
