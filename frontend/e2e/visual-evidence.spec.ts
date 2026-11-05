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
