import { expect, open, test } from './fixtures';

const STYLES = ['candles', 'hollow', 'ohlc', 'heikin', 'line', 'area', 'step', 'baseline'];

/** Counts pixels of the green and the red the bar styles use on the main chart canvas. */
async function barPixels(page: import('@playwright/test').Page): Promise<{ up: number; down: number }> {
  return page.locator('fse-trading-chart canvas').first().evaluate((el) => {
    const c = el as HTMLCanvasElement;
    const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
    let up = 0, down = 0;
    const near = (r: number, g: number, b: number, t: number[]) => Math.abs(r - t[0]) < 6 && Math.abs(g - t[1]) < 6 && Math.abs(b - t[2]) < 6;
    for (let i = 0; i < data.length; i += 4) {
      if (data[i + 3] < 250) continue;
      if (near(data[i], data[i + 1], data[i + 2], [22, 163, 74])) up++;
      else if (near(data[i], data[i + 1], data[i + 2], [220, 38, 38])) down++;
    }
    return { up, down };
  });
}

test.describe('chart styles', () => {
  test('the user can switch the market chart between all eight styles', async ({ page }) => {
    await open(page, 'markets');
    const group = page.getByRole('radiogroup', { name: 'Chart style' });
    await expect(group.getByRole('radio')).toHaveCount(STYLES.length);
    await expect(group.getByRole('radio', { name: 'Area' })).toHaveAttribute('aria-checked', 'true');

    for (const id of STYLES) {
      await group.locator(`[data-style="${id}"]`).click();
      await expect(group.locator(`[data-style="${id}"]`)).toHaveAttribute('aria-checked', 'true');
      if (['candles', 'hollow', 'ohlc', 'heikin', 'baseline'].includes(id)) {
        await expect.poll(async () => (await barPixels(page)).up, { message: `${id}: green bars or fill` }).toBeGreaterThan(20);
        await expect.poll(async () => (await barPixels(page)).down, { message: `${id}: red bars or fill` }).toBeGreaterThan(20);
      } else {
        await page.waitForTimeout(150);
        const px = await barPixels(page);
        expect(px.up + px.down, `${id}: draws no bar colors`).toBeLessThan(20);
      }
      await page.locator('fse-trading-chart .chart-container').screenshot({ path: `test-results/chart-style-${id}.png` });
    }
  });

  test('the chosen style is remembered and an unknown stored one is ignored', async ({ page }) => {
    await open(page, 'markets');
    const group = page.getByRole('radiogroup', { name: 'Chart style' });
    await group.locator('[data-style="candles"]').click();
    await page.reload();
    await expect(page.getByRole('radiogroup', { name: 'Chart style' }).locator('[data-style="candles"]')).toHaveAttribute('aria-checked', 'true');
    await page.evaluate(() => localStorage.setItem('fs.chartStyle', 'pie'));
    await page.reload();
    await expect(page.getByRole('radiogroup', { name: 'Chart style' }).locator('[data-style="area"]')).toHaveAttribute('aria-checked', 'true');
  });
});
