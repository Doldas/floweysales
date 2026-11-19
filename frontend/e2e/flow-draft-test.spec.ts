import { expect, open, test } from './fixtures';
import type { Page, Route } from '@playwright/test';
// @ts-ignore: no type declarations for the .mjs fixtures
import { flows } from '../smoke-fixtures.mjs';

// Draft tests (#273): "Test unsaved draft" runs exactly what the editor shows,
// without saving. The mock backend stands in for the Go engine; these tests
// pin the browser side of the contract: the snapshot sent, the node-level
// preflight, and how a refused run is reported.
const DRAFT_RUN = '**/api/admin/flows/draft/run';

// Filling the name makes the editor dirty, which would make a template choice ask to discard.
async function newFlow(page: Page, name: string | null = 'Draft under test') {
  await open(page, 'flows');
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
  if (name) await studio.getByLabel('Flow name').fill(name);
  return studio;
}

function draftRun(overrides: Partial<{ status: number; body: unknown }> = {}) {
  return (route: Route) => route.fulfill({
    status: overrides.status ?? 202,
    contentType: 'application/json',
    body: JSON.stringify(overrides.body ?? { status: 'queued', run_id: 301, draft_test: true, scratch: true, simulated: true, graph_hash: 'a'.repeat(64) }),
  });
}

test.describe('Draft tests of unsaved flows', () => {
  test('editing the graph while a draft test is in flight does not change what runs', async ({ page }) => {
    const sent: any[] = [];
    let runStatus = 'RUNNING';
    let release!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    await page.route(DRAFT_RUN, async route => {
      sent.push(route.request().postDataJSON());
      await gate;
      await draftRun()(route);
    });
    await page.route('**/api/admin/flows/draft/runs', route => route.fulfill({
      json: { runs: [{ id: 301, status: runStatus, trigger: 'draft test', output: '', error: '', graph_hash: 'a'.repeat(64) }] },
    }));
    await page.route('**/api/admin/flows/draft/runs/301/steps', route => route.fulfill({ json: { steps: [] } }));
    await page.route('**/api/admin/flows/draft/runs/301/snapshot', route => route.fulfill({ json: { graph: sent[0].graph } }));

    const studio = await newFlow(page, null);
    await studio.getByLabel('Starter templates').selectOption('price-alerts');
    await studio.getByText('Playbook copied as a new, disabled flow').waitFor();
    await studio.locator('[aria-label="Select Big moves"]').click();
    const settings = studio.locator('[aria-label="Complete node settings"]');
    await settings.getByLabel('Rule').fill('change_1d_pct <= -5');
    await settings.getByRole('button', { name: 'Close node settings' }).click();

    await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
    await expect.poll(() => sent.length).toBe(1);

    // The request is still pending: keep editing the same node.
    await studio.locator('[aria-label="Select Big moves"]').click();
    await settings.getByLabel('Rule').fill('change_1d_pct <= -9');
    await settings.getByRole('button', { name: 'Close node settings' }).click();

    release();
    await expect(studio.getByText('Draft test queued from your unsaved editor graph')).toBeVisible();

    const rules = (graph: any) => JSON.stringify(graph.nodes);
    expect(rules(sent[0].graph)).toContain('change_1d_pct <= -5');
    expect(rules(sent[0].graph)).not.toContain('change_1d_pct <= -9');
    // The edit made during the run is kept and still unsaved.
    await expect(studio.getByText('Unsaved changes')).toBeVisible();
    await studio.locator('[aria-label="Select Big moves"]').click();
    await expect(settings.getByLabel('Rule')).toHaveValue('change_1d_pct <= -9');
    await settings.getByRole('button', { name: 'Close node settings' }).click();

    await expect(studio.getByRole('region', { name: 'Run 301' })).toContainText('Running');
    await studio.locator('[aria-label="Select Watchlist quotes"]').click();
    await settings.getByRole('button', { name: 'Delete node' }).click();
    await expect(studio.locator('[data-node-id="quotes"]')).toHaveCount(0);
    await expect(studio.locator('[data-node-id="moves"]')).not.toContainText('next');

    runStatus = 'SUCCESS';
    await expect(studio.getByRole('region', { name: 'Run 301' })).toContainText('Success');
    await studio.getByRole('button', { name: 'Open in Run history' }).click();
    await expect(studio.getByRole('group', { name: 'Execution graph for run 301' }).locator('[data-node-id="quotes"]')).toBeVisible();
    const refreshTrace = page.waitForRequest(request => /\/api\/admin\/flows\/draft\/runs\/301\/steps$/.test(request.url()));
    await studio.getByRole('button', { name: 'Refresh history' }).click();
    await refreshTrace;
  });

  test('an invalid graph is reported per node and is never sent', async ({ page }) => {
    const sent: string[] = [];
    await page.route(DRAFT_RUN, route => { sent.push(route.request().url()); return draftRun()(route); });

    const studio = await newFlow(page);
    await studio.getByRole('button', { name: '+ Add node' }).click();
    await studio.getByLabel('Search nodes').fill('agent');
    await studio.locator('#node-picker').getByRole('button', { name: 'Agent', exact: true }).click();
    await page.keyboard.press('Escape');

    const validation = studio.locator('[aria-label="Flow graph validation"]');
    await expect(validation.getByRole('status')).toContainText('issue');
    await expect(validation.locator('li button').first()).toBeVisible();

    await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
    await expect(studio.getByText(/Resolve \d+ graph issue\(s\) before running/)).toBeVisible();
    expect(sent).toEqual([]);
  });

  test('a graph the server rejects shows its reason and saves nothing', async ({ page }) => {
    const writes: string[] = [];
    page.on('request', r => { if (['PUT', 'POST'].includes(r.method()) && !r.url().endsWith('/draft/run')) writes.push(`${r.method()} ${new URL(r.url()).pathname}`); });
    await page.route(DRAFT_RUN, draftRun({ status: 400, body: { error: 'node "Research": unknown model profile' } }));

    const studio = await newFlow(page);
    await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
    await expect(studio.getByText('unknown model profile')).toBeVisible();
    await expect(studio.getByText('Draft test queued')).toHaveCount(0);
    await expect(studio.getByText('Unsaved changes')).toBeVisible();
    expect(writes.filter(w => /flows/.test(w))).toEqual([]);
  });

  test.describe('of a saved flow with unsaved edits', () => {
    const SAVED_RUN = '**/api/admin/flows/9/run';
    const queued = { status: 'queued', flow_id: 9, run_id: 88, flow_revision: null, draft_test: true, simulated: true, graph_hash: 'b'.repeat(64) };

    // Flow 9 of the mock backend, edited in the editor; the run is followed in the run panel.
    async function editSavedFlow(page: Page, sent: any[], writes: string[], graph?: unknown) {
      page.on('request', r => { if (['PUT', 'POST'].includes(r.method()) && /\/api\/admin\/flows/.test(r.url()) && !r.url().endsWith('/run')) writes.push(`${r.method()} ${new URL(r.url()).pathname}`); });
      await page.route(SAVED_RUN, route => { sent.push(route.request().postDataJSON()); return route.fulfill({ status: 202, contentType: 'application/json', body: JSON.stringify(queued) }); });
      await page.route('**/api/admin/flows/9/runs/88/steps', route => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ steps: [] }) }));
      if (graph) {
        await page.route('**/api/admin/flows', async route => {
          if (route.request().method() !== 'GET') return route.fallback();
          return route.fulfill({ json: { flows: flows.map((f: any) => f.id === 9 ? { ...f, graph } : f) } });
        });
      }
      await open(page, 'flows');
      const studio = page.locator('fse-flow-studio');
      await studio.getByRole('tab', { name: 'My Flows' }).click();
      await studio.locator('.overview-flow-card').filter({ hasText: 'Earnings preview draft' }).getByRole('button', { name: 'Edit' }).click();
      await expect(studio.getByRole('tab', { name: 'Flow editor', exact: true })).toHaveAttribute('aria-selected', 'true');
      await studio.locator('[aria-label="Select Analyst"]').click();
      const settings = studio.locator('[aria-label="Complete node settings"]');
      await settings.getByLabel('Label', { exact: true }).fill('Unsaved analyst');
      await settings.getByRole('button', { name: 'Close node settings' }).click();
      await expect(studio.getByText('Unsaved changes')).toBeVisible();
      return studio;
    }

    test('runs the edited graph, not the stored version, and saves nothing', async ({ page }) => {
      const sent: any[] = [];
      const writes: string[] = [];
      const studio = await editSavedFlow(page, sent, writes);

      await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
      await expect(studio.getByText('Draft test queued from your unsaved editor graph')).toBeVisible();
      expect(sent).toHaveLength(1);
      expect(sent[0]).toMatchObject({ target: 'draft', simulate_side_effects: true });
      expect(sent[0].confirm_live_side_effects).toBeUndefined();
      expect(sent[0].graph.nodes.find((n: any) => n.id === 'agent').label).toBe('Unsaved analyst');
      expect(writes).toEqual([]);
      // The editor keeps the edit, still unsaved.
      await expect(studio.getByText('Unsaved changes')).toBeVisible();
    });

    test('real side effects run only after the tester allows and confirms them', async ({ page }) => {
      const graph = {
        version: 2,
        nodes: [
          { id: 'start', type: 'trigger', label: 'Start', x: 60, y: 140 },
          { id: 'agent', type: 'agent', label: 'Analyst', prompt: 'Summarise the filing.', x: 300, y: 140 },
          { id: 'mail', type: 'notify', label: 'Email me', channel: 'email', subject: '{{flow}} on {{date}}', x: 540, y: 60 },
          { id: 'report', type: 'output', label: 'Report', x: 540, y: 220 },
        ],
        edges: [{ from: 'start', to: 'agent' }, { from: 'agent', to: 'mail' }, { from: 'agent', to: 'report' }],
      };
      const sent: any[] = [];
      const writes: string[] = [];
      const studio = await editSavedFlow(page, sent, writes, graph);

      const allow = studio.getByLabel('Allow real side effects in this test');
      await expect(allow).toBeEnabled();
      await allow.check();

      // Declining the confirmation sends nothing.
      const prompts: string[] = [];
      page.once('dialog', dialog => { prompts.push(dialog.message()); void dialog.dismiss(); });
      await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
      await expect.poll(() => prompts.length).toBe(1);
      expect(prompts[0]).toContain('REAL side effects (Email me)');
      expect(sent).toEqual([]);

      page.once('dialog', dialog => void dialog.accept());
      await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
      await expect(studio.getByText('confirmed side effects run for real')).toBeVisible();
      expect(sent).toHaveLength(1);
      expect(sent[0]).toMatchObject({ target: 'draft', simulate_side_effects: false, confirm_live_side_effects: true });
      expect(writes).toEqual([]);
    });
  });

  test.describe('without permission to run flows', () => {
    test.use({ role: 'USER', grants: ['flows.read'] });

    test('a read-only user can see the editor without being offered a run', async ({ page }) => {
      const studio = await newFlow(page);
      await expect(studio.getByRole('button', { name: 'Test unsaved draft', exact: true })).toBeDisabled();
      await expect(studio.getByText('Running flows requires the Run flows permission.')).toBeVisible();
    });
  });

  test.describe('when the server revokes permission after the editor loads', () => {
    test.use({ role: 'USER', grants: ['flows.read', 'flows.run'] });

    test('a refused draft test is reported and nothing is queued or saved', async ({ page }) => {
      await page.route(DRAFT_RUN, draftRun({ status: 403, body: { error: 'forbidden: flows.run required' } }));

      const studio = await newFlow(page);
      await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
      await expect(studio.getByText('forbidden: flows.run required')).toBeVisible();
      await expect(studio.getByText('Draft test queued')).toHaveCount(0);
      await expect(studio.getByText('Unsaved changes')).toBeVisible();
    });
  });
});
