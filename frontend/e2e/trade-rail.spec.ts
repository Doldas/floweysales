import { expect, open, test } from './fixtures';

test.describe('trade rail beside the chart', () => {
  test('the watchlist shows each tracked instrument with its price and how fresh it is, and a row opens it', async ({ page }) => {
    await open(page, 'markets');
    const list = page.getByRole('list', { name: 'Watchlist prices' });
    const row = (symbol: string) => list.locator(`[data-symbol="${symbol}"]`);
    await expect(list.locator('button')).toHaveCount(3);
    await expect(row('NVDA')).toContainText('118.16');
    await expect(row('BTCUSDT')).toContainText('67,234.50');
    await expect(row('BTCUSDT')).toContainText('live');
    await expect(row('VOLV-B')).toContainText('285.40');
    await expect(row('VOLV-B')).toContainText('stale · 2 h');
    await expect(row('NVDA')).toHaveAttribute('aria-pressed', 'true');

    await row('VOLV-B').click();
    await expect(page.getByRole('heading', { level: 1 })).toContainText('VOLV-B');
    await expect(row('VOLV-B')).toHaveAttribute('aria-pressed', 'true');
    await expect(row('NVDA')).toHaveAttribute('aria-pressed', 'false');
  });

  test('a paper order can be composed and reviewed from the chart page without leaving it', async ({ page }) => {
    await open(page, 'markets');
    const ticket = page.getByRole('region', { name: 'Paper order ticket' });
    await expect(ticket.getByText('Order ticket')).toBeVisible();
    await expect(ticket.getByLabel('Account summary')).toContainText('Buying power');
    await expect(ticket.getByRole('link', { name: /Full paper desk/ })).toHaveAttribute('href', '#/portfolio');
    await ticket.getByLabel('Quantity').fill('5');
    await ticket.getByRole('button', { name: /Review buy order/ }).click();
    await expect(ticket.getByText('Review order')).toBeVisible();
    await expect(page).toHaveURL(/#\/markets$/);
  });

  test('the full desk is still the place for positions and the blotter', async ({ page }) => {
    await open(page, 'markets');
    await expect(page.getByRole('region', { name: 'Paper order ticket' }).getByRole('tab', { name: /Positions/ })).toHaveCount(0);
    await open(page, 'portfolio');
    await expect(page.getByRole('tab', { name: /Positions/ })).toBeVisible();
    await expect(page.getByLabel('Account summary')).toBeVisible();
  });
});

test.describe('without live-market access', () => {
  test.use({ role: 'USER' });
  test('a signed-in user without it sees the watchlist but no order ticket', async ({ page }) => {
    // Without live markets the page opens on the instrument last viewed.
    await page.addInitScript(() => { localStorage.setItem('lastTrackedSymbol', 'NVDA'); localStorage.setItem('lastProvider', 'YAHOO'); });
    await open(page, 'markets');
    await expect(page.getByRole('region', { name: 'Watchlist' })).toBeVisible();
    await expect(page.getByRole('region', { name: 'Paper order ticket' })).toHaveCount(0);
  });
});
