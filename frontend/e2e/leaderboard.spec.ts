import { expect, open, test } from './fixtures';

test.describe('leaderboard on Markets', () => {
  test('ranks companies and shows the research disclaimer', async ({ page }) => {
    await open(page, 'markets');
    const board = page.getByRole('region', { name: /Leaderboard/ });
    await expect(board).toBeVisible();
    const rows = board.locator('.board-row');
    await expect(rows.first()).toBeVisible();
    await expect(rows.first()).toContainText('GOOGL');
    await expect(board.getByTestId('leaderboard-disclaimer')).toContainText('not personalized financial advice');
  });

  test('switching the strategy reloads the ranking', async ({ page }) => {
    await open(page, 'markets');
    const board = page.getByRole('region', { name: /Leaderboard/ });
    await expect(board.locator('.board-row').first()).toBeVisible();
    const request = page.waitForRequest(r => r.url().includes('/api/market/leaderboard') && r.url().includes('strategy=value'));
    await board.getByRole('button', { name: 'Cheap & solid' }).click();
    await request;
    await expect(board.getByRole('button', { name: 'Cheap & solid' })).toHaveAttribute('aria-pressed', 'true');
  });
});

test.describe('leaderboard methodology', () => {
  test('explains the ranking above the results and can be collapsed', async ({ page }) => {
    await open(page, 'markets');
    const method = page.getByTestId('leaderboard-method');
    await expect(method).toBeVisible();
    await expect(method.getByRole('heading', { name: 'How this ranking works' })).toBeVisible();
    await expect(method).toContainText('percentile within');
    await expect(method).toContainText('22%'); // the weight of Value
    await expect(method).toContainText('Prices as of 2026-10-28');
    // The explanation sits between the controls and the table.
    const board = page.getByRole('region', { name: /Leaderboard/ });
    const order = await board.evaluate(el => {
      const at = (sel: string) => [...el.querySelectorAll('*')].indexOf(el.querySelector(sel)!);
      return [at('.board-controls'), at('[data-testid="leaderboard-method"]'), at('.board-table')];
    });
    expect(order[0]).toBeLessThan(order[1]);
    expect(order[1]).toBeLessThan(order[2]);

    const toggle = method.getByRole('button', { name: 'Hide' });
    await toggle.click();
    await expect(method.getByText('What it weighs')).toBeHidden();
    await expect(method.getByRole('button', { name: 'Show' })).toHaveAttribute('aria-expanded', 'false');
  });

  test('a company\'s score can be opened up by keyboard, without opening the company', async ({ page }) => {
    await open(page, 'markets');
    const board = page.getByRole('region', { name: /Leaderboard/ });
    const first = board.locator('.board-row').first();
    const button = first.getByRole('button', { name: 'Score breakdown' });
    await button.focus();
    await page.keyboard.press('Enter');
    await expect(button).toHaveAttribute('aria-expanded', 'true');
    const detail = board.locator('.explain-row').first();
    await expect(detail).toContainText('Value');
    await expect(detail.getByRole('table')).toBeVisible();
    // Enter on the button must not also open the company (that would make its row active).
    await expect(first).not.toHaveClass(/active/);
    await expect(button).toBeFocused();
  });
});
