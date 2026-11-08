import {
  AfterViewInit,
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  effect,
  inject,
  input,
  output,
  signal,
  viewChild,
} from '@angular/core';
import {
  Chart,
  BarController,
  BarElement,
  LineController,
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  Legend,
  Tooltip,
  Title,
  Filler,
  type ChartConfiguration,
  type ChartDataset,
  type Plugin,
  type ScriptableContext,
} from 'chart.js';
import zoomPlugin from 'chartjs-plugin-zoom';

import { ThemeService } from '../theme.service';
import { DEFAULT_SETTINGS, studyLabel, type IndicatorSettings } from '../indicator-settings';
import {
  DRAWING_TOOLS,
  MAX_DRAWINGS,
  distanceToSegment,
  fibPrices,
  indexAtTime,
  needsSecondPoint,
  type Anchor,
  type Drawing,
  type DrawingKind,
  type DrawingTool,
} from '../core/chart-drawings';
import { barsFor, DEFAULT_CHART_STYLE, isCandleStyle, type ChartStyleId, type Ohlc } from '../core/chart-styles';
import { HistorySeries, TradingRecord } from '../models';
import { formatNumber, formatUtcDateTime, formatUtcMonthDay, formatUtcTime } from '../ui/datetime';
import {
  bollinger,
  ema,
  macd,
  rsi as rsiCalc,
  vwap,
  atr as atrCalc,
  ichimoku as ichimokuCalc,
  stochastic as stochasticCalc,
  DEFAULT_INDICATORS,
  type IndicatorFlags,
  type IndicatorKey,
} from '../indicators';

const UP_COLOR = '#16a34a';
const DOWN_COLOR = '#dc2626';

/** What the bar-drawing plugin needs from the component. */
interface BarsOptions {
  style: ChartStyleId;
  bars: () => Ohlc[];
}

/**
 * What each plugin reads from its component. It is kept beside the chart, not in the plugin
 * options: Chart.js calls a function found in options and passes on what it returns.
 */
const barsState = new WeakMap<Chart, BarsOptions>();
const drawingsState = new WeakMap<Chart, DrawingsOptions>();

/**
 * Draws candles, hollow candles and OHLC bars beneath the studies. The price line
 * dataset stays in the chart (signals, tooltips and the scale use it) but is made
 * transparent while one of these styles is on.
 */
const barsPlugin: Plugin<'line'> = {
  id: 'fsBars',
  beforeDatasetsDraw(chart) {
    const o = barsState.get(chart);
    if (!o || !isCandleStyle(o.style)) return;
    const x = chart.scales['x'];
    const y = chart.scales['y'];
    if (!x || !y) return;
    const bars = o.bars();
    const first = Math.max(0, Math.floor(x.min));
    const last = Math.min(bars.length - 1, Math.ceil(x.max));
    if (last < first) return;
    const step = Math.abs(x.getPixelForValue(first + 1) - x.getPixelForValue(first)) || 8;
    const width = Math.max(1, Math.min(step * 0.7, 28));
    const { ctx, chartArea } = chart;
    ctx.save();
    ctx.beginPath();
    ctx.rect(chartArea.left, chartArea.top, chartArea.width, chartArea.height);
    ctx.clip();
    ctx.lineWidth = 1;
    for (let i = first; i <= last; i++) {
      const b = bars[i];
      const up = b.c >= b.o;
      const color = up ? UP_COLOR : DOWN_COLOR;
      const cx = Math.round(x.getPixelForValue(i)) + 0.5;
      const yo = y.getPixelForValue(b.o);
      const yc = y.getPixelForValue(b.c);
      ctx.strokeStyle = color;
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.moveTo(cx, y.getPixelForValue(b.h));
      ctx.lineTo(cx, y.getPixelForValue(b.l));
      if (o.style === 'ohlc') {
        const half = Math.max(2, width / 2);
        ctx.moveTo(cx - half, yo);
        ctx.lineTo(cx, yo);
        ctx.moveTo(cx, yc);
        ctx.lineTo(cx + half, yc);
        ctx.stroke();
        continue;
      }
      ctx.stroke();
      const left = Math.round(cx - width / 2) + 0.5;
      const top = Math.min(yo, yc);
      const height = Math.max(1, Math.abs(yo - yc));
      if (o.style === 'hollow' && up) {
        ctx.strokeRect(left, top, width, height);
      } else {
        ctx.fillRect(left, top, width, height);
      }
    }
    ctx.restore();
  },
};

const DRAWING_COLORS: Record<DrawingKind, string> = { level: '#f59e0b', trend: '#38bdf8', fib: '#a78bfa' };
const HIT_PX = 7;

/** What the drawing plugin needs from the component. */
interface DrawingsOptions {
  list: () => Drawing[];
  pending: () => Drawing | null;
  selected: () => string | null;
  times: () => number[];
  text: () => string;
}

interface Seg { x1: number; y1: number; x2: number; y2: number; level?: number; price?: number }

/** A drawing as line segments in canvas pixels. A level and a retracement run across the width they cover. */
function segmentsOf(chart: Chart, d: Drawing, times: number[]): Seg[] {
  const x = chart.scales['x'];
  const y = chart.scales['y'];
  if (!x || !y || !times.length) return [];
  const px = (a: Anchor) => x.getPixelForValue(indexAtTime(times, a.t));
  const py = (a: Anchor) => y.getPixelForValue(a.p);
  const area = chart.chartArea;
  if (d.kind === 'level') return [{ x1: area.left, y1: py(d.a), x2: area.right, y2: py(d.a), price: d.a.p }];
  if (!d.b) return [];
  if (d.kind === 'trend') return [{ x1: px(d.a), y1: py(d.a), x2: px(d.b), y2: py(d.b) }];
  const left = Math.min(px(d.a), px(d.b));
  const right = Math.max(px(d.a), px(d.b));
  return fibPrices(d.a, d.b).map(l => {
    const yy = y.getPixelForValue(l.price);
    return { x1: left, y1: yy, x2: right, y2: yy, level: l.level, price: l.price };
  });
}

/** Distance in pixels from a point to the nearest part of a drawing. */
function distanceToDrawing(chart: Chart, d: Drawing, times: number[], px: number, py: number): number {
  let best = Infinity;
  for (const g of segmentsOf(chart, d, times)) best = Math.min(best, distanceToSegment(px, py, g.x1, g.y1, g.x2, g.y2));
  return best;
}

/** Draws the user's levels, trend lines and retracements above the series. */
const drawingsPlugin: Plugin<'line'> = {
  id: 'fsDrawings',
  afterDatasetsDraw(chart) {
    const o = drawingsState.get(chart);
    if (!o) return;
    const times = o.times();
    const items = [...o.list(), ...(o.pending() ? [o.pending()!] : [])];
    if (!items.length || !times.length) return;
    const { ctx, chartArea } = chart;
    ctx.save();
    ctx.beginPath();
    ctx.rect(chartArea.left, chartArea.top, chartArea.width, chartArea.height);
    ctx.clip();
    ctx.font = '11px Inter, sans-serif';
    ctx.textBaseline = 'middle';
    const price = (v: number) => formatNumber(v, { maximumFractionDigits: 4 });
    for (const d of items) {
      const color = DRAWING_COLORS[d.kind];
      const chosen = d.id === o.selected();
      const draft = d === o.pending();
      ctx.strokeStyle = color;
      ctx.fillStyle = color;
      ctx.lineWidth = chosen ? 2.5 : 1.5;
      ctx.setLineDash(draft ? [5, 4] : d.kind === 'level' ? [6, 4] : []);
      const segs = segmentsOf(chart, d, times);
      for (const g of segs) {
        ctx.beginPath();
        ctx.moveTo(g.x1, g.y1);
        ctx.lineTo(g.x2, g.y2);
        ctx.stroke();
      }
      ctx.setLineDash([]);
      if (d.kind === 'fib' && d.b) {
        // The move itself, dotted, so the levels are read against it.
        const x = chart.scales['x'];
        const y = chart.scales['y'];
        ctx.setLineDash([2, 3]);
        ctx.beginPath();
        ctx.moveTo(x.getPixelForValue(indexAtTime(times, d.a.t)), y.getPixelForValue(d.a.p));
        ctx.lineTo(x.getPixelForValue(indexAtTime(times, d.b.t)), y.getPixelForValue(d.b.p));
        ctx.stroke();
        ctx.setLineDash([]);
      }
      for (const g of segs) {
        if (g.price === undefined) continue;
        const label = g.level === undefined ? price(g.price) : `${g.level} · ${price(g.price)}`;
        const w = ctx.measureText(label).width + 8;
        const lx = g.level === undefined ? chartArea.right - w - 2 : Math.max(chartArea.left + 2, g.x1 + 2);
        ctx.globalAlpha = 0.9;
        ctx.fillStyle = color;
        ctx.fillRect(lx, g.y1 - 8, w, 16);
        ctx.globalAlpha = 1;
        ctx.fillStyle = '#0b0e1a';
        ctx.fillText(label, lx + 4, g.y1);
      }
      if (chosen || draft) {
        ctx.fillStyle = color;
        for (const a of [d.a, d.b]) {
          if (!a) continue;
          const x = chart.scales['x'].getPixelForValue(indexAtTime(times, a.t));
          const y = chart.scales['y'].getPixelForValue(a.p);
          ctx.beginPath();
          ctx.arc(x, y, 4, 0, Math.PI * 2);
          ctx.fill();
        }
      }
    }
    ctx.restore();
  },
};

Chart.register(
  drawingsPlugin,
  barsPlugin,
  BarController,
  BarElement,
  LineController,
  LineElement,
  PointElement,
  LinearScale,
  CategoryScale,
  Legend,
  Tooltip,
  Title,
  Filler,
  zoomPlugin,
);

const MAX_POINTS = 4096;
const MAX_SEED_POINTS = 4096;

/** Flag key controlling each analysis dataset (6..15), or null for base. */
const DATASET_FLAG: (IndicatorKey | null)[] = [
  null,
  null,
  null,
  null,
  null,
  null,
  'ema9',
  'ema21',
  'ema50',
  'ema100',
  'ema200',
  'bb',
  'bb',
  'bb',
  'vwap',
  'volume',
  'ichimoku',
  'ichimoku',
  'ichimoku',
  'ichimoku',
];

const EMA_KEYS = ['ema9', 'ema21', 'ema50', 'ema100', 'ema200'] as const;

const EMA_COLORS: Record<string, string> = {
  ema9: '#eab308',
  ema21: '#3b82f6',
  ema50: '#8b5cf6',
  ema100: '#f97316',
  ema200: '#ec4899',
};

/** `#rrggbb` → `rgba(r, g, b, a)`; other colour formats are returned unchanged. */
function withAlpha(color: string, alpha: number): string {
  const m = /^#([0-9a-f]{6})$/i.exec(color.trim());
  if (!m) return color;
  const n = parseInt(m[1], 16);
  return `rgba(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`;
}

function newId(): string {
  return `d${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`;
}

/** Partial OHLC kept per data point for rich tooltips. */
interface PointMeta {
  t: number;
  o?: number;
  h?: number;
  l?: number;
  c: number;
  v?: number;
}

@Component({
  selector: 'fse-trading-chart',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="draw-bar" role="toolbar" aria-label="Drawing tools">
      @for (t of tools; track t.id) {
        <button type="button" class="fs-chip" [attr.aria-pressed]="tool() === t.id" [title]="t.hint" [attr.data-tool]="t.id"
                [disabled]="t.id !== 'cursor' && drawings().length >= maxDrawings" (click)="pickTool(t.id)"><i class="bi" [class]="t.icon" aria-hidden="true"></i> {{ t.label }}</button>
      }
      <button type="button" class="fs-chip danger" [disabled]="!selectedId()" (click)="deleteSelected()"><i class="bi bi-x-lg" aria-hidden="true"></i> Delete selected</button>
      <button type="button" class="fs-chip" [class.danger]="confirmClear()" [disabled]="!drawings().length" (click)="clearDrawings()"><i class="bi bi-trash3" aria-hidden="true"></i> {{ confirmClear() ? 'Confirm' : 'Clear drawings (' + drawings().length + ')' }}</button>
    </div>
    <div class="legend-hint">
      @if (tool() === 'cursor') { <i class="bi bi-mouse" aria-hidden="true"></i> Scroll to zoom · drag to pan · click a drawing to select it } @else { <i class="bi bi-pencil" aria-hidden="true"></i> {{ toolHint() }} · Esc to cancel }
    </div>
    <div class="chart-container" [class.drawing]="tool() !== 'cursor'" tabindex="-1" (keydown)="onKeydown($event)">
      <canvas #canvas></canvas>
    </div>
    @if (flags().rsi) {
      <div class="subchart-container">
        <canvas #rsiCanvas></canvas>
      </div>
    }
    @if (flags().macd) {
      <div class="subchart-container">
        <canvas #macdCanvas></canvas>
      </div>
    }
    @if (flags().stoch) {
      <div class="subchart-container">
        <canvas #stochCanvas></canvas>
      </div>
    }
    @if (flags().atr) {
      <div class="subchart-container">
        <canvas #atrCanvas></canvas>
      </div>
    }
  `,
  styles: [
    `
      :host { display: block; }
      :host { min-width: 0; }
      .legend-hint { font-size: 0.75rem; color: var(--fs-muted); display: flex; gap: 6px; align-items: center; height: 1.1rem; margin-bottom: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
      .draw-bar { display: flex; flex-wrap: wrap; align-items: center; gap: 0.35rem; margin-bottom: 4px; .fs-chip { padding: 0.18rem 0.6rem; font-size: 0.75rem; } .fs-chip.danger { color: var(--bs-danger, #dc2626); } }
      .chart-container { outline: none; }
      .chart-container.drawing canvas { cursor: crosshair; touch-action: none; }
      .chart-container { height: var(--fs-pane-h, 380px); position: relative; min-width: 0; width: 100%; }
      .subchart-container { height: 96px; position: relative; min-width: 0; width: 100%; margin-top: 8px; }
      @media (max-width: 700px) {
        .chart-container { height: var(--fs-pane-h, 270px); }
        .subchart-container { height: 92px; }
        .legend-hint { font-size: 0.7rem; }
      }
      @media (max-width: 390px) { .chart-container { height: var(--fs-pane-h, 240px); } }
    `,
  ],
})
export class TradingChartComponent implements AfterViewInit {
  private readonly theme = inject(ThemeService);
  readonly canvas = viewChild.required<ElementRef<HTMLCanvasElement>>('canvas');
  readonly rsiCanvas = viewChild<ElementRef<HTMLCanvasElement>>('rsiCanvas');
  readonly macdCanvas = viewChild<ElementRef<HTMLCanvasElement>>('macdCanvas');
  readonly stochCanvas = viewChild<ElementRef<HTMLCanvasElement>>('stochCanvas');
  readonly atrCanvas = viewChild<ElementRef<HTMLCanvasElement>>('atrCanvas');

  /** Bumped by the parent to clear history on asset change. */
  readonly epoch = input(0);
  /** Enabled analysis studies (chips in the chart header). */
  readonly flags = input<IndicatorFlags>(DEFAULT_INDICATORS);
  /** The periods the studies are worked out with. */
  readonly settings = input<IndicatorSettings>(DEFAULT_SETTINGS);
  /** How the price series is drawn: candles, bars, line, area and so on. */
  readonly chartStyle = input<ChartStyleId>(DEFAULT_CHART_STYLE);
  /** The user's drawings for this instrument; changes are reported through drawingsChange. */
  readonly drawings = input<Drawing[]>([]);
  readonly drawingsChange = output<Drawing[]>();

  readonly tools = DRAWING_TOOLS;
  readonly maxDrawings = MAX_DRAWINGS;
  readonly tool = signal<DrawingTool>('cursor');
  readonly selectedId = signal<string | null>(null);
  readonly confirmClear = signal(false);
  readonly toolHint = () => DRAWING_TOOLS.find(t => t.id === this.tool())?.hint ?? '';
  /** The drawing being placed: its first point is set and its second follows the pointer. */
  private pending: Drawing | null = null;

  private chart: Chart | null = null;
  private rsiChart: Chart | null = null;
  private macdChart: Chart | null = null;
  private stochChart: Chart | null = null;
  private atrChart: Chart | null = null;
  private signals: (string | null)[] = [];
  private meta: PointMeta[] = [];
  private barsCache: { key: string; bars: Ohlc[] } | null = null;

  /** How x-axis labels are formatted for the loaded series. */
  private xLabelMode: 'intraday' | 'sparse' | 'date' = 'date';

  constructor() {
    // Re-color when the theme changes.
    effect(() => {
      const def = this.theme.def();
      if (this.chart) {
        this.applyStyle(false);
        (this.chart.data.datasets[2] ?? {}).borderColor = def.fastEmaColor;
        (this.chart.data.datasets[3] ?? {}).borderColor = def.slowEmaColor;
        this.chart.update('none');
      }
      for (const c of [this.chart, this.rsiChart, this.macdChart, this.stochChart, this.atrChart]) {
        if (!c) continue;
        for (const scale of Object.values(c.options.scales ?? {})) {
          if (!scale) continue;
          if (scale.grid) scale.grid.color = def.gridColor;
          if (scale.ticks) scale.ticks.color = def.textColor;
        }
        const legend = c.options.plugins?.legend?.labels;
        if (legend) legend.color = def.textColor;
        const title = c.options.plugins?.title;
        if (title) title.color = def.textColor;
        c.update('none');
      }
    });

    effect(() => {
      if (this.epoch() > 0) this.clear();
    });

    effect(() => {
      this.chartStyle();
      this.applyStyle();
    });

    // Drawings changed from outside (another instrument loaded): redraw, and drop a selection that is gone.
    effect(() => {
      const list = this.drawings();
      if (this.selectedId() && !list.some(d => d.id === this.selectedId())) this.selectedId.set(null);
      this.chart?.draw();
    });

    // A drawing tool takes the drag; panning is for the cursor.
    effect(() => {
      const pan = this.tool() === 'cursor';
      const zoom = this.chart?.options.plugins?.zoom?.pan;
      if (zoom) zoom.enabled = pan;
    });

    // Re-render analysis whenever flags or the sub-chart canvases change.
    // Reading the viewChild signals here subscribes us to @if insert/remove.
    effect(() => {
      this.flags();
      this.settings();
      this.rsiCanvas();
      this.macdCanvas();
      this.stochCanvas();
      this.atrCanvas();
      if (this.chart) {
        this.applyIndicators();
        // A study switched on or off, or a period changed, shows at once and not on the next hover.
        this.chart.update('none');
      }
    });
  }

  ngAfterViewInit(): void {
    const ctx = this.canvas().nativeElement.getContext('2d') as CanvasRenderingContext2D;
    this.chart = new Chart(ctx, this.buildConfig());
    this.applyStyle(false);
    const el = this.canvas().nativeElement;
    el.addEventListener('pointerdown', (e) => this.onPointerDown(e));
    el.addEventListener('pointermove', (e) => this.onPointerMove(e));
    this.applyIndicators();
  }

  pickTool(id: DrawingTool): void {
    this.pending = null;
    this.tool.set(this.tool() === id ? 'cursor' : id);
    this.chart?.draw();
  }

  deleteSelected(): void {
    const id = this.selectedId();
    if (!id) return;
    this.selectedId.set(null);
    this.drawingsChange.emit(this.drawings().filter(d => d.id !== id));
  }

  clearDrawings(): void {
    if (!this.confirmClear()) {
      this.confirmClear.set(true);
      setTimeout(() => this.confirmClear.set(false), 4000);
      return;
    }
    this.confirmClear.set(false);
    this.selectedId.set(null);
    this.pending = null;
    this.drawingsChange.emit([]);
  }

  onKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      if (this.pending) this.pending = null;
      else this.tool.set('cursor');
      this.chart?.draw();
    } else if ((event.key === 'Delete' || event.key === 'Backspace') && this.selectedId()) {
      event.preventDefault();
      this.deleteSelected();
    }
  }

  /** The time and price under the pointer, snapped to the nearest bar in time; null outside the plot. */
  private anchorAt(e: PointerEvent): Anchor | null {
    const chart = this.chart;
    if (!chart || !this.meta.length) return null;
    const rect = this.canvas().nativeElement.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const y = e.clientY - rect.top;
    const a = chart.chartArea;
    if (x < a.left || x > a.right || y < a.top || y > a.bottom) return null;
    const idx = Math.max(0, Math.min(this.meta.length - 1, Math.round(chart.scales['x'].getValueForPixel(x) ?? 0)));
    return { t: this.meta[idx].t, p: chart.scales['y'].getValueForPixel(y) ?? 0 };
  }

  private onPointerDown(e: PointerEvent): void {
    const chart = this.chart;
    if (!chart || e.button !== 0) return;
    const tool = this.tool();
    if (tool === 'cursor') {
      const rect = this.canvas().nativeElement.getBoundingClientRect();
      const times = this.meta.map((m) => m.t);
      let best: { id: string; dist: number } | null = null;
      for (const d of this.drawings()) {
        const dist = distanceToDrawing(chart, d, times, e.clientX - rect.left, e.clientY - rect.top);
        if (dist <= HIT_PX && (!best || dist < best.dist)) best = { id: d.id, dist };
      }
      this.selectedId.set(best?.id ?? null);
      this.canvas().nativeElement.parentElement?.focus({ preventScroll: true });
      chart.draw();
      return;
    }
    e.preventDefault();
    const anchor = this.anchorAt(e);
    if (!anchor) return;
    const kind: DrawingKind = tool;
    if (!needsSecondPoint(kind)) {
      this.finish({ id: newId(), kind, a: anchor });
    } else if (!this.pending) {
      this.pending = { id: newId(), kind, a: anchor, b: anchor };
      this.canvas().nativeElement.parentElement?.focus({ preventScroll: true });
      chart.draw();
    } else if (anchor.t === this.pending.a.t && anchor.p === this.pending.a.p) {
      return;
    } else {
      this.finish({ ...this.pending, b: anchor });
    }
  }

  private onPointerMove(e: PointerEvent): void {
    if (!this.pending) return;
    const anchor = this.anchorAt(e);
    if (!anchor) return;
    this.pending = { ...this.pending, b: anchor };
    this.chart?.draw();
  }

  private finish(d: Drawing): void {
    this.pending = null;
    this.tool.set('cursor');
    this.selectedId.set(d.id);
    this.drawingsChange.emit([...this.drawings(), d].slice(-MAX_DRAWINGS));
  }

  /**
   * Seeds the chart with provider OHLC history: price line with gradient
   * fill, client-side EMA overlays matching the backend tracker periods, and
   * running highest/lowest reference lines. Returns the latest seeded values
   * so the parent can warm the KPI cards before the first live tick.
   */
  seedSeries(series: HistorySeries): { price: number; fastEMA: number; slowEMA: number } | null {
    if (!this.chart || !series?.points?.length) return null;

    this.clear();

    const points = series.points;
    const iv = (series.interval || '').toLowerCase();
    const intraday = /^(\d+m|\d+h|5|15|30|60)$/.test(iv);
    const weekly = iv.includes('w');
    const longTerm = !intraday && ['3Y', '5Y', '10Y', 'MAX'].includes(series.range);
    const sparse = weekly || longTerm;
    this.xLabelMode = intraday ? 'intraday' : sparse ? 'sparse' : 'date';

    const labelOf = (t: number, prevYear: number): string => {
      const d = new Date(t * 1000);
      if (intraday) {
        return formatUtcTime(d);
      }
      if (sparse) {
        // Long ranges: label only year transitions so the axis reads 2023,
        // 2024, ... instead of repeating month names across the whole span.
        const year = d.getUTCFullYear();
        return year !== prevYear ? String(year) : '';
      }
      return formatUtcMonthDay(d);
    };

    // EMAs are computed over the full close series (accuracy), then the
    // arrays are stride-sampled to keep the chart responsive on multi-decade
    // ranges (e.g. Yahoo MAX with tens of thousands of bars).
    const fastK = 2 / ((series.emaConfig?.fastPeriod ?? 12) + 1);
    const slowK = 2 / ((series.emaConfig?.slowPeriod ?? 26) + 1);
    let fast = points[0].c;
    let slow = points[0].c;
    let highest = points[0].c;
    let lowest = points[0].c;

    const fullLabels: string[] = [];
    const fullPrices: number[] = [];
    const fullStops: (number | null)[] = [];
    const fullFast: number[] = [];
    const fullSlow: number[] = [];
    const fullHigh: number[] = [];
    const fullLow: number[] = [];
    const fullMeta: PointMeta[] = [];

    const stops = series.stops;
    const hasStops = Array.isArray(stops) && stops.length === points.length;

    // Force the first point to carry a year label so the axis is anchored.
    let prevYear = new Date(points[0].t * 1000).getUTCFullYear() - 1;

    for (let i = 0; i < points.length; i++) {
      const p = points[i];
      fast = p.c * fastK + fast * (1 - fastK);
      slow = p.c * slowK + slow * (1 - slowK);
      highest = Math.max(highest, p.h || p.c);
      lowest = Math.min(lowest, p.l || p.c);

      fullLabels.push(labelOf(p.t, prevYear));
      prevYear = new Date(p.t * 1000).getUTCFullYear();
      fullPrices.push(p.c);
      fullStops.push(hasStops && stops![i] > 0 ? stops![i] : null);
      fullFast.push(fast);
      fullSlow.push(slow);
      fullHigh.push(highest);
      fullLow.push(lowest);
      fullMeta.push({ t: p.t, o: p.o, h: p.h, l: p.l, c: p.c, v: p.v });
    }

    const step = Math.max(1, Math.ceil(fullPrices.length / MAX_SEED_POINTS));
    const labels: string[] = [];
    const prices: number[] = [];
    const stopLine: (number | null)[] = [];
    const fastLine: number[] = [];
    const slowLine: number[] = [];
    const highLine: number[] = [];
    const lowLine: number[] = [];

    for (let i = 0; i < fullPrices.length; i += step) {
      labels.push(fullLabels[i]);
      prices.push(fullPrices[i]);
      stopLine.push(fullStops[i]);
      fastLine.push(fullFast[i]);
      slowLine.push(fullSlow[i]);
      highLine.push(fullHigh[i]);
      lowLine.push(fullLow[i]);
      this.signals.push(null);
      this.meta.push(fullMeta[i]);
    }
    // Always keep the most recent point even when the stride skips it.
    const lastIdx = fullPrices.length - 1;
    if ((lastIdx % step) !== 0) {
      labels.push(fullLabels[lastIdx]);
      prices.push(fullPrices[lastIdx]);
      stopLine.push(fullStops[lastIdx]);
      fastLine.push(fullFast[lastIdx]);
      slowLine.push(fullSlow[lastIdx]);
      highLine.push(fullHigh[lastIdx]);
      lowLine.push(fullLow[lastIdx]);
      this.signals.push(null);
      this.meta.push(fullMeta[lastIdx]);
    }

    const d = this.chart.data;
    d.labels = labels;
    d.datasets[0].data = prices;
    d.datasets[1].data = stopLine;
    d.datasets[2].data = fastLine;
    d.datasets[3].data = slowLine;
    d.datasets[4].data = highLine;
    d.datasets[5].data = lowLine;
    this.applyIndicators();
    this.applyStyle(false);
    // A new series has other bar positions: the old zoom window would show it squashed or empty.
    this.chart.resetZoom('none');
    this.chart.update('none');

    const last = points[points.length - 1];
    return { price: last.c, fastEMA: fast, slowEMA: slow };
  }

  addTick(record: TradingRecord): void {
    if (!this.chart) return;

    const timestamp = record.timestamp || Math.floor(Date.now() / 1000);
    const prevT = this.meta.length ? this.meta[this.meta.length - 1].t : undefined;
    const label = this.xLabelFor(timestamp, prevT);
    const priceVal =
      record.marketData && typeof record.marketData.price === 'number'
        ? record.marketData.price
        : record.price || 0;

    const statusStr = (record.status ?? '').toUpperCase();
    const signal = statusStr.includes('GOLDEN')
      ? 'GOLDEN_CROSS'
      : statusStr.includes('DEATH')
        ? 'DEATH_CROSS'
        : statusStr.includes('SELL')
          ? 'SELL'
          : statusStr.includes('BUY')
            ? 'BUY'
            : null;

    const existing = this.chart.data.datasets[0].data as (number | null)[];
    const known = existing.filter((p): p is number => p !== null);
    const latestHigh = known.length ? Math.max(...known, priceVal) : priceVal;
    const latestLow = known.length ? Math.min(...known, priceVal) : priceVal;
    const highest = record.marketData?.quote?.highest ?? latestHigh;
    const lowest = record.marketData?.quote?.lowest ?? latestLow;

    this.signals.push(signal);
    this.meta.push({ t: timestamp, c: priceVal });
    (this.chart.data.labels as string[]).push(label);
    (this.chart.data.datasets[0].data as (string | number | null)[]).push(priceVal);
    (this.chart.data.datasets[1].data as (string | number | null)[]).push(
      record.stop_loss > 0 ? record.stop_loss : null,
    );
    (this.chart.data.datasets[2].data as (string | number | null)[]).push(record.fast_ema || priceVal);
    (this.chart.data.datasets[3].data as (string | number | null)[]).push(record.slow_ema || priceVal);
    (this.chart.data.datasets[4].data as (string | number | null)[]).push(highest);
    (this.chart.data.datasets[5].data as (string | number | null)[]).push(lowest);

    if ((this.chart.data.labels as string[]).length > MAX_POINTS) {
      (this.chart.data.labels as string[]).shift();
      this.signals.shift();
      this.meta.shift();
      for (const ds of this.chart.data.datasets) {
        (ds.data as unknown[]).shift();
      }
    }
    this.applyIndicators();
    this.chart.update('none');
  }

  /** Formats one x-axis label consistently with the loaded series' mode. */
  private xLabelFor(t: number, prevT?: number): string {
    const d = new Date(t * 1000);
    if (this.xLabelMode === 'intraday') {
      return formatUtcTime(d);
    }
    if (this.xLabelMode === 'sparse') {
      const prevYear = prevT ? new Date(prevT * 1000).getUTCFullYear() : -1;
      return d.getUTCFullYear() !== prevYear ? String(d.getUTCFullYear()) : '';
    }
    return formatUtcMonthDay(d);
  }

  clear(): void {
    this.signals = [];
    this.meta = [];
    if (this.chart) {
      this.chart.data.labels = [];
      for (const ds of this.chart.data.datasets) ds.data = [];
      this.applyIndicators();
      this.chart.update('none');
    }
  }

  resetZoom(): void {
    this.chart?.resetZoom();
  }

  /** The bars the current style draws, recomputed only when the series or the style changes. */
  private currentBars(): Ohlc[] {
    const style = this.chartStyle();
    const last = this.meta.at(-1);
    const key = `${style}|${this.meta.length}|${this.meta[0]?.t}|${last?.t}|${last?.c}`;
    if (this.barsCache?.key !== key) this.barsCache = { key, bars: barsFor(style, this.meta) };
    return this.barsCache.bars;
  }

  /** Dresses the price dataset for the chosen style; bar styles hide the line and let the plugin draw. */
  private applyStyle(update = true): void {
    const chart = this.chart;
    if (!chart) return;
    const style = this.chartStyle();
    const price = this.theme.def().priceColor;
    const ds = chart.data.datasets[0] as ChartDataset<'line'>;
    const bars = isCandleStyle(style);
    const base = this.meta[0]?.c ?? 0;
    ds.borderColor = bars ? 'transparent' : price;
    ds.borderWidth = bars ? 0 : 2;
    ds.tension = style === 'step' || bars ? 0 : 0.2;
    ds.stepped = style === 'step';
    ds.fill =
      style === 'area'
        ? { target: 'origin' }
        : style === 'baseline'
          ? { target: { value: base }, above: withAlpha(UP_COLOR, 0.18), below: withAlpha(DOWN_COLOR, 0.18) }
          : false;
    ds.segment =
      style === 'baseline'
        ? { borderColor: (c) => ((c.p1.parsed.y ?? base) >= base ? UP_COLOR : DOWN_COLOR) }
        : {};
    barsState.set(chart, { style, bars: () => this.currentBars() });
    drawingsState.set(chart, {
      list: () => this.drawings(),
      pending: () => this.pending,
      selected: () => this.selectedId(),
      times: () => this.meta.map((m) => m.t),
      text: () => this.theme.def().textColor,
    });
    if (update) chart.update('none');
  }

  private signalAt(index: number): string | null {
    return this.signals[index] ?? null;
  }

  private fmtPrice(v: number | null | undefined): string {
    return v === undefined || v === null ? '' : formatNumber(v, { maximumFractionDigits: 2 });
  }

  /** Recomputes every enabled study from the meta series and syncs panels. */
  private applyIndicators(): void {
    this.recomputeAnalysis();
    this.syncPanels();
  }

  private recomputeAnalysis(): void {
    const chart = this.chart;
    if (!chart) return;
    const f = this.flags();
    const closes = this.meta.map((m) => m.c);
    const n = closes.length;
    const ds = chart.data.datasets;

    const setSeries = (idx: number, data: (number | null)[], on: boolean): void => {
      const dataset = ds[idx];
      if (!dataset) return;
      dataset.data = on ? data : [];
      dataset.hidden = !on;
    };

    // --- analysis EMAs ------------------------------------------------------
    const cfg = this.settings();
    for (const key of EMA_KEYS) {
      const idx = DATASET_FLAG.indexOf(key);
      const on = f[key] && n > 0;
      setSeries(idx, on ? ema(closes, cfg[key]) : [], on);
      if (ds[idx]) ds[idx].label = studyLabel(key, cfg);
    }

    // --- Bollinger Bands ----------------------------------------------------
    const bbOn = f.bb && n > 0;
    if (bbOn) {
      const bb = bollinger(closes, cfg.bbPeriod, cfg.bbMult);
      setSeries(11, bb.upper, true);
      setSeries(12, bb.mid, true);
      setSeries(13, bb.lower, true);
    } else {
      setSeries(11, [], false);
      setSeries(12, [], false);
      setSeries(13, [], false);
    }

    // --- VWAP ---------------------------------------------------------------
    if (f.vwap && n > 0) {
      setSeries(14, vwap(closes, this.meta.map((m) => m.v ?? null)), true);
    } else {
      setSeries(14, [], false);
    }

    // --- Ichimoku cloud (datasets 16..19) -------------------------------------
    const ichiOn = f.ichimoku && n > 0;
    if (ichiOn) {
      const highs = this.meta.map((m) => m.h ?? m.c);
      const lows = this.meta.map((m) => m.l ?? m.c);
      const ich = ichimokuCalc(highs, lows, cfg.tenkan, cfg.kijun, cfg.senkouB, cfg.kijun);
      setSeries(16, ich.tenkan, true);
      setSeries(17, ich.kijun, true);
      setSeries(18, ich.senkouA, true);
      setSeries(19, ich.senkouB, true);
    } else {
      for (const i of [16, 17, 18, 19]) setSeries(i, [], false);
    }

    // --- Volume bars (scaled to the bottom quarter of the price axis) ------
    const volDs = ds[15];
    if (volDs) {
      const volOn = f.volume && n > 0;
      const volumes: (number | null)[] = volOn ? this.meta.map((m) => m.v ?? null) : [];
      const maxVol = volumes.reduce<number>((mx, v) => (v !== null && v > mx ? v : mx), 0);
      volDs.data = volumes;
      volDs.hidden = !volOn || maxVol === 0;
      const volScale = chart.options.scales?.['vol'] as { max?: number } | undefined;
      if (volScale) volScale.max = maxVol > 0 ? maxVol * 4 : undefined;
    }
  }

  private syncPanels(): void {
    const chart = this.chart;
    if (!chart) return;
    const f = this.flags();
    const labels = chart.data.labels as string[];
    const closes = this.meta.map((m) => m.c);
    const highs = this.meta.map((m) => m.h ?? m.c);
    const lows = this.meta.map((m) => m.l ?? m.c);
    const cfg = this.settings();

    // --- RSI panel ----------------------------------------------------------
    const wantRsi = f.rsi && !!this.rsiCanvas()?.nativeElement;
    if (!wantRsi && this.rsiChart) {
      this.rsiChart.destroy();
      this.rsiChart = null;
    } else if (wantRsi) {
      const vals = rsiCalc(closes, cfg.rsiPeriod);
      if (!this.rsiChart) {
        const ctx = this.rsiCanvas()!.nativeElement.getContext('2d') as CanvasRenderingContext2D;
        this.rsiChart = new Chart(ctx, this.buildRsiConfig(labels, vals));
      } else {
        const d = this.rsiChart.data;
        d.labels = labels;
        d.datasets[0].data = vals;
        d.datasets[1].data = labels.map(() => 70);
        d.datasets[2].data = labels.map(() => 30);
      }
      this.retitle(this.rsiChart, `RSI (${cfg.rsiPeriod})`, `RSI ${cfg.rsiPeriod}`);
      this.rsiChart.update('none');
    }

    // --- MACD panel ---------------------------------------------------------
    const wantMacd = f.macd && !!this.macdCanvas()?.nativeElement;
    if (!wantMacd && this.macdChart) {
      this.macdChart.destroy();
      this.macdChart = null;
    } else if (wantMacd) {
      const m = macd(closes, cfg.macdFast, cfg.macdSlow, cfg.macdSignal);
      if (!this.macdChart) {
        const ctx = this.macdCanvas()!.nativeElement.getContext('2d') as CanvasRenderingContext2D;
        this.macdChart = new Chart(ctx, this.buildMacdConfig(labels, m));
      } else {
        const d = this.macdChart.data;
        d.labels = labels;
        d.datasets[0].data = m.line;
        d.datasets[1].data = m.signal;
        d.datasets[2].data = m.hist;
      }
      this.retitle(this.macdChart, `MACD (${cfg.macdFast}, ${cfg.macdSlow}, ${cfg.macdSignal})`);
      this.macdChart.update('none');
    }

    // --- Stochastic panel -----------------------------------------------------
    const wantStoch = f.stoch && !!this.stochCanvas()?.nativeElement;
    if (!wantStoch && this.stochChart) {
      this.stochChart.destroy();
      this.stochChart = null;
    } else if (wantStoch) {
      const st = stochasticCalc(highs, lows, closes, cfg.stochK, cfg.stochD, cfg.stochSmooth);
      const guides = (v: number) => labels.map(() => v);
      if (!this.stochChart) {
        const ctx = this.stochCanvas()!.nativeElement.getContext('2d') as CanvasRenderingContext2D;
        this.stochChart = new Chart(ctx, this.buildStochConfig(labels, st.k, st.d, guides));
      } else {
        const d = this.stochChart.data;
        d.labels = labels;
        d.datasets[0].data = st.k;
        d.datasets[1].data = st.d;
        d.datasets[2].data = guides(80);
        d.datasets[3].data = guides(20);
      }
      this.retitle(this.stochChart, `Stochastic (${cfg.stochK}, ${cfg.stochSmooth}, ${cfg.stochD})`);
      this.stochChart.update('none');
    }

    // --- ATR panel ------------------------------------------------------------
    const wantAtr = f.atr && !!this.atrCanvas()?.nativeElement;
    if (!wantAtr && this.atrChart) {
      this.atrChart.destroy();
      this.atrChart = null;
    } else if (wantAtr) {
      const vals = atrCalc(highs, lows, closes, cfg.atrPeriod);
      if (!this.atrChart) {
        const ctx = this.atrCanvas()!.nativeElement.getContext('2d') as CanvasRenderingContext2D;
        this.atrChart = new Chart(ctx, this.buildAtrConfig(labels, vals));
      } else {
        this.atrChart.data.labels = labels;
        this.atrChart.data.datasets[0].data = vals;
      }
      this.retitle(this.atrChart, `ATR (${cfg.atrPeriod}), price units`);
      this.atrChart.update('none');
    }
  }

  /** Keeps a panel's title and first dataset name in step with the periods in use. */
  private retitle(chart: Chart, title: string, label?: string): void {
    const t = chart.options.plugins?.title;
    if (t) t.text = title;
    if (label && chart.data.datasets[0]) chart.data.datasets[0].label = label;
  }

  private buildStochConfig(labels: string[], k: (number | null)[], d: (number | null)[], guides: (v: number) => number[]): ChartConfiguration<'line'> {
    const def = this.theme.def();
    return {
      type: 'line',
      data: {
        labels,
        datasets: [
          { label: '%K', data: k, borderColor: '#0ea5e9', borderWidth: 1.5, pointRadius: 0, tension: 0.2 },
          { label: '%D', data: d, borderColor: '#f59e0b', borderWidth: 1.2, pointRadius: 0, tension: 0.2 },
          { label: '80', data: guides(80), borderColor: 'rgba(239, 68, 68, 0.55)', borderWidth: 1, borderDash: [4, 4], pointRadius: 0 },
          { label: '20', data: guides(20), borderColor: 'rgba(22, 163, 74, 0.55)', borderWidth: 1, borderDash: [4, 4], pointRadius: 0 },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { display: true, labels: { color: def.textColor, font: { size: 9 }, boxWidth: 10, filter: (item) => item.datasetIndex! < 2 } },
          title: { display: true, text: 'Stochastic', color: def.textColor, font: { size: 10, weight: 'bold' } },
          tooltip: { callbacks: { label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}` } },
        },
        scales: {
          x: { display: false },
          y: { min: 0, max: 100, ticks: { stepSize: 20, color: def.textColor, font: { size: 9 } }, grid: { color: def.gridColor } },
        },
      },
    };
  }

  private buildAtrConfig(labels: string[], vals: (number | null)[]): ChartConfiguration<'line'> {
    const def = this.theme.def();
    return {
      type: 'line',
      data: { labels, datasets: [{ label: 'ATR', data: vals, borderColor: '#14b8a6', borderWidth: 1.5, pointRadius: 0, tension: 0.2 }] },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { display: false },
          title: { display: true, text: 'ATR', color: def.textColor, font: { size: 10, weight: 'bold' } },
          tooltip: { callbacks: { label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}` } },
        },
        scales: {
          x: { display: false },
          y: { ticks: { color: def.textColor, font: { size: 9 } }, grid: { color: def.gridColor } },
        },
      },
    };
  }

  private buildConfig(): ChartConfiguration<'line'> {
    const def = this.theme.def();
    const emaDs = (key: IndicatorKey, label: string): ChartDataset<'line'> => ({
      label,
      data: [],
      borderColor: EMA_COLORS[key],
      borderWidth: key === 'ema200' ? 2 : 1.3,
      tension: 0.2,
      pointRadius: 0,
      hidden: true,
    });

    return {
      type: 'line',
      data: {
        labels: [],
        datasets: [
          {
            label: 'Price',
            data: [],
            borderColor: def.priceColor,
            borderWidth: 2,
            tension: 0.2,
            fill: { target: 'origin' },
            backgroundColor: (ctx: ScriptableContext<'line'>) => {
              const chart = ctx.chart;
              const area = chart.chartArea;
              if (!area) return 'transparent';
              const g = chart.ctx.createLinearGradient(0, area.top, 0, area.bottom);
              const price = this.theme.def().priceColor;
              g.addColorStop(0, withAlpha(price, 0.22));
              g.addColorStop(1, withAlpha(price, 0));
              return g;
            },
            pointRadius: (ctx: ScriptableContext<'line'>) => {
              const s = this.signalAt(ctx.dataIndex);
              return s === 'BUY' || s === 'SELL' ? 8 : s ? 6 : 0;
            },
            pointBackgroundColor: (ctx: ScriptableContext<'line'>) => {
              switch (this.signalAt(ctx.dataIndex)) {
                case 'BUY': return '#16a34a';
                case 'SELL': return '#dc2626';
                case 'GOLDEN_CROSS': return '#eab308';
                case 'DEATH_CROSS': return '#9333ea';
                default: return this.theme.def().priceColor;
              }
            },
            pointBorderColor: '#ffffff',
            pointBorderWidth: 2,
            pointHoverRadius: 10,
          },
          {
            label: 'Stop Loss',
            data: [],
            borderColor: '#ef4444',
            borderWidth: 1.5,
            borderDash: [4, 4],
            tension: 0,
            pointRadius: 0,
          },
          {
            label: 'Fast EMA',
            data: [],
            borderColor: def.fastEmaColor,
            borderWidth: 1.5,
            tension: 0.2,
            pointRadius: 0,
          },
          { label: 'Slow EMA', data: [], borderColor: def.slowEmaColor, borderWidth: 1.5, tension: 0.2, pointRadius: 0 },
          { label: 'Highest Ref', data: [], borderColor: '#14b8a6', borderWidth: 1, borderDash: [5, 5], tension: 0, pointRadius: 0 },
          { label: 'Lowest Ref', data: [], borderColor: '#f97316', borderWidth: 1, borderDash: [5, 5], tension: 0, pointRadius: 0 },
          // --- analysis datasets (indices 6..15, toggled via chips) ---------
          emaDs('ema9', 'EMA 9'),
          emaDs('ema21', 'EMA 21'),
          emaDs('ema50', 'EMA 50'),
          emaDs('ema100', 'EMA 100'),
          emaDs('ema200', 'EMA 200'),
          {
            label: 'BB Upper',
            data: [],
            borderColor: 'rgba(14, 165, 233, 0.65)',
            borderWidth: 1,
            pointRadius: 0,
            tension: 0.2,
            fill: { target: 13, above: 'rgba(14, 165, 233, 0.07)' },
            hidden: true,
          },
          {
            label: 'BB Basis',
            data: [],
            borderColor: 'rgba(14, 165, 233, 0.4)',
            borderWidth: 1,
            borderDash: [3, 3],
            pointRadius: 0,
            tension: 0.2,
            hidden: true,
          },
          {
            label: 'BB Lower',
            data: [],
            borderColor: 'rgba(14, 165, 233, 0.65)',
            borderWidth: 1,
            pointRadius: 0,
            tension: 0.2,
            hidden: true,
          },
          {
            label: 'VWAP',
            data: [],
            borderColor: '#eab308',
            borderWidth: 1.5,
            borderDash: [6, 3],
            pointRadius: 0,
            tension: 0.2,
            hidden: true,
          },
          {
            label: 'Volume',
            data: [],
            type: 'bar',
            order: -1,
            yAxisID: 'vol',
            backgroundColor: 'rgba(100, 116, 139, 0.32)',
            borderWidth: 0,
            hidden: true,
          } as unknown as ChartDataset<'line'>,
          // --- Ichimoku (indices 16..19) ---------------------------------------
          { label: 'Tenkan', data: [], borderColor: '#2563eb', borderWidth: 1.2, pointRadius: 0, tension: 0, hidden: true },
          { label: 'Kijun', data: [], borderColor: '#dc2626', borderWidth: 1.2, pointRadius: 0, tension: 0, hidden: true },
          {
            label: 'Senkou A',
            data: [],
            borderColor: 'rgba(22, 163, 74, 0.55)',
            borderWidth: 1,
            pointRadius: 0,
            tension: 0,
            fill: { target: 19, above: 'rgba(22, 163, 74, 0.13)', below: 'rgba(220, 38, 38, 0.13)' },
            hidden: true,
          },
          { label: 'Senkou B', data: [], borderColor: 'rgba(220, 38, 38, 0.55)', borderWidth: 1, pointRadius: 0, tension: 0, hidden: true },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: { duration: 200 },
        interaction: { mode: 'index', intersect: false },
        scales: {
          x: {
            grid: { color: def.gridColor },
            ticks: { maxTicksLimit: 10, color: def.textColor },
          },
          y: {
            grid: { color: def.gridColor },
            ticks: { color: def.textColor },
            grace: '3%',
            // Wicks reach beyond the closes the line dataset sets the scale from.
            afterDataLimits: (scale) => {
              if (!isCandleStyle(this.chartStyle())) return;
              const bars = this.currentBars();
              const x = this.chart?.scales['x'];
              const from = Math.max(0, Math.floor(x?.min ?? 0));
              const to = Math.min(bars.length - 1, Math.ceil(x?.max ?? bars.length - 1));
              for (let i = from; i <= to; i++) {
                scale.min = Math.min(scale.min, bars[i].l);
                scale.max = Math.max(scale.max, bars[i].h);
              }
            },
          },
          vol: { display: false, position: 'right', beginAtZero: true, grid: { display: false } },
        },
        plugins: {
          legend: {
            labels: {
              color: def.textColor,
              font: { family: 'Inter, sans-serif', size: 11 },
              // Analysis lines are toggled via the chips, not the legend.
              filter: (item) => !DATASET_FLAG[item.datasetIndex ?? -1],
            },
          },
          tooltip: {
            callbacks: {
              title: (items) => {
                const m = this.meta[items[0]?.dataIndex ?? -1];
                if (!m) return '';
                return formatUtcDateTime(m.t * 1000);
              },
              label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}`,
              afterBody: (items) => {
                const m = this.meta[items[0]?.dataIndex ?? -1];
                if (!m) return [];
                const lines: string[] = [];
                if (m.o !== undefined) {
                  lines.push(`Open ${this.fmtPrice(m.o)}   High ${this.fmtPrice(m.h)}`);
                  lines.push(`Low ${this.fmtPrice(m.l)}   Close ${this.fmtPrice(m.c)}`);
                }
                if (m.v) {
                  lines.push(`Volume ${formatNumber(Math.round(m.v))}`);
                }
                return lines;
              },
            },
          },
          zoom: {
            pan: { enabled: true, mode: 'x' },
            zoom: { wheel: { enabled: true }, pinch: { enabled: true }, mode: 'x' },
          },
        },
      },
    };
  }

  private buildRsiConfig(labels: string[], vals: (number | null)[]): ChartConfiguration<'line'> {
    const def = this.theme.def();
    return {
      type: 'line',
      data: {
        labels,
        datasets: [
          {
            label: 'RSI 14',
            data: vals,
            borderColor: '#8b5cf6',
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.2,
          },
          {
            label: '70',
            data: labels.map(() => 70),
            borderColor: 'rgba(239, 68, 68, 0.55)',
            borderWidth: 1,
            borderDash: [4, 4],
            pointRadius: 0,
          },
          {
            label: '30',
            data: labels.map(() => 30),
            borderColor: 'rgba(22, 163, 74, 0.55)',
            borderWidth: 1,
            borderDash: [4, 4],
            pointRadius: 0,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { display: false },
          title: { display: true, text: 'RSI (14)', color: def.textColor, font: { size: 10, weight: 'bold' } },
          tooltip: { callbacks: { label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}` } },
        },
        scales: {
          x: { display: false },
          y: {
            min: 0,
            max: 100,
            ticks: { stepSize: 30, color: def.textColor, font: { size: 9 } },
            grid: { color: def.gridColor },
          },
        },
      },
    };
  }

  private buildMacdConfig(labels: string[], m: ReturnType<typeof macd>): ChartConfiguration<'line'> {
    const def = this.theme.def();
    return {
      type: 'line',
      data: {
        labels,
        datasets: [
          {
            type: 'bar',
            label: 'Histogram',
            data: m.hist,
            order: -1,
            backgroundColor: (ctx: ScriptableContext<'bar'>) => {
              const y = (ctx.parsed as { y?: number } | undefined)?.y ?? 0;
              return y >= 0 ? 'rgba(22, 163, 74, 0.55)' : 'rgba(220, 38, 38, 0.55)';
            },
            borderWidth: 0,
          } as unknown as ChartDataset<'line'>,
          {
            label: 'MACD',
            data: m.line,
            borderColor: '#0ea5e9',
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.2,
          },
          {
            label: 'Signal',
            data: m.signal,
            borderColor: '#f59e0b',
            borderWidth: 1.2,
            pointRadius: 0,
            tension: 0.2,
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: {
            display: true,
            labels: { color: def.textColor, font: { size: 9 }, boxWidth: 10 },
          },
          title: { display: true, text: 'MACD (12, 26, 9)', color: def.textColor, font: { size: 10, weight: 'bold' } },
          tooltip: { callbacks: { label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}` } },
        },
        scales: {
          x: { display: false },
          y: { ticks: { color: def.textColor, font: { size: 9 } }, grid: { color: def.gridColor } },
        },
      },
    };
  }
}
