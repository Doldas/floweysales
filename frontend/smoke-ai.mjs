import { chromium } from 'playwright';

// End-to-end smoke test for the research agent dock. Requires:
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

const base = process.env.SMOKE_URL || 'http://localhost:8080/';
await page.goto(base, { waitUntil: 'networkidle' });
await page.waitForTimeout(1500);

// 1. Point the server at the mock model (Settings → API integrations does the same PUT).
const configured = await page.evaluate(async (mock) => {
  const res = await fetch('/api/ai/config', { method: 'PUT', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ base_url: mock, model: 'mock-model', max_steps: 12, max_tool_calls: 48 }) });
  return res.ok;
}, MOCK);
check('mock model configured', configured);
await page.reload({ waitUntil: 'networkidle' });

// 2. The dock opens from the top bar with contextual suggestions.
await page.getByRole('button', { name: 'Toggle research agent' }).click();
await page.locator('#fs-agent-dock.open').waitFor({ state: 'visible' });
check('agent dock opens', await page.locator('#fs-agent-dock').isVisible());
const suggestions = await page.locator('#fs-agent-dock .suggestion').count();
check('suggestions rendered', suggestions > 0, `${suggestions} cards`);

// 3. Send a message; tool calls render as agent steps.
await page.locator('#fs-agent-dock textarea').fill('market stats please');
await page.locator('#fs-agent-dock textarea').press('Enter');
await page.waitForSelector('#fs-agent-dock .step', { timeout: 20000 }).catch(() => {});
const stepCount = await page.locator('#fs-agent-dock .step').count();
check('tool call rendered as agent step', stepCount >= 1, `${stepCount} step(s)`);
await page.locator('#fs-agent-dock .step summary').first().click();
const toolText = (await page.locator('#fs-agent-dock .step-pre').allTextContents()).join('\n');
check('tool result has backend data', /record_count|min_price|symbol/.test(toolText), toolText.slice(0, 80).replace(/\n/g, ' '));

await page
  .waitForFunction(() => [...document.querySelectorAll('#fs-agent-dock .msg-agent .bubble.chat-markdown')].some(b => (b.textContent || '').trim().length > 0), null, { timeout: 20000 })
  .then(() => check('assistant streamed response', true))
  .catch(() => check('assistant streamed response', false));

const md = await page.evaluate(() => {
  const bubble = document.querySelector('#fs-agent-dock .msg-agent .bubble.chat-markdown');
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
