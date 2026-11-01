export interface MarketData {
  assetDataProvider?: string;
  type?: string;
  symbol?: string;
  name?: string;
  price?: number;
  currency?: string;
  isin?: string;
  description?: string;
  navDate?: string;
  rating?: number;
  fees?: { product?: number; management?: number };
  risk?: { value?: number; text?: string };
  performance?: {
    '1d'?: number; '1m'?: number; '3m'?: number; '6m'?: number;
    '1y'?: number; 'thisYear'?: number; '3y'?: number; '5y'?: number;
  };
  esg?: {
    score?: number; rating?: number; lowCarbon?: boolean;
    environmentalScore?: number; socialScore?: number; governanceScore?: number;
    carbonRiskScore?: number; category?: string; svanen?: boolean;
  };
  holdings?: Array<[string, number] | StockHolding>;
  extended_info?: {
    indexFund?: boolean; sharpeRatio?: number; standardDeviation?: number;
    capital?: number; primaryBenchmark?: string; recommendedHoldingPeriod?: string;
  };
  quote?: {
    buy?: number; sell?: number; last?: number; highest?: number; lowest?: number;
    change?: number; changePercent?: number; spread?: number;
  };
  keyIndicators?: {
    peRatio?: number; psRatio?: number; pbRatio?: number; eps?: number;
    directYield?: number; marketCap?: number;
  };
  listing?: {
    shortName?: string; tickerSymbol?: string; marketPlaceName?: string; marketListName?: string;
  };
  full_data?: unknown;
}

export interface StockHolding {
  name?: string;
  symbol?: string;
  weight?: number;
  percent?: number;
  country?: string;
  sector?: string;
}

export interface SymbolInfo {
  symbol: string;
  name: string;
  type: string;
  exchange: string;
}

export interface TradingRecord {
  timestamp: number;
  symbol: string;
  provider?: string;
  price: number;
  stop_loss: number;
  fast_ema: number;
  slow_ema: number;
  currency: string;
  status: string;
  marketData?: MarketData;
}

export type OrderSide = 'BUY' | 'SELL';
export type OrderType = 'MARKET' | 'LIMIT' | 'STOP' | 'STOP_LIMIT';
export type OrderTif = 'GTC' | 'DAY' | 'IOC';
export type OrderStatus = 'OPEN' | 'FILLED' | 'CANCELLED' | 'REJECTED';

/** One simulated broker execution, including its lifecycle fields. */
export interface Order {
  id: number;
  symbol: string;
  side: OrderSide;
  type: OrderType;
  tif: OrderTif;
  quantity: number;
  /** Reference price captured when the order was submitted. */
  price: number;
  limit_price: number;
  stop_price: number;
  fill_price: number;
  fee: number;
  timestamp: number;
  filled_at: number;
  status: OrderStatus;
  reason: string;
}

/** Net holding of one symbol after every execution is netted out. */
export interface Position {
  symbol: string;
  quantity: number;
  avg_entry: number;
  cost_basis: number;
  last_price: number;
  market_value: number;
  unrealized_pnl: number;
  realized_pnl: number;
  open_orders: number;
  first_fill_at: number;
  last_fill_at: number;
}

/** Account level view of the book. */
export interface PortfolioSummary {
  starting_cash: number;
  cash: number;
  buying_power: number;
  equity: number;
  market_value: number;
  unrealized_pnl: number;
  realized_pnl: number;
  commission_paid: number;
  position_count: number;
  long_count: number;
  short_count: number;
  open_order_count: number;
  trade_count: number;
  winning_symbols: number;
  losing_symbols: number;
  allow_short: boolean;
  commission_pct: number;
  commission_fixed: number;
}

export interface PortfolioResponse {
  positions: Position[];
  summary: PortfolioSummary;
}

/** Payload sent to POST /api/orders. */
export interface OrderRequest {
  symbol: string;
  side: OrderSide;
  type: OrderType;
  tif: OrderTif;
  quantity: number;
  limit_price?: number;
  stop_price?: number;
}

export interface OrderResponse {
  status: string;
  order_id: number;
  order: Order;
  summary?: PortfolioSummary;
}

export interface ForumPost {
  id: number;
  author: string;
  title: string;
  content: string;
  symbol?: string;
  timestamp: number;
  upvotes: number;
}

export interface MarketStats {
  symbol: string;
  min_price: number;
  max_price: number;
  avg_price: number;
  latest_price: number;
  record_count: number;
}

/** Provider ID contributed by an installed plugin, e.g. 'YAHOO'. */
export type ProviderId = string;

const PROVIDER_ID = /^[A-Z][A-Z0-9_]{1,31}$/;

/** True for a well-formed provider ID (whether or not it is installed). */
export function isProviderId(value: unknown): value is ProviderId {
  return typeof value === 'string' && PROVIDER_ID.test(value);
}

/** An installed provider, as served by GET /api/providers. */
export interface ProviderCapability {
  id: ProviderId;
  label: string;
  plugin_id: string;
  plugin_version: string;
  official_api: boolean;
  search: boolean;
  ohlc: boolean;
  fundamentals: boolean;
  capabilities: string[];
  quote_kind: string;
  requires_key: boolean;
  experimental: boolean;
  /** False when a required setting such as an API key is missing. */
  enabled: boolean;
  min_poll_seconds: number;
  cache_ttl_seconds: number;
  notes: string;
}

/** A single OHLC bar from the provider. */
export interface HistoryPoint {
  t: number; // unix seconds
  o: number;
  h: number;
  l: number;
  c: number;
  v: number;
}

/** Provider OHLC series served by GET /api/history. */
export interface HistorySeries {
  symbol: string;
  provider: string;
  range: string;
  interval: string;
  currency: string;
  points: HistoryPoint[];
  emaConfig: { fastPeriod: number; slowPeriod: number };
  /** Trailing-stop level per point (aligned with points), replayed by the backend. */
  stops?: number[];
}

export const HISTORY_RANGES = ['1D', '5D', '1M', '3M', '1Y', '3Y', '5Y', '10Y', 'MAX'] as const;
export type HistoryRange = (typeof HISTORY_RANGES)[number];

/** One reported company quarter from GET /api/financials. */
export interface QuarterlyFinancial {
  period: string; // e.g. "CY2026 Q3" (Yahoo) or "Q2 FY2027" (Finnhub)
  period_basis?: "calendar" | "fiscal";
  period_end: string; // "2026-06-30"
  revenue?: number;
  gross_profit?: number;
  operating_income?: number;
  net_income?: number;
  eps?: number;
  free_cash_flow?: number;
  currency?: string;
  form?: string; // SEC form ("10-Q") when known
}

/** Quarterly statements response envelope. */
export interface FinancialsResponse {
  from_year?: number;
  to_year?: number;
  symbol: string;
  provider: string;
  source: string;
  reports: QuarterlyFinancial[];
  note?: string;
}

// AI Chat types
export interface Message {
  role: 'user' | 'assistant' | 'tool' | 'system';
  content?: string;
  name?: string;
  tool_call_id?: string;
  content_json?: any;
  tool_calls?: any[];
}

export interface ChatRequest {
  model: string;
  messages: Message[];
  tools?: any[];
  tool_choice?: string;
  temperature?: number;
  stream?: boolean;
}

export interface ToolCall {
  index: number;
  id: string;
  type: 'function';
  function: {
    name: string;
    arguments: string;
  };
}

/** One ranked company from GET /api/market/leaderboard (see leaderboard.go). */
export interface LeaderboardRow {
  rank: number;
  symbol: string;
  score: number;
  grade: string;
  coverage_pct: number;
  value?: number; quality?: number; growth?: number; momentum?: number; timing?: number; risk?: number;
  price?: number; currency?: string; as_of?: string;
  pe_ttm?: number; peg?: number; earnings_yield_pct?: number;
  revenue_growth_pct?: number; eps_growth_pct?: number; net_margin_pct?: number; fcf_margin_pct?: number;
  change_1m_pct?: number; change_3m_pct?: number; change_1y_pct?: number; momentum_12_1_pct?: number;
  from_high_pct?: number; rsi14?: number; above_sma200?: boolean; volatility_pct?: number; max_drawdown_pct?: number;
  ttm_through?: string;
  reasons: string;
  flags: string;
}

export interface LeaderboardOption { id: string; label: string; description: string }

export interface LeaderboardResponse {
  universe: string;
  strategy: string;
  provider: string;
  as_of: string;
  generated_at: string;
  rows: LeaderboardRow[];
  unranked: { symbol: string; error: string }[];
  universes: LeaderboardOption[];
  strategies: LeaderboardOption[];
}
