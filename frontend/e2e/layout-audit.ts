import type { Page } from '@playwright/test';

/**
 * Layout checks shared by the responsive audit (#257) and page-level phone
 * tests: horizontal overflow, content that sticks out of the viewport
 * without being inside a scroll container, cut-off text and, on touch-size
 * screens, targets under 24 px.
 */
export interface Finding { kind: string; target: string; detail: string }

/** Collects layout defects of the current page. Runs in the page. */
export async function audit(page: Page, opts: { touch: boolean }): Promise<Finding[]> {
  return page.evaluate(({ touch }) => {
    const out: { kind: string; target: string; detail: string }[] = [];
    const vw = document.documentElement.clientWidth;
    const name = (e: Element) => {
      const id = e.id ? `#${e.id}` : '';
      const cls = typeof (e as HTMLElement).className === 'string' ? (e as HTMLElement).className.trim().split(/\s+/).slice(0, 2).join('.') : '';
      return `${e.tagName.toLowerCase()}${id}${cls ? '.' + cls : ''}`;
    };
    const visible = (e: Element) => {
      const r = e.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) return false;
      const s = getComputedStyle(e);
      return s.visibility !== 'hidden' && s.display !== 'none' && s.opacity !== '0';
    };
    /** True when an ancestor clips or scrolls its overflow, so a wide child is not a page defect. */
    const contained = (e: Element) => {
      for (let p = e.parentElement; p && p !== document.body; p = p.parentElement) {
        const s = getComputedStyle(p);
        if (/(auto|scroll|hidden|clip)/.test(s.overflowX)) { const r = p.getBoundingClientRect(); if (r.right <= vw + 1) return true; }
        if (s.position === 'fixed') return false;
      }
      return false;
    };
    const overflow = document.documentElement.scrollWidth - vw;
    if (overflow > 1) out.push({ kind: 'overflow', target: 'document', detail: `${overflow}px wider than the viewport` });
    const seen = new Set<string>();
    for (const e of Array.from(document.querySelectorAll('#main-content *'))) {
      if (!visible(e)) continue;
      const r = e.getBoundingClientRect();
      if ((r.right > vw + 1 || r.left < -1) && !contained(e)) {
        const key = name(e);
        if (!seen.has('o' + key)) { seen.add('o' + key); out.push({ kind: 'sticks-out', target: key, detail: `left ${Math.round(r.left)} right ${Math.round(r.right)} of ${vw}` }); }
      }
      const s = getComputedStyle(e);
      // Text cut off without an ellipsis or a way to read it.
      if (!e.closest('.visually-hidden') && e.children.length === 0 && e.textContent!.trim() && /(hidden|clip)/.test(s.overflowX) && s.textOverflow === 'clip' && e.scrollWidth > e.clientWidth + 1 && s.whiteSpace !== 'normal') {
        const key = name(e);
        if (!seen.has('c' + key)) { seen.add('c' + key); out.push({ kind: 'clipped-text', target: key, detail: `"${e.textContent!.trim().slice(0, 30)}" ${e.scrollWidth}>${e.clientWidth}` }); }
      }
    }
    if (touch) {
      const sel = 'button, a[href], [role=button], [role=tab], input:not([type=hidden]), select, textarea, [role=menuitem]';
      for (const e of Array.from(document.querySelectorAll(sel))) {
        if (!visible(e)) continue;
        if ((e as HTMLInputElement).type === 'checkbox' || (e as HTMLInputElement).type === 'radio') continue;
        const r = e.getBoundingClientRect();
        if (r.bottom < 0) continue;
        // Inline links inside a sentence are exempt (WCAG 2.5.8).
        const inSentence = Array.from(e.parentElement?.childNodes ?? []).some(n => n.nodeType === 3 && n.textContent!.trim().length > 3);
        if (inSentence && getComputedStyle(e).display.startsWith('inline')) continue;
        if (r.width < 24 || r.height < 24) {
          const key = name(e) + (e.getAttribute('aria-label') ? `[${e.getAttribute('aria-label')}]` : '');
          if (!seen.has('t' + key)) { seen.add('t' + key); out.push({ kind: 'touch-target', target: key, detail: `${Math.round(r.width)}x${Math.round(r.height)}` }); }
        }
      }
    }
    return out;
  }, opts);
}
