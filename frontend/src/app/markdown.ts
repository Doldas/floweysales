/**
 * A tiny, dependency-free Markdown renderer for chat bubbles.
 *
 * Every piece of the source is HTML-escaped before any formatting is applied,
 * so model/user output can never inject markup or script. The renderer only
 * ever emits tags it generates itself and only allows http(s) links.
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

interface InlineState {
  code: string[];
}

/** Renders inline constructs: code, links, bold, italic, strike, underline. */
function renderInline(text: string, state: InlineState): string {
  // Pull `code` spans out first so emphasis rules never touch them.
  let out = text.replace(/`([^`]+)`/g, (_m, code: string) => {
    const idx = state.code.push(code) - 1;
    return `\u0000C${idx}\u0000`;
  });

  out = out.replace(
    /\[([^\]]+)\]\((https?:\/\/[^\s)]+|\/[^\s)]*)\)/g,
    '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>',
  );

  out = out.replace(/\*\*\*([^*]+)\*\*\*/g, '<strong><em>$1</em></strong>');
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  // Underscore emphasis only at word boundaries, so identifiers such as get_sec_facts stay intact.
  out = out.replace(/(^|[^\w])__([^_\n]+)__(?!\w)/g, '$1<strong>$2</strong>');
  out = out.replace(/~~([^~]+)~~/g, '<del>$1</del>');
  out = out.replace(/\*([^*\n]+)\*/g, '<em>$1</em>');
  out = out.replace(/(^|[^\w])_([^_\n]+)_(?!\w)/g, '$1<em>$2</em>');

  out = out.replace(/\u0000C(\d+)\u0000/g, (_m, i: string) => {
    return `<code>${state.code[Number(i)] ?? ''}</code>`;
  });

  return out;
}

function restoreCodePlaceholders(html: string, state: InlineState): string {
  return html.replace(/\u0000C(\d+)\u0000/g, (_m, i: string) => `<code>${state.code[Number(i)] ?? ''}</code>`);
}

const FENCE = /^```([A-Za-z0-9+#._-]*)\s*$/;

export function renderMarkdown(source: string): string {
  if (!source) return '';

  const escaped = escapeHtml(source);
  const fences: string[] = [];

  const lines = escaped.split('\n');
  const out: string[] = [];
  const inline: InlineState = { code: [] };
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
      const lang = fenceMatch[1] || 'code';
      const body: string[] = [];
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) {
        body.push(lines[i]);
        i++;
      }
      const idx = fences.push(
        `<div class="md-code"><div class="md-code-head"><span class="md-lang">${lang}</span>` +
          `<button type="button" class="md-copy" data-md-copy>copy</button></div>` +
          `<pre><code>${body.join('\n')}</code></pre></div>`,
      ) - 1;
      out.push(`\u0000F${idx}\u0000`);
      continue;
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

    // Unordered list
    if (/^\s*[-*+]\s+/.test(line)) {
      flushParagraph();
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*+]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*+]\s+/, ''));
        i++;
      }
      i--;
      out.push(`<ul>${items.map((it) => `<li>${renderInline(it, inline)}</li>`).join('')}</ul>`);
      continue;
    }

    // Ordered list
    if (/^\s*\d+[.)]\s+/.test(line)) {
      flushParagraph();
      const items: string[] = [];
      while (i < lines.length && /^\s*\d+[.)]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*\d+[.)]\s+/, ''));
        i++;
      }
      i--;
      out.push(`<ol>${items.map((it) => `<li>${renderInline(it, inline)}</li>`).join('')}</ol>`);
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
