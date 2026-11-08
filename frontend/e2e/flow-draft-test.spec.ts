import { expect, open, test } from './fixtures';
import type { Page, Route } from '@playwright/test';

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
    status: overrides.status ?? 200,
    contentType: 'application/json',
    body: JSON.stringify(overrides.body ?? { status: 'queued', run_id: 301, draft_test: true, scratch: true, simulated: true, graph_hash: 'a'.repeat(64) }),
  });
}

test.describe('Draft tests of unsaved flows', () => {
  test('editing the graph while a draft test is in flight does not change what runs', async ({ page }) => {
    const sent: any[] = [];
    let release!: () => void;
    const gate = new Promise<void>(resolve => { release = resolve; });
    await page.route(DRAFT_RUN, async route => {
      sent.push(route.request().postDataJSON());
      await gate;
      await draftRun()(route);
    });

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

  test.describe('without permission to run flows', () => {
    test.use({ role: 'USER', grants: ['flows.read'] });

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
