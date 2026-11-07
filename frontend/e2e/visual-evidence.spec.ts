import { expect, open, test } from './fixtures';

// Deliberately uses deterministic mock fixtures, never a real user account.
// Screenshots are attached even on success so a human/AI reviewer can examine
// desktop and phone layouts directly from the GitHub Actions HTML report.
test.describe('UI evidence: Flow Studio and Research Agent', () => {
  test('captures My Flows overview and graph editor', async ({ page }, testInfo) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'My Flows' }).click();
    const card = studio.locator('.overview-flow-card').filter({ hasText: 'Filing summary' });
    await expect(card).toBeVisible();
    await testInfo.attach('my-flows-overview.png', {
      body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
      contentType: 'image/png',
    });

    await card.getByRole('button', { name: 'Edit', exact: true }).click();
    await expect(studio.getByLabel('Flow name')).toHaveValue('Filing summary');
    await expect(studio.getByRole('group', { name: 'Flow canvas: nodes and directed connections' })).toBeVisible();
    await testInfo.attach('flow-studio-editor.png', {
      body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
      contentType: 'image/png',
    });
  });

  test('captures Research Agent layout', async ({ page }, testInfo) => {
    await open(page, 'agent');
    await expect(page.getByRole('heading', { name: 'Research Agent' })).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Message the research agent' })).toBeVisible();
    await testInfo.attach('research-agent.png', {
      body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
      contentType: 'image/png',
    });
  });
});

test('Monte Carlo result keeps replay outcomes separate from simulated uncertainty', async ({ page }, testInfo) => {
  await open(page, 'strategies');
  const run = page.getByRole('button', { name: 'Run test' });
  await expect(run).toBeEnabled();
  await run.click();
  const panel = page.locator('fs-strategy-montecarlo');
  await expect(panel.getByRole('heading', { name: 'How robust is this result?' })).toBeVisible();
  await panel.getByRole('button', { name: 'Run simulation' }).click();

  await expect(panel.getByText(/95% Wilson interval/)).toBeVisible();
  await expect(panel.getByText(/not a probability forecast/)).toBeVisible();
  const table = panel.getByRole('table');
  await expect(table).toBeVisible();
  await expect(table).toHaveAttribute('aria-describedby', 'smc-evidence-notice');
  await expect(table.getByRole('columnheader', { name: 'Happened' })).toBeVisible();
  await expect(table.getByRole('columnheader', { name: 'Median' })).toBeVisible();
  await expect(panel.getByRole('heading', { name: 'What this assumes' })).toBeVisible();
  await expect(panel.getByRole('heading', { name: 'What it cannot tell you' })).toBeVisible();

  await testInfo.attach('monte-carlo-observed-vs-simulated.png', {
    body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
    contentType: 'image/png',
  });
});
