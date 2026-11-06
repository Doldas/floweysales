import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, catchError, map, of } from 'rxjs';
import {
  IndicesResponse,
  AboutInfo,
  ForumPost,
  FinancialsResponse,
  HistorySeries,
  BacktestParams,
  BacktestResult, StrategyMonteCarloRequest, StrategyMonteCarloResult, BatchResult, SensitivityResult, MonteCarloResult, SavedBacktest, BacktestSummary,
  LeaderboardResponse,
  RankingPreferences,
  RankingPreferencesResponse,
  MarketData,
  Order,
  OrderRequest,
  OrderResponse,
  CompanyWorld,
  EventsResponse,
  PersonaCatalog,
  ModelProfile,
  SavedTeam,
  SavedTeamMember,
  SavedStrategy,
  StrategyCatalog,
  StrategyDef,
  StrategyRunRequest,
  FitRequest,
  FitResult,
  StrategyRunResult,
  WorldView,
  PlanProfile,
  TodayBriefing,
  SizeResponse,
  PortfolioAnalyticsResponse,
  PlanResponse,
  PortfolioResponse,
  ProviderCapability,
  SymbolInfo,
  TradingRecord,
} from './models';

export interface TrackedAsset{
 symbol:string;
 provider:string;
}
export interface SharedConfig {
 symbol:string;
 provider:string;
 trackers?:TrackedAsset[];
}

@Injectable({ providedIn: 'root' })
export class MarketService {
  private readonly http = inject(HttpClient);

  getTrackers():Observable<{trackers:TrackedAsset[];max_trackers:number}>{
    return this.http.get<{trackers:TrackedAsset[];max_trackers:number}>('/api/trackers');
  }
  stopTracker(t:TrackedAsset):Observable<{trackers:TrackedAsset[]}>{
    return this.http.delete<{trackers:TrackedAsset[]}>('/api/trackers',{params:{symbol:t.symbol,provider:t.provider}});
  }

  getAbout(): Observable<AboutInfo> {
    return this.http.get<AboutInfo>('/api/about');
  }

  /** Providers contributed by installed plugins. */
  getProviders(): Observable<ProviderCapability[]> {
    return this.http.get<ProviderCapability[]>('/api/providers').pipe(map(list => list ?? []));
  }

  getConfig(): Observable<SharedConfig> {
    return this.http.get<SharedConfig>('/api/config');
  }

  applyConfig(symbol: string, provider: string): Observable<SharedConfig> {
    return this.http.post<SharedConfig>('/api/config', { symbol, provider });
  }

  getPriceHistory(symbol: string, limit = 2000): Observable<TradingRecord[]> {
    const params = { symbol, limit };
    return this.http.get<TradingRecord[]>('/api/prices', { params });
  }

  /** Provider OHLC series used to seed the chart when tracking starts. */
  getMarketHistory(symbol: string, provider: string, range: string): Observable<HistorySeries> {
    return this.http.get<HistorySeries>('/api/history', { params: { symbol, provider, range } });
  }

  /** Quarterly income-statement reports for the tracked ticker. */
  getQuarterlyFinancials(symbol: string, provider: string, fromYear?: number, toYear?: number): Observable<FinancialsResponse> {
    const params: Record<string,string> = { symbol, provider };
    if (fromYear !== undefined && toYear !== undefined) {
      params['from_year'] = String(fromYear);
      params['to_year'] = String(toYear);
    }
    return this.http.get<FinancialsResponse>('/api/financials', { params });
  }

  /** The index registry: each index with its provenance, or why it is unavailable. */
  getIndices(): Observable<IndicesResponse> {
    return this.http.get<IndicesResponse>('/api/market/indices');
  }

  /** Imports the user's own CSV file of constituents for an index. */
  importIndexCsv(id: string, body: { csv: string; as_of: string; source_name: string; license_terms: string; license_confirmed: boolean }): Observable<{ saved: boolean }> {
    return this.http.put<{ saved: boolean }>(`/api/market/indices/${encodeURIComponent(id)}/csv`, body);
  }

  /** Fetches constituents with the user's own saved Financial Modeling Prep key. */
  fetchIndexFromFmp(id: string, body: { license_terms: string; license_confirmed: boolean }): Observable<{ saved: boolean }> {
    return this.http.post<{ saved: boolean }>(`/api/market/indices/${encodeURIComponent(id)}/fmp`, body);
  }

  removeIndexSource(id: string): Observable<{ deleted: boolean }> {
    return this.http.delete<{ deleted: boolean }>(`/api/market/indices/${encodeURIComponent(id)}/source`);
  }

  /** The key is write-only: the server never returns it. */
  saveFmpKey(apiKey: string): Observable<{ saved: boolean }> {
    return this.http.put<{ saved: boolean }>('/api/market/indices/fmp-key', { api_key: apiKey });
  }

  removeFmpKey(): Observable<{ deleted: boolean }> {
    return this.http.delete<{ deleted: boolean }>('/api/market/indices/fmp-key');
  }

  /** Ranks a preset universe, or up to 25 symbols, for a strategy. */
  getLeaderboard(universe: string, strategy: string, symbols: string[] = [], ignorePreferences = false): Observable<LeaderboardResponse> {
    const params: Record<string, string> = { strategy, explain: '1' };
    if (ignorePreferences) params['preferences'] = 'off';
    if (symbols.length) params['symbols'] = symbols.join(',');
    else params['universe'] = universe;
    return this.http.get<LeaderboardResponse>('/api/market/leaderboard', { params });
  }

  /** The signed-in user's saved leaderboard objective and constraints. */
  getRankingPreferences(): Observable<RankingPreferencesResponse> {
    return this.http.get<RankingPreferencesResponse>('/api/market/leaderboard/preferences');
  }

  saveRankingPreferences(prefs: RankingPreferences): Observable<RankingPreferencesResponse> {
    return this.http.put<RankingPreferencesResponse>('/api/market/leaderboard/preferences', prefs);
  }

  /** Replays a leaderboard strategy over past prices; slow on a cold cache. */
  backtest(params: Record<string, string>): Observable<BacktestResult> {
    return this.http.get<BacktestResult>('/api/market/backtest', { params });
  }

  /** One strategy over several universes and periods; each cell is an ordinary backtest. */
  backtestBatch(params: Record<string, string>, progressId?: string): Observable<BatchResult> {
    return this.http.get<BatchResult>('/api/market/backtest/batch', { params: progressId ? { ...params, progress_id: progressId } : params });
  }

  /** How many backtests of a named batch have finished; 404 until the run starts. */
  backtestProgress(id: string): Observable<unknown> {
    return this.http.get<unknown>('/api/market/backtest/progress', { params: { id } });
  }

  /** One setting at a time (trading cost, holdings, rebalance) around a base backtest. */
  backtestMonteCarlo(params: Record<string, string>): Observable<MonteCarloResult> {
    return this.http.get<MonteCarloResult>('/api/market/backtest/montecarlo', { params });
  }

  backtestSensitivity(params: Record<string, string>): Observable<SensitivityResult> {
    return this.http.get<SensitivityResult>('/api/market/backtest/sensitivity', { params });
  }

  /** The user's saved backtests (without their results). */
  listBacktestRuns(): Observable<{ runs: SavedBacktest[]; max: number }> {
    return this.http.get<{ runs: SavedBacktest[]; max: number }>('/api/market/backtest/runs');
  }
  /** Saves a run; the server runs the query itself and stores its own answer. */
  saveBacktestRun(name: string, kind: SavedBacktest['kind'], params: Record<string, string>): Observable<{ id: number }> {
    return this.http.post<{ id: number }>('/api/market/backtest/runs', { name, kind, query: new URLSearchParams(params).toString() });
  }
  getBacktestRun(id: number): Observable<SavedBacktest & { result: unknown }> {
    return this.http.get<SavedBacktest & { result: unknown }>(`/api/market/backtest/runs/${id}`);
  }
  /** A model-written summary of a saved run, checked by the server against the stored figures. */
  summarizeBacktestRun(id: number): Observable<BacktestSummary> {
    return this.http.post<BacktestSummary>(`/api/market/backtest/runs/${id}/summary`, {});
  }
  deleteBacktestRun(id: number): Observable<{ deleted: boolean }> {
    return this.http.delete<{ deleted: boolean }>(`/api/market/backtest/runs/${id}`);
  }

  /** The investor characters an agent can answer as. */
  getPersonas(): Observable<PersonaCatalog> { return this.http.get<PersonaCatalog>('/api/ai/personas'); }
  /** Model profiles an administrator prepared (enabled ones only). */
  getProfiles(): Observable<{ profiles: ModelProfile[] }> { return this.http.get<{ profiles: ModelProfile[] }>('/api/ai/profiles'); }
  getAdminProfiles(): Observable<{ profiles: ModelProfile[]; max: number }> { return this.http.get<{ profiles: ModelProfile[]; max: number }>('/api/admin/ai/profiles'); }
  saveAdminProfile(p: Partial<ModelProfile>): Observable<{ id: number }> {
    return p.id ? this.http.put<{ id: number }>(`/api/admin/ai/profiles/${p.id}`, p) : this.http.post<{ id: number }>('/api/admin/ai/profiles', p);
  }
  deleteAdminProfile(id: number): Observable<unknown> { return this.http.delete(`/api/admin/ai/profiles/${id}`); }
  /** The user's saved agent teams. */
  getSavedTeams(): Observable<{ teams: SavedTeam[]; max: number }> { return this.http.get<{ teams: SavedTeam[]; max: number }>('/api/ai/teams'); }
  saveTeam(t: { id?: number; name: string; chair: boolean; members: SavedTeamMember[]; max_tokens?: number | null; max_cost?: number | null; max_tool_calls?: number | null }): Observable<{ id: number }> {
    return t.id ? this.http.put<{ id: number }>(`/api/ai/teams/${t.id}`, t) : this.http.post<{ id: number }>('/api/ai/teams', t);
  }
  deleteTeam(id: number): Observable<unknown> { return this.http.delete(`/api/ai/teams/${id}`); }
  /** Built-in Fibonacci strategies and the user's saved ones. */
  getStrategies(): Observable<StrategyCatalog> { return this.http.get<StrategyCatalog>('/api/strategies'); }
  createStrategy(def: StrategyDef): Observable<SavedStrategy> { return this.http.post<SavedStrategy>('/api/strategies', def); }
  updateStrategy(id: number, def: StrategyDef): Observable<SavedStrategy> { return this.http.put<SavedStrategy>(`/api/strategies/${id}`, def); }
  deleteStrategy(id: number): Observable<void> { return this.http.delete<void>(`/api/strategies/${id}`); }
  /** Replays a strategy over one instrument's daily bars. Nothing is ordered. */
  runStrategy(req: StrategyRunRequest): Observable<StrategyRunResult> { return this.http.post<StrategyRunResult>('/api/strategies/backtest', req); }
  /** Chooses numbers on one window and tests them on the next. Research only; nothing is ordered. */
  runWalkForward(req: FitRequest): Observable<FitResult> { return this.http.post<FitResult>('/api/strategies/walkforward', req); }

  /** Resamples the closed trades of a strategy replay with a seed. Research only; nothing is ordered. */
  strategyMonteCarlo(req: StrategyMonteCarloRequest): Observable<StrategyMonteCarloResult> { return this.http.post<StrategyMonteCarloResult>('/api/strategies/montecarlo', req); }

  /** The signed-in user's saved wealth plan, or nulls when none is saved. */
  getPlan(): Observable<PlanResponse> {
    return this.http.get<PlanResponse>('/api/plan');
  }

  savePlan(profile: PlanProfile): Observable<PlanResponse> {
    return this.http.put<PlanResponse>('/api/plan/profile', profile);
  }

  /** A plan for an unsaved profile; nothing is stored. */
  previewPlan(profile: PlanProfile): Observable<PlanResponse> {
    return this.http.post<PlanResponse>('/api/plan/preview', profile);
  }

  /** Concentration, risk and drift of the paper portfolio. */
  getPortfolioAnalytics(): Observable<PortfolioAnalyticsResponse> {
    return this.http.get<PortfolioAnalyticsResponse>('/api/portfolio/analytics');
  }

  /** How many units to trade so a stop-out loses riskPct of equity. Places nothing. */
  sizePosition(symbol: string, side: 'buy' | 'sell', riskPct: number): Observable<SizeResponse> {
    return this.http.post<SizeResponse>('/api/trading/size', { symbol, side, risk_pct: riskPct });
  }

  /** Dividends, splits and earnings dates between two ISO dates (window up to 400 days). */
  getEvents(symbol: string, from: string, to: string, provider = ''): Observable<EventsResponse> {
    const params: Record<string, string> = { symbol, from, to };
    if (provider) params['provider'] = provider;
    return this.http.get<EventsResponse>('/api/events', { params });
  }

  /** Headlines sorted into themes, with the sectors and commodities each theme touches. */
  getWorld(refresh = false): Observable<WorldView> {
    return this.http.get<WorldView>('/api/world', { params: refresh ? { refresh: '1' } : {} });
  }

  /** How today's world themes reach one company, from its sector and its own headlines. */
  getCompanyWorld(symbol: string, sector = ''): Observable<CompanyWorld> {
    const params: Record<string, string> = { symbol };
    if (sector) params['sector'] = sector;
    return this.http.get<CompanyWorld>('/api/world/company', { params });
  }

  /** Phase, macro regime, paper portfolio and three things worth a look; no model runs. */
  getToday(): Observable<TodayBriefing> {
    return this.http.get<TodayBriefing>('/api/today');
  }

  getStats(symbol: string): Observable<MarketStatsJSON> {
    return this.http.get<MarketStatsJSON>('/api/stats', { params: { symbol } });
  }

  /** Order book, optionally filtered by symbol and/or lifecycle status. */
  getOrders(symbol?: string, status?: string): Observable<Order[]> {
    let params: Record<string, string> = {};
    if (symbol) params['symbol'] = symbol;
    if (status) params['status'] = status;
    return this.http.get<Order[]>('/api/orders', { params });
  }

  /**
   * Submits a ticket. The engine validates it (position, buying power,
   * prices) and executes marketable orders immediately, so a 400 response
   * carries a human readable `error` field.
   */
  submitOrder(order: OrderRequest): Observable<OrderResponse> {
    return this.http.post<OrderResponse>('/api/orders', order);
  }

  /** Moves an order to a terminal status (cancel / reject). */
  cancelOrder(id: number, reason?: string): Observable<{ status: string; order: Order }> {
    return this.http.patch<{ status: string; order: Order }>('/api/orders', {
      id,
      status: 'CANCELLED',
      reason: reason ?? 'cancelled by user',
    });
  }

  /** Net positions plus the account summary. */
  getPortfolio(): Observable<PortfolioResponse> {
    return this.http.get<PortfolioResponse>('/api/positions');
  }

  getForum(symbol?: string, limit = 50): Observable<ForumPost[]> {
    return this.http.get<ForumPost[]>('/api/forum', {
      params: symbol ? { symbol, limit } : { limit },
    });
  }

  createForumPost(post: {
    author: string;
    title: string;
    content: string;
    symbol: string;
  }): Observable<{ status: string; post_id: number }> {
    return this.http.post<{ status: string; post_id: number }>('/api/forum', post);
  }

  votePost(id: number, delta: number): Observable<{ status: string }> {
    return this.http.post<{ status: string }>('/api/forum/vote', { id, delta });
  }

  search(query: string, provider: string): Observable<SymbolInfo[]> {
    return this.http
      .get<SymbolInfo[]>('/api/search', { params: { query, provider } })
      .pipe(map((r) => r ?? []));
  }

  clearHistory(): Observable<{ status: string; message: string }> {
    return this.http.delete<{ status: string; message: string }>('/api/history');
  }
}

interface MarketStatsJSON {
  symbol: string;
  min_price: number;
  max_price: number;
  avg_price: number;
  latest_price: number;
  record_count: number;
}

/** Extracts a user-friendly message from an unknown HTTP error. */
export function describeHttpError(err: unknown): string {
  if (err instanceof HttpErrorResponse) {
    const detail =
      (err.error && (err.error.error || err.error.message)) || null;
    if (err.status === 0) {
      return 'Cannot reach the trading engine. Is the backend running?';
    }
    if (detail) {
      return `${err.status} — ${detail}`;
    }
    return `Request failed (${err.status} ${err.statusText || 'error'})`;
  }
  if (err instanceof Error) return err.message;
  return 'Unexpected error';
}

/** Wraps observables so failures surface as a friendly message. */
export function safeCall<T>(source: Observable<T>): Observable<T | null> {
  return source.pipe(catchError((e) => { console.error(e); return of(null as T | null); }));
}

export type { MarketData };
