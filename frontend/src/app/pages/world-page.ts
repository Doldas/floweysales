import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';

import { ROLE_ICON, ROLE_LABEL, publishedLabel, roleLine, themeLabels, worldPrompt } from '../components/world-view';
import { WorldImpactComponent } from '../components/world-impact';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import type { WorldHeadline, WorldTheme, WorldView } from '../models';
import { MarketService } from '../services';
import { SymbolSearchComponent } from '../shell/symbol-search';
import { SourceComponent } from '../ui/source';
import { SkeletonComponent } from '../ui/skeleton';
import { ProgressStatusComponent } from '../ui/progress-status';

/** What is happening in the world, and which sectors and commodities it touches. */
@Component({
  selector: 'fs-world-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink, SymbolSearchComponent, WorldImpactComponent, SourceComponent, SkeletonComponent, ProgressStatusComponent],
  template: `
    <div class="fs-page research-container">
      <header class="fs-page-header">
        <div>
          <div class="fs-eyebrow">Outlook</div>
          <h1>World</h1>
          <p>Current headlines sorted into themes, with the sectors and commodities each theme tends to move. Research, not advice.</p>
        </div>
        <div class="actions">
          <button type="button" class="btn btn-outline-secondary" [disabled]="loading()" (click)="load(true)"><i class="bi bi-arrow-clockwise"></i> Refresh</button>
          @if (world(); as w) { @if (w.themes.length) { <button type="button" class="btn btn-outline-secondary" (click)="agent.ask(prompt(w))"><i class="bi bi-stars"></i> Summarize</button> } }
        </div>
      </header>

      @if (loading() && !world()) {
        <div [attr.aria-busy]="true">
          <fs-progress-status stage="Reading the news" />
          <fs-skeleton shape="rows" [rows]="4" aria-hidden="true" />
        </div>
      } @else if (failed()) {
        <div class="fs-empty"><i class="bi bi-globe2"></i><h3>The world view could not load</h3><p class="mb-0">Try again in a moment.</p></div>
      } @else if (world(); as w) {
        <p class="meta">
          As of {{ w.generated_at }} · from {{ w.sources.join(' and ') || 'no source' }}
          @if (loading()) { · refreshing… }
        </p>
        @for (n of w.notes ?? []; track n) { <p class="note">{{ n }}</p> }

        @if (w.themes.length) {
          <section class="card sectors" aria-label="Sectors under the active themes">
            <h2>Sectors</h2>
            <table>
              <thead><tr><th>Sector</th><th>Tends to gain from</th><th>Tends to lose from</th><th>Cuts both ways</th></tr></thead>
              <tbody>
                @for (s of w.sectors; track s.name) {
                  <tr>
                    <th scope="row">{{ s.name }}</th>
                    <td class="pos">{{ labels(w, s.benefits_from) }}</td>
                    <td class="neg">{{ labels(w, s.hurt_by) }}</td>
                    <td>{{ labels(w, s.mixed) }}</td>
                  </tr>
                }
              </tbody>
            </table>
          </section>

          <div class="themes">
            @for (t of w.themes; track t.id) {
              <article class="card theme">
                <h2>{{ t.label }} <small>{{ t.count }} headlines</small></h2>
                <ul class="headlines">
                  @for (h of t.headlines; track h.url) {
                    <li><a [href]="h.url" target="_blank" rel="noopener noreferrer">{{ h.title }}</a> <small>{{ h.source }} · {{ published(h) }}</small> <fs-source [fact]="h.provenance" /></li>
                  }
                </ul>
                <h3>Who is exposed when {{ t.driver }} rise</h3>
                <ul class="exposures">
                  @for (x of t.exposures; track x.kind + x.name) {
                    <li [title]="x.why"><span [class]="'role ' + x.role">{{ icon(x.role) }} {{ roleLabel(x.role) }}</span> <strong>{{ x.name }}</strong> <small>{{ x.kind }} · {{ x.why }}</small></li>
                  }
                </ul>
              </article>
            }
          </div>
        } @else {
          <div class="fs-empty"><i class="bi bi-globe2"></i><h3>No headlines yet</h3>
            <p class="mb-0">Install a provider plugin with the news capability, or save a web search key under <a routerLink="/settings">Settings → Integrations</a>.</p></div>
        }

        <h2 class="company-title">Check a company</h2>
        <fs-symbol-search class="page-search" [navigate]="false" placeholder="Pick a company to see how this reaches it…" />
        @if (market.trackedSymbol()) { <fs-world-impact [symbol]="market.trackedSymbol()" /> }
      }
    </div>
  `,
  styles: [`
.actions { display: flex; gap: 0.5rem; flex-wrap: wrap; }
.meta, .muted, small { color: var(--fs-muted); font-size: 0.8rem; }
.note { color: var(--fs-warn); font-size: 0.85rem; }
.card { padding: 1rem 1.25rem; margin-bottom: 1rem; }
h2 { font-size: 1.05rem; margin: 0 0 0.5rem; }
h3 { font-size: 0.85rem; color: var(--fs-muted); margin: 0.9rem 0 0.4rem; font-weight: 600; }
.sectors { overflow-x: auto; }
table { width: 100%; border-collapse: collapse; font-size: 0.85rem; min-width: 36rem; }
th, td { padding: 0.4rem 0.6rem; border-top: 1px solid var(--fs-border); text-align: left; vertical-align: top; }
thead th { border-top: 0; color: var(--fs-muted); font-weight: 600; font-size: 0.75rem; }
.pos { color: var(--fs-pos); } .neg { color: var(--fs-neg); }
.themes { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 26rem), 1fr)); gap: 1rem; align-items: start; }
.theme { margin-bottom: 0; }
ul { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.35rem; }
li { font-size: 0.88rem; }
.role { font-size: 0.7rem; font-weight: 600; padding: 0.05rem 0.45rem; border-radius: 999px; white-space: nowrap; margin-right: 0.3rem; }
.role.benefits { background: var(--fs-pos-soft); color: var(--fs-pos); }
.role.hurt { background: var(--fs-neg-soft); color: var(--fs-neg); }
.role.mixed { background: var(--fs-warn-soft); color: var(--fs-warn); }
.company-title { margin-top: 1.5rem; }
.page-search { display: block; max-width: 380px; margin-bottom: 1rem; }
  `],
})
export class WorldPage {
  readonly market = inject(MarketState);
  readonly agent = inject(AgentSession);
  private readonly api = inject(MarketService);

  readonly world = signal<WorldView | null>(null);
  readonly loading = signal(false);
  readonly failed = signal(false);
  private readonly now = Date.now();

  readonly icon = (role: WorldTheme['exposures'][number]['role']) => ROLE_ICON[role];
  readonly roleLabel = (role: WorldTheme['exposures'][number]['role']) => ROLE_LABEL[role];
  readonly labels = (w: WorldView, ids: string[] | undefined) => themeLabels(w, ids).join(', ') || '–';
  readonly prompt = worldPrompt;
  readonly roleLine = roleLine;
  published(h: WorldHeadline): string { return publishedLabel(h, this.now); }

  constructor() { this.load(false); }

  load(refresh: boolean): void {
    this.loading.set(true);
    this.failed.set(false);
    this.api.getWorld(refresh).subscribe({
      next: w => { this.world.set(w); this.loading.set(false); },
      error: () => { this.failed.set(true); this.loading.set(false); },
    });
  }
}
