import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  computed,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule, ReactiveFormsModule, FormControl } from '@angular/forms';
import { MatToolbarModule } from '@angular/material/toolbar';
import { MatCardModule } from '@angular/material/card';
import { MatButtonModule } from '@angular/material/button';
import { MatChipsModule } from '@angular/material/chips';
import { MatFormFieldModule } from '@angular/material/form-field';
import { MatInputModule } from '@angular/material/input';
import { MatSelectModule } from '@angular/material/select';
import { MatAutocompleteModule } from '@angular/material/autocomplete';
import { MatIconModule } from '@angular/material/icon';
import { MatButtonToggleModule } from '@angular/material/button-toggle';
import { MatSnackBar, MatSnackBarModule } from '@angular/material/snack-bar';
import { MatProgressBarModule } from '@angular/material/progress-bar';
import { MatTooltipModule } from '@angular/material/tooltip';
import {
  BehaviorSubject,
  Subject,
  debounceTime,
  distinctUntilChanged,
  switchMap,
  timer,
} from 'rxjs';

import { MarketService, describeHttpError, type TrackedAsset } from './services';
import { AuthState } from './auth.service';
import { LoginComponent } from './components/login';
import { LiveSseService } from './live.service';
import { OrdersState } from './orders-state';
import { ThemeService, THEMES } from './theme.service';
import { AiChatComponent } from './components/ai-chat';
import { AssetInfoComponent } from './components/asset-info';
import { FinancialsComponent } from './components/financials';
import { TradingChartComponent } from './components/chart';
import { LedgerComponent } from './components/ledger';
import { ForumComponent } from './components/forum';
import { FlowStudioComponent } from './components/flow-studio';
import { ApiSettingsComponent } from './components/api-settings';
import { UserspaceFilesComponent } from './components/userspace-files';
import {
  HISTORY_RANGES,
  PROVIDERS,
  type HistoryRange,
  type HistorySeries,
  type MarketData,
  type ProviderId,
  type SymbolInfo,
  type TradingRecord,
} from './models';
import {
  INDICATOR_KEYS,
  INDICATOR_LABELS,
  computeInsights,
  loadIndicatorFlags,
  saveIndicatorFlags,
  type IndicatorFlags,
  type IndicatorKey,
} from './indicators';

const SEARCH_DEBOUNCE_MS = 250;
const FIRST_TICK_TIMEOUT_MS = 25_000;

@Component({
  selector: 'app-root',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  imports: [
    CommonModule,
    FormsModule,
    ReactiveFormsModule,
    MatToolbarModule,
    MatCardModule,
    MatButtonModule,
    MatFormFieldModule,
    MatInputModule,
    MatSelectModule,
    MatAutocompleteModule,
    MatIconModule,
    MatButtonToggleModule,
    MatChipsModule,
    MatSnackBarModule,
    MatProgressBarModule,
    MatTooltipModule,
    AiChatComponent,
    LoginComponent,
    AssetInfoComponent,
    FinancialsComponent,
    TradingChartComponent,
    LedgerComponent,
    ForumComponent,
    FlowStudioComponent,
    ApiSettingsComponent,
    UserspaceFilesComponent,
  ],
  templateUrl: './app.html',
  styleUrl: './app.scss',
})
export class App {
  private readonly api = inject(MarketService);
  readonly auth = inject(AuthState);
  private readonly stream = inject(LiveSseService);
  private readonly ordersState = inject(OrdersState);
  private readonly snackBar = inject(MatSnackBar);
  private readonly destroyRef = inject(DestroyRef);

  readonly themeService = inject(ThemeService);
  readonly themes = THEMES;
  readonly providers = PROVIDERS;

  readonly workspaceTabs = [
    { id: 'market', label: 'Overview', icon: 'dashboard', adminOnly: false },
    { id: 'charts', label: 'Charts', icon: 'show_chart', adminOnly: false },
    { id: 'financials', label: 'Financials', icon: 'account_balance', adminOnly: false },
    { id: 'assistant', label: 'AI Assistant', icon: 'smart_toy', adminOnly: false },
    { id: 'ledger', label: 'Ledger', icon: 'receipt_long', adminOnly: false },
    { id: 'forum', label: 'Forum', icon: 'forum', adminOnly: false },
    { id: 'files', label: 'My Workspace', icon: 'folder', adminOnly: false },
    { id: 'studio', label: 'Flow Studio', icon: 'account_tree', adminOnly: true },
    { id: 'api-settings', label: 'API Settings', icon: 'settings', adminOnly: true },
  ] as const;
  readonly workspace = signal<(typeof this.workspaceTabs)[number]['id']>(this.initialWorkspace());
  readonly activeWorkspaceLabel = computed(
    () => this.workspaceTabs.find(tab => tab.id === this.workspace())?.label ?? 'Overview',
  );

  private initialWorkspace(): (typeof this.workspaceTabs)[number]['id'] {
    const value = typeof localStorage === 'undefined' ? 'market' : localStorage.getItem('floweysales.workspace');
    return this.workspaceTabs.some(tab => tab.id === value) ? value as (typeof this.workspaceTabs)[number]['id'] : 'market';
  }

  openWorkspace(id: (typeof this.workspaceTabs)[number]['id']): void {
    if ((id === 'studio' || id === 'api-settings') && this.auth.user()?.role !== 'SYS_ADMIN') return;
    this.workspace.set(id);
    localStorage.setItem('floweysales.workspace', id);
  }

  readonly chart = viewChild(TradingChartComponent);

  // ---- tracked asset -------------------------------------------------------
  readonly trackedAssets=signal<TrackedAsset[]>([]);
  readonly trackedSymbol = signal('');
  readonly trackedName = signal('');
  readonly currency = signal('USD');
  readonly provider = signal<ProviderId>('YAHOO');
  readonly chartEpoch = signal(0);
  readonly connecting = signal(false);

  /** History range shown by the chart; shared with the range toggles. */
  readonly historyRange = signal<HistoryRange>('1M');
  readonly loadingHistory = signal(false);
  readonly historyRanges = HISTORY_RANGES;

  /** Last seeded values from market history; warm the KPIs pre-live-tick. */
  readonly historySeed = signal<{ price: number; fastEMA: number; slowEMA: number } | null>(null);
  readonly livePrice = computed(() => this.latestTick()?.marketData?.price ?? this.latestTick()?.price ?? this.historySeed()?.price ?? 0);

  // ---- analysis ------------------------------------------------------------
  readonly indicatorKeys = INDICATOR_KEYS;
  readonly indicatorLabels = INDICATOR_LABELS;
  /** Enabled chart studies, persisted to localStorage. */
  readonly indicators = signal<IndicatorFlags>(loadIndicatorFlags());
  readonly enabledIndicatorKeys = computed(() =>
    INDICATOR_KEYS.filter((k) => this.indicators()[k]),
  );
  /** Full series of the currently loaded history — feeds the insight cards. */
  readonly marketSeries = signal<HistorySeries | null>(null);
  readonly insights = computed(() => {
    const s = this.marketSeries();
    return s?.points?.length ? computeInsights(s.points, s.interval) : [];
  });

  readonly latestTick = this.stream.latestTick.asReadonly();
  readonly connected = this.stream.connected.asReadonly();
  readonly marketData = computed<MarketData | null>(() => this.latestTick()?.marketData ?? null);

  /** Shows the fundamentals card only when the provider returned real data. */
  readonly hasAssetFundamentals = computed(() => {
    const md = this.marketData();
    if (!md) return false;
    return !!(md.description || md.quote || md.performance || md.holdings || md.risk || md.keyIndicators);
  });

  readonly dayChange = computed<number | null>(() => this.marketData()?.quote?.changePercent ?? null);
  readonly pnlFromLedger = this.ordersState.totalPnL;
  readonly totalPnL = computed(() => this.pnlFromLedger());

  // ---- search --------------------------------------------------------------
  readonly symbolCtl = new FormControl<SymbolInfo | string>('', { nonNullable: true });
  readonly searchOptions = signal<SymbolInfo[]>([]);
  readonly searching = signal(false);

  private readonly lastTyped = new BehaviorSubject<string>('');
  private readonly providerChanged = new Subject<void>();
  private firstTickTimer: ReturnType<typeof setTimeout> | null = null;
  private gotTickForCurrentAsset = false;

  constructor() {
    void this.auth.check();
    effect(() => {
      if (this.auth.ready() && this.auth.enabled() && this.auth.user()?.role !== 'SYS_ADMIN' && this.workspace() === 'studio') {
        this.openWorkspace('market');
      }
    });
    // Persist indicator toggles across sessions.
    effect(() => saveIndicatorFlags(this.indicators()));

    // Debounced provider-aware autocomplete search. Fires when the user types
    // or when the provider dropdown changes while a keyword is present.
    this.providerChanged
      .pipe(switchMap(() => this.lastTyped.pipe(distinctUntilChanged())))
      .subscribe((kw: string) => this.runSearch(kw));

    this.symbolCtl.valueChanges
      .pipe(debounceTime(SEARCH_DEBOUNCE_MS))
      .subscribe((v) => {
        const keyword = typeof v === 'string' ? v : v.symbol;
        this.lastTyped.next(keyword);
        if (typeof v !== 'string') {
          // A concrete option got selected: track it immediately.
          const symbol = v.symbol;
          this.updateKpisFromSearch(v);
          this.launchTracking(symbol);
        } else {
          this.runSearch(keyword);
        }
      });

    // Wire live ticks into the UI.
    effect(() => {
      const tick = this.stream.latestTick();
      if (tick) this.handleTick(tick);
    });

    // Restore initial state from backend (falls back to localStorage).
    this.api.getConfig().subscribe({
      next: (cfg) => {
        const symbol = (cfg?.symbol ?? localStorage.getItem('lastTrackedSymbol') ?? 'GIB').toUpperCase();
        this.trackedAssets.set(cfg?.trackers ?? []);
        const provider = (cfg?.provider ?? localStorage.getItem('lastProvider') ?? 'YAHOO') as ProviderId;
        // Normalize to uppercase ID if backend returned a display name
        if (typeof provider === 'string') {
          const p = provider.toUpperCase();
          if (p === 'YAHOO' || p === 'AVANZA' || p === 'FINNHUB' || p === 'BINANCE') {
            this.provider.set(p as ProviderId);
          } else {
            this.provider.set('YAHOO');
          }
        } else {
          this.provider.set('YAHOO');
        }
        this.symbolCtl.setValue(symbol, { emitEvent: false });
        if (this.trackedAssets().length > 0) {
          const active=this.trackedAssets().find(t=>t.symbol===symbol && t.provider===this.provider()) ?? this.trackedAssets()[0];
          this.provider.set(active.provider as ProviderId);
          this.symbolCtl.setValue(active.symbol,{emitEvent:false});
          this.launchTracking(active.symbol);
        } else {
          // A deliberately empty registry must remain stopped after reload.
          this.stream.close();
          this.trackedSymbol.set('');
          this.connecting.set(false);
        }
      },
      error: () => {
        this.showSnack('Cannot reach the trading engine — is the backend running?', true);
      },
    });

    this.destroyRef.onDestroy(() => this.stream.close());
  }

  displayOption = (value: SymbolInfo | string): string =>
    typeof value === 'string' ? value : (value.symbol ?? '');

  readonly statusBadgeText = computed(() => {
    if (!this.stream.attempted()) return 'Connecting Engine…';
    if (!this.stream.connected()) return '🔴 DISCONNECTED';
    const status = (this.latestTick()?.status ?? '').toUpperCase();
    if (status.includes('SELL')) return '🔴 STOPPED OUT';
    return 'LIVE STREAM ACTIVE';
  });

  onProviderChanged(pi: ProviderId): void {
    this.provider.set(pi);
    localStorage.setItem('lastProvider', pi);
    this.providerChanged.next();
  }

  refreshTrackers():void {
    this.api.getTrackers().subscribe({
      next: data=>this.trackedAssets.set(data.trackers??[]),
      error:()=>{}
    });
  }
  viewTracker(target:TrackedAsset):void{
    this.provider.set(target.provider as ProviderId);
    this.symbolCtl.setValue(target.symbol,{emitEvent:false});
    this.launchTracking(target.symbol);
  }
  stopTracker(target:TrackedAsset):void{
    this.api.stopTracker(target).subscribe({
      next: response=>{
        this.trackedAssets.set(response.trackers??[]);
        if(this.trackedSymbol()===target.symbol && this.provider()===target.provider){
          const next=this.trackedAssets()[0];
          if(next)this.viewTracker(next);
          else{this.stream.close();this.trackedSymbol.set('');this.marketSeries.set(null);}
        }
      },
      error:err=>this.showSnack('Unable to stop tracker: '+describeHttpError(err),true)
    });
  }

  changeTheme(themeId: string): void {
    this.themeService.set(themeId as never);
  }

  /** "Track Asset" button handler. */
  trackAsset(): void {
    const raw = typeof this.symbolCtl.value === 'string'
      ? this.symbolCtl.value
      : this.symbolCtl.value.symbol;
    const symbol = raw.trim().toUpperCase();
    if (!symbol) {
      this.showSnack('Enter a ticker symbol first.', true);
      return;
    }
    this.validateAndTrack(symbol);
  }

  resetZoom(): void {
    this.chart()?.resetZoom();
  }

  /** Toggles one or more analysis studies (chip group change handler). */
  onIndicatorsChange(keys: string[]): void {
    const next: IndicatorFlags = { ...this.indicators() };
    for (const k of INDICATOR_KEYS) next[k] = keys.includes(k);
    this.indicators.set(next);
  }

  askedClear = signal(false);
  confirmClearHistory(): void {
    if (!this.askedClear()) {
      this.askedClear.set(true);
      timer(4000).subscribe(() => this.askedClear.set(false));
      return;
    }
    this.askedClear.set(false);
    this.api.clearHistory().subscribe({
      next: (res) => {
        this.showSnack(res?.message ?? 'History cleared.', false);
        this.chartsReset();
        this.launchTracking(this.trackedSymbol());
      },
      error: (err) => this.showSnack('Clear failed: ' + describeHttpError(err), true),
    });
  }

  private validateAndTrack(symbol: string): void {
    const provider = this.provider();
    // Avanza numeric orderbook IDs are valid even though the search
    // endpoint doesn't index them — skip validation for those.
    if (provider === 'AVANZA' && /^\d+$/.test(symbol)) {
      this.launchTracking(symbol);
      return;
    }

    this.api.search(symbol, provider).subscribe({
      next: (results) => {
        // Prefer the exact search hit so the tracked symbol is the
        // provider's own (e.g. "BTCUSDT" on Binance) — no re-suffixing.
        if (!results || results.length === 0) {
          this.showUnknownTickerError(symbol, provider);
          return;
        }
        const exact = results.find(
          (r) => r.symbol.toUpperCase() === symbol.toUpperCase(),
        );
        this.updateKpisFromSearch(exact ?? results[0]);
        this.launchTracking(exact ? exact.symbol : symbol);
      },
      error: (err) => this.showSnack('Ticker validation failed: ' + describeHttpError(err), true),
    });
  }

  private showUnknownTickerError(symbol: string, provider: ProviderId): void {
    this.snackBar.open(
      `Ticker “${symbol}” was not found on ${provider}. Use the search box above to discover supported tickers.`,
      'OK',
      { duration: 7000, panelClass: ['snack-error'] },
    );
  }

  /** Tracks the given symbol with the current provider, resetting state. */
  private launchTracking(symbol: string): void {
    const provider = this.provider();
    this.connecting.set(true);
    this.gotTickForCurrentAsset = false;
    this.persist(symbol);

    this.chartsReset();
    this.historySeed.set(null);
    this.marketSeries.set(null);
    this.stream.connectFor(symbol,provider);
    this.ordersState.loadFor(symbol);

    this.api.applyConfig(symbol, provider).subscribe({
      next: () => {this.refreshTrackers();this.loadMarketHistory(symbol, provider);},
      error: (err) => {
        this.connecting.set(false);
        this.showSnack('Failed to switch asset: ' + describeHttpError(err), true);
      },
    });
  }

  /** Switches the chart range (1D/5D/1M/3M/1Y) and refetches the series. */
  setHistoryRange(range: HistoryRange): void {
    if (this.historyRange() === range) return;
    this.historyRange.set(range);
    const symbol = this.trackedSymbol();
    if (symbol) {
      this.loadMarketHistory(symbol, this.provider());
    }
  }

  /** Seeds the chart with provider OHLC history for the tracked asset. */
  private loadMarketHistory(symbol: string, provider: ProviderId): void {
    this.loadingHistory.set(true);
    this.api.getMarketHistory(symbol, provider, this.historyRange()).subscribe({
      next: (series) => {
        this.loadingHistory.set(false);
        this.connecting.set(false);
        if (series.currency) this.currency.set(series.currency);
        this.marketSeries.set(series);
        this.historySeed.set(this.chart()?.seedSeries(series) ?? null);
        this.armFirstTickTimeout(symbol);
      },
      error: () => {
        this.loadingHistory.set(false);
        this.connecting.set(false);
        this.showSnack(
          `No ${this.historyRange()} market data from ${provider} for “${symbol}” — the chart will start with live ticks.`,
          false,
        );
        this.armFirstTickTimeout(symbol);
      },
    });
  }

  private armFirstTickTimeout(symbol: string): void {
    if (this.firstTickTimer) clearTimeout(this.firstTickTimer);
    this.firstTickTimer = setTimeout(() => {
      if (!this.gotTickForCurrentAsset) {
        this.showSnack(
          `No live data received for “${symbol}” — the market may be closed or the ticker is unsupported by this provider.`,
          true,
        );
      }
    }, FIRST_TICK_TIMEOUT_MS);
  }

  private handleTick(record: TradingRecord, fromHistory = false): void {
    this.gotTickForCurrentAsset = true;
    if (this.firstTickTimer) {
      clearTimeout(this.firstTickTimer);
      this.firstTickTimer = null;
    }

    this.currency.set(record.marketData?.currency || record.currency || this.currency());
    this.ordersState.updateMark(
      record.symbol,
      record.marketData?.price ?? record.price,
    );

    if (!fromHistory) {
      this.chart()?.addTick(record);
    }
  }

  public updateKpisFromSearch(option: SymbolInfo): void {
    if (option?.name) this.trackedName.set(option.name);
  }

  private chartsReset(): void {
    this.chart()?.clear();
    this.chartEpoch.update((n) => n + 1);
  }

  private persist(symbol: string): void {
    localStorage.setItem('lastTrackedSymbol', symbol);
    localStorage.setItem('lastProvider', this.provider());
    this.trackedSymbol.set(symbol);
  }

  private runSearch(keyword: string): void {
    const provider = this.provider();
    if (!keyword || keyword.trim().length < 2) {
      this.searchOptions.set([]);
      return;
    }
    this.searching.set(true);
    this.api.search(keyword.trim(), provider).subscribe({
      next: (list) => {
        this.searching.set(false);
        this.searchOptions.set(list ?? []);
      },
      error: () => {
        this.searching.set(false);
        this.searchOptions.set([]);
      },
    });
  }

  private showSnack(message: string, error: boolean): void {
    this.snackBar.open(message, 'OK', {
      duration: error ? 6000 : 3500,
      panelClass: error ? ['snack-error'] : ['snack-ok'],
    });
  }
}
