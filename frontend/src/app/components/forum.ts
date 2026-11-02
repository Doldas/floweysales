import {
  ChangeDetectionStrategy,
  Component,
  computed,
  effect,
  ElementRef,
  inject,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { DatePipe } from '@angular/common';
import { toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { ActivatedRoute, Router } from '@angular/router';
import { map } from 'rxjs';

import { MarketService, describeHttpError } from '../services';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import { ForumPost } from '../models';
import { timeAgo } from '../ui/format';
import {
  BODY_MAX,
  TITLE_MAX,
  excerpt,
  initials,
  isLong,
  matchesQuery,
  missingFields,
  postKey,
  postMillis,
  sortPosts,
  topicCounts,
  type FeedSort,
} from './community-feed';

const FETCH_LIMIT = 200;
const AUTHOR_KEY = 'fs.community.author';
const VOTED_KEY = 'fs.community.voted';
const DRAFT_KEY = 'fs.community.draft';

function readStorage(key: string): string {
  try { return localStorage.getItem(key) ?? ''; } catch { return ''; }
}
function writeStorage(key: string, value: string): void {
  try { value ? localStorage.setItem(key, value) : localStorage.removeItem(key); } catch { /* private mode */ }
}

/**
 * The Community feed. The topic being read is explicit and lives in the URL
 * (`/community?topic=NVDA`), so it can be shared and survives a reload; it is
 * deliberately not tied to the market selected elsewhere in the app.
 */
@Component({
  selector: 'fse-forum',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DatePipe, FormsModule],
  templateUrl: './forum.html',
  styleUrl: './forum.scss',
})
export class ForumComponent {
  private readonly api = inject(MarketService);
  private readonly toasts = inject(NotifyService);
  private readonly router = inject(Router);
  private readonly market = inject(MarketState);

  protected readonly titleMax = TITLE_MAX;
  protected readonly bodyMax = BODY_MAX;
  protected readonly timeAgo = timeAgo;
  protected readonly initials = initials;
  protected readonly excerpt = excerpt;
  protected readonly isLong = isLong;
  protected readonly postKey = postKey;
  protected readonly postMillis = postMillis;

  private readonly authorField = viewChild<ElementRef<HTMLInputElement>>('authorField');
  private readonly titleField = viewChild<ElementRef<HTMLInputElement>>('titleField');
  private readonly bodyField = viewChild<ElementRef<HTMLTextAreaElement>>('bodyField');

  /** Everything the community has said: feeds the topic list and its counts. */
  readonly allPosts = signal<ForumPost[]>([]);
  /** Posts for the selected topic (includes provider forums when a topic is set). */
  readonly topicPosts = signal<ForumPost[]>([]);
  readonly loading = signal(true);
  readonly failed = signal('');

  readonly topic = toSignal(
    inject(ActivatedRoute).queryParamMap.pipe(map(p => (p.get('topic') ?? '').trim().toUpperCase())),
    { initialValue: '' },
  );
  readonly query = signal('');
  readonly sort = signal<FeedSort>('latest');
  readonly expanded = signal<ReadonlySet<string>>(new Set());
  readonly voted = signal<ReadonlySet<string>>(this.loadVoted());
  readonly voting = signal<ReadonlySet<string>>(new Set());

  readonly composerOpen = signal(false);
  readonly publishing = signal(false);
  author = readStorage(AUTHOR_KEY);
  title = '';
  content = '';
  composeTopic = '';

  /** Symbols of the markets being tracked: always offered as topics. */
  readonly watched = computed(() => this.market.trackedAssets().map(a => a.symbol));
  readonly topics = computed(() => {
    const extra = [...this.watched()];
    if (this.topic()) extra.push(this.topic());
    return topicCounts(this.allPosts(), extra);
  });
  readonly trackedSymbol = computed(() => this.market.trackedSymbol().toUpperCase());

  readonly visible = computed(() => {
    const q = this.query();
    const base = this.topic() ? this.topicPosts() : this.allPosts();
    return sortPosts(base.filter(p => matchesQuery(p, q)), this.sort());
  });
  readonly base = computed(() => (this.topic() ? this.topicPosts() : this.allPosts()));
  readonly missing = computed(() => {
    this.formVersion();
    return missingFields({ author: this.author, title: this.title, content: this.content });
  });
  private readonly formVersion = signal(0);

  private requestId = 0;

  constructor() {
    const draft = this.loadDraft();
    if (draft) {
      this.title = draft.title;
      this.content = draft.content;
      this.composeTopic = draft.topic;
      this.composerOpen.set(!!(draft.title || draft.content));
    }
    effect(() => {
      const topic = this.topic();
      untracked(() => this.loadTopic(topic));
    });
    this.loadAll();
  }

  // ------------------------------------------------------------ navigation

  selectTopic(symbol: string): void {
    void this.router.navigate([], {
      queryParams: { topic: symbol || null },
      queryParamsHandling: 'merge',
    });
  }

  clearFilters(): void {
    this.query.set('');
    this.selectTopic('');
  }

  // ------------------------------------------------------------ loading

  reload(): void {
    this.loadAll();
    this.loadTopic(this.topic());
  }

  private loadAll(): void {
    this.api.getForum(undefined, FETCH_LIMIT).subscribe({
      next: posts => { this.allPosts.set(posts ?? []); this.failed.set(''); if (!this.topic()) this.loading.set(false); },
      error: err => { this.failed.set(describeHttpError(err)); this.loading.set(false); },
    });
  }

  private loadTopic(topic: string): void {
    const id = ++this.requestId;
    if (!topic) { this.topicPosts.set([]); return; }
    this.loading.set(true);
    this.api.getForum(topic, FETCH_LIMIT).subscribe({
      next: posts => {
        if (id !== this.requestId) return;
        this.topicPosts.set(posts ?? []);
        this.failed.set('');
        this.loading.set(false);
      },
      error: err => {
        if (id !== this.requestId) return;
        this.failed.set(describeHttpError(err));
        this.loading.set(false);
      },
    });
  }

  // ------------------------------------------------------------ composing

  /** Opens the composer, aimed at `topic` (default: the topic being read, unless a draft already has one). */
  openComposer(topic?: string): void {
    if (topic !== undefined) this.composeTopic = topic;
    else if (!this.composeTopic) this.composeTopic = this.topic();
    this.composerOpen.set(true);
    setTimeout(() => (this.author ? this.titleField() : this.authorField())?.nativeElement.focus());
  }

  discard(): void {
    if ((this.title || this.content) && !confirm('Discard this draft?')) return;
    this.title = '';
    this.content = '';
    this.composeTopic = '';
    this.saveDraft();
    this.composerOpen.set(false);
  }

  edited(): void {
    this.formVersion.update(v => v + 1);
    this.saveDraft();
  }

  onComposeKey(event: KeyboardEvent): void {
    if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      this.submit();
    } else if (event.key === 'Escape' && !this.title && !this.content) {
      this.composerOpen.set(false);
    }
  }

  submit(): void {
    if (this.publishing()) return;
    if (this.missing().length) {
      const field = !this.author.trim() ? this.authorField() : !this.title.trim() ? this.titleField() : this.bodyField();
      field?.nativeElement.focus();
      return;
    }
    const symbol = this.composeTopic.trim().toUpperCase();
    this.publishing.set(true);
    this.api.createForumPost({ author: this.author.trim(), title: this.title.trim(), content: this.content.trim(), symbol }).subscribe({
      next: () => {
        writeStorage(AUTHOR_KEY, this.author.trim());
        this.title = '';
        this.content = '';
        this.saveDraft();
        this.publishing.set(false);
        this.composerOpen.set(false);
        this.toasts.success(symbol ? `Published to ${symbol}.` : 'Published to the community.');
        this.query.set('');
        // Show the post where the reader will look for it.
        if (symbol && symbol !== this.topic()) this.selectTopic(symbol);
        else this.reload();
        this.sort.set('latest');
      },
      error: err => {
        this.publishing.set(false);
        this.toasts.error('Could not publish: ' + describeHttpError(err));
      },
    });
  }

  // ------------------------------------------------------------ reading

  toggleExpanded(post: ForumPost): void {
    const key = postKey(post);
    this.expanded.update(set => {
      const next = new Set(set);
      next.has(key) ? next.delete(key) : next.add(key);
      return next;
    });
  }

  isExpanded(post: ForumPost): boolean {
    return this.expanded().has(postKey(post));
  }

  hasVoted(post: ForumPost): boolean {
    return this.voted().has(postKey(post));
  }

  /** Toggles this browser's upvote: +1 the first time, -1 to take it back. */
  vote(post: ForumPost): void {
    const key = postKey(post);
    if (post.source === 'provider' || this.voting().has(key)) return;
    const had = this.voted().has(key);
    const delta = had ? -1 : 1;
    this.setVote(post, key, !had, delta);
    this.voting.update(s => new Set(s).add(key));
    this.api.votePost(post.id, delta).subscribe({
      next: () => this.voting.update(s => { const n = new Set(s); n.delete(key); return n; }),
      error: err => {
        this.setVote(post, key, had, -delta);
        this.voting.update(s => { const n = new Set(s); n.delete(key); return n; });
        this.toasts.error('Vote failed: ' + describeHttpError(err));
      },
    });
  }

  private setVote(post: ForumPost, key: string, voted: boolean, delta: number): void {
    const bump = (list: ForumPost[]) => list.map(p => (postKey(p) === key ? { ...p, upvotes: Math.max(0, p.upvotes + delta) } : p));
    this.allPosts.update(bump);
    this.topicPosts.update(bump);
    this.voted.update(set => {
      const next = new Set(set);
      voted ? next.add(key) : next.delete(key);
      writeStorage(VOTED_KEY, JSON.stringify([...next]));
      return next;
    });
  }

  // ------------------------------------------------------------ persistence

  private loadVoted(): ReadonlySet<string> {
    try {
      const raw = JSON.parse(readStorage(VOTED_KEY) || '[]');
      return new Set(Array.isArray(raw) ? raw.filter((k): k is string => typeof k === 'string') : []);
    } catch { return new Set(); }
  }

  private loadDraft(): { title: string; content: string; topic: string } | null {
    try {
      const raw = JSON.parse(readStorage(DRAFT_KEY) || 'null');
      if (!raw || typeof raw !== 'object') return null;
      return { title: String(raw.title ?? ''), content: String(raw.content ?? ''), topic: String(raw.topic ?? '') };
    } catch { return null; }
  }

  private saveDraft(): void {
    const empty = !this.title && !this.content;
    writeStorage(DRAFT_KEY, empty ? '' : JSON.stringify({ title: this.title, content: this.content, topic: this.composeTopic }));
  }
}
