// Pure helpers behind the Community feed: no Angular, so they run under node --test.
import type { ForumPost } from '../models';

export type FeedSort = 'latest' | 'top';

export const TITLE_MAX = 120;
export const BODY_MAX = 4000;
const CLAMP_CHARS = 320;
const CLAMP_LINES = 5;

export interface Topic {
  symbol: string;
  count: number;
}

/** Stable identity: provider posts can share numeric ids with local ones. */
export function postKey(post: Pick<ForumPost, 'id' | 'source'>): string {
  return `${post.source === 'provider' ? 'provider' : 'local'}:${post.id}`;
}

/** Epoch milliseconds from seconds, milliseconds or an ISO string; 0 when unreadable. */
export function postMillis(post: Pick<ForumPost, 'timestamp'>): number {
  const raw = post.timestamp as unknown;
  const n = typeof raw === 'string' && /^\d+(\.\d+)?$/.test(raw) ? Number(raw) : raw;
  if (typeof n === 'number' && Number.isFinite(n) && n > 0) return n < 1e11 ? n * 1000 : n;
  if (typeof raw === 'string') {
    const t = Date.parse(raw);
    if (Number.isFinite(t)) return t;
  }
  return 0;
}

/** Topics sorted by activity, then name; `extra` symbols are listed even without posts. */
export function topicCounts(posts: ForumPost[], extra: string[] = []): Topic[] {
  const counts = new Map<string, number>();
  for (const p of posts) {
    const s = (p.symbol ?? '').trim().toUpperCase();
    if (s) counts.set(s, (counts.get(s) ?? 0) + 1);
  }
  for (const e of extra) {
    const s = e.trim().toUpperCase();
    if (s && !counts.has(s)) counts.set(s, 0);
  }
  return [...counts].map(([symbol, count]) => ({ symbol, count }))
    .sort((a, b) => b.count - a.count || a.symbol.localeCompare(b.symbol));
}

/** Case-insensitive match on title, text, author and symbol; every word must match. */
export function matchesQuery(post: ForumPost, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = `${post.title} ${post.content} ${post.author} ${post.symbol ?? ''}`.toLowerCase();
  return words.every(w => hay.includes(w));
}

export function sortPosts(posts: ForumPost[], sort: FeedSort): ForumPost[] {
  const byTime = (a: ForumPost, b: ForumPost) => postMillis(b) - postMillis(a);
  return [...posts].sort(sort === 'top' ? (a, b) => (b.upvotes - a.upvotes) || byTime(a, b) : byTime);
}

/** Long posts collapse behind "Show more". */
export function isLong(content: string): boolean {
  return content.length > CLAMP_CHARS || content.split('\n').length > CLAMP_LINES;
}

/** First part of a long post, cut at a word boundary. */
export function excerpt(content: string): string {
  if (!isLong(content)) return content;
  const lines = content.split('\n').slice(0, CLAMP_LINES).join('\n');
  const cut = lines.slice(0, CLAMP_CHARS);
  const space = cut.lastIndexOf(' ');
  return (space > CLAMP_CHARS * 0.6 ? cut.slice(0, space) : cut).trimEnd() + '…';
}

/** What is still missing before a post can be published, in reading order. */
export function missingFields(draft: { author: string; title: string; content: string }): string[] {
  const out: string[] = [];
  if (!draft.author.trim()) out.push('your name');
  if (!draft.title.trim()) out.push('a title');
  if (!draft.content.trim()) out.push('your thesis');
  return out;
}

export function initials(author: string): string {
  const parts = author.trim().split(/\s+/).filter(Boolean);
  if (!parts.length) return '?';
  return (parts.length > 1 ? parts[0][0] + parts[1][0] : parts[0].slice(0, 1)).toUpperCase();
}
