import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { forkJoin } from 'rxjs';

import type { CorporateEvent } from '../models';
import { MarketService } from '../services';
import { daysUntil, summarizeEvents } from './events-view';

const DAY = 86_400_000;

/**
 * Next earnings date, next ex-dividend date and trailing-twelve-month
 * dividends for a company, from a provider plugin with the events
 * capability. Without one the strip stays hidden.
 */
@Component({
  selector: 'fs-upcoming-events',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
@if (summary(); as s) {
  <section class="card upcoming" aria-label="Upcoming events">
    <div class="item">
      <span class="label">Next earnings</span>
      @if (s.nextEarnings; as e) {
        <span class="value">{{ e.date }}</span>
        <span class="sub">{{ days(e.date) }}{{ e.confirmed ? '' : ' · estimated' }}</span>
      } @else { <span class="value">–</span><span class="sub">No date announced</span> }
    </div>
    <div class="item">
      <span class="label">Next ex-dividend</span>
      @if (s.nextDividend; as d) {
        <span class="value">{{ d.date }}</span>
        <span class="sub">{{ days(d.date) }}@if (d.amount !== undefined) { · {{ d.amount }} {{ d.currency }} per share }</span>
      } @else { <span class="value">–</span><span class="sub">None announced</span> }
    </div>
    <div class="item">
      <span class="label">Dividends, last 12 months</span>
      @if (s.ttmDividend !== undefined) {
        <span class="value">{{ s.ttmDividend }}<small> per share</small></span>
        <span class="sub">@if (s.yieldPct !== undefined) { {{ s.yieldPct }}% yield at the current price } @else { Yield needs a current price }</span>
      } @else { <span class="value">–</span><span class="sub">No dividends on record</span> }
    </div>
  </section>
}
  `,
  styles: [`
:host { display: block; }
.upcoming { display: grid; grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr)); gap: 0.5rem 1.5rem; padding: 0.9rem 1.25rem; margin-bottom: 1rem; }
.item { display: flex; flex-direction: column; gap: 0.1rem; min-width: 0; }
.label { font-size: 0.75rem; color: var(--fs-muted); }
.value { font-size: 1.1rem; font-weight: 650; font-variant-numeric: tabular-nums; small { font-size: 0.7rem; font-weight: 400; color: var(--fs-muted); } }
.sub { font-size: 0.78rem; color: var(--fs-muted); }
  `],
})
export class UpcomingEventsComponent {
  readonly symbol = input.required<string>();
  readonly provider = input<string>('');
  /** The last price, for the dividend yield; 0 when unknown. */
  readonly price = input<number>(0);

  private readonly api = inject(MarketService);
  private readonly events = signal<{ past: CorporateEvent[]; upcoming: CorporateEvent[] } | null>(null);
  private readonly today = new Date().toISOString().slice(0, 10);

  readonly summary = computed(() => {
    const e = this.events();
    return e ? summarizeEvents(e.past, e.upcoming, this.price(), this.today) : null;
  });
  days = (date: string): string => daysUntil(date, this.today);

  constructor() {
    effect(onCleanup => {
      const symbol = this.symbol();
      untracked(() => this.events.set(null));
      if (!symbol) return;
      const iso = (offsetDays: number) => new Date(Date.parse(this.today) + offsetDays * DAY).toISOString().slice(0, 10);
      // Two windows, each under the 400 day limit: the past year and the next 90 days.
      const sub = forkJoin([this.api.getEvents(symbol, iso(-365), this.today, this.provider()), this.api.getEvents(symbol, this.today, iso(90), this.provider())]).subscribe({
        next: ([past, upcoming]) => this.events.set({ past: past.events, upcoming: upcoming.events }),
        error: () => this.events.set(null), // no provider with events, or it failed: no strip
      });
      onCleanup(() => sub.unsubscribe());
    });
  }
}
