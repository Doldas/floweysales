import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { FormsModule } from '@angular/forms';
import type { Subscription } from 'rxjs';

import { MarketService, describeHttpError } from '../services';
import type { BacktestParams, BacktestResult, BacktestSummary, BatchResult, MonteCarloResult, SavedBacktest, SensitivityResult } from '../models';
import type { Provenance } from '../core/provenance';
import { SourceComponent } from '../ui/source';
import { SkeletonComponent } from '../ui/skeleton';
import { ProgressStatusComponent } from '../ui/progress-status';
import { PROGRESS_POLL_MS, newRunId, parseRunProgress, type RunProgress } from '../ui/run-progress';
import { periodError } from '../core/strategy-period';
import { MONTE_CARLO_DEFAULTS, monteCarloError, monteCarloHeadline, monteCarloParams, monteCarloRows, type MonteCarloSettings, batchHeadline, batchParams, batchRows, exportUrl, savedLabel, sensitivityHeadline, sensitivityParams, sensitivityRows, singleParams, chartSummary, curvePath, metricTiles, regimeRows, segmentRows, stressRows, warningText } from './backtest-view';

const CHART_W = 640;
const CHART_H = 220;

/**
 * Replays a leaderboard strategy over past prices and reported quarters,
 * rebalanced on a schedule, against a benchmark. The server does the replay
 * (internal/backtest); this panel shows the result and its stated biases.
 */
@Component({
  selector: 'fs-backtest-panel',
  imports: [FormsModule, SourceComponent, SkeletonComponent, ProgressStatusComponent],
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
    <fs-progress-status stage="Replaying the strategy" />
    <div class="bt-tiles" aria-hidden="true">
      @for (i of skeleton; track i) { <div class="card fs-kpi"><fs-skeleton shape="kpi" /></div> }
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

    @if (segments().length) {
      <h4 class="h6 mb-1">Consecutive parts of the period</h4>
      <div class="table-responsive">
        <table class="table table-sm bt-segments">
          <thead><tr><th scope="col">Period (UTC)</th><th scope="col" class="text-end">Return / year</th><th scope="col" class="text-end">Benchmark</th><th scope="col" class="text-end">Excess</th><th scope="col" class="text-end">Worst drop</th></tr></thead>
          <tbody>
            @for (s of segments(); track s.period) {
              <tr><td>{{ s.period }}</td><td class="text-end">{{ s.cagr }}</td><td class="text-end">{{ s.benchmark }}</td>
                <td class="text-end" [class.text-success]="s.excessTone === 'up'" [class.text-danger]="s.excessTone === 'down'">{{ s.excess }}</td><td class="text-end">{{ s.drawdown }}</td></tr>
            }
          </tbody>
        </table>
      </div>
      <p class="bt-note">{{ r.segment_summary }} {{ r.segment_note }}</p>
    }

    @if (stress().length || r.stress_not_covered?.length) {
      <h4 class="h6 mb-1 mt-3">Named stress windows</h4>
      @if (stress().length) {
        <div class="table-responsive">
          <table class="table table-sm bt-stress">
            <caption class="visually-hidden">Strategy and benchmark inside named past market falls</caption>
            <thead><tr><th scope="col">Window (UTC)</th><th scope="col" class="text-end">Strategy</th><th scope="col" class="text-end">Benchmark</th><th scope="col" class="text-end">Excess</th><th scope="col" class="text-end">Worst drop</th></tr></thead>
            <tbody>
              @for (s of stress(); track s.name) {
                <tr><td>{{ s.name }}<br /><span class="bt-note">{{ s.period }}, {{ s.days }} days</span></td><td class="text-end">{{ s.ret }}</td><td class="text-end">{{ s.benchmark }}</td>
                  <td class="text-end" [class.text-success]="s.excessTone === 'up'" [class.text-danger]="s.excessTone === 'down'">{{ s.excess }}</td><td class="text-end">{{ s.drawdown }}</td></tr>
              }
            </tbody>
          </table>
        </div>
      }
      @if (r.stress_not_covered?.length) { <p class="bt-note">Not tested, the data does not cover: {{ r.stress_not_covered!.join(', ') }}.</p> }
      <p class="bt-note">{{ r.stress_note }} Returns are totals over the window, not per year.</p>
    }

    @if (regimes().length) {
      <h4 class="h6 mb-1 mt-3">Results by market regime</h4>
      <div class="table-responsive">
        <table class="table table-sm bt-regimes">
          <caption class="visually-hidden">Strategy and benchmark return per year by benchmark trend and volatility</caption>
          <thead><tr><th scope="col">Regime</th><th scope="col" class="text-end">Days</th><th scope="col" class="text-end">Strategy / year</th><th scope="col" class="text-end">Benchmark</th><th scope="col" class="text-end">Excess</th></tr></thead>
          <tbody>
            @for (g of regimes(); track g.name) {
              <tr><td>{{ g.name }} ({{ g.share }})</td><td class="text-end">{{ g.days }}</td><td class="text-end">{{ g.cagr }}</td><td class="text-end">{{ g.benchmark }}</td>
                <td class="text-end" [class.text-success]="g.excessTone === 'up'" [class.text-danger]="g.excessTone === 'down'">{{ g.excess }}</td></tr>
            }
          </tbody>
        </table>
      </div>
      <p class="bt-note">{{ r.regime_note }}</p>
    }

    @if (r.trades !== undefined || r.concentration) {
      <p class="bt-note mt-2">
        @if (r.trades !== undefined) { {{ r.trades }} trades (buys and sells of single holdings). }
        @if (r.concentration; as c) { Total return {{ c.total_return_pct.toFixed(1) }}% over {{ c.periods }} rebalance periods; without the best {{ c.dropped }}: {{ c.without_best_return_pct.toFixed(1) }}%. }
      </p>
    }
    @if (r.walk_forward_note) { <p class="bt-note">{{ r.walk_forward_note }}</p> }

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

  <details class="bt-robust" [open]="robustOpen()" (toggle)="toggleRobust($any($event.target).open)">
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
      @if (batchLoading()) {
        <fs-progress-status class="bt-batch-progress" [stage]="batchProgress()?.stage ?? 'Running the batch'" [done]="batchProgress()?.done ?? null" [total]="batchProgress()?.total ?? null" />
      }
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

    <div class="bt-sens bt-mc">
      <h4 class="h6 mb-1">How wide was the range of outcomes?</h4>
      <p class="bt-note">Reshuffles and redraws the days of this backtest into many simulated paths. It shows how much the result depended on the order and mix of days; it does not predict returns. Same settings and seed give the same numbers.</p>
      <div class="bt-mc-form">
        <label>Method
          <select class="form-select form-select-sm" [ngModel]="mc().method" (ngModelChange)="setMc('method', $event)" name="mc-method">
            <option value="block">Block bootstrap (keeps streaks)</option>
            <option value="bootstrap">Bootstrap (independent days)</option>
            <option value="shuffle">Shuffle (same days, new order)</option>
          </select>
        </label>
        <label>Runs <input class="form-control form-control-sm" type="number" min="100" max="20000" step="100" [ngModel]="mc().runs" (ngModelChange)="setMc('runs', $event)" name="mc-runs"></label>
        <label>Seed <input class="form-control form-control-sm" type="number" min="0" step="1" [ngModel]="mc().seed" (ngModelChange)="setMc('seed', $event)" name="mc-seed"></label>
        <label>Alert at a fall of (%) <input class="form-control form-control-sm" type="number" min="1" max="100" placeholder="off" [ngModel]="mc().drawdownLimit" (ngModelChange)="setMc('drawdownLimit', $event)" name="mc-limit"></label>
        <label>Slippage (bps per side) <input class="form-control form-control-sm" type="number" min="0" max="100" placeholder="none" [ngModel]="mc().slippage" (ngModelChange)="setMc('slippage', $event)" name="mc-slip"></label>
        <label>Extra cost (bps per year) <input class="form-control form-control-sm" type="number" min="0" max="1000" placeholder="none" [ngModel]="mc().extraCost" (ngModelChange)="setMc('extraCost', $event)" name="mc-cost"></label>
        <label>Volatility
          <select class="form-select form-select-sm" [ngModel]="mc().volatility" (ngModelChange)="setMc('volatility', $event)" name="mc-vol">
            <option value="1">As observed</option>
            <option value="1.25">25% higher</option>
            <option value="1.5">50% higher</option>
            <option value="2">Double</option>
          </select>
        </label>
      </div>
      <button type="button" class="btn btn-sm btn-outline-primary mt-2" (click)="runMonteCarlo()" [disabled]="mcLoading() || (years() === 0 && !from())">{{ mcLoading() ? 'Simulating…' : 'Run simulation' }}</button>
      @if (mcError(); as err) { <p class="text-danger small mt-2" role="alert">{{ err }}</p> }
      @if (mcResult(); as m) {
        <div class="mt-2" aria-live="polite">
          @for (line of mcHeadline(); track line) { <p class="mb-1 small">{{ line }}</p> }
          <p class="bt-note mb-1">Simulated, not observed. The “Happened” column is the replayed history; the other columns are percentiles of simulated paths.</p>
          <div class="fs-scroll-x">
            <table class="table table-sm align-middle mb-2">
              <caption class="visually-hidden">Simulated outcomes with the observed result for comparison</caption>
              <thead><tr><th scope="col">Figure</th><th scope="col" class="num">Happened</th><th scope="col" class="num">5th</th><th scope="col" class="num">25th</th><th scope="col" class="num">Median</th><th scope="col" class="num">75th</th><th scope="col" class="num">95th</th></tr></thead>
              <tbody>
                @for (r of mcRows(); track r.id) {
                  <tr><th scope="row">{{ r.label }}</th><td class="num">{{ r.observed }}</td><td class="num">{{ r.p5 }}</td><td class="num">{{ r.p25 }}</td><td class="num">{{ r.p50 }}</td><td class="num">{{ r.p75 }}</td><td class="num">{{ r.p95 }}</td></tr>
                }
              </tbody>
            </table>
          </div>
          <h5 class="h6 mb-1">What this assumes</h5>
          <ul class="bt-limits small">@for (a of m.simulation.assumptions; track a) { <li>{{ a }}</li> }</ul>
          <h5 class="h6 mb-1">What it cannot tell you</h5>
          <ul class="bt-limits small">@for (l of m.simulation.limitations; track l) { <li>{{ l }}</li> }</ul>
        </div>
      }
    </div>

    <div class="bt-saved">
      <h4 class="h6 mb-1">Saved results</h4>
      <p class="bt-note">Saving stores the question and the server's answer, so you can reopen it or download it with its parameters. Dates are UTC.</p>
      <div class="bt-batch-actions">
        <input class="form-control form-control-sm bt-name" type="text" maxlength="80" placeholder="Name this result" aria-label="Name for the saved result" [ngModel]="saveName()" (ngModelChange)="saveName.set($event)" name="save-name">
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="save('single')" [disabled]="!canSave('single')">Save backtest</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="save('batch')" [disabled]="!canSave('batch')">Save markets and periods</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="save('sensitivity')" [disabled]="!canSave('sensitivity')">Save settings check</button>
        <button type="button" class="btn btn-sm btn-outline-secondary" (click)="save('montecarlo')" [disabled]="!canSave('montecarlo')">Save simulation</button>
      </div>
      @if (savedError(); as err) { <p class="text-danger small" role="alert">{{ err }}</p> }
      @if (saved().length) {
        <ul class="bt-saved-list">
          @for (r of saved(); track r.id) {
            <li>
              <span>{{ label(r) }}</span>
              <span class="bt-saved-actions">
                <button type="button" class="btn btn-sm btn-link p-0" (click)="openSaved(r)">Open</button>
                <button type="button" class="btn btn-sm btn-link p-0" (click)="summarize(r)" [disabled]="summaryLoading() === r.id">{{ summaryLoading() === r.id ? 'Summarizing…' : 'Summarize' }}</button>
                <a class="btn btn-sm btn-link p-0" [href]="exportHref(r.id, 'csv')" download>CSV</a>
                <a class="btn btn-sm btn-link p-0" [href]="exportHref(r.id, 'json')" download>JSON</a>
                <button type="button" class="btn btn-sm btn-link text-danger p-0" (click)="removeSaved(r)" [attr.aria-label]="'Delete ' + r.name">Delete</button>
              </span>
            </li>
          }
        </ul>
      } @else { <p class="bt-note">Nothing saved yet.</p> }
      @if (openedRun(); as o) {
        <div class="bt-note" role="status">“{{ o.name }}” was opened as a {{ kindLabel(o.kind) }}. Where it came from: <fs-source [fact]="o.provenance" /></div>
      }
      @if (aiSummary(); as s) {
        <div class="bt-summary" aria-live="polite">
          <h5 class="h6 mb-1">Summary of “{{ summaryName() }}”</h5>
          @if (s.verified) {
            <p class="bt-note">Every figure below was found in the recorded results ({{ s.citations }}). The check is mechanical; it does not judge the wording.</p>
            <p class="bt-summary-text">{{ s.summary }}</p>
          } @else {
            <p class="text-warning small" role="status">{{ s.reason }}</p>
            <pre class="bt-summary-facts">{{ s.facts }}</pre>
          }
          @for (n of s.notes; track n) { <p class="bt-note">{{ n }}</p> }
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
.bt-mc-form { display: flex; flex-wrap: wrap; gap: 0.5rem 0.75rem; label { font-size: 0.75rem; color: var(--fs-muted); display: grid; gap: 0.15rem; min-width: 8rem; } }
.bt-pick { border: 0; padding: 0; margin: 0.5rem 0; display: flex; flex-wrap: wrap; gap: 0.4rem 1rem; legend { font-size: 0.75rem; color: var(--fs-muted); float: none; width: auto; margin: 0; } label { font-size: 0.85rem; display: inline-flex; gap: 0.35rem; align-items: center; } }
.bt-batch-actions { display: flex; flex-wrap: wrap; gap: 0.6rem; align-items: center; margin-bottom: 0.5rem; }
.bt-batch p { font-size: 0.85rem; }
.bt-sens { border-top: 1px solid var(--fs-border); margin-top: 0.75rem; padding-top: 0.75rem; }
.bt-saved { border-top: 1px solid var(--fs-border); margin-top: 0.75rem; padding-top: 0.75rem; }
.bt-name { max-width: 16rem; }
.bt-saved-list { list-style: none; padding: 0; margin: 0; display: grid; gap: 0.35rem; li { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 0.4rem; font-size: 0.85rem; } }
.bt-summary { margin-top: 0.75rem; }
.bt-summary-text { white-space: pre-wrap; font-size: 0.9rem; }
.bt-summary-facts { white-space: pre-wrap; font-size: 0.8rem; max-height: 16rem; overflow: auto; }
.bt-saved-actions { display: inline-flex; gap: 0.75rem; }
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
  /** Counts from the server while a batch runs; null until it reports them. */
  readonly batchProgress = signal<RunProgress | null>(null);
  readonly batchError = signal('');
  private batchRequest?: Subscription;
  private progressTimer?: ReturnType<typeof setInterval>;
  readonly sens = signal<SensitivityResult | null>(null);
  readonly sensLoading = signal(false);
  readonly sensError = signal('');
  private sensRequest?: Subscription;
  readonly mc = signal<MonteCarloSettings>({ ...MONTE_CARLO_DEFAULTS });
  readonly mcResult = signal<MonteCarloResult | null>(null);
  readonly mcLoading = signal(false);
  readonly mcError = signal('');
  private mcRequest?: Subscription;
  // What each result was asked for, so saving stores exactly that question.
  private lastParams: { single?: Record<string, string>; batch?: Record<string, string>; sensitivity?: Record<string, string>; montecarlo?: Record<string, string> } = {};
  savedLoaded = false;
  readonly saved = signal<SavedBacktest[]>([]);
  readonly saveName = signal('');
  readonly savedError = signal('');
  readonly aiSummary = signal<BacktestSummary | null>(null);
  readonly summaryName = signal('');
  /** The saved run now shown in the result areas, with how its result was produced. */
  readonly openedRun = signal<{ name: string; kind: SavedBacktest['kind']; provenance: Provenance } | null>(null);
  readonly summaryLoading = signal<number | null>(null);
  readonly label = savedLabel;
  readonly kindLabel = (k: SavedBacktest['kind']): string => (k === 'batch' ? 'markets-and-periods result' : k === 'sensitivity' ? 'settings check' : k === 'montecarlo' ? 'simulation' : 'backtest');
  readonly exportHref = exportUrl;
  readonly sensRows = computed(() => sensitivityRows(this.sens()?.variants ?? []));
  readonly mcRows = computed(() => (this.mcResult() ? monteCarloRows(this.mcResult()!) : []));
  readonly mcHeadline = computed(() => (this.mcResult() ? monteCarloHeadline(this.mcResult()!) : []));
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
  readonly segments = computed(() => segmentRows(this.result()?.segments));
  readonly stress = computed(() => stressRows(this.result()?.stress));
  readonly regimes = computed(() => regimeRows(this.result()?.regimes));
  readonly summary = computed(() => (this.result() ? chartSummary(this.result()!) : ''));

  constructor() {
    inject(DestroyRef).onDestroy(() => { this.request?.unsubscribe(); this.batchRequest?.unsubscribe(); this.sensRequest?.unsubscribe(); this.mcRequest?.unsubscribe(); this.stopProgress(); });
    // The market being viewed is ticked by default; a switch resets the batch.
    effect(() => {
      const u = this.universe();
      untracked(() => { this.pickedMarkets.set(u === 'custom' ? [] : [u]); this.batch.set(null); this.batchError.set(''); this.sens.set(null); this.sensError.set(''); this.mcResult.set(null); this.mcError.set(''); this.openedRun.set(null); });
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

  toggleRobust(open: boolean): void {
    this.robustOpen.set(open);
    if (open && !this.savedLoaded) { this.savedLoaded = true; this.loadSaved(); }
  }

  /** Whether the result of this kind is on screen and a name is given. */
  canSave(kind: SavedBacktest['kind']): boolean {
    const have = kind === 'single' ? this.result() : kind === 'batch' ? this.batch() : kind === 'montecarlo' ? this.mcResult() : this.sens();
    return !!have && !!this.lastParams[kind] && !!this.saveName().trim();
  }

  save(kind: SavedBacktest['kind']): void {
    const params = this.lastParams[kind];
    if (!params || !this.canSave(kind)) return;
    this.savedError.set('');
    this.api.saveBacktestRun(this.saveName().trim(), kind, params).subscribe({
      next: () => { this.saveName.set(''); this.loadSaved(); },
      error: err => this.savedError.set(describeHttpError(err)),
    });
  }

  loadSaved(): void {
    this.api.listBacktestRuns().subscribe({
      next: res => this.saved.set(res.runs),
      error: () => this.saved.set([]),
    });
  }

  private openRequest?: Subscription;

  openSaved(r: SavedBacktest): void {
    this.savedError.set('');
    this.openRequest?.unsubscribe();
    this.openRequest = this.api.getBacktestRun(r.id).subscribe({
      next: full => {
        this.openedRun.set(full.provenance ? { name: full.name, kind: full.kind, provenance: full.provenance } : null);
        if (full.kind === 'batch') this.batch.set(full.result as BatchResult);
        else if (full.kind === 'sensitivity') this.sens.set(full.result as SensitivityResult);
        else if (full.kind === 'montecarlo') this.mcResult.set(full.result as MonteCarloResult);
        else { this.result.set(full.result as BacktestResult); this.error.set(''); }
      },
      error: err => this.savedError.set(describeHttpError(err)),
    });
  }

  summarize(r: SavedBacktest): void {
    this.savedError.set('');
    this.summaryLoading.set(r.id);
    this.api.summarizeBacktestRun(r.id).subscribe({
      next: res => { this.aiSummary.set(res); this.summaryName.set(r.name); this.summaryLoading.set(null); },
      error: err => { this.savedError.set(describeHttpError(err)); this.summaryLoading.set(null); },
    });
  }

  removeSaved(r: SavedBacktest): void {
    this.api.deleteBacktestRun(r.id).subscribe({
      next: () => { this.saved.update(list => list.filter(x => x.id !== r.id)); if (this.openedRun()?.name === r.name) this.openedRun.set(null); },
      error: err => this.savedError.set(describeHttpError(err)),
    });
  }

  runBatch(): void {
    if (!this.batchCells() || this.batchCells() > this.maxCells) return;
    this.batchRequest?.unsubscribe();
    this.batchLoading.set(true);
    this.batchError.set('');
    const params = batchParams({ universes: this.pickedMarkets(), periods: this.pickedPeriods(), strategy: this.strategy(), top: this.top(), rebalance: this.rebalance() });
    const runId = newRunId();
    this.startProgress(runId);
    this.batchRequest = this.api.backtestBatch(params, runId).subscribe({
      next: res => { this.stopProgress(); this.lastParams.batch = params; this.openedRun.set(null); this.batch.set(res); this.batchLoading.set(false); },
      error: err => { this.stopProgress(); this.batchError.set(describeHttpError(err)); this.batchLoading.set(false); },
    });
  }

  /** Asks the server how many backtests of this batch are done; a failed ask just shows no count. */
  private startProgress(runId: string): void {
    this.stopProgress();
    this.progressTimer = setInterval(() => {
      this.api.backtestProgress(runId).subscribe({
        next: raw => { if (this.batchLoading()) this.batchProgress.set(parseRunProgress(raw)); },
        error: () => undefined,
      });
    }, PROGRESS_POLL_MS);
  }

  private stopProgress(): void {
    clearInterval(this.progressTimer);
    this.progressTimer = undefined;
    this.batchProgress.set(null);
  }

  setMc<K extends keyof MonteCarloSettings>(key: K, value: MonteCarloSettings[K] | number | null): void {
    this.mc.update(m => ({ ...m, [key]: value === null || value === undefined ? '' : String(value) }));
  }

  runMonteCarlo(): void {
    const bad = monteCarloError(this.mc());
    this.mcError.set(bad);
    if (bad) return;
    this.mcRequest?.unsubscribe();
    this.mcLoading.set(true);
    const params = monteCarloParams({
      universe: this.universe(), strategy: this.strategy(), symbols: this.symbols(),
      top: this.top(), rebalance: this.rebalance(), years: this.years(),
      ...(this.years() === 0 ? { from: this.from(), to: this.to() || undefined } : {}),
    }, this.mc());
    this.mcRequest = this.api.backtestMonteCarlo(params).subscribe({
      next: res => { this.lastParams.montecarlo = params; this.openedRun.set(null); this.mcResult.set(res); this.mcLoading.set(false); },
      error: err => { this.mcError.set(describeHttpError(err)); this.mcLoading.set(false); },
    });
  }

  runSensitivity(): void {
    if (this.years() === 0) return;
    this.sensRequest?.unsubscribe();
    this.sensLoading.set(true);
    this.sensError.set('');
    const params = sensitivityParams({ universe: this.universe(), symbols: this.symbols(), strategy: this.strategy(), top: this.top(), rebalance: this.rebalance(), years: this.years() });
    this.sensRequest = this.api.backtestSensitivity(params).subscribe({
      next: res => { this.lastParams.sensitivity = params; this.openedRun.set(null); this.sens.set(res); this.sensLoading.set(false); },
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
    const params = singleParams({
      universe: this.universe(), strategy: this.strategy(), symbols: this.symbols(),
      top: this.top(), rebalance: this.rebalance(), years: this.years(),
      ...(this.years() === 0 ? { from: this.from(), to: this.to() || undefined } : {}),
    });
    this.request = this.api.backtest(params).subscribe({
      next: res => { this.lastParams.single = params; this.openedRun.set(null); this.result.set(res); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.result.set(null); this.openedRun.set(null); this.loading.set(false); },
    });
  }
}
