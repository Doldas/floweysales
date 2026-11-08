import { ChangeDetectionStrategy, Component, computed, effect, inject, signal, untracked, viewChild } from '@angular/core';
import { HelpButtonComponent } from '../ui/help-button';
import { DecimalPipe } from '@angular/common';
import { RouterLink } from '@angular/router';

import { Access } from '../core/access';
import { AgentSession } from '../core/agent-session';
import { MarketState } from '../core/market-state';
import { NotifyService } from '../core/notify.service';
import { AssetInfoComponent } from '../components/asset-info';
import { PaneComponent } from '../ui/pane';
import { chartMeta, chartSummary, type ChartMetaInput } from '../core/chart-meta';
import { formatUtcDateTime } from '../ui/datetime';
import { LeaderboardComponent } from '../components/leaderboard';
import { TradingChartComponent } from '../components/chart';
import { LedgerComponent } from '../components/ledger';
import { WatchlistComponent } from '../components/watchlist';
import { SymbolSearchComponent } from '../shell/symbol-search';
import { ProgressStatusComponent } from '../ui/progress-status';
import { INDICATOR_KEYS } from '../indicators';
import { SETTING_FIELDS, studyLabel } from '../indicator-settings';
import { parseDrawings, type Drawing } from '../core/chart-drawings';
import { INTERVALS, availableIntervals, emaSeed, intervalSeconds, parseIntervalChoice, resampleSeries, type IntervalChoice, type IntervalId } from '../core/resample';
import { CHART_STYLES, DEFAULT_CHART_STYLE, parseChartStyle, type ChartStyleId } from '../core/chart-styles';

const CHART_STYLE_KEY = 'fs.chartStyle';
const INTERVAL_KEY = 'fs.chartInterval';

function readInterval(): IntervalChoice {
  try { return parseIntervalChoice(localStorage.getItem(INTERVAL_KEY)); } catch { return 'auto'; }
}
const drawingsKey = (provider: string, symbol: string) => `fs.drawings.${provider}:${symbol}`;

function readChartStyle(): ChartStyleId {
  try { return parseChartStyle(localStorage.getItem(CHART_STYLE_KEY)); } catch { return DEFAULT_CHART_STYLE; }
}
import type { TrackedAsset } from '../services';

interface Kpi { label: string; value: string; sub?: string; tone?: 'pos' | 'neg' | ''; icon: string }

/** Markets: active instrument, trackers, chart with studies, insights, fundamentals and the leaderboard. */
@Component({
  selector: 'fs-markets-page',
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [PaneComponent, HelpButtonComponent, DecimalPipe, RouterLink, AssetInfoComponent, LeaderboardComponent, TradingChartComponent, LedgerComponent, WatchlistComponent, SymbolSearchComponent, ProgressStatusComponent],
  templateUrl: './markets-page.html',
  styleUrl: './markets-page.scss',
})
export class MarketsPage {
  readonly market = inject(MarketState);
  readonly access = inject(Access);
  private readonly agent = inject(AgentSession);
  private readonly notify = inject(NotifyService);
  private readonly chart = viewChild(TradingChartComponent);

  /** The tracked instrument on screen, as the watchlist marks it. */
  readonly selectedAsset = computed<TrackedAsset | null>(() => {
    const symbol = this.market.trackedSymbol();
    return symbol ? { symbol, provider: this.market.trackedProvider() } : null;
  });
  readonly chartStyles = CHART_STYLES;
  /** How the price is drawn; kept in this browser. */
  readonly chartStyle = signal<ChartStyleId>(readChartStyle());
  readonly intervals = INTERVALS;
  /** The bar length the user asked for; kept in this browser. */
  readonly interval = signal<IntervalChoice>(readInterval());
  /** Lengths the provider's bars for this range can be merged into. */
  readonly mergeable = computed<IntervalId[]>(() => availableIntervals(intervalSeconds(this.market.marketSeries()?.interval)));
  /** What is drawn: the choice when the loaded bars allow it, else the provider's own bars. */
  readonly shownInterval = computed<IntervalChoice>(() => (this.interval() !== 'auto' && this.mergeable().includes(this.interval() as IntervalId) ? this.interval() : 'auto'));
  readonly shownSeries = computed(() => {
    const s = this.market.marketSeries();
    return s ? resampleSeries(s, this.shownInterval()) : null;
  });
  readonly nativeInterval = computed(() => this.market.marketSeries()?.interval ?? '');
  /** The drawings of the instrument on screen; kept in this browser, one list per provider and symbol. */
  readonly drawings = signal<Drawing[]>([]);
  readonly indicatorKeys = INDICATOR_KEYS;
  readonly settingFields = SETTING_FIELDS;
  /** The setting groups in order, each with its fields, for the settings panel. */
  readonly settingGroups = (() => {
    const groups: { name: string; fields: typeof SETTING_FIELDS }[] = [];
    for (const f of SETTING_FIELDS) {
      const g = groups.find(x => x.name === f.group);
      if (g) g.fields.push(f); else groups.push({ name: f.group, fields: [f] });
    }
    return groups;
  })();
  readonly settingsOpen = signal(false);
  readonly studyLabel = (key: string) => studyLabel(key, this.market.indicatorSettings());
  setSetting(key: (typeof SETTING_FIELDS)[number]['key'], event: Event): void {
    const raw = (event.target as HTMLInputElement).value.trim();
    this.market.setIndicatorSetting(key, raw === '' ? NaN : Number(raw));
  }
  readonly confirmClear = signal(false);

  readonly change = computed(() => this.market.dayChange() ?? this.market.rangeChange());
  readonly changeLabel = computed(() => (this.market.dayChange() !== null ? 'today' : this.market.historyRange()));

  private readonly chartInput = computed<ChartMetaInput>(() => {
    const s = this.shownSeries();
    return {
      provider: s?.provider || this.market.provider(),
      interval: s?.interval,
      mergedFrom: this.shownInterval() === 'auto' ? undefined : this.nativeInterval(),
      currency: s?.currency || this.market.currency(),
      range: this.market.historyRange(),
      times: s?.points?.map(p => p.t),
      emaFast: s?.emaConfig?.fastPeriod,
      emaSlow: s?.emaConfig?.slowPeriod,
      studies: this.market.enabledIndicatorKeys().map(k => this.studyLabel(k)),
    };
  });
  readonly chartFacts = computed(() => chartMeta(this.chartInput(), formatUtcDateTime));
  readonly chartSummary = computed(() => chartSummary(this.chartInput()));

  /** Phones start with the shorter chart they always had; the user can still resize it. */
  readonly chartHeight = typeof window !== 'undefined' && window.innerWidth <= 700 ? 270 : 380;

  readonly kpis = computed<Kpi[]>(() => {
    const cur = this.market.currency();
    const fmt = (n: number | null | undefined) => (n !== null && n !== undefined && Number.isFinite(n) && n !== 0 ? n.toFixed(2) : '—');
    const tick = this.market.latestTick();
    const seed = this.market.historySeed();
    const points = this.market.marketSeries()?.points ?? [];
    const high = points.length ? Math.max(...points.map(p => p.h || p.c)) : null;
    const low = points.length ? Math.min(...points.map(p => p.l || p.c)) : null;
    const price = this.market.livePrice();
    if (this.market.mode() === 'research') {
      const chg = this.market.rangeChange();
      return [
        { label: 'Last close', value: price > 0 ? `${price.toFixed(2)} ${cur}` : '—', icon: 'bi-currency-exchange' },
        { label: `Change · ${this.market.historyRange()}`, value: chg === null ? '—' : `${chg >= 0 ? '+' : ''}${chg.toFixed(2)}%`, tone: chg === null ? '' : chg >= 0 ? 'pos' : 'neg', icon: 'bi-arrow-left-right' },
        { label: 'Range high', value: fmt(high), sub: cur, icon: 'bi-arrow-up' },
        { label: 'Range low', value: fmt(low), sub: cur, icon: 'bi-arrow-down' },
      ];
    }
    const pnl = this.market.totalPnL();
    return [
      { label: 'Latest price', value: price > 0 ? `${price.toFixed(2)} ${cur}` : '—', sub: tick ? 'live tick' : seed ? 'from history' : '', icon: 'bi-currency-exchange' },
      { label: 'Stop loss', value: (tick?.stop_loss ?? 0) > 0 ? `${tick!.stop_loss.toFixed(2)} ${cur}` : 'Standby', tone: 'neg', icon: 'bi-shield-exclamation' },
      { label: 'Fast EMA', value: fmt(tick?.fast_ema ?? seed?.fastEMA), icon: 'bi-lightning' },
      { label: 'Slow EMA', value: fmt(tick?.slow_ema ?? seed?.slowEMA), icon: 'bi-hourglass-split' },
      { label: 'Unrealised P&L', value: `${pnl >= 0 ? '+' : ''}${pnl.toFixed(2)} ${cur}`, tone: pnl > 0 ? 'pos' : pnl < 0 ? 'neg' : '', icon: 'bi-wallet2' },
    ];
  });

  constructor() {
    // Load the drawings of whichever instrument is on screen.
    effect(() => {
      const symbol = this.market.trackedSymbol();
      const provider = this.market.provider();
      if (!symbol) { this.drawings.set([]); return; }
      let stored: Drawing[] = [];
      try { stored = parseDrawings(JSON.parse(localStorage.getItem(drawingsKey(provider, symbol)) ?? '[]')); } catch { /* none saved */ }
      this.drawings.set(stored);
    });
    // Seed the chart whenever history for the active instrument arrives.
    effect(() => {
      const native = this.market.marketSeries();
      const series = this.shownSeries();
      const chart = this.chart();
      if (!chart || !native || !series) return;
      untracked(() => {
        const seed = chart.seedSeries(series);
        // The KPI cards describe the provider's bars, not the merged ones drawn.
        this.market.historySeed.set(series === native ? seed : emaSeed(native));
      });
    });
    // Stream live ticks into the chart.
    effect(() => {
      const tick = this.market.latestTick();
      const chart = this.chart();
      // Live ticks are single prices, not bars of the length on screen, so they wait while bars are merged.
      if (chart && tick && tick.symbol === untracked(this.market.trackedSymbol) && untracked(this.shownInterval) === 'auto') untracked(() => chart.addTick(tick));
    });
  }

  isActive(t: TrackedAsset): boolean {
    return t.symbol === this.market.trackedSymbol() && t.provider === this.market.provider();
  }

  askAgent(): void {
    const sym = this.market.trackedSymbol();
    if (!sym) return;
    const studies = this.market.enabledIndicatorKeys().map(k => this.studyLabel(k)).join(', ');
    this.agent.ask(`Analyze ${sym} on ${this.market.provider()} over the ${this.market.historyRange()} range${studies ? ` using ${studies}` : ''}: trend, momentum, key support and resistance, and what would change the picture.`);
  }

  clearHistory(): void {
    if (!this.confirmClear()) {
      this.confirmClear.set(true);
      setTimeout(() => this.confirmClear.set(false), 4000);
      return;
    }
    this.confirmClear.set(false);
    this.market.clearHistory();
  }

  setDrawings(list: Drawing[]): void {
    this.drawings.set(list);
    const symbol = this.market.trackedSymbol();
    if (!symbol) return;
    const key = drawingsKey(this.market.provider(), symbol);
    try { list.length ? localStorage.setItem(key, JSON.stringify(list)) : localStorage.removeItem(key); } catch { /* the drawings last for this visit */ }
  }

  chooseInterval(choice: IntervalChoice): void {
    this.interval.set(choice);
    try { localStorage.setItem(INTERVAL_KEY, choice); } catch { /* the choice lasts for this visit */ }
  }

  /** Why a length is off: it is finer than the bars the provider sent for this range. */
  intervalHint(id: IntervalId): string {
    const native = this.nativeInterval();
    if (this.mergeable().includes(id)) return `Merge the provider's ${native} bars into ${id} bars`;
    return native ? `The provider sends ${native} bars for ${this.market.historyRange()}; a finer length is not available, and a shorter range may offer it.` : 'Load a price history first.';
  }

  setChartStyle(id: ChartStyleId): void {
    this.chartStyle.set(id);
    try { localStorage.setItem(CHART_STYLE_KEY, id); } catch { /* the choice lasts for this visit */ }
  }

  resetZoom(): void {
    this.chart()?.resetZoom();
  }

  stop(t: TrackedAsset, event: Event): void {
    event.stopPropagation();
    this.market.stop(t);
    this.notify.info(`${t.symbol} removed from ${this.market.mode() === 'live' ? 'trackers' : 'your watchlist'}.`);
  }
}
