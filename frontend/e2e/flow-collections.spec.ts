import { expect, open, test } from './fixtures';

// Collection nodes (#441 phase 5): a flow reads one of the owner's
// collections. The mock backend lists the user's collections; the server
// resolves the collection again when the flow runs.
const DRAFT_RUN = '**/api/admin/flows/draft/run';

test.describe('Collection nodes in Flow Studio', () => {
  test('a collection node asks for a collection and sends its id', async ({ page }) => {
    await page.route(/\/api\/collections(\?.*)?$/, route => route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({ collections: [{ id: 7, name: 'Chips' }, { id: 8, name: 'Dividend ideas' }] }),
    }));
    const sent: any[] = [];
    await page.route(DRAFT_RUN, route => {
      sent.push(route.request().postDataJSON());
      return route.fulfill({ contentType: 'application/json', body: JSON.stringify({ status: 'queued', run_id: 302, draft_test: true, scratch: true, simulated: true, graph_hash: 'b'.repeat(64) }) });
    });

    await open(page, 'flows');
    const studio = page.locator('fse-flow-studio');
    await studio.getByRole('tab', { name: 'Flow editor', exact: true }).click();
    await studio.getByLabel('Flow name').fill('Chips digest');
    await studio.getByRole('button', { name: '+ Add node' }).click();
    await studio.getByLabel('Search nodes').fill('collection');
    const picker = studio.locator('#node-picker');
    await expect(picker.getByRole('button', { name: 'Collection news' })).toBeVisible();
    await picker.getByRole('button', { name: 'Collection members' }).click();
    await page.keyboard.press('Escape');
    await studio.getByRole('button', { name: 'Start connection from Start' }).click();
    await studio.getByRole('button', { name: 'Connect into Collection members' }).click();

    const validation = studio.locator('[aria-label="Flow graph validation"]');
    await expect(validation).toContainText('Choose a collection');
    await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
    expect(sent).toEqual([]);

    await validation.getByRole('button', { name: 'Choose a collection: Collection members · Edit node' }).click();
    // The issue opens the node's settings, which stay open.
    const settings = studio.locator('[aria-label="Complete node settings"]');
    const choice = settings.getByRole('combobox', { name: /^Collection\b/ });
    await expect(choice.locator('option')).toHaveText(['Choose a collection', 'Chips', 'Dividend ideas']);
    await choice.selectOption({ label: 'Chips' });
    await expect(validation).not.toContainText('Choose a collection');
    await settings.getByRole('button', { name: 'Close node settings' }).click();

    await studio.getByRole('button', { name: 'Test unsaved draft', exact: true }).click();
    await expect(studio.getByText('Draft test queued from your unsaved editor graph')).toBeVisible();
    const node = sent[0].graph.nodes.find((n: any) => n.type === 'collection');
    expect(node).toMatchObject({ collection: '7', limit: 25 });
    expect(node.symbols).toBeUndefined();
  });
});
