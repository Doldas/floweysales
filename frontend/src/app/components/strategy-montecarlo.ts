import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import type { Subscription } from 'rxjs';
import { MarketService, describeHttpError } from '../services';
import type { StrategyMonteCarloResult, StrategyRunRequest, StrategyRunResult } from '../models';
import {
  MIN_TRADES, TRADE_MC_DEFAULTS, closedTradeCount, tradeMonteCarloConcentration, tradeMonteCarloCsv, tradeMonteCarloError, tradeMonteCarloFileName, tradeMonteCarloHeadline, tradeMonteCarloJson, tradeMonteCarloReadiness, tradeMonteCarloRequest, tradeMonteCarloRows,
  type TradeMonteCarloSettings,
} from '../core/strategy-montecarlo';

/**
 * Robustness panel for one strategy replay: resamples the closed trades with a
 * seed. Simulated figures are labeled as such and kept apart from the replay
 * above. Research only; nothing is ordered.
 */
@Component({
  selector: 'fs-strategy-montecarlo',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<section class="card panel" aria-labelledby="smc-title">
  <h2 id="smc-title" class="h6 mb-1">How robust is this result?</h2>
  <p class="smc-note">Redraws the trades of the replay into many simulated sequences with a seed you choose, to show how much the result depended on which trades came and in what order. It does not predict returns and it never changes the replay above.</p>
  @if (readiness(); as why) {
    <p class="smc-note" role="status">{{ why }}</p>
  } @else {
    <div class="smc-form">
      <label for="smc-method">Method
        <select id="smc-method" class="form-select form-select-sm" [value]="settings().method" (change)="set('method', $any($event.target).value)">
          <option value="trade_bootstrap">Bootstrap (draw trades with replacement)</option>
          <option value="trade_shuffle">Shuffle (same trades, new order)</option>
        </select>
      </label>
      <label for="smc-runs">Runs <input id="smc-runs" class="form-control form-control-sm" type="number" min="100" max="20000" step="100" [value]="settings().runs" (input)="set('runs', $any($event.target).value)"></label>
      <label for="smc-seed">Seed <input id="smc-seed" class="form-control form-control-sm" type="number" min="0" step="1" [value]="settings().seed" (input)="set('seed', $any($event.target).value)"></label>
      <label for="smc-limit">Alert at a fall of (%) <input id="smc-limit" class="form-control form-control-sm" type="number" min="1" max="100" placeholder="off" [value]="settings().drawdownLimit" (input)="set('drawdownLimit', $any($event.target).value)"></label>
      <label for="smc-cost">Extra cost (bps per trade) <input id="smc-cost" class="form-control form-control-sm" type="number" min="0" max="500" placeholder="none" [value]="settings().extraCost" (input)="set('extraCost', $any($event.target).value)"></label>
      <label for="smc-disp">Spread of trades
        <select id="smc-disp" class="form-select form-select-sm" [value]="settings().dispersion" (change)="set('dispersion', $any($event.target).value)">
          <option value="1">As observed</option>
          <option value="1.25">25% wider</option>
          <option value="1.5">50% wider</option>
          <option value="2">Double</option>
        </select>
      </label>
      <label for="smc-delay">Fill delay (bars) <input id="smc-delay" class="form-control form-control-sm" type="number" min="0" max="5" step="1" placeholder="none" [value]="settings().delayBars" (input)="set('delayBars', $any($event.target).value)"></label>
      <label for="smc-regime">Turbulent stretch (% of trades) <input id="smc-regime" class="form-control form-control-sm" type="number" min="0" max="100" placeholder="none" [value]="settings().regimeShare" (input)="set('regimeShare', $any($event.target).value)"></label>
      <label for="smc-regime-scale">Spread in that stretch
        <select id="smc-regime-scale" class="form-select form-select-sm" [value]="settings().regimeScale" (change)="set('regimeScale', $any($event.target).value)">
          <option value="1.5">1.5 times</option>
          <option value="2">2 times</option>
          <option value="3">3 times</option>
          <option value="4">4 times</option>
        </select>
      </label>
      <label for="smc-size">Position size
        <select id="smc-size" class="form-select form-select-sm" [value]="settings().positionScale" (change)="set('positionScale', $any($event.target).value)">
          <option value="0.5">Half</option>
          <option value="1">As in the replay</option>
          <option value="1.5">1.5 times (more concentrated)</option>
          <option value="2">Double (more concentrated)</option>
          <option value="3">Triple (more concentrated)</option>
        </select>
      </label>
    </div>
    <button type="button" class="btn btn-sm btn-outline-primary mt-2" (click)="run()" [disabled]="loading()">{{ loading() ? 'Simulating…' : 'Run simulation' }}</button>
  }
  @if (error(); as err) { <p class="text-danger small mt-2" role="alert">{{ err }}</p> }
  @if (sim(); as m) {
    <div class="mt-2" aria-live="polite">
      @for (line of headline(); track line) { <p class="mb-1 small">{{ line }}</p> }
      <p class="smc-note mb-1">Simulated, not observed. The “Happened” column is the replay; the other columns are percentiles of simulated sequences. Data {{ m.data_start }} to {{ m.data_end }} (UTC) from {{ m.provider }}, generated {{ m.generated_at }}.</p>
      <div class="fs-scroll-x">
        <table class="table table-sm align-middle mb-2">
          <caption class="visually-hidden">Simulated outcomes with the replay result for comparison</caption>
          <thead><tr><th scope="col">Figure</th><th scope="col" class="num">Happened</th><th scope="col" class="num">5th</th><th scope="col" class="num">25th</th><th scope="col" class="num">Median</th><th scope="col" class="num">75th</th><th scope="col" class="num">95th</th></tr></thead>
          <tbody>
            @for (r of rows(); track r.id) {
              <tr><th scope="row">{{ r.label }}</th><td class="num">{{ r.observed }}</td><td class="num">{{ r.p5 }}</td><td class="num">{{ r.p25 }}</td><td class="num">{{ r.p50 }}</td><td class="num">{{ r.p75 }}</td><td class="num">{{ r.p95 }}</td></tr>
            }
          </tbody>
        </table>
      </div>
      @if (concentration(); as c) { <p class="smc-note mb-1">{{ c }}</p> }
      <div class="d-flex gap-2 mb-2">
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="download('csv')"><i class="bi bi-filetype-csv" aria-hidden="true"></i> Export CSV</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="download('json')"><i class="bi bi-filetype-json" aria-hidden="true"></i> Export JSON</button>
      </div>
      <h3 class="h6 mb-1">What this assumes</h3>
      <ul class="smc-list small">@for (a of m.simulation.assumptions; track a) { <li>{{ a }}</li> }</ul>
      <h3 class="h6 mb-1">What it cannot tell you</h3>
      <ul class="smc-list small">@for (l of m.simulation.limitations; track l) { <li>{{ l }}</li> }</ul>
      @for (n of m.notes; track n) { <p class="smc-note">{{ n }}</p> }
    </div>
  }
</section>
  `,
  styles: [`
.smc-note { font-size: 0.75rem; color: var(--fs-muted); margin: 0 0 0.5rem; }
.smc-form { display: flex; flex-wrap: wrap; gap: 0.5rem 0.75rem; label { font-size: 0.75rem; color: var(--fs-muted); display: grid; gap: 0.15rem; min-width: 8rem; } }
.smc-list { color: var(--fs-muted); padding-left: 1.1rem; margin: 0 0 0.5rem; }
.num { text-align: end; font-variant-numeric: tabular-nums; }
  `],
})
export class StrategyMonteCarloComponent {
  private readonly api = inject(MarketService);
  /** The replay the simulation resamples; a new replay clears the old simulation. */
  readonly replay = input.required<StrategyRunResult>();
  /** The request that produced the replay, so the server replays the same question. */
  readonly request = input.required<StrategyRunRequest | null>();

  readonly settings = signal<TradeMonteCarloSettings>({ ...TRADE_MC_DEFAULTS });
  readonly sim = signal<StrategyMonteCarloResult | null>(null);
  readonly loading = signal(false);
  readonly error = signal('');
  private sub?: Subscription;

  readonly readiness = computed(() => tradeMonteCarloReadiness(closedTradeCount(this.replay().trades)));
  readonly rows = computed(() => (this.sim() ? tradeMonteCarloRows(this.sim()!) : []));
  readonly headline = computed(() => (this.sim() ? tradeMonteCarloHeadline(this.sim()!) : []));
  readonly concentration = computed(() => (this.sim() ? tradeMonteCarloConcentration(this.sim()!) : ''));
  readonly minTrades = MIN_TRADES;

  constructor() {
    effect(() => {
      this.replay();
      untracked(() => { this.sub?.unsubscribe(); this.sim.set(null); this.error.set(''); this.loading.set(false); });
    });
  }

  set<K extends keyof TradeMonteCarloSettings>(key: K, value: string): void {
    this.settings.update(s => ({ ...s, [key]: value }));
  }

  run(): void {
    const req = this.request();
    const bad = tradeMonteCarloError(this.settings());
    this.error.set(bad);
    if (bad || !req) return;
    this.sub?.unsubscribe();
    this.loading.set(true);
    this.sub = this.api.strategyMonteCarlo(tradeMonteCarloRequest(req, this.settings())).subscribe({
      next: res => { this.sim.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.loading.set(false); },
    });
  }

  /** Saves the shown simulation, with strategy, market, UTC data dates, model version, seed, assumptions and limitations, as a file. */
  download(kind: 'csv' | 'json'): void {
    const m = this.sim();
    if (!m) return;
    const blob = new Blob([kind === 'csv' ? tradeMonteCarloCsv(m) : tradeMonteCarloJson(m)], { type: kind === 'csv' ? 'text/csv;charset=utf-8' : 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = tradeMonteCarloFileName(m, kind);
    a.click();
    URL.revokeObjectURL(url);
  }
}
