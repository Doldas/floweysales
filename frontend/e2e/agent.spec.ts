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

    await expect(dock.locator('fs-tool-call').first()).toBeVisible();
    await dock.locator('fs-tool-call summary').first().click();
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

test.describe('tool call inspection', () => {
  test('a chat tool call shows its input, output and a way to copy and find', async ({ page }) => {
    await open(page);
    await page.getByRole('button', { name: 'Toggle research agent' }).click();
    const dock = page.locator('#fs-agent-dock');
    await dock.getByRole('textbox', { name: 'Message the research agent' }).fill('How is NVDA doing?');
    await page.keyboard.press('Enter');
    const call = dock.locator('fs-tool-call').first();
    await call.locator('summary').click();
    await expect(call.getByRole('heading', { name: 'Input' })).toBeVisible();
    await expect(call.getByRole('heading', { name: 'Output' })).toBeVisible();
    await expect(call).toContainText('"symbol": "NVDA"');
    await call.getByRole('searchbox').fill('price');
    await expect(call).toContainText('1 matching line');
    await expect(call.getByRole('button', { name: 'Copy' })).toBeVisible();
    await expect(call.getByRole('button', { name: 'Download' })).toBeVisible();
  });

  test('agent teams show each member\'s calls with input, output, timing, failures and cuts', async ({ page }) => {
    await open(page, 'team');
    await page.getByRole('button', { name: 'Ask the team' }).click();
    await expect(page.getByText('2 tool calls')).toBeVisible();
    await expect(page.getByText('1 failed')).toBeVisible();

    const owner = page.getByRole('article', { name: 'Ashgrove' });
    await owner.locator('details.steps > summary').click();
    const calls = owner.locator('fs-tool-call');
    await expect(calls).toHaveCount(2);
    // Results are matched by call id: the second call finished first, and each still has its own answer.
    await calls.nth(0).locator('summary').click();
    await expect(calls.nth(0)).toContainText('NVDA moat');
    await expect(calls.nth(0)).toContainText('moat result');
    await expect(calls.nth(0)).toContainText('117.2 KB');
    await expect(calls.nth(0)).toContainText('480 ms');
    await expect(calls.nth(0)).toContainText('call #1');
    await calls.nth(1).locator('summary').click();
    await expect(calls.nth(1)).toContainText('margins result');
    await expect(calls.nth(1)).not.toContainText('moat result');

    const teacher = page.getByRole('article', { name: 'Holm' });
    await teacher.locator('details.steps > summary').click();
    const failed = teacher.locator('fs-tool-call').first();
    await failed.locator('summary').click();
    await expect(failed.getByRole('heading', { name: 'Error' })).toBeVisible();
    await expect(failed).toContainText('filing section not found');
    await expect(failed).toContainText('Holm');
  });
});
