import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

const axeSource = readFileSync(join(process.cwd(), 'node_modules', 'axe-core', 'axe.min.js'), 'utf8');
const NOTE = '# Note\n\nSome *text* here.\n\n- one\n- two\n';

async function mockWorkspace(page: Page, initial: Record<string, string>) {
  const store = new Map(Object.entries(initial));
  await page.route('**/api/userspace/**', async route => {
    const url = new URL(route.request().url());
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (url.pathname === '/api/userspace/files') return json({ files: [...store].map(([path, content]) => ({ path, content_type: 'text/markdown', size: content.length })) });
    if (url.pathname === '/api/userspace/file' && route.request().method() === 'GET') {
      const content = store.get(url.searchParams.get('path') ?? '');
      return content === undefined ? json({ error: 'not found' }, 404) : route.fulfill({ status: 200, contentType: 'application/octet-stream', body: content });
    }
    if (url.pathname === '/api/userspace/file' && route.request().method() === 'PUT') {
      const body = route.request().postDataJSON() as { path: string; content: string };
      store.set(body.path, body.content);
      return json({ ok: true });
    }
    return json({ error: 'unmocked' }, 404);
  });
}

async function openNote(page: Page) {
  await mockWorkspace(page, { '/docs/note.md': NOTE });
  await open(page, 'workspace');
  await page.getByRole('button', { name: 'docs', exact: true }).first().click();
  await page.getByRole('button', { name: /note\.md/ }).click();
  return page.locator('fse-markdown-editor');
}

async function axeViolations(page: Page): Promise<string[]> {
  await page.addScriptTag({ content: axeSource });
  return page.evaluate(async () => {
    // @ts-expect-error axe is injected above
    const r = await window.axe.run(document.querySelector('fse-markdown-editor'), { runOnly: { type: 'tag', values: ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'] } });
    return r.violations.map((v: { id: string; nodes: unknown[] }) => `${v.id} (${v.nodes.length})`);
  });
}

// Accessibility and editing-history criteria of #269 for the Workspace Markdown editor.
test.describe('Workspace Markdown editor accessibility', () => {
  for (const [tab, label] of [['Edit source', 'source'], ['Preview', 'preview'], ['Rich text', 'rich text']] as const) {
    test(`has no axe violations in ${label} mode`, async ({ page }) => {
      const editor = await openNote(page);
      await editor.getByRole('tab', { name: tab }).click();
      await expect(editor.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true');
      expect(await axeViolations(page)).toEqual([]);
    });
  }

  test('the mode tabs and rich-text toolbar work from the keyboard', async ({ page }) => {
    const editor = await openNote(page);
    await editor.getByRole('tab', { name: 'Edit source' }).focus();
    await page.keyboard.press('ArrowRight');
    await expect(editor.getByRole('tab', { name: 'Preview' })).toHaveAttribute('aria-selected', 'true');
    await page.keyboard.press('ArrowRight');
    await expect(editor.getByRole('tab', { name: 'Rich text' })).toHaveAttribute('aria-selected', 'true');

    const toolbar = editor.getByRole('toolbar', { name: 'Rich text formatting' });
    const buttons = toolbar.getByRole('button');
    expect(await buttons.count()).toBeGreaterThan(8);
    for (const button of await buttons.all()) await expect(button, 'every toolbar button has an accessible name').toHaveAccessibleName(/\S/);

    // Reach the toolbar with Tab and apply bold with Enter, no pointer involved.
    const rich = editor.getByRole('textbox', { name: 'Rich text editor' });
    await rich.getByText('Some').click();
    await page.keyboard.press('Home');
    await page.keyboard.press('Shift+End');
    await toolbar.getByRole('button', { name: 'Bold' }).focus();
    await page.keyboard.press('Enter');
    await expect(rich.locator('b, strong')).toContainText('Some');
    await expect(page.getByRole('status').filter({ hasText: 'Unsaved changes' })).toBeVisible();
  });

  test('undo and redo restore source edits and the dirty state', async ({ page }) => {
    const editor = await openNote(page);
    const source = editor.getByLabel('Markdown source');
    await source.click();
    await page.keyboard.press('Control+End');
    await page.keyboard.type('Extra line');
    await expect(source).toHaveValue(NOTE + 'Extra line');

    const toolbar = editor.getByRole('toolbar', { name: 'Source editing' });
    // The browser may group typing into several history steps, so step until the text matches.
    const stepUntil = async (button: 'Undo' | 'Redo', expected: string) => {
      for (let i = 0; i < 12 && (await source.inputValue()) !== expected; i++) await toolbar.getByRole('button', { name: button }).click();
      await expect(source).toHaveValue(expected);
    };
    await stepUntil('Undo', NOTE);
    await stepUntil('Redo', NOTE + 'Extra line');

    // The keyboard shortcut does the same.
    await source.focus();
    for (let i = 0; i < 12 && (await source.inputValue()) !== NOTE; i++) await page.keyboard.press('Control+z');
    await expect(source).toHaveValue(NOTE);
  });

  test('undo and redo work in the rich text editor', async ({ page }) => {
    const editor = await openNote(page);
    await editor.getByRole('tab', { name: 'Rich text' }).click();
    const rich = editor.getByRole('textbox', { name: 'Rich text editor' });
    await rich.getByText('Some').click();
    await page.keyboard.press('End');
    await page.keyboard.type(' More words.');
    await expect(rich).toContainText('More words.');

    const toolbar = editor.getByRole('toolbar', { name: 'Rich text formatting' });
    await toolbar.getByRole('button', { name: 'Undo' }).click();
    await expect(rich).not.toContainText('More words.');
    await toolbar.getByRole('button', { name: 'Redo' }).click();
    await expect(rich).toContainText('More words.');
  });
});
