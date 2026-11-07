/**
 * A tiny, dependency-free Markdown renderer shared by chat bubbles, help pages, run output and the Workspace
 * (Markdown preview, rich-text editor and README overview).
 *
 * Every piece of the source is HTML-escaped before any formatting is applied,
 * so model/user output can never inject markup or script. The renderer only
 * ever emits tags it generates itself, only allows http(s) links (plus Workspace
 * paths when a Workspace folder is given) and never loads images: an image is
 * rendered as a labeled link.
 */

const HTML_ESCAPES: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
};

export function escapeHtml(value: string): string {
  return value.replace(/[&<>"']/g, (ch) => HTML_ESCAPES[ch]);
}

function unescapeHtml(value: string): string {
  return value
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, '&');
}

export interface MarkdownOptions {
  /**
   * Workspace folder the document lives in (for example `/reports`). When set, relative links resolve inside the
   * Workspace and render as in-app links; targets that escape the Workspace root are rendered as plain text.
   */
  workspaceBase?: string;
  /**
   * Rich-text editing output: code blocks are plain `pre` elements carrying their language in a data attribute and
   * there are no copy buttons, so the editor can write Markdown back without rewriting anything.
   */
  rich?: boolean;
}

/**
 * Resolves a Markdown link target against a Workspace folder. Returns the normalized absolute Workspace path, or
 * null when the target is not a Workspace path (external scheme, empty, anchor-only) or leaves the Workspace
 * through `..` segments. Query strings and fragments are dropped.
 */
export function resolveWorkspacePath(base: string, href: string): string | null {
  let target = href.trim();
  if (!target || target.startsWith('#') || target.startsWith('//') || /^[A-Za-z][A-Za-z0-9+.-]*:/.test(target)) return null;
  target = target.replace(/[?#].*$/, '');
  try {
    target = decodeURIComponent(target);
  } catch {
    return null;
  }
  if (!target || /[\u0000-\u001f\\]/.test(target)) return null;
  const parts = target.startsWith('/') ? [] : base.split('/').filter(Boolean);
  for (const segment of target.split('/')) {
    if (segment === '' || segment === '.') continue;
    if (segment === '..') {
      if (!parts.length) return null;
      parts.pop();
      continue;
    }
    parts.push(segment);
  }
  return parts.length ? '/' + parts.join('/') : null;
}

interface InlineState {
  code: string[];
  math: string[];
  /** Opening tags of generated links, kept out of the text so emphasis rules never touch attributes. */
  tags: string[];
  options: MarkdownOptions;
}

/** Renders a link or image target; unsafe or out-of-Workspace targets fall back to plain text. */
function renderTarget(label: string, escapedHref: string, state: InlineState, image: boolean): string {
  const href = unescapeHtml(escapedHref).trim();
  const base = state.options.workspaceBase;
  const keep = ` data-md-href="${escapeHtml(href)}"${image ? ' data-md-image="1"' : ''}`;
  const prefix = image ? 'Image: ' : '';
  const stash = (tag: string) => `\u0000T${state.tags.push(tag) - 1}\u0000${prefix}${label}</a>`;
  if (/^https?:\/\/[^\s]+$/i.test(href)) {
    return stash(`<a href="${escapeHtml(href)}" target="_blank" rel="noopener noreferrer"${keep}>`);
  }
  if (base !== undefined) {
    const path = resolveWorkspacePath(base, href);
    if (path === null) return label;
    return stash(`<a href="#" class="md-ws-link" data-ws-path="${escapeHtml(path)}"${keep}>`);
  }
  if (!image && href.startsWith('/') && !href.startsWith('//') && !/[\u0000-\u001f\\]/.test(href)) {
    return stash(`<a href="${escapeHtml(href)}" target="_blank" rel="noopener noreferrer"${keep}>`);
  }
  return image ? label : `[${label}](${escapedHref})`;
}

/** Renders inline constructs: code, links, bold, italic, strike, underline. */
function renderInline(text: string, state: InlineState): string {
  // Pull `code` spans out first so emphasis rules never touch them.
  let out = text.replace(/`([^`]+)`/g, (_m, code: string) => {
    const idx = state.code.push(code) - 1;
    return `\u0000C${idx}\u0000`;
  });

  // Images are never loaded (no remote content, no Workspace file fetches): they render as a labeled link.
  out = out.replace(/!\[([^\]]*)\]\(([^\s)]+)\)/g, (_m, alt: string, href: string) =>
    renderTarget(alt || 'image', href, state, true),
  );
  // The label may hold one level of brackets, as in a citation link: [[S1]](https://...).
  out = out.replace(/\[((?:[^\[\]]|\[[^\[\]]*\])+)\]\(([^\s)]+)\)/g, (_m, label: string, href: string) =>
    renderTarget(label, href, state, false),
  );
  // Autolinks: <https://example.com/page> (the text is already escaped here).
  out = out.replace(/&lt;(https?:\/\/[^\s<>]+?)&gt;/g, (_m, href: string) => renderTarget(href, href, state, false));
  // Math must be extracted before backslash-escape and emphasis rules.
  // Link attributes are already stashed above; fenced and inline code are isolated.
  out = out.replace(/(^|[^\\])(?:\\\((.+?)\\\)|\$([^$\n]+)\$)/g,
    (original, prefix: string, parens: string | undefined, dollars: string | undefined) => {
      const body = parens ?? dollars ?? '';
      if (dollars !== undefined && !isInlineDollarFormula(unescapeHtml(body))) return original;
      const idx = state.math.push(renderFormula(unescapeHtml(body))) - 1;
      return prefix + '\u0000M' + idx + '\u0000';
    });

  // Backslash escapes for inline punctuation (also written by the rich-text editor).
  out = out.replace(/\\([\\`*_{}[\]()#+\-.!|~])/g, (_m, ch: string) => `&#${ch.charCodeAt(0)};`);

  out = out.replace(/\*\*\*([^*]+)\*\*\*/g, '<strong><em>$1</em></strong>');
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Underscore emphasis only at word boundaries, so identifiers such as get_sec_facts stay intact.
  out = out.replace(/(^|[^\w])__([^_\n]+)__(?!\w)/g, '$1<strong>$2</strong>');
  out = out.replace(/~~([^~]+)~~/g, '<del>$1</del>');
  out = out.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
  out = out.replace(/(^|[^\w])_([^_\n]+)_(?!\w)/g, '$1<em>$2</em>');

  out = out.replace(/\u0000T(\d+)\u0000/g, (_m, i: string) => state.tags[Number(i)] ?? '');
  out = out.replace(/\u0000C(\d+)\u0000/g, (_m, i: string) => {
    return `<code>${state.code[Number(i)] ?? ''}</code>`;
  });

  out = out.replace(/\u0000M(\d+)\u0000/g, (_m, i: string) => state.math[Number(i)] ?? '');
  return out;
}

function restoreCodePlaceholders(html: string, state: InlineState): string {
  return html.replace(/\u0000C(\d+)\u0000/g, (_m, i: string) => `<code>${state.code[Number(i)] ?? ''}</code>`);
}

const FENCE = /^```([A-Za-z0-9+#._-]*)\s*$/;
const LIST_ITEM = /^(\s*)([-*+]|\d+[.)])\s+(.*)$/;

interface ListItem {
  indent: number;
  ordered: boolean;
  text: string;
}

/** Builds one (possibly nested) list from consecutive item lines, advancing `pos`. */
function buildList(items: ListItem[], pos: { i: number }, state: InlineState): string {
  const { indent, ordered } = items[pos.i];
  let html = '';
  while (pos.i < items.length) {
    const item = items[pos.i];
    if (item.indent < indent || (item.indent === indent && item.ordered !== ordered)) break;
    if (item.indent > indent) {
      html += buildList(items, pos, state);
      continue;
    }
    pos.i++;
    const task = item.text.match(/^\[([ xX])\]\s+(.*)$/);
    let inner = renderInline(task ? task[2] : item.text, state);
    if (pos.i < items.length && items[pos.i].indent > indent) inner += buildList(items, pos, state);
    const attr = task ? ` data-task="${task[1] === ' ' ? 'open' : 'done'}"` : '';
    html += `<li${attr}>${inner}</li>`;
  }
  const tag = ordered ? 'ol' : 'ul';
  return `<${tag}>${html}</${tag}>`;
}

export function renderMarkdown(source: string, options: MarkdownOptions = {}): string {
  if (!source) return '';

  const escaped = escapeHtml(source.replace(/\r\n?/g, '\n'));
  const fences: string[] = [];

  const lines = escaped.split('\n');
  const out: string[] = [];
  const inline: InlineState = { code: [], math: [], tags: [], options };
  let paragraph: string[] = [];

  const flushParagraph = () => {
    if (!paragraph.length) return;
    out.push(`<p>${renderInline(paragraph.join('<br />'), inline)}</p>`);
    paragraph = [];
  };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];

    // Fenced code block
    const fenceMatch = line.match(FENCE);
    if (fenceMatch) {
      flushParagraph();
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        body.push(lines[i]);
        i++;
      }
      const lang = fenceMatch[1];
      const idx = fences.push(
        options.rich
          ? `<pre data-lang="${lang}"><code>${body.join('\n')}</code></pre>`
          : `<div class="md-code"><div class="md-code-head"><span class="md-lang">${lang || 'code'}</span>` +
              `<button type="button" class="md-copy" data-md-copy>copy</button></div>` +
              `<pre><code>${body.join('\n')}</code></pre></div>`,
      ) - 1;
      out.push(`\u0000F${idx}\u0000`);
      continue;
    }

    // Display TeX: \[...\] or $$...$$ (single or multiple lines), and
    // the legacy [ ... ] block produced by some text-only model prompts.
    // Recognize these BEFORE inline dollar handling: financial currency such
    // as "$2.78 bn" must never become a math block.
    const mathOpen = line.trim();
    const compact = mathOpen.match(/^(?:\$\$(.+)\$\$|\\\[(.+)\\\])$/);
    if (compact) {
      const formula = compact[1] ?? compact[2];
      if (formula.trim() && formula.length <= 2000) {
        flushParagraph();
        const idx = fences.push(renderFormula(unescapeHtml(formula.trim()), true)) - 1;
        out.push('\u0000F' + idx + '\u0000');
        continue;
      }
    }
    if (mathOpen === '\\[' || mathOpen === '$$' || mathOpen === '[') {
      const close = mathOpen === '$$' ? '$$' : mathOpen === '[' ? ']' : '\\]';
      let j = i + 1;
      while (j < lines.length && j - i <= 30 && lines[j].trim() !== close) {
        // An unclosed math block must never swallow a following code fence
        // or an unrelated paragraph (including a financial price).
        if (FENCE.test(lines[j]) || (mathOpen === '$$' && lines[j].trim() === '')) break;
        j++;
      }
      const formula = lines.slice(i + 1, j).join('\n').trim();
      if (j < lines.length && lines[j].trim() === close && formula.length > 0 && formula.length <= 2000 &&
        (mathOpen !== '[' || /\\(?:frac|text|times|sum|sqrt|mathrm)/.test(formula))) {
        flushParagraph();
        const idx = fences.push(renderFormula(unescapeHtml(formula), true)) - 1;
        out.push('\u0000F' + idx + '\u0000');
        i = j;
        continue;
      }
    }

    // Horizontal rule
    if (/^(-{3,}|\*{3,}|_{3,})\s*$/.test(line)) {
      flushParagraph();
      out.push('<hr />');
      continue;
    }

    // Heading
    const heading = line.match(/^(#{1,6})\s+(.*)$/);
    if (heading) {
      flushParagraph();
      const level = heading[1].length;
      out.push(`<h${level}>${renderInline(heading[2], inline)}</h${level}>`);
      continue;
    }

    // Table: header row + separator row
    if (line.includes('|') && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|[\s:|-]*\s*$/.test(lines[i + 1])) {
      flushParagraph();
      const parseRow = (row: string) =>
        row
          .trim()
          .replace(/^\|/, '')
          .replace(/\|$/, '')
          .split('|')
          .map((c) => c.trim());
      const header = parseRow(line);
      i += 2;
      const rows: string[][] = [];
      while (i < lines.length && lines[i].includes('|') && lines[i].trim() !== '') {
        rows.push(parseRow(lines[i]));
        i++;
      }
      i--;
      const thead = `<tr>${header.map((c) => `<th>${renderInline(c, inline)}</th>`).join('')}</tr>`;
      const tbody = rows
        .map((r) => `<tr>${r.map((c) => `<td>${renderInline(c, inline)}</td>`).join('')}</tr>`)
        .join('');
      out.push(`<div class="md-table-wrap"><table><thead>${thead}</thead><tbody>${tbody}</tbody></table></div>`);
      continue;
    }

    // Blockquote
    if (/^&gt;\s?/.test(line)) {
      flushParagraph();
      const quote: string[] = [];
      while (i < lines.length && /^&gt;\s?/.test(lines[i])) {
        quote.push(lines[i].replace(/^&gt;\s?/, ''));
        i++;
      }
      i--;
      out.push(`<blockquote>${renderInline(quote.join('<br />'), inline)}</blockquote>`);
      continue;
    }

    // Lists (unordered, ordered, nested by indentation, task items)
    if (LIST_ITEM.test(line)) {
      flushParagraph();
      const items: ListItem[] = [];
      while (i < lines.length) {
        const m = lines[i].match(LIST_ITEM);
        if (!m) break;
        items.push({ indent: m[1].replace(/\t/g, '    ').length, ordered: /\d/.test(m[2][0]), text: m[3] });
        i++;
      }
      i--;
      const pos = { i: 0 };
      while (pos.i < items.length) out.push(buildList(items, pos, inline));
      continue;
    }

    // Blank line ends a paragraph
    if (line.trim() === '') {
      flushParagraph();
      continue;
    }

    paragraph.push(line);
  }
  flushParagraph();

  let html = out.join('');
  html = restoreCodePlaceholders(html, inline);
  html = html.replace(/\u0000F(\d+)\u0000/g, (_m, i: string) => fences[Number(i)] ?? '');
  return html;
}

/** Pretty-prints tool payloads (JSON) so tool results stay readable. */
export function formatToolOutput(content?: string): string {
  if (!content) return '';
  try {
    const parsed = JSON.parse(content);
    return JSON.stringify(parsed, null, 2);
  } catch {
    return content;
  }
}


const esc = (text: string): string => text.replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[ch]!);
const SYMBOLS: Record<string, string> = {
  times: '×', cdot: '·', div: '÷', pm: '±', leq: '≤', geq: '≥',
  neq: '≠', approx: '≈', sim: '∼', infty: '∞', alpha: 'α',
  beta: 'β', gamma: 'γ', delta: 'δ', Delta: 'Δ', pi: 'π',
  sum: '∑', prod: '∏', to: '→', rightarrow: '→', percent: '%',
  ' ': ' ', ',': ' ', ';': ' ', '!': '', '&': '&',
};
const COMMAND = /^[A-Za-z]+/;

/** Render a deliberately bounded subset of TeX to semantic, escaped HTML. */
export function renderFormula(tex: string, display = false): string {
  if (!tex || tex.length > 2000) return '<span class="md-math-error" role="note">Formula is too long or empty: ' + esc(tex.slice(0, 200)) + '</span>';
  let i = 0;
  let depth = 0;
  let unsupported = false;
  const arg = (): string => {
    while (tex[i] === ' ') i++;
    if (tex[i] === '{') {
      i++;
      const value = parse('}');
      if (tex[i] === '}') i++;
      else unsupported = true;
      return value;
    }
    return parse(undefined, true);
  };
  const parse = (end?: string, single = false): string => {
    if (++depth > 24) { unsupported = true; depth--; return ''; }
    let out = '';
    let consumed = 0;
    while (i < tex.length && tex[i] !== end && consumed++ < 2000) {
      const ch = tex[i++];
      if (ch === '\\') {
        const word = tex.slice(i).match(COMMAND)?.[0];
        if (word) {
          i += word.length;
          if (word === 'frac' || word === 'dfrac' || word === 'tfrac') {
            const top = arg(), bottom = arg();
            out += '<span class="md-frac"><span class="md-num">' + top + '</span><span class="md-den">' + bottom + '</span></span>';
          } else if (word === 'text' || word === 'mathrm' || word === 'operatorname') {
            out += '<span class="md-math-text">' + arg() + '</span>';
          } else if (word === 'sqrt') {
            out += '<span class="md-root">√<span>' + arg() + '</span></span>';
          } else if (word === 'left' || word === 'right') {
            // A sizing modifier; keep the following delimiter visible.
          } else if (Object.prototype.hasOwnProperty.call(SYMBOLS, word)) {
            out += esc(SYMBOLS[word]);
          } else {
            unsupported = true;
            out += esc('\\' + word);
          }
        } else if (i < tex.length) {
          const escaped = tex[i++];
          if (escaped === '\\') out += '<span class="md-math-break"> </span>';
          else out += esc(SYMBOLS[escaped] ?? escaped);
        } else unsupported = true;
      } else if (ch === '^' || ch === '_') {
        out += ch === '^' ? '<sup>' + arg() + '</sup>' : '<sub>' + arg() + '</sub>';
      } else if (ch === '{') {
        out += parse('}');
        if (tex[i] === '}') i++;
        else unsupported = true;
      } else if (ch === '}') {
        unsupported = true;
        out += '}';
      } else if (ch === '\n') {
        out += ' ';
      } else {
        out += esc(ch);
      }
      if (single) break;
    }
    depth--;
    return out;
  };
  const html = parse();
  if (unsupported || i < tex.length) {
    return '<code class="md-math-error" role="note" aria-label="Unsupported mathematical expression">' + esc(tex) + '</code>';
  }
  const label = esc(tex).replace(/\s+/g, ' ');
  return '<span class="' + (display ? 'md-math md-math-display' : 'md-math md-math-inline') +
    '" role="math" aria-label="' + label + '" title="' + label + '" tabindex="' + (display ? '0' : '-1') + '">' + html + '</span>';
}

/** Detects formula-like content without interpreting prices such as "$2.78 bn" as TeX. */
export function isInlineDollarFormula(value: string): boolean {
  return value.length <= 2000 && !/^\s|\s$/.test(value) &&
    /(?:\\[A-Za-z]+|[=^_{}+\-*/])/.test(value) &&
    !/^\d+(?:[.,]\d+)?\s*(?:bn|m|usd|sek|eur)?$/i.test(value);
}
