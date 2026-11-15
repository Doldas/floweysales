import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

// A run started from the flow editor is followed in the editor: a run panel
// with status, progress and node steps, and the canvas nodes toned by their
// recorded status, without switching to Run history.

const T0 = '2026-11-15T12:00:00Z';

/** Each poll of the run list advances the run one stage: queued, start done, agent done, finished. */
async function backend(page: Page, finalStatus: 'SUCCESS' | 'FAILED' = 'SUCCESS') {
  let polls = 0;
  const cancels: string[] = [];
  const stage = () => Math.min(polls, 3);
  const json = (body: unknown, status = 200) => ({ status, contentType: 'application/json', body: JSON.stringify(body) });
  await page.route('**/api/admin/flows/9/run', route => route.fulfill(json({ status: 'queued', flow_id: 9, run_id: 77, flow_revision: null, draft_test: false }, 202)));
  await page.route('**/api/admin/flows/9/runs', route => {
    polls++;
    const s = stage();
    const status = s === 0 ? 'QUEUED' : s < 3 ? 'RUNNING' : finalStatus;
    const done = s === 3;
    return route.fulfill(json({ runs: [{
      id: 77, flow_id: 9, trigger: 'manual', status,
      output: done && finalStatus === 'SUCCESS' ? '## Earnings\n\nRevenue grew 12%.' : '',
      error: done && finalStatus === 'FAILED' ? 'agent node "Analyst": provider timeout after 45s' : '',
      started_at: s ? T0 : undefined, finished_at: done ? '2026-11-15T12:00:09Z' : null, duration_ms: done ? 9000 : null,
    }] }));
  });
  await page.route('**/api/admin/flows/9/runs/77/steps', route => {
    const s = stage();
    const steps = [];
    if (s >= 1) steps.push({ id: 1, node: 'start', label: 'Start', status: 'SUCCESS', output: '', duration_ms: 2 });
    if (s >= 2) steps.push(finalStatus === 'FAILED'
      ? { id: 2, node: 'agent', label: 'Analyst', status: 'FAILED', output: '', error: 'provider timeout after 45s', duration_ms: 45000 }
      : { id: 2, node: 'agent', label: 'Analyst', status: 'SUCCESS', output: 'Revenue grew 12%.', duration_ms: 8000 });
    if (s >= 3 && finalStatus === 'SUCCESS') steps.push({ id: 3, node: 'report', label: 'Report', status: 'SUCCESS', output: '## Earnings', duration_ms: 5 });
    return route.fulfill(json({ steps }));
  });
  await page.route('**/api/admin/flows/9/runs/77/cancel', route => { cancels.push('77'); return route.fulfill(json({ status: 'cancelled' })); });
  return { cancels, polls: () => polls };
}

async function editFlow(page: Page) {
  await open(page, 'flows');
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: 'My Flows' }).click();
  await studio.locator('.overview-flow-card').filter({ hasText: 'Earnings preview draft' }).getByRole('button', { name: 'Edit' }).click();
  await expect(studio.getByRole('tab', { name: 'Flow editor', exact: true })).toHaveAttribute('aria-selected', 'true');
  return studio;
}

test.describe('Following a run in the flow editor', () => {
  test('Run now shows progress, node status and the output in the editor', async ({ page }) => {
    await backend(page);
    const studio = await editFlow(page);
    await studio.getByRole('button', { name: 'Run now', exact: true }).click();

    const panel = studio.getByRole('region', { name: 'Run 77' });
    await expect(panel).toBeVisible();
    // It finishes on its own, without leaving the editor.
    await expect(panel.getByRole('status')).toHaveText('Run #77 success · 3 of 3 nodes finished', { timeout: 15_000 });
    await expect(studio.getByRole('tab', { name: 'Flow editor', exact: true })).toHaveAttribute('aria-selected', 'true');
    await expect(panel.getByRole('progressbar', { name: 'Nodes finished' })).toHaveAttribute('aria-valuenow', '3');
    await expect(panel.getByRole('list', { name: 'Nodes run so far' }).getByRole('listitem')).toHaveCount(3);
    await expect(panel.getByText('Revenue grew 12%.').last()).toBeVisible();
    await expect(panel).toContainText('Finished Nov 15, 2026');
    // The canvas states each node's status in text and tone.
    const analyst = studio.locator('[aria-label="Select Analyst"]');
    await expect(analyst.locator('.live-badge')).toHaveText('✓ 8.0 s');
    await expect(analyst.locator('rect.flow-node')).toHaveClass(/tone-ok/);

    // Run history opens on the same run with its trace.
    await panel.getByRole('button', { name: 'Open in Run history' }).click();
    await expect(studio.getByRole('tab', { name: 'Run history' })).toHaveAttribute('aria-selected', 'true');
    await expect(studio.getByRole('button', { name: 'Hide node trace' })).toBeVisible();
  });

  test('a failed node is shown with its error, and the panel closes', async ({ page }) => {
    await backend(page, 'FAILED');
    const studio = await editFlow(page);
    await studio.getByRole('button', { name: 'Run now', exact: true }).click();
    const panel = studio.getByRole('region', { name: 'Run 77' });
    await expect(panel.getByRole('status')).toHaveText('Run #77 failed · 2 of 3 nodes finished · 1 failed', { timeout: 15_000 });
    await expect(panel.getByRole('alert')).toContainText('provider timeout after 45s');
    await expect(panel.locator('details[open]')).toContainText('provider timeout after 45s');
    await expect(studio.locator('[aria-label="Select Analyst"] .live-badge')).toHaveText('✕ failed');
    await expect(studio.locator('[aria-label="Select Report"] .live-badge')).toHaveText('· not run');
    await panel.getByRole('button', { name: 'Close run panel' }).click();
    await expect(panel).toHaveCount(0);
    await expect(studio.locator('.live-badge')).toHaveCount(0);
  });

  test('a run can be cancelled from the panel', async ({ page }) => {
    const { cancels } = await backend(page);
    const studio = await editFlow(page);
    await studio.getByRole('button', { name: 'Run now', exact: true }).click();
    const panel = studio.getByRole('region', { name: 'Run 77' });
    await panel.getByRole('button', { name: /Remove from queue|Cancel run/ }).click();
    await expect.poll(() => cancels).toEqual(['77']);
  });
});
