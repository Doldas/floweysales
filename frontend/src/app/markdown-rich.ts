/**
 * Rich-text editing support for Markdown files.
 *
 * `htmlToMarkdown` writes the DOM of the rich-text editor back as Markdown the shared renderer
 * (`markdown.ts`) reads again. It only emits constructs from a fixed allow-list, so pasted or injected markup can
 * never reach the saved file, and `richTextBlockers` names the constructs the rich editor cannot represent so the
 * UI can keep such documents in source mode instead of silently stripping content.
 */

/** The small part of the DOM the serializer needs (real DOM nodes and test fakes both fit). */
export interface RichNode {
  nodeType: number;
  nodeName: string;
  textContent: string | null;
  childNodes: ArrayLike<RichNode>;
  getAttribute?(name: string): string | null;
}

const TEXT = 3;
const ELEMENT = 1;
const BLOCKS = new Set(['P', 'DIV', 'H1', 'H2', 'H3', 'H4', 'H5', 'H6', 'UL', 'OL', 'LI', 'BLOCKQUOTE', 'PRE', 'HR', 'TABLE', 'SECTION', 'ARTICLE']);
const DROPPED = new Set(['SCRIPT', 'STYLE', 'IFRAME', 'OBJECT', 'EMBED', 'LINK', 'META', 'TEMPLATE', 'NOSCRIPT', 'SVG', 'MATH', 'FORM', 'INPUT', 'BUTTON']);

const attr = (node: RichNode, name: string): string => node.getAttribute?.(name) ?? '';
const kids = (node: RichNode): RichNode[] => Array.from(node.childNodes);
const tag = (node: RichNode): string => (node.nodeType === ELEMENT ? node.nodeName.toUpperCase() : '');
const isBlock = (node: RichNode): boolean => BLOCKS.has(tag(node)) || node.getAttribute?.('class')?.includes('md-table-wrap') === true;

/** Escapes characters the shared renderer would read as inline formatting. */
function escapeText(text: string): string {
  return text
    .replace(/ /g, ' ')
    .replace(/([\\`*[\]~])/g, '\\$1')
    .replace(/(^|[^\w])_|_(?=[^\w]|$)/g, (m) => m.replace('_', '\\_'));
}

/** Escapes a block-level marker at the start of a line (`#`, `>`, `-`, `1.`). */
function escapeLineStart(line: string): string {
  return line
    .replace(/^(\s*)(#{1,6}\s)/, '$1\\$2')
    .replace(/^(\s*)([-+>])(\s|$)/, '$1\\$2$3')
    .replace(/^(\s*)(\d+)([.)])(\s)/, '$1$2\\$3$4')
    .replace(/^(\s*)(([-*_])\3{2,})\s*$/, '$1\\$2');
}

/** Allows web links and Workspace-relative targets; anything with another scheme is dropped. */
function safeHref(href: string): string {
  const value = href.trim();
  if (!value || value === '#') return '';
  if (/[\u0000-\u001f\s]/.test(value) && !/^https?:\/\//i.test(value)) return '';
  if (/^[A-Za-z][A-Za-z0-9+.-]*:/.test(value) && !/^https?:\/\//i.test(value)) return '';
  if (value.startsWith('//')) return '';
  return value.replace(/ /g, '%20').replace(/\(/g, '%28').replace(/\)/g, '%29');
}

function inline(nodes: RichNode[]): string {
  let out = '';
  for (const node of nodes) {
    if (node.nodeType === TEXT) {
      out += escapeText(node.textContent ?? '');
      continue;
    }
    if (node.nodeType !== ELEMENT) continue;
    const name = tag(node);
    if (DROPPED.has(name)) continue;
    const content = () => inline(kids(node));
    switch (name) {
      case 'BR':
        out += '\n';
        break;
      case 'STRONG':
      case 'B': {
        const text = content();
        out += text.trim() ? `**${text}**` : text;
        break;
      }
      case 'EM':
      case 'I': {
        const text = content();
        out += text.trim() ? `*${text}*` : text;
        break;
      }
      case 'DEL':
      case 'S':
      case 'STRIKE': {
        const text = content();
        out += text.trim() ? `~~${text}~~` : text;
        break;
      }
      case 'CODE':
        out += (node.textContent ?? '').trim() ? `\`${(node.textContent ?? '').replace(/`/g, "'")}\`` : '';
        break;
      case 'A': {
        const text = content();
        const original = attr(node, 'data-md-href') || attr(node, 'href');
        const href = safeHref(original);
        if (attr(node, 'data-md-image') === '1' && href) {
          out += `![${text.replace(/^Image: /, '')}](${href})`;
        } else {
          out += href && text.trim() ? `[${text}](${href})` : text;
        }
        break;
      }
      default:
        out += content();
    }
  }
  return out;
}

function inlineBlock(nodes: RichNode[]): string {
  return inline(nodes)
    .split('\n')
    .map((l) => escapeLineStart(l.replace(/[ \t]+$/g, '')))
    .join('\n')
    .trim();
}

function listItems(list: RichNode, depth: number): string[] {
  const ordered = tag(list) === 'OL';
  const lines: string[] = [];
  let n = 0;
  for (const item of kids(list)) {
    if (tag(item) !== 'LI') continue;
    n++;
    const marker = ordered ? `${n}. ` : '- ';
    const task = attr(item, 'data-task');
    const prefix = task === 'done' ? '[x] ' : task === 'open' ? '[ ] ' : '';
    const own: RichNode[] = [];
    const nested: RichNode[] = [];
    for (const child of kids(item)) (tag(child) === 'UL' || tag(child) === 'OL' ? nested : own).push(child);
    const text = inline(own)
      .replace(/\n+/g, ' ')
      .trim();
    lines.push(' '.repeat(depth * 2) + marker + prefix + text);
    for (const sub of nested) lines.push(...listItems(sub, depth + 1));
  }
  return lines;
}

function tableRows(table: RichNode): string[] {
  const rows: RichNode[] = [];
  const collect = (node: RichNode) => {
    for (const child of kids(node)) {
      if (tag(child) === 'TR') rows.push(child);
      else if (['THEAD', 'TBODY', 'TFOOT'].includes(tag(child))) collect(child);
    }
  };
  collect(table);
  if (!rows.length) return [];
  const cells = rows.map((row) =>
    kids(row)
      .filter((c) => tag(c) === 'TH' || tag(c) === 'TD')
      .map((c) => inline(kids(c)).replace(/\n+/g, ' ').replace(/\|/g, '\\|').trim()),
  );
  const width = Math.max(...cells.map((r) => r.length));
  const pad = (r: string[]) => Array.from({ length: width }, (_v, i) => r[i] ?? '');
  const line = (r: string[]) => `| ${pad(r).join(' | ')} |`;
  return [line(cells[0]), `| ${Array.from({ length: width }, () => '---').join(' | ')} |`, ...cells.slice(1).map(line)];
}

function blocks(nodes: RichNode[]): string[] {
  const out: string[] = [];
  let run: RichNode[] = [];
  const flush = () => {
    const text = inlineBlock(run);
    if (text) out.push(text);
    run = [];
  };
  for (const node of nodes) {
    if (node.nodeType === TEXT || !isBlock(node)) {
      if (node.nodeType === TEXT || node.nodeType === ELEMENT) run.push(node);
      continue;
    }
    flush();
    const name = tag(node);
    if (/^H[1-6]$/.test(name)) {
      const text = inline(kids(node)).replace(/\n+/g, ' ').trim();
      if (text) out.push(`${'#'.repeat(Number(name[1]))} ${text}`);
    } else if (name === 'UL' || name === 'OL') {
      const lines = listItems(node, 0);
      if (lines.length) out.push(lines.join('\n'));
    } else if (name === 'BLOCKQUOTE') {
      const text = blocks(kids(node)).join('\n\n');
      if (text) out.push(text.split('\n').map((l) => (l ? `> ${l}` : '>')).join('\n'));
    } else if (name === 'PRE') {
      const lang = attr(node, 'data-lang').replace(/[^A-Za-z0-9+#._-]/g, '');
      const body = (node.textContent ?? '').replace(/\n$/, '');
      out.push('```' + lang + '\n' + body.replace(/^```/gm, '` ``') + '\n```');
    } else if (name === 'HR') {
      out.push('---');
    } else if (name === 'TABLE') {
      const rows = tableRows(node);
      if (rows.length) out.push(rows.join('\n'));
    } else if (name === 'LI') {
      out.push(...blocks(kids(node)));
    } else if (kids(node).some(isBlock)) {
      out.push(...blocks(kids(node)));
    } else {
      const text = inlineBlock(kids(node));
      if (text) out.push(text);
    }
  }
  flush();
  return out;
}

/** Serializes the rich-text editor's DOM as Markdown with a trailing newline (empty when there is no content). */
export function htmlToMarkdown(root: RichNode): string {
  const text = blocks(kids(root)).join('\n\n');
  return text ? text + '\n' : '';
}

/**
 * Names the Markdown constructs in `source` that the rich-text editor cannot represent without rewriting or
 * losing them. An empty list means rich-text editing is safe; otherwise the file stays in source mode.
 */
export function richTextBlockers(source: string): string[] {
  const found: string[] = [];
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  if (lines[0] === '---' && lines.slice(1).includes('---')) found.push('front matter');
  let inFence = false;
  let prev = '';
  const add = (reason: string) => {
    if (!found.includes(reason)) found.push(reason);
  };
  for (const line of lines) {
    if (/^```/.test(line)) {
      inFence = !inFence;
      prev = '';
      continue;
    }
    if (inFence) continue;
    const bare = line.replace(/`[^`]*`/g, '');
    // Math is deliberately not editable through the current rich-text DOM:
    // it is a rendered span and serializing it would silently erase TeX
    // delimiters, macro syntax and mathematical meaning.
    if (/\\\(|\\\[|\$\$/.test(bare) || /(^|[^\\])\$([^$\n]+)\$/.test(bare) &&
      /(?:\\[A-Za-z]+|[=^_{}+\-*/])/.test(bare)) add('mathematical formulas');
    // The simple GFM table preview splits on pipes; rich editing of an
    // escaped pipe would silently change cell boundaries. Preserve source.
    if (/^\s*\|/.test(line) && /\\\|/.test(line)) add('escaped table pipes');
    if (/<!--/.test(bare)) add('HTML comments');
    else if (/<\/?[A-Za-z][A-Za-z0-9-]*(\s[^>]*)?\/?>/.test(bare)) add('raw HTML');
    if (/\[\^[^\]]+\]/.test(bare)) add('footnotes');
    if (/^\s{0,3}\[[^\]]+\]:\s*\S/.test(bare)) add('reference-style links');
    if (/\]\[[^\]]*\]/.test(bare)) add('reference-style links');
    if (/^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(line) && /:-|-:/.test(line)) add('table column alignment');
    if (/^( {4}|\t)\S/.test(line) && prev.trim() === '' && !/^\s*([-*+]|\d+[.)])\s/.test(line)) add('indented code blocks');
    if (/^(=+|-{2,})\s*$/.test(line) && prev.trim() !== '' && !/^\s*([-*+>|#]|\d+[.)])/.test(prev)) add('setext headings');
    if (/^>\s*>/.test(line)) add('nested blockquotes');
    if (/^>\s*([-*+]|\d+[.)])\s/.test(line)) add('lists inside quotes');
    prev = line;
  }
  if (inFence) add('an unclosed code fence');
  return found;
}
