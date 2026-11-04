import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { Subscription } from 'rxjs';

import { MarketService, describeHttpError } from '../services';
import type { BacktestParams, BacktestResult, BatchResult, SensitivityResult } from '../models';
import { periodError } from '../core/strategy-period';
import { batchHeadline, batchRows, sensitivityHeadline, sensitivityRows, chartSummary, curvePath, metricTiles, warningText } from './backtest-view';

const CHART_W = 640;
const CHART_H = 220;

/**
 * Replays a leaderboard strategy over past prices and reported quarters,
 * rebalanced on a schedule, against a benchmark. The server does the replay
 * (internal/backtest); this panel shows the result and its stated biases.
 */
@Component({
  selector: 'fs-backtest-panel',
  imports: [FormsModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<section class="bt" aria-labelledby="bt-title">
  <div class="bt-head">
    <h3 id="bt-title" class="bt-title"><i class="bi bi-clock-history me-2" aria-hidden="true"></i>Backtest: {{ strategyLabel() }}</h3>
    <div class="bt-controls">
      <div class="fs-segmented" role="group" aria-label="Holdings">
        @for (n of topOptions; track n) { <button type="button" [attr.aria-pressed]="top() === n" (click)="set('top', n)">Top {{ n }}</button> }
      </div>
      <div class="fs-segmented" role="group" aria-label="Rebalance">
        <button type="button" [attr.aria-pressed]="rebalance() === 'monthly'" (click)="set('rebalance', 'monthly')">Monthly</button>
        <button type="button" [attr.aria-pressed]="rebalance() === 'quarterly'" (click)="set('rebalance', 'quarterly')">Quarterly</button>
      </div>
      <div class="fs-segmented" role="group" aria-label="Period">
        <button type="button" [attr.aria-pressed]="years() === 3" (click)="set('years', 3)">3Y</button>
        <button type="button" [attr.aria-pressed]="years() === 5" (click)="set('years', 5)">5Y</button>
        <button type="button" [attr.aria-pressed]="years() === 10" (click)="set('years', 10)">10Y</button>
        <button type="button" [attr.aria-pressed]="years() === 0" (click)="set('years', 0)">Custom</button>
      </div>
    </div>
  </div>

  @if (years() === 0) {
    <form class="bt-custom" (ngSubmit)="runCustom()" novalidate>
      <label>From (UTC) <input type="date" class="form-control form-control-sm" name="from" [ngModel]="from()" (ngModelChange)="from.set($event)" min="1990-01-01" [max]="today"></label>
      <label>To (UTC) <input type="date" class="form-control form-control-sm" name="to" [ngModel]="to()" (ngModelChange)="to.set($event)" [max]="today"></label>
      <button type="submit" class="btn btn-sm btn-primary">Run</button>
      @if (customError(); as msg) { <span class="text-danger small" role="alert">{{ msg }}</span> }
    </form>
  }

  @if (loading()) {
    <div class="progress rounded-0" style="height: 2px" role="progressbar" aria-label="Replaying the strategy"><div class="progress-bar progress-bar-striped progress-bar-animated w-100"></div></div>
    <div class="bt-tiles placeholder-glow" aria-hidden="true">
      @for (i of skeleton; track i) { <div class="card fs-kpi"><span class="placeholder col-6"></span><span class="placeholder col-9"></span></div> }
    </div>
    <p class="bt-note">Replaying the strategy month by month on prices and quarters as they were known then. The first run of a universe takes a while; repeats are instant.</p>
  }

  @if (error(); as err) {
    <div class="fs-empty m-3" role="alert">
      <i class="bi bi-exclamation-triangle"></i>
      <h4>Could not run the backtest</h4>
      <p class="mb-0">{{ err }}</p>
      <button type="button" class="btn btn-sm btn-outline-secondary mt-1" (click)="run()">Try again</button>
    </div>
  } @else if (result(); as r) {
    <div class="bt-tiles">
      @for (t of tiles(); track t.id) {
        <div class="card fs-kpi" [attr.data-tone]="t.tone" [title]="t.hint">
          <span class="fs-kpi-label">{{ t.label }}</span>
          <span class="fs-kpi-value">
            @if (t.tone === 'pos') { <i class="bi bi-caret-up-fill" aria-hidden="true"></i> }
            @if (t.tone === 'neg') { <i class="bi bi-caret-down-fill" aria-hidden="true"></i> }
            {{ t.value }}
          </span>
        </div>
      }
    </div>

    <figure class="bt-chart">
      <svg [attr.viewBox]="'0 0 ' + w + ' ' + h" role="img" [attr.aria-label]="summary()" preserveAspectRatio="none">
        <path class="bench" [attr.d]="benchPath()" fill="none" />
        <path class="strategy" [attr.d]="strategyPath()" fill="none" />
      </svg>
      <figcaption class="bt-legend">
        <span><svg width="28" height="8" aria-hidden="true"><line x1="0" y1="4" x2="28" y2="4" class="strategy" /></svg>{{ strategyLabel() }}</span>
        @if (r.benchmark) { <span><svg width="28" height="8" aria-hidden="true"><line x1="0" y1="4" x2="28" y2="4" class="bench" /></svg>{{ r.benchmark }} (price index)</span> }
        <span class="ms-auto">{{ r.curve[0]?.date }} to {{ r.curve[r.curve.length - 1]?.date }}, 100 at the start</span>
      </figcaption>
    </figure>

    @if (r.notice) { <p class="bt-note" role="note"><i class="bi bi-info-circle" aria-hidden="true"></i> {{ r.notice }}</p> }

    <div class="bt-warnings alert alert-warning" role="note">
      <strong>Read this before trusting the result</strong>
      <ul class="mb-0">
        @for (w of r.warnings; track w) { <li>{{ warningText(w) }}</li> }
        <li>Past results do not predict future ones.</li>
      </ul>
    </div>

    <div class="bt-foot">
      <button type="button" class="btn btn-sm btn-ghost" (click)="showTable.set(!showTable())" [attr.aria-expanded]="showTable()" aria-controls="bt-rebalances">
        {{ showTable() ? 'Hide' : 'Show' }} {{ r.rebalances.length }} rebalances
      </button>
    </div>
    @if (showTable()) {
      <div class="fs-scroll-x" id="bt-rebalances">
        <table class="table table-sm align-middle mb-0">
          <caption class="visually-hidden">Holdings chosen at each rebalance</caption>
          <thead><tr><th scope="col">Date</th><th scope="col">Holdings</th><th scope="col" class="num">Turnover</th><th scope="col" class="num">With fundamentals</th></tr></thead>
          <tbody>
            @for (b of r.rebalances; track b.date) {
              <tr><th scope="row">{{ b.date }}</th><td>{{ b.holdings.join(', ') }}</td><td class="num">{{ b.turnover_pct }}%</td><td class="num">{{ b.fundamentals_coverage_pct }}%</td></tr>
            }
          </tbody>
        </table>
      </div>
    }
  }

  <details class="bt-robust" [open]="robustOpen()" (toggle)="robustOpen.set($any($event.target).open)">
    <summary>Test across markets and periods</summary>
    <p class="bt-note">Runs this strategy once for each market and period you tick, with the same holdings and rebalance settings, and shows the spread. At most {{ maxCells }} backtests per run; each is a full replay, so a cold run takes a while.</p>
    <fieldset class="bt-pick"><legend>Markets</legend>
      @for (u of marketChoices(); track u.id) {
        <label><input type="checkbox" [checked]="pickedMarkets().includes(u.id)" [disabled]="!pickedMarkets().includes(u.id) && pickedMarkets().length >= 4" (change)="toggleMarket(u.id)"> {{ u.label }}</label>
      }
    </fieldset>
    <fieldset class="bt-pick"><legend>Periods</legend>
      @for (p of periodChoices; track p) {
        <label><input type="checkbox" [checked]="pickedPeriods().includes(p)" (change)="togglePeriod(p)"> {{ p }} years</label>
      }
    </fieldset>
    <div class="bt-batch-actions">
      <button type="button" class="btn btn-sm btn-primary" (click)="runBatch()" [disabled]="batchLoading() || !batchCells()">
        {{ batchLoading() ? 'Running…' : 'Run ' + batchCells() + ' backtests' }}
      </button>
      @if (batchCells() > maxCells) { <span class="text-danger small" role="alert">Too many: pick fewer markets or periods ({{ maxCells }} at most).</span> }
    </div>
    @if (batchError(); as err) { <p class="text-danger small" role="alert">{{ err }}</p> }
    @if (batch(); as b) {
      <div class="bt-batch" aria-live="polite">
        @for (line of headline(); track line) { <p class="mb-1">{{ line }}</p> }
        <div class="fs-scroll-x">
          <table class="table table-sm align-middle mb-2">
            <caption class="visually-hidden">Backtest results by market and period</caption>
            <thead><tr><th scope="col">Market</th><th scope="col">Period</th><th scope="col" class="num">Return / yr</th><th scope="col" class="num">Vs benchmark</th><th scope="col" class="num">Worst fall</th><th scope="col" class="num">Sharpe</th></tr></thead>
            <tbody>
              @for (r of rows(); track r.key) {
                <tr>
                  <th scope="row">{{ r.universe }}</th><td>{{ r.period }}</td>
                  @if (r.error) { <td colspan="4" class="text-danger small">{{ r.error }}</td> }
                  @else { <td class="num" [attr.data-tone]="r.tone">{{ r.cagr }}</td><td class="num">{{ r.excess }}</td><td class="num">{{ r.drawdown }}</td><td class="num">{{ r.sharpe }}</td> }
                </tr>
              }
            </tbody>
          </table>
        </div>
        <ul class="bt-limits small">
          @for (l of b.summary.limitations; track l) { <li>{{ l }}</li> }
        </ul>
      </div>
    }
    <div class="bt-sens">
      <h4 class="h6 mb-1">How much do the settings matter?</h4>
      <p class="bt-note">Changes one setting at a time around this backtest: trading cost, number of holdings and rebalance interval.</p>
      <button type="button" class="btn btn-sm btn-outline-primary" (click)="runSensitivity()" [disabled]="sensLoading() || years() === 0">{{ sensLoading() ? 'Running…' : 'Run sensitivity' }}</button>
      @if (years() === 0) { <span class="small text-muted ms-2">Choose 3Y, 5Y or 10Y to run this.</span> }
      @if (sensError(); as err) { <p class="text-danger small mt-2" role="alert">{{ err }}</p> }
      @if (sens(); as s) {
        <div class="mt-2" aria-live="polite">
          @for (line of sensHeadline(); track line) { <p class="mb-1 small">{{ line }}</p> }
          <div class="fs-scroll-x">
            <table class="table table-sm align-middle mb-2">
              <caption class="visually-hidden">Backtest results with one setting changed</caption>
              <thead><tr><th scope="col">Setting</th><th scope="col" class="num">Return / yr</th><th scope="col" class="num">Vs benchmark</th><th scope="col" class="num">Worst fall</th></tr></thead>
              <tbody>
                @for (r of sensRows(); track r.key) {
                  <tr [class.fw-semibold]="r.base">
                    <th scope="row">{{ r.setting }}</th>
                    @if (r.error) { <td colspan="3" class="text-danger small">{{ r.error }}</td> }
                    @else { <td class="num" [attr.data-tone]="r.tone">{{ r.cagr }}</td><td class="num">{{ r.excess }}</td><td class="num">{{ r.drawdown }}</td> }
                  </tr>
                }
              </tbody>
            </table>
          </div>
          <ul class="bt-limits small">@for (n of s.summary.notes; track n) { <li>{{ n }}</li> }</ul>
        </div>
      }
    </div>
  </details>
</section>
  `,
  styles: [`
:host { display: block; min-width: 0; border-top: 1px solid var(--fs-border); }
.bt { padding: 1rem 1.25rem; display: grid; gap: 0.9rem; }
.bt-head { display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: center; }
.bt-title { font-size: 0.95rem; margin: 0; margin-right: auto; }
.bt-controls { display: flex; flex-wrap: wrap; gap: 0.5rem; }
.bt-custom { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 0.6rem; padding: 0 1rem 0.5rem; label { font-size: 0.75rem; color: var(--fs-muted); display: grid; gap: 0.15rem; } }
.bt-note { font-size: 0.75rem; color: var(--fs-muted); margin: 0; }
.bt-tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(8.5rem, 1fr)); gap: 0.6rem; }
.bt-tiles .fs-kpi { padding: 0.7rem 0.85rem; .fs-kpi-value { font-size: 1.15rem; } }
.fs-kpi[data-tone='pos'] .fs-kpi-value { color: var(--fs-pos); }
.fs-kpi[data-tone='neg'] .fs-kpi-value { color: var(--fs-neg); }
.bt-chart { margin: 0; svg:first-child { width: 100%; height: 14rem; display: block; background: var(--fs-surface-2, transparent); border-radius: 8px; } }
.strategy { stroke: var(--fs-accent); stroke-width: 2; vector-effect: non-scaling-stroke; }
.bench { stroke: var(--fs-muted); stroke-width: 1.75; stroke-dasharray: 6 4; vector-effect: non-scaling-stroke; }
.bt-legend { display: flex; flex-wrap: wrap; gap: 0.5rem 1.25rem; font-size: 0.75rem; color: var(--fs-muted); margin-top: 0.4rem;
  span { display: inline-flex; align-items: center; gap: 0.4rem; } }
.bt-warnings { font-size: 0.8rem; margin: 0; ul { padding-left: 1.1rem; margin-top: 0.25rem; } }
.bt-foot { display: flex; }
.bt-robust { border-top: 1px solid var(--fs-border); padding-top: 0.75rem; summary { cursor: pointer; font-weight: 600; font-size: 0.9rem; } }
.bt-pick { border: 0; padding: 0; margin: 0.5rem 0; display: flex; flex-wrap: wrap; gap: 0.4rem 1rem; legend { font-size: 0.75rem; color: var(--fs-muted); float: none; width: auto; margin: 0; } label { font-size: 0.85rem; display: inline-flex; gap: 0.35rem; align-items: center; } }
.bt-batch-actions { display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: center; margin-bottom: 0.5rem; }
.bt-batch p { font-size: 0.85rem; }
.bt-sens { border-top: 1px solid var(--fs-border); margin-top: 0.75rem; padding-top: 0.75rem; }
.bt-limits { color: var(--fs-muted); padding-left: 1.1rem; margin: 0; }
td[data-tone='pos'] { color: var(--fs-pos); } td[data-tone='neg'] { color: var(--fs-neg); }
.num { text-align: right; white-space: nowrap; }
@media (max-width: 575.98px) { .bt { padding-inline: 1rem; } }
  `],
})
export class BacktestPanelComponent {
  /** The leaderboard universe id, or "custom" with symbols. */
  readonly universe = input.required<string>();
  readonly strategy = input.required<string>();
  readonly strategyLabel = input<string>('');
  readonly symbols = input<string[]>([]);
  /** Leaderboard universes the batch can test; the watchlist is not one of them. */
  readonly markets = input<{ id: string; label: string }[]>([]);

  private readonly api = inject(MarketService);
  readonly topOptions = [5, 10, 15] as const;
  readonly skeleton = [1, 2, 3, 4, 5, 6];
  readonly w = CHART_W;
  readonly h = CHART_H;
  readonly warningText = warningText;

  readonly top = signal<number>(10);
  readonly rebalance = signal<BacktestParams['rebalance']>('monthly');
  readonly years = signal<BacktestParams['years']>(5);
  readonly from = signal('');
  readonly to = signal('');
  readonly customError = signal('');
  readonly today = new Date().toISOString().slice(0, 10);
  readonly result = signal<BacktestResult | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  readonly showTable = signal(false);
  private request?: Subscription;

  // Batch: the same strategy over several markets and periods.
  readonly maxCells = 12;
  readonly periodChoices = ['3', '5', '10'];
  readonly robustOpen = signal(false);
  readonly pickedMarkets = signal<string[]>([]);
  readonly pickedPeriods = signal<string[]>(['3', '5', '10']);
  readonly batch = signal<BatchResult | null>(null);
  readonly batchLoading = signal(false);
  readonly batchError = signal('');
  private batchRequest?: Subscription;
  readonly sens = signal<SensitivityResult | null>(null);
  readonly sensLoading = signal(false);
  readonly sensError = signal('');
  private sensRequest?: Subscription;
  readonly sensRows = computed(() => sensitivityRows(this.sens()?.variants ?? []));
  readonly sensHeadline = computed(() => (this.sens() ? sensitivityHeadline(this.sens()!) : []));
  /** The current universe first, then the other leaderboard universes. */
  readonly marketChoices = computed(() => {
    const list = this.markets();
    const current = this.universe();
    return list.some(m => m.id === current) || current === 'custom' ? list : [{ id: current, label: current }, ...list];
  });
  readonly batchCells = computed(() => this.pickedMarkets().length * this.pickedPeriods().length);
  readonly rows = computed(() => batchRows(this.batch()?.cells ?? [], id => this.markets().find(m => m.id === id)?.label ?? id));
  readonly headline = computed(() => (this.batch() ? batchHeadline(this.batch()!.summary) : []));

  readonly tiles = computed(() => (this.result() ? metricTiles(this.result()!.metrics) : []));
  readonly strategyPath = computed(() => curvePath(this.result()?.curve ?? [], CHART_W, CHART_H, 'equity'));
  readonly benchPath = computed(() => curvePath(this.result()?.curve ?? [], CHART_W, CHART_H, 'benchmark'));
  readonly summary = computed(() => (this.result() ? chartSummary(this.result()!) : ''));

  constructor() {
    inject(DestroyRef).onDestroy(() => { this.request?.unsubscribe(); this.batchRequest?.unsubscribe(); this.sensRequest?.unsubscribe(); });
    // The market being viewed is ticked by default; a switch resets the batch.
    effect(() => {
      const u = this.universe();
      untracked(() => { this.pickedMarkets.set(u === 'custom' ? [] : [u]); this.batch.set(null); this.batchError.set(''); this.sens.set(null); this.sensError.set(''); });
    });
    // Runs once inputs are set, and again when the board switches universe or strategy.
    effect(() => {
      this.universe(); this.strategy(); this.symbols();
      untracked(() => this.run());
    });
  }

  set(key: 'top' | 'rebalance' | 'years', value: number | string): void {
    if (key === 'top') this.top.set(value as number);
    else if (key === 'rebalance') this.rebalance.set(value as BacktestParams['rebalance']);
    else this.years.set(value as BacktestParams['years']);
    this.customError.set('');
    if (key === 'years' && value === 0) return; // wait for dates
    this.run();
  }

  toggleMarket(id: string): void {
    this.pickedMarkets.update(l => (l.includes(id) ? l.filter(x => x !== id) : [...l, id]));
  }

  togglePeriod(p: string): void {
    this.pickedPeriods.update(l => (l.includes(p) ? l.filter(x => x !== p) : [...l, p]));
  }

  runBatch(): void {
    if (!this.batchCells() || this.batchCells() > this.maxCells) return;
    this.batchRequest?.unsubscribe();
    this.batchLoading.set(true);
    this.batchError.set('');
    this.batchRequest = this.api.backtestBatch({
      universes: this.pickedMarkets(), periods: this.pickedPeriods(), strategy: this.strategy(), top: this.top(), rebalance: this.rebalance(),
    }).subscribe({
      next: res => { this.batch.set(res); this.batchLoading.set(false); },
      error: err => { this.batchError.set(describeHttpError(err)); this.batchLoading.set(false); },
    });
  }

  runSensitivity(): void {
    if (this.years() === 0) return;
    this.sensRequest?.unsubscribe();
    this.sensLoading.set(true);
    this.sensError.set('');
    this.sensRequest = this.api.backtestSensitivity({
      universe: this.universe(), symbols: this.symbols(), strategy: this.strategy(), top: this.top(), rebalance: this.rebalance(), years: this.years(),
    }).subscribe({
      next: res => { this.sens.set(res); this.sensLoading.set(false); },
      error: err => { this.sensError.set(describeHttpError(err)); this.sensLoading.set(false); },
    });
  }

  runCustom(): void {
    const msg = !this.from() ? 'Choose a start date.' : periodError(this.from(), this.to(), this.today);
    this.customError.set(msg);
    if (!msg) this.run();
  }

  run(): void {
    this.request?.unsubscribe();
    this.loading.set(true);
    this.error.set('');
    this.request = this.api.backtest({
      universe: this.universe(), strategy: this.strategy(), symbols: this.symbols(),
      top: this.top(), rebalance: this.rebalance(), years: this.years(),
      ...(this.years() === 0 ? { from: this.from(), to: this.to() || undefined } : {}),
    }).subscribe({
      next: res => { this.result.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.result.set(null); this.loading.set(false); },
    });
  }
}
