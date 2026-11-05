import { expect, open, test } from './fixtures';

// Real browser journeys against the built UI with deterministic seeded API
// fixtures. These never contact a model provider or mutate production data.
// Backend execution and real-provider handoffs are tracked separately in #289.
test.describe('Flow Studio journeys and UX regression', () => {
  test('find a flow, inspect its graph, then inspect its persisted run', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'My Flows' }).click();

    const flow = studio.locator('.overview-flow-card').filter({
      has: studio.getByRole('heading', { name: 'Filing summary', exact: true }),
    });
    await expect(flow).toBeVisible();
    await expect(flow).toContainText('nodes');
    await flow.getByRole('button', { name: 'Edit' }).click();

    await expect(studio.getByRole('tab', { name: 'Flow editor' }))
      .toHaveAttribute('aria-selected', 'true');
    await expect(studio.getByLabel('Flow name')).toHaveValue('Filing summary');
    await expect(studio.getByRole('group', { name: 'Flow canvas: nodes and directed connections' }))
      .toBeVisible();
    await expect(studio.locator('[aria-label="Select Analyst"]')).toBeVisible();

    await studio.getByRole('tab', { name: 'My Flows' }).click();
    await flow.getByRole('button', { name: 'History' }).click();
    await expect(studio.getByRole('tab', { name: 'Run history' }))
      .toHaveAttribute('aria-selected', 'true');
    await expect(studio.getByLabel('Select flow for history'))
      .toHaveValue('7');

    const completed = studio.locator('details.flow-run').filter({ has: studio.locator('summary', { hasText: '#41' }) });
    await expect(completed).toContainText('SUCCESS');
    await completed.locator('summary').click();
    await completed.getByRole('button', { name: 'Inspect nodes' }).click();
    await expect(studio.getByText('get_sec_facts').first()).toBeVisible();
  });

  test('shows a failed workflow with actionable error state instead of hiding it', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'My Flows' }).click();

    const flow = studio.locator('.overview-flow-card').filter({
      has: studio.getByRole('heading', { name: 'Daily risk screen', exact: true }),
    });
    await flow.getByRole('button', { name: 'History' }).click();
    await expect(studio.getByLabel('Select flow for history')).toHaveValue('8');

    const failed = studio.locator('details.flow-run').filter({ has: studio.locator('summary', { hasText: '#52' }) });
    await expect(failed).toContainText('FAILED');
    await failed.locator('summary').click();
    await expect(failed.locator('[role="alert"]')).toContainText('Execution failed');
    await expect(failed.locator('[role="alert"]')).not.toBeEmpty();
  });

  test('keeps graph editor and actions reachable on phone and desktop', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'My Flows' }).click();
    await studio.locator('.overview-flow-card').filter({
      has: studio.getByRole('heading', { name: 'Earnings preview draft', exact: true }),
    }).getByRole('button', { name: 'Edit' }).click();

    await expect(studio.getByLabel('Flow name')).toHaveValue('Earnings preview draft');
    await expect(studio.getByRole('button', { name: 'Save flow' })).toBeVisible();
    await expect(studio.getByRole('group', { name: 'Flow canvas: nodes and directed connections' }))
      .toBeVisible();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');

    // The screen must not get horizontally scrollable simply by opening the
    // editor. A wider canvas is fine if its own container handles overflow.
    const documentOverflow = await page.evaluate(() =>
      document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(documentOverflow).toBeLessThanOrEqual(2);
  });
});
