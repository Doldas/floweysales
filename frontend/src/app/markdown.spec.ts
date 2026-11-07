// Runs with Node's built-in test runner:
//   node --test src/app/markdown.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown, resolveWorkspacePath } from './markdown.ts';

test('identifiers with underscores are not emphasised', () => {
  const html = renderMarkdown('Source: get_sec_facts and calculate_dcf');
  assert.match(html, /get_sec_facts and calculate_dcf/);
  assert.doesNotMatch(html, /<em>/);
});

test('underscore and asterisk emphasis still work at word boundaries', () => {
  assert.match(renderMarkdown('an _important_ point'), /an <em>important<\/em> point/);
  assert.match(renderMarkdown('(__bold__)'), /\(<strong>bold<\/strong>\)/);
  assert.match(renderMarkdown('*star*'), /<em>star<\/em>/);
});

test('model output cannot inject markup', () => {
  const html = renderMarkdown('<script>alert(1)</script> [x](javascript:alert(1))');
  assert.doesNotMatch(html, /<script>/);
  assert.doesNotMatch(html, /href="javascript:/);
});

test('headings, nested and task lists, tables, quotes and fenced code render', () => {
  const html = renderMarkdown(
    '# Title\n\n- one\n  - nested\n- [x] done\n- [ ] open\n\n1. a\n2. b\n\n> quote\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n```ts\nconst x = 1;\n```',
  );
  assert.match(html, /<h1>Title<\/h1>/);
  assert.doesNotMatch(html, /# Title/);
  assert.match(html, /<li>one<ul><li>nested<\/li><\/ul><\/li>/);
  assert.match(html, /<li data-task="done">done<\/li>/);
  assert.match(html, /<li data-task="open">open<\/li>/);
  assert.match(html, /<ol><li>a<\/li><li>b<\/li><\/ol>/);
  assert.match(html, /<blockquote>quote<\/blockquote>/);
  assert.match(html, /<table>.*<th>a<\/th>.*<td>2<\/td>/);
  assert.match(html, /<span class="md-lang">ts<\/span>/);
  assert.match(html, /<code>const x = 1;<\/code>/);
});

test('malicious HTML, event handlers and dangerous URLs never become markup', () => {
  const payload = [
    '<img src=x onerror=alert(1)>',
    '<iframe src="https://evil.example"></iframe>',
    '[a](javascript:alert(1))',
    '[b](JaVaScRiPt:alert(1))',
    '[c](data:text/html,<script>alert(1)</script>)',
    '![d](javascript:alert(1))',
    '![e](https://evil.example/pixel.png)',
    '[f](https://ok.example/"onmouseover="alert(1))',
  ].join('\n\n');
  for (const options of [{}, { workspaceBase: '/docs' }, { workspaceBase: '/docs', rich: true }]) {
    const html = renderMarkdown(payload, options);
    assert.doesNotMatch(html, /<img|<iframe|<script/i);
    assert.doesNotMatch(html, /href="javascript:/i);
    assert.doesNotMatch(html, /href="data:/i);
    assert.doesNotMatch(html, /<[^>]*\s(onerror|onmouseover)=/i);
  }
});

test('images are never loaded; they render as labeled links', () => {
  const html = renderMarkdown('![Chart](https://example.com/c.png) and ![Local](./c.png)', { workspaceBase: '/r' });
  assert.doesNotMatch(html, /<img/);
  assert.match(html, /<a href="https:\/\/example.com\/c.png"[^>]*>Image: Chart<\/a>/);
  assert.match(html, /data-ws-path="\/r\/c.png"[^>]*>Image: Local<\/a>/);
});

test('relative links resolve inside the Workspace and cannot traverse out of it', () => {
  const html = renderMarkdown('[a](notes/a.md) [b](../b.md) [c](/top.md) [d](../../../etc/passwd) [e](%2e%2e/%2e%2e/%2e%2e/x)', {
    workspaceBase: '/reports/q1',
  });
  assert.match(html, /data-ws-path="\/reports\/q1\/notes\/a.md"/);
  assert.match(html, /data-ws-path="\/reports\/b.md"/);
  assert.match(html, /data-ws-path="\/top.md"/);
  assert.doesNotMatch(html, /passwd/);
  assert.doesNotMatch(html, /data-ws-path="\/x"/);
  assert.match(html, / d e/, 'the labels survive as plain text');
});

test('resolveWorkspacePath normalizes and rejects', () => {
  assert.equal(resolveWorkspacePath('/a/b', './c.md'), '/a/b/c.md');
  assert.equal(resolveWorkspacePath('/a/b', '../c.md?x=1#h'), '/a/c.md');
  assert.equal(resolveWorkspacePath('/', 'x/./y//z.md'), '/x/y/z.md');
  assert.equal(resolveWorkspacePath('/a', '../../x'), null);
  assert.equal(resolveWorkspacePath('/a', '/../x'), null);
  assert.equal(resolveWorkspacePath('/a', 'https://example.com/x'), null);
  assert.equal(resolveWorkspacePath('/a', 'javascript:alert(1)'), null);
  assert.equal(resolveWorkspacePath('/a', '//evil.example/x'), null);
  assert.equal(resolveWorkspacePath('/a', '#top'), null);
  assert.equal(resolveWorkspacePath('/a', 'a\\..\\b'), null);
  assert.equal(resolveWorkspacePath('/a', '%00'), null);
  assert.equal(resolveWorkspacePath('/a', '%zz'), null);
  assert.equal(resolveWorkspacePath('/a', ''), null);
});

test('underscores and asterisks in link targets stay intact', () => {
  const html = renderMarkdown('[x](https://example.com/_a_/b*c*d)');
  assert.match(html, /href="https:\/\/example.com\/_a_\/b\*c\*d"/);
  assert.doesNotMatch(html, /<em>/);
});

test('backslash escapes keep punctuation literal', () => {
  const html = renderMarkdown('a \\*not em\\* and \\_x\\_');
  assert.doesNotMatch(html, /<em>/);
  assert.match(html, /a &#42;not em&#42; and &#95;x&#95;/);
});

test('rich output has plain code blocks and no copy buttons', () => {
  const html = renderMarkdown('```py\nprint(1)\n```', { rich: true });
  assert.match(html, /<pre data-lang="py"><code>print\(1\)<\/code><\/pre>/);
  assert.doesNotMatch(html, /button/);
});

test('a flow report links its citation markers and autolinks its source list', () => {
  const html = renderMarkdown('Revenue rose [[S1]](https://example.com/a).\n\n**Sources**\n- [S1] Acme; <https://example.com/a>; read by the agent');
  assert.match(html, /<a href="https:\/\/example\.com\/a"[^>]*>\[S1\]<\/a>/);
  assert.match(html, /<li>\[S1\] Acme; <a href="https:\/\/example\.com\/a"[^>]*>https:\/\/example\.com\/a<\/a>; read by the agent<\/li>/);
  // A script address is never made a link.
  assert.doesNotMatch(renderMarkdown('<javascript:alert(1)> and [[S1]](javascript:alert(1))'), /href="javascript/);
});
