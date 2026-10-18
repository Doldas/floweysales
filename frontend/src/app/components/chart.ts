import {
  AfterViewInit,
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  effect,
  inject,
  input,
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
  type ScriptableContext,
} from 'chart.js';
import zoomPlugin from 'chartjs-plugin-zoom';

import { ThemeService } from '../theme.service';
import { HistorySeries, TradingRecord } from '../models';
import {
  bollinger,
  ema,
  macd,
  rsi as rsiCalc,
  vwap,
  DEFAULT_INDICATORS,
  type IndicatorFlags,
  type IndicatorKey,
} from '../indicators';

Chart.register(
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
];

const EMA_PARAMS: Record<string, number> = {
  ema9: 9,
  ema21: 21,
  ema50: 50,
  ema100: 100,
  ema200: 200,
};

const EMA_COLORS: Record<string, string> = {
  ema9: '#f0b429',
  ema21: '#4ea1ff',
  ema50: '#b478ff',
  ema100: '#ff8a4c',
  ema200: '#ff4cd8',
};

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
    <div class="legend-hint">⚡ <b>Scroll</b> Zoom • <b>Drag</b> Pan Axis</div>
    <div class="chart-container">
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
  `,
  styles: [
    `
      :host { display: block; }
      .legend-hint { font-size: 0.85rem; opacity: 0.7; margin-bottom: 4px; }
      .chart-container { height: 380px; position: relative; }
      .subchart-container { height: 96px; position: relative; margin-top: 8px; }
    `,
  ],
})
export class TradingChartComponent implements AfterViewInit {
  private readonly theme = inject(ThemeService);
  readonly canvas = viewChild.required<ElementRef<HTMLCanvasElement>>('canvas');
  readonly rsiCanvas = viewChild<ElementRef<HTMLCanvasElement>>('rsiCanvas');
  readonly macdCanvas = viewChild<ElementRef<HTMLCanvasElement>>('macdCanvas');

  /** Bumped by the parent to clear history on asset change. */
  readonly epoch = input(0);
  /** Enabled analysis studies (chips in the chart header). */
  readonly flags = input<IndicatorFlags>(DEFAULT_INDICATORS);

  private chart: Chart | null = null;
  private rsiChart: Chart | null = null;
  private macdChart: Chart | null = null;
  private signals: (string | null)[] = [];
  private meta: PointMeta[] = [];

  constructor() {
    // Re-color when the theme changes.
    effect(() => {
      const def = this.theme.def();
      if (this.chart) {
        (this.chart.data.datasets[0] ?? {}).borderColor = def.priceColor;
        (this.chart.data.datasets[2] ?? {}).borderColor = def.fastEmaColor;
        this.chart.update('none');
      }
    });

    effect(() => {
      if (this.epoch() > 0) this.clear();
    });

    // Re-render analysis whenever flags or the sub-chart canvases change.
    // Reading the viewChild signals here subscribes us to @if insert/remove.
    effect(() => {
      this.flags();
      this.rsiCanvas();
      this.macdCanvas();
      if (this.chart) this.applyIndicators();
    });
  }

  ngAfterViewInit(): void {
    const ctx = this.canvas().nativeElement.getContext('2d') as CanvasRenderingContext2D;
    this.chart = new Chart(ctx, this.buildConfig());
    this.applyIndicators();
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
    const intraday = /^(5m|15m|1h|4h|5|15|60)$/.test(iv);
    const weekly = iv.includes('w');
    const longTerm = !intraday && ['3Y', '5Y', '10Y', 'MAX'].includes(series.range);

    const labelOf = (t: number): string => {
      const d = new Date(t * 1000);
      if (intraday) {
        return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
      }
      if (weekly || longTerm) {
        return d.toLocaleDateString(undefined, { month: 'short', year: '2-digit' });
      }
      return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
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

    for (let i = 0; i < points.length; i++) {
      const p = points[i];
      fast = p.c * fastK + fast * (1 - fastK);
      slow = p.c * slowK + slow * (1 - slowK);
      highest = Math.max(highest, p.h || p.c);
      lowest = Math.min(lowest, p.l || p.c);

      fullLabels.push(labelOf(p.t));
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
    this.chart.update('none');

    const last = points[points.length - 1];
    return { price: last.c, fastEMA: fast, slowEMA: slow };
  }

  addTick(record: TradingRecord): void {
    if (!this.chart) return;

    const timestamp = record.timestamp || Math.floor(Date.now() / 1000);
    const label = new Date(timestamp * 1000).toLocaleTimeString();
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

  private signalAt(index: number): string | null {
    return this.signals[index] ?? null;
  }

  private fmtPrice(v: number | null | undefined): string {
    return v === undefined || v === null ? '' : v.toLocaleString(undefined, { maximumFractionDigits: 2 });
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
    for (const key of ['ema9', 'ema21', 'ema50', 'ema100', 'ema200'] as IndicatorKey[]) {
      const idx = DATASET_FLAG.indexOf(key);
      const on = f[key] && n > 0;
      setSeries(idx, on ? ema(closes, EMA_PARAMS[key]) : [], on);
    }

    // --- Bollinger Bands ----------------------------------------------------
    const bbOn = f.bb && n > 0;
    if (bbOn) {
      const bb = bollinger(closes, 20, 2);
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

    // --- RSI panel ----------------------------------------------------------
    const wantRsi = f.rsi && !!this.rsiCanvas()?.nativeElement;
    if (!wantRsi && this.rsiChart) {
      this.rsiChart.destroy();
      this.rsiChart = null;
    } else if (wantRsi) {
      const vals = rsiCalc(closes, 14);
      if (!this.rsiChart) {
        const ctx = this.rsiCanvas()!.nativeElement.getContext('2d') as CanvasRenderingContext2D;
        this.rsiChart = new Chart(ctx, this.buildRsiConfig(labels, vals));
      } else {
        const d = this.rsiChart.data;
        d.labels = labels;
        d.datasets[0].data = vals;
        d.datasets[1].data = labels.map(() => 70);
        d.datasets[2].data = labels.map(() => 30);
        this.rsiChart.update('none');
      }
    }

    // --- MACD panel ---------------------------------------------------------
    const wantMacd = f.macd && !!this.macdCanvas()?.nativeElement;
    if (!wantMacd && this.macdChart) {
      this.macdChart.destroy();
      this.macdChart = null;
    } else if (wantMacd) {
      const m = macd(closes);
      if (!this.macdChart) {
        const ctx = this.macdCanvas()!.nativeElement.getContext('2d') as CanvasRenderingContext2D;
        this.macdChart = new Chart(ctx, this.buildMacdConfig(labels, m));
      } else {
        const d = this.macdChart.data;
        d.labels = labels;
        d.datasets[0].data = m.line;
        d.datasets[1].data = m.signal;
        d.datasets[2].data = m.hist;
        this.macdChart.update('none');
      }
    }
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
              g.addColorStop(0, 'rgba(0, 255, 157, 0.22)');
              g.addColorStop(1, 'rgba(0, 255, 157, 0)');
              return g;
            },
            pointRadius: (ctx: ScriptableContext<'line'>) => {
              const s = this.signalAt(ctx.dataIndex);
              return s === 'BUY' || s === 'SELL' ? 8 : s ? 6 : 0;
            },
            pointBackgroundColor: (ctx: ScriptableContext<'line'>) => {
              switch (this.signalAt(ctx.dataIndex)) {
                case 'BUY': return '#00ff9d';
                case 'SELL': return '#ff0055';
                case 'GOLDEN_CROSS': return '#ffd700';
                case 'DEATH_CROSS': return '#9d00ff';
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
            borderColor: '#ff0055',
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
          { label: 'Slow EMA', data: [], borderColor: '#ffaa00', borderWidth: 1.5, tension: 0.2, pointRadius: 0 },
          { label: 'Highest Ref', data: [], borderColor: '#00ffcc', borderWidth: 1, borderDash: [5, 5], tension: 0, pointRadius: 0 },
          { label: 'Lowest Ref', data: [], borderColor: '#ff6600', borderWidth: 1, borderDash: [5, 5], tension: 0, pointRadius: 0 },
          // --- analysis datasets (indices 6..15, toggled via chips) ---------
          emaDs('ema9', 'EMA 9'),
          emaDs('ema21', 'EMA 21'),
          emaDs('ema50', 'EMA 50'),
          emaDs('ema100', 'EMA 100'),
          emaDs('ema200', 'EMA 200'),
          {
            label: 'BB Upper',
            data: [],
            borderColor: 'rgba(0, 240, 255, 0.65)',
            borderWidth: 1,
            pointRadius: 0,
            tension: 0.2,
            fill: { target: 13, above: 'rgba(0, 240, 255, 0.06)' },
            hidden: true,
          },
          {
            label: 'BB Basis',
            data: [],
            borderColor: 'rgba(0, 240, 255, 0.4)',
            borderWidth: 1,
            borderDash: [3, 3],
            pointRadius: 0,
            tension: 0.2,
            hidden: true,
          },
          {
            label: 'BB Lower',
            data: [],
            borderColor: 'rgba(0, 240, 255, 0.65)',
            borderWidth: 1,
            pointRadius: 0,
            tension: 0.2,
            hidden: true,
          },
          {
            label: 'VWAP',
            data: [],
            borderColor: '#ffd700',
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
            backgroundColor: 'rgba(125, 133, 144, 0.35)',
            borderWidth: 0,
            hidden: true,
          } as unknown as ChartDataset<'line'>,
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: { duration: 200 },
        interaction: { mode: 'index', intersect: false },
        scales: {
          x: {
            grid: { color: 'rgba(255,255,255,0.05)' },
            ticks: { maxTicksLimit: 10, color: '#7d8590' },
          },
          y: { grid: { color: 'rgba(255,255,255,0.05)' }, ticks: { color: '#7d8590' } },
          vol: { display: false, position: 'right', beginAtZero: true, grid: { display: false } },
        },
        plugins: {
          legend: {
            labels: {
              color: '#f0f6fc',
              font: { family: 'Orbitron, sans-serif', size: 11 },
              // Analysis lines are toggled via the chips, not the legend.
              filter: (item) => !DATASET_FLAG[item.datasetIndex ?? -1],
            },
          },
          tooltip: {
            callbacks: {
              title: (items) => {
                const m = this.meta[items[0]?.dataIndex ?? -1];
                if (!m) return '';
                const d = new Date(m.t * 1000);
                return d.toLocaleString(undefined, {
                  month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
                });
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
                  lines.push(`Volume ${Math.round(m.v).toLocaleString()}`);
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
    return {
      type: 'line',
      data: {
        labels,
        datasets: [
          {
            label: 'RSI 14',
            data: vals,
            borderColor: '#b478ff',
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.2,
          },
          {
            label: '70',
            data: labels.map(() => 70),
            borderColor: 'rgba(255, 80, 80, 0.55)',
            borderWidth: 1,
            borderDash: [4, 4],
            pointRadius: 0,
          },
          {
            label: '30',
            data: labels.map(() => 30),
            borderColor: 'rgba(80, 255, 128, 0.55)',
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
          title: { display: true, text: 'RSI (14)', color: '#7d8590', font: { size: 10, weight: 'bold' } },
          tooltip: { callbacks: { label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}` } },
        },
        scales: {
          x: { display: false },
          y: {
            min: 0,
            max: 100,
            ticks: { stepSize: 30, color: '#7d8590', font: { size: 9 } },
            grid: { color: 'rgba(255,255,255,0.05)' },
          },
        },
      },
    };
  }

  private buildMacdConfig(labels: string[], m: ReturnType<typeof macd>): ChartConfiguration<'line'> {
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
              return y >= 0 ? 'rgba(0, 255, 157, 0.55)' : 'rgba(255, 0, 85, 0.55)';
            },
            borderWidth: 0,
          } as unknown as ChartDataset<'line'>,
          {
            label: 'MACD',
            data: m.line,
            borderColor: '#00f0ff',
            borderWidth: 1.5,
            pointRadius: 0,
            tension: 0.2,
          },
          {
            label: 'Signal',
            data: m.signal,
            borderColor: '#ffaa00',
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
            labels: { color: '#7d8590', font: { size: 9 }, boxWidth: 10 },
          },
          title: { display: true, text: 'MACD (12, 26, 9)', color: '#7d8590', font: { size: 10, weight: 'bold' } },
          tooltip: { callbacks: { label: (item) => `${item.dataset.label}: ${this.fmtPrice(item.parsed?.y)}` } },
        },
        scales: {
          x: { display: false },
          y: { ticks: { color: '#7d8590', font: { size: 9 } }, grid: { color: 'rgba(255,255,255,0.05)' } },
        },
      },
    };
  }
}
