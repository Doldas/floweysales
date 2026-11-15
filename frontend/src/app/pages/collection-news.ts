import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';

import type { Collection } from '../core/collections';
import {
  NEWS_FILTERS, RELEVANCE_HELP, RELEVANCE_LABELS, filterNews, newsCoverageText, newsReasons, relevanceCounts, safeNewsUrl,
  type CollectionNews as News, type CollectionNewsItem, type NewsFilter,
} from '../core/collection-news';
import { InstrumentsService } from '../core/instruments.service';
import { describeHttpError } from '../services';
import { formatUtcDateTime } from '../ui/datetime';

/**
 * Headlines for one collection (#441 phase 4), each from the instrument's own
 * provider, with direct, thematic or listed relevance and the reason. The
 * providers are asked only when the user loads the news.
 */
@Component({
  selector: 'fs-collection-news',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<section class="card" aria-labelledby="news-title">
  <div class="card-header news-head">
    <h2 class="card-title me-auto" id="news-title">News</h2>
    @if (news()) {
      <div class="fs-segmented" role="group" aria-label="Show relevance">
        @for (f of filters; track f) {
          <button type="button" [attr.aria-pressed]="filter() === f" (click)="filter.set(f)">{{ labels[f] }} <span class="muted">{{ counts()[f] }}</span></button>
        }
      </div>
    }
    <button type="button" class="btn btn-outline-secondary btn-sm" (click)="load()" [disabled]="loading()">
      <i class="bi bi-newspaper" aria-hidden="true"></i> {{ loading() ? 'Loading…' : news() ? 'Reload news' : 'Load news' }}
    </button>
  </div>
  <div class="card-body">
    <p class="news-note" id="news-note">
      <i class="bi bi-info-circle" aria-hidden="true"></i>
      Headlines from each instrument's own provider. Relevance is a text match with its reason, not a judgment of the story, and nothing here is advice.
    </p>
    @if (error(); as err) {
      <div class="alert alert-warning mb-0" role="alert">{{ err }}</div>
    } @else if (news(); as n) {
      <p class="muted small legend">
        @for (r of relevances; track r) {
          <span><strong>{{ labels[r] }}</strong>: {{ help[r] }}</span>
        }
      </p>
      @if (shown().length) {
        <ul class="list-unstyled news mb-2" aria-describedby="news-note">
          @for (item of shown(); track item.url) {
            <li>
              <div class="news-title">
                @if (link(item); as href) {
                  <a [href]="href" target="_blank" rel="noopener noreferrer">{{ item.title }}</a>
                } @else {
                  <span>{{ item.title }}</span>
                }
                <span class="fs-status no-dot" [class.is-success]="item.relevance === 'direct'" [class.is-info]="item.relevance === 'thematic'">{{ labels[item.relevance] }}</span>
              </div>
              <div class="muted small">
                {{ item.provider }}@if (item.source) { · {{ item.source }} } · {{ item.published ? utc(item.published) : 'Date unknown' }}
              </div>
              <ul class="reasons small" [attr.aria-label]="'Why it is listed: ' + item.title">
                @for (r of reasons(item); track $index) { <li>{{ r }}</li> }
              </ul>
            </li>
          }
        </ul>
      } @else if (n.items.length) {
        <p class="muted">No {{ labels[filter()].toLowerCase() }} headlines. Choose another relevance.</p>
      } @else {
        <p class="muted">No headlines right now.</p>
      }
      <p class="muted small mb-0" data-testid="news-coverage">{{ coverageText() }} Retrieved {{ utc(n.retrieved_at) }}.</p>
      @if (n.coverage.no_source) {
        <p class="muted small mb-0 mt-1">
          <i class="bi bi-slash-circle" aria-hidden="true"></i>
          @if (n.news_providers.length) { News comes from {{ n.news_providers.join(', ') }}. } @else { No installed provider offers news. }
          Instruments on other providers get none: FloweySales never borrows another provider's headlines for the same ticker.
        </p>
      }
    } @else {
      <p class="muted mb-0">Select <strong>Load news</strong> to ask the instruments' providers for recent headlines.</p>
    }
  </div>
</section>
  `,
  styles: [`
    :host { display: block; min-width: 0; }
    .news-head { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; }
    .news-note { font-size: 0.8rem; color: var(--fs-muted); margin: 0 0 0.6rem; }
    .legend { display: flex; flex-wrap: wrap; gap: 0.2rem 1rem; margin: 0 0 0.6rem; }
    .news { display: grid; gap: 0.8rem; }
    .news > li { border-top: 1px solid var(--fs-border); padding-top: 0.6rem; min-width: 0; }
    .news > li:first-child { border-top: 0; padding-top: 0; }
    .news-title { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.2rem 0.6rem; font-weight: 600; overflow-wrap: anywhere; }
    .news-title a { display: inline-block; min-height: 24px; }
    .reasons { margin: 0.2rem 0 0; padding-left: 1.1rem; color: var(--fs-text-2); overflow-wrap: anywhere; }
  `],
})
export class CollectionNews {
  private readonly api = inject(InstrumentsService);
  private readonly destroyRef = inject(DestroyRef);

  readonly collection = input.required<Collection>();

  readonly filters = NEWS_FILTERS;
  readonly relevances = ['direct', 'thematic', 'listed'] as const;
  readonly labels = RELEVANCE_LABELS;
  readonly help = RELEVANCE_HELP;

  readonly news = signal<News | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  readonly filter = signal<NewsFilter>('all');

  readonly counts = computed(() => relevanceCounts(this.news()?.items ?? []));
  readonly shown = computed(() => filterNews(this.news()?.items ?? [], this.filter()));
  readonly coverageText = computed(() => newsCoverageText(this.news()));

  private shownFor = 0;

  constructor() {
    // Another collection starts empty: its providers are asked only on request.
    effect(() => {
      const id = this.collection().id;
      untracked(() => {
        if (id === this.shownFor) return;
        this.shownFor = id;
        this.news.set(null);
        this.error.set('');
        this.filter.set('all');
      });
    });
  }

  utc(value: string | number): string { return formatUtcDateTime(typeof value === 'number' ? value * 1000 : value); }
  link(item: CollectionNewsItem): string { return safeNewsUrl(item.url); }
  reasons(item: CollectionNewsItem): string[] { return newsReasons(item); }

  load(): void {
    const id = this.collection().id;
    this.loading.set(true);
    this.api.collectionNews(id).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: n => {
        this.loading.set(false);
        if (this.collection().id !== id) return;
        this.error.set('');
        this.news.set(n);
      },
      error: err => { this.loading.set(false); this.error.set(describeHttpError(err)); },
    });
  }
}
