// Responsive viewport smoke test on actual Chromium layout.
// Works against the compiled Angular SPA served at localhost:4173.
// Mock backend only, no trading, credentials or DB mutations.
import assert from 'node:assert/strict';
import { chromium } from 'playwright';

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
  page.on('pageerror', e => errors.push(e.message));

  await page.route('**/api/**', async route => {
    const pathname = new URL(route.request().url()).pathname;
    let body;
    if (pathname === '/api/auth/me') {
      body = JSON.stringify({ enabled: true, user: { id: 1, email: 'responsive@example.test', role } });
    } else if (pathname === '/api/config') {
      body = JSON.stringify({ symbol: 'NVDA', provider: 'YAHOO' });
    } else if (pathname === '/api/ai/config') {
      body = JSON.stringify({ configured: true, model: 'local-test', max_steps: 12, max_tool_calls: 48, can_configure: role === 'SYS_ADMIN', presets: [], tools: [] });
    } else {
      return route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"mock backend unavailable"}' });
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body });
  });
  const check = (label, ok, details = '') => {
    assertions++;
    if (!ok) failed.push(`${name} ${width}px: ${label} ${details}`);
    console.log(`${ok ? 'PASS' : 'FAIL'} ${name} (${width}px): ${label} ${details}`);
  };
  try {
    await page.goto(base, { waitUntil: 'domcontentloaded' });
    await page.waitForSelector(role === 'USER' ? '.research-container' : '.terminal-container', { timeout: 20000 });

    const layout = await page.evaluate(() => {
      const doc = document.documentElement;
      return {
        viewport: doc.clientWidth,
        documentWidth: doc.scrollWidth,
        topbarWidth: document.querySelector('.topbar')?.getBoundingClientRect().width,
        rowWidth: document.querySelector('.control-card')?.getBoundingClientRect().width,
      };
    });
    check('no horizontal page overflow', layout.documentWidth <= layout.viewport + 2,
      `document=${layout.documentWidth}, viewport=${layout.viewport}`);

    if (role === 'SYS_ADMIN') {
      for (const selector of ['.topbar', '.control-card', '.grid', '.chart-card', '.ledger-card']) {
        const element = page.locator(selector);
        check(`${selector} displayed`, await element.count() === 1);
        if (await element.count() > 0) {
          const box = await element.boundingBox();
          check(`${selector} fits horizontally`, !!box && box.x >= -2 && box.x + box.width <= width + 2,
            box ? `x=${box.x} w=${box.width}` : 'missing');
        }
      }
      const breakpoint = await page.evaluate(() => ({
        grid: getComputedStyle(document.querySelector('.grid')).gridTemplateColumns,
        chart: document.querySelector('.chart-container')?.getBoundingClientRect().height,
      }));
      if (width <= 700) check('mobile chart height fits', !!breakpoint.chart && breakpoint.chart <= 280, JSON.stringify(breakpoint));

      const launcher = page.locator('.ai-launcher');
      check('AI launcher available', await launcher.isVisible());
      if (await launcher.isVisible()) {
        await launcher.click();
        await page.waitForSelector('.ai-chat-panel');
        const panel = await page.locator('.ai-chat-panel').boundingBox();
        check('AI chat remains within viewport', !!panel && panel.x >= -1 && panel.x + panel.width <= width + 1 && panel.y >= -1 && panel.y + panel.height <= height + 2, JSON.stringify(panel));
        if (width <= 700) check('mobile AI panel is fullscreen', !!panel && Math.abs(panel.width - width) <= 2);
        check('AI composer visible', await page.locator('.ai-input').isVisible());
        await page.getByRole('button', { name: 'Minimize' }).click();
      }
    } else {
      check('research page visible', await page.locator('.research-container').isVisible());
      check('no admin trading terminal', await page.locator('.ledger-card').count() === 0);
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
