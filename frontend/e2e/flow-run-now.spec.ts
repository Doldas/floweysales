import { expect, open, test } from './fixtures';

// Mock-backed counterpart of the opt-in live workflow suite
// (live-e2e/workflows.spec.ts): the same UI path (My Flows -> Run now ->
// "Run queued" -> history) against the deterministic mock backend, so the
// selectors the live suite depends on are guarded in normal CI without
// credentials, a deployed app or model calls.
test.describe('Run now journey (mock counterpart of the live suite)', () => {
  test('Run now posts to the run endpoint, confirms and opens history', async ({ page }) => {
    const posted: string[] = [];
    page.on('request', r => { if (r.method() === 'POST' && /\/api\/admin\/flows\/\d+\/run$/.test(r.url())) posted.push(new URL(r.url()).pathname); });

    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'My Flows' }).click();
    const card = studio.locator('.overview-flow-card').filter({ has: page.getByRole('heading', { name: 'Earnings preview draft', exact: true }) });
    await expect(card).toBeVisible();
    await card.getByRole('button', { name: 'Run now' }).click();

    await expect(page.getByText('Run queued', { exact: false })).toBeVisible();
    await expect(studio.getByRole('tab', { name: 'Run history' })).toHaveAttribute('aria-selected', 'true');
    expect(posted).toEqual(['/api/admin/flows/9/run']);
  });

  test('inspecting nodes of a finished run exposes the step trace the live suite verifies', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'My Flows' }).click();
    await studio.locator('.overview-flow-card').filter({ hasText: 'Filing summary' }).getByRole('button', { name: 'History' }).click();
    const run = studio.locator('details.flow-run').filter({ has: page.locator('summary', { hasText: '#41' }) });
    await run.locator('summary').click();
    await run.getByRole('button', { name: 'Inspect nodes' }).click();
    await expect(studio.getByText('Analyst').first()).toBeVisible();
  });
});
