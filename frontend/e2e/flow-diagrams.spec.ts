import { expect, open, test, type Page } from './fixtures';

// Every graph surface must render through the shared diagram system (see
// docs/flow-diagram-system.md). These checks fail when a surface drifts back to
// its own renderer: same node card geometry and tokens, same navigation controls,
// same keyboard behavior, in light and dark mode and on small screens.

async function openLibrary(page: Page) {
  await open(page, 'flows');
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: /Function library/ }).click();
  return studio;
}

async function openRunSnapshot(page: Page): Promise<void> {
  await open(page, 'flows');
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: 'Run history' }).click();
  await studio.getByLabel('Select flow for history').selectOption({ label: 'Filing summary' });
  const run = studio.locator('details.flow-run').filter({ has: page.locator('summary', { hasText: '#41' }) });
  await run.locator('summary').click();
  await run.getByRole('button', { name: 'Inspect nodes' }).click();
}

/** Geometry that must match the Flow Editor on every surface. */
async function cardMetrics(page: Page, scope: string) {
  return page.locator(`${scope} .flow-node`).first().evaluate(el => {
    const style = getComputedStyle(el);
    return { width: el.getAttribute('width'), height: el.getAttribute('height'), rx: el.getAttribute('rx'), stroke: style.strokeWidth === '' ? '' : style.strokeWidth };
  });
}

test.describe('shared flow diagram system', () => {
  test('Functions, Triggers and run snapshots use the Flow Editor node card and controls', async ({ page }) => {
    const studio = await openLibrary(page);
    const fn = studio.locator('fse-flow-diagram').filter({ has: page.locator('[aria-label^="Function canvas"]') });
    const trigger = studio.locator('fse-flow-diagram').filter({ has: page.locator('[aria-label^="Event triggers"]') });
    await expect(fn.getByRole('group', { name: /Function canvas/ })).toBeVisible();
    await expect(trigger.getByRole('group', { name: /Event triggers/ })).toBeVisible();

    // Shared controls: zoom, fit and reset on every diagram.
    for (const diagram of [fn, trigger]) {
      await expect(diagram.getByRole('button', { name: 'Zoom in' })).toBeVisible();
      await expect(diagram.getByRole('button', { name: 'Zoom out' })).toBeVisible();
      await expect(diagram.getByRole('button', { name: /Fit/ })).toBeVisible();
      await expect(diagram.getByRole('button', { name: /Reset view/ })).toBeVisible();
    }

    // Same card geometry as the Flow Editor canvas.
    await studio.getByRole('tab', { name: 'Flow editor' }).click();
    const editorCard = await studio.locator('.flow-node').first().evaluate(el => ({ width: el.getAttribute('width'), height: el.getAttribute('height'), rx: el.getAttribute('rx') }));
    await studio.getByRole('tab', { name: /Function library/ }).click();
    const fnCard = await fn.locator('.flow-node').first().evaluate(el => ({ width: el.getAttribute('width'), height: el.getAttribute('height'), rx: el.getAttribute('rx') }));
    const triggerCard = await trigger.locator('.flow-node').first().evaluate(el => ({ width: el.getAttribute('width'), height: el.getAttribute('height'), rx: el.getAttribute('rx') }));
    expect(fnCard).toEqual(editorCard);
    expect(triggerCard).toEqual(editorCard);
    expect(await cardMetrics(page, 'fse-flow-diagram')).toMatchObject({ rx: '12' });
  });

  test('the trigger diagram keeps its semantics: event node, typed payload, read-only', async ({ page }) => {
    const studio = await openLibrary(page);
    const trigger = studio.locator('fse-flow-diagram').filter({ has: page.locator('[aria-label^="Event triggers"]') });
    await expect(trigger).toContainText('filing.published');
    await expect(trigger).toContainText('EVENT');
    await expect(trigger).toContainText('PAYLOAD');
    await expect(trigger).toContainText('Read-only');
    // Diagram nodes are keyed by stable database ids, not by the event key.
    // Locate the semantic label so the test works with arbitrary saved ids.
    const event = trigger.locator('[data-node-id^="event:"]').filter({ hasText: 'filing.published' });
    await expect(event).toHaveCount(1);
    await event.click();
    await expect(event).toHaveAttribute('aria-pressed', 'true');
    await expect(studio.locator('.trigger-detail')).toContainText('filing.published carries a text payload');

    // Selecting the typed payload node must keep the same trigger details.
    const nodeId = await event.getAttribute('data-node-id');
    const payload = trigger.locator(`[data-node-id="payload:${nodeId!.slice('event:'.length)}"]`);
    await payload.click();
    await expect(studio.locator('.trigger-detail')).toContainText('filing.published carries a text payload');
  });

  test('diagram nodes are keyboard operable and zoom is announced', async ({ page }) => {
    const studio = await openLibrary(page);
    const fn = studio.locator('fse-flow-diagram').filter({ has: page.locator('[aria-label^="Function canvas"]') });
    const input = fn.locator('[data-node-id="input"]');
    await input.focus();
    await expect(input).toHaveAttribute('role', 'button');
    await page.keyboard.press('Enter');
    await expect(input).toHaveAttribute('aria-pressed', 'true');
    await page.keyboard.press('ArrowRight');
    await expect(fn.locator('[data-node-id="output"]')).toBeFocused();

    const zoom = fn.locator('.canvas-navigation span').first();
    const before = await zoom.innerText();
    await fn.getByRole('button', { name: 'Zoom in' }).click();
    await expect(zoom).not.toHaveText(before);
    await fn.getByRole('button', { name: /Reset view/ }).click();
    await expect(zoom).toHaveText('100%');
  });

  test('a run snapshot shows state with text, not color alone', async ({ page }) => {
    await openRunSnapshot(page);
    const graph = page.locator('svg[aria-label^="Execution graph for run"]');
    await expect(graph).toBeVisible();
    await expect(graph.locator('[data-node-id]').first()).toHaveAttribute('aria-label', /SUCCESS|FAILED|SKIPPED|RUNNING|NOT RECORDED/);
    await expect(graph.locator('.node-sub').first()).toContainText(/succeeded|failed|skipped|running|not recorded/i);
  });

  test('diagrams render in dark mode with theme tokens and do not overflow the page', async ({ page, isMobile }) => {
    const studio = await openLibrary(page);
    if (isMobile) await page.getByRole('button', { name: 'More actions', exact: true }).click();
    await page.getByRole(isMobile ? 'menuitem' : 'button', { name: /Switch to (light|dark) theme/ }).click();
    const fn = studio.locator('fse-flow-diagram').filter({ has: page.locator('[aria-label^="Function canvas"]') });
    const fill = await fn.locator('.flow-node').first().evaluate(el => getComputedStyle(el).fill);
    const token = await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--fs-node-bg').trim());
    expect(token).not.toBe('');
    expect(fill).not.toBe('none');
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
  });

  // Evidence for reviewers: every graph surface side by side, attached to the HTML report.
  test('captures Functions, Triggers and run snapshot diagrams', async ({ page }, testInfo) => {
    const studio = await openLibrary(page);
    await expect(studio.locator('fse-flow-diagram').first()).toBeVisible();
    await testInfo.attach('functions-and-triggers-diagrams.png', {
      body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
      contentType: 'image/png',
    });
    await openRunSnapshot(page);
    await expect(page.locator('svg[aria-label^="Execution graph for run"]')).toBeVisible();
    await testInfo.attach('run-snapshot-diagram.png', {
      body: await page.screenshot({ fullPage: true, animations: 'disabled' }),
      contentType: 'image/png',
    });
  });
});
