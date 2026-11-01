// Automated accessibility check of the app shell, every workspace page and
// Flow Studio with axe-core in Chromium, in both colour themes.
// Works against the compiled Angular SPA served at localhost:4173.
// Mock backend only, no trading, credentials or DB mutations.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { chromium } from 'playwright';
import { installMocks } from './smoke-fixtures.mjs';

const base = process.env.A11Y_URL || 'http://127.0.0.1:4173/';
const axeSource = readFileSync(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
// WCAG 2.1 A and AA rules. Findings of any impact fail the check.
const tags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

const browser = await chromium.launch({ headless: true });
const failed = [];
let checks = 0;

async function audit(page, label, selector = 'fse-flow-studio') {
  checks++;
  await page.waitForTimeout(150);
  const result = await page.evaluate(async ({ selector, tags }) => {
    const r = await window.axe.run(document.querySelector(selector), { runOnly: { type: 'tag', values: tags } });
    return r.violations.map(v => ({ id: v.id, impact: v.impact, help: v.help, targets: v.nodes.slice(0, 5).map(n => n.target.join(' ')) }));
  }, { selector, tags });
  if (result.length) {
    failed.push(`${label}:\n` + result.map(v => `  [${v.impact}] ${v.id}: ${v.help}\n    ${v.targets.join('\n    ')}`).join('\n'));
  }
  console.log(`${result.length ? 'FAIL' : 'PASS'} ${label}${result.length ? ` (${result.length} rule(s))` : ''}`);
}

try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  const missing = await installMocks(page, { email: 'a11y@example.test' });
  await page.goto(new URL('#/flows', base).href, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('fse-flow-studio', { timeout: 20000 });
  await page.addScriptTag({ content: axeSource });

  for (const name of ['My Flows', 'Flow editor', 'Function library & triggers', 'Run history', 'Approvals & audit']) {
    await page.locator('fse-flow-studio').getByRole('tab', { name, exact: true }).click();
    await audit(page, `Flow Studio: ${name}`);
  }

  // A saved flow in the editor, with the agent node chosen from the keyboard.
  await page.locator('fse-flow-studio').getByRole('tab', { name: 'My Flows', exact: true }).click();
  await page.locator('fse-flow-studio').getByRole('button', { name: 'Edit', exact: true }).first().click();
  const agentNode = page.locator('fse-flow-studio [aria-label="Select Analyst"]');
  await agentNode.waitFor({ state: 'visible' });
  await agentNode.focus();
  await page.keyboard.press('Enter');
  checks++;
  try {
    await page.locator('fse-flow-studio [aria-label="Complete node settings"] legend', { hasText: 'Research tools' }).waitFor({ state: 'visible', timeout: 5000 });
    console.log('PASS Enter on a focused node opens its settings with research tools');
  } catch {
    failed.push('agent node settings with research tools did not open from the keyboard');
    console.log('FAIL Enter on a focused node opens its settings with research tools');
  }
  await audit(page, 'Flow editor: agent node settings');

  // Run history with the node trace and its recorded tool calls.
  await page.locator('fse-flow-studio').getByRole('tab', { name: 'My Flows', exact: true }).click();
  await page.locator('fse-flow-studio .overview-flow-card', { hasText: 'Filing summary' }).getByRole('button', { name: 'History', exact: true }).click();
  await page.locator('fse-flow-studio summary', { hasText: '#41' }).first().click();
  await page.locator('fse-flow-studio').getByRole('button', { name: 'Inspect nodes' }).first().click();
  await page.locator('fse-flow-studio [aria-label^="Execution graph for run"]').waitFor({ state: 'visible' });
  await audit(page, 'Run history: node trace');
  await page.locator('fse-flow-studio [aria-label="Analyst SUCCESS"]').focus();
  await page.keyboard.press('Enter');
  await page.locator('fse-flow-studio').getByText('not granted to this agent').first().waitFor({ state: 'visible', timeout: 5000 });
  await audit(page, 'Run history: agent step with tool calls');

  // An investor playbook with the screen and notify settings open.
  await page.locator('fse-flow-studio').getByRole('tab', { name: 'Flow editor', exact: true }).click();
  page.once('dialog', d => d.accept());
  await page.locator('fse-flow-studio').getByLabel('Starter templates').selectOption('price-alerts');
  await page.locator('fse-flow-studio [aria-label="Select Big moves"]').click();
  await page.locator('fse-flow-studio [aria-label="Complete node settings"]').getByLabel('Rule').waitFor({ state: 'visible', timeout: 5000 });
  await audit(page, 'Flow editor: investor screen settings');
  await page.locator('fse-flow-studio [aria-label="Select Email me"]').focus();
  await page.keyboard.press('Enter');
  await page.locator('fse-flow-studio [aria-label="Complete node settings"]').getByLabel('Send to').waitFor({ state: 'visible', timeout: 5000 });
  await audit(page, 'Flow editor: notify settings');

  // Every page of the workspace, with the shell, in both themes.
  const pages = ['', 'agent', 'flows', 'inbox', 'markets', 'plan', 'financials', 'portfolio', 'workspace', 'community', 'notifications', 'settings'];
  for (const theme of ['dark', 'light']) {
    await page.evaluate(t => { localStorage.setItem('fs.theme', t); document.documentElement.setAttribute('data-bs-theme', t); }, theme);
    for (const path of pages) {
      await page.goto(new URL('#/' + path, base).href, { waitUntil: 'networkidle' });
      await page.waitForSelector('#main-content > *:not(router-outlet)', { timeout: 10000 });
      await page.addScriptTag({ content: axeSource });
      await audit(page, `Page ${path || 'command-center'} (${theme})`, 'body');
    }
  }
  // Markets → leaderboard → backtest panel with the chart and its warnings.
  await page.goto(new URL('#/markets', base).href, { waitUntil: 'networkidle' });
  await page.getByRole('button', { name: 'Backtest this strategy' }).click();
  await page.locator('fs-backtest-panel svg[role="img"]').waitFor({ state: 'visible', timeout: 5000 });
  await page.addScriptTag({ content: axeSource });
  await audit(page, 'Markets: backtest panel', 'fs-backtest-panel');
  await page.getByRole('button', { name: /rebalances/ }).click();
  await audit(page, 'Markets: backtest rebalance table', 'fs-backtest-panel');

  // Settings → Plugins: installed plugins, an available update and the store.
  await page.goto(new URL('#/settings', base).href, { waitUntil: 'networkidle' });
  await page.getByRole('tab', { name: 'Plugins', exact: true }).click();
  await page.locator('fse-plugin-settings article').first().waitFor({ state: 'visible', timeout: 5000 });
  await page.addScriptTag({ content: axeSource });
  await audit(page, 'Settings: plugin store', 'fse-plugin-settings');

  // Overlays: the agent dock with a finished answer and the command palette.
  await page.goto(new URL('#/', base).href, { waitUntil: 'networkidle' });
  await page.addScriptTag({ content: axeSource });
  await page.fill('#hero-prompt', 'Research brief on NVDA');
  await page.press('#hero-prompt', 'Enter');
  await page.locator('fs-agent-dock .step').first().waitFor({ state: 'visible', timeout: 5000 });
  await audit(page, 'Agent dock with tool steps', 'fs-agent-dock');
  await page.keyboard.press('Escape');
  await page.keyboard.press('Control+k');
  await page.locator('.palette[open]').waitFor({ state: 'visible' });
  await audit(page, 'Command palette', '.palette');

  if (missing.size) console.log('Unmocked API paths:', [...missing].join(', '));
  assert.equal(errors.length, 0, errors.join(' | '));
} finally {
  await browser.close();
}
console.log(`\nAccessibility checks: ${checks - failed.length}/${checks} checks passed (axe WCAG 2.1 A/AA)`);
assert.equal(failed.length, 0, failed.join('\n'));
