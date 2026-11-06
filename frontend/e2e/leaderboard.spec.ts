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

test.describe('where the companies come from', () => {
  test('the list source, limits and what was left out are stated above the results', async ({ page }) => {
    await open(page, 'markets');
    const origin = page.getByTestId('leaderboard-origin');
    await expect(origin.getByRole('heading', { name: 'Where do these companies come from?' })).toBeVisible();
    await expect(origin).toContainText('Hand-picked by the maintainers');
    await expect(origin).toContainText('no link to an original record is recorded');
    await expect(origin).toContainText('Not an official index');
    await expect(origin).toContainText('BRK-B (fewer than four reported quarters)');
    await expect(origin).toContainText('−1 no usable data');
    // Above the methodology, which is above the table.
    const board = page.getByRole('region', { name: /Leaderboard/ });
    const order = await board.evaluate(el => {
      const at = (sel: string) => [...el.querySelectorAll('*')].indexOf(el.querySelector(sel)!);
      return [at('[data-testid="leaderboard-origin"]'), at('[data-testid="leaderboard-method"]'), at('.board-table')];
    });
    expect(order[0]).toBeLessThan(order[1]);
    expect(order[1]).toBeLessThan(order[2]);
  });

  test('each company answers why it is here, where its numbers come from and why it ranks', async ({ page }) => {
    await open(page, 'markets');
    const board = page.getByRole('region', { name: /Leaderboard/ });
    await board.locator('.board-row').first().getByRole('button', { name: 'Score breakdown' }).click();
    const detail = board.locator('.explain-row').first();
    await expect(detail.getByRole('heading', { name: 'Why it is here' })).toBeVisible();
    await expect(detail.getByRole('heading', { name: 'Where its numbers come from' })).toBeVisible();
    await expect(detail.getByRole('heading', { name: 'Why it ranks here' })).toBeVisible();
    await expect(detail).toContainText('Provider: YAHOO');
    await expect(detail).toContainText('fundamentals through 2026-09-30');
  });

  test('the list can be downloaded with its sources', async ({ page }) => {
    await open(page, 'markets');
    const download = page.waitForEvent('download');
    await page.getByTestId('leaderboard-origin').getByRole('button', { name: 'Download list as CSV' }).click();
    const file = await download;
    expect(file.suggestedFilename()).toMatch(/^leaderboard-.*\.csv$/);
  });
});

test.describe('index universes', () => {
  test('unavailable indices are inspectable, never ranked', async ({ page }) => {
    await open(page, 'markets');
    const board = page.getByRole('region', { name: /Leaderboard/ });
    await board.getByRole('button', { name: /OMX Stockholm 30/ }).click();
    const panel = board.getByRole('region', { name: 'About this index' });
    await expect(panel).toContainText('Unavailable');
    await expect(panel).toContainText('No list loaded');
    await expect(panel).toContainText('Membership is not a ranking or a recommendation');
    await expect(board.locator('.board-row').first()).toBeVisible();
  });
});
