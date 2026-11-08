import { expect, open, test } from './fixtures';

/** Counts opaque pixels near a color on the main chart canvas. */
async function colorPixels(page: import('@playwright/test').Page, rgb: number[], slack = 8, minAlpha = 240): Promise<number> {
  return page.locator('fse-trading-chart canvas').first().evaluate((el, [target, slack, minAlpha]) => {
    const c = el as HTMLCanvasElement;
    const { data } = c.getContext('2d')!.getImageData(0, 0, c.width, c.height);
    const t = target as number[];
    let n = 0;
    for (let i = 0; i < data.length; i += 4) {
      if (data[i + 3] > (minAlpha as number) && Math.abs(data[i] - t[0]) < (slack as number) && Math.abs(data[i + 1] - t[1]) < (slack as number) && Math.abs(data[i + 2] - t[2]) < (slack as number)) n++;
    }
    return n;
  }, [rgb, slack, minAlpha]);
}

/** The chart is empty until the history has loaded and been seeded; its teal "Highest Ref" line shows it has. */
async function waitForSeries(page: import('@playwright/test').Page): Promise<void> {
  await expect.poll(() => colorPixels(page, [20, 184, 166], 40, 100)).toBeGreaterThan(30);
}

const AMBER = [245, 158, 11];
const SKY = [56, 189, 248];
const VIOLET = [167, 139, 250];

test.describe('chart drawings', () => {
  // Several browsers share the machine in CI; these steps wait on layout and a redraw.
  test.use({ actionTimeout: 10_000 });
  test.slow();
  test('a level, a trend line and a Fibonacci retracement can be drawn, selected, deleted and survive a reload', async ({ page }) => {
    await open(page, 'markets');
    const canvas = page.locator('fse-trading-chart canvas').first();
    await canvas.scrollIntoViewIfNeeded();
    await waitForSeries(page);
    const box = (await canvas.boundingBox())!;
    const at = (fx: number, fy: number) => ({ x: box.x + box.width * fx, y: box.y + box.height * fy });
    const bar = page.getByRole('toolbar', { name: 'Drawing tools' });

    await bar.locator('[data-tool="level"]').click();
    await page.mouse.click(at(0.5, 0.4).x, at(0.5, 0.4).y);
    await expect(bar.getByRole('button', { name: /Clear drawings \(1\)/ })).toBeEnabled();
    await expect(bar.locator('[data-tool="cursor"]')).toHaveAttribute('aria-pressed', 'true');
    await expect.poll(() => colorPixels(page, AMBER)).toBeGreaterThan(200);

    await bar.locator('[data-tool="trend"]').click();
    await page.mouse.click(at(0.2, 0.66).x, at(0.2, 0.66).y);
    await page.mouse.move(at(0.5, 0.55).x, at(0.5, 0.55).y);
    await page.mouse.click(at(0.8, 0.4).x, at(0.8, 0.4).y);
    await expect(bar.getByRole('button', { name: /Clear drawings \(2\)/ })).toBeEnabled();
    await expect.poll(() => colorPixels(page, SKY)).toBeGreaterThan(100);

    await bar.locator('[data-tool="fib"]').click();
    await page.mouse.click(at(0.3, 0.68).x, at(0.3, 0.68).y);
    await page.mouse.click(at(0.7, 0.46).x, at(0.7, 0.46).y);
    await expect(bar.getByRole('button', { name: /Clear drawings \(3\)/ })).toBeEnabled();
    await expect.poll(() => colorPixels(page, VIOLET)).toBeGreaterThan(100);

    await page.reload();
    await expect(page.getByRole('toolbar', { name: 'Drawing tools' }).getByRole('button', { name: /Clear drawings \(3\)/ })).toBeEnabled();
    // Select the level (once the history has loaded). A drawing is anchored to its price, and the
    // price axis can differ a little after the reload (it follows the loaded history and the
    // viewport), so the level is looked for along the middle of the chart, not at the drawn pixel.
    await expect(async () => {
      const canvas2 = page.locator('fse-trading-chart canvas').first();
      await canvas2.scrollIntoViewIfNeeded();
      const b2 = (await canvas2.boundingBox())!;
      for (let fy = 0.3; fy <= 0.6; fy += 0.01) {
        await page.mouse.click(b2.x + b2.width * 0.5, b2.y + b2.height * fy);
        if (await page.getByRole('button', { name: 'Delete selected' }).isEnabled()) return;
      }
      throw new Error('the level could not be selected');
    }).toPass({ timeout: 15000 });
    await page.keyboard.press('Delete');
    await expect(page.getByRole('toolbar', { name: 'Drawing tools' }).getByRole('button', { name: /Clear drawings \(2\)/ })).toBeEnabled();

    await page.getByRole('button', { name: /Clear drawings/ }).click();
    await page.getByRole('button', { name: 'Confirm' }).click();
    await expect(page.getByRole('button', { name: /Clear drawings \(0\)/ })).toBeDisabled();
  });

  test('Escape cancels a half-placed line and stored garbage is ignored', async ({ page }) => {
    await open(page, 'markets');
    await page.evaluate(() => {
      const key = Object.keys(localStorage).find(k => k.startsWith('fs.drawings.')) ?? 'fs.drawings.YAHOO:NVDA';
      localStorage.setItem(key, '[{"id":1},"x",{"id":"a","kind":"level","a":{"t":"no","p":1}}]');
    });
    await page.reload();
    const bar = page.getByRole('toolbar', { name: 'Drawing tools' });
    await expect(bar.getByRole('button', { name: /Clear drawings \(0\)/ })).toBeDisabled();
    await page.locator('fse-trading-chart canvas').first().scrollIntoViewIfNeeded();
    await waitForSeries(page);
    const box = (await page.locator('fse-trading-chart canvas').first().boundingBox())!;
    await bar.locator('[data-tool="trend"]').click();
    await page.mouse.click(box.x + box.width * 0.3, box.y + box.height * 0.5);
    await page.keyboard.press('Escape');
    await page.keyboard.press('Escape');
    await expect(bar.locator('[data-tool="cursor"]')).toHaveAttribute('aria-pressed', 'true');
    await expect(bar.getByRole('button', { name: /Clear drawings \(0\)/ })).toBeDisabled();
  });
});
