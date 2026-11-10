import { expect, open, test } from './fixtures';
import { INVESTOR_PLAYBOOKS, buildPlaybook } from '../src/app/components/investor-nodes';

// Every template in the production catalogue gets its own named Playwright
// case. A newly added template is included automatically: no hand-maintained IDs.
// This is an offline UI / graph / validation test, NOT a real provider execution.
const starters = [
  { id: 'research', name: 'Template: Market research report', nodes: 3, edges: 2 },
  { id: 'risk', name: 'Template: Risk score decision', nodes: 5, edges: 4 },
  { id: 'event', name: 'Template: Event-driven analysis', nodes: 3, edges: 2 },
] as const;

async function chooseTemplate(page: import('@playwright/test').Page, id: string): Promise<void> {
  await open(page, 'flows');
  const studio = page.locator('fse-flow-studio');
  await studio.getByRole('tab', { name: 'Flow editor' }).click();
  await studio.getByLabel('Starter templates').selectOption(id);
}

async function assertTemplateGraph(page: import('@playwright/test').Page, name: string, nodes: number, edges: number): Promise<void> {
  const studio = page.locator('fse-flow-studio');
  await expect(studio.getByLabel('Flow name')).toHaveValue(name);
  const canvas = studio.getByRole('group', { name: 'Flow canvas: nodes and directed connections' });
  await expect(canvas).toBeVisible();
  await expect(canvas.locator('[data-canvas-node]')).toHaveCount(nodes);
  await expect(canvas.locator('path.flow-edge')).toHaveCount(edges);
  await expect(studio.locator('.graph-validation')).toContainText('No structural issues');
  await expect(studio.getByRole('button', { name: 'Save flow' })).toBeVisible();
  // Viewing templates cannot schedule or execute them.
  await expect(studio.getByLabel('Enabled', { exact: true })).not.toBeChecked();
}

test.describe('Every Flow Studio starter template — mock, no execution', () => {
  for (const starter of starters) {
    test(starter.id, async ({ page }, info) => {
      test.skip(info.project.name !== 'desktop', 'The full catalogue is tested once on desktop');
      await chooseTemplate(page, starter.id);
      await assertTemplateGraph(page, starter.name, starter.nodes, starter.edges);
    });
  }
});

test.describe('Every investor playbook template — SYS_ADMIN', () => {
  test.use({ role: 'SYS_ADMIN' });
  for (const item of INVESTOR_PLAYBOOKS) {
    test(item.id, async ({ page }, info) => {
      test.skip(info.project.name !== 'desktop', 'The full catalogue is tested once on desktop');
      const built = buildPlaybook(item.id, { admin: true });
      expect(built, `admin template ${item.id}`).not.toBeNull();
      await chooseTemplate(page, item.id);
      await assertTemplateGraph(page, built!.name, built!.graph.nodes.length, built!.graph.edges.length);
    });
  }
});

test.describe('Every permitted investor playbook template — USER', () => {
  test.use({ role: 'USER', grants: ['flows.read', 'flows.write', 'flows.run', 'models.use'] });
  for (const item of INVESTOR_PLAYBOOKS) {
    if (item.adminOnly) continue;
    test(item.id, async ({ page }, info) => {
      test.skip(info.project.name !== 'desktop', 'The full catalogue is tested once on desktop');
      const built = buildPlaybook(item.id, { admin: false });
      expect(built, `user template ${item.id}`).not.toBeNull();
      await chooseTemplate(page, item.id);
      await assertTemplateGraph(page, built!.name, built!.graph.nodes.length, built!.graph.edges.length);
    });
  }
  test('admin-only playbooks are absent from user template selector', async ({ page }, info) => {
    test.skip(info.project.name !== 'desktop');
    await open(page, 'flows');
    await page.locator('fse-flow-studio').getByRole('tab', { name: 'Flow editor' }).click();
    const select = page.locator('fse-flow-studio').getByLabel('Starter templates');
    for (const item of INVESTOR_PLAYBOOKS.filter(p => p.adminOnly)) {
      await expect(select.locator(`option[value="${item.id}"]`)).toHaveCount(0);
    }
  });
});

// A real phone still gets a representative template rendering regression.
test('a playbook canvas is usable on mobile without page overflow', async ({ page }, info) => {
  test.skip(info.project.name !== 'mobile');
  const built = buildPlaybook(INVESTOR_PLAYBOOKS[0].id, { admin: true })!;
  await chooseTemplate(page, INVESTOR_PLAYBOOKS[0].id);
  await assertTemplateGraph(page, built.name, built.graph.nodes.length, built.graph.edges.length);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(2);
});
