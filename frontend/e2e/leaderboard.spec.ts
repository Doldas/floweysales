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
