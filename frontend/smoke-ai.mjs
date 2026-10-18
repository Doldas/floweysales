import { chromium } from 'playwright';

// End-to-end smoke test for the AI chat panel. Requires:
//   - backend on :8080 (serving the built app)
//   - mock OpenAI-compatible server on :8099 (mock_openai.mjs)

const MOCK = process.env.MOCK_URL || 'http://localhost:8099/v1';

const browser = await chromium.launch();
const page = await browser.newPage();
const errors = [];
page.on('pageerror', (e) => errors.push(e.message));
page.on('console', (m) => {
  if (m.type() === 'error') errors.push(m.text());
});

const results = [];
const check = (label, ok, extra = '') => {
  results.push(ok);
  console.log(`${ok ? 'OK  ' : 'FAIL'} ${label}${extra ? ' :: ' + extra : ''}`);
};

await page.goto('http://localhost:8080/', { waitUntil: 'networkidle' });
await page.waitForTimeout(2500);

// 1. Launcher present, panel opens.
const wrapper = page.locator('fse-ai-chat .ai-chat-wrapper');
check('ai-chat component rendered', (await wrapper.count()) === 1);

if ((await page.locator('.ai-launcher').count()) === 0) {
  // starts expanded? collapse first
  const minBtn = page.locator('.ai-chat-header .icon-btn').last();
  if (await minBtn.count()) await minBtn.click();
  await page.waitForTimeout(300);
}

const hasLauncher = (await page.locator('.ai-launcher').count()) === 1;
check('collapsed launcher visible', hasLauncher);

await page.locator('.ai-launcher').click();
await page.waitForTimeout(400);
check('panel expands on click', (await page.locator('.ai-chat-panel').count()) === 1);

// 2. Empty state + quick prompts.
const suggestions = await page.locator('.ai-suggestion-btn').count();
check('quick prompts rendered', suggestions > 0, `${suggestions} buttons`);

// 3. Settings loads presets from /api/ai/config.
await page.locator('.ai-chat-header .icon-btn').first().click();
await page.waitForTimeout(800);
const presets = await page.locator('.preset-chip').count();
check('settings presets loaded', presets >= 4, `${presets} presets`);

// 4. Configure the mock endpoint.
const inputs = page.locator('.ai-settings .field input');
await inputs.nth(0).fill(MOCK); // base URL
await inputs.nth(1).fill('mock-model'); // model
await page.locator('.ai-settings .settings-actions button').first().click();
await page.waitForTimeout(500);
check('settings closed after save', (await page.locator('.ai-settings').count()) === 0);

// 5. Send a message and wait for the streamed answer.
await page.locator('.ai-input').fill('market stats please');
await page.locator('.ai-send').click();

await page.waitForSelector('.ai-tool', { timeout: 20000 }).catch(() => {});
const toolCount = await page.locator('.ai-tool').count();
check('tool call rendered', toolCount >= 1, `${toolCount} tool card(s)`);

const toolTexts = await page.locator('.ai-tool pre').allTextContents();
const toolText = toolTexts.join('\n');
check('tool result has backend data', /record_count|min_price|symbol/.test(toolText), toolText.slice(0, 80).replace(/\n/g, ' '));

await page
  .waitForFunction(
    () => {
      const bubbles = [...document.querySelectorAll('.ai-message.assistant .bubble')];
      return bubbles.some((b) => (b.textContent || '').trim().length > 0);
    },
    null,
    { timeout: 20000 },
  )
  .then(() => check('assistant streamed response', true))
  .catch(() => check('assistant streamed response', false));

const md = await page.evaluate(() => {
  const bubble = document.querySelector('.ai-message.assistant .bubble.chat-markdown');
  if (!bubble) return null;
  return {
    headings: bubble.querySelectorAll('h1,h2,h3,h4,h5,h6').length,
    lists: bubble.querySelectorAll('ul li, ol li').length,
    tables: bubble.querySelectorAll('table').length,
    codeBlocks: bubble.querySelectorAll('.md-code').length,
    links: bubble.querySelectorAll('a[href]').length,
    scripts: bubble.querySelectorAll('script').length,
  };
});
console.log('markdown elements:', JSON.stringify(md));
check('markdown rendered', !!md && md.headings > 0 && md.lists > 0 && md.tables > 0 && md.codeBlocks > 0 && md.links > 0);
check('no raw HTML/scripts injected', !md || md.scripts === 0);

// 6. No console errors.
check('no console errors', errors.length === 0, errors.slice(0, 3).join(' | '));

await browser.close();

const failed = results.filter((r) => !r).length;
console.log(`\n${results.length - failed}/${results.length} checks passed`);
process.exit(failed ? 1 : 0);
