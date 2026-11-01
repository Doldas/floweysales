import { ChangeDetectionStrategy, Component, DestroyRef, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { Subject, debounceTime, switchMap } from 'rxjs';

import { Access } from '../core/access';
import { INVESTOR_PLAYBOOKS } from '../components/investor-nodes';
import { EMPTY_FORM, allocationRows, fanPaths, fanSummary, formFromProfile, milestoneDots, phaseSteps, profileFromForm, type PlanForm } from '../components/plan-view';
import type { PlanProfile, WealthPlan } from '../models';
import { MarketService, describeHttpError } from '../services';
import { formatMoney } from '../ui/format';

const CHART_W = 680;
const CHART_H = 260;

/**
 * Wealth plan: which phase of building wealth the investor is in, what that
 * phase needs, a target allocation and a seeded projection of how likely the
 * goal is. Everything is computed by the server (internal/planner).
 */
@Component({
  selector: 'fs-plan-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [RouterLink],
  template: `
<div class="fs-page">
  <header class="fs-page-header">
    <div>
      <div class="fs-eyebrow">Plan</div>
      <h1>Wealth plan</h1>
      <p>Where you are, what that phase needs and how likely your goal is. A plan to discuss, not advice.</p>
    </div>
  </header>

  @if (loadError(); as err) { <div class="alert alert-danger" role="alert">{{ err }}</div> }

  @if (plan(); as p) {
    <section class="card phase" aria-labelledby="phase-title">
      <div class="card-body">
        <div class="fs-eyebrow">Your phase</div>
        <h2 id="phase-title" class="phase-name">{{ p.phase.name }}</h2>
        <p class="phase-job">{{ p.phase.job }}</p>
        <ol class="track" aria-label="Phases of building wealth">
          @for (s of steps(); track s.id) {
            <li [attr.aria-current]="s.state === 'current' ? 'step' : null" [attr.data-state]="s.state">
              <span class="dot" aria-hidden="true">@if (s.state === 'done') { <i class="bi bi-check-lg"></i> } @else { {{ $index + 1 }} }</span>
              <span class="step-label">{{ s.label }}@if (s.state === 'current') { <span class="visually-hidden"> (current phase)</span> }</span>
            </li>
          }
        </ol>
      </div>
    </section>
  } @else if (!loading()) {
    <div class="fs-empty">
      <i class="bi bi-bullseye"></i>
      <h3>Set up your plan</h3>
      <p class="mb-0">Fill in your monthly expenses below. Five more numbers give you a phase, an allocation and a projection.</p>
    </div>
  }

  <div class="plan-grid">
    <section class="card" aria-labelledby="form-title">
      <div class="card-header"><h2 class="card-title" id="form-title">Your numbers</h2></div>
      <form class="card-body form-grid" (submit)="$event.preventDefault(); save()">
        @for (f of numberFields; track f.key) {
          <div>
            <label class="form-label" [for]="'pf-' + f.key">{{ f.label }}</label>
            <input class="form-control" [id]="'pf-' + f.key" type="number" min="0" inputmode="decimal" [attr.step]="f.step ?? 'any'" [attr.aria-describedby]="f.hint ? 'pf-' + f.key + '-hint' : null"
                   [value]="form()[f.key]" (input)="set(f.key, $any($event.target).value)">
            @if (f.hint) { <div class="form-text" [id]="'pf-' + f.key + '-hint'">{{ f.hint }}</div> }
          </div>
        }
        <div>
          <label class="form-label" for="pf-risk">Risk profile</label>
          <select class="form-select" id="pf-risk" [value]="form().risk_profile" (change)="set('risk_profile', $any($event.target).value)">
            <option value="conservative">Conservative</option>
            <option value="balanced">Balanced</option>
            <option value="growth">Growth</option>
            <option value="aggressive">Aggressive</option>
          </select>
        </div>
        <div>
          <label class="form-label" for="pf-currency">Currency</label>
          <input class="form-control" id="pf-currency" maxlength="3" autocomplete="off" placeholder="USD" [value]="form().currency" (input)="set('currency', $any($event.target).value)">
        </div>
        <div class="form-check">
          <input class="form-check-input" type="checkbox" id="pf-retired" [checked]="form().retired" (change)="set('retired', $any($event.target).checked)">
          <label class="form-check-label" for="pf-retired">Already living off the portfolio</label>
        </div>
        @if (access.liveMarkets()) {
          <div class="form-check">
            <input class="form-check-input" type="checkbox" id="pf-paper" [checked]="form().use_paper_portfolio" (change)="set('use_paper_portfolio', $any($event.target).checked)">
            <label class="form-check-label" for="pf-paper">Use my paper portfolio as "invested"</label>
          </div>
        }
        <div class="form-actions">
          <button type="submit" class="btn btn-primary" [disabled]="saving() || !valid()">{{ saving() ? 'Saving…' : 'Save plan' }}</button>
          <span class="form-note" role="status">
            @if (saveError(); as e) { <span class="text-neg">{{ e }}</span> }
            @else if (savedAt()) { Saved. }
            @else if (valid() && dirty()) { What if: not saved yet. }
          </span>
        </div>
      </form>
    </section>

    @if (plan(); as p) {
      <div class="results">
        <section class="tiles" aria-label="Plan summary">
          <div class="card fs-kpi">
            <span class="fs-kpi-label">{{ income() ? 'Money lasts' : 'Chance of reaching goal' }}</span>
            <span class="fs-kpi-value">{{ p.probability_goal_pct.toFixed(0) }}%</span>
            <span class="fs-kpi-sub">{{ income() ? 'of simulated paths never run out' : 'goal ' + money(p.goal_amount) + ' in today\\'s money' }}</span>
          </div>
          @if (!income()) {
            <div class="card fs-kpi">
              <span class="fs-kpi-label">Years to goal (median)</span>
              <span class="fs-kpi-value">{{ p.years_to_goal_p50 !== undefined ? p.years_to_goal_p50 : 'Not within ' + p.horizon_years }}</span>
              <span class="fs-kpi-sub">years</span>
            </div>
            <div class="card fs-kpi">
              <span class="fs-kpi-label">Needed per month for 75 %</span>
              <span class="fs-kpi-value">{{ p.required_contribution_75 !== undefined ? money(p.required_contribution_75) : '—' }}</span>
              <span class="fs-kpi-sub">{{ p.required_contribution_75 !== undefined ? 'monthly contribution' : 'out of reach with these numbers' }}</span>
            </div>
          }
          <div class="card fs-kpi">
            <span class="fs-kpi-label">Financial independence number</span>
            <span class="fs-kpi-value">{{ money(p.fi_number) }}</span>
            <span class="fs-kpi-sub">{{ p.fi_ratio_pct.toFixed(1) }}% there · 25× yearly expenses, a rule of thumb</span>
          </div>
        </section>

        <section class="card" aria-labelledby="chart-title">
          <div class="card-header chart-head">
            <h2 class="card-title me-auto" id="chart-title">Projection</h2>
            <div class="fs-segmented" role="group" aria-label="Money">
              <button type="button" [attr.aria-pressed]="real()" (click)="real.set(true)">Today's money</button>
              <button type="button" [attr.aria-pressed]="!real()" (click)="real.set(false)">Nominal</button>
            </div>
          </div>
          <div class="card-body">
            <svg class="fan" [attr.viewBox]="'0 0 ' + w + ' ' + h" role="img" [attr.aria-label]="summary()" preserveAspectRatio="none">
              <path class="band" [attr.d]="fan().band" />
              <path class="median" [attr.d]="fan().median" fill="none" />
              @if (fan().goalY !== null) { <line class="goal" x1="0" [attr.x2]="w" [attr.y1]="fan().goalY" [attr.y2]="fan().goalY" /> }
              @for (d of dots(); track d.label) { <circle class="mile" [attr.cx]="d.x" [attr.cy]="d.y" r="4"><title>{{ d.label }} in year {{ d.year }}</title></circle> }
            </svg>
            <ul class="legend">
              <li><span class="swatch band-sw" aria-hidden="true"></span>Likely range (10th–90th percentile)</li>
              <li><span class="swatch median-sw" aria-hidden="true"></span>Median</li>
              @if (fan().goalY !== null) { <li><span class="swatch goal-sw" aria-hidden="true"></span>Goal {{ money(p.goal_amount) }}</li> }
              @if (dots().length) { <li><span class="swatch dot-sw" aria-hidden="true"></span>Milestones: {{ milestoneText() }}</li> }
            </ul>
            <details class="mt-2">
              <summary>Show the numbers</summary>
              <div class="fs-scroll-x">
                <table class="table table-sm mb-0">
                  <caption class="visually-hidden">Projected wealth by year</caption>
                  <thead><tr><th scope="col">Year</th><th scope="col" class="num">Low (10th)</th><th scope="col" class="num">Median</th><th scope="col" class="num">High (90th)</th></tr></thead>
                  <tbody>
                    @for (b of tableBands(); track b.year) {
                      <tr><th scope="row">{{ b.year }}@if (b.age) { <small> · age {{ b.age }}</small> }</th>
                        <td class="num">{{ money(real() ? b.real_p10 : b.p10) }}</td><td class="num">{{ money(real() ? b.real_p50 : b.p50) }}</td><td class="num">{{ money(real() ? b.real_p90 : b.p90) }}</td></tr>
                    }
                  </tbody>
                </table>
              </div>
            </details>
          </div>
        </section>

        <section class="card" aria-labelledby="alloc-title">
          <div class="card-header"><h2 class="card-title" id="alloc-title">Target allocation</h2></div>
          <div class="card-body">
            <div class="stack" role="img" [attr.aria-label]="allocationLabel()">
              @for (r of allocation(); track r.id) { @if (r.pct > 0) { <span [attr.data-asset]="r.id" [style.width.%]="r.pct">{{ r.pct }}%</span> } }
            </div>
            <table class="table table-sm mb-0 mt-2">
              <caption class="visually-hidden">Target allocation by asset class</caption>
              <thead><tr><th scope="col">Asset class</th><th scope="col" class="num">Target</th></tr></thead>
              <tbody>
                @for (r of allocation(); track r.id) {
                  <tr><th scope="row"><span class="swatch" [attr.data-asset]="r.id" aria-hidden="true"></span>{{ r.label }}@if (r.note && r.pct > 0) { <small class="d-block text-muted">{{ r.note }}</small> }</th><td class="num">{{ r.pct }}%</td></tr>
                }
              </tbody>
            </table>
          </div>
        </section>

        <section class="card" aria-labelledby="todo-title">
          <div class="card-header"><h2 class="card-title" id="todo-title">What this phase needs</h2></div>
          <div class="card-body">
            <ul class="checklist">@for (item of p.phase.checklist; track item) { <li>{{ item }}</li> }</ul>
            @if (playbooks().length) {
              <h3 class="sub">Recommended automations</h3>
              <div class="d-flex flex-wrap gap-2">
                @for (pb of playbooks(); track pb.id) {
                  @if (access.flowsWrite()) {
                    <a class="btn btn-sm btn-outline-secondary" routerLink="/flows" [queryParams]="{ template: pb.id }"><i class="bi" [class]="pb.icon" aria-hidden="true"></i>{{ pb.label }}</a>
                  } @else { <span class="fs-chip">{{ pb.label }}</span> }
                }
              </div>
            } @else {
              <p class="text-muted mb-0">No investing automations yet: this phase is about debt and a buffer.</p>
            }
          </div>
        </section>

        <details class="card assumptions">
          <summary class="card-header">Assumptions</summary>
          <div class="card-body">
            <table class="table table-sm">
              <caption class="visually-hidden">Capital-market assumptions behind the projection</caption>
              <thead><tr><th scope="col">Asset</th><th scope="col" class="num">Expected return</th><th scope="col" class="num">Volatility</th></tr></thead>
              <tbody>
                @for (a of assumptionRows(); track a.label) { <tr><th scope="row">{{ a.label }}</th><td class="num">{{ a.ret }}%</td><td class="num">{{ a.vol }}%</td></tr> }
              </tbody>
            </table>
            <p class="mb-1">Inflation {{ p.assumptions.inflation_pct }}%, {{ p.assumptions.paths }} simulated paths.</p>
            <p class="mb-0 text-muted">{{ p.assumptions.note }}</p>
          </div>
        </details>
      </div>
    }
  </div>

  <p class="disclosure">Research aid, not investment advice. Projections are simulations from the assumptions above, not forecasts, and nothing here trades for you.</p>
</div>
  `,
  styles: [`
:host { display: block; }
.phase-name { font-size: 1.6rem; margin: 0.1rem 0; }
.phase-job { color: var(--fs-text-2); max-width: 60ch; }
.track { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 0.5rem; list-style: none; padding: 0; margin: 1rem 0 0;
  li { display: flex; flex-direction: column; align-items: center; gap: 0.35rem; text-align: center; font-size: 0.78rem; color: var(--fs-muted); }
  .dot { display: grid; place-items: center; width: 1.8rem; height: 1.8rem; border-radius: 50%; border: 2px solid var(--fs-border-strong); font-weight: 600; }
  li[data-state='done'] .dot { background: var(--fs-pos-soft); border-color: var(--fs-pos); color: var(--fs-pos); }
  li[data-state='current'] { color: var(--fs-text); font-weight: 650; }
  li[data-state='current'] .dot { background: var(--fs-accent); border-color: var(--fs-accent); color: #fff; } }
@media (max-width: 575.98px) { .track { grid-template-columns: repeat(3, minmax(0, 1fr)); row-gap: 0.9rem; } }
.plan-grid { display: grid; grid-template-columns: minmax(18rem, 22rem) minmax(0, 1fr); gap: 1rem; align-items: start; margin-top: 1rem; }
@media (max-width: 991.98px) { .plan-grid { grid-template-columns: minmax(0, 1fr); } }
.form-grid { display: grid; gap: 0.8rem; }
.form-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 0.75rem; }
.form-note { font-size: 0.8rem; color: var(--fs-muted); }
.results { display: grid; gap: 1rem; min-width: 0; }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr)); gap: 0.75rem; }
.chart-head { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; }
.fan { width: 100%; height: 16rem; display: block; }
.band { fill: color-mix(in srgb, var(--fs-accent) 22%, transparent); stroke: none; }
.median { stroke: var(--fs-accent); stroke-width: 2.5; vector-effect: non-scaling-stroke; }
.goal { stroke: var(--fs-text-2); stroke-width: 1.5; stroke-dasharray: 6 4; vector-effect: non-scaling-stroke; }
.mile { fill: var(--fs-surface, #fff); stroke: var(--fs-accent); stroke-width: 2; vector-effect: non-scaling-stroke; }
.legend { display: flex; flex-wrap: wrap; gap: 0.4rem 1.25rem; list-style: none; padding: 0; margin: 0.6rem 0 0; font-size: 0.78rem; color: var(--fs-muted);
  li { display: inline-flex; align-items: center; gap: 0.4rem; } }
.swatch { display: inline-block; width: 0.9rem; height: 0.9rem; border-radius: 3px; margin-right: 0.4rem; vertical-align: -0.1rem; }
.legend .swatch { margin-right: 0; }
.band-sw { background: color-mix(in srgb, var(--fs-accent) 30%, transparent); }
.median-sw { height: 3px; background: var(--fs-accent); }
.goal-sw { height: 0; border-top: 2px dashed var(--fs-text-2); border-radius: 0; }
.dot-sw { border-radius: 50%; border: 2px solid var(--fs-accent); }
.stack { display: flex; height: 2rem; border-radius: 6px; overflow: hidden; background: var(--fs-surface-3);
  span { display: grid; place-items: center; font-size: 0.75rem; font-weight: 650; color: #fff; min-width: 0; overflow: hidden; } }
[data-asset='core_equity'] { background: #2f6fed; }
[data-asset='bonds'] { background: #176a54; }
[data-asset='cash'] { background: #6b7280; }
[data-asset='satellite'] { background: #b45309; }
.num { text-align: right; white-space: nowrap; }
.checklist { padding-left: 1.1rem; margin-bottom: 0.75rem; li { margin-bottom: 0.3rem; } }
.sub { font-size: 0.85rem; text-transform: uppercase; letter-spacing: 0.04em; color: var(--fs-muted); margin: 1rem 0 0.5rem; }
.assumptions > summary { cursor: pointer; }
.disclosure { font-size: 0.78rem; color: var(--fs-muted); margin-top: 1rem; }
  `],
})
export class PlanPage {
  readonly access = inject(Access);
  private readonly api = inject(MarketService);

  readonly w = CHART_W;
  readonly h = CHART_H;
  readonly numberFields: { key: keyof PlanForm & string; label: string; hint?: string; step?: string }[] = [
    { key: 'monthly_expenses', label: 'Monthly expenses', hint: 'Required. Your living costs per month.' },
    { key: 'emergency_fund', label: 'Emergency fund (cash)' },
    { key: 'high_interest_debt', label: 'Debt above 8 % interest' },
    { key: 'invested', label: 'Invested today' },
    { key: 'monthly_contribution', label: 'Monthly contribution' },
    { key: 'age', label: 'Age', step: '1' },
    { key: 'retirement_age', label: 'Target retirement age', step: '1' },
    { key: 'goal_amount', label: 'Goal amount', hint: 'In today\'s money. Empty means the financial independence number.' },
  ];

  readonly form = signal<PlanForm>({ ...EMPTY_FORM });
  readonly plan = signal<WealthPlan | null>(null);
  readonly loading = signal(true);
  readonly loadError = signal('');
  readonly saving = signal(false);
  readonly saveError = signal('');
  readonly savedAt = signal(false);
  readonly dirty = signal(false);
  readonly real = signal(true);
  private readonly changes = new Subject<PlanProfile>();

  readonly profile = computed(() => profileFromForm(this.form()));
  readonly valid = computed(() => this.profile() !== null);
  readonly income = computed(() => this.plan()?.phase.id === 'income');
  readonly currency = computed(() => this.form().currency.trim());
  readonly steps = computed(() => phaseSteps(this.plan()?.phase.id));
  readonly fan = computed(() => fanPaths(this.plan()?.bands ?? [], CHART_W, CHART_H, this.real(), this.real() ? this.plan()?.goal_amount ?? 0 : 0));
  readonly dots = computed(() => (this.real() ? milestoneDots(this.plan()?.milestones ?? [], this.plan()?.bands ?? [], CHART_W, CHART_H, this.fan().max) : []));
  readonly summary = computed(() => fanSummary(this.plan()?.bands ?? [], this.real(), this.plan()?.goal_amount ?? 0, this.currency(), formatMoney));
  readonly allocation = computed(() => allocationRows(this.plan()?.target_allocation ?? { core_equity: 0, bonds: 0, cash: 100, satellite: 0 }));
  readonly allocationLabel = computed(() => 'Target allocation: ' + this.allocation().filter(r => r.pct > 0).map(r => `${r.label} ${r.pct} percent`).join(', '));
  readonly tableBands = computed(() => (this.plan()?.bands ?? []).filter(b => b.year % 5 === 0 || b.year === this.plan()!.horizon_years));
  readonly milestoneText = computed(() =>
    this.dots().map(d => `${/^\d+$/.test(d.label) ? formatMoney(Number(d.label), this.currency()) : d.label} (year ${d.year})`).join(', '));
  readonly playbooks = computed(() => {
    const ids = this.plan()?.phase.playbooks ?? [];
    return INVESTOR_PLAYBOOKS.filter(p => ids.includes(p.id) && (!p.adminOnly || this.access.isAdmin()));
  });
  readonly assumptionRows = computed(() => {
    const a = this.plan()?.assumptions;
    return a ? [
      { label: 'Core equity', ret: a.core_equity.expected_return_pct, vol: a.core_equity.volatility_pct },
      { label: 'Bonds', ret: a.bonds.expected_return_pct, vol: a.bonds.volatility_pct },
      { label: 'Cash', ret: a.cash.expected_return_pct, vol: a.cash.volatility_pct },
      { label: 'Satellite', ret: a.satellite.expected_return_pct, vol: a.satellite.volatility_pct },
    ] : [];
  });

  constructor() {
    const sub = this.changes.pipe(debounceTime(400), switchMap(p => this.api.previewPlan(p))).subscribe({
      next: res => { if (res.plan) this.plan.set(res.plan); },
      error: err => this.saveError.set(describeHttpError(err)),
    });
    inject(DestroyRef).onDestroy(() => sub.unsubscribe());
    this.api.getPlan().subscribe({
      next: res => {
        this.form.set(formFromProfile(res.profile));
        this.plan.set(res.plan);
        this.loading.set(false);
      },
      error: err => { this.loadError.set(describeHttpError(err)); this.loading.set(false); },
    });
  }

  money = (v: number): string => formatMoney(v, this.currency());

  set<K extends keyof PlanForm>(key: K, value: PlanForm[K]): void {
    this.form.update(f => ({ ...f, [key]: value }));
    this.dirty.set(true);
    this.savedAt.set(false);
    this.saveError.set('');
    const p = this.profile();
    if (p) this.changes.next(p);
  }

  save(): void {
    const p = this.profile();
    if (!p) return;
    this.saving.set(true);
    this.saveError.set('');
    this.api.savePlan(p).subscribe({
      next: res => { this.plan.set(res.plan); this.saving.set(false); this.dirty.set(false); this.savedAt.set(true); },
      error: err => { this.saveError.set(describeHttpError(err)); this.saving.set(false); },
    });
  }
}
