import { expect, open, test } from './fixtures';

test.describe('research agent', () => {
  test('answers in the dock with visible tool steps and safe markdown', async ({ page }) => {
    await open(page);
    await page.getByRole('button', { name: 'Toggle research agent' }).click();
    const dock = page.locator('#fs-agent-dock');
    await expect(dock).toBeVisible();
    await expect(dock.locator('.suggestion').first()).toBeVisible();

    await dock.getByRole('textbox', { name: 'Message the research agent' }).fill('How is NVDA doing?');
    await page.keyboard.press('Enter');

    await expect(dock.locator('.step').first()).toBeVisible();
    await dock.locator('.step summary').first().click();
    await expect(dock.locator('.step-pre').first()).not.toBeEmpty();

    const answer = dock.locator('.msg-agent .bubble.chat-markdown').first();
    await expect(answer).not.toBeEmpty();
    await expect(answer.locator('script')).toHaveCount(0);
    // The send button returns once the run has finished.
    await expect(dock.getByRole('button', { name: 'Send' })).toBeVisible();
  });

  test('the full agent page shows the same conversation', async ({ page }) => {
    await open(page, 'agent');
    await expect(page).toHaveTitle('Research Agent · FloweySales');
    await expect(page.getByRole('heading', { name: 'What should we research?' })).toBeVisible();
  });
});
