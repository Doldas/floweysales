import { expect, open, test } from './fixtures';

/** A fingerprint of what the price canvas shows right now. */
const pixels = (page: import('@playwright/test').Page) =>
  page.locator('canvas').first().evaluate((c) => (c as HTMLCanvasElement).toDataURL());

test.describe('chart repaint and feedback', () => {
  test('the chosen style and bar length are marked and redraw the chart at once', async ({ page }) => {
    await open(page, 'markets');
    await expect(page.getByTestId('chart-meta')).toContainText('daily bars');

    const bars = page.locator('[data-style="ohlc"]');
    const week = page.locator('[data-interval="1wk"]');
    const accent = async (l: import('@playwright/test').Locator) => l.evaluate((e) => getComputedStyle(e).borderColor);
    const idle = await accent(bars);

    let before = await pixels(page);
    await bars.click();
    await expect(bars).toHaveAttribute('aria-checked', 'true');
    await expect.poll(() => accent(bars), { message: 'the chosen style looks chosen' }).not.toBe(idle);
    await expect.poll(() => pixels(page), { message: 'style change repaints' }).not.toBe(before);

    before = await pixels(page);
    await week.click();
    await expect.poll(() => accent(week), { message: 'the chosen length looks chosen' }).not.toBe(idle);
    await expect.poll(() => pixels(page), { message: 'bar length change repaints' }).not.toBe(before);
  });

  test('a new bar length shows the whole series even after zooming', async ({ page }) => {
    await open(page, 'markets');
    await page.getByRole('button', { name: '1Y', exact: true }).click();
    await expect(page.getByTestId('chart-meta')).toContainText('1Y');
    const canvas = page.locator('canvas').first();
    await canvas.scrollIntoViewIfNeeded();
    const settle = async () => { await page.mouse.move(0, 0); await page.waitForTimeout(400); return pixels(page); };

    await page.locator('[data-interval="1mo"]').click();
    const fresh = await settle();

    await page.locator('[data-interval="auto"]').click();
    await page.waitForTimeout(400);
    const box = (await canvas.boundingBox())!;
    await page.mouse.move(box.x + box.width * 0.8, box.y + box.height / 2);
    for (let i = 0; i < 6; i++) await page.mouse.wheel(0, -300);
    await page.waitForTimeout(400);

    await page.locator('[data-interval="1mo"]').click();
    expect(await settle(), 'the zoom of the old bars is not carried over to the new ones').toBe(fresh);
  });
});
