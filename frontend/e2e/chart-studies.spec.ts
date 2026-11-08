import { expect, open, test } from './fixtures';

async function pixels(page: import('@playwright/test').Page, rgb: number[]): Promise<number> {
  return page.locator('fse-trading-chart canvas').first().evaluate((el, t) => {
    const c = el as HTMLCanvasElement;
    const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
    let n = 0;
    for (let i = 0; i < data.length; i += 4) if (data[i + 3] > 200 && Math.abs(data[i] - t[0]) < 20 && Math.abs(data[i + 1] - t[1]) < 20 && Math.abs(data[i + 2] - t[2]) < 20) n++;
    return n;
  }, rgb);
}

test.describe('chart studies', () => {
  test('Stochastic and ATR get their own panels and Ichimoku draws on the price chart', async ({ page }) => {
    await open(page, 'markets');
    const studies = page.getByRole('group', { name: 'Chart analysis studies' });
    const canvases = page.locator('fse-trading-chart canvas');
    const before = await canvases.count();

    await studies.getByRole('button', { name: /^Stochastic 14,3,3$/ }).click();
    await expect(canvases).toHaveCount(before + 1);
    await studies.getByRole('button', { name: /^ATR 14$/ }).click();
    await expect(canvases).toHaveCount(before + 2);

    // Tenkan is blue (#2563eb) and is not drawn until Ichimoku is on.
    const blue = await pixels(page, [37, 99, 235]);
    await studies.getByRole('button', { name: /^Ichimoku 9,26,52$/ }).click();
    await expect.poll(() => pixels(page, [37, 99, 235])).toBeGreaterThan(blue + 20);

    await studies.getByRole('button', { name: /^Stochastic/ }).click();
    await studies.getByRole('button', { name: /^ATR/ }).click();
    await expect(canvases).toHaveCount(before);
    await expect(studies.getByRole('button', { name: /^Stochastic/ })).toHaveAttribute('aria-pressed', 'false');
  });

  test('periods can be changed, are checked, remembered and reset', async ({ page }) => {
    await open(page, 'markets');
    const studies = page.getByRole('group', { name: 'Chart analysis studies' });
    await studies.getByRole('button', { name: 'Periods' }).click();
    const panel = page.getByRole('group', { name: 'Study periods' });

    await panel.getByLabel('Period', { exact: true }).nth(1).fill('21'); // RSI
    await panel.getByLabel('Period', { exact: true }).nth(1).dispatchEvent('change');
    await expect(studies.getByRole('button', { name: 'RSI 21' })).toBeVisible();

    // A fast MACD period at or above the slow one is refused with a reason and not applied.
    await panel.getByLabel('Fast').fill('30');
    await panel.getByLabel('Fast').dispatchEvent('change');
    await expect(panel.getByRole('alert')).toContainText('MACD fast must be shorter than slow');
    await expect(studies.getByRole('button', { name: 'MACD 12,26,9' })).toBeVisible();

    await panel.getByLabel('EMA 1').fill('12');
    await panel.getByLabel('EMA 1').dispatchEvent('change');
    await expect(studies.getByRole('button', { name: 'EMA 12' })).toBeVisible();

    await page.reload();
    const again = page.getByRole('group', { name: 'Chart analysis studies' });
    await expect(again.getByRole('button', { name: 'RSI 21' })).toBeVisible();
    await expect(again.getByRole('button', { name: 'EMA 12' })).toBeVisible();

    await again.getByRole('button', { name: 'Periods' }).click();
    await page.getByRole('button', { name: 'Reset periods' }).click();
    await expect(again.getByRole('button', { name: 'RSI 14' })).toBeVisible();
    await expect(again.getByRole('button', { name: 'EMA 9' })).toBeVisible();
  });

  test('stored periods that cannot be used are replaced by the defaults', async ({ page }) => {
    await page.addInitScript(() => localStorage.setItem('fse.indicatorSettings', '{"rsiPeriod":0,"macdFast":99}'));
    await open(page, 'markets');
    const studies = page.getByRole('group', { name: 'Chart analysis studies' });
    await expect(studies.getByRole('button', { name: 'RSI 14' })).toBeVisible();
    await expect(studies.getByRole('button', { name: 'MACD 12,26,9' })).toBeVisible();
  });
});
