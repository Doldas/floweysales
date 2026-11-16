import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Locator, Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

// The Flow Editor's editable canvas (#262). These tests pin how it behaves, through what a user
// does and what gets saved, not through its markup: dragging a node, drawing a wire from an
// output port to an input port, connecting by keyboard, panning, zooming and the context menu.
// They were written against the editor's own canvas first, and run unchanged on the shared
// <fse-flow-diagram> component, so the move from one to the other cannot change behavior.
//
// A new flow starts as Start -> Research (agent-1), already connected.

interface SavedNode { id: string; type: string; label: string; x: number; y: number }
interface SavedEdge { from: string; to: string; from_port?: string; to_port?: string }
interface SavedGraph { nodes: SavedNode[]; edges: SavedEdge[] }

/** Presses Save flow and returns the graph the editor sent; nothing reaches a server. */
async function savedGraph(page: Page, studio: Locator): Promise<SavedGraph> {
  const sent = page.waitForRequest(r => /\/api\/admin\/flows(\/\d+)?$/.test(r.url()) && ['POST', 'PUT'].includes(r.method()));
  await studio.getByRole('button', { name: 'Save flow' }).click();
  return (await sent).postDataJSON().graph;
}

const canvas = (studio: Locator) => studio.locator('svg[aria-label^="Flow canvas"]');

async function newFlow(page: Page) {
  await page.route(/\/api\/admin\/flows(\/\d+)?$/, route => {
    if (!['POST', 'PUT'].includes(route.request().method())) return route.fallback();
    const body = route.request().postDataJSON();
    return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ id: 99, name: body.name, graph: body.graph, enabled: false, interval_minutes: 0, event_key: '' }) });
  });
  await open(page, 'flows');
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
  await studio.getByLabel('Flow name').fill('Canvas under test');
  await canvas(studio).scrollIntoViewIfNeeded();
  return studio;
}

/** Adds an agent through the node picker and returns its saved node (not connected to anything). */
async function addAgent(page: Page, studio: Locator): Promise<SavedNode> {
  const known = (await savedGraph(page, studio)).nodes.map(n => n.id);
  await studio.getByRole('button', { name: '+ Add node' }).click();
  await studio.getByLabel('Search nodes').fill('agent');
  await studio.locator('#node-picker').getByRole('button', { name: 'Agent', exact: true }).click();
  await page.keyboard.press('Escape');
  const added = (await savedGraph(page, studio)).nodes.find(n => !known.includes(n.id));
  if (!added) throw new Error('the agent was not added');
  return added;
}

const port = (studio: Locator, kind: 'out' | 'in', label: string) =>
  studio.getByRole('button', { name: `${kind === 'out' ? 'Start connection from' : 'Connect into'} ${label}`, exact: true });

const center = async (el: Locator) => {
  const box = await el.boundingBox();
  if (!box) throw new Error('not visible');
  return { x: box.x + box.width / 2, y: box.y + box.height / 2 };
};

const viewBox = async (studio: Locator) => (await canvas(studio).getAttribute('viewBox'))!.split(' ').map(Number);

/** Waits until the view stops changing, so positions read next belong to the drawn canvas. */
async function settled(studio: Locator) {
  let last = '';
  await expect.poll(async () => { const now = (await canvas(studio).getAttribute('viewBox'))!; const same = now === last; last = now; return same; }, { intervals: [60, 60, 60, 120] }).toBe(true);
}

const fit = async (studio: Locator) => { await studio.getByRole('button', { name: /^\s*Fit/ }).click(); await settled(studio); };

/**
 * A spot of the canvas where no node is: its right edge (or left edge), below the sticky top bar
 * the canvas can be scrolled under. The canvas is scrolled into view first.
 */
const emptySpot = async (studio: Locator, edge: 'right' | 'left' = 'right') => {
  await canvas(studio).scrollIntoViewIfNeeded();
  const b = (await canvas(studio).boundingBox())!;
  return { x: edge === 'right' ? b.x + b.width - 8 : b.x + 14, y: Math.max(b.y + 100, 140) };
};

async function drag(page: Page, from: { x: number; y: number }, to: { x: number; y: number }, steps = 8) {
  await page.mouse.move(from.x, from.y);
  await page.mouse.down();
  await page.mouse.move(to.x, to.y, { steps });
  await page.mouse.up();
}

/** A saved function with declared typed pins (inputs score and threshold: number; outputs result: text, passed: boolean). */
const TYPED_FUNCTION = JSON.parse(readFileSync(join(__dirname, '..', '..', 'testdata', 'graphs', 'function-typed.json'), 'utf8'));

/** Adds a Function node from the context menu and points it at the typed function. Returns its saved node. */
async function addTypedFunction(page: Page, studio: Locator, which: number): Promise<SavedNode> {
  // Every Function node lands on the grid by its count; the right click only has to open the menu on empty canvas.
  const spot = await emptySpot(studio, which === 1 ? 'right' : 'left');
  await page.mouse.click(spot.x, spot.y, { button: 'right' });
  await studio.getByRole('menuitem', { name: 'Function', exact: true }).click();
  await expect(canvas(studio).locator('[data-canvas-node]')).toHaveCount(2 + which);
  const node = canvas(studio).locator('[data-canvas-node]').nth(1 + which);
  await node.focus();
  await page.keyboard.press('Enter');
  const settings = studio.getByRole('region', { name: 'Complete node settings' });
  await settings.locator('select').filter({ hasText: 'Choose saved function' }).selectOption({ label: 'Risk assessment' });
  // A click on empty canvas closes the settings, as a user would.
  await canvas(studio).scrollIntoViewIfNeeded();
  const empty = await emptySpot(studio);
  await page.mouse.click(empty.x, empty.y);
  await expect(settings).toHaveCount(0);
  const graph = await savedGraph(page, studio);
  return graph.nodes.filter(n => n.type === 'function')[which - 1];
}

const pins = (studio: Locator, nodeIndex: number, side: 'in' | 'out') =>
  canvas(studio).locator('[data-canvas-node]').nth(nodeIndex).locator(side === 'in' ? 'circle.pin-in' : 'circle.pin-out');

test.describe('Flow Editor canvas editing', () => {
  test('dragging a node moves it by the distance dragged, in whole pixels, and Undo puts it back', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'a touch screen pans the canvas; a node is moved with a mouse');
    const studio = await newFlow(page);
    const before = (await savedGraph(page, studio)).nodes.find(n => n.id === 'start')!;
    await fit(studio);
    const [view, box] = [await viewBox(studio), (await canvas(studio).boundingBox())!];
    const at = await center(studio.getByLabel('Select Start', { exact: true }));
    await drag(page, at, { x: at.x + 120, y: at.y + 60 });

    const after = (await savedGraph(page, studio)).nodes.find(n => n.id === 'start')!;
    expect(Number.isInteger(after.x) && Number.isInteger(after.y)).toBe(true);
    expect(after.x - before.x).toBeCloseTo(120 * view[2] / box.width, 0);
    expect(after.y - before.y).toBeCloseTo(60 * view[3] / box.height, 0);

    await studio.getByRole('button', { name: 'Undo graph change' }).click();
    const undone = (await savedGraph(page, studio)).nodes.find(n => n.id === 'start')!;
    expect([undone.x, undone.y]).toEqual([before.x, before.y]);
  });

  test('dragging from an output port to an input port connects the two nodes', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'wires are drawn with a mouse');
    const studio = await newFlow(page);
    const agent = await addAgent(page, studio);
    await fit(studio);
    await drag(page, await center(port(studio, 'out', 'Start')), await center(port(studio, 'in', agent.label)));
    await expect(studio.getByText(/Connected Start → /)).toBeVisible();

    const graph = await savedGraph(page, studio);
    expect(graph.edges.filter(e => e.from === 'start' && e.to === agent.id)).toHaveLength(1);
    expect(graph.edges).toHaveLength(2);
  });

  test('a wire dropped on empty canvas connects nothing', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'wires are drawn with a mouse');
    const studio = await newFlow(page);
    await addAgent(page, studio);
    await fit(studio);
    await drag(page, await center(port(studio, 'out', 'Start')), await emptySpot(studio));
    expect((await savedGraph(page, studio)).edges).toHaveLength(1);
  });

  test('a second wire between the same ports is refused', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'wires are drawn with a mouse');
    const studio = await newFlow(page);
    await fit(studio);
    // Start -> Research already exists.
    await drag(page, await center(port(studio, 'out', 'Start')), await center(port(studio, 'in', 'Research')));
    await expect(studio.getByText('These ports are already connected.')).toBeVisible();
    expect((await savedGraph(page, studio)).edges).toHaveLength(1);
  });

  test('a node cannot be wired to itself', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'wires are drawn with a mouse');
    const studio = await newFlow(page);
    await fit(studio);
    await drag(page, await center(port(studio, 'out', 'Research')), await center(port(studio, 'in', 'Research')));
    await expect(studio.getByText('A node cannot connect to itself.')).toBeVisible();
    expect((await savedGraph(page, studio)).edges).toHaveLength(1);
  });

  test('ports connect by click and by keyboard, and the hint offers Cancel', async ({ page }) => {
    const studio = await newFlow(page);
    const agent = await addAgent(page, studio);
    const start = port(studio, 'out', 'Start');
    await start.focus();
    await page.keyboard.press('Enter');
    await expect(studio.getByRole('status').filter({ hasText: 'Select an input port' })).toBeVisible();
    await studio.getByRole('button', { name: 'Cancel', exact: true }).first().click();
    await expect(studio.getByText('Select an input port')).toHaveCount(0);

    await start.click();
    await port(studio, 'in', agent.label).click();
    expect((await savedGraph(page, studio)).edges.some(e => e.from === 'start' && e.to === agent.id)).toBe(true);
    await expect(studio.getByText('Select an input port')).toHaveCount(0);

    // Keyboard: Enter on an output port, then Enter on an input port.
    const second = await addAgent(page, studio);
    await port(studio, 'out', 'Research').focus();
    await page.keyboard.press('Enter');
    await port(studio, 'in', second.label).last().focus();
    await page.keyboard.press('Enter');
    const graph = await savedGraph(page, studio);
    expect(graph.edges.some(e => e.from === 'agent-1' && e.to === second.id)).toBe(true);
  });

  test('a drawn wire runs from the output side of one node to the input side of the next', async ({ page }) => {
    const studio = await newFlow(page);
    const graph = await savedGraph(page, studio);
    const [from, to] = [graph.nodes.find(n => n.id === 'start')!, graph.nodes.find(n => n.id === 'agent-1')!];
    const paths = canvas(studio).locator('path.flow-edge');
    await expect(paths).toHaveCount(1);
    const numbers = (await paths.first().getAttribute('d'))!.match(/-?\d+(\.\d+)?/g)!.map(Number);
    expect([numbers[0], numbers[1]]).toEqual([from.x + 125, from.y + 35]);
    expect([numbers[numbers.length - 2], numbers[numbers.length - 1]]).toEqual([to.x, to.y + 35]);
  });

  test('every node is a focusable button that selects on Enter and opens its settings', async ({ page }) => {
    const studio = await newFlow(page);
    await studio.getByLabel('Select Start', { exact: true }).focus();
    await page.keyboard.press('Enter');
    await expect(studio.getByRole('region', { name: 'Complete node settings' })).toBeVisible();
    // Clicking empty canvas closes them again.
    const spot = await emptySpot(studio);
    await page.mouse.click(spot.x, spot.y);
    await expect(studio.getByRole('region', { name: 'Complete node settings' })).toHaveCount(0);
  });

  test('the canvas pans by dragging empty space, zooms with the buttons and resets', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'panning by touch is covered by the gesture tests');
    const studio = await newFlow(page);
    const start = await viewBox(studio);
    const spot = await emptySpot(studio);
    await drag(page, spot, { x: spot.x - 100, y: spot.y + 50 });
    await settled(studio);
    const panned = await viewBox(studio);
    expect(panned[0]).toBeGreaterThan(start[0]);
    expect(panned[1]).toBeLessThan(start[1]);
    expect(panned[2]).toBeCloseTo(start[2]);

    await studio.getByRole('button', { name: 'Zoom in' }).click();
    await expect.poll(async () => (await viewBox(studio))[2]).toBeLessThan(panned[2]);
    await studio.getByRole('button', { name: 'Zoom out' }).click();
    await studio.getByRole('button', { name: 'Zoom out' }).click();
    await expect.poll(async () => (await viewBox(studio))[2]).toBeGreaterThan(panned[2]);

    await studio.getByRole('button', { name: /Reset view/ }).click();
    await expect.poll(() => viewBox(studio)).toEqual([0, 0, 820, 380]);
  });

  test('the mouse wheel zooms the canvas', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'the wheel is a mouse control');
    const studio = await newFlow(page);
    const before = (await viewBox(studio))[2];
    const spot = await emptySpot(studio);
    await page.mouse.move(spot.x, spot.y);
    await page.mouse.wheel(0, -300);
    await expect.poll(async () => (await viewBox(studio))[2]).toBeLessThan(before);
  });

  test('the context menu on the canvas creates a node', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'the context menu is a right click');
    const studio = await newFlow(page);
    const spot = await emptySpot(studio);
    await page.mouse.click(spot.x, spot.y, { button: 'right' });
    const menu = studio.getByRole('menu', { name: 'Create node' });
    await expect(menu).toBeVisible();
    await menu.getByRole('menuitem', { name: 'Research Agent' }).click();
    await expect(menu).toHaveCount(0);
    expect((await savedGraph(page, studio)).nodes.filter(n => n.type === 'agent')).toHaveLength(2);
  });

  test('a function with declared pins shows them as typed ports and wires to them by pin', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'wires are drawn with a mouse');
    await page.route('**/api/admin/blueprint-functions', route => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ functions: [{ id: 5, name: 'Risk assessment', graph: TYPED_FUNCTION }] }) }));
    const studio = await newFlow(page);
    const first = await addTypedFunction(page, studio, 1);
    expect(first.type).toBe('function');
    await fit(studio);

    // Two typed inputs and two typed outputs, in the order the function declares them.
    await expect(pins(studio, 2, 'in')).toHaveCount(2);
    await expect(pins(studio, 2, 'out')).toHaveCount(2);
    expect(await pins(studio, 2, 'in').evaluateAll(els => els.map(e => e.querySelector('title')?.textContent))).toEqual(['Input score (number)', 'Input threshold (number)']);
    expect(await pins(studio, 2, 'out').evaluateAll(els => els.map(e => e.querySelector('title')?.textContent))).toEqual(['Output result (text)', 'Output passed (boolean)']);
    // The plain ports of that node are replaced by its pins.
    await expect(port(studio, 'in', first.label)).toHaveCount(0);
    await expect(port(studio, 'out', first.label)).toHaveCount(0);

    // A plain output may feed a typed input: the wire names the pin and the graph becomes version 2.
    await drag(page, await center(port(studio, 'out', 'Research')), await center(pins(studio, 2, 'in').nth(0)));
    await expect(studio.getByText(/Connected Research → .*\.score/)).toBeVisible();
    // A typed output may feed a plain input.
    const agent = await addAgent(page, studio);
    await fit(studio);
    await drag(page, await center(pins(studio, 2, 'out').nth(1)), await center(port(studio, 'in', agent.label)));
    await expect(studio.getByText(/Connected .*\.passed → /)).toBeVisible();

    const sent = page.waitForRequest(r => /\/api\/admin\/flows(\/\d+)?$/.test(r.url()) && ['POST', 'PUT'].includes(r.method()));
    await studio.getByRole('button', { name: 'Save flow' }).click();
    const saved = (await sent).postDataJSON().graph as SavedGraph & { version?: number };
    expect(saved.version).toBe(2);
    expect(saved.edges).toContainEqual({ from: 'agent-1', to: first.id, to_port: 'in:score' });
    expect(saved.edges).toContainEqual({ from: first.id, to: agent.id, from_port: 'out:passed' });
  });

  test('typed ports of different types refuse to connect, and the wire ends at the pin', async ({ page }, testInfo) => {
    test.skip(testInfo.project.name === 'mobile', 'wires are drawn with a mouse');
    await page.route('**/api/admin/blueprint-functions', route => route.fulfill({ contentType: 'application/json', body: JSON.stringify({ functions: [{ id: 5, name: 'Risk assessment', graph: TYPED_FUNCTION }] }) }));
    const studio = await newFlow(page);
    const one = await addTypedFunction(page, studio, 1);
    const two = await addTypedFunction(page, studio, 2);
    await fit(studio);
    // text output "result" into number input "score"
    await drag(page, await center(pins(studio, 2, 'out').nth(0)), await center(pins(studio, 3, 'in').nth(0)));
    await expect(studio.getByText('Cannot connect text output "result" to number input "score".')).toBeVisible();
    // number is not an output of this function; boolean "passed" into number "threshold" is refused too
    await drag(page, await center(pins(studio, 2, 'out').nth(1)), await center(pins(studio, 3, 'in').nth(1)));
    await expect(studio.getByText('Cannot connect boolean output "passed" to number input "threshold".')).toBeVisible();
    expect((await savedGraph(page, studio)).edges).toHaveLength(1);
    expect([one.id, two.id].every(Boolean)).toBe(true);
  });

  test('fit brings every node into view', async ({ page }) => {
    const studio = await newFlow(page);
    await addAgent(page, studio);
    await fit(studio);
    const svg = (await canvas(studio).boundingBox())!;
    for (const name of await canvas(studio).locator('[data-canvas-node]').evaluateAll(els => els.map(e => e.getAttribute('aria-label')!))) {
      const b = (await studio.getByLabel(name, { exact: true }).boundingBox())!;
      expect(b.x).toBeGreaterThanOrEqual(svg.x - 1);
      expect(b.x + b.width).toBeLessThanOrEqual(svg.x + svg.width + 1);
      expect(b.y).toBeGreaterThanOrEqual(svg.y - 1);
      expect(b.y + b.height).toBeLessThanOrEqual(svg.y + svg.height + 1);
    }
  });
});
