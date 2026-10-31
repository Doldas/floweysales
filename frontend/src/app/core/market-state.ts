import { DestroyRef, Injectable, computed, effect, inject, signal } from '@angular/core';
import { Observable, map, of } from 'rxjs';

import { MarketService, describeHttpError, type TrackedAsset } from '../services';
import { LiveSseService } from '../live.service';
import { OrdersState } from '../orders-state';
import { NotifyService } from './notify.service';
import {
  HISTORY_RANGES,
  isProviderId,
  type HistoryRange,
  type HistorySeries,
  type MarketData,
  type ProviderCapability,
  type ProviderId,
  type SymbolInfo,
  type TradingRecord,
} from '../models';
import {
  INDICATOR_KEYS,
  computeInsights,
  loadIndicatorFlags,
  saveIndicatorFlags,
  type IndicatorFlags,
  type IndicatorKey,
} from '../indicators';

const FIRST_TICK_TIMEOUT_MS = 25_000;
const MAX_TRACKERS = 16;
const WATCHLIST_KEY = 'fs.research.watchlist';

/**
 * `live`: SYS_ADMIN or auth disabled — server trackers, SSE ticks and the paper book.
 * `research`: USER accounts — history and statements only, with a local watchlist.
 */
export type MarketMode = 'live' | 'research';

/**
 * MarketState is the single source of truth for the active instrument:
 * trackers, provider, history range, live ticks, indicators and insights.
 * Pages (Command Center, Markets, Financials, Portfolio) and the agent read it.
 */
@Injectable({ providedIn: 'root' })
export class MarketState {
  private readonly api = inject(MarketService);
  private readonly stream = inject(LiveSseService);
  private readonly orders = inject(OrdersState);
  private readonly notify = inject(NotifyService);

  readonly mode = signal<MarketMode>('live');
  readonly initialized = signal(false);
  readonly maxTrackers = MAX_TRACKERS;
  /** Providers of the installed plugins; empty until loaded or when none are installed. */
  readonly providers = signal<ProviderCapability[]>([]);
  readonly providersLoaded = signal(false);
  readonly historyRanges = HISTORY_RANGES;

  readonly trackedAssets = signal<TrackedAsset[]>([]);
  readonly trackedSymbol = signal('');
  readonly trackedName = signal('');
  readonly provider = signal<ProviderId>('YAHOO');
  readonly currency = signal('USD');
  readonly connecting = signal(false);
  /** Bumped on every asset switch so the chart clears its series. */
  readonly chartEpoch = signal(0);

  readonly historyRange = signal<HistoryRange>('1M');
  readonly loadingHistory = signal(false);
  readonly marketSeries = signal<HistorySeries | null>(null);
  /** Values the chart derived from history; warm the KPIs before the first tick. */
  readonly historySeed = signal<{ price: number; fastEMA: number; slowEMA: number } | null>(null);

  readonly indicators = signal<IndicatorFlags>(loadIndicatorFlags());
  readonly enabledIndicatorKeys = computed(() => INDICATOR_KEYS.filter(k => this.indicators()[k]));
  readonly insights = computed(() => {
    const s = this.marketSeries();
    return s?.points?.length ? computeInsights(s.points, s.interval) : [];
  });

  readonly latestTick = this.stream.latestTick.asReadonly();
  readonly connected = this.stream.connected.asReadonly();
  readonly attempted = this.stream.attempted.asReadonly();
  readonly marketData = computed<MarketData | null>(() => this.latestTick()?.marketData ?? null);
  readonly hasAssetFundamentals = computed(() => {
    const md = this.marketData();
    return !!md && !!(md.description || md.quote || md.performance || md.holdings || md.risk || md.keyIndicators);
  });

  /** Last close of the loaded history series. */
  private readonly lastClose = computed(() => {
    const points = this.marketSeries()?.points;
    return points?.length ? points[points.length - 1].c : 0;
  });
  readonly livePrice = computed(
    () => this.latestTick()?.marketData?.price ?? this.latestTick()?.price ?? this.historySeed()?.price ?? this.lastClose(),
  );
  /** Change over the loaded range (first → last close), for research mode and as a fallback. */
  readonly rangeChange = computed<number | null>(() => {
    const points = this.marketSeries()?.points;
    if (!points || points.length < 2 || !points[0].c) return null;
    return ((points[points.length - 1].c - points[0].c) / points[0].c) * 100;
  });
  readonly dayChange = computed<number | null>(() => this.marketData()?.quote?.changePercent ?? null);
  readonly totalPnL = this.orders.totalPnL;
  readonly sparkline = computed(() => {
    const points = this.marketSeries()?.points ?? [];
    const step = Math.max(1, Math.ceil(points.length / 80));
    return points.filter((_, i) => i % step === 0).map(p => p.c);
  });

  readonly statusLabel = computed(() => {
    if (this.mode() === 'research') return 'Research mode';
    if (!this.trackedSymbol()) return 'No active tracker';
    if (!this.attempted()) return 'Connecting…';
    if (!this.connected()) return 'Disconnected';
    const status = (this.latestTick()?.status ?? '').toUpperCase();
    if (status.includes('SELL')) return 'Stopped out';
    return 'Live';
  });

  private firstTickTimer: ReturnType<typeof setTimeout> | null = null;
  private gotTickForCurrentAsset = false;

  constructor() {
    effect(() => saveIndicatorFlags(this.indicators()));
    effect(() => {
      const tick = this.stream.latestTick();
      if (tick) this.handleTick(tick);
    });
    inject(DestroyRef).onDestroy(() => this.stream.close());
  }

  /** Restores the active instrument once the session role is known. */
  init(mode: MarketMode): void {
    if (this.initialized()) return;
    this.initialized.set(true);
    this.mode.set(mode);
    this.refreshProviders();
    if (mode === 'research') {
      this.trackedAssets.set(this.loadWatchlist());
      const last = this.readLocal('lastTrackedSymbol');
      const provider = this.readLocal('lastProvider');
      if (isProviderId(provider)) this.provider.set(provider);
      const first = last ? { symbol: last, provider: this.provider() } : this.trackedAssets()[0];
      if (first) this.view(first);
      return;
    }
    this.api.getConfig().subscribe({
      next: cfg => {
        const symbol = (cfg?.symbol ?? this.readLocal('lastTrackedSymbol') ?? 'GIB').toUpperCase();
        const provider = String(cfg?.provider ?? this.readLocal('lastProvider') ?? 'YAHOO').toUpperCase();
        this.provider.set(isProviderId(provider) ? provider : 'YAHOO');
        this.trackedAssets.set(cfg?.trackers ?? []);
        if (this.trackedAssets().length > 0) {
          const active = this.trackedAssets().find(t => t.symbol === symbol && t.provider === this.provider()) ?? this.trackedAssets()[0];
          this.view(active);
        } else {
          // A deliberately empty registry must remain stopped after reload.
          this.stream.close();
          this.trackedSymbol.set('');
        }
      },
      error: () => this.notify.error('Cannot reach the FloweySales engine. Is the backend running?'),
    });
  }

  /**
   * Reloads the installed providers (after a plugin install or removal). When
   * the selected provider is gone, the first installed one is selected.
   */
  refreshProviders(): void {
    this.api.getProviders().subscribe({
      next: list => {
        this.providers.set(list);
        this.providersLoaded.set(true);
        if (list.length && !list.some(p => p.id === this.provider())) this.provider.set(list[0].id);
      },
      error: () => this.providersLoaded.set(true),
    });
  }

  /** Display label of a provider ID. */
  providerLabel(id: ProviderId): string {
    return this.providers().find(p => p.id === id)?.label ?? id;
  }

  setProvider(provider: ProviderId): void {
    this.provider.set(provider);
    this.writeLocal('lastProvider', provider);
  }

  /** Provider-aware ticker search for autocompletes. */
  search(keyword: string, provider: ProviderId = this.provider()): Observable<SymbolInfo[]> {
    const query = keyword.trim();
    if (query.length < 2) return of([]);
    return this.api.search(query, provider).pipe(map(list => list ?? []));
  }

  /** Validates a typed symbol (or takes a search hit) and makes it active. */
  track(target: string | SymbolInfo): void {
    if (typeof target !== 'string') {
      this.launch(target.symbol, target.name);
      return;
    }
    const symbol = target.trim().toUpperCase();
    if (!symbol) {
      this.notify.error('Enter a ticker symbol first.');
      return;
    }
    const provider = this.provider();
    // Avanza numeric orderbook IDs are valid even though search doesn't index them.
    if (provider === 'AVANZA' && /^\d+$/.test(symbol)) {
      this.launch(symbol);
      return;
    }
    this.api.search(symbol, provider).subscribe({
      next: results => {
        if (!results?.length) {
          this.notify.error(`“${symbol}” was not found on ${provider}. Use search to discover supported tickers.`, 'Unknown ticker');
          return;
        }
        // Prefer the exact hit so the provider's own symbol is kept (e.g. BTCUSDT).
        const exact = results.find(r => r.symbol.toUpperCase() === symbol);
        this.launch(exact ? exact.symbol : symbol, (exact ?? results[0]).name);
      },
      error: err => this.notify.error('Ticker validation failed: ' + describeHttpError(err)),
    });
  }

  view(target: TrackedAsset): void {
    if (isProviderId(target.provider)) this.provider.set(target.provider);
    this.launch(target.symbol);
  }

  stop(target: TrackedAsset): void {
    const afterRemoval = (list: TrackedAsset[]) => {
      this.trackedAssets.set(list);
      if (this.trackedSymbol() === target.symbol && this.provider() === target.provider) {
        const next = list[0];
        if (next) this.view(next);
        else {
          this.stream.close();
          this.trackedSymbol.set('');
          this.trackedName.set('');
          this.marketSeries.set(null);
        }
      }
    };
    if (this.mode() === 'research') {
      const list = this.trackedAssets().filter(t => !(t.symbol === target.symbol && t.provider === target.provider));
      this.saveWatchlist(list);
      afterRemoval(list);
      return;
    }
    this.api.stopTracker(target).subscribe({
      next: response => afterRemoval(response.trackers ?? []),
      error: err => this.notify.error('Unable to stop tracker: ' + describeHttpError(err)),
    });
  }

  setHistoryRange(range: HistoryRange): void {
    if (this.historyRange() === range) return;
    this.historyRange.set(range);
    const symbol = this.trackedSymbol();
    if (symbol) this.loadHistory(symbol, this.provider());
  }

  toggleIndicator(key: IndicatorKey): void {
    this.indicators.update(flags => ({ ...flags, [key]: !flags[key] }));
  }

  clearHistory(): void {
    this.api.clearHistory().subscribe({
      next: res => {
        this.notify.success(res?.message ?? 'History cleared.');
        const symbol = this.trackedSymbol();
        if (symbol) this.launch(symbol);
      },
      error: err => this.notify.error('Clear failed: ' + describeHttpError(err)),
    });
  }

  refreshTrackers(): void {
    if (this.mode() === 'research') return;
    this.api.getTrackers().subscribe({ next: data => this.trackedAssets.set(data.trackers ?? []), error: () => {} });
  }

  private launch(symbol: string, name?: string): void {
    const provider = this.provider();
    this.connecting.set(true);
    this.gotTickForCurrentAsset = false;
    if (name) this.trackedName.set(name);
    else if (symbol !== this.trackedSymbol()) this.trackedName.set('');
    this.trackedSymbol.set(symbol);
    this.writeLocal('lastTrackedSymbol', symbol);
    this.writeLocal('lastProvider', provider);
    this.chartEpoch.update(n => n + 1);
    this.historySeed.set(null);
    this.marketSeries.set(null);

    if (this.mode() === 'research') {
      const list = this.trackedAssets();
      if (!list.some(t => t.symbol === symbol && t.provider === provider)) {
        const next = [{ symbol, provider }, ...list].slice(0, MAX_TRACKERS);
        this.trackedAssets.set(next);
        this.saveWatchlist(next);
      }
      this.loadHistory(symbol, provider, false);
      return;
    }

    this.stream.connectFor(symbol, provider);
    this.orders.loadFor(symbol);
    this.api.applyConfig(symbol, provider).subscribe({
      next: () => {
        this.refreshTrackers();
        this.loadHistory(symbol, provider);
      },
      error: err => {
        this.connecting.set(false);
        this.notify.error('Failed to switch asset: ' + describeHttpError(err));
      },
    });
  }

  private loadHistory(symbol: string, provider: ProviderId, expectTicks = this.mode() === 'live'): void {
    this.loadingHistory.set(true);
    this.api.getMarketHistory(symbol, provider, this.historyRange()).subscribe({
      next: series => {
        if (symbol !== this.trackedSymbol()) return;
        this.loadingHistory.set(false);
        this.connecting.set(false);
        if (series.currency) this.currency.set(series.currency);
        this.marketSeries.set(series);
        if (expectTicks) this.armFirstTickTimeout(symbol);
      },
      error: () => {
        if (symbol !== this.trackedSymbol()) return;
        this.loadingHistory.set(false);
        this.connecting.set(false);
        this.notify.info(`No ${this.historyRange()} history from ${provider} for “${symbol}”.` + (expectTicks ? ' The chart will start with live ticks.' : ''));
        if (expectTicks) this.armFirstTickTimeout(symbol);
      },
    });
  }

  private armFirstTickTimeout(symbol: string): void {
    if (this.firstTickTimer) clearTimeout(this.firstTickTimer);
    this.firstTickTimer = setTimeout(() => {
      if (!this.gotTickForCurrentAsset && this.trackedSymbol() === symbol) {
        this.notify.info(`No live data received for “${symbol}” yet. The market may be closed or the provider does not stream it.`);
      }
    }, FIRST_TICK_TIMEOUT_MS);
  }

  private handleTick(record: TradingRecord): void {
    this.gotTickForCurrentAsset = true;
    if (this.firstTickTimer) {
      clearTimeout(this.firstTickTimer);
      this.firstTickTimer = null;
    }
    this.currency.set(record.marketData?.currency || record.currency || this.currency());
    if (record.marketData?.name && !this.trackedName()) this.trackedName.set(record.marketData.name);
    this.orders.updateMark(record.symbol, record.marketData?.price ?? record.price);
  }

  private loadWatchlist(): TrackedAsset[] {
    try {
      const raw = JSON.parse(localStorage.getItem(WATCHLIST_KEY) ?? '[]');
      return Array.isArray(raw)
        ? raw.filter(t => typeof t?.symbol === 'string' && isProviderId(t?.provider)).slice(0, MAX_TRACKERS)
        : [];
    } catch {
      return [];
    }
  }

  private saveWatchlist(list: TrackedAsset[]): void {
    try {
      localStorage.setItem(WATCHLIST_KEY, JSON.stringify(list));
    } catch {}
  }

  private readLocal(key: string): string | null {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  }

  private writeLocal(key: string, value: string): void {
    try {
      localStorage.setItem(key, value);
    } catch {}
  }
}
