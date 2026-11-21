import { ChangeDetectionStrategy, Component, computed, DestroyRef, inject, signal } from '@angular/core';

import {
  type DaySegment, type ExchangeHours, type ExchangeSession, WORLD_EXCHANGES,
  formatCountdown, sessionAt, tradingDayWindows, utcClock, utcDaySegments,
} from '../core/market-hours';

const TICK_MS = 15_000;
const HOUR = 3_600_000;

interface ClockRow {
  ex: ExchangeHours;
  session: ExchangeSession;
  segments: Array<DaySegment & { tone: 'past' | 'now' | 'next' }>;
  hours: string;
  status: string;
}

/**
 * Where the world's major exchanges are in their trading day: a 24-hour UTC track per exchange with
 * its regular sessions and a line at now, whether it is open, on a lunch break or closed, and how
 * long until it closes or opens. Regular weekday hours only; holidays and early closes are not known.
 */
@Component({
  selector: 'fse-market-clock',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="card clock" aria-labelledby="clock-title">
      <div class="card-header">
        <h2 class="card-title me-auto" id="clock-title"><i class="bi bi-globe2 me-2 text-primary" aria-hidden="true"></i>Market sessions</h2>
        <span class="clock-now tabular"><span class="visually-hidden">Now </span>{{ clock() }} UTC</span>
      </div>
      <div class="card-body">
        <p class="clock-summary"><strong>{{ openCount() }}</strong> of {{ rows().length }} open</p>
        <div class="clock-grid" [style.--now]="nowShare()">
          <div class="clock-scale" aria-hidden="true"><span>00</span><span>06</span><span>12</span><span>18</span><span>24</span></div>
          <ul class="clock-list fs-stagger" aria-label="Exchanges and their regular hours in UTC">
            @for (r of rows(); track r.ex.id) {
              <li class="clock-row" [attr.data-exchange]="r.ex.id" [attr.data-state]="r.session.state" [title]="r.ex.name">
                <span class="clock-dot" aria-hidden="true"></span>
                <span class="clock-name"><strong>{{ r.ex.city }}</strong>@if (r.hours) { <small class="tabular">{{ r.hours }}</small> }</span>
                <span class="clock-state">{{ r.status }}</span>
                <span class="clock-track" aria-hidden="true">
                  @for (s of r.segments; track s.start) {
                    <span class="clock-seg" [class]="s.tone" [style.left.%]="s.from * 100" [style.width.%]="(s.to - s.from) * 100"></span>
                  }
                  <span class="clock-mark"></span>
                </span>
              </li>
            }
          </ul>
        </div>
        <p class="clock-note">Regular weekday hours in UTC. Public holidays and early closes are not known.</p>
      </div>
    </section>
  `,
  styles: [`
    :host { display: block; }
    .clock-now { font-size: 0.8rem; font-weight: 650; color: var(--fs-text-2); padding: 0.1rem 0.5rem; border-radius: 999px; background: var(--fs-surface-3); }
    .clock-summary { margin: 0 0 0.6rem; font-size: 0.82rem; color: var(--fs-muted); strong { color: var(--fs-pos); font-size: 1rem; } }
    .clock-grid { --track-start: 7.5rem; position: relative; }
    .clock-scale {
      display: flex; justify-content: space-between; margin-left: var(--track-start); padding-bottom: 0.2rem;
      font-size: 0.65rem; color: var(--fs-muted); font-variant-numeric: tabular-nums;
      span { width: 0; display: flex; justify-content: center; }
    }
    .clock-list { list-style: none; margin: 0; padding: 0; display: grid; gap: 0.35rem; }
    .clock-row {
      display: grid; grid-template-columns: 0.75rem calc(var(--track-start) - 0.75rem) 1fr; grid-template-rows: auto auto; align-items: center;
      row-gap: 0.15rem; font-size: 0.8rem;
    }
    .clock-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--fs-muted); opacity: 0.55; }
    .clock-name { display: flex; flex-direction: column; line-height: 1.15; min-width: 0; small { color: var(--fs-muted); font-size: 0.68rem; } }
    .clock-state { grid-column: 3; text-align: right; color: var(--fs-muted); font-size: 0.72rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .clock-track {
      grid-column: 3; grid-row: 2; position: relative; height: 0.5rem; animation-delay: inherit; border-radius: 999px; background: var(--fs-surface-3);
      background-image: linear-gradient(90deg, transparent calc(25% - 0.5px), var(--fs-border) 25%, transparent calc(25% + 0.5px), transparent calc(50% - 0.5px), var(--fs-border) 50%, transparent calc(50% + 0.5px), transparent calc(75% - 0.5px), var(--fs-border) 75%, transparent calc(75% + 0.5px));
    }
    .clock-name, .clock-dot { grid-row: 1 / span 2; }
    .clock-seg {
      position: absolute; top: 0; bottom: 0; border-radius: 999px; transform-origin: left center;
      animation: fs-grow-x var(--fs-dur-4) var(--fs-ease-out) backwards; animation-delay: inherit;
      &.past { background: color-mix(in srgb, var(--fs-muted) 35%, transparent); }
      &.next { background: color-mix(in srgb, var(--fs-accent) 40%, transparent); }
      &.now { background: linear-gradient(90deg, color-mix(in srgb, var(--fs-pos) 70%, transparent), var(--fs-pos)); box-shadow: 0 0 10px -2px var(--fs-pos); }
    }
    .clock-mark { position: absolute; top: -0.2rem; bottom: -0.2rem; left: calc(var(--now) * 100%); width: 2px; margin-left: -1px; border-radius: 1px; background: var(--fs-text); }
    .clock-row[data-state='open'] {
      .clock-dot { opacity: 1; background: var(--fs-pos); box-shadow: 0 0 0 0 color-mix(in srgb, var(--fs-pos) 60%, transparent); animation: fs-pulse 2s infinite; }
      .clock-state { color: var(--fs-pos); font-weight: 600; }
    }
    .clock-row[data-state='break'] { .clock-dot { opacity: 1; background: var(--fs-warn); } .clock-state { color: var(--fs-warn); } }
    .clock-note { margin: 0.7rem 0 0; font-size: 0.7rem; color: var(--fs-muted); }
  `],
})
export class MarketClockComponent {
  readonly now = signal(Date.now());

  readonly rows = computed<ClockRow[]>(() => {
    const now = this.now();
    return WORLD_EXCHANGES.map(ex => {
      const session = sessionAt(ex, now);
      const segments = utcDaySegments(ex, now).map(s => ({ ...s, tone: s.end <= now ? 'past' as const : s.start <= now ? 'now' as const : 'next' as const }));
      const hours = tradingDayWindows(ex, now).map(w => `${utcClock(w.start)}–${utcClock(w.end)}`).join(', ');
      return { ex, session, segments, hours, status: statusText(ex, session, now) };
    });
  });

  readonly openCount = computed(() => this.rows().filter(r => r.session.state === 'open').length);
  readonly clock = computed(() => utcClock(this.now()));
  readonly nowShare = computed(() => String((this.now() % (24 * HOUR)) / (24 * HOUR)));

  constructor() {
    const tick = () => this.now.set(Date.now());
    const timer = setInterval(tick, TICK_MS);
    const onVisible = () => { if (document.visibilityState === 'visible') tick(); };
    document.addEventListener('visibilitychange', onVisible);
    inject(DestroyRef).onDestroy(() => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', onVisible);
    });
  }
}

function statusText(ex: ExchangeHours, s: ExchangeSession, now: number): string {
  if (ex.always) return 'Open around the clock';
  if (s.state === 'open' && s.closesAt) return `Open · closes in ${formatCountdown(s.closesAt - now)}`;
  if (!s.opensAt) return 'Closed';
  const label = s.state === 'break' ? 'Lunch break' : 'Closed';
  // Past a day away, a weekday and a time read better than a long countdown.
  if (s.opensAt - now > 24 * HOUR) {
    const day = new Date(s.opensAt).toLocaleDateString('en-US', { weekday: 'short', timeZone: 'UTC' });
    return `${label} · opens ${day} ${utcClock(s.opensAt)}`;
  }
  return `${label} · opens in ${formatCountdown(s.opensAt - now)}`;
}
