// Runs with Node's built-in test runner:
//   node --test src/app/markdown-rich.spec.ts
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown } from './markdown.ts';
import { htmlToMarkdown, richTextBlockers, type RichNode } from './markdown-rich.ts';

// A minimal parser for the HTML the shared renderer emits, so the round trip runs without a browser DOM.
interface FakeNode extends RichNode {
  attrs: Record<string, string>;
  parent?: FakeNode;
}
const VOID = new Set(['br', 'hr', 'img', 'input']);
function decode(text: string): string {
  return text
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&#(\d+);/g, (_m, n: string) => String.fromCharCode(Number(n)))
    .replace(/&amp;/g, '&');
}
function el(name: string, attrs: Record<string, string> = {}, children: FakeNode[] = []): FakeNode {
  const node: FakeNode = {
    nodeType: 1,
    nodeName: name.toUpperCase(),
    attrs,
    childNodes: children,
    get textContent() {
      return children.map((c) => c.textContent ?? '').join('');
    },
    getAttribute: (n: string) => attrs[n] ?? null,
  } as FakeNode;
  return node;
}
function text(value: string): FakeNode {
  return { nodeType: 3, nodeName: '#text', textContent: value, childNodes: [], attrs: {} };
}
function parse(html: string): FakeNode {
  const root = el('div');
  const stack: FakeNode[] = [root];
  const re = /<(\/?)([a-z0-9]+)((?:\s+[a-z-]+="[^"]*")*)\s*(\/?)>|([^<]+)/gi;
  let m: RegExpExecArray | null;
  while ((m = re.exec(html))) {
    const top = stack[stack.length - 1];
    if (m[5] !== undefined) {
      (top.childNodes as FakeNode[]).push(text(decode(m[5])));
    } else if (m[1]) {
      stack.pop();
    } else {
      const attrs: Record<string, string> = {};
      for (const a of m[3].matchAll(/([a-z-]+)="([^"]*)"/gi)) attrs[a[1]] = decode(a[2]);
      const node = el(m[2], attrs);
      (top.childNodes as FakeNode[]).push(node);
      if (!VOID.has(m[2].toLowerCase()) && !m[4]) stack.push(node);
    }
  }
  return root;
}
const roundTrip = (md: string): string => htmlToMarkdown(parse(renderMarkdown(md, { rich: true, workspaceBase: '/docs' })));

test('canonical Markdown survives a rich-text round trip unchanged', () => {
  const doc = [
    '# Title',
    'A paragraph with **bold**, *italic*, ~~gone~~ and `code`.',
    '- one\n  - nested\n- two',
    '1. first\n2. second',
    '- [x] done\n- [ ] open',
    '> quoted text',
    '```ts\nconst a = 1;\n\nconst b = 2;\n```',
    '---',
    '| a | b |\n| --- | --- |\n| 1 | 2 |',
    'See [the docs](https://example.com/docs) and [notes](./notes/a.md) and ![chart](chart.png).',
  ].join('\n\n') + '\n';
  assert.equal(roundTrip(doc), doc);
});

test('relative link targets are preserved as written, not rewritten to absolute paths', () => {
  assert.equal(roundTrip('[a](../up/a.md)\n'), '[a](../up/a.md)\n');
});

test('literal punctuation is escaped so the next render shows it literally', () => {
  const md = htmlToMarkdown(el('div', {}, [el('p', {}, [text('2 * 3 = 6 and a_b and _x_ and [y] # not a heading')])]));
  assert.match(md, /2 \\\* 3/);
  assert.match(md, /a_b/);
  assert.match(md, /\\_x\\_/);
  assert.match(md, /\\\[y\\\]/);
  const lead = htmlToMarkdown(el('div', {}, [el('p', {}, [text('# not a heading')]), el('p', {}, [text('- not a list')]), el('p', {}, [text('1. nor this')])]));
  assert.equal(lead, '\\# not a heading\n\n\\- not a list\n\n1\\. nor this\n');
  assert.doesNotMatch(renderMarkdown(lead), /<h1>|<ul>|<ol>/);
});

test('editor-created markup (divs, b/i, line breaks) becomes Markdown', () => {
  const root = el('div', {}, [
    el('h2', {}, [text('Notes')]),
    el('div', {}, [text('plain '), el('b', {}, [text('bold')]), el('br'), text('next line')]),
    el('ul', {}, [el('li', {}, [text('a')]), el('li', {}, [el('i', {}, [text('b')])])]),
  ]);
  assert.equal(htmlToMarkdown(root), '## Notes\n\nplain **bold**\nnext line\n\n- a\n- *b*\n');
});

test('script, iframe, event handlers and dangerous links in editor DOM never reach the file', () => {
  const root = el('div', {}, [
    el('p', { onclick: 'alert(1)' }, [text('safe')]),
    el('script', {}, [text('alert(1)')]),
    el('iframe', { src: 'https://evil.example' }, [text('frame')]),
    el('p', {}, [el('a', { href: 'javascript:alert(1)' }, [text('click')])]),
    el('p', {}, [el('a', { href: 'data:text/html,<script>' }, [text('data')])]),
    el('p', {}, [el('a', { href: '//evil.example' }, [text('proto')])]),
    el('p', {}, [el('a', { href: 'https://ok.example/a (b)' }, [text('web')])]),
    el('p', {}, [el('img', { src: 'https://evil.example/x.png', onerror: 'alert(1)' })]),
  ]);
  const md = htmlToMarkdown(root);
  assert.doesNotMatch(md, /script|iframe|alert|onclick|onerror|evil|javascript:|data:/);
  assert.match(md, /^safe\n/);
  assert.match(md, /click\n/);
  assert.match(md, /\[web\]\(https:\/\/ok\.example\/a%20%28b%29\)/);
});

test('an empty editor serializes to an empty file', () => {
  assert.equal(htmlToMarkdown(el('div', {}, [el('p', {}, [el('br')])])), '');
  assert.equal(htmlToMarkdown(el('div')), '');
});

test('content the rich editor cannot represent is named so the file stays in source mode', () => {
  assert.deepEqual(richTextBlockers('# Plain\n\n- list\n\n`<b>` inline code is fine\n\n```html\n<div>fenced is fine</div>\n```\n'), []);
  assert.deepEqual(richTextBlockers('Text <span style="x">raw</span>'), ['raw HTML']);
  assert.deepEqual(richTextBlockers('<!-- hidden -->\n\ntext'), ['HTML comments']);
  assert.ok(richTextBlockers('---\ntitle: x\n---\n\nbody').includes('front matter'));
  assert.ok(richTextBlockers('A claim[^1]\n\n[^1]: source').includes('footnotes'));
  assert.ok(richTextBlockers('[ref]: https://example.com\n').includes('reference-style links'));
  assert.ok(richTextBlockers('See [text][ref]').includes('reference-style links'));
  assert.ok(richTextBlockers('| a | b |\n| :-: | --: |\n| 1 | 2 |').includes('table column alignment'));
  assert.ok(richTextBlockers('para\n\n    indented code\n').includes('indented code blocks'));
  assert.ok(richTextBlockers('Heading\n=======\n').includes('setext headings'));
  assert.ok(richTextBlockers('> a\n> > b').includes('nested blockquotes'));
  assert.ok(richTextBlockers('```\nnever closed').includes('an unclosed code fence'));
});

test('mathematical markup blocks lossy rich edits but keeps USD and fenced code editable', () => {
  for (const md of [
    '$$\\frac{1}{2}$$',
    '$$\n\\text{TV}=31.3\n$$',
    '\\[x^2+y^2\\]',
    '\\(x^2\\)',
    '$x^2$',
  ]) assert.ok(richTextBlockers(md).includes('mathematical formulas'), md);
  for (const md of [
    'Price is $3.07 bn and $17.8 bn',
    'Equations in code: ' + '\x60' + '\\(x^2\\)' + '\x60',
    '\x60\x60\x60tex\n\\[\\frac{1}{2}\\]\n\x60\x60\x60',
  ]) assert.ok(!richTextBlockers(md).includes('mathematical formulas'), md);
});

test('rich mode refuses escaped GFM table pipes rather than losing columns', () => {
  const md = '| Name | Logic |\n| --- | --- |\n| Base | a \\| b |';
  assert.ok(richTextBlockers(md).includes('escaped table pipes'));
  assert.deepEqual(richTextBlockers('| Name | Logic |\n| --- | --- |\n| Base | 12 |'), []);
});
