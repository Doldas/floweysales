import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';

import { buildGuide, checkRepoLinks, checkSitePages, headingIds, parseFrontMatter, renderMarkdown, resolveLink, slugify, stalePages } from './build-docs.mjs';
import { syncBrand, tokensCss } from './sync-brand.mjs';

/** A throwaway repository layout with the given guide pages. */
function fixture(pages, extra = {}) {
  const base = mkdtempSync(join(tmpdir(), 'fs-docs-test-'));
  mkdirSync(join(base, 'docs', 'guide'), { recursive: true });
  for (const [name, text] of Object.entries(pages)) writeFileSync(join(base, 'docs', 'guide', name), text);
  for (const [name, text] of Object.entries(extra)) writeFileSync(join(base, name), text);
  return base;
}
const page = (title, body) => `---\ntitle: ${title}\nlast-reviewed: 2026-11-04\n---\n\n${body}\n`;

test('front matter is split from the body', () => {
  const { meta, body } = parseFrontMatter('---\ntitle: A\norder: 2\n---\n# Hi\n');
  assert.equal(meta.title, 'A');
  assert.equal(meta.order, '2');
  assert.equal(body.trim(), '# Hi');
});

test('heading ids are stable and unique', () => {
  assert.equal(slugify('Who can do what?'), 'who-can-do-what');
  assert.deepEqual([...headingIds('# A\n## Same\n## Same\n```\n# not a heading\n```\n')], ['a', 'same', 'same-2']);
});

test('markdown escapes HTML before formatting', () => {
  const ctx = { file: '/x/docs/guide/a.md', guideFiles: new Set(), base: '/x', headings: {}, errors: [], links: [] };
  const { html } = renderMarkdown('A <script>alert(1)</script> and **bold** and `<b>`', ctx);
  assert.ok(!html.includes('<script>'));
  assert.ok(html.includes('&lt;script&gt;'));
  assert.ok(html.includes('<strong>bold</strong>'));
  assert.ok(html.includes('<code>&lt;b&gt;</code>'));
});

test('tables, lists and code blocks render', () => {
  const ctx = { file: '/x/docs/guide/a.md', guideFiles: new Set(), base: '/x', headings: {}, errors: [], links: [] };
  const { html } = renderMarkdown('| A | B |\n| --- | --- |\n| 1 | 2 |\n\n- one\n- two\n\n1. first\n\n```\ncode & more\n```', ctx);
  assert.match(html, /<table>.*<th scope="col">A<\/th>.*<td>2<\/td>/s);
  assert.match(html, /<ul><li>one<\/li><li>two<\/li><\/ul>/);
  assert.match(html, /<ol><li>first<\/li><\/ol>/);
  assert.match(html, /<pre><code>code &amp; more<\/code><\/pre>/);
});

test('links resolve to guide pages, repository files and anchors, or fail', () => {
  const base = fixture({ 'a.md': page('A', '# A\n## Part'), 'b.md': page('B', '# B') }, { 'README.md': 'x' });
  const from = join(base, 'docs', 'guide', 'a.md');
  const guideFiles = new Set([from, join(base, 'docs', 'guide', 'b.md')]);
  const opts = { guideFiles, base, headings: { a: new Set(['a', 'part']), b: new Set(['b']) } };
  assert.equal(resolveLink('b.md', from, opts).href, 'b.html');
  assert.equal(resolveLink('b.md#b', from, opts).href, 'b.html#b');
  assert.match(resolveLink('b.md#nope', from, opts).error, /no heading/);
  assert.match(resolveLink('../../README.md', from, opts).href, /github\.com\/Doldas\/floweysales\/blob\/main\/README\.md$/);
  assert.match(resolveLink('../../docs', from, opts).href, /\/tree\/main\/docs$/);
  assert.match(resolveLink('../missing.md', from, opts).error, /no such file/);
  assert.match(resolveLink('../../../outside.md', from, opts).error, /no such file/);
  assert.equal(resolveLink('https://example.org/x', from, opts).external, true);
});

test('a build writes pages and reports broken links and missing front matter', () => {
  const ok = fixture({ 'index.md': page('Home', '# Home\n[B](b.md)'), 'b.md': page('B', '# B\n[home](index.md#home)') });
  const out = mkdtempSync(join(tmpdir(), 'fs-docs-out-'));
  const good = buildGuide({ out, base: ok });
  assert.deepEqual(good.errors, []);
  assert.ok(existsSync(join(out, 'guide', 'index.html')));
  assert.match(readFileSync(join(out, 'guide', 'index.html'), 'utf8'), /<html lang="en-US">/);
  assert.ok(JSON.parse(readFileSync(join(out, 'guide', 'search.json'), 'utf8')).length === 2);

  const bad = fixture({ 'index.md': page('Home', '# Home\n[gone](gone.md) [x](#nope)'), 'b.md': '# no front matter' });
  const result = buildGuide({ out: mkdtempSync(join(tmpdir(), 'fs-docs-out-')), base: bad });
  const text = result.errors.join('\n');
  assert.match(text, /gone\.md/);
  assert.match(text, /no heading "#nope"/);
  assert.match(text, /needs a title/);
  assert.match(text, /needs last-reviewed/);
});

test('site pages must exist and be en-US', () => {
  const site = mkdtempSync(join(tmpdir(), 'fs-site-'));
  writeFileSync(join(site, 'a.html'), '<html lang="sv"><a href="gone.html">x</a><a href="https://x.org">y</a><img src="a.html"></html>');
  const errors = checkSitePages(site).join('\n');
  assert.match(errors, /broken link gone\.html/);
  assert.match(errors, /lang must be en-US/);
  assert.doesNotMatch(errors, /x\.org/);
});

test('the real guide builds with no broken links', () => {
  const out = mkdtempSync(join(tmpdir(), 'fs-docs-real-'));
  const { errors, pages } = buildGuide({ out });
  assert.deepEqual(errors, []);
  assert.ok(pages.length >= 8);
});

test('the site brand copies match the app', () => {
  assert.deepEqual(syncBrand({ write: false }), [], 'run: node scripts/sync-brand.mjs');
});

test('tokens css carries both themes and follows the system setting', () => {
  const css = tokensCss(readFileSync(new URL('../frontend/src/styles/_tokens.scss', import.meta.url), 'utf8'));
  assert.match(css, /:root\[data-theme='dark'\]/);
  assert.match(css, /prefers-color-scheme: dark/);
  assert.match(css, /--fs-accent: #5b5ff0/);
});

test('entry documents link only to files that exist', () => {
  const base = fixture({}, { 'README.md': '[ok](docs) [gone](docs/gone.md) [web](https://x.org) [top](#top)\n```\n[skip](nowhere.md)\n```' });
  const errors = checkRepoLinks(['README.md', 'missing.md'], base);
  assert.deepEqual(errors, ['README.md: broken link docs/gone.md', 'missing.md: file is missing']);
  assert.deepEqual(checkRepoLinks(['README.md', 'docs/README.md', 'CONTRIBUTING.md']), []);
});

test('pages not reviewed for six months are listed, and a bad date is an error', () => {
  const pages = [
    { file: 'old.md', meta: { 'last-reviewed': '2026-01-01' } },
    { file: 'new.md', meta: { 'last-reviewed': '2026-10-01' } },
  ];
  assert.deepEqual(stalePages(pages, new Date('2026-11-04')), ['old.md: last reviewed 2026-01-01']);
  const base = fixture({ 'index.md': '---\ntitle: Home\nlast-reviewed: soon\n---\n\n# Home\n' });
  const result = buildGuide({ out: mkdtempSync(join(tmpdir(), 'fs-docs-out-')), base });
  assert.match(result.errors.join('\n'), /not a date/);
});
