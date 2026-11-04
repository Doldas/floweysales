import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { ActivatedRoute, Router, RouterLink } from '@angular/router';
import { map } from 'rxjs';

import { DOCS_URL, HELP_TOPICS, searchHelp, topicById } from '../help/help-topics';
import { renderMarkdown } from '../markdown';

/** In-app help: searchable articles, the glossary and a link to the documentation site. */
@Component({
  selector: 'fs-help-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
<div class="fs-page">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">Help</div>
      <h1>Help</h1>
      <p>Written documentation for FloweySales. Answers from the Research Agent are AI-generated and separate from this.</p>
    </div>
    <a class="btn btn-outline-secondary" [href]="docsUrl" target="_blank" rel="noopener noreferrer">
      <i class="bi bi-box-arrow-up-right" aria-hidden="true"></i>Full documentation
    </a>
  </header>

  <div class="help-layout">
    <nav class="help-nav" aria-label="Help topics">
      <label class="visually-hidden" for="help-search">Search help</label>
      <input id="help-search" type="search" class="form-control" placeholder="Search help…" autocomplete="off"
             [value]="query()" (input)="query.set($any($event.target).value)">
      <ul>
        @for (t of results(); track t.id) {
          <li>
            <a [routerLink]="[]" [queryParams]="{ topic: t.id }" [class.active]="t.id === current().id"
               [attr.aria-current]="t.id === current().id ? 'page' : null">
              <strong>{{ t.title }}</strong><small>{{ t.summary }}</small>
            </a>
          </li>
        } @empty {
          <li class="empty" role="status">No help article matches “{{ query() }}”. Try the <a [href]="docsUrl" target="_blank" rel="noopener noreferrer">documentation site</a>.</li>
        }
      </ul>
    </nav>

    <article class="help-article card" aria-labelledby="help-title">
      <div class="card-body">
        <div class="fs-eyebrow">{{ current().area }}</div>
        <h2 id="help-title">{{ current().title }}</h2>
        <div class="chat-markdown" [innerHTML]="html()"></div>
      </div>
    </article>
  </div>
</div>
  `,
  styles: [`
    .help-layout { display: grid; grid-template-columns: minmax(14rem, 20rem) 1fr; gap: 1rem; align-items: start; }
    .help-nav ul { list-style: none; margin: .75rem 0 0; padding: 0; display: grid; gap: .25rem; }
    .help-nav a { display: grid; gap: .1rem; padding: .5rem .65rem; border-radius: var(--bs-border-radius); color: var(--fs-text); text-decoration: none; border: 1px solid transparent; }
    .help-nav a:hover { background: var(--fs-surface-2); }
    .help-nav a.active { background: var(--fs-accent-soft); border-color: var(--fs-accent); }
    .help-nav small { color: var(--fs-muted); }
    .help-nav .empty { color: var(--fs-muted); padding: .5rem; }
    .help-article h2 { font-size: 1.25rem; margin-bottom: .75rem; }
    @media (max-width: 767.98px) { .help-layout { grid-template-columns: 1fr; } }
  `],
})
export class HelpPage {
  private readonly route = inject(ActivatedRoute);
  readonly docsUrl = DOCS_URL;
  readonly query = signal('');
  private readonly topicId = toSignal(this.route.queryParamMap.pipe(map(p => p.get('topic'))), { initialValue: null });
  readonly results = computed(() => searchHelp(this.query()));
  readonly current = computed(() => topicById(this.topicId()) ?? HELP_TOPICS[0]);
  readonly html = computed(() => renderMarkdown(this.current().body));
}
