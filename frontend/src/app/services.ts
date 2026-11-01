import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, catchError, map, of } from 'rxjs';
import {
  ForumPost,
  FinancialsResponse,
  HistorySeries,
  BacktestParams,
  BacktestResult,
  LeaderboardResponse,
  MarketData,
  Order,
  OrderRequest,
  OrderResponse,
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

  /** Ranks a preset universe, or up to 25 symbols, for a strategy. */
  getLeaderboard(universe: string, strategy: string, symbols: string[] = []): Observable<LeaderboardResponse> {
    const params: Record<string, string> = { strategy };
    if (symbols.length) params['symbols'] = symbols.join(',');
    else params['universe'] = universe;
    return this.http.get<LeaderboardResponse>('/api/market/leaderboard', { params });
  }

  /** Replays a leaderboard strategy over past prices; slow on a cold cache. */
  backtest(p: BacktestParams): Observable<BacktestResult> {
    const params: Record<string, string> = { strategy: p.strategy, top: String(p.top), rebalance: p.rebalance, years: String(p.years) };
    if (p.symbols?.length) params['symbols'] = p.symbols.join(',');
    else params['universe'] = p.universe;
    return this.http.get<BacktestResult>('/api/market/backtest', { params });
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
