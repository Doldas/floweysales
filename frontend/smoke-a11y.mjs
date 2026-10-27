// Automated accessibility check of Flow Studio with axe-core in Chromium.
// Works against the compiled Angular SPA served at localhost:4173.
// Mock backend only, no trading, credentials or DB mutations.
import assert from 'node:assert/strict';
import { createRequire } from 'node:module';
import { readFileSync } from 'node:fs';
import { chromium } from 'playwright';

const base = process.env.A11Y_URL || 'http://127.0.0.1:4173/';
const axeSource = readFileSync(createRequire(import.meta.url).resolve('axe-core/axe.min.js'), 'utf8');
// WCAG 2.1 A and AA rules. Findings of any impact fail the check.
const tags = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'];

const graph = {
  version: 2,
  nodes: [
    { id: 'start', type: 'trigger', label: 'Start', x: 60, y: 140 },
    { id: 'agent', type: 'agent', label: 'Analyst', prompt: 'Summarise the filing.', tools: ['get_sec_facts'], max_tool_calls: 4, x: 300, y: 140 },
    { id: 'report', type: 'output', label: 'Report', x: 540, y: 140 },
  ],
  edges: [{ from: 'start', to: 'agent' }, { from: 'agent', to: 'report' }],
};
const flow = { id: 7, name: 'Filing summary', description: 'Accessibility fixture', enabled: false, graph, owner_id: 1, created_at: '2026-10-20T10:00:00Z', updated_at: '2026-10-25T10:00:00Z' };
const run = { id: 41, flow_id: 7, status: 'SUCCESS', trigger: 'manual', output: 'Revenue grew 12% (get_sec_facts, FY2025).', started_at: '2026-10-25T10:00:00Z', finished_at: '2026-10-25T10:00:09Z', created_at: '2026-10-25T10:00:00Z' };
const mocks = {
  '/api/auth/me': { enabled: true, user: { id: 1, email: 'a11y@example.test', role: 'SYS_ADMIN' }, permissions: ['flows.read'] },
  '/api/config': { symbol: 'NVDA', provider: 'YAHOO', trackers: [{ symbol: 'NVDA', provider: 'YAHOO' }] },
  '/api/ai/config': { configured: true, model: 'local-test', max_steps: 12, max_tool_calls: 48, can_configure: true, presets: [], tools: [] },
  '/api/ai/models/available': { models: [{ model: 'local-test', label: 'Local test' }], default_model: 'local-test' },
  '/api/admin/flows': { flows: [flow] },
  '/api/admin/flows/7': flow,
  '/api/admin/flows/7/runs': { runs: [run] },
  '/api/admin/flows/7/revisions': { revisions: [] },
  '/api/userspace/files': { files: [] },
  '/api/admin/flows/7/runs/41/snapshot': { graph },
  '/api/admin/flows/7/runs/41/steps': { steps: [
    { id: 1, node: 'start', label: 'Start', status: 'SUCCESS', output: '', duration_ms: 1, created_at: '2026-10-25T10:00:00Z' },
    { id: 2, node: 'agent', label: 'Analyst', status: 'SUCCESS', input: 'Summarise the filing.', output: 'Revenue grew 12%.', duration_ms: 8000, attempts: 1, created_at: '2026-10-25T10:00:08Z',
      tool_calls: [
        { attempt: 1, name: 'get_sec_facts', arguments: '{"symbol":"NVDA"}', ok: true, result_bytes: 2048 },
        { attempt: 1, name: 'search_web', arguments: '{}', ok: false, error: 'tool search_web is not granted to this agent', result_bytes: 0 },
      ] },
  ] },
  '/api/admin/blueprint-functions': { functions: [] },
  '/api/admin/blueprint-triggers': { triggers: [] },
  '/api/admin/flow-event-adapters': { adapters: [] },
  '/api/admin/flow-side-effects': { side_effects: [] },
  '/api/admin/flow-audit': { entries: [] },
  '/api/admin/flow-agent-tools': {
    tools: [{ name: 'get_sec_facts', description: 'SEC XBRL facts' }, { name: 'search_web', description: 'Web search' }],
    max_tools: 8, max_tool_calls: 16, default_tool_calls: 6,
  },
};

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
  const missing = new Set();
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', route => {
    const pathname = new URL(route.request().url()).pathname;
    const body = mocks[pathname];
    if (body === undefined) { missing.add(pathname); return route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"mock backend unavailable"}' }); }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
  await page.goto(base, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('.terminal-container', { timeout: 20000 });
  await page.addScriptTag({ content: axeSource });
  await page.getByRole('tab', { name: 'Flow Studio' }).click();
  await page.waitForSelector('fse-flow-studio');

  for (const name of ['My Flows', 'Flow editor', 'Function library & triggers', 'My Workspace', 'Run history', 'Approvals & audit']) {
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
  await page.locator('fse-flow-studio').getByRole('button', { name: 'History', exact: true }).first().click();
  await page.locator('fse-flow-studio summary', { hasText: '#41' }).first().click();
  await page.locator('fse-flow-studio').getByRole('button', { name: 'Inspect nodes' }).first().click();
  await page.locator('fse-flow-studio [aria-label^="Execution graph for run"]').waitFor({ state: 'visible' });
  await audit(page, 'Run history: node trace');
  await page.locator('fse-flow-studio [aria-label="Analyst SUCCESS"]').focus();
  await page.keyboard.press('Enter');
  await page.locator('fse-flow-studio').getByText('not granted to this agent').first().waitFor({ state: 'visible', timeout: 5000 });
  await audit(page, 'Run history: agent step with tool calls');

  if (missing.size) console.log('Unmocked API paths:', [...missing].join(', '));
  assert.equal(errors.length, 0, errors.join(' | '));
} finally {
  await browser.close();
}
console.log(`\nAccessibility checks: ${checks - failed.length}/${checks} checks passed (axe WCAG 2.1 A/AA)`);
assert.equal(failed.length, 0, failed.join('\n'));
