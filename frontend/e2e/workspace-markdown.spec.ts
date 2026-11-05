import type { Page } from '@playwright/test';
import { expect, open, test } from './fixtures';

const HOSTILE = [
  '# Hostile report',
  '',
  'Text with **bold**, a [bad link](javascript:window.__xss=1) and <img src=x onerror="window.__xss=1">.',
  '',
  '<script>window.__xss = 1</script>',
  '',
  '[escape](../../../etc/passwd) and [next](./other.md)',
  '',
  '- [x] done item',
  '- [ ] open item',
  '',
  '| a | b |',
  '| --- | --- |',
  '| 1 | 2 |',
  '',
].join('\n');

/** A small in-memory Workspace behind the mock API. */
async function mockWorkspace(page: Page, initial: Record<string, string>) {
  const store = new Map(Object.entries(initial));
  const puts: { path: string; content: string }[] = [];
  await page.route('**/api/userspace/**', async route => {
    const request = route.request();
    const url = new URL(request.url());
    if (url.pathname === '/api/userspace/files') {
      const files = [...store].map(([path, content]) => ({ path, content_type: 'text/markdown', size: content.length }));
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ files }) });
    }
    if (url.pathname === '/api/userspace/file' && request.method() === 'GET') {
      const content = store.get(url.searchParams.get('path') ?? '');
      return content === undefined
        ? route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not found"}' })
        : route.fulfill({ status: 200, contentType: 'application/octet-stream', body: content });
    }
    if (url.pathname === '/api/userspace/file' && request.method() === 'PUT') {
      const body = request.postDataJSON() as { path: string; content: string };
      store.set(body.path, body.content);
      puts.push(body);
      return route.fulfill({ status: 200, contentType: 'application/json', body: '{"ok":true}' });
    }
    return route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"unmocked"}' });
  });
  return { store, puts };
}

test.describe('Workspace Markdown', () => {
  test('preview renders safely, rich text saves Markdown and drafts survive mode switches', async ({ page }) => {
    await mockWorkspace(page, { '/docs/hostile.md': HOSTILE, '/docs/other.md': '# Other\n' });
    await open(page, 'workspace');
    await page.getByRole('button', { name: 'docs', exact: true }).first().click();
    await page.getByRole('button', { name: /hostile\.md/ }).click();

    const editor = page.locator('fse-markdown-editor');
    await expect(editor.getByRole('tab', { name: 'Edit source' })).toHaveAttribute('aria-selected', 'true');
    await expect(editor.getByLabel('Markdown source')).toHaveValue(HOSTILE);

    // Preview: rendered, nothing executable, no traversal link.
    await editor.getByRole('tab', { name: 'Preview' }).click();
    const preview = editor.getByLabel('Rendered preview');
    await expect(preview.getByRole('heading', { name: 'Hostile report' })).toBeVisible();
    await expect(preview).not.toContainText('# Hostile');
    await expect(preview).not.toContainText('**bold**');
    await expect(preview.locator('strong')).toHaveText('bold');
    await expect(preview.locator('script, img, iframe')).toHaveCount(0);
    await expect(preview.locator('a[href^="javascript:" i]')).toHaveCount(0);
    await expect(preview.locator('a[data-ws-path="/etc/passwd"]')).toHaveCount(0);
    await expect(preview.locator('table td').last()).toHaveText('2');
    expect(await page.evaluate(() => (window as unknown as { __xss?: number }).__xss)).toBeUndefined();

    // The file contains raw HTML, so rich text is disabled with a visible reason and the source stays untouched.
    await expect(editor.getByRole('tab', { name: 'Rich text' })).toBeDisabled();
    await expect(editor.getByRole('note')).toContainText('raw HTML');

    // A relative link opens the other Workspace file.
    await preview.getByRole('link', { name: 'next' }).click();
    await expect(editor.getByLabel('Markdown source')).toHaveValue('# Other\n');
  });

  test('rich text edits are written back as valid Markdown and unsaved state is shown', async ({ page }) => {
    const ws = await mockWorkspace(page, { '/docs/note.md': '# Note\n\nSome *text* here.\n\n- one\n- two\n' });
    await open(page, 'workspace');
    await page.getByRole('button', { name: 'docs', exact: true }).first().click();
    await page.getByRole('button', { name: /note\.md/ }).click();
    const editor = page.locator('fse-markdown-editor');
    await expect(page.getByRole('status').filter({ hasText: 'Saved' })).toBeVisible();

    await editor.getByRole('tab', { name: 'Rich text' }).click();
    const rich = editor.getByRole('textbox', { name: 'Rich text editor' });
    await expect(rich.getByRole('heading', { name: 'Note' })).toBeVisible();
    await rich.getByText('Some').click();
    await page.keyboard.press('End');
    await page.keyboard.type(' More words.');
    await expect(page.getByRole('status').filter({ hasText: 'Unsaved changes' })).toBeVisible();

    // The draft is the same text in source mode.
    await editor.getByRole('tab', { name: 'Edit source' }).click();
    await expect(editor.getByLabel('Markdown source')).toHaveValue('# Note\n\nSome *text* here. More words.\n\n- one\n- two\n');

    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByRole('status').filter({ hasText: 'Saved' }).first()).toBeVisible();
    expect(ws.puts.at(-1)).toMatchObject({ path: '/docs/note.md', content: '# Note\n\nSome *text* here. More words.\n\n- one\n- two\n' });
  });

  test('the mode tabs fit the screen and a long draft does not take over the viewport', async ({ page }) => {
    await mockWorkspace(page, { '/docs/long.md': Array.from({ length: 400 }, (_v, i) => `Line ${i}`).join('\n\n') });
    await open(page, 'workspace');
    await page.getByRole('button', { name: 'docs', exact: true }).first().click();
    await page.getByRole('button', { name: /long\.md/ }).click();
    const source = page.getByLabel('Markdown source');
    const viewport = page.viewportSize()!;
    expect((await source.boundingBox())!.height).toBeLessThanOrEqual(viewport.height * 0.6);
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow).toBeLessThanOrEqual(1);
  });
});

test.describe('Workspace README overview', () => {
  const files = {
    '/proj/README.md': '# Project overview\n\nSee [ideas](../notes/ideas.md) and [escape](../../../etc/passwd) and [gone](missing.md).\n\n' + Array.from({ length: 40 }, (_v, i) => `Paragraph ${i}`).join('\n\n'),
    '/proj/data.md': '# data',
    '/notes/ideas.md': '# Ideas\n',
  };

  test('a folder with a README shows it rendered; one without keeps the normal view', async ({ page }) => {
    await mockWorkspace(page, files);
    await open(page, 'workspace');
    await page.getByRole('button', { name: 'proj', exact: true }).first().click();
    const overview = page.getByRole('region', { name: /Overview/ }).or(page.locator('.readme-overview'));
    await expect(overview.getByRole('heading', { name: 'Project overview' })).toBeVisible();
    await expect(overview).not.toContainText('# Project overview');
    // Long README: clipped with an explicit way to read everything.
    await expect(overview.getByRole('button', { name: 'Show full README' }).first()).toBeVisible();
    await overview.getByRole('button', { name: 'Show full README' }).first().click();
    await expect(overview.getByText('Paragraph 39')).toBeVisible();
    // Unsafe and missing targets: no link for the traversal, a clear message for the missing file.
    await expect(overview.locator('a[data-ws-path="/etc/passwd"]')).toHaveCount(0);
    await overview.getByRole('link', { name: 'gone' }).click();
    await expect(page.getByText(/does not exist in your Workspace/)).toBeVisible();

    // The README stays an ordinary file.
    await overview.getByRole('button', { name: 'Open README' }).click();
    await expect(page.getByLabel('Markdown source')).toHaveValue(files['/proj/README.md']);

    // A link in the README opens the other file.
    await overview.getByRole('link', { name: 'ideas' }).click();
    await expect(page.getByLabel('Markdown source')).toHaveValue('# Ideas\n');

    // A folder without a README (the parent README is not inherited).
    await page.getByRole('button', { name: 'notes', exact: true }).first().click();
    await expect(page.locator('.readme-overview')).toContainText('No README.md in this folder');
    await expect(page.locator('.readme-overview').getByRole('heading')).toHaveCount(0);
  });

  test('saving the README updates the overview', async ({ page }) => {
    await mockWorkspace(page, { '/proj/README.md': '# Old title\n' });
    await open(page, 'workspace');
    await page.getByRole('button', { name: 'proj', exact: true }).first().click();
    const overview = page.locator('.readme-overview');
    await expect(overview.getByRole('heading', { name: 'Old title' })).toBeVisible();
    await overview.getByRole('button', { name: 'Open README' }).click();
    await expect(page.getByLabel('Markdown source')).toHaveValue('# Old title\n');
    await page.getByLabel('Markdown source').fill('# New title\n');
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(overview.getByRole('heading', { name: 'New title' })).toBeVisible();
  });
});
