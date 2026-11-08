import { expect, open, test } from './fixtures';

// An answer that wraps its TeX in backticks (as models often do) must show a typeset formula, not a code span.
test('a DCF formula the model wrapped in backticks is typeset in the chat answer', async ({ page }) => {
  const answer = '**DCF calculation**\n\n`\\text{PV} = \\frac{\\text{Net CF}}{r} = \\frac{32.4\\ \\text{B}}{0.15} = \\mathbf{213\\ \\text{B USD}}`\n\nUse `get_sec_facts` for inputs.';
  await page.route('**/api/ai/chat', route => route.fulfill({
    status: 200, contentType: 'text/event-stream',
    body: [{ type: 'run_start', model: 'small-model' }, { type: 'delta', content: answer }, { type: 'done' }].map(e => `data: ${JSON.stringify(e)}\n\n`).join(''),
  }));
  await open(page, 'agent');
  await page.getByRole('textbox', { name: 'Message the research agent' }).fill('Value it');
  await page.keyboard.press('Enter');

  const formula = page.locator('.md-math').first();
  await expect(formula).toBeVisible();
  await expect(formula.locator('.md-frac')).toHaveCount(2);
  await expect(formula).not.toContainText('\\frac');
  await expect(formula.locator('strong')).toContainText('213');
  // Real code in the same answer stays code.
  await expect(page.locator('code', { hasText: 'get_sec_facts' })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(1);
});
