import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

// Visual regression for every graph surface (#262): the Flow Editor, Functions, Triggers and a
// run snapshot, in the light and the dark theme.
//
// A screenshot of a canvas differs from one machine to the next (fonts, anti-aliasing), so this
// compares what the screenshot would show instead: where every card, port and wire is, which classes
// and colors they have after the theme is applied, the text on them and the controls. That is exact
// and the same everywhere. A change that moves a card, recolors a state or drops a port fails with a
// readable diff of the description.
//
// A deliberate change is accepted by updating the snapshots and reading the diff:
//   npx playwright test e2e/flow-canvas-visual.spec.ts --update-snapshots
// The files are in e2e/__snapshots__/flow-canvas-visual.spec.ts/.

const TYPED_FUNCTION = JSON.parse(readFileSync(join(__dirname, '..', '..', 'testdata', 'graphs', 'function-typed.json'), 'utf8'));

/** What a canvas shows, in the units of its own coordinate system, with the theme applied. */
async function describeCanvas(svg: Locator) {
  return svg.evaluate(el => {
    const round = (v: string | null) => (v === null || v === '' || Number.isNaN(Number(v)) ? v : String(Math.round(Number(v) * 100) / 100));
    const classes = (e: Element) => [...e.classList].filter(c => !c.startsWith('ng-') && c !== 'selected-by-test').sort().join(' ');
    const paint = (e: Element, props: string[]) => {
      const s = getComputedStyle(e);
      return Object.fromEntries(props.map(p => [p, s.getPropertyValue(p).trim()]));
    };
    const shell = el.closest('.diagram-shell, .canvas-shell') as Element;
    const surface = el.closest('.diagram-surface') as Element | null;
    return {
      theme: document.documentElement.getAttribute('data-bs-theme'),
      viewBox: el.getAttribute('viewBox'),
      label: el.getAttribute('aria-label'),
      surface: surface ? paint(surface, ['background-color']) : null,
      toolbar: [...shell.querySelectorAll('.canvas-navigation button')].map(b => (b.getAttribute('aria-label') || b.textContent || '').trim()),
      edges: [...el.querySelectorAll('path.flow-edge')].map(p => ({
        d: p.getAttribute('d'),
        classes: classes(p),
        ...paint(p, ['stroke', 'stroke-width', 'stroke-dasharray']),
      })),
      nodes: [...el.querySelectorAll('[data-canvas-node]')].map(g => {
        const rect = g.querySelector('rect.flow-node')!;
        const accent = g.querySelector('rect.flow-node-accent')!;
        return {
          name: g.getAttribute('aria-label'),
          role: g.getAttribute('role'),
          card: {
            x: round(rect.getAttribute('x')), y: round(rect.getAttribute('y')), width: round(rect.getAttribute('width')), height: round(rect.getAttribute('height')), rx: round(rect.getAttribute('rx')),
            classes: classes(rect), ...paint(rect, ['fill', 'stroke', 'stroke-width', 'opacity']),
          },
          accent: { type: accent.getAttribute('data-type'), ...paint(accent, ['fill']) },
          texts: [...g.querySelectorAll('text')].map(t => ({
            classes: classes(t), text: (t.textContent || '').trim(), x: round(t.getAttribute('x')), y: round(t.getAttribute('y')),
            ...paint(t, ['fill', 'font-size', 'font-weight']),
          })),
          ports: [...g.querySelectorAll('circle')].map(c => ({
            classes: classes(c), cx: round(c.getAttribute('cx')), cy: round(c.getAttribute('cy')), r: round(c.getAttribute('r')),
            name: c.getAttribute('aria-label') || c.querySelector('title')?.textContent || null, ...paint(c, ['fill', 'stroke', 'stroke-width']),
          })),
        };
      }),
    };
  });
}

const asText = (description: unknown) => JSON.stringify(description, null, 2) + '\n';

async function settled(svg: Locator) {
  let last = '';
  await expect.poll(async () => { const now = (await svg.getAttribute('viewBox'))!; const same = now === last; last = now; return same; }, { intervals: [60, 60, 60, 120] }).toBe(true);
}

/** Both themes of one canvas: the description of each is compared to its snapshot. */
async function bothThemes(page: Page, name: string, svg: () => Locator, toggle: () => Promise<void>) {
  await page.emulateMedia({ colorScheme: 'light', reducedMotion: 'reduce' });
  // A color read in the middle of a CSS transition is no color of the theme; there are none while this is set.
  await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' });
  await expect(svg()).toBeVisible();
  await settled(svg());
  await page.mouse.move(0, 0);
  const light = await describeCanvas(svg());
  expect(light.theme === 'dark', 'the first description is the light theme').toBe(false);
  expect(asText(light)).toMatchSnapshot(`${name}-light.json`);
  await toggle();
  await settled(svg());
  const dark = await describeCanvas(svg());
  expect(dark.theme).toBe('dark');
  expect(asText(dark)).toMatchSnapshot(`${name}-dark.json`);
  // The same canvas in the other theme: the colors change, the geometry does not.
  expect(dark.nodes.map(n => n.card.x + ',' + n.card.y)).toEqual(light.nodes.map(n => n.card.x + ',' + n.card.y));
  expect(dark.nodes[0].card.fill).not.toBe(light.nodes[0].card.fill);
}

const switchTheme = (page: Page) => async () => {
  await page.getByRole('button', { name: /Switch to (light|dark) theme/ }).click();
  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'dark');
  await page.mouse.move(0, 0);
};

test.describe('graph surfaces, described against their snapshots', () => {
  // The canvas is described in canvas units, so it is the same at every screen width; one project is enough.
  test.beforeEach(({}, testInfo) => test.skip(testInfo.project.name !== 'desktop', 'the description does not depend on the screen width'));

  test('Flow Editor: a new flow', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
    await bothThemes(page, 'editor-new-flow', () => studio.locator('svg[aria-label^="Flow canvas"]'), switchTheme(page));
  });

  test('Flow Editor: a template, fitted, with a node selected', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
    await studio.getByLabel('Starter templates').selectOption('price-alerts');
    await studio.getByText('Playbook copied as a new, disabled flow').waitFor();
    await studio.getByRole('button', { name: /^\s*Fit/ }).click();
    await studio.getByLabel('Select Big moves', { exact: true }).click();
    await expect(studio.getByRole('region', { name: 'Complete node settings' })).toBeVisible();
    await bothThemes(page, 'editor-template-selected', () => studio.locator('svg[aria-label^="Flow canvas"]'), switchTheme(page));
  });

  test('Flow Editor: a function call with typed pins', async ({ page }) => {
    await page.route('**/api/admin/blueprint-functions', route => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ functions: [{ id: 5, name: 'Risk assessment', graph: TYPED_FUNCTION }] }) }));
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
    const svg = () => studio.locator('svg[aria-label^="Flow canvas"]');
    await svg().scrollIntoViewIfNeeded();
    const b = (await svg().boundingBox())!;
    await page.mouse.click(b.x + b.width - 8, Math.max(b.y + 100, 140), { button: 'right' });
    await studio.getByRole('menuitem', { name: 'Function', exact: true }).click();
    await expect(svg().locator('[data-canvas-node]')).toHaveCount(3);
    await svg().locator('[data-canvas-node]').nth(2).focus();
    await page.keyboard.press('Enter');
    const settings = studio.getByRole('region', { name: 'Complete node settings' });
    await settings.locator('select').filter({ hasText: 'Choose saved function' }).selectOption({ label: 'Risk assessment' });
    await svg().scrollIntoViewIfNeeded();
    const e = (await svg().boundingBox())!;
    await page.mouse.click(e.x + e.width - 8, Math.max(e.y + 100, 140));
    await expect(settings).toHaveCount(0);
    await studio.getByRole('button', { name: /^\s*Fit/ }).click();
    await bothThemes(page, 'editor-typed-pins', svg, switchTheme(page));
  });

  test('Functions: the function canvas', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: /Function library/ }).click();
    await bothThemes(page, 'functions', () => studio.locator('svg[aria-label^="Function canvas"]'), switchTheme(page));
  });

  test('Functions: a typed function preview', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: /Function library/ }).click();
    await studio.getByRole('button', { name: 'New typed function' }).click();
    await bothThemes(page, 'functions-typed', () => studio.locator('svg[aria-label^="Typed function graph"]'), switchTheme(page));
  });

  test('Triggers: the event triggers canvas', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: /Function library/ }).click();
    await bothThemes(page, 'triggers', () => studio.locator('svg[aria-label^="Event triggers"]'), switchTheme(page));
  });

  test('Run history: the execution graph of a run', async ({ page }) => {
    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'Run history' }).click();
    await studio.getByLabel('Select flow for history').selectOption({ label: 'Filing summary' });
    const run = studio.locator('details.flow-run').filter({ has: page.locator('summary', { hasText: '#41' }) });
    await run.locator('summary').click();
    await run.getByRole('button', { name: 'Inspect nodes' }).click();
    const graph = () => page.locator('svg[aria-label^="Execution graph for run"]');
    await expect(graph()).toBeVisible();
    await bothThemes(page, 'run-snapshot', graph, switchTheme(page));
  });
});
