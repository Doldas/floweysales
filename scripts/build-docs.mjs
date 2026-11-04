#!/usr/bin/env node
// Builds the user guide (docs/guide/*.md) into static pages for the docs site
// and checks every link in them.
//
//   node scripts/build-docs.mjs --out <dir>   write <dir>/guide/*.html
//   node scripts/build-docs.mjs --check       build to a temp dir and check links
//   node scripts/build-docs.mjs --check --external   also fetch every external address (scheduled job, never a merge gate)
//
// Internal links are checked on every run: a link to another guide page, a
// repository file or a heading anchor must resolve, or the build fails. Links
// to other sites are listed but not fetched, so an outside site cannot block a
// merge. Dependency-free: Node only.
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, writeFileSync, cpSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
export const REPO = 'https://github.com/Doldas/floweysales';
export const DOCS_URL = 'https://doldas.github.io/floweysales/';

const esc = s => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

export function slugify(text) {
  return text.toLowerCase().replace(/`/g, '').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'section';
}

/** Splits `---` front matter (flat key: value lines) from the Markdown body. */
export function parseFrontMatter(source) {
  const m = source.match(/^---\n([\s\S]*?)\n---\n?/);
  if (!m) return { meta: {}, body: source };
  const meta = {};
  for (const line of m[1].split('\n')) {
    const kv = line.match(/^([\w-]+):\s*(.*)$/);
    if (kv) meta[kv[1]] = kv[2].trim();
  }
  return { meta, body: source.slice(m[0].length) };
}

/**
 * Resolves a Markdown link found in `fromFile` (a path under docs/guide).
 * Returns { href } for the rendered page, or { error } when it cannot resolve.
 */
export function resolveLink(target, fromFile, { guideFiles, base = root, headings = {} }) {
  if (/^(https?:|mailto:)/.test(target)) return { href: target, external: true };
  const [pathPart, anchor] = target.split('#');
  const frag = anchor ? `#${anchor}` : '';
  if (!pathPart) return { href: frag, anchor, self: true };
  const abs = resolve(dirname(fromFile), pathPart);
  const name = abs.split(sep).pop();
  if (guideFiles.has(abs)) {
    const slug = name.replace(/\.md$/, '');
    if (anchor && headings[slug] && !headings[slug].has(anchor)) return { error: `no heading "#${anchor}" in ${name}` };
    return { href: `${slug}.html${frag}` };
  }
  if (!abs.startsWith(base + sep) || !existsSync(abs)) return { error: `no such file: ${pathPart}` };
  const rel = relative(base, abs).split(sep).join('/');
  const kind = existsSync(abs) && readdirOrNull(abs) ? 'tree' : 'blob';
  return { href: `${REPO}/${kind}/main/${rel}${frag}`, external: true };
}

function readdirOrNull(p) {
  try { return readdirSync(p); } catch { return null; }
}

function inline(text, ctx) {
  const codes = [];
  let out = text.replace(/`([^`]+)`/g, (_m, c) => `\u0000${codes.push(`<code>${esc(c)}</code>`) - 1}\u0000`);
  out = esc(out);
  out = out.replace(/\[([^\]]+)\]\(([^)\s]+)\)/g, (_m, label, target) => {
    const r = resolveLink(target.replace(/&amp;/g, '&'), ctx.file, ctx);
    if (r.error) { ctx.errors.push(`${relative(ctx.base, ctx.file)}: ${r.error}`); return label; }
    ctx.links.push({ file: ctx.file, href: r.href, external: !!r.external, anchor: r.self ? r.anchor : undefined });
    const ext = r.external ? ' rel="noopener noreferrer"' : '';
    return `<a href="${esc(r.href)}"${ext}>${label}</a>`;
  });
  out = out.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  out = out.replace(/(^|[\s(])\*([^*\s][^*]*)\*(?=[\s).,;:]|$)/g, '$1<em>$2</em>');
  return out.replace(/\u0000(\d+)\u0000/g, (_m, i) => codes[+i]);
}

/** Renders the guide's Markdown subset: headings, paragraphs, lists, tables, code, quotes. */
export function renderMarkdown(body, ctx) {
  const lines = body.replace(/\r/g, '').split('\n');
  const html = [];
  const ids = new Map();
  const toc = [];
  let i = 0;
  const para = [];
  const flush = () => { if (para.length) { html.push(`<p>${inline(para.join(' '), ctx)}</p>`); para.length = 0; } };
  while (i < lines.length) {
    const line = lines[i];
    if (/^```/.test(line)) {
      flush();
      const code = [];
      i++;
      while (i < lines.length && !/^```/.test(lines[i])) code.push(lines[i++]);
      i++;
      html.push(`<pre><code>${esc(code.join('\n'))}</code></pre>`);
      continue;
    }
    const h = line.match(/^(#{1,4})\s+(.*)$/);
    if (h) {
      flush();
      const level = h[1].length;
      const plain = h[2].replace(/`/g, '');
      let id = slugify(plain);
      const n = ids.get(id) ?? 0;
      ids.set(id, n + 1);
      if (n) id = `${id}-${n + 1}`;
      if (level >= 2) toc.push({ level, id, text: plain });
      html.push(`<h${level} id="${id}">${inline(h[2], ctx)}</h${level}>`);
      i++;
      continue;
    }
    if (/^\s*$/.test(line)) { flush(); i++; continue; }
    if (/^---+\s*$/.test(line)) { flush(); html.push('<hr>'); i++; continue; }
    if (/^>\s?/.test(line)) {
      flush();
      const q = [];
      while (i < lines.length && /^>\s?/.test(lines[i])) q.push(lines[i++].replace(/^>\s?/, ''));
      html.push(`<blockquote><p>${inline(q.join(' '), ctx)}</p></blockquote>`);
      continue;
    }
    if (/^\|.*\|\s*$/.test(line) && /^\|[\s:|-]+\|\s*$/.test(lines[i + 1] ?? '')) {
      flush();
      const cells = row => row.trim().replace(/^\||\|$/g, '').split('|').map(c => c.trim());
      const head = cells(line);
      i += 2;
      const rows = [];
      while (i < lines.length && /^\|.*\|\s*$/.test(lines[i])) rows.push(cells(lines[i++]));
      html.push(`<div class="table-wrap"><table><thead><tr>${head.map(c => `<th scope="col">${inline(c, ctx)}</th>`).join('')}</tr></thead><tbody>${rows.map(r => `<tr>${r.map(c => `<td>${inline(c, ctx)}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`);
      continue;
    }
    const li = line.match(/^(\s*)([-*]|\d+\.)\s+(.*)$/);
    if (li) {
      flush();
      const ordered = /\d/.test(li[2]);
      const items = [];
      while (i < lines.length) {
        const m = lines[i].match(/^(\s*)([-*]|\d+\.)\s+(.*)$/);
        if (m) { items.push(m[3]); i++; continue; }
        if (/^\s{2,}\S/.test(lines[i]) && items.length) { items[items.length - 1] += ` ${lines[i].trim()}`; i++; continue; }
        break;
      }
      const tag = ordered ? 'ol' : 'ul';
      html.push(`<${tag}>${items.map(t => `<li>${inline(t, ctx)}</li>`).join('')}</${tag}>`);
      continue;
    }
    para.push(line.trim());
    i++;
  }
  flush();
  return { html: html.join('\n'), toc };
}

/** Heading ids of a Markdown body, the way renderMarkdown assigns them. */
export function headingIds(body) {
  const ids = new Map();
  const out = new Set();
  let inCode = false;
  for (const line of body.split('\n')) {
    if (/^```/.test(line)) inCode = !inCode;
    const h = !inCode && line.match(/^(#{1,4})\s+(.*)$/);
    if (!h) continue;
    let id = slugify(h[2].replace(/`/g, ''));
    const n = ids.get(id) ?? 0;
    ids.set(id, n + 1);
    out.add(n ? `${id}-${n + 1}` : id);
  }
  return out;
}

function pageHtml({ meta, slug, content, toc, pages }) {
  const nav = pages.map(p => `<li><a href="${p.slug}.html"${p.slug === slug ? ' aria-current="page"' : ''}>${esc(p.title)}</a></li>`).join('');
  const idx = pages.findIndex(p => p.slug === slug);
  const prev = pages[idx - 1], next = pages[idx + 1];
  const onThis = toc.filter(t => t.level === 2).map(t => `<li><a href="#${t.id}">${esc(t.text)}</a></li>`).join('');
  const reviewed = meta['last-reviewed'] ? `<p class="reviewed">Last reviewed <time datetime="${meta['last-reviewed']}">${meta['last-reviewed']}</time> (UTC)</p>` : '';
  return `<!doctype html>
<html lang="en-US">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="description" content="${esc(meta.summary || 'FloweySales user guide')}">
  <title>${esc(meta.title || slug)} · FloweySales user guide</title>
  <link rel="icon" type="image/svg+xml" href="../icons/icon.svg">
  <link rel="stylesheet" href="../tokens.css">
  <link rel="stylesheet" href="../site.css">
  <script>try{var t=localStorage.getItem('fs.theme');if(t==='light'||t==='dark')document.documentElement.setAttribute('data-theme',t)}catch(e){}</script>
</head>
<body>
  <a class="skip" href="#main">Skip to content</a>
  <header class="topbar wrap">
    <a class="brand" href="../" aria-label="FloweySales home"><img src="../icons/icon.svg" alt="" width="32" height="32"> FloweySales</a>
    <nav aria-label="Main"><a href="./" aria-current="page">User guide</a><a href="../api.html">API explorer</a><a href="${REPO}">GitHub</a><button type="button" id="theme" class="theme-toggle" aria-label="Switch between light and dark">Theme</button></nav>
  </header>
  <div class="wrap guide">
    <nav class="guide-nav" aria-label="User guide">
      <label class="visually-hidden" for="guide-search">Search the guide</label>
      <input id="guide-search" type="search" placeholder="Search the guide…" autocomplete="off">
      <ul id="guide-list">${nav}</ul>
      <p id="guide-empty" role="status" hidden>No page matches.</p>
    </nav>
    <main id="main" class="prose">
      ${content}
      ${reviewed}
      <nav class="pager" aria-label="Next and previous">${prev ? `<a rel="prev" href="${prev.slug}.html">← ${esc(prev.title)}</a>` : '<span></span>'}${next ? `<a rel="next" href="${next.slug}.html">${esc(next.title)} →</a>` : ''}</nav>
    </main>
    ${onThis ? `<aside class="on-this" aria-label="On this page"><strong>On this page</strong><ul>${onThis}</ul></aside>` : ''}
  </div>
  <footer class="wrap"><span>MIT licensed · research aid, not investment advice · all times UTC</span><span><a href="${REPO}/issues">Report a documentation problem</a></span></footer>
  <script src="../guide.js" defer></script>
</body>
</html>
`;
}

/** Builds the guide into `out`. Returns { pages, errors, links }. */
export function buildGuide({ out, base = root }) {
  const dir = join(base, 'docs', 'guide');
  const files = readdirSync(dir).filter(f => f.endsWith('.md')).sort().map(f => join(dir, f));
  const guideFiles = new Set(files);
  const parsed = files.map(file => {
    const { meta, body } = parseFrontMatter(readFileSync(file, 'utf8'));
    return { file, slug: file.split(sep).pop().replace(/\.md$/, ''), meta, body };
  });
  const errors = [];
  for (const p of parsed) {
    if (!p.meta.title) errors.push(`${relative(base, p.file)}: front matter needs a title`);
    if (!p.meta['last-reviewed']) errors.push(`${relative(base, p.file)}: front matter needs last-reviewed`);
  }
  const headings = Object.fromEntries(parsed.map(p => [p.slug, headingIds(p.body)]));
  parsed.sort((a, b) => Number(a.meta.order ?? 99) - Number(b.meta.order ?? 99));
  const pages = parsed.map(p => ({ slug: p.slug, title: p.meta.title ?? p.slug }));
  const links = [];
  const guideOut = join(out, 'guide');
  mkdirSync(guideOut, { recursive: true });
  const search = [];
  for (const p of parsed) {
    const ctx = { file: p.file, guideFiles, base, headings, errors, links };
    const { html, toc } = renderMarkdown(p.body, ctx);
    for (const l of links.filter(l => l.file === p.file && l.anchor !== undefined)) {
      if (!headings[p.slug].has(l.anchor)) errors.push(`${relative(base, p.file)}: no heading "#${l.anchor}" on this page`);
    }
    writeFileSync(join(guideOut, `${p.slug}.html`), pageHtml({ meta: p.meta, slug: p.slug, content: html, toc, pages }));
    search.push({ slug: p.slug, title: p.meta.title, summary: p.meta.summary ?? '', headings: toc.map(t => t.text) });
  }
  if (!parsed.some(p => p.slug === 'index')) errors.push('docs/guide/index.md is missing');
  writeFileSync(join(guideOut, 'search.json'), JSON.stringify(search));
  return { pages, errors, links };
}

/** Checks the site's own pages: every relative href and src must exist. */
export function checkSitePages(siteDir) {
  const errors = [];
  const walk = d => readdirSync(d, { withFileTypes: true }).flatMap(e => (e.isDirectory() ? walk(join(d, e.name)) : [join(d, e.name)]));
  for (const file of walk(siteDir).filter(f => f.endsWith('.html'))) {
    const html = readFileSync(file, 'utf8');
    for (const m of html.matchAll(/(?:href|src)="([^"#]*)(#[^"]*)?"/g)) {
      const target = m[1];
      if (!target || /^(https?:|mailto:|data:)/.test(target)) continue;
      const p = resolve(dirname(file), target);
      if (!existsSync(p) && !(target.endsWith('/') && existsSync(join(p, 'index.html')))) {
        errors.push(`${relative(siteDir, file)}: broken link ${target}`);
      }
    }
    const lang = html.match(/<html[^>]*lang="([^"]+)"/)?.[1];
    if (lang !== 'en-US') errors.push(`${relative(siteDir, file)}: lang must be en-US, found ${lang}`);
  }
  return errors;
}

/** Checks that relative links in the repository's entry documents point at files that exist. */
export function checkRepoLinks(files, base = root) {
  const errors = [];
  for (const rel of files) {
    const file = join(base, rel);
    if (!existsSync(file)) { errors.push(`${rel}: file is missing`); continue; }
    const text = readFileSync(file, 'utf8').replace(/```[\s\S]*?```/g, '');
    for (const m of text.matchAll(/\]\(([^)\s]+)\)/g)) {
      const target = m[1].split('#')[0];
      if (!target || /^(https?:|mailto:|#)/.test(m[1])) continue;
      if (!existsSync(resolve(dirname(file), target))) errors.push(`${rel}: broken link ${m[1]}`);
    }
  }
  return errors;
}

/** Fetches each external address once and returns the ones that did not answer with a success status. */
async function checkExternal(urls) {
  const bad = [];
  for (const url of urls) {
    try {
      const res = await fetch(url, { method: 'GET', redirect: 'follow', signal: AbortSignal.timeout(15000), headers: { 'user-agent': 'floweysales-docs-link-check' } });
      if (!res.ok) bad.push(`${url} (HTTP ${res.status})`);
    } catch (e) {
      bad.push(`${url} (${e.name === 'TimeoutError' ? 'timed out' : e.message})`);
    }
  }
  return bad;
}

async function main() {
  const args = process.argv.slice(2);
  const check = args.includes('--check');
  const outArg = args.indexOf('--out');
  const tmp = check ? mkdtempSync(join(tmpdir(), 'fs-docs-')) : null;
  const out = check ? tmp : resolve(outArg >= 0 ? args[outArg + 1] : join(root, 'site'));
  if (!check && outArg < 0) { console.error('Usage: build-docs.mjs --out <dir> | --check'); process.exit(2); }
  if (out !== join(root, 'site')) cpSync(join(root, 'site'), out, { recursive: true });
  const { pages, errors, links } = buildGuide({ out });
  errors.push(...checkSitePages(out));
  errors.push(...checkRepoLinks(['README.md', 'docs/README.md', 'CONTRIBUTING.md']));
  if (tmp) rmSync(tmp, { recursive: true, force: true });
  const external = new Set(links.filter(l => l.external && l.href.startsWith('http')).map(l => l.href));
  if (errors.length) {
    console.error(`Documentation problems (${errors.length}):\n- ${[...new Set(errors)].join('\n- ')}`);
    process.exit(1);
  }
  if (args.includes('--external')) {
    const bad = await checkExternal(external);
    if (bad.length) {
      console.error(`External links that did not answer (${bad.length} of ${external.size}):\n- ${bad.join('\n- ')}`);
      process.exit(1);
    }
    console.log(`Guide: ${pages.length} pages, ${links.length} links checked; ${external.size} external addresses answered.`);
    return;
  }
  console.log(`Guide: ${pages.length} pages, ${links.length} links checked (${external.size} external addresses not fetched).`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) await main();
