import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import type { Subscription } from 'rxjs';

import { Access } from '../core/access';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { MarketService, describeHttpError } from '../services';
import type { LeaderboardOption, LeaderboardResponse, LeaderboardRow } from '../models';
import { LEADERBOARD_FACTORS, LEADERBOARD_STRATEGIES, LEADERBOARD_UNIVERSES } from './investor-nodes';
import { BacktestPanelComponent } from './backtest-panel';
import { FACTOR_LABELS, STRATEGY_PLAYBOOK, flagList, leaderboardPrompt, num, pct, reasonList, scoreTone } from './leaderboard-view';

const WATCHLIST = 'watchlist';
const STORE_KEY = 'fs.leaderboard';

/**
 * Markets leaderboard: ranks a universe of companies for a strategy from
 * quarterly reports, valuation, trend, entry timing and risk. Scores come
 * from the server; this panel only displays them.
 */
@Component({
  selector: 'fs-leaderboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe, RouterLink, BacktestPanelComponent],
  template: `
<section class="card board" aria-labelledby="board-title">
  <div class="card-header board-head">
    <div class="me-auto">
      <h2 class="card-title" id="board-title"><i class="bi bi-trophy me-2 text-primary" aria-hidden="true"></i>Leaderboard: worth a look now</h2>
      <div class="board-sub">{{ strategyInfo()?.description }}</div>
    </div>
    <div class="board-actions">
      @if (data(); as d) { <span class="as-of" title="Latest price date in the data">as of {{ d.as_of || '—' }}</span> }
      <button type="button" class="btn btn-ghost btn-icon btn-sm" (click)="load()" [disabled]="loading()" aria-label="Refresh leaderboard" title="Refresh">
        <i class="bi bi-arrow-clockwise" aria-hidden="true"></i>
      </button>
    </div>
  </div>

  <div class="board-controls">
    <div class="fs-segmented" role="group" aria-label="Universe">
      @for (u of universes(); track u.id) {
        <button type="button" [attr.aria-pressed]="universe() === u.id" (click)="setUniverse(u.id)" [title]="u.description">{{ u.label }}</button>
      }
      @if (watchlistSymbols().length) {
        <button type="button" [attr.aria-pressed]="universe() === watchlist" (click)="setUniverse(watchlist)" title="The symbols on your watchlist or trackers">My watchlist</button>
      }
    </div>
    <div class="strategies" role="group" aria-label="Strategy">
      @for (s of strategies(); track s.id) {
        <button type="button" class="fs-chip" [attr.aria-pressed]="strategy() === s.id" (click)="setStrategy(s.id)" [title]="s.description">{{ s.label }}</button>
      }
    </div>
  </div>

  @if (loading()) {
    <div class="progress rounded-0" style="height: 2px" role="progressbar" aria-label="Scoring companies"><div class="progress-bar progress-bar-striped progress-bar-animated w-100"></div></div>
    @if (!data()) { <p class="board-note">Scoring {{ universeLabel() }} on quarterly reports and a year of prices. The first load of a universe takes a little while; later switches are instant.</p> }
  }

  @if (error(); as err) {
    <div class="fs-empty m-3" role="alert">
      <i class="bi bi-exclamation-triangle"></i>
      <h3>Could not build the leaderboard</h3>
      <p class="mb-0">{{ err }}</p>
      <button type="button" class="btn btn-sm btn-outline-secondary mt-1" (click)="load()">Try again</button>
    </div>
  } @else if (data(); as d) {
    @if (!d.rows.length) {
      <div class="fs-empty m-3"><i class="bi bi-inbox"></i><h3>No company had enough data to rank</h3><p class="mb-0">Check that a market-data provider plugin is installed and enabled.</p></div>
    } @else {
      <div class="fs-scroll-x">
        <table class="table table-hover align-middle mb-0 board-table">
          <caption class="visually-hidden">{{ strategyInfo()?.label }} ranking of {{ universeLabel() }}</caption>
          <thead>
            <tr>
              <th scope="col" class="num">#</th>
              <th scope="col">Company</th>
              <th scope="col">Score</th>
              <th scope="col" class="factors-col"><span class="visually-hidden">Factor scores</span><span aria-hidden="true" class="factor-head">@for (f of factors; track f) { <span [title]="factorLabels[f]">{{ factorLabels[f][0] }}</span> }</span></th>
              <th scope="col" class="num">Price</th>
              <th scope="col" class="num" title="Trailing twelve-month price / earnings">P/E</th>
              <th scope="col" class="num" title="Trailing twelve-month revenue growth">Rev y/y</th>
              <th scope="col" class="num" title="12-month return excluding the latest month">12-1M</th>
              <th scope="col" class="num" title="Distance from the 52-week high">From high</th>
              <th scope="col" class="num">RSI</th>
              <th scope="col">Why it ranks</th>
            </tr>
          </thead>
          <tbody>
            @for (r of visibleRows(); track r.symbol) {
              <tr class="board-row" [class.active]="r.symbol === market.trackedSymbol()" tabindex="0" (click)="open(r)" (keydown.enter)="open(r)"
                  [attr.aria-label]="'Open ' + r.symbol + ', rank ' + r.rank + ', score ' + r.score">
                <td class="num rank">{{ r.rank }}</td>
                <td class="company"><span class="sym">{{ r.symbol }}</span> <span class="grade" [attr.data-grade]="r.grade" [title]="'Grade ' + r.grade">{{ r.grade }}</span></td>
                <td class="score-cell">
                  <span class="score tabular">{{ r.score | number: '1.0-0' }}</span>
                  <span class="bar" aria-hidden="true"><span [style.width.%]="r.score" [attr.data-tone]="tone(r.score)"></span></span>
                </td>
                <td class="factors-col">
                  <span class="factors">
                    @for (f of factors; track f) {
                      <span class="factor" [attr.data-tone]="tone(factor(r, f))" [title]="factorLabels[f] + ': ' + (factor(r, f) ?? 'no data')">
                        <span [style.height.%]="factor(r, f) ?? 4"></span>
                      </span>
                    }
                  </span>
                  @if (r.coverage_pct < 100) { <span class="visually-hidden">Some factors have no data.</span> }
                </td>
                <td class="num tabular">{{ r.price !== undefined ? (r.price | number: '1.2-2') : '–' }} <small>{{ r.currency }}</small></td>
                <td class="num tabular">{{ num(r.pe_ttm) }}</td>
                <td class="num tabular" [class.text-pos]="(r.revenue_growth_pct ?? 0) > 0" [class.text-neg]="(r.revenue_growth_pct ?? 0) < 0">{{ pct(r.revenue_growth_pct) }}</td>
                <td class="num tabular" [class.text-pos]="(r.momentum_12_1_pct ?? 0) > 0" [class.text-neg]="(r.momentum_12_1_pct ?? 0) < 0">{{ pct(r.momentum_12_1_pct) }}</td>
                <td class="num tabular">{{ pct(r.from_high_pct) }}</td>
                <td class="num tabular">{{ num(r.rsi14, 0) }}</td>
                <td class="why">
                  @for (reason of reasons(r); track reason) { <span class="reason">{{ reason }}</span> }
                  @for (flag of flags(r); track flag.id) { <span class="fs-status is-warning no-dot flag">{{ flag.label }}</span> }
                </td>
              </tr>
            }
          </tbody>
        </table>
      </div>
      <div class="board-foot">
        @if (d.rows.length > 10) {
          <button type="button" class="btn btn-sm btn-ghost" (click)="showAll.set(!showAll())" [attr.aria-expanded]="showAll()">
            {{ showAll() ? 'Show top 10' : 'Show all ' + d.rows.length }}
          </button>
        }
        <button type="button" class="btn btn-sm btn-agent" (click)="askAgent()"><i class="bi bi-stars" aria-hidden="true"></i>Research the top 5</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="showBacktest.set(!showBacktest())" [attr.aria-expanded]="showBacktest()" aria-controls="board-backtest">
          <i class="bi bi-clock-history" aria-hidden="true"></i>Backtest this strategy
        </button>
        @if (access.flowsWrite()) {
          <a class="btn btn-sm btn-outline-secondary" routerLink="/flows" [queryParams]="{ template: playbook() }"><i class="bi bi-diagram-3" aria-hidden="true"></i>Automate as a flow</a>
        }
        <span class="board-note me-auto">
          Scores are 0–100 percentiles within this universe from provider data; a missing factor counts as 40. Research, not investment advice.
          @if (d.unranked.length) { Not ranked (no data): {{ unrankedList() }}. }
        </span>
      </div>
    }
    @if (showBacktest()) {
      <div id="board-backtest">
        <fs-backtest-panel [universe]="universe() === watchlist ? 'custom' : universe()" [strategy]="strategy()" [strategyLabel]="strategyInfo()?.label ?? strategy()"
          [symbols]="universe() === watchlist ? watchlistSymbols() : []" />
      </div>
    }
  }
</section>
  `,
  styles: [`
:host { display: block; min-width: 0; }
.board-head { flex-wrap: wrap; gap: 0.5rem; align-items: flex-start; }
.board-sub { font-size: 0.8rem; color: var(--fs-muted); margin-top: 0.15rem; }
.board-actions { display: flex; align-items: center; gap: 0.4rem; }
.as-of { font-size: 0.75rem; color: var(--fs-muted); white-space: nowrap; }
.board-controls { display: grid; gap: 0.6rem; padding: 0.85rem 1.25rem; border-bottom: 1px solid var(--fs-border); }
.strategies { display: flex; flex-wrap: wrap; gap: 0.35rem; .fs-chip { padding: 0.2rem 0.65rem; font-size: 0.78rem; } }
.board-note { font-size: 0.75rem; color: var(--fs-muted); margin: 0; padding: 0.75rem 1.25rem; }
.board-foot { display: flex; flex-wrap: wrap; align-items: center; gap: 0.5rem; padding: 0.75rem 1.25rem; border-top: 1px solid var(--fs-border);
  .board-note { padding: 0; flex: 1 1 260px; order: 9; } }
.board-table { font-size: 0.82rem; min-width: 900px;
  th { font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--fs-muted); font-weight: 600; white-space: nowrap; }
  td, th { padding: 0.5rem 0.6rem; }
  .num { text-align: right; white-space: nowrap; small { color: var(--fs-muted); font-size: 0.68rem; } } }
.board-row { cursor: pointer; &.active td { background: var(--fs-accent-soft); } &:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: -2px; } }
.rank { color: var(--fs-muted); font-weight: 600; width: 2rem; }
.company { white-space: nowrap; .sym { font-weight: 650; margin-right: 0.3rem; } }
.grade { display: inline-grid; place-items: center; width: 1.3rem; height: 1.3rem; border-radius: 6px; font-size: 0.7rem; font-weight: 700;
  background: var(--fs-surface-3); color: var(--fs-text-2);
  &[data-grade='A'] { background: var(--fs-pos-soft); color: var(--fs-pos); }
  &[data-grade='B'] { background: var(--fs-info-soft); color: var(--fs-info); }
  &[data-grade='E'] { background: var(--fs-neg-soft); color: var(--fs-neg); } }
.score-cell { white-space: nowrap; min-width: 7rem; }
.score { display: inline-block; width: 1.8rem; font-weight: 650; }
.bar { display: inline-block; vertical-align: middle; width: 4.5rem; height: 6px; border-radius: 3px; background: var(--fs-surface-3); overflow: hidden;
  span { display: block; height: 100%; border-radius: 3px; background: var(--fs-muted); } }
[data-tone='pos'] > span, .bar span[data-tone='pos'] { background: var(--fs-pos); }
[data-tone='neg'] > span, .bar span[data-tone='neg'] { background: var(--fs-neg); }
[data-tone='mid'] > span, .bar span[data-tone='mid'] { background: var(--fs-warn); }
.factors-col { width: 7.5rem; }
.factor-head, .factors { display: inline-grid; grid-template-columns: repeat(6, 0.9rem); gap: 0.2rem; text-align: center; }
.factor { display: flex; align-items: flex-end; height: 1.25rem; background: var(--fs-surface-3); border-radius: 3px; overflow: hidden;
  span { display: block; width: 100%; } }
.why { min-width: 18rem; max-width: 30rem; line-height: 1.35; }
.reason { display: block; color: var(--fs-text-2); font-size: 0.76rem; }
.flag { margin: 0.2rem 0.25rem 0 0; font-size: 0.66rem; }
@media (max-width: 575.98px) { .board-controls, .board-foot, .board-note { padding-inline: 1rem; } }
  `],
})
export class LeaderboardComponent {
  readonly market = inject(MarketState);
  readonly access = inject(Access);
  private readonly api = inject(MarketService);
  private readonly agent = inject(AgentSession);

  readonly watchlist = WATCHLIST;
  readonly factors = LEADERBOARD_FACTORS;
  readonly factorLabels = FACTOR_LABELS;
  readonly pct = pct;
  readonly num = num;

  readonly universe = signal('us_mega');
  readonly strategy = signal('best');
  readonly data = signal<LeaderboardResponse | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  readonly showAll = signal(false);
  readonly showBacktest = signal(false);
  private request?: Subscription;

  // Labels come with the first response; until then the client copy names them.
  readonly universes = computed<LeaderboardOption[]>(() => this.data()?.universes ?? LEADERBOARD_UNIVERSES.map(u => ({ ...u, description: '' })));
  readonly strategies = computed<LeaderboardOption[]>(() => this.data()?.strategies ?? LEADERBOARD_STRATEGIES.map(s => ({ ...s, description: '' })));
  readonly strategyInfo = computed(() => this.strategies().find(s => s.id === this.strategy()));
  readonly universeLabel = computed(() => this.universe() === WATCHLIST ? 'your watchlist' : this.universes().find(u => u.id === this.universe())?.label ?? this.universe());
  readonly watchlistSymbols = computed(() => [...new Set(this.market.trackedAssets().map(a => a.symbol.toUpperCase()))].slice(0, 25));
  readonly visibleRows = computed(() => (this.showAll() ? this.data()?.rows ?? [] : (this.data()?.rows ?? []).slice(0, 10)));
  readonly unrankedList = computed(() => (this.data()?.unranked ?? []).map(u => u.symbol).join(', '));
  readonly playbook = computed(() => STRATEGY_PLAYBOOK[this.strategy()] ?? 'top10-buy-now');

  constructor() {
    try {
      const saved = JSON.parse(localStorage.getItem(STORE_KEY) || '{}');
      if (typeof saved.universe === 'string' && saved.universe !== WATCHLIST) this.universe.set(saved.universe);
      if (typeof saved.strategy === 'string') this.strategy.set(saved.strategy);
    } catch { /* storage may be unavailable */ }
    inject(DestroyRef).onDestroy(() => this.request?.unsubscribe());
    this.load();
  }

  setUniverse(id: string): void {
    if (id === this.universe()) return;
    this.universe.set(id);
    this.showAll.set(false);
    this.save();
    this.load();
  }

  setStrategy(id: string): void {
    if (id === this.strategy()) return;
    this.strategy.set(id);
    this.save();
    this.load();
  }

  load(): void {
    this.request?.unsubscribe();
    this.loading.set(true);
    this.error.set('');
    const symbols = this.universe() === WATCHLIST ? this.watchlistSymbols() : [];
    this.request = this.api.getLeaderboard(this.universe(), this.strategy(), symbols).subscribe({
      next: res => { this.data.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.loading.set(false); },
    });
  }

  open(row: LeaderboardRow): void {
    this.market.view({ symbol: row.symbol, provider: this.data()?.provider || this.market.provider() });
    // The chart and fundamentals load at the top of the page.
    window.scrollTo({ top: 0, behavior: 'smooth' });
  }

  askAgent(): void {
    const d = this.data();
    if (!d?.rows.length) return;
    this.agent.ask(leaderboardPrompt(d.rows, this.universeLabel(), this.strategyInfo()?.label ?? this.strategy(), d.as_of));
  }

  factor(row: LeaderboardRow, f: string): number | undefined {
    return row[f as keyof LeaderboardRow] as number | undefined;
  }

  tone(score: number | undefined): string { return scoreTone(score); }
  reasons(row: LeaderboardRow): string[] { return reasonList(row); }
  flags(row: LeaderboardRow) { return flagList(row); }

  private save(): void {
    try { localStorage.setItem(STORE_KEY, JSON.stringify({ universe: this.universe(), strategy: this.strategy() })); } catch { /* ignore */ }
  }
}
