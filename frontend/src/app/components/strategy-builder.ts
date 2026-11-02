import { ChangeDetectionStrategy, Component, computed, model } from '@angular/core';

import type { StrategyComparison, StrategyCondition, StrategyDef, StrategyLevel } from '../models';
import { OperandEditorComponent } from './operand-editor';
import { COMPARISONS, defaultOperand, describeStrategy, problems } from './strategy-view';

type RuleList = 'entry' | 'exit';

/**
 * Builds a strategy from rules: entry conditions, an optional exit, stop,
 * target, day limit and sizing. Every change goes back through `draft`, so
 * the page owns the strategy and decides when to run or save it.
 */
@Component({
  selector: 'fs-strategy-builder',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [OperandEditorComponent],
  template: `
<form class="builder" (submit)="$event.preventDefault()" aria-label="Strategy builder">
  <div class="row-fields">
    <label class="grow">Name
      <input class="form-control form-control-sm" maxlength="60" [value]="draft().name" (input)="patch({ name: $any($event.target).value })" placeholder="My pullback strategy" />
    </label>
    <label>Direction
      <select class="form-select form-select-sm" (change)="patch({ direction: $any($event.target).value })">
        <option value="long" [selected]="draft().direction === 'long'">Long (buy)</option>
        <option value="short" [selected]="draft().direction === 'short'">Short (sell)</option>
      </select>
    </label>
  </div>
  <label>Notes
    <textarea class="form-control form-control-sm" rows="2" maxlength="400" [value]="draft().description ?? ''" (input)="patch({ description: $any($event.target).value })"></textarea>
  </label>

  <fieldset>
    <legend>Enter when all of these hold</legend>
    @for (c of draft().entry; track $index) {
      <div class="rule">
        <fs-operand-editor [operand]="c.left" (operandChange)="setCondition('entry', $index, { left: $event })" [label]="'Entry ' + ($index + 1) + ' left'" />
        <select class="form-select form-select-sm cmp" [attr.aria-label]="'Entry ' + ($index + 1) + ' comparison'" (change)="setCondition('entry', $index, { op: $any($event.target).value })">
          @for (o of comparisons; track o.op) { <option [value]="o.op" [selected]="o.op === c.op">{{ o.label }}</option> }
        </select>
        <fs-operand-editor [operand]="c.right" (operandChange)="setCondition('entry', $index, { right: $event })" [label]="'Entry ' + ($index + 1) + ' right'" />
        <button type="button" class="btn btn-sm btn-ghost" [attr.aria-label]="'Remove entry condition ' + ($index + 1)" (click)="removeCondition('entry', $index)"><i class="bi bi-x-lg"></i></button>
      </div>
    }
    <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="draft().entry.length >= 8" (click)="addCondition('entry')"><i class="bi bi-plus-lg"></i> Add condition</button>
  </fieldset>

  <fieldset>
    <legend>Exit early when any of these holds <small>(optional)</small></legend>
    @for (c of draft().exit ?? []; track $index) {
      <div class="rule">
        <fs-operand-editor [operand]="c.left" (operandChange)="setCondition('exit', $index, { left: $event })" [label]="'Exit ' + ($index + 1) + ' left'" />
        <select class="form-select form-select-sm cmp" [attr.aria-label]="'Exit ' + ($index + 1) + ' comparison'" (change)="setCondition('exit', $index, { op: $any($event.target).value })">
          @for (o of comparisons; track o.op) { <option [value]="o.op" [selected]="o.op === c.op">{{ o.label }}</option> }
        </select>
        <fs-operand-editor [operand]="c.right" (operandChange)="setCondition('exit', $index, { right: $event })" [label]="'Exit ' + ($index + 1) + ' right'" />
        <button type="button" class="btn btn-sm btn-ghost" [attr.aria-label]="'Remove exit condition ' + ($index + 1)" (click)="removeCondition('exit', $index)"><i class="bi bi-x-lg"></i></button>
      </div>
    }
    <button type="button" class="btn btn-sm btn-outline-secondary" [disabled]="(draft().exit ?? []).length >= 8" (click)="addCondition('exit')"><i class="bi bi-plus-lg"></i> Add exit condition</button>
  </fieldset>

  <div class="levels">
    @for (kind of kinds; track kind) {
      <fieldset>
        <legend>
          <label class="check"><input type="checkbox" class="form-check-input" [checked]="!!level(kind)" (change)="toggleLevel(kind, $any($event.target).checked)" /> {{ kind === 'stop' ? 'Stop loss' : 'Profit target' }}</label>
        </legend>
        @if (level(kind); as l) {
          <fs-operand-editor [operand]="l.of" (operandChange)="setLevel(kind, { of: $event })" [label]="kind" />
          <label class="inline">{{ kind === 'stop' ? 'Beyond it by' : 'Stretched by' }}
            <input type="number" class="form-control form-control-sm num" step="0.1" min="-20" max="20" [value]="l.atr ?? 0" [attr.aria-label]="kind + ' offset in ATR'"
              (change)="setLevel(kind, { atr: toNumber($any($event.target).value) })" /> ATR
          </label>
        }
      </fieldset>
    }
  </div>

  <div class="row-fields">
    <label>Close after (days, 0 = never)
      <input type="number" class="form-control form-control-sm num" min="0" max="2000" step="1" [value]="draft().max_bars ?? 0" (change)="patch({ max_bars: toInt($any($event.target).value, 0, 2000) })" />
    </label>
    <label>Position size
      <select class="form-select form-select-sm" (change)="setSizing({ mode: $any($event.target).value })">
        <option value="risk_pct" [selected]="draft().sizing.mode === 'risk_pct'">Risk % of equity per trade</option>
        <option value="fixed_pct" [selected]="draft().sizing.mode === 'fixed_pct'">% of equity per trade</option>
      </select>
    </label>
    <label>%
      <input type="number" class="form-control form-control-sm num" step="0.1" min="0.1" [max]="draft().sizing.mode === 'risk_pct' ? 10 : 100" [value]="draft().sizing.value"
        (change)="setSizing({ value: toNumber($any($event.target).value) })" />
    </label>
    <label title="Bars on each side that confirm a swing high or low. Larger values find bigger, slower swings.">Swing strength
      <input type="number" class="form-control form-control-sm num" min="2" max="20" step="1" [value]="draft().swing_bars ?? 5" (change)="patch({ swing_bars: toInt($any($event.target).value, 2, 20) })" />
    </label>
    <label title="Smallest swing leg, in ATRs, that counts for Fibonacci levels.">Min swing (ATR)
      <input type="number" class="form-control form-control-sm num" min="0" max="20" step="0.5" [value]="draft().min_leg_atr ?? 2" (change)="patch({ min_leg_atr: toNumber($any($event.target).value) })" />
    </label>
  </div>

  <section class="readback" aria-label="The strategy in words">
    <h4>In words</h4>
    @for (line of words(); track line) { <p>{{ line }}</p> }
    @for (p of issues(); track p) { <p class="issue" role="alert"><i class="bi bi-exclamation-circle"></i> {{ p }}</p> }
  </section>
</form>
  `,
  styles: [`
:host { display: block; }
.builder { display: grid; gap: 0.9rem; }
label { display: flex; flex-direction: column; gap: 0.2rem; font-size: 0.78rem; color: var(--fs-muted); min-width: 0; }
label.inline, label.check { flex-direction: row; align-items: center; gap: 0.4rem; color: var(--fs-text); font-size: 0.85rem; }
.row-fields { display: flex; flex-wrap: wrap; gap: 0.75rem; align-items: flex-end; }
.grow { flex: 1 1 14rem; }
.num { width: 5.5rem; }
fieldset { border: 1px solid var(--fs-border); border-radius: 8px; padding: 0.6rem 0.8rem 0.8rem; display: grid; gap: 0.5rem; min-width: 0; }
legend { font-size: 0.85rem; font-weight: 600; float: none; width: auto; padding: 0 0.4rem; margin: 0; small { font-weight: 400; color: var(--fs-muted); } }
.rule { display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto; gap: 0.4rem; align-items: start; }
.cmp { width: auto; }
.levels { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 22rem), 1fr)); gap: 0.75rem; }
.readback { background: var(--fs-surface-2); border-radius: 8px; padding: 0.7rem 0.9rem; h4 { font-size: 0.8rem; color: var(--fs-muted); margin: 0 0 0.3rem; } p { margin: 0 0 0.2rem; font-size: 0.85rem; } }
.issue { color: var(--fs-warn); }
@media (max-width: 767.98px) { .rule { grid-template-columns: minmax(0, 1fr); } }
  `],
})
export class StrategyBuilderComponent {
  readonly draft = model.required<StrategyDef>();

  readonly comparisons = COMPARISONS;
  readonly kinds = ['stop', 'target'] as const;
  readonly words = computed(() => describeStrategy(this.draft()));
  readonly issues = computed(() => problems(this.draft()));

  patch(change: Partial<StrategyDef>): void { this.draft.update(d => ({ ...d, ...change })); }

  addCondition(list: RuleList): void {
    const fresh: StrategyCondition = { left: defaultOperand('price'), op: '>', right: defaultOperand('ema') };
    this.patch({ [list]: [...(this.draft()[list] ?? []), fresh] });
  }
  removeCondition(list: RuleList, i: number): void { this.patch({ [list]: (this.draft()[list] ?? []).filter((_, k) => k !== i) }); }
  setCondition(list: RuleList, i: number, change: Partial<StrategyCondition> & { op?: StrategyComparison }): void {
    this.patch({ [list]: (this.draft()[list] ?? []).map((c, k) => (k === i ? { ...c, ...change } : c)) });
  }

  level(kind: 'stop' | 'target'): StrategyLevel | null | undefined { return this.draft()[kind]; }
  toggleLevel(kind: 'stop' | 'target', on: boolean): void {
    this.patch({ [kind]: on ? { of: kind === 'stop' ? { type: 'lowest', period: 10 } : { type: 'highest', period: 20 }, atr: 0 } : null });
  }
  setLevel(kind: 'stop' | 'target', change: Partial<StrategyLevel>): void {
    const current = this.draft()[kind];
    if (current) this.patch({ [kind]: { ...current, ...change } });
  }
  setSizing(change: Partial<StrategyDef['sizing']>): void {
    const next = { ...this.draft().sizing, ...change };
    if (change.mode === 'risk_pct') next.value = Math.min(next.value, 10);
    this.patch({ sizing: next });
  }

  toNumber(raw: string): number { const v = Number(raw); return Number.isFinite(v) ? v : 0; }
  toInt(raw: string, min: number, max: number): number { const v = Math.round(Number(raw)); return Number.isFinite(v) ? Math.min(Math.max(v, min), max) : min; }
}
