import { ChangeDetectionStrategy, Component, effect, inject, input, signal, untracked } from '@angular/core';

import type { CompanyWorld, WorldHeadline } from '../models';
import { AgentSession } from '../core/agent-session';
import { MarketService } from '../services';
import { ROLE_ICON, companyPrompt, publishedLabel, roleLine } from './world-view';

const SECTORS = ['Energy', 'Materials', 'Industrials', 'Consumer Discretionary', 'Consumer Staples', 'Health Care', 'Financials',
  'Information Technology', 'Communication Services', 'Utilities', 'Real Estate'];

/**
 * How the events in the world right now reach one company: the themes that
 * touch its sector, and the themes its own headlines mention.
 */
@Component({
  selector: 'fs-world-impact',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<section class="card world" aria-label="World events and this company">
  <header>
    <div>
      <h2>World events and {{ symbol() }}</h2>
      <p class="sub">
        @if (data(); as d) {
          @if (d.sector) { Sector: <strong>{{ d.sector }}</strong> ({{ sourceNote(d) }}). } @else { Sector unknown. }
        }
        Headlines sorted into themes; the links come from a fixed table, not a forecast.
      </p>
    </div>
    <div class="tools">
      <label class="visually-hidden" for="world-sector">Sector</label>
      <select id="world-sector" class="form-select form-select-sm" [value]="chosen()" (change)="chosen.set($any($event.target).value)">
        <option value="">Sector: automatic</option>
        @for (s of sectors; track s) { <option [value]="s">{{ s }}</option> }
      </select>
      @if (data(); as d) { @if (d.themes.length) { <button type="button" class="btn btn-sm btn-outline-secondary" (click)="ask(d)"><i class="bi bi-stars"></i> Explain</button> } }
    </div>
  </header>
  @if (loading()) {
    <p class="muted">Reading the news…</p>
  } @else if (failed()) {
    <p class="muted">Could not load the world view for this company.</p>
  } @else if (data(); as d) {
    @for (n of d.notes ?? []; track n) { <p class="note">{{ n }}</p> }
    @for (t of d.themes; track t.id) {
      <article class="theme">
        <h3>{{ t.label }}
          @if (t.role) { <span class="role" [class]="'role ' + t.role" [title]="t.why ?? ''">{{ icon(t.role) }} {{ line(t.role, t.driver) }}</span> }
        </h3>
        @if (t.why) { <p class="why">{{ t.why }}</p> }
        <p class="basis">Matched on: {{ t.basis }}</p>
        <ul>
          @for (h of t.own_headlines ?? []; track h.url) {
            <li><span class="tag">{{ symbol() }}</span> <a [href]="h.url" target="_blank" rel="noopener noreferrer">{{ h.title }}</a> <small>{{ h.source }} · {{ published(h) }}</small></li>
          }
          @for (h of t.headlines.slice(0, 2); track h.url) {
            <li><span class="tag world-tag">World</span> <a [href]="h.url" target="_blank" rel="noopener noreferrer">{{ h.title }}</a> <small>{{ h.source }} · {{ published(h) }}</small></li>
          }
        </ul>
      </article>
    } @empty {
      <p class="muted">No current world theme is matched to {{ symbol() }}. @if (!d.sector) { Choose a sector above to check its exposure. }</p>
    }
    @if (d.news.length) {
      <details>
        <summary>All {{ d.news.length }} headlines about {{ symbol() }}</summary>
        <ul>@for (h of d.news; track h.url) { <li><a [href]="h.url" target="_blank" rel="noopener noreferrer">{{ h.title }}</a> <small>{{ h.source }} · {{ published(h) }}</small></li> }</ul>
      </details>
    }
  }
</section>
  `,
  styles: [`
:host { display: block; }
.world { padding: 1rem 1.25rem; margin-bottom: 1rem; }
header { display: flex; flex-wrap: wrap; gap: 0.75rem; justify-content: space-between; align-items: flex-start; }
h2 { font-size: 1.05rem; margin: 0; }
.sub, .muted, .why, .basis, small { color: var(--fs-muted); font-size: 0.8rem; }
.sub { margin: 0.2rem 0 0; }
.tools { display: flex; gap: 0.5rem; align-items: center; }
.tools select { width: auto; max-width: 14rem; }
.theme { border-top: 1px solid var(--fs-border); padding: 0.7rem 0 0.2rem; margin-top: 0.7rem; }
h3 { font-size: 0.95rem; margin: 0 0 0.2rem; display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: baseline; }
.why, .basis { margin: 0 0 0.2rem; }
.role { font-size: 0.75rem; font-weight: 600; padding: 0.05rem 0.45rem; border-radius: 999px; }
.role.benefits { background: var(--fs-pos-soft); color: var(--fs-pos); }
.role.hurt { background: var(--fs-neg-soft); color: var(--fs-neg); }
.role.mixed { background: var(--fs-warn-soft); color: var(--fs-warn); }
ul { list-style: none; padding: 0; margin: 0.3rem 0 0.4rem; display: grid; gap: 0.3rem; }
li { font-size: 0.88rem; }
.tag { font-size: 0.68rem; font-weight: 700; padding: 0.05rem 0.35rem; border-radius: 4px; background: var(--fs-accent-soft); color: var(--fs-accent-text); margin-right: 0.3rem; }
.world-tag { background: var(--fs-surface-3); color: var(--fs-muted); }
.note { font-size: 0.82rem; color: var(--fs-warn); margin: 0.6rem 0 0; }
details { margin-top: 0.6rem; font-size: 0.85rem; }
  `],
})
export class WorldImpactComponent {
  readonly symbol = input.required<string>();

  private readonly api = inject(MarketService);
  private readonly agent = inject(AgentSession);

  readonly sectors = SECTORS;
  readonly chosen = signal('');
  readonly data = signal<CompanyWorld | null>(null);
  readonly loading = signal(false);
  readonly failed = signal(false);
  private readonly now = Date.now();

  readonly icon = (role: 'benefits' | 'hurt' | 'mixed') => ROLE_ICON[role];
  readonly line = roleLine;
  published(h: WorldHeadline): string { return publishedLabel(h, this.now); }

  sourceNote(d: CompanyWorld): string {
    return d.sector_source === 'chosen' ? 'chosen by you' : d.sector_source === 'config' ? 'from PAPER_SYMBOL_SECTORS' : 'built-in list';
  }

  ask(d: CompanyWorld): void { this.agent.ask(companyPrompt(d)); }

  constructor() {
    effect(onCleanup => {
      const symbol = this.symbol();
      const sector = this.chosen();
      untracked(() => { this.data.set(null); this.failed.set(false); this.loading.set(!!symbol); });
      if (!symbol) return;
      const sub = this.api.getCompanyWorld(symbol, sector).subscribe({
        next: d => { this.data.set(d); this.loading.set(false); },
        error: () => { this.failed.set(true); this.loading.set(false); },
      });
      onCleanup(() => sub.unsubscribe());
    });
  }
}
