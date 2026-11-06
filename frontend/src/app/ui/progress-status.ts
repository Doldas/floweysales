import { ChangeDetectionStrategy, Component, DestroyRef, OnInit, computed, inject, input, output, signal } from '@angular/core';

import { elapsedSince, progressText, stallHint } from './async-state';

/**
 * Says what a long wait is doing: the stage, a count when one is measured, and
 * after a few seconds that it is still working. It never invents a percentage:
 * the bar is determinate only when `done` and `total` are given.
 */
@Component({
  selector: 'fs-progress-status',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="ps" role="status" aria-live="polite">
      <span class="spinner-border spinner-border-sm text-secondary" aria-hidden="true"></span>
      <span>{{ text() }}</span>
      @if (cancellable()) { <button type="button" class="btn btn-sm btn-link p-0 ms-1" (click)="cancel.emit()">Cancel</button> }
    </div>
    @if (hint(); as h) { <div class="ps-hint" role="status">{{ h }}</div> }
    @if (total() !== null && done() !== null) {
      <div class="progress" style="height: 3px" role="progressbar" [attr.aria-valuenow]="done()" aria-valuemin="0"
           [attr.aria-valuemax]="total()" [attr.aria-label]="stage()">
        <div class="progress-bar" [style.width.%]="percent()"></div>
      </div>
    }
  `,
  styles: [`
    .ps { display: flex; align-items: center; gap: .5rem; color: var(--fs-muted); font-size: .85rem; margin-bottom: .35rem; min-height: 1.5rem; }
    .ps-hint { color: var(--fs-muted); font-size: .8rem; margin-bottom: .35rem; }
  `],
})
export class ProgressStatusComponent implements OnInit {
  readonly stage = input.required<string>();
  readonly done = input<number | null>(null);
  readonly total = input<number | null>(null);
  /** Epoch ms when the work began, when the caller knows it; defaults to when this status appeared. */
  readonly since = input<number | null>(null);
  /** Shows a Cancel button for work that can be stopped. */
  readonly cancellable = input(false);
  readonly cancel = output<void>();

  private readonly started = Date.now();
  private readonly elapsed = computed(() => elapsedSince(this.since() ?? this.started, this.now()));
  readonly hint = computed(() => stallHint(this.elapsed(), this.cancellable()));
  private readonly now = signal(Date.now());
  readonly text = computed(() => progressText(this.stage(), this.elapsed(), this.done(), this.total()));
  readonly percent = computed(() => (this.total() ? Math.min(100, ((this.done() ?? 0) / this.total()!) * 100) : 0));

  private readonly destroyRef = inject(DestroyRef);

  ngOnInit(): void {
    const timer = setInterval(() => this.now.set(Date.now()), 1000);
    this.destroyRef.onDestroy(() => clearInterval(timer));
  }
}
