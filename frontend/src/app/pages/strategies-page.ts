import { ChangeDetectionStrategy, Component, DestroyRef, computed, effect, inject, signal, untracked } from '@angular/core';
import { HelpButtonComponent } from '../ui/help-button';
import { ActivatedRoute } from '@angular/router';
import type { Subscription } from 'rxjs';

import { StrategyBuilderComponent } from '../components/strategy-builder';
import {
  blankStrategy, chartSummary, cloneForEditing, describeStrategy, geometry, groupPresets, ownerLine, levelLines, linePath, nowText, problems, resultTiles, strategyPrompt, tradeMarkers,
} from '../components/strategy-view';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { PERIOD_PRESETS, parseSavedPeriods, periodError, periodRequest, withSavedPeriod, type PeriodChoice, type SavedPeriod } from '../core/strategy-period';
import { todayUtc } from '../ui/datetime';
import type { Persona, SavedStrategy, StrategyCatalog, StrategyDef, StrategyRunResult } from '../models';
import { MarketService, describeHttpError } from '../services';
import { SymbolSearchComponent } from '../shell/symbol-search';

const PERIODS_KEY = 'fs.strategy.periods';
const W = 720;
const H_PRICE = 260;
const H_EQUITY = 160;

/**
 * Fibonacci strategies and strategies built from rules, replayed over one
 * instrument's daily bars. The rules run on the server (internal/strategy);
 * this page builds them and shows what they would have done. It places no orders.
 */
@Component({
  selector: 'fs-strategies-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [HelpButtonComponent, SymbolSearchComponent, StrategyBuilderComponent],
  template: `
<div class="fs-page research-container">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">Research</div>
      <h1>Strategies <fs-help-button topic="strategies" /> @if (market.trackedSymbol()) { <span class="text-muted fw-normal">· {{ market.trackedSymbol() }}</span> }</h1>
      <p>Fibonacci strategies and your own rules, replayed over past daily prices. A test of an idea, not a forecast, and nothing is ordered.</p>
    </div>
    <fs-symbol-search class="page-search" [navigate]="false" placeholder="Test on a company or index…" />
  </header>

  <div class="layout">
    <aside class="list card" aria-label="Strategies">
      @for (g of groups(); track g.key) {
        <h2>{{ g.label }}</h2>
        @if (g.note) { <p class="grp-note">{{ g.note }}</p> }
        <ul>
          @for (p of g.items; track p.id) {
            <li><button type="button" class="pick" [attr.aria-current]="draftId() === p.id ? 'true' : null" (click)="choose(p, p.id!)">
              <strong>{{ p.name }}</strong><small>{{ p.direction === 'long' ? 'Long' : 'Short' }}</small></button></li>
          }
        </ul>
      }
      <h2>My strategies</h2>
      <ul>
        @for (s of catalog()?.custom ?? []; track s.id) {
          <li><button type="button" class="pick" [attr.aria-current]="draftId() === s.definition.id ? 'true' : null" (click)="choose(s.definition, s.definition.id!)">
            <strong>{{ s.definition.name }}</strong><small>{{ s.definition.direction === 'long' ? 'Long' : 'Short' }}</small></button></li>
        } @empty { <li class="muted">None yet.</li> }
      </ul>
      <button type="button" class="btn btn-sm btn-outline-secondary w-100" (click)="startNew()"><i class="bi bi-plus-lg"></i> New strategy</button>
    </aside>

    <main class="main">
      @if (draft(); as d) {
        <section class="card panel" aria-label="Strategy">
          <div class="panel-head">
            <div>
              <h2>{{ d.name || 'Untitled strategy' }}</h2>
              @if (owner(); as o) { <p class="owner"><i class="bi bi-person-badge" aria-hidden="true"></i> {{ o }}</p> }
              @if (d.description) { <p class="muted">{{ d.description }}</p> }
            </div>
            <div class="actions">
              @if (isPreset()) {
                <button type="button" class="btn btn-sm btn-outline-secondary" (click)="customize()"><i class="bi bi-pencil-square"></i> Customize a copy</button>
              } @else {
                <button type="button" class="btn btn-sm btn-primary" [disabled]="saving() || blocked().length > 0" (click)="save(false)"><i class="bi bi-save"></i> {{ savedId() ? 'Save' : 'Save strategy' }}</button>
                @if (savedId()) {
                  <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="saving() || blocked().length > 0" (click)="save(true)">Save as new</button>
                  <button type="button" class="btn btn-sm btn-outline-danger" [disabled]="saving()" (click)="remove()"><i class="bi bi-trash"></i> Delete</button>
                }
              }
            </div>
          </div>
          @if (notice(); as n) { <p class="notice" role="status">{{ n }}</p> }

          @if (isPreset()) {
            <div class="readback">@for (line of words(); track line) { <p>{{ line }}</p> }</div>
          } @else {
            <fs-strategy-builder [draft]="d" (draftChange)="draft.set($event)" />
          }

          <div class="run-bar">
            <div class="fs-segmented" role="group" aria-label="History">
              @for (r of ranges; track r) { <button type="button" [attr.aria-pressed]="range() === r" (click)="range.set(r)">{{ r }}</button> }
              <button type="button" [attr.aria-pressed]="range() === 'CUSTOM'" (click)="range.set('CUSTOM')">Custom</button>
            </div>
            @if (range() === 'CUSTOM') {
              <div class="custom-period">
                <label>From (UTC) <input type="date" class="form-control form-control-sm" min="1990-01-01" [max]="today" [value]="from()" (change)="from.set($any($event.target).value)" /></label>
                <label>To (UTC) <input type="date" class="form-control form-control-sm" min="1990-01-01" [value]="to()" (change)="to.set($any($event.target).value)" /></label>
                @if (periodProblem()) { <span class="text-danger small" role="alert">{{ periodProblem() }}</span> }
                @else {
                  <span class="saved-periods">
                    <input type="text" class="form-control form-control-sm" maxlength="40" placeholder="Name this period" aria-label="Name for this period" [value]="periodName()" (input)="periodName.set($any($event.target).value)" />
                    <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="!periodName().trim()" (click)="savePeriod()">Save period</button>
                  </span>
                }
                @for (p of savedPeriods(); track p.name) {
                  <span class="saved-chip">
                    <button type="button" class="btn btn-sm btn-outline-secondary" (click)="usePeriod(p)">{{ p.name }}</button>
                    <button type="button" class="btn btn-sm btn-link text-muted" [attr.aria-label]="'Remove saved period ' + p.name" (click)="removePeriod(p)"><i class="bi bi-x" aria-hidden="true"></i></button>
                  </span>
                }
              </div>
            }
            <label class="cost">Cost per side
              <input type="number" class="form-control form-control-sm" min="0" max="200" step="1" [value]="cost()" (change)="cost.set(+$any($event.target).value || 0)" /> bps
            </label>
            <button type="button" class="btn btn-primary" [disabled]="loading() || !market.trackedSymbol() || blocked().length > 0 || !!periodProblem()" (click)="run()">
              <i class="bi bi-play-fill"></i> Run test
            </button>
            @if (!market.trackedSymbol()) { <span class="muted">Pick an instrument first.</span> }
          </div>
        </section>

        @if (loading()) {
          <div class="progress rounded-0" style="height: 2px" role="progressbar" aria-label="Replaying the strategy"><div class="progress-bar progress-bar-striped progress-bar-animated w-100"></div></div>
        }
        @if (error(); as err) { <div class="fs-empty" role="alert"><i class="bi bi-exclamation-triangle"></i><h3>Could not run the test</h3><p class="mb-0">{{ err }}</p></div> }

        @if (result(); as r) {
          <section class="card panel" aria-label="Result">
            <div class="panel-head">
              <div><h2>Result · {{ r.symbol }}</h2><p class="muted">{{ r.from }} to {{ r.to }}, daily bars from {{ r.provider }}, costs {{ r.config.cost_bps }} bps per side.</p></div>
              <button type="button" class="btn btn-sm btn-outline-secondary" (click)="agent.ask(prompt(r))"><i class="bi bi-stars"></i> Assess</button>
            </div>
            <p class="now" [class.hit]="r.latest.entry_signal && !r.latest.in_position">{{ now(r) }}</p>

            <div class="tiles">
              @for (t of tiles(); track t.id) {
                <div class="card fs-kpi" [attr.data-tone]="t.tone" [title]="t.hint"><span class="fs-kpi-label">{{ t.label }}</span><span class="fs-kpi-value">{{ t.value }}</span></div>
              }
            </div>

            <figure class="chart">
              <svg [attr.viewBox]="'0 0 ' + w + ' ' + hp" role="img" [attr.aria-label]="summary()" preserveAspectRatio="none">
                @for (l of lines(); track l.label) { <line class="lvl" [class.ext]="l.ext" x1="0" [attr.x2]="w" [attr.y1]="l.y" [attr.y2]="l.y"><title>{{ l.label }}</title></line> }
                <path class="price" [attr.d]="pricePath()" fill="none" />
                @for (m of markers(); track $index) {
                  <circle [attr.cx]="m.x" [attr.cy]="m.y" r="4" [class]="'mk ' + m.kind + (m.win ? ' win' : ' loss')"><title>{{ m.label }}</title></circle>
                }
              </svg>
              <figcaption class="legend">
                <span><i class="sw price"></i>{{ r.symbol }} (100 at the start)</span>
                <span><i class="dot entry"></i>Entry</span><span><i class="dot exit"></i>Exit</span>
                @if (lines().length) { <span><i class="sw lvl"></i>Fibonacci levels of the latest swing ({{ r.latest.leg_low }}–{{ r.latest.leg_high }})</span> }
              </figcaption>
            </figure>

            <figure class="chart">
              <svg [attr.viewBox]="'0 0 ' + w + ' ' + he" role="img" aria-label="Strategy equity against buy and hold" preserveAspectRatio="none">
                <path class="hold" [attr.d]="holdPath()" fill="none" />
                <path class="equity" [attr.d]="equityPath()" fill="none" />
              </svg>
              <figcaption class="legend"><span><i class="sw equity"></i>Strategy</span><span><i class="sw hold"></i>Buy and hold</span></figcaption>
            </figure>

            <div class="alert alert-warning" role="note">
              <strong>Read this before trusting the result</strong>
              <ul class="mb-0">
                @if (r.notice) { <li>{{ r.notice }}</li> }
                @for (w of r.warnings; track w) { <li>{{ warningText(w) }}</li> }
                <li>Past results do not predict future ones.</li>
              </ul>
            </div>

            @if (r.walk_forward?.length) {
              <h3 class="h6 mt-3">Consistency across the period</h3>
              <p class="text-muted small mb-1">The history is cut into four consecutive windows and the trades are scored by when they were entered. The rules are the same in each, so a result that holds in only one window leans on that stretch.</p>
              <div class="fs-scroll-x">
                <table class="table table-sm align-middle">
                  <caption class="visually-hidden">Trades per window</caption>
                  <thead><tr><th scope="col">Window</th><th scope="col" class="text-end">Trades</th><th scope="col" class="text-end">Win rate</th><th scope="col" class="text-end">Strategy</th><th scope="col" class="text-end">Buy and hold</th></tr></thead>
                  <tbody>
                    @for (w of r.walk_forward; track w.from) {
                      <tr>
                        <td>{{ w.from }} – {{ w.to }}</td>
                        <td class="text-end">{{ w.trades }}</td>
                        <td class="text-end">{{ w.trades ? w.win_rate_pct.toFixed(0) + '%' : '–' }}</td>
                        <td class="text-end">{{ w.trades ? (w.return_pct >= 0 ? '+' : '') + w.return_pct.toFixed(1) + '%' : '–' }}</td>
                        <td class="text-end">{{ (w.buy_hold_return_pct >= 0 ? '+' : '') + w.buy_hold_return_pct.toFixed(1) }}%</td>
                      </tr>
                    }
                  </tbody>
                </table>
              </div>
            }

            <button type="button" class="btn btn-sm btn-ghost" (click)="showTrades.set(!showTrades())" [attr.aria-expanded]="showTrades()">{{ showTrades() ? 'Hide' : 'Show' }} {{ r.trades.length }} trades</button>
            @if (showTrades()) {
              <div class="fs-scroll-x">
                <table class="table table-sm align-middle mb-0">
                  <caption class="visually-hidden">Trades in the replay</caption>
                  <thead><tr><th scope="col">Entry</th><th scope="col" class="num">Price</th><th scope="col">Exit</th><th scope="col" class="num">Price</th><th scope="col">Why</th><th scope="col" class="num">Days</th><th scope="col" class="num">Return</th><th scope="col" class="num">R</th></tr></thead>
                  <tbody>
                    @for (t of r.trades; track $index) {
                      <tr><td>{{ t.entry_date }}</td><td class="num">{{ t.entry_price }}</td><td>{{ t.exit_date }}</td><td class="num">{{ t.exit_price }}</td><td>{{ t.reason }}</td>
                        <td class="num">{{ t.bars }}</td><td class="num" [class.pos]="t.return_pct > 0" [class.neg]="t.return_pct < 0">{{ t.return_pct > 0 ? '+' : '' }}{{ t.return_pct }}%</td><td class="num">{{ t.r ?? '' }}</td></tr>
                    }
                  </tbody>
                </table>
              </div>
            }
          </section>
        }
      } @else {
        <div class="fs-empty"><i class="bi bi-diagram-2"></i><h3>Choose a strategy</h3>
          <p class="mb-0">Pick a Fibonacci strategy on the left to read its rules and test it, or build your own.</p></div>
      }
    </main>
  </div>
</div>
  `,
  styles: [`
.page-search { flex: 0 1 380px; min-width: min(100%, 260px); }
.layout { display: grid; grid-template-columns: 17rem minmax(0, 1fr); gap: 1rem; align-items: start; }
.list { padding: 0.8rem; display: grid; gap: 0.5rem; }
.list h2 { font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--fs-muted); margin: 0.3rem 0 0; }
.grp-note { font-size: 0.72rem; color: var(--fs-muted); margin: -0.2rem 0 0; }
.owner { font-size: 0.8rem; color: var(--fs-accent-text, var(--fs-accent)); margin: 0.2rem 0 0; }
.list ul { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.25rem; }
.pick { width: 100%; text-align: left; background: transparent; border: 1px solid transparent; border-radius: 6px; padding: 0.4rem 0.5rem; color: var(--fs-text);
  display: flex; flex-direction: column; font-size: 0.85rem; small { color: var(--fs-muted); } }
.pick:hover { background: var(--fs-surface-3); }
.pick[aria-current='true'] { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
.main { display: grid; gap: 1rem; min-width: 0; }
.panel { padding: 1rem 1.25rem; display: grid; gap: 0.9rem; min-width: 0; }
.panel-head { display: flex; flex-wrap: wrap; gap: 0.6rem; justify-content: space-between; align-items: flex-start; }
.panel h2 { font-size: 1.05rem; margin: 0; }
.actions { display: flex; flex-wrap: wrap; gap: 0.4rem; }
.muted, .readback p { color: var(--fs-muted); font-size: 0.82rem; margin: 0.2rem 0 0; }
.readback p { color: var(--fs-text-2, var(--fs-text)); font-size: 0.88rem; }
.notice { font-size: 0.82rem; color: var(--fs-pos); margin: 0; }
.custom-period { display: flex; flex-wrap: wrap; gap: 0.5rem 0.75rem; align-items: end; width: 100%; label { display: grid; gap: 0.15rem; font-size: 0.8125rem; color: var(--fs-muted); } }
.saved-periods, .saved-chip { display: inline-flex; gap: 0.35rem; align-items: center; }
.run-bar { display: flex; flex-wrap: wrap; gap: 0.75rem; align-items: center; border-top: 1px solid var(--fs-border); padding-top: 0.8rem; }
.cost { display: flex; align-items: center; gap: 0.4rem; font-size: 0.8rem; color: var(--fs-muted); input { width: 5rem; } }
.now { font-size: 0.88rem; margin: 0; }
.now.hit { color: var(--fs-pos); font-weight: 600; }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(8.5rem, 1fr)); gap: 0.6rem; }
.tiles .fs-kpi { padding: 0.7rem 0.85rem; .fs-kpi-value { font-size: 1.1rem; } }
.fs-kpi[data-tone='pos'] .fs-kpi-value { color: var(--fs-pos); }
.fs-kpi[data-tone='neg'] .fs-kpi-value { color: var(--fs-neg); }
.chart { margin: 0; svg { width: 100%; display: block; background: var(--fs-surface-2, transparent); border-radius: 8px; } }
.chart:nth-of-type(1) svg { height: 16rem; } .chart:nth-of-type(2) svg { height: 10rem; }
path { vector-effect: non-scaling-stroke; }
.price { stroke: var(--fs-text-2, var(--fs-text)); stroke-width: 1.5; }
.equity { stroke: var(--fs-accent); stroke-width: 2; }
.hold { stroke: var(--fs-muted); stroke-width: 1.75; stroke-dasharray: 6 4; }
.lvl { stroke: var(--fs-warn); stroke-width: 1; stroke-dasharray: 3 4; opacity: 0.75; vector-effect: non-scaling-stroke; }
.lvl.ext { stroke: var(--fs-accent); }
.mk { stroke: var(--fs-bg); stroke-width: 1; }
.mk.entry { fill: var(--fs-accent); }
.mk.exit.win { fill: var(--fs-pos); } .mk.exit.loss { fill: var(--fs-neg); }
.legend { display: flex; flex-wrap: wrap; gap: 0.4rem 1.2rem; font-size: 0.75rem; color: var(--fs-muted); margin-top: 0.4rem; span { display: inline-flex; align-items: center; gap: 0.35rem; } }
.sw { display: inline-block; width: 1.5rem; height: 0; border-top: 2px solid currentColor; }
.sw.price { color: var(--fs-text-2, var(--fs-text)); } .sw.equity { color: var(--fs-accent); } .sw.hold { color: var(--fs-muted); border-top-style: dashed; } .sw.lvl { color: var(--fs-warn); border-top-style: dotted; }
.dot { width: 0.6rem; height: 0.6rem; border-radius: 50%; display: inline-block; } .dot.entry { background: var(--fs-accent); } .dot.exit { background: var(--fs-pos); }
.num { text-align: right; white-space: nowrap; } .pos { color: var(--fs-pos); } .neg { color: var(--fs-neg); }
@media (max-width: 991.98px) { .layout { grid-template-columns: minmax(0, 1fr); } }
  `],
})
export class StrategiesPage {
  readonly market = inject(MarketState);
  readonly agent = inject(AgentSession);
  private readonly api = inject(MarketService);
  private readonly route = inject(ActivatedRoute);

  readonly w = W;
  readonly hp = H_PRICE;
  readonly he = H_EQUITY;
  readonly ranges = PERIOD_PRESETS;
  readonly today = todayUtc();
  readonly prompt = strategyPrompt;
  readonly now = nowText;

  readonly catalog = signal<StrategyCatalog | null>(null);
  readonly personas = signal<Persona[]>([]);
  readonly draft = signal<StrategyDef | null>(null);
  /** The id of the strategy shown: a preset id, "custom:<n>", or null for an unsaved one. */
  readonly draftId = signal<string | null>(null);
  readonly range = signal<PeriodChoice>('5Y');
  readonly from = signal('');
  readonly to = signal('');
  readonly periodName = signal('');
  readonly savedPeriods = signal<SavedPeriod[]>(this.loadPeriods());
  /** Why the custom dates cannot be sent, or ''. */
  readonly periodProblem = computed(() => this.range() === 'CUSTOM' ? periodError(this.from(), this.to(), this.today) : '');
  readonly cost = signal(10);
  readonly result = signal<StrategyRunResult | null>(null);
  readonly loading = signal(false);
  readonly saving = signal(false);
  readonly error = signal('');
  readonly notice = signal('');
  readonly showTrades = signal(false);
  private request?: Subscription;

  readonly isPreset = computed(() => !!this.draftId() && !this.draftId()!.startsWith('custom:'));
  readonly savedId = computed(() => {
    const id = this.draftId();
    return id?.startsWith('custom:') ? Number(id.slice(7)) : null;
  });
  readonly groups = computed(() => groupPresets(this.catalog()?.presets ?? [], this.personas()));
  readonly owner = computed(() => (this.draft() && this.isPreset() ? ownerLine(this.draft()!, this.personas()) : ''));
  readonly words = computed(() => (this.draft() ? describeStrategy(this.draft()!) : []));
  readonly blocked = computed(() => (this.draft() && !this.isPreset() ? problems(this.draft()!) : []));

  readonly tiles = computed(() => (this.result() ? resultTiles(this.result()!) : []));
  readonly summary = computed(() => (this.result() ? chartSummary(this.result()!) : ''));
  readonly warningText = (id: string) => this.catalog()?.warnings?.[id] ?? id.replace(/_/g, ' ');

  private readonly priceGeometry = computed(() => {
    const r = this.result();
    if (!r) return null;
    const values = [...r.curve.map(p => p.close), ...(r.latest.levels ?? []).map(l => l.price / r.base_price * 100)];
    return geometry(values, r.curve.length, W, H_PRICE);
  });
  readonly pricePath = computed(() => (this.result() && this.priceGeometry() ? linePath(this.result()!.curve.map(p => p.close), this.priceGeometry()!) : ''));
  readonly markers = computed(() => (this.result() && this.priceGeometry() ? tradeMarkers(this.result()!, this.priceGeometry()!) : []));
  readonly lines = computed(() => (this.result() && this.priceGeometry() ? levelLines(this.result()!, this.priceGeometry()!) : []));
  private readonly equityGeometry = computed(() => {
    const r = this.result();
    return r ? geometry([...r.curve.map(p => p.equity), ...r.curve.map(p => p.close)], r.curve.length, W, H_EQUITY) : null;
  });
  readonly equityPath = computed(() => (this.result() && this.equityGeometry() ? linePath(this.result()!.curve.map(p => p.equity), this.equityGeometry()!) : ''));
  readonly holdPath = computed(() => (this.result() && this.equityGeometry() ? linePath(this.result()!.curve.map(p => p.close), this.equityGeometry()!) : ''));

  constructor() {
    inject(DestroyRef).onDestroy(() => this.request?.unsubscribe());
    this.api.getPersonas().subscribe({ next: c => this.personas.set(c.personas), error: () => {} });
    this.reload(true);
    // A new instrument re-runs the strategy on show, so the picture follows the symbol.
    effect(() => {
      const symbol = this.market.trackedSymbol();
      untracked(() => { if (symbol && this.draft() && this.result() && !this.blocked().length) this.run(); });
    });
  }

  private reload(selectFirst = false): void {
    this.api.getStrategies().subscribe({
      next: c => {
        this.catalog.set(c);
        if (selectFirst && c.presets.length) {
          // A link from a character's card can name the strategy to open.
          const wanted = this.route.snapshot.queryParamMap.get('strategy');
          const first = c.presets.find(p => p.id === wanted) ?? c.presets[0];
          this.choose(first, first.id!);
        }
      },
      error: err => this.error.set(describeHttpError(err)),
    });
  }

  choose(def: StrategyDef, id: string): void {
    this.draft.set(cloneForEditing(def, false));
    this.draftId.set(id);
    this.result.set(null);
    this.error.set('');
    this.notice.set('');
    if (this.market.trackedSymbol()) this.run();
  }

  startNew(): void {
    this.draft.set(blankStrategy());
    this.draftId.set(null);
    this.result.set(null);
    this.error.set('');
    this.notice.set('');
  }

  customize(): void {
    const d = this.draft();
    if (!d) return;
    this.draft.set(cloneForEditing(d, true));
    this.draftId.set(null);
    this.notice.set('This is your own copy. Change the rules, run it, and save it when you like it.');
  }

  private loadPeriods(): SavedPeriod[] {
    try { return parseSavedPeriods(localStorage.getItem(PERIODS_KEY)); } catch { return []; }
  }

  private storePeriods(list: SavedPeriod[]): void {
    this.savedPeriods.set(list);
    try { localStorage.setItem(PERIODS_KEY, JSON.stringify(list)); } catch { /* Saved periods stay for this visit only. */ }
  }

  savePeriod(): void {
    if (this.periodProblem() || !this.periodName().trim()) return;
    this.storePeriods(withSavedPeriod(this.savedPeriods(), { name: this.periodName(), from: this.from(), to: this.to() }));
    this.periodName.set('');
  }

  usePeriod(p: SavedPeriod): void { this.range.set('CUSTOM'); this.from.set(p.from); this.to.set(p.to); }

  removePeriod(p: SavedPeriod): void { this.storePeriods(this.savedPeriods().filter(x => x.name !== p.name)); }

  run(): void {
    const d = this.draft();
    const symbol = this.market.trackedSymbol();
    if (!d || !symbol) return;
    this.request?.unsubscribe();
    this.loading.set(true);
    this.error.set('');
    const common = { symbol, provider: this.market.provider(), ...periodRequest(this.range(), this.from(), this.to()), cost_bps: this.cost() };
    const req = this.isPreset() ? { ...common, strategy_id: this.draftId()! } : { ...common, definition: d };
    this.request = this.api.runStrategy(req).subscribe({
      next: r => { this.result.set(r); this.loading.set(false); },
      error: err => { this.error.set(describeHttpError(err)); this.result.set(null); this.loading.set(false); },
    });
  }

  save(asNew: boolean): void {
    const d = this.draft();
    if (!d || this.blocked().length) return;
    this.saving.set(true);
    this.notice.set('');
    const id = this.savedId();
    const body: StrategyDef = { ...d, id: undefined, name: asNew && id ? `${d.name} (2)`.slice(0, 60) : d.name };
    const call = id && !asNew ? this.api.updateStrategy(id, body) : this.api.createStrategy(body);
    call.subscribe({
      next: (s: SavedStrategy) => {
        this.saving.set(false);
        this.draft.set(s.definition);
        this.draftId.set(s.definition.id!);
        this.notice.set('Saved.');
        this.reload();
      },
      error: err => { this.saving.set(false); this.notice.set(''); this.error.set(describeHttpError(err)); },
    });
  }

  remove(): void {
    const id = this.savedId();
    if (!id || !confirm('Delete this strategy? This cannot be undone.')) return;
    this.saving.set(true);
    this.api.deleteStrategy(id).subscribe({
      next: () => { this.saving.set(false); this.draft.set(null); this.draftId.set(null); this.result.set(null); this.reload(); },
      error: err => { this.saving.set(false); this.error.set(describeHttpError(err)); },
    });
  }
}
