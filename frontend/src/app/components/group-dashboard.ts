import { ChangeDetectionStrategy, Component, DestroyRef, Directive, ElementRef, computed, effect, inject, input, output, signal, untracked } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Subject, catchError, from, map, mergeMap, of, switchMap, toArray } from 'rxjs';

import { DIMENSION_LABELS, composition, type Collection, type CollectionMember, type Dimension } from '../core/collections';
import {
  MIN_CORRELATION_DAYS, arcPath, computeGroupStats, correlationMatrix, dailyCloses, donutSlices, pct,
  type DailyClose, type GroupMemberInput, type GroupStats,
} from '../core/group-stats';
import { instrumentKey } from '../core/instrument-ref';
import type { HistorySeries } from '../models';
import { MarketService } from '../services';
import { ThemeService } from '../theme.service';
import { formatUtcDate, formatUtcDateTime } from '../ui/datetime';
import { PaneComponent } from '../ui/pane';

/** The ranges a group can be compared over; all give daily bars. */
const RANGES = ['1M', '3M', '1Y', '3Y'] as const;
type GroupRange = (typeof RANGES)[number];
const RANGE_KEY = 'fs.markets.groupRange';
/** At most this many instruments are asked for history per load. */
export const GROUP_HISTORY_MEMBERS = 25;
/** The correlation grid stays readable up to this many instruments. */
const CORRELATION_MEMBERS = 12;
// Each color and line pattern combination is unique across the 25 plotted instruments.
const SERIES_COLORS = [
  { light: '#b44316', dark: '#ff986b' },
  { light: '#007a56', dark: '#5ee5b1' },
  { light: '#783cca', dark: '#bd9aff' },
  { light: '#956300', dark: '#f6cd64' },
  { light: '#b12563', dark: '#f785b3' },
  { light: '#007685', dark: '#59d9e9' },
  { light: '#be292d', dark: '#ff7c83' },
  { light: '#537415', dark: '#b9da70' },
] as const;
const SERIES_DASHES = [null, '8 4', '2 4', '8 3 2 3'] as const;

/** Histories already loaded this visit, by provider, symbol and range. */
const historyCache = new Map<string, { at: number; series: HistorySeries | null }>();
const CACHE_MS = 5 * 60_000;

function readRange(): GroupRange {
  try {
    const v = localStorage.getItem(RANGE_KEY);
    return (RANGES as readonly string[]).includes(v ?? '') ? (v as GroupRange) : '1Y';
  } catch { return '1Y'; }
}

// Chart geometry, in SVG units.
const LH = 240, LP = { l: 40, r: 10, t: 12, b: 24 };
const SH = 230, SP = { l: 44, r: 14, t: 14, b: 40 };

function niceTicks(min: number, max: number, count = 4): number[] {
  if (!(max > min)) return [min];
  const raw = (max - min) / count;
  const mag = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map(s => s * mag).find(s => s >= raw) ?? raw;
  const ticks: number[] = [];
  for (let v = Math.ceil(min / step) * step; v <= max + 1e-9; v += step) ticks.push(Math.round(v * 1e6) / 1e6);
  return ticks;
}

interface Hover { i: number; x: number }

/** Reports the element's content width, so a chart is drawn at its real size and its text stays legible. */
@Directive({ selector: '[fsWidth]' })
export class WidthDirective {
  readonly fsWidth = output<number>();
  constructor() {
    const el = inject(ElementRef<HTMLElement>).nativeElement as HTMLElement;
    if (typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(entries => {
      const w = Math.round(entries[0]?.contentRect.width ?? 0);
      if (w > 0) this.fsWidth.emit(w);
    });
    ro.observe(el);
    inject(DestroyRef).onDestroy(() => ro.disconnect());
  }
}

/**
 * Dashboard of one group (a collection): how its instruments moved together
 * over a range, computed in the browser from each instrument's own provider
 * history. Composition comes from the stored instrument details.
 */
@Component({
  selector: 'fs-group-dashboard',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [PaneComponent, WidthDirective],
  template: `
    <fs-pane paneId="markets.groupDashboard" [heading]="'Group dashboard · ' + collection().name" [focusable]="true">
      <span paneSummary>{{ collection().kind === 'model' ? 'Model portfolio' : 'Watch group' }} · {{ members().length }} instruments@if (stats()?.returnPct !== null && stats()?.returnPct !== undefined) { · {{ range() }} {{ pct(stats()!.returnPct) }} }</span>
      <span paneActions style="display: contents">
        <div class="fs-segmented" role="group" aria-label="Dashboard range">
          @for (r of ranges; track r) {
            <button type="button" [attr.aria-pressed]="range() === r" (click)="setRange(r)">{{ r }}</button>
          }
        </div>
        <button type="button" class="btn btn-sm btn-ghost btn-icon" (click)="refresh()" [disabled]="loading()" aria-label="Reload prices" title="Reload prices"><i class="bi bi-arrow-clockwise" [class.spin]="loading()" aria-hidden="true"></i></button>
      </span>
      <div class="dash" [attr.aria-busy]="loading()">
        @if (loading() && !stats()) {
          <div class="dash-loading" role="status"><span class="pulse" aria-hidden="true"></span>Loading {{ range() }} prices for {{ asked() }} instruments…</div>
        }

        @if (stats(); as s) {
          <div class="dash-body" [class.stale]="loading()">
            <!-- Headline figures -->
            <div class="tiles" role="list" aria-label="Group figures">
              <div class="tile hero" role="listitem">
                <span class="tile-label">Group · {{ range() }}</span>
                <span class="tile-value" [class.text-pos]="(s.returnPct ?? 0) > 0" [class.text-neg]="(s.returnPct ?? 0) < 0">
                  @if (s.returnPct !== null) { <i class="bi" [class.bi-arrow-up-right]="s.returnPct >= 0" [class.bi-arrow-down-right]="s.returnPct < 0" aria-hidden="true"></i> }
                  {{ pct(s.returnPct) }}
                </span>
                <span class="tile-sub">{{ s.weighting === 'model' ? 'Hypothetical weights' : 'Equal weights' }}, buy and hold</span>
              </div>
              <div class="tile" role="listitem">
                <span class="tile-label">Volatility</span>
                <span class="tile-value">{{ pct(s.volatilityPct, false) }}</span>
                <span class="tile-sub">Annualized, daily closes</span>
              </div>
              <div class="tile" role="listitem">
                <span class="tile-label">Max drawdown</span>
                <span class="tile-value" [class.text-neg]="(s.maxDrawdownPct ?? 0) < 0">{{ pct(s.maxDrawdownPct) }}</span>
                <span class="tile-sub">Largest fall from a high</span>
              </div>
              <div class="tile" role="listitem">
                <span class="tile-label">Breadth</span>
                <span class="tile-value">{{ s.up }} up · {{ s.down }} down</span>
                <span class="breadth" aria-hidden="true">
                  <span class="b-up" [style.flex-grow]="s.up"></span><span class="b-flat" [style.flex-grow]="s.flat"></span><span class="b-down" [style.flex-grow]="s.down"></span>
                </span>
              </div>
              @if (s.best) {
                <div class="tile" role="listitem">
                  <span class="tile-label">Best</span>
                  <span class="tile-value">{{ s.best.symbol }} <small [class.text-pos]="s.best.returnPct > 0" [class.text-neg]="s.best.returnPct < 0">{{ pct(s.best.returnPct) }}</small></span>
                  <span class="tile-sub">{{ s.best.provider }}</span>
                </div>
              }
              @if (s.worst) {
                <div class="tile" role="listitem">
                  <span class="tile-label">Worst</span>
                  <span class="tile-value">{{ s.worst.symbol }} <small [class.text-pos]="s.worst.returnPct > 0" [class.text-neg]="s.worst.returnPct < 0">{{ pct(s.worst.returnPct) }}</small></span>
                  <span class="tile-sub">{{ s.worst.provider }}</span>
                </div>
              }
            </div>

            <div class="charts">
              <figure class="chart wide">
                <figcaption><strong>Group index</strong> <span class="muted">100 = {{ indexStart() }}</span></figcaption>
                @if (line(); as L) {
                  <div class="plot" (fsWidth)="LW.set($event)" (mouseleave)="hover.set(null)">
                    <svg [attr.viewBox]="'0 0 ' + LW() + ' ' + LH" [attr.width]="LW()" [attr.height]="LH" class="svg" role="img" tabindex="0"
                         [attr.aria-label]="'Group index from 100 to ' + (s.index.at(-1)?.value ?? 100).toFixed(1) + ' over ' + range() + '. Use the arrow keys to read each day.'"
                         (mousemove)="move($event)" (click)="move($event)" (keydown)="key($event)" (blur)="hover.set(null)">
                      @for (t of L.ticks; track t) {
                        <line class="grid" [class.base]="t === 100" [attr.x1]="LP.l" [attr.x2]="LW() - LP.r" [attr.y1]="L.y(t)" [attr.y2]="L.y(t)" />
                        <text class="axis" [attr.x]="LP.l - 6" [attr.y]="L.y(t) + 4" text-anchor="end">{{ t }}</text>
                      }
                      @for (d of L.xTicks; track d.x) {
                        <text class="axis" [attr.x]="d.x" [attr.y]="LH - 6" text-anchor="middle">{{ d.label }}</text>
                      }
                      <path class="area" [attr.d]="L.area" />
                      <path class="group-line" [attr.d]="L.d" />
                      @for (m of L.members; track m.key) {
                        <path class="member-line" [class.dimmed]="activeLine() && activeLine() !== m.key"
                              [class.highlighted]="activeLine() === m.key" [attr.stroke]="m.color"
                              [attr.stroke-dasharray]="m.dash" [attr.d]="m.d">
                          <title>{{ m.label }}</title>
                        </path>
                      }
                      @if (hover(); as h) {
                        <line class="cross" [attr.x1]="h.x" [attr.x2]="h.x" [attr.y1]="LP.t" [attr.y2]="LH - LP.b" />
                        <circle class="dot" [attr.cx]="h.x" [attr.cy]="L.y(s.index[h.i].value)" r="4.5" />
                      }
                    </svg>
                    @if (hover(); as h) {
                      <div class="tip" role="status" [style.left.px]="h.x" [class.flip]="h.x > LW() * 0.6">
                        <div class="tip-day">{{ day(s.index[h.i].day) }}</div>
                        <div class="tip-row"><span class="sw sw-accent"></span>Group <b>{{ s.index[h.i].value.toFixed(1) }}</b> <span class="muted">{{ pct(s.index[h.i].value - 100) }}</span></div>
                        @for (m of hoverMembers(); track m.key) {
                          <div class="tip-row">
                            <svg class="line-swatch" viewBox="0 0 24 8" aria-hidden="true">
                              <line x1="0" x2="24" y1="4" y2="4" [attr.stroke]="m.color" [attr.stroke-dasharray]="m.dash" />
                            </svg>
                            {{ m.label }} <b>{{ m.value.toFixed(1) }}</b>
                          </div>
                        }
                      </div>
                    }
                  </div>
                  <ul class="line-legend" aria-label="Group chart series">
                    <li class="index-key"><span class="group-swatch" aria-hidden="true"></span>Group index</li>
                    @for (m of L.members; track m.key) {
                      <li>
                        <button type="button" [attr.aria-pressed]="activeLine() === m.key" (click)="toggleLine(m.key)">
                          <svg class="line-swatch" viewBox="0 0 24 8" aria-hidden="true">
                            <line x1="0" x2="24" y1="4" y2="4" [attr.stroke]="m.color" [attr.stroke-dasharray]="m.dash" />
                          </svg>
                          {{ m.label }}
                        </button>
                      </li>
                    }
                  </ul>
                } @else {
                  <p class="muted small">Not enough shared days to draw an index.</p>
                }
                <details class="as-table">
                  <summary>Show as table</summary>
                  <div class="t-scroll" tabindex="0" role="group" aria-label="Group index table">
                    <table class="table table-sm">
                      <thead><tr><th scope="col">Day (UTC)</th><th scope="col" class="num">Index</th></tr></thead>
                      <tbody>
                        @for (p of indexTable(); track p.day) { <tr><td>{{ p.day }}</td><td class="num">{{ p.value.toFixed(2) }}</td></tr> }
                      </tbody>
                    </table>
                  </div>
                </details>
              </figure>

              <!-- Composition: part of the whole, at most six slices -->
              <figure class="chart">
                <figcaption class="cap-row">
                  <strong>Composition</strong>
                  <label class="visually-hidden" for="donut-dim">Composition by</label>
                  <select id="donut-dim" class="form-select form-select-sm cap-select" [value]="dimension()" (change)="picked.set($any($event.target).value)">
                    @for (d of dimensions(); track d.id) { <option [value]="d.id" [selected]="d.id === dimension()">{{ d.label }}</option> }
                  </select>
                </figcaption>
                @if (donut().slices.length) {
                  <div class="donut-wrap">
                    <svg viewBox="0 0 160 160" class="donut" role="img" [attr.aria-label]="donutLabel()">
                      @for (sl of donut().arcs; track sl.key; let i = $index) {
                        <path [attr.d]="sl.d" [attr.class]="'slice ' + sl.cls" [class.dim]="donutHover() !== null && donutHover() !== i"
                              (mouseenter)="donutHover.set(i)" (mouseleave)="donutHover.set(null)"><title>{{ sl.label }}: {{ sl.text }}</title></path>
                      }
                      <text x="80" y="76" text-anchor="middle" class="donut-total">{{ donutCenter().value }}</text>
                      <text x="80" y="96" text-anchor="middle" class="donut-sub">{{ donutCenter().label }}</text>
                    </svg>
                    <ul class="legend">
                      @for (sl of donut().arcs; track sl.key; let i = $index) {
                        <li (mouseenter)="donutHover.set(i)" (mouseleave)="donutHover.set(null)" [class.dim]="donutHover() !== null && donutHover() !== i">
                          <span class="sw" [attr.class]="'sw ' + sl.cls"></span><span class="lg-label">{{ sl.label }}</span><span class="lg-val">{{ sl.text }}</span>
                        </li>
                      }
                    </ul>
                  </div>
                  <p class="muted small mb-0">{{ donut().note }}</p>
                } @else {
                  <p class="muted small">{{ donut().note || 'Nothing to show yet.' }}</p>
                }
              </figure>

              <!-- Return per instrument: above or below zero -->
              <figure class="chart">
                <figcaption><strong>Return per instrument</strong> <span class="muted">{{ range() }}, own currency</span></figcaption>
                <ul class="bars" [attr.aria-label]="'Return per instrument over ' + range()">
                  @for (b of bars(); track b.key) {
                    <li class="bar-row" [title]="b.symbol + ' on ' + b.provider + ': ' + pct(b.returnPct)">
                      <span class="bar-name">{{ b.symbol }}</span>
                      <span class="bar-track">
                        <span class="bar-zero" [style.left.%]="zeroAt()"></span>
                        <span class="bar" [class.neg]="b.returnPct < 0" [style.left.%]="b.left" [style.width.%]="b.width"></span>
                      </span>
                      <span class="bar-val" [class.text-pos]="b.returnPct > 0" [class.text-neg]="b.returnPct < 0">{{ pct(b.returnPct) }}</span>
                    </li>
                  }
                </ul>
              </figure>

              <!-- Risk against return -->
              <figure class="chart">
                <figcaption><strong>Risk and return</strong> <span class="muted">volatility against return, {{ range() }}</span></figcaption>
                @if (scatter(); as P) {
                  <div class="plot" (fsWidth)="SW.set($event)">
                    <svg [attr.viewBox]="'0 0 ' + SW() + ' ' + SH" [attr.width]="SW()" [attr.height]="SH" class="svg" role="img" [attr.aria-label]="'Volatility against return for ' + P.dots.length + ' instruments; the table below lists each.'">
                      @for (t of P.yTicks; track t) {
                        <line class="grid" [class.base]="t === 0" [attr.x1]="SP.l" [attr.x2]="SW() - SP.r" [attr.y1]="P.y(t)" [attr.y2]="P.y(t)" />
                        <text class="axis" [attr.x]="SP.l - 6" [attr.y]="P.y(t) + 4" text-anchor="end">{{ t }}%</text>
                      }
                      @for (t of P.xTicks; track t) {
                        <text class="axis" [attr.x]="P.x(t)" [attr.y]="SH - 22" text-anchor="middle">{{ t }}%</text>
                      }
                      <text class="axis" [attr.x]="(SP.l + SW() - SP.r) / 2" [attr.y]="SH - 4" text-anchor="middle">Volatility (annualized)</text>
                      @for (d of P.dots; track d.key) {
                        <g class="pt" [class.on]="scatterHover() === d.key" (mouseenter)="scatterHover.set(d.key)" (mouseleave)="scatterHover.set(null)">
                          <circle class="hit" [attr.cx]="d.cx" [attr.cy]="d.cy" r="12" />
                          <circle class="dot" [attr.cx]="d.cx" [attr.cy]="d.cy" r="5" />
                          @if (d.label || scatterHover() === d.key) {
                            <text class="pt-label" [attr.x]="d.cx + 8" [attr.y]="d.cy - 8">{{ d.symbol }}</text>
                          }
                          <title>{{ d.symbol }}: return {{ pct(d.ret) }}, volatility {{ pct(d.vol, false) }}</title>
                        </g>
                      }
                    </svg>
                  </div>
                } @else {
                  <p class="muted small">Volatility needs at least three daily closes per instrument.</p>
                }
                <details class="as-table">
                  <summary>Show as table</summary>
                  <div class="t-scroll" tabindex="0" role="group" aria-label="Instrument figures table">
                    <table class="table table-sm">
                      <thead><tr><th scope="col">Instrument</th><th scope="col" class="num">Return</th><th scope="col" class="num">Volatility</th><th scope="col" class="num">Max drawdown</th><th scope="col" class="num">Last close</th></tr></thead>
                      <tbody>
                        @for (m of s.members; track m.key) {
                          <tr>
                            <th scope="row">{{ m.symbol }} <small class="muted">{{ m.provider }}</small></th>
                            <td class="num">{{ pct(m.returnPct) }}</td>
                            <td class="num">{{ pct(m.volatilityPct, false) }}</td>
                            <td class="num">{{ pct(m.maxDrawdownPct) }}</td>
                            <td class="num">{{ m.last.close.toFixed(2) }} {{ m.currency ?? '' }} <small class="muted">{{ m.last.day }}</small></td>
                          </tr>
                        }
                      </tbody>
                    </table>
                  </div>
                </details>
              </figure>

              <!-- How the instruments move together -->
              @if (corr(); as C) {
                <figure class="chart wide">
                  <figcaption><strong>Correlation</strong> <span class="muted">daily returns, {{ range() }}; blue moves together, red moves apart</span></figcaption>
                  <div class="t-scroll" tabindex="0" role="group" aria-label="Correlation grid">
                    <table class="corr">
                      <thead><tr><td></td>@for (n of C.names; track $index) { <th scope="col">{{ n }}</th> }</tr></thead>
                      <tbody>
                        @for (row of C.rows; track $index; let i = $index) {
                          <tr>
                            <th scope="row">{{ C.names[i] }}</th>
                            @for (v of row; track $index; let j = $index) {
                              <td [style.background]="corrColor(v)" [class.ink-light]="v !== null && Math.abs(v) > 0.55" [class.ink-dark]="v !== null && Math.abs(v) <= 0.55" [title]="C.names[i] + ' and ' + C.names[j] + ': ' + (v === null ? 'too few shared days' : v.toFixed(2))">{{ v === null ? '·' : v.toFixed(2) }}</td>
                            }
                          </tr>
                        }
                      </tbody>
                    </table>
                  </div>
                  <div class="corr-scale" aria-hidden="true"><span>−1</span><span class="ramp"></span><span>+1</span></div>
                  <p class="muted small mb-0">A dot means fewer than {{ minCorr }} shared trading days.</p>
                </figure>
              }
            </div>

            <p class="dash-note small muted mb-0">
              Computed in your browser from each instrument's own provider history: daily closes, {{ range() }},
              in each instrument's own currency{{ s.currencies.length > 1 ? ' (' + s.currencies.join(', ') + '; not converted)' : '' }}.
              The group index buys on the first day every instrument has a price and holds without rebalancing.
              @if (s.missing.length) { No history for {{ s.missing.join(', ') }}. }
              @if (s.unweighted.length) { Left out of the index for lack of a weight: {{ s.unweighted.join(', ') }}. }
              @if (skipped()) { {{ skipped() }} instruments beyond the first {{ maxMembers }} were not loaded. }
              @if (loadedAt()) { Loaded {{ loadedAt() }}. }
            </p>
          </div>
        }
      </div>
    </fs-pane>
  `,
  styles: [`
    :host {
      display: block;
      --v-accent: #2a78d6; --v-accent-area: rgba(42, 120, 214, .10);
      --v-pos: #2a78d6; --v-neg: #e34948;
      --v-gray: #b9bcc4; --v-grid: #e6e8ee; --v-base: #c3c2b7;
      --v-1: #2a78d6; --v-2: #eb6834; --v-3: #1baf7a; --v-4: #eda100; --v-5: #e87ba4; --v-6: #008300; --v-other: #a3a6ad;
      --v-surface: var(--fs-surface);
    }
    :host-context([data-bs-theme='dark']) {
      --v-accent: #3987e5; --v-accent-area: rgba(57, 135, 229, .16);
      --v-pos: #3987e5; --v-neg: #e66767;
      --v-gray: #4a5160; --v-grid: #222b3a; --v-base: #383f4c;
      --v-1: #3987e5; --v-2: #d95926; --v-3: #199e70; --v-4: #c98500; --v-5: #d55181; --v-6: #008300; --v-other: #6b7180;
    }
    .dash-loading { display: flex; gap: .6rem; align-items: center; color: var(--fs-muted); padding: 1.5rem 0; }
    .pulse { width: .7rem; height: .7rem; border-radius: 50%; background: var(--v-accent); animation: pulse 1.2s ease-in-out infinite; }
    @keyframes pulse { 50% { opacity: .25; transform: scale(.7); } }
    .spin { display: inline-block; animation: spin 1s linear infinite; }
    @keyframes spin { to { transform: rotate(360deg); } }
    .dash-body { transition: opacity .2s; }
    .dash-body.stale { opacity: .6; }
    .tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(9.5rem, 1fr)); gap: .75rem; margin-bottom: 1rem; }
    .tile { display: flex; flex-direction: column; gap: .15rem; padding: .75rem .9rem; border: 1px solid var(--fs-border); border-radius: var(--fs-radius-sm, 8px); background: var(--fs-surface-2); animation: rise .35s ease-out both; }
    .tile:nth-child(2) { animation-delay: .04s; } .tile:nth-child(3) { animation-delay: .08s; } .tile:nth-child(4) { animation-delay: .12s; } .tile:nth-child(5) { animation-delay: .16s; } .tile:nth-child(6) { animation-delay: .2s; }
    @keyframes rise { from { opacity: 0; transform: translateY(6px); } }
    .tile-label { font-size: .75rem; color: var(--fs-muted); text-transform: uppercase; letter-spacing: .04em; }
    .tile-value { font-size: 1.2rem; font-weight: 600; }
    .tile.hero .tile-value { font-size: 1.75rem; }
    .tile-value small { font-size: .85rem; font-weight: 500; }
    .tile-sub { font-size: .75rem; color: var(--fs-muted); }
    .breadth { display: flex; gap: 2px; height: 6px; margin-top: .3rem; }
    .breadth span { border-radius: 3px; flex-basis: 0; min-width: 0; }
    .b-up { background: var(--v-pos); } .b-down { background: var(--v-neg); } .b-flat { background: var(--v-gray); }
    .charts { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 19rem), 1fr)); gap: 1rem; }
    .chart { margin: 0; padding: .9rem; border: 1px solid var(--fs-border); border-radius: var(--fs-radius-sm, 8px); background: var(--v-surface); min-width: 0; }
    .chart.wide { grid-column: 1 / -1; }
    figcaption { margin-bottom: .5rem; font-size: .9rem; }
    figcaption > .muted { margin-left: .4rem; }
    .cap-row { display: flex; justify-content: space-between; align-items: center; gap: .5rem; flex-wrap: wrap; }
    .cap-select { width: auto; }
    .muted { color: var(--fs-muted); font-weight: 400; }
    .plot { position: relative; }
    .svg { display: block; overflow: visible; max-width: 100%; }
    .svg:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: 2px; border-radius: 4px; }
    .grid { stroke: var(--v-grid); stroke-width: 1; vector-effect: non-scaling-stroke; }
    .grid.base { stroke: var(--v-base); }
    .axis { fill: var(--fs-muted); font-size: 11px; font-variant-numeric: tabular-nums; }
    .member-line { fill: none; stroke-width: 1.75; stroke-linejoin: round; vector-effect: non-scaling-stroke; }
    .member-line.dimmed { opacity: .15; }
    .member-line.highlighted { stroke-width: 3; }
    .group-line { fill: none; stroke: var(--v-accent); stroke-width: 3.25; stroke-linejoin: round; stroke-linecap: round; animation: draw 1s ease-out both; }
    @keyframes draw { from { clip-path: inset(0 100% 0 0); } to { clip-path: inset(0 0 0 0); } }
    .area { fill: var(--v-accent-area); stroke: none; animation: fade .8s ease-out both; }
    @keyframes fade { from { opacity: 0; } }
    .cross { stroke: var(--fs-muted); stroke-width: 1; vector-effect: non-scaling-stroke; }
    .dot { fill: var(--v-accent); stroke: var(--v-surface); stroke-width: 2; }
    .tip { position: absolute; top: 0; transform: translateX(10px); background: var(--fs-surface); border: 1px solid var(--fs-border); border-radius: 6px; padding: .4rem .55rem; font-size: .78rem; box-shadow: 0 4px 14px rgba(0,0,0,.12); pointer-events: none; white-space: nowrap; z-index: 2; }
    .tip.flip { transform: translateX(calc(-100% - 10px)); }
    .tip-day { font-weight: 600; margin-bottom: .15rem; }
    .tip-row { display: flex; align-items: center; gap: .35rem; }
    .sw { display: inline-block; width: .7rem; height: .7rem; border-radius: 3px; flex: 0 0 auto; }
    .sw-accent { background: var(--v-accent); }
    .line-legend { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem; list-style: none; margin: .45rem 0 .6rem; padding: .15rem; max-height: 9rem; overflow: auto; }
    .line-legend button, .index-key { display: inline-flex; align-items: center; gap: .4rem; font-size: .8rem; }
    .index-key { padding: .3rem .45rem .3rem 0; font-weight: 600; }
    .line-legend button { padding: .3rem .5rem; border: 1px solid var(--fs-border); border-radius: 6px; background: var(--v-surface); color: var(--fs-text); text-align: left; }
    .line-legend button[aria-pressed='true'] { border-color: var(--fs-accent); background: var(--fs-accent-soft); }
    .line-swatch, .group-swatch { width: 24px; flex: 0 0 24px; }
    .line-swatch { height: 8px; }
    .line-swatch line { stroke-width: 2; }
    .group-swatch { height: 3px; border-radius: 2px; background: var(--v-accent); }
    @media (max-width: 700px) { .line-legend button { min-height: 36px; } }
    .s1 { fill: var(--v-1); background: var(--v-1); } .s2 { fill: var(--v-2); background: var(--v-2); } .s3 { fill: var(--v-3); background: var(--v-3); }
    .s4 { fill: var(--v-4); background: var(--v-4); } .s5 { fill: var(--v-5); background: var(--v-5); } .s6 { fill: var(--v-6); background: var(--v-6); }
    .so { fill: var(--v-other); background: var(--v-other); }
    .donut-wrap { display: flex; align-items: center; gap: 1rem; flex-wrap: wrap; }
    .donut { width: 150px; height: 150px; flex: 0 0 auto; animation: grow .6s ease-out both; }
    @keyframes grow { from { opacity: 0; transform: rotate(-30deg) scale(.9); } }
    .slice { stroke: var(--v-surface); stroke-width: 2; transition: opacity .15s, transform .15s; transform-origin: 80px 80px; cursor: default; }
    .slice:hover { transform: scale(1.04); }
    .dim { opacity: .35; }
    .donut-total { font-size: 22px; font-weight: 600; fill: var(--fs-text); }
    .donut-sub { font-size: 10px; fill: var(--fs-muted); }
    .legend { list-style: none; padding: 0; margin: 0; flex: 1 1 9rem; min-width: 0; font-size: .82rem; }
    .legend li { display: flex; align-items: center; gap: .45rem; padding: .15rem 0; transition: opacity .15s; }
    .lg-label { flex: 1 1 auto; min-width: 0; overflow-wrap: anywhere; }
    .lg-val { font-variant-numeric: tabular-nums; color: var(--fs-muted); }
    .bars { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: .35rem; }
    .bar-row { display: grid; grid-template-columns: minmax(3.5rem, 6rem) 1fr 4.2rem; align-items: center; gap: .5rem; font-size: .82rem; }
    .bar-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .bar-track { position: relative; height: 12px; }
    .bar-zero { position: absolute; top: -2px; bottom: -2px; width: 1px; background: var(--v-base); }
    .bar { position: absolute; top: 0; bottom: 0; background: var(--v-pos); border-radius: 0 4px 4px 0; animation: bar .6s ease-out both; transform-origin: left; }
    .bar.neg { background: var(--v-neg); border-radius: 4px 0 0 4px; transform-origin: right; }
    @keyframes bar { from { transform: scaleX(0); } }
    .bar-val { text-align: right; font-variant-numeric: tabular-nums; }
    .pt .hit { fill: transparent; }
    .pt .dot { transition: r .15s; }
    .pt.on .dot { r: 7; }
    .pt-label { font-size: 11px; fill: var(--fs-text); paint-order: stroke; stroke: var(--v-surface); stroke-width: 3px; }
    .as-table { margin-top: .5rem; font-size: .82rem; }
    .as-table summary { cursor: pointer; color: var(--fs-muted); min-height: 24px; display: inline-flex; align-items: center; }
    .t-scroll { overflow-x: auto; max-height: 18rem; overflow-y: auto; }
    .t-scroll:focus-visible { outline: 2px solid var(--fs-accent); outline-offset: 2px; }
    .num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
    .corr { border-collapse: separate; border-spacing: 2px; font-size: .75rem; font-variant-numeric: tabular-nums; }
    .corr th { font-weight: 500; padding: .2rem .35rem; white-space: nowrap; text-align: left; }
    .corr thead th { text-align: center; }
    .corr td { min-width: 2.9rem; height: 1.9rem; text-align: center; border-radius: 4px; color: var(--fs-text); }
    .corr td.ink-light { color: #fff; }
    .corr td.ink-dark { color: #0b0b0b; }
    .corr-scale { display: flex; align-items: center; gap: .4rem; font-size: .72rem; color: var(--fs-muted); margin-top: .4rem; }
    .corr-scale .ramp { width: 8rem; height: 8px; border-radius: 4px; background: linear-gradient(90deg, #c23b3b, #f0efec 50%, #1c5cab); }
    .dash-note { margin-top: 1rem; }
    @media (prefers-reduced-motion: reduce) {
      .tile, .donut, .area, .bar, .pulse, .spin { animation: none; }
      .group-line { animation: none; }
      .slice, .slice:hover { transition: none; transform: none; }
    }
  `],
})
export class GroupDashboard {
  private readonly api = inject(MarketService);
  private readonly destroyRef = inject(DestroyRef);
  private readonly theme = inject(ThemeService);

  readonly collection = input.required<Collection>();
  readonly members = input.required<CollectionMember[]>();

  readonly ranges = RANGES;
  readonly maxMembers = GROUP_HISTORY_MEMBERS;
  readonly minCorr = MIN_CORRELATION_DAYS;
  readonly LH = LH; readonly LP = LP;
  readonly SH = SH; readonly SP = SP;
  /** Drawn widths in CSS pixels, measured from the page. */
  readonly LW = signal(640);
  readonly SW = signal(320);
  readonly Math = Math;
  readonly pct = pct;

  readonly range = signal<GroupRange>(readRange());
  readonly loading = signal(false);
  readonly stats = signal<GroupStats | null>(null);
  readonly loadedAt = signal('');
  private readonly closes = signal<Map<string, DailyClose[]>>(new Map());
  readonly hover = signal<Hover | null>(null);
  private readonly highlighted = signal<string | null>(null);
  readonly donutHover = signal<number | null>(null);
  readonly scatterHover = signal<string | null>(null);
  /** The composition the user picked; a model portfolio starts with its weights. */
  readonly picked = signal<Dimension | 'weight' | null>(null);
  readonly dimension = computed<Dimension | 'weight'>(() => {
    const p = this.picked();
    const model = this.collection().kind === 'model';
    return p && (p !== 'weight' || model) ? p : model ? 'weight' : 'type';
  });

  /** Unique instruments, in the group's order. */
  private readonly targets = computed(() => {
    const seen = new Set<string>();
    return this.members().filter(m => {
      const k = instrumentKey(m.instrument);
      if (seen.has(k)) return false;
      seen.add(k);
      return true;
    });
  });
  readonly asked = computed(() => Math.min(this.targets().length, GROUP_HISTORY_MEMBERS));
  readonly skipped = computed(() => Math.max(0, this.targets().length - GROUP_HISTORY_MEMBERS));

  private readonly load$ = new Subject<{ members: CollectionMember[]; range: GroupRange; kind: string; force: boolean }>();

  constructor() {
    this.load$.pipe(
      switchMap(({ members, range, kind, force }) => {
        this.loading.set(true);
        return from(members).pipe(
          mergeMap(m => this.history(m, range, force).pipe(map(series => ({ m, series }))), 4),
          toArray(),
          map(results => ({ results, kind, order: members.map(m => instrumentKey(m.instrument)) })),
        );
      }),
      takeUntilDestroyed(this.destroyRef),
    ).subscribe(({ results, kind, order }) => {
      results.sort((a, b) => order.indexOf(instrumentKey(a.m.instrument)) - order.indexOf(instrumentKey(b.m.instrument)));
      const inputs: GroupMemberInput[] = results.map(({ m, series }) => ({
        key: instrumentKey(m.instrument),
        symbol: m.instrument.symbol,
        provider: m.instrument.provider,
        currency: series?.currency || m.instrument.currency,
        weight: m.weight ?? undefined,
        points: series?.points ?? [],
      }));
      const closes = new Map<string, DailyClose[]>();
      for (const i of inputs) closes.set(i.key, dailyCloses(i.points));
      this.closes.set(closes);
      this.stats.set(computeGroupStats(inputs, kind === 'model' ? 'model' : 'equal'));
      this.loadedAt.set(formatUtcDateTime(Date.now()));
      this.hover.set(null);
      this.loading.set(false);
    });
    effect(() => {
      const members = this.targets().slice(0, GROUP_HISTORY_MEMBERS);
      const range = this.range();
      const kind = this.collection().kind;
      untracked(() => this.load$.next({ members, range, kind, force: false }));
    });
  }

  private history(m: CollectionMember, range: GroupRange, force: boolean) {
    const key = `${instrumentKey(m.instrument)}|${range}`;
    const hit = historyCache.get(key);
    if (!force && hit && Date.now() - hit.at < CACHE_MS) return of(hit.series);
    return this.api.getMarketHistory(m.instrument.symbol, m.instrument.provider, range).pipe(
      catchError(() => of(null)),
      map(series => {
        historyCache.set(key, { at: Date.now(), series });
        return series;
      }),
    );
  }

  setRange(r: GroupRange): void {
    this.range.set(r);
    try { localStorage.setItem(RANGE_KEY, r); } catch { /* the choice lasts for this visit */ }
  }

  refresh(): void {
    this.load$.next({ members: this.targets().slice(0, GROUP_HISTORY_MEMBERS), range: this.range(), kind: this.collection().kind, force: true });
  }

  day(d: string): string { return formatUtcDate(`${d}T00:00:00Z`); }

  readonly indexStart = computed(() => {
    const first = this.stats()?.index[0];
    return first ? this.day(first.day) : 'first common day';
  });
  readonly indexTable = computed(() => [...(this.stats()?.index ?? [])].reverse());

  private readonly seriesStyles = computed(() => {
    const targets = this.targets().slice(0, GROUP_HISTORY_MEMBERS);
    const theme = this.theme.theme();
    return new Map(targets.map(({ instrument }, i) => {
      const { symbol, provider } = instrument;
      const duplicate = targets.some(({ instrument: other }, j) => i !== j && other.symbol.toUpperCase() === symbol.toUpperCase());
      return [instrumentKey(instrument), {
        label: duplicate ? `${symbol} · ${provider}` : symbol,
        color: SERIES_COLORS[i % SERIES_COLORS.length][theme],
        dash: SERIES_DASHES[(i + Math.floor(i / SERIES_COLORS.length)) % SERIES_DASHES.length],
      }];
    }));
  });

  readonly line = computed(() => {
    const s = this.stats();
    if (!s || s.index.length < 2) return null;
    const LW = this.LW();
    const idx = s.index;
    const times = idx.map(p => Date.parse(`${p.day}T00:00:00Z`));
    const t0 = times[0], t1 = times[times.length - 1];
    const x = (t: number) => LP.l + ((t - t0) / Math.max(1, t1 - t0)) * (LW - LP.l - LP.r);
    // Each instrument rebased to 100 on the index's first day, closed days carried forward.
    const members = [...this.closes().entries()].filter(([key]) => key in s.indexWeights).map(([key, closes]) => {
      let c = 0;
      const values = idx.map(p => {
        while (c + 1 < closes.length && closes[c + 1].day <= p.day) c++;
        return closes[c]?.day <= p.day ? closes[c].close : NaN;
      });
      const base = values.find(v => Number.isFinite(v)) ?? NaN;
      return { key, values: values.map(v => (v / base) * 100) };
    });
    const all = [...idx.map(p => p.value), ...members.flatMap(m => m.values.filter(Number.isFinite))];
    let lo = Math.min(...all), hi = Math.max(...all);
    const padY = (hi - lo) * 0.06 || 1;
    lo -= padY; hi += padY;
    const y = (v: number) => LP.t + (1 - (v - lo) / (hi - lo)) * (LH - LP.t - LP.b);
    const path = (vals: number[]) => vals.map((v, i) => (Number.isFinite(v) ? `${i && Number.isFinite(vals[i - 1]) ? 'L' : 'M'}${x(times[i]).toFixed(1)},${y(v).toFixed(1)}` : '')).join('');
    const d = path(idx.map(p => p.value));
    const area = `${d}L${x(t1).toFixed(1)},${(LH - LP.b).toFixed(1)}L${x(t0).toFixed(1)},${(LH - LP.b).toFixed(1)}Z`;
    const xTicks = (LW < 420 ? [0, 0.5, 1] : [0, 0.33, 0.66, 1]).map(f => {
      const t = t0 + f * (t1 - t0);
      return { x: x(t), label: formatUtcDate(t).replace(/,? \d{4}$/, m => (t1 - t0 > 300 * 864e5 ? m : '')) };
    });
    const styles = this.seriesStyles();
    const lines = members.flatMap(({ key, values }) => {
      const style = styles.get(key);
      return style ? [{ key, d: path(values), values, ...style }] : [];
    });
    return { d, area, y, xs: times.map(x), ticks: niceTicks(lo, hi), xTicks, members: lines };
  });

  readonly activeLine = computed(() => this.line()?.members.find(m => m.key === this.highlighted())?.key ?? null);

  toggleLine(key: string): void {
    this.highlighted.update(current => current === key ? null : key);
  }

  readonly hoverMembers = computed(() => {
    const h = this.hover(), L = this.line(), active = this.activeLine();
    if (!h || !L) return [];
    return L.members
      .filter(m => !active || m.key === active)
      .map(m => ({ key: m.key, label: m.label, color: m.color, dash: m.dash, value: m.values[h.i] }))
      .filter(m => Number.isFinite(m.value))
      .sort((a, b) => b.value - a.value)
      .slice(0, 6);
  });

  move(e: MouseEvent): void {
    const L = this.line();
    if (!L) return;
    const svg = e.currentTarget as SVGSVGElement;
    const box = svg.getBoundingClientRect();
    const px = ((e.clientX - box.left) / box.width) * this.LW();
    this.hoverAt(L.xs.reduce((best, x, i) => (Math.abs(x - px) < Math.abs(L.xs[best] - px) ? i : best), 0));
  }

  key(e: KeyboardEvent): void {
    const L = this.line();
    if (!L) return;
    const cur = this.hover()?.i ?? L.xs.length - 1;
    const next = e.key === 'ArrowLeft' ? cur - 1 : e.key === 'ArrowRight' ? cur + 1 : e.key === 'Home' ? 0 : e.key === 'End' ? L.xs.length - 1 : null;
    if (next === null) return;
    e.preventDefault();
    this.hoverAt(Math.max(0, Math.min(L.xs.length - 1, next)));
  }

  private hoverAt(i: number): void {
    const L = this.line();
    if (L) this.hover.set({ i, x: L.xs[i] });
  }

  readonly dimensions = computed(() => {
    const dims: { id: Dimension | 'weight'; label: string }[] = (['type', 'currency', 'exchange', 'country', 'provider'] as Dimension[]).map(id => ({ id, label: DIMENSION_LABELS[id] }));
    if (this.collection().kind === 'model') dims.unshift({ id: 'weight', label: 'Weight by instrument' });
    return dims;
  });

  readonly donut = computed(() => {
    const d = this.dimension();
    const members = this.members();
    const model = this.collection().kind === 'model';
    let rows: { key: string; label: string; value: number }[];
    let note: string;
    let unit: 'count' | 'weight';
    if (d === 'weight') {
      rows = members.filter(m => (m.weight ?? 0) > 0).map(m => ({ key: String(m.id), label: m.instrument.symbol, value: m.weight! }));
      const total = rows.reduce((s, r) => s + r.value, 0);
      if (total < 100) rows.push({ key: '__unallocated', label: 'Unallocated', value: 100 - total });
      note = 'Hypothetical weights you set; not holdings.';
      unit = 'weight';
    } else {
      const c = composition(model ? 'model' : 'watch', members, d);
      unit = c.mode === 'weight' && c.rows.some(r => (r.weight ?? 0) > 0) ? 'weight' : 'count';
      rows = c.rows.map(r => ({ key: r.key || '__unknown', label: r.label, value: unit === 'weight' ? r.weight ?? 0 : r.count }));
      note = c.known === c.total ? `${DIMENSION_LABELS[d]} known for all ${c.total} instruments.` : `${DIMENSION_LABELS[d]} known for ${c.known} of ${c.total}; the rest show as Unknown.`;
      if (unit === 'weight') note += ' Shares of the summed weights.';
    }
    const slices = donutSlices(rows);
    let at = 0;
    const arcs = slices.map((sl, i) => {
      const start = at;
      at += sl.pct / 100;
      const cls = sl.other || sl.key === '__unknown' || sl.key === '__unallocated' ? 'so' : `s${i + 1}`;
      const text = unit === 'weight' ? `${sl.value.toFixed(1)}% · ${sl.pct.toFixed(0)}%` : `${sl.value} · ${sl.pct.toFixed(0)}%`;
      return { ...sl, d: arcPath(80, 80, 72, 46, start, at), cls, text: unit === 'weight' && d === 'weight' ? `${sl.value.toFixed(1)}%` : text };
    });
    return { slices, arcs, note, unit, total: rows.reduce((s, r) => s + r.value, 0) };
  });

  readonly donutCenter = computed(() => {
    const D = this.donut();
    const i = this.donutHover();
    if (i !== null && D.arcs[i]) return { value: `${D.arcs[i].pct.toFixed(0)}%`, label: D.arcs[i].label.slice(0, 18) };
    return { value: `${this.members().length}`, label: 'instruments' };
  });

  readonly donutLabel = computed(() => {
    const D = this.donut();
    const dim = this.dimensions().find(x => x.id === this.dimension())?.label ?? '';
    return `Composition by ${dim.toLowerCase()}: ${D.arcs.map(a => `${a.label} ${a.pct.toFixed(0)}%`).join(', ')}.`;
  });

  readonly bars = computed(() => {
    const s = this.stats();
    if (!s) return [];
    const max = Math.max(...s.members.map(m => Math.abs(m.returnPct)), 0.0001);
    const lo = Math.min(0, ...s.members.map(m => m.returnPct)), hi = Math.max(0, ...s.members.map(m => m.returnPct));
    const span = Math.max(hi - lo, max);
    const zero = ((0 - lo) / span) * 100;
    return [...s.members].sort((a, b) => b.returnPct - a.returnPct).map(m => {
      const w = (Math.abs(m.returnPct) / span) * 100;
      return { key: m.key, symbol: m.symbol, provider: m.provider, returnPct: m.returnPct, width: Math.max(w, 0.5), left: m.returnPct >= 0 ? zero : zero - w };
    });
  });
  readonly zeroAt = computed(() => {
    const s = this.stats();
    if (!s?.members.length) return 0;
    const lo = Math.min(0, ...s.members.map(m => m.returnPct)), hi = Math.max(0, ...s.members.map(m => m.returnPct));
    const max = Math.max(...s.members.map(m => Math.abs(m.returnPct)), 0.0001);
    return ((0 - lo) / Math.max(hi - lo, max)) * 100;
  });

  readonly scatter = computed(() => {
    const s = this.stats();
    const pts = (s?.members ?? []).filter(m => m.volatilityPct !== null);
    if (!s || !pts.length) return null;
    const SW = this.SW();
    const vols = pts.map(m => m.volatilityPct!), rets = pts.map(m => m.returnPct);
    const xTicks = niceTicks(0, Math.max(...vols) * 1.1 || 1, 3);
    const xMax = Math.max(xTicks.at(-1)!, Math.max(...vols) * 1.1);
    let yLo = Math.min(0, ...rets), yHi = Math.max(0, ...rets);
    const pad = (yHi - yLo) * 0.1 || 1;
    yLo -= pad; yHi += pad;
    const x = (v: number) => SP.l + (v / xMax) * (SW - SP.l - SP.r);
    const y = (v: number) => SP.t + (1 - (v - yLo) / (yHi - yLo)) * (SH - SP.t - SP.b);
    const labelled = new Set([s.best?.key, s.worst?.key]);
    return {
      x, y, xTicks, yTicks: niceTicks(yLo, yHi, 4).map(t => Math.round(t * 10) / 10),
      dots: pts.map(m => ({ key: m.key, symbol: m.symbol, ret: m.returnPct, vol: m.volatilityPct!, cx: x(m.volatilityPct!), cy: y(m.returnPct), label: labelled.has(m.key) || pts.length <= 6 })),
    };
  });

  readonly corr = computed(() => {
    const s = this.stats();
    if (!s || s.members.length < 2) return null;
    const list = s.members.slice(0, CORRELATION_MEMBERS);
    const closes = this.closes();
    return { names: list.map(m => m.symbol), rows: correlationMatrix(list.map(m => ({ key: m.key, closes: closes.get(m.key) ?? [] }))) };
  });

  /** Diverging fill: red below zero, a neutral gray at zero, blue above. */
  corrColor(v: number | null): string {
    if (v === null) return 'var(--fs-surface-2)';
    const mix = (a: number[], b: number[], f: number) => a.map((x, i) => Math.round(x + (b[i] - x) * f));
    const mid = [240, 239, 236];
    const c = v >= 0 ? mix(mid, [28, 92, 171], v) : mix(mid, [194, 59, 59], -v);
    return `rgb(${c.join(',')})`;
  }
}
