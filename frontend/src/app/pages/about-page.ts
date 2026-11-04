import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import type { AboutInfo } from '../models';
import { DOCS_URL, REPO_URL } from '../help/help-topics';
import { MarketService } from '../services';
import { formatUtcDateTime } from '../ui/datetime';

/** What FloweySales is, which build this is, and where the project lives. */
@Component({
  selector: 'fs-about-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<div class="fs-page">
  <header class="fs-page-header">
    <div class="d-flex align-items-center gap-3">
      <img src="icons/icon.svg" alt="" width="56" height="56" class="about-icon">
      <div>
        <div class="fs-eyebrow">About</div>
        <h1>FloweySales</h1>
        <p>An agentic finance workspace: market data, an AI research agent, strategies and automations in one place.</p>
      </div>
    </div>
  </header>

  <div class="row g-3">
    <section class="col-lg-6" aria-labelledby="about-what">
      <div class="card h-100"><div class="card-body">
        <h2 id="about-what" class="h5">What it is for</h2>
        <ul class="mb-3">
          <li>Research companies with real market data and an agent that shows its tool calls.</li>
          <li>Compare ideas with a transparent leaderboard and backtests.</li>
          <li>Automate repeatable research as flows, with approvals for anything that needs a person.</li>
        </ul>
        <p class="mb-0 text-muted">FloweySales is a research aid, not investment advice. It does not place real orders.</p>
      </div></div>
    </section>

    <section class="col-lg-6" aria-labelledby="about-build">
      <div class="card h-100"><div class="card-body">
        <h2 id="about-build" class="h5">This build</h2>
        @if (info(); as i) {
          <dl class="about-dl">
            <dt>Version</dt>
            <dd>@if (i.development) { Development build } @else { <code>{{ i.commit }}</code> }</dd>
            @if (i.build_time) { <dt>Built</dt><dd>{{ built(i.build_time) }}</dd> }
            <dt>License</dt><dd>{{ i.license }} · <a [href]="repo + '/blob/main/LICENSE'" target="_blank" rel="noopener noreferrer">read the license</a></dd>
          </dl>
        } @else if (failed()) {
          <p class="text-muted mb-0" role="status">Build information is not available.</p>
        } @else {
          <p class="text-muted mb-0" role="status">Loading…</p>
        }
      </div></div>
    </section>

    <section class="col-12" aria-labelledby="about-links">
      <div class="card"><div class="card-body">
        <h2 id="about-links" class="h5">Project links</h2>
        <ul class="about-links">
          <li><a [href]="docs" target="_blank" rel="noopener noreferrer"><i class="bi bi-book" aria-hidden="true"></i>Documentation</a></li>
          <li><a [href]="repo" target="_blank" rel="noopener noreferrer"><i class="bi bi-github" aria-hidden="true"></i>GitHub repository</a></li>
          <li><a [href]="repo + '/issues'" target="_blank" rel="noopener noreferrer"><i class="bi bi-bug" aria-hidden="true"></i>Report an issue or request a feature</a></li>
          <li><a [href]="repo + '/releases'" target="_blank" rel="noopener noreferrer"><i class="bi bi-tag" aria-hidden="true"></i>Releases</a></li>
        </ul>
        <p class="mb-0 text-muted">Contributions are welcome under the MIT license; see CONTRIBUTING.md in the repository.</p>
      </div></div>
    </section>
  </div>
</div>
  `,
  styles: [`
    .about-icon { border-radius: 14px; }
    .about-dl { display: grid; grid-template-columns: auto 1fr; gap: .4rem 1rem; margin: 0; }
    .about-dl dt { color: var(--fs-muted); font-weight: 500; }
    .about-dl dd { margin: 0; }
    .about-links { list-style: none; padding: 0; margin: 0 0 .75rem; display: grid; gap: .4rem; }
    .about-links i { margin-right: .5rem; }
  `],
})
export class AboutPage {
  private readonly api = inject(MarketService);
  readonly info = signal<AboutInfo | null>(null);
  readonly failed = signal(false);
  readonly docs = DOCS_URL;
  readonly repo = REPO_URL;

  constructor() {
    this.api.getAbout().subscribe({ next: i => this.info.set(i), error: () => this.failed.set(true) });
  }

  built(value: string): string { return formatUtcDateTime(value); }
}
