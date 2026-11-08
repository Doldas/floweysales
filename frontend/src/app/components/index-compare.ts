import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';

import { MarketService } from '../services';
import type { IndexComparison, IndexInfo } from '../models';
import { benchmarkLine, comparable, failedChecks, levelLabel, pairLine, percent, selectionProblem } from './index-compare-view';

/**
 * Compares two to six of the user's own index lists and can rank their union.
 * Securities are matched by ISIN or exchange plus ticker, currencies are never
 * converted, and an index without a list is named, not estimated.
 */
@Component({
  selector: 'fs-index-compare',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="ixc" aria-labelledby="ixc-h">
      <h3 class="h6" id="ixc-h">Compare or combine indices</h3>
      @if (options().length < 2) {
        <p class="text-muted" role="status">Supply your own list for at least two indices to compare or combine them. None is substituted.</p>
      } @else {
        <fieldset class="ixc-pick">
          <legend class="visually-hidden">Indices to compare</legend>
          @for (i of options(); track i.id) {
            <label class="ixc-opt"><input type="checkbox" [checked]="selected().includes(i.id)" (change)="toggle(i.id)" /> {{ i.name }}</label>
          }
        </fieldset>
        <div class="ixc-actions">
          <button type="button" class="btn btn-sm btn-outline-primary" [disabled]="busy() || !!problem()" (click)="compare()">Compare</button>
          <button type="button" class="btn btn-sm btn-primary" [disabled]="!!problem()" (click)="combine.emit(selected())">Rank the combination</button>
          @if (problem() && selected().length) { <span class="ixc-hint">{{ problem() }}</span> }
        </div>
        @if (error()) { <p class="ixc-err" role="alert">{{ error() }}</p> }
        @if (result(); as c) {
          <div class="ixc-result" role="region" aria-label="Index comparison">
            <div class="ixc-wrap">
              <table>
                <caption class="visually-hidden">Index lists side by side</caption>
                <thead><tr><th scope="col">Index</th><th scope="col">Securities</th><th scope="col">Currency</th><th scope="col">As of (UTC)</th><th scope="col">Past membership</th><th scope="col">Benchmark</th><th scope="col">List</th></tr></thead>
                <tbody>
                  @for (i of c.indices; track i.id) {
                    <tr>
                      <th scope="row">{{ i.name }}</th>
                      <td>{{ i.count }}<small class="text-muted"> ({{ share(i.with_isin, i.count) }} with ISIN)</small></td>
                      <td>{{ i.currency }}</td>
                      <td>{{ i.as_of }}@if (i.age_days !== null) { <small class="text-muted"> ({{ i.age_days }} days)</small> }</td>
                      <td>{{ i.point_in_time ? 'from ' + (i.history_from || 'a recorded date') : 'today only' }}</td>
                      <td>{{ benchmark(i) }}</td>
                      <td><span class="ixc-level" [attr.data-level]="i.verification?.level">{{ level(i.verification) }}</span></td>
                    </tr>
                  }
                </tbody>
              </table>
            </div>
            @for (p of c.pairs; track p.a + p.b) {
              <p class="ixc-pair">{{ pair(c, p) }}</p>
            }
            <p class="ixc-pair">Together: {{ c.union_count }} different securities.</p>
            @if (c.unavailable.length) { <p class="ixc-warn" role="status">No list from you for {{ c.unavailable.join(', ') }}; left out, not estimated.</p> }
            @for (w of c.warnings; track w) { <p class="ixc-warn" role="status">{{ w }}</p> }
            @for (i of c.indices; track i.id) {
              @for (f of failed(i.verification); track f) { <p class="ixc-warn">{{ i.name }}: {{ f }}</p> }
            }
            <p class="text-muted ixc-hint">{{ c.matching }}</p>
          </div>
        }
      }
    </section>
  `,
  styles: [`
    .ixc { display: grid; gap: 0.5rem; border-top: 1px solid var(--fs-border); padding-top: 0.75rem; margin-top: 0.75rem; }
    .ixc-pick { display: flex; flex-wrap: wrap; gap: 0.5rem 1rem; border: 0; padding: 0; margin: 0; }
    .ixc-opt { display: inline-flex; gap: 0.35rem; align-items: center; font-size: 0.85rem; }
    .ixc-actions { display: flex; flex-wrap: wrap; gap: 0.5rem; align-items: center; }
    .ixc-hint { font-size: 0.75rem; color: var(--fs-muted); margin: 0; }
    .ixc-err { color: var(--fs-neg); font-size: 0.8rem; margin: 0; }
    .ixc-wrap { overflow-x: auto; max-width: 100%; }
    .ixc-wrap table { border-collapse: collapse; font-size: 0.8rem; min-width: 38rem; }
    .ixc-wrap th, .ixc-wrap td { padding: 0.3rem 0.6rem; border-bottom: 1px solid var(--fs-border); text-align: left; white-space: nowrap; }
    .ixc-pair { font-size: 0.82rem; margin: 0; }
    .ixc-warn { border-left: 3px solid var(--fs-warn); padding-left: 0.6rem; font-size: 0.8rem; margin: 0; }
    .ixc-level[data-level='verified'] { color: var(--fs-pos); font-weight: 600; }
    .ixc-level[data-level='approximate'] { color: var(--fs-warn); font-weight: 600; }
  `],
})
export class IndexCompareComponent {
  private readonly api = inject(MarketService);

  readonly indices = input.required<IndexInfo[]>();
  /** Emits the chosen index ids when the user wants their union ranked. */
  readonly combine = output<string[]>();

  readonly options = computed(() => comparable(this.indices()));
  readonly selected = signal<string[]>([]);
  readonly problem = computed(() => selectionProblem(this.selected()));
  readonly result = signal<IndexComparison | null>(null);
  readonly error = signal('');
  readonly busy = signal(false);

  readonly level = levelLabel;
  readonly failed = failedChecks;
  readonly pair = pairLine;
  readonly benchmark = benchmarkLine;
  share(n: number, total: number): string { return total ? percent(n / total) : '0%'; }

  toggle(id: string): void {
    this.result.set(null);
    this.selected.update(s => (s.includes(id) ? s.filter(x => x !== id) : [...s, id]));
  }

  compare(): void {
    if (this.problem()) return;
    this.busy.set(true);
    this.error.set('');
    this.api.compareIndices(this.selected()).subscribe({
      next: r => { this.busy.set(false); this.result.set(r.comparison); },
      error: e => { this.busy.set(false); this.result.set(null); this.error.set(e?.error?.error ?? 'The comparison failed.'); },
    });
  }
}
