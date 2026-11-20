import { ChangeDetectionStrategy, Component, computed, ElementRef, HostListener, inject, input, signal } from '@angular/core';
import { DecimalPipe } from '@angular/common';

import { directionFor, distancePercent, formatPrice, proximity } from '../core/price-alerts';
import { PriceAlertsService } from '../core/price-alerts.service';
import { signedPercent } from '../core/quote-board';

/**
 * A bell beside a price that sets price alerts for the instrument: a level to reach from the price now,
 * and the alerts already set with how near the price is to each. Alerts are kept in this browser.
 */
@Component({
  selector: 'fse-price-alert-menu',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [DecimalPipe],
  template: `
    <button type="button" class="btn btn-outline-secondary alert-toggle" [class.btn-sm]="compact()" [attr.aria-expanded]="open()" aria-controls="price-alert-panel" (click)="toggle()">
      <i class="bi" [class.bi-bell]="!activeCount()" [class.bi-bell-fill]="activeCount() > 0" aria-hidden="true"></i><span [class.visually-hidden]="compact()">Price alert</span>
      @if (activeCount()) { <span class="alert-count" [attr.aria-label]="activeCount() + ' active'">{{ activeCount() }}</span> }
    </button>
    @if (open()) {
      <div class="alert-panel" [class.start]="compact()" id="price-alert-panel" role="dialog" [attr.aria-label]="'Price alerts for ' + symbol()">
        <form class="alert-form" (submit)="add($event)">
          <label class="form-label small" for="price-alert-level">Alert when {{ symbol() }} reaches</label>
          <div class="input-group input-group-sm">
            <input id="price-alert-level" class="form-control tabular" type="number" min="0" step="any" inputmode="decimal" [value]="level()"
                   (input)="level.set($any($event.target).value)" />
            <button type="submit" class="btn btn-primary" [disabled]="!direction()">Add alert</button>
          </div>
          <p class="alert-hint" aria-live="polite">
            @if (direction(); as d) {
              {{ d === 'above' ? 'Above' : 'Below' }} the price now · {{ pct(distance()) }} away
            } @else { Enter a level above or below {{ price() | number: '1.2-4' }}. }
          </p>
        </form>
        @if (alerts().length) {
          <ul class="alert-list" aria-label="Alerts for this instrument">
            @for (a of alerts(); track a.id) {
              <li [class.hit]="!!a.triggered_at">
                <span class="alert-what">{{ a.direction === 'above' ? 'Above' : 'Below' }} <strong class="tabular">{{ a.level | number: '1.2-4' }}</strong></span>
                @if (a.triggered_at) { <span class="alert-state">Hit</span> }
                @else { <span class="alert-prox" aria-hidden="true"><span class="fs-grow-x" [style.width.%]="near(a) * 100"></span></span> }
                <button type="button" class="btn btn-sm btn-ghost" (click)="alertsSvc.remove(a.id)" [attr.aria-label]="'Remove alert ' + a.direction + ' ' + fmt(a.level)"><i class="bi bi-x-lg" aria-hidden="true"></i></button>
              </li>
            }
          </ul>
        }
        <p class="alert-note">Kept in this browser and checked against provider quotes. An alert never places an order.</p>
      </div>
    }
  `,
  styles: [`
    :host { position: relative; display: inline-block; font-size: 1rem; font-weight: 400; line-height: 1.5; letter-spacing: normal; align-self: center; }
    .alert-panel.start { right: auto; left: 0; transform-origin: top left; }
    .alert-toggle { display: inline-flex; align-items: center; gap: 0.4rem; }
    .alert-toggle[aria-expanded='true'] .bi, .bi-bell-fill { color: var(--fs-warn); }
    .alert-count { min-width: 1.15rem; padding: 0 0.3rem; border-radius: 999px; font-size: 0.7rem; font-weight: 700; line-height: 1.15rem; color: #1b1300; background: var(--fs-warn); }
    .alert-panel {
      position: absolute; right: 0; top: calc(100% + 0.4rem); z-index: 30; width: min(20rem, calc(100vw - 2rem)); padding: 0.85rem;
      border: 1px solid var(--fs-border); border-radius: 0.8rem; background: var(--fs-surface); box-shadow: var(--fs-shadow);
      transform-origin: top right; animation: alert-pop var(--fs-dur-2) var(--fs-ease-spring) backwards;
    }
    @keyframes alert-pop { from { opacity: 0; transform: translateY(-4px) scale(0.96); } }
    .alert-hint { margin: 0.35rem 0 0; font-size: 0.75rem; color: var(--fs-muted); }
    .alert-list { list-style: none; margin: 0.75rem 0 0; padding: 0.6rem 0 0; border-top: 1px solid var(--fs-border); display: grid; gap: 0.3rem; }
    .alert-list li { display: grid; grid-template-columns: 1fr 4.5rem auto; align-items: center; gap: 0.5rem; font-size: 0.82rem; }
    .alert-prox { height: 0.35rem; border-radius: 999px; background: var(--fs-surface-3); overflow: hidden; span { display: block; height: 100%; border-radius: inherit; background: var(--fs-warn); } }
    .alert-state { font-size: 0.72rem; font-weight: 700; color: var(--fs-warn); text-align: center; }
    .alert-note { margin: 0.7rem 0 0; font-size: 0.7rem; color: var(--fs-muted); }
    @media (prefers-reduced-motion: reduce) { .alert-panel { animation: none; } }
  `],
})
export class PriceAlertMenuComponent {
  readonly symbol = input.required<string>();
  /** Icon only, with the panel opening to the right: for a place beside the title. */
  readonly compact = input(false);
  readonly provider = input.required<string>();
  readonly price = input(0);

  readonly alertsSvc = inject(PriceAlertsService);
  private readonly host = inject(ElementRef<HTMLElement>);

  readonly open = signal(false);
  readonly level = signal('');
  readonly alerts = computed(() => this.alertsSvc.forInstrument({ symbol: this.symbol(), provider: this.provider() }));
  readonly activeCount = computed(() => this.alerts().filter(a => !a.triggered_at).length);
  private readonly levelValue = computed(() => Number(this.level()));
  readonly direction = computed(() => directionFor(this.levelValue(), this.price()));
  readonly distance = computed(() => distancePercent(this.levelValue(), this.price()));

  toggle(): void {
    if (!this.open() && this.price() > 0) this.level.set(String(+(this.price() * 1.02).toPrecision(6)));
    this.open.update(o => !o);
  }

  add(event: Event): void {
    event.preventDefault();
    if (this.alertsSvc.add({ symbol: this.symbol(), provider: this.provider() }, this.levelValue(), this.price())) this.level.set('');
  }

  near(a: Parameters<typeof proximity>[0]): number { return proximity(a, this.price()); }
  pct(p: number | null): string { return signedPercent(p); }
  fmt(n: number): string { return formatPrice(n); }

  @HostListener('document:keydown.escape') close(): void { this.open.set(false); }
  @HostListener('document:click', ['$event']) outside(e: MouseEvent): void {
    if (this.open() && !this.host.nativeElement.contains(e.target as Node)) this.open.set(false);
  }
}
