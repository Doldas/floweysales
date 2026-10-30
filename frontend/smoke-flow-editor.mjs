// Flow Studio editing paths in Chromium: create, wire, preflight, save,
// publish, run and inspect the node trace with its tool calls.
// Works against the compiled Angular SPA served at localhost:4173.
// Stateful mock backend only, no trading, credentials or DB mutations.
import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const base = process.env.FLOW_EDITOR_URL || 'http://127.0.0.1:4173/';
const now = () => new Date().toISOString();

// --- Stateful mock of the Flow Studio API ---
const state = { flows: [], runs: [], steps: {}, requests: [] };
function flowById(id) { return state.flows.find(f => f.id === id); }
function handle(method, pathname, body) {
  let m;
  if (pathname === '/api/auth/me') return { enabled: true, user: { id: 1, email: 'editor@example.test', role: 'SYS_ADMIN' }, permissions: ['flows.read'] };
  if (pathname === '/api/config') return { symbol: 'NVDA', provider: 'YAHOO', trackers: [{ symbol: 'NVDA', provider: 'YAHOO' }] };
  if (pathname === '/api/ai/config') return { configured: true, model: 'local-test', max_steps: 12, max_tool_calls: 48, can_configure: true, presets: [], tools: [] };
  if (pathname === '/api/ai/models/available') return { models: [{ model: 'local-test', label: 'Local test' }], default_model: 'local-test' };
  if (pathname === '/api/admin/blueprint-functions') return { functions: [] };
  if (pathname === '/api/admin/blueprint-triggers') return { triggers: [] };
  if (pathname === '/api/admin/flow-agent-tools') return { tools: [{ name: 'get_sec_facts', description: 'SEC XBRL facts' }], max_tools: 8, max_tool_calls: 16, default_tool_calls: 6 };
  if (pathname === '/api/userspace/files') return { files: [] };
  if (pathname === '/api/admin/flows' && method === 'GET') return { flows: state.flows };
  if (pathname === '/api/admin/flows' && method === 'POST') {
    const flow = { id: state.flows.length + 1, ...body, active_revision: null, latest_revision: null, has_unpublished_changes: true, created_at: now(), updated_at: now() };
    state.flows.push(flow);
    return flow;
  }
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)$/))) {
    const flow = flowById(+m[1]);
    if (method === 'PUT') Object.assign(flow, body, { has_unpublished_changes: true, updated_at: now() });
    return flow;
  }
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/publish$/))) {
    const flow = flowById(+m[1]);
    flow.active_revision = flow.latest_revision = (flow.latest_revision || 0) + 1;
    flow.has_unpublished_changes = false;
    return { ...flow, active_revision: flow.active_revision };
  }
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/revisions$/))) {
    const flow = flowById(+m[1]);
    return { revisions: flow?.active_revision ? [{ revision: flow.active_revision, note: 'first', created_at: now(), active: true }] : [] };
  }
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/readiness$/))) return { applies: false, ready: true, checks: [] };
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/run$/))) {
    const flow = flowById(+m[1]);
    const run = { id: 100 + state.runs.length, flow_id: flow.id, status: 'SUCCESS', trigger: 'manual', flow_revision: flow.active_revision,
      output: 'Revenue grew 12% (get_sec_facts, FY2025).', error: '', started_at: now(), finished_at: now(), duration_ms: 900 };
    state.runs.push(run);
    const agents = flow.graph.nodes.filter(n => n.type === 'agent');
    state.steps[run.id] = flow.graph.nodes.map((n, i) => ({ id: i + 1, node: n.id, label: n.label, status: 'SUCCESS', output: n.type === 'agent' ? 'Revenue grew 12%.' : '', duration_ms: 10, created_at: now(),
      ...(n.id === agents[0]?.id ? { tool_calls: [{ attempt: 1, name: 'get_sec_facts', arguments: '{"symbol":"NVDA"}', ok: true, result_bytes: 2048 }] } : {}) }));
    return { run_id: run.id, flow_revision: run.flow_revision };
  }
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/runs$/))) return { runs: state.runs.filter(r => r.flow_id === +m[1]) };
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/runs\/(\d+)\/steps$/))) return { steps: state.steps[+m[2]] || [] };
  if ((m = pathname.match(/^\/api\/admin\/flows\/(\d+)\/runs\/(\d+)\/snapshot$/))) return { graph: flowById(+m[1]).graph };
  return undefined;
}

const browser = await chromium.launch({ headless: true });
const failed = [];
let checks = 0;
function check(label, ok, details = '') {
  checks++;
  if (!ok) failed.push(`${label} ${details}`);
  console.log(`${ok ? 'PASS' : 'FAIL'} ${label}${ok || !details ? '' : ' ' + details}`);
}

try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  await page.route('**/api/**', async route => {
    const req = route.request();
    const pathname = new URL(req.url()).pathname;
    const body = req.postData() ? JSON.parse(req.postData()) : undefined;
    if (req.method() !== 'GET') state.requests.push({ method: req.method(), pathname, body });
    const response = handle(req.method(), pathname, body);
    if (response === undefined) return route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"mock backend unavailable"}' });
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(response) });
  });
  await page.goto(new URL('#/flows', base).href, { waitUntil: 'domcontentloaded' });
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
  const validation = studio.locator('[aria-label="Flow graph validation"] [role="status"]');

  // 1. Create: a new flow starts as Start → Research and passes preflight.
  await studio.getByLabel('Flow name').fill('E2E filing review');
  check('new flow passes preflight', (await validation.innerText()) === 'No structural issues', await validation.innerText());

  // 2. Preflight: an unconnected agent is reported before anything is saved.
  await studio.getByRole('button', { name: '+ Add node' }).click();
  await studio.getByLabel('Search nodes').fill('agent');
  await studio.locator('#node-picker').getByRole('button', { name: 'Agent', exact: true }).click();
  await page.keyboard.press('Escape');
  await page.waitForTimeout(100);
  const issues = await validation.innerText();
  check('unconnected agent is reported by preflight', /issue/.test(issues), issues);
  check('preflight issue links to the node', (await studio.locator('[aria-label="Flow graph validation"] li button').count()) > 0);

  // 3. Wire: click-to-connect from Research's output port into the new agent.
  await studio.locator('[aria-label="Start connection from Research"]').click();
  await studio.getByRole('status').filter({ hasText: 'Select an input port' }).waitFor();
  await studio.locator('[aria-label="Connect into Research agent"]').click();
  await page.waitForTimeout(100);
  check('wiring clears the preflight issue', (await validation.innerText()) === 'No structural issues', await validation.innerText());

  // Grant Research a tool and require grounded figures from the keyboard-opened settings.
  await studio.locator('[aria-label="Select Research"]').focus();
  await page.keyboard.press('Enter');
  const settings = studio.locator('[aria-label="Complete node settings"]');
  await settings.getByLabel('SEC facts').check();
  await settings.getByLabel('Require grounded figures').check();
  await settings.getByRole('button', { name: 'Close node settings' }).click();

  // An output added with the new agent selected is wired to it.
  await studio.locator('[aria-label="Select Research agent"]').click();
  await studio.getByRole('button', { name: '+ Add node' }).click();
  await studio.getByLabel('Search nodes').fill('output');
  await studio.locator('#node-picker').getByRole('button', { name: 'Output', exact: true }).click();
  await page.keyboard.press('Escape');

  // 4. Save: the server receives exactly the graph built in the editor.
  await studio.getByRole('button', { name: 'Save flow' }).click();
  await studio.getByText('All changes saved').waitFor();
  const save = state.requests.find(r => r.method === 'POST' && r.pathname === '/api/admin/flows');
  const graph = save?.body?.graph;
  const types = graph?.nodes.map(n => n.type).sort().join(',');
  check('save sends the flow name', save?.body?.name === 'E2E filing review', JSON.stringify(save?.body?.name));
  check('save sends trigger, two agents and an output', types === 'agent,agent,output,trigger', types);
  const byLabel = label => graph?.nodes.find(n => n.label === label)?.id;
  const hasEdge = (from, to) => graph?.edges.some(e => e.from === from && e.to === to);
  check('saved graph has the wired edge Research → Research agent', hasEdge(byLabel('Research'), byLabel('Research agent')));
  check('saved graph has the edge Research agent → Output Result', hasEdge(byLabel('Research agent'), byLabel('Output Result')));
  const research = graph?.nodes.find(n => n.label === 'Research');
  check('save sends the tool grant and grounding', research?.tools?.join() === 'get_sec_facts' && research?.require_grounding === true, JSON.stringify(research));
  check('saved flow is a disabled manual flow', save?.body?.enabled === false && !save?.body?.interval_minutes && !save?.body?.event_key, JSON.stringify(save?.body));

  // 5. Publish: the saved draft becomes revision 1.
  const versions = studio.locator('[aria-label="Published versions"] [role="status"]');
  check('unpublished flow says runs use the draft', (await versions.innerText()).startsWith('Not published'), await versions.innerText());
  await studio.getByLabel('Publish note').fill('first review');
  await studio.getByRole('button', { name: 'Publish revision' }).click();
  await versions.filter({ hasText: 'Active: revision 1' }).waitFor();
  const publish = state.requests.find(r => r.pathname.endsWith('/publish'));
  check('publish sends the note', publish?.body?.note === 'first review', JSON.stringify(publish?.body));
  check('publish is disabled when the draft matches', await studio.getByRole('button', { name: 'Publish revision' }).isDisabled());

  // 6. Run: the active revision is queued.
  await studio.getByRole('button', { name: 'Run now', exact: true }).last().click();
  await studio.getByText('Agent run queued (rev 1)').waitFor();
  const run = state.requests.find(r => r.pathname.endsWith('/run'));
  check('run targets the active revision', run?.body?.target === 'active', JSON.stringify(run?.body));

  // 7. Inspect: the run's node trace lists the agent's tool calls.
  await studio.getByRole('tab', { name: 'Run history', exact: true }).click();
  await studio.getByRole('button', { name: 'Refresh history' }).click();
  await studio.locator('summary', { hasText: '#100' }).first().click();
  await studio.getByRole('button', { name: 'Inspect nodes' }).first().click();
  await studio.locator('[aria-label="Research SUCCESS"]').click();
  await studio.getByText('get_sec_facts').first().waitFor({ timeout: 5000 });
  check('node trace shows the recorded tool call', await studio.getByText('get_sec_facts').first().isVisible());
  check('run history labels the revision', (await studio.locator('summary', { hasText: '#100' }).first().innerText()).includes('rev 1'));

  check('no uncaught JavaScript errors', errors.length === 0, errors.join(' | '));
} catch (e) {
  failed.push(e.stack || e.message);
} finally {
  await browser.close();
}
console.log(`\nFlow editor checks: ${checks - failed.length}/${checks} passed`);
assert.equal(failed.length, 0, failed.join('\n'));
