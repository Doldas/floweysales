import { NgTemplateOutlet } from '@angular/common';
import { ChangeDetectionStrategy, Component, input, model } from '@angular/core';

import type { StrategyOperand, StrategyOperandType } from '../models';
import { FIB_RATIOS, OPERAND_TYPES, PRICE_FIELDS, defaultOperand } from './strategy-view';

/** Picks one side of a rule: a price, an indicator, a number or a Fibonacci level. */
@Component({
  selector: 'fs-operand-editor',
  imports: [NgTemplateOutlet],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
<span class="operand">
  <select class="form-select form-select-sm" [attr.aria-label]="label() + ' type'" [value]="operand().type" (change)="setType($any($event.target).value)">
    @for (g of groups; track g) {
      <optgroup [label]="g">
        @for (t of types; track t.type) { @if (t.group === g) { <option [value]="t.type" [selected]="t.type === operand().type">{{ t.label }}</option> } }
      </optgroup>
    }
  </select>
  @switch (operand().type) {
    @case ('price') {
      <select class="form-select form-select-sm" [attr.aria-label]="label() + ' price field'" (change)="patch({ field: $any($event.target).value })">
        @for (f of fields; track f) { <option [value]="f" [selected]="f === operand().field">{{ f }}</option> }
      </select>
    }
    @case ('const') {
      <input type="number" class="form-control form-control-sm" step="any" [attr.aria-label]="label() + ' number'" [value]="operand().value ?? 0"
        (change)="patch({ value: num($any($event.target).value, 0) })" />
    }
    @case ('ema') { <ng-container *ngTemplateOutlet="avg" /> }
    @case ('sma') { <ng-container *ngTemplateOutlet="avg" /> }
    @case ('rsi') { <ng-container *ngTemplateOutlet="period" /> }
    @case ('atr') { <ng-container *ngTemplateOutlet="period" /> }
    @case ('highest') { <ng-container *ngTemplateOutlet="period" /> }
    @case ('lowest') { <ng-container *ngTemplateOutlet="period" /> }
    @case ('fib_retr') { <ng-container *ngTemplateOutlet="ratio" /> }
    @case ('fib_ext') { <ng-container *ngTemplateOutlet="ratio" /> }
  }
</span>
<ng-template #period>
  <input type="number" class="form-control form-control-sm num" min="1" max="400" step="1" [attr.aria-label]="label() + ' period in days'" [value]="operand().period ?? 14"
    (change)="patch({ period: int($any($event.target).value, 14) })" />
</ng-template>
<ng-template #avg>
  <input type="number" class="form-control form-control-sm num" min="1" max="400" step="1" [attr.aria-label]="label() + ' period in days'" [value]="operand().period ?? 21"
    (change)="patch({ period: int($any($event.target).value, 21) })" />
  <select class="form-select form-select-sm" [attr.aria-label]="label() + ' average of'" (change)="patch({ field: $any($event.target).value })">
    @for (f of fields; track f) { <option [value]="f" [selected]="f === (operand().field ?? 'close')">of {{ f }}</option> }
  </select>
</ng-template>
<ng-template #ratio>
  <input type="number" class="form-control form-control-sm num" min="0" max="5" step="0.001" list="fib-ratios" [attr.aria-label]="label() + ' Fibonacci ratio'" [value]="operand().ratio ?? 0.618"
    (change)="patch({ ratio: num($any($event.target).value, 0.618) })" />
  <datalist id="fib-ratios">@for (r of ratios; track r) { <option [value]="r"></option> }</datalist>
</ng-template>
  `,
  styles: [`
:host { display: block; min-width: 0; }
.operand { display: flex; flex-wrap: wrap; gap: 0.3rem; }
.operand select { width: auto; max-width: 100%; }
.operand .num { width: 5.2rem; }
.operand input[type='number']:not(.num) { width: 7rem; }
  `],
})
export class OperandEditorComponent {
  readonly operand = model.required<StrategyOperand>();
  /** Names the control for assistive technology, for example "Entry 1 left". */
  readonly label = input<string>('Operand');

  readonly types = OPERAND_TYPES;
  readonly groups = [...new Set(OPERAND_TYPES.map(t => t.group))];
  readonly fields = PRICE_FIELDS;
  readonly ratios = FIB_RATIOS;

  setType(type: StrategyOperandType): void { this.operand.set(defaultOperand(type)); }
  patch(change: Partial<StrategyOperand>): void { this.operand.update(o => ({ ...o, ...change })); }
  num(raw: string, fallback: number): number { const v = Number(raw); return Number.isFinite(v) ? v : fallback; }
  int(raw: string, fallback: number): number { const v = Math.round(Number(raw)); return Number.isFinite(v) && v >= 1 ? Math.min(v, 400) : fallback; }
}
