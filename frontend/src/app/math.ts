/**
 * Small, dependency-free, strictly escaped TeX subset for AI research reports.
 * This is display-only: it NEVER evaluates equations or modifies financial data.
 * Unknown commands render as escaped source instead of pretending to typeset.
 */
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
