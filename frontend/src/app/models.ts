import type { BudgetSummary } from './components/team-budget';
import type { Provenance } from './core/provenance';
import type { AnswerClaim, AnswerFinding, AnswerSource, ClaimStats } from './core/answer-sources';
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
  /** When the server polled the provider (Unix seconds). */
  poll_time?: number;
  /** The provider's own time of the quote (Unix seconds); absent when it did not say. */
  quote_time?: number;
  /** How far behind the market the provider says the quote is; absent when unknown. */
  delay_seconds?: number;
  market_state?: 'open' | 'closed' | 'unknown';
  /** `provider`, or `regular_hours` (computed from the exchange's weekday hours; holidays unknown). */
  market_basis?: string;
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
  /** Set by the server for posts that come from an installed provider plugin (read-only). */
  source?: 'provider';
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
  /** Where the data came from (internal/provenance.Fact), or absent when the server recorded none. */
  provenance?: Provenance;
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

/** The latest price of an instrument, as GET /api/quote serves it. */
export interface QuoteSnapshot {
  symbol: string;
  currency: string;
  price: number;
  as_of_unix: number;
  age_seconds: number;
  is_realtime: boolean;
  stale: boolean;
  /** open, closed or unknown. */
  market_session: string;
  quote_type: string;
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
  /** Where the data came from (internal/provenance.Fact), or absent when the server recorded none. */
  provenance?: Provenance;
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
  /** How far the score can be trusted, and why. */
  confidence?: 'high' | 'medium' | 'low';
  confidence_note?: string;
  /** Competing explanations, separated by semicolons. */
  alternatives?: string;
  fundamentals_age_days?: number;
  /** Present when the request asked to explain the score. */
  breakdown?: LeaderboardBreakdown;
  /** Why the company is on the list, where its numbers come from, and which listing it is: three separate answers. */
  discovery?: { why: string; source: string; kind: string; url: string | null; as_of: string | null; list_id?: string; list_version?: string | null; how?: string;
    lists?: { id: string; label: string; kind: string; source: string; as_of: string | null; rule: string }[] };
  /** What is recorded about the security beyond its ticker; an ISIN nobody verified is "not recorded". */
  identifier?: { symbol: string; isin: string | null; isin_status: 'recorded' | 'not recorded'; name: string | null };
  /** The region the row was compared within, and what that comparison rests on (global scope). */
  region?: string;
  compared_with?: string;
  /** Set when a second provider filled this row or disagreed with the primary. */
  data_provider?: string;
  provider_note?: string;
  provider_conflict?: { kind: string; detail: string; differs_pct?: number };
  metrics_source?: { provider: string; price_as_of?: string; fundamentals_through?: string; record_url?: string | null; fact?: Provenance };
  listing?: { exchange: string; mic?: string; country?: string; known: boolean };
}

export interface ScopeCoverage {
  region: string; label: string; listed: number; ranked: number; price_newest: string; price_oldest: string;
  countries: string[]; exchanges: string[]; currencies: string[]; providers: string[];
}
export interface ScopeComparability {
  mode: 'region' | 'all'; mode_label: string; currencies: Record<string, number>; mixed_currencies: boolean;
  fx: { applied: boolean; source: string | null; note: string }; accounting: string; timezones: string; warnings: string[];
}
/** The scope a ranking was computed for: filters, what it is compared with, and per-region coverage. */
export interface LeaderboardScope {
  filters: { active: boolean; description: string }; compare: 'region' | 'all';
  coverage: ScopeCoverage[]; comparability: ScopeComparability;
}
/** What the user asked to narrow the ranking by; codes are upper case, regions are ids. */
export interface ScopeRequest { regions?: string[]; countries?: string[]; exchanges?: string[]; currencies?: string[]; compare?: 'region' | 'all' }

export interface UniverseSource { kind: 'curated' | 'disclosure' | 'custom' | string; name: string; url: string | null; as_of: string | null; rule: string; limitations: string }
export interface LeaderboardSelection {
  initial: number; ranked: number; excluded: { symbol: string; reason: string }[];
  steps: { step: string; count: number; detail?: string }[];
  countries: Record<string, number>; exchanges: Record<string, number>;
}

export interface LeaderboardMetricDetail {
  field: string; label: string; value?: number | boolean | string; percentile: number; weight: number; higher_is_better: boolean;
}
export interface LeaderboardFactorDetail {
  factor: string; weight: number; missing: boolean; score: number; contribution: number;
  capped_from?: number; cap_reason?: string; metrics?: LeaderboardMetricDetail[];
}
export interface LeaderboardBreakdown { factors: LeaderboardFactorDetail[]; missing_factor_score: number; total: number }

/** How a ranking is computed, as the server describes it. */
export interface LeaderboardMethodMetric { field: string; label: string; note: string; higher_is_better: boolean; weight: number }
export interface LeaderboardMethodology {
  version: string; hash: string; strategy?: string;
  weights?: Record<string, number>;
  factors?: string[];
  factor_info?: Record<string, { label: string; question: string }>;
  metrics?: Record<string, LeaderboardMethodMetric[]>;
  caps?: string[];
  missing_factor_score?: number;
  scale?: string;
  missing_data?: string;
}

/** What a ranking was computed with, so it can be reproduced and judged. */
export interface LeaderboardProvenance {
  methodology: LeaderboardMethodology;
  universe_id: string;
  universe_hash: string;
  inputs_hash: string;
  price_dates: { newest: string; oldest: string };
  disclaimer: string;
  /** The ranking, the list and the data behind it in the shared provenance format. */
  fact?: Provenance;
  universe_fact?: Provenance;
  data_age?: LeaderboardDataAge;
  bias?: LeaderboardBias;
}

/** How old the inputs are and whether they differ in age; `statements` is ready to show. */
export interface LeaderboardDataAge {
  mixed: boolean;
  price_newest: string; price_oldest: string; price_span_days: number;
  older_than_newest: number; stale_price: number; stale_fundamentals: number; no_fundamentals: number;
  newest_price_age_days?: number;
  list_as_of?: string | null; list_age_days?: number; list_stale?: boolean;
  statements: string[];
}

/** Selection and survivorship effects measured from the data already fetched. */
export interface LeaderboardBias {
  point_in_time: boolean;
  measures: Record<string, number | string>;
  statements: string[];
}

/** A user's own objective and constraints for the leaderboard. */
export interface RankingPreferences {
  objective: string;
  max_volatility_pct?: number;
  exclude_unprofitable?: boolean;
  require_fundamentals?: boolean;
  exclude_symbols?: string[];
}
export interface RankingPreferencesResponse {
  preferences: RankingPreferences;
  objectives: Record<string, string>;
  summary: string[];
  stored: boolean;
}

/** One index of GET /api/market/indices. An unavailable index has no constituents and says why. */
export interface IndexInfo {
  id: string; name: string; region: string; country: string; currency: string; provider: string;
  nominal_count: number; benchmark_symbol: string;
  methodology: { summary: string; url: string | null };
  constituent_source: { name: string; url: string | null; retrieved_at: string | null; license_verified: boolean };
  as_of: string | null; status: 'available' | 'unavailable'; unavailable_reason?: string;
  licensing: string; exclusions: string; share_class_policy: string;
  history: { point_in_time: boolean; note: string }; benchmark_note: string;
  count: number; warnings: string[]; age_days: number | null; stale: boolean; available: boolean;
  /** How the signed-in user can supply a list: a CSV file, or their own Financial Modeling Prep key. */
  sources?: ('csv' | 'fmp')[];
  /** What the user supplied; null until they import or fetch a list. */
  source?: IndexUserSource | null;
  /** The automated checks of the supplied list; absent until a list exists. */
  verification?: IndexVerification;
}

/** Outcome of the automated checks of a list. "verified" is not an endorsement by the index provider. */
export interface IndexVerification {
  level: 'verified' | 'approximate'; summary: string; identity_share: number;
  checks: { id: string; ok: boolean; detail: string }[];
}

/** A way to supply a list, with the licence restriction that applies to it. */
export interface IndexAdapter { id: string; name: string; kind: 'file' | 'api'; needs_key: boolean; documented: boolean; restrictions: string; indexes?: string[] }

export interface IndexBrief {
  id: string; name: string; currency: string; count: number; as_of: string; age_days: number | null; point_in_time: boolean;
  history_from?: string; benchmark_symbol: string; benchmark_total_return: boolean; verification?: IndexVerification; with_isin: number;
}

export interface IndexPair { a: string; b: string; shared: number; jaccard: number; symbols: string[] }

/** GET /api/market/indices/compare */
export interface IndexComparison {
  indices: IndexBrief[]; pairs: IndexPair[]; union_count: number; unavailable: string[]; currencies: string[]; warnings: string[]; matching: string;
}

/** Provenance of a list the user supplied (nothing ships with the app). */
export interface IndexUserSource {
  provider: 'csv' | 'fmp'; source_name: string; as_of: string; imported_at: string; count: number;
  license_terms: string; license_confirmed: boolean; point_in_time: boolean; history_from?: string; change_records: number;
  unverified?: boolean; warnings: string[];
}

export interface IndicesResponse { indices: IndexInfo[]; generated_at: string; notice: string; fmp_key_saved?: boolean; adapters?: IndexAdapter[] }

/** A refused import or fetch: the reason and one line per problem. */
export interface IndexSourceRefusal { error: string; problems?: string[] }

/** Which constituents a backtest on an index used at each rebalance. */
export interface BacktestMembership {
  mode: 'point_in_time' | 'point_in_time_with_fallback' | 'current_members'; provider: string; source_name: string; as_of: string; history_from?: string;
  point_in_time_rebalances: number; fallback_rebalances: number; note: string;
}

export interface LeaderboardOption { id: string; label: string; description: string; benchmark?: string; source?: UniverseSource; region?: string; countries?: string[]; components?: string[] }

export interface LeaderboardResponse {
  universe: string;
  strategy: string;
  provider: string;
  as_of: string;
  generated_at: string;
  rows: LeaderboardRow[];
  unranked: { symbol: string; error: string }[];
  /** The list that was ranked, with its source; absent from older servers. */
  universe_info?: LeaderboardOption;
  selection?: LeaderboardSelection;
  scope?: LeaderboardScope;
  /** The caller's saved preferences and whether they shaped this ranking. */
  preferences?: { applied: boolean; values: RankingPreferences; summary: string[]; effective_weights: Record<string, number> };
  universes: LeaderboardOption[];
  strategies: LeaderboardOption[];
  provenance?: LeaderboardProvenance;
}

/** Return and risk figures of a backtest (see internal/backtest). */
export interface BacktestMetrics {
  cagr_pct: number;
  volatility_pct: number;
  sharpe: number;
  sortino: number;
  max_drawdown_pct: number;
  calmar: number;
  hit_rate_pct: number;
  avg_turnover_pct: number;
  benchmark_cagr_pct?: number;
  excess_cagr_pct?: number;
}

/** One day of the replayed curves; both start at 100. */
export interface BacktestPoint { date: string; equity: number; benchmark?: number }

export interface BacktestRebalance { date: string; holdings: string[]; turnover_pct: number; fundamentals_coverage_pct: number; /** Share of the wanted order value the liquidity cap left unfilled. */ unfilled_pct?: number }

/** The answer of GET /api/market/backtest. */
export interface BacktestResult {
  /** Where the data came from (internal/provenance.Fact), or absent when the server recorded none. */
  provenance?: Provenance;
  universe: string;
  strategy: string;
  benchmark?: string;
  provider: string;
  symbols: string[];
  generated_at: string;
  /** Dates asked for and the provider's data bounds, UTC YYYY-MM-DD. */
  from?: string;
  to?: string;
  data_start?: string;
  data_end?: string;
  /** Set when the replay does not cover the period that was asked for. */
  notice?: string;
  metrics: BacktestMetrics;
  curve: BacktestPoint[];
  rebalances: BacktestRebalance[];
  warnings: string[];
  /** How fills were assumed, from the settings the run used; entries with modeled=false are not simulated at all. */
  execution_assumptions?: BacktestAssumption[];
  /** What the order-size cap and impact charge did; absent unless a portfolio size was given. */
  liquidity?: BacktestLiquidity;
  /** What the benchmark is and whether its currency matches the symbols; absent without a benchmark. */
  benchmark_info?: BacktestBenchmarkInfo;
  /** Present when the universe is an index the user supplied. */
  membership?: BacktestMembership;
  /** Quote currencies the provider reported for the symbols (not converted). */
  currencies?: string[];
  /** Consecutive parts of the period, each measured on its own; absent when the period is too short. */
  segments?: BacktestSegment[];
  segment_summary?: string;
  segment_note?: string;
  /** Buys and sells of single holdings, and the return with and without the best rebalance periods. */
  trades?: number;
  concentration?: BacktestConcentration;
  /** Named past windows the replay fully covers, the ones it does not, and the hindsight caveat. */
  stress?: BacktestStress[];
  stress_not_covered?: string[];
  stress_note?: string;
  /** Replayed days grouped by the benchmark's own trend and volatility; labels describe the past only. */
  regimes?: BacktestRegime[];
  regime_unlabeled_days?: number;
  regime_note?: string;
  walk_forward_note?: string;
}
export interface BacktestLiquidity {
  orders: number; partial_orders: number; unknown_volume_orders: number; max_participation_pct: number;
  unfilled_pct: number; impact_drag_bps: number; spread_drag_bps: number;
}
export interface BacktestAssumption { name: string; value: string; modeled: boolean }
export interface BacktestBenchmarkInfo {
  symbol: string; source: 'universe_default' | 'chosen'; basis: string; currency?: string; first_bar?: string; last_bar?: string;
  currency_comparable: boolean; note?: string;
}
export interface BacktestConcentration { periods: number; dropped: number; total_return_pct: number; without_best_return_pct: number }
export interface BacktestStress {
  name: string; from: string; to: string; days: number; return_pct: number; max_drawdown_pct: number;
  benchmark_return_pct?: number; excess_pct?: number;
}
export interface BacktestRegime {
  id: string; name: string; days: number; share_pct: number;
  cagr_pct?: number; benchmark_cagr_pct?: number; excess_cagr_pct?: number;
}
export interface BacktestSegment {
  from: string; to: string; days: number; cagr_pct: number; volatility_pct: number; max_drawdown_pct: number; sharpe: number;
  benchmark_cagr_pct?: number; excess_cagr_pct?: number;
}

/** One universe and period of GET /api/market/backtest/batch. A failed cell has an error and no metrics. */
export interface BatchCell {
  universe: string; period: string; from?: string; to?: string; data_start?: string; data_end?: string; notice?: string;
  rebalances?: number; metrics?: BacktestMetrics; warnings?: string[]; error?: string;
}
export interface BatchSpread { min: number; median: number; max: number }
export interface BatchSummary {
  cells: number; completed: number; failed: number;
  cagr_pct?: BatchSpread; excess_cagr_pct?: BatchSpread; max_drawdown_pct?: BatchSpread; sharpe?: BatchSpread;
  beat_benchmark: number; with_benchmark: number; negative_return: number; weakest?: string; strongest?: string; limitations: string[];
}
export interface BatchResult {
  strategy: string; generated_at: string; universes: string[]; periods: string[]; cells: BatchCell[]; summary: BatchSummary;
}

/** One setting changed in GET /api/market/backtest/sensitivity. */
export interface SensitivityVariant { parameter: string; value: string; base?: boolean; metrics?: BacktestMetrics; error?: string }
export interface SensitivityResult {
  universe: string; strategy: string; period: string; generated_at: string; base: Record<string, string>;
  variants: SensitivityVariant[];
  summary: { completed: number; failed: number; cagr_pct?: BatchSpread; excess_cagr_pct?: BatchSpread; positive_excess: number; with_benchmark: number; cost_break_even_bps?: number; notes: string[] };
}

/** Percentiles of one simulated figure, in percent of the starting value. */
export interface MonteCarloDist { p5: number; p25: number; p50: number; p75: number; p95: number }
export interface MonteCarloSimulation {
  model_version: string;
  config: { method: string; runs: number; horizon_days: number; seed: number; block_days?: number; drawdown_limit_pct?: number; extra_cost_bps_per_year?: number; volatility_scale: number };
  observed_days: number;
  observed: { days: number; total_return_pct: number; max_drawdown_pct: number };
  terminal_return_pct: MonteCarloDist; max_drawdown_pct: MonteCarloDist;
  prob_loss_pct: number; prob_breach_pct?: number; prob_ruin_pct: number;
  assumptions: string[]; limitations: string[];
}
/** GET /api/market/backtest/montecarlo: simulated outcomes next to the unchanged observed metrics. */
export interface MonteCarloResult {
  universe: string; strategy: string; benchmark?: string; provider: string; generated_at: string; backtest_generated_at: string;
  from: string; to?: string; data_start?: string; data_end?: string;
  backtest_metrics: BacktestMetrics; backtest_warnings: string[];
  simulation: MonteCarloSimulation;
  slippage_bps?: number; slippage_drag_bps_per_year?: number; notes: string[];
}

/** A saved backtest as listed; GET /api/market/backtest/runs/{id} adds its result. */
export interface BacktestSummary { run_id: number; verified: boolean; summary?: string; facts: string; notes: string[]; reason?: string; attempts: number; citations: string }
export interface SavedBacktest { id: number; name: string; kind: 'single' | 'batch' | 'sensitivity' | 'montecarlo'; query: string; created_at: string; provenance?: Provenance }

export interface BacktestParams {
  universe: string;
  strategy: string;
  symbols?: string[];
  top: number;
  rebalance: 'monthly' | 'quarterly';
  /** Slippage in basis points per side; absent or 0 means none is modeled. */
  slippage?: number;
  /** Trading days between the ranking and the fill (0 to 5); absent or 0 fills at the same close. */
  delay?: number;
  /** Quoted bid-ask spread in basis points; half is paid per side. */
  spread?: number;
  /** Portfolio size in the price currency. When set, orders are sized against traded volume; absent or 0 means liquidity is not modeled. */
  notional?: number;
  /** Market impact coefficient in basis points; used only with a portfolio size. */
  impact?: number;
  /** A preset length, or 0 when from/to are used. */
  years: 3 | 5 | 10 | 0;
  from?: string;
  to?: string;
}

/** Wealth plan inputs (see internal/planner). */
export interface PlanProfile {
  monthly_expenses: number;
  emergency_fund?: number;
  high_interest_debt?: number;
  invested?: number;
  use_paper_portfolio?: boolean;
  monthly_contribution?: number;
  age?: number;
  retirement_age?: number;
  goal_amount?: number;
  risk_profile?: 'conservative' | 'balanced' | 'growth' | 'aggressive';
  retired?: boolean;
  currency?: string;
}

export interface PlanAllocation { core_equity: number; bonds: number; cash: number; satellite: number }

export interface PlanPhase { id: string; name: string; job: string; checklist: string[]; playbooks: string[] }

export interface PlanBand { year: number; age?: number; p10: number; p50: number; p90: number; real_p10: number; real_p50: number; real_p90: number }

export interface PlanMilestone { label: string; amount: number; year?: number }

export interface PlanAssetAssumption { expected_return_pct: number; volatility_pct: number }

export interface PlanAssumptions {
  core_equity: PlanAssetAssumption; bonds: PlanAssetAssumption; cash: PlanAssetAssumption; satellite: PlanAssetAssumption;
  inflation_pct: number; corr_equity_bond: number; corr_equity_satellite: number; paths: number; note: string;
}

export interface WealthPlan {
  phase: PlanPhase;
  target_allocation: PlanAllocation;
  fi_number: number;
  fi_ratio_pct: number;
  goal_amount: number;
  horizon_years: number;
  bands: PlanBand[];
  probability_goal_pct: number;
  years_to_goal_p50?: number;
  required_contribution_75?: number;
  milestones: PlanMilestone[];
  assumptions: PlanAssumptions;
}

/** GET /api/plan, PUT /api/plan/profile and POST /api/plan/preview. */
export interface PlanResponse { profile: PlanProfile | null; plan: WealthPlan | null; provenance?: Provenance }

export interface PortfolioSuggestion { symbol: string; side: 'buy' | 'sell'; amount: number; reason: string }

/** Risk of the paper portfolio from GET /api/portfolio/analytics (see internal/portfolio). */
export interface PortfolioAnalytics {
  total_value: number;
  weights_pct: Record<string, number>;
  top_weight_pct: number;
  hhi: number;
  effective_holdings: number;
  volatility_pct?: number;
  beta?: number;
  max_drawdown_1y_pct?: number;
  correlation: number[][];
  symbols: string[];
  risk_contribution_pct: Record<string, number>;
  class_weights_pct: Record<string, number>;
  drift_pct?: Record<string, number>;
  suggestions: PortfolioSuggestion[];
  alerts: string[];
}

export interface PortfolioAnalyticsResponse {
  /** Where the data came from (internal/provenance.Fact), or absent when the server recorded none. */
  provenance?: Provenance;
  analytics: PortfolioAnalytics;
  benchmark: string;
  has_plan: boolean;
  generated_at: string;
}

/** POST /api/trading/size: a ticket size from the risk the investor accepts. */
export interface SizeResponse {
  symbol: string;
  side: 'buy' | 'sell';
  entry: number;
  equity: number;
  risk_pct: number;
  atr_mult: number;
  atr: number;
  stop_source: 'atr' | 'given';
  result: { quantity: number; stop: number; risk_amount: number; position_pct: number; capped_by?: 'max_weight' | 'buying_power' };
}

/** A dividend, split or earnings date from GET /api/events (see internal/market/events.go). */
export interface CorporateEvent {
  symbol: string;
  kind: 'dividend' | 'split' | 'earnings';
  date: string;
  amount?: number;
  currency?: string;
  ratio?: string;
  confirmed: boolean;
  provider?: string;
}

export interface EventsResponse { symbol: string; from: string; to: string; events: CorporateEvent[] }

/** GET /api/today: every part is optional (see internal/server/today.go). */
export interface TodayBriefing {
  generated_at: string;
  /** One fact per part, keyed "phase", "portfolio", "macro" and "worth_a_look.<part>". */
  provenance?: Record<string, Provenance>;
  phase?: { id: string; name: string; probability_goal_pct: number; probability_label?: string; next_checklist_item?: string };
  macro?: { regime: 'risk_on' | 'caution' | 'risk_off'; reasons?: string; as_of?: string };
  portfolio?: { equity: number; unrealized_pnl: number; positions: number; day_change?: number; day_change_pct?: number; drawdown_from_peak_pct?: number; top_alert?: string };
  worth_a_look?: {
    leaderboard?: { symbol: string; score: number; grade: string; reasons?: string; universe: string; strategy: string; as_of?: string };
    insider?: { symbol: string; largest_buy_value: number; largest_buyer?: string; cluster_buy?: boolean; latest?: string; url?: string };
    earnings?: { symbol: string; date: string; days_until?: number; confirmed: boolean };
  };
}

export type Stance = 'fits' | 'partly' | 'does_not_fit' | 'insufficient_data';
export interface Persona {
  id: string; name: string; tagline: string; archetype: string; horizon: string;
  principles: string[]; checks: string[]; avoids: string[]; voice: string; tools: string[]; caveat?: string;
  strategies?: { kind: 'leaderboard' | 'rules'; id: string; why: string }[];
  process?: string[]; sizing?: string[]; sources?: { title: string; url?: string }[];
  portfolio?: ReferencePortfolio;
}
export interface PersonaCatalog { personas: Persona[]; stances: Stance[]; max_team: number }
/** One event of a team run, as streamed by POST /api/ai/team. */
export interface TeamEvent {
  model?: string;
  type: 'member_start' | 'member_done' | 'delta' | 'tool_start' | 'tool_result' | 'notice' | 'error' | 'done' | string;
  agent?: string; name?: string; content?: string; result?: string; tool_error?: string; error?: string; stance?: string;
  arguments?: string; call_id?: string; seq?: number; duration_ms?: number; truncated?: boolean; original_bytes?: number; redacted?: boolean;
  severity?: string; code?: string; attempt?: number; max_attempts?: number; detail?: string;
  /** On the 'budget' event that ends a run with a budget. */
  budget?: BudgetSummary;
  /** With a `sources` event: what the member retrieved and what the citation check found in its answer. */
  sources?: AnswerSource[]; audit?: AnswerFinding[]; removed?: AnswerFinding[]; claims?: AnswerClaim[]; claim_stats?: ClaimStats;
}
export type StrategyOperandType = 'price' | 'const' | 'ema' | 'sma' | 'rsi' | 'atr' | 'highest' | 'lowest' | 'fib_retr' | 'fib_ext' | 'swing_high' | 'swing_low';
export interface StrategyOperand { type: StrategyOperandType; field?: string; period?: number; ratio?: number; value?: number }
export type StrategyComparison = '>' | '>=' | '<' | '<=' | 'crosses_above' | 'crosses_below';
export interface StrategyCondition { left: StrategyOperand; op: StrategyComparison; right: StrategyOperand }
export interface StrategyLevel { of: StrategyOperand; atr?: number }
export interface StrategyDef {
  id?: string; name: string; description?: string; family?: string; persona?: string; direction: 'long' | 'short';
  swing_bars?: number; min_leg_atr?: number;
  entry: StrategyCondition[]; exit?: StrategyCondition[]; stop?: StrategyLevel | null; target?: StrategyLevel | null;
  max_bars?: number; sizing: { mode: 'fixed_pct' | 'risk_pct'; value: number };
}
export interface SavedStrategy { id: number; updated_at: string; definition: StrategyDef }
export interface StrategyCatalog { presets: StrategyDef[]; custom: SavedStrategy[]; warnings: Record<string, string> }
export interface StrategyTrade {
  entry_date: string; entry_price: number; exit_date: string; exit_price: number; stop?: number; target?: number;
  bars: number; return_pct: number; equity_return_pct?: number; r?: number; reason: 'stop' | 'target' | 'signal' | 'max_bars' | 'open';
}
export interface StrategyRunMetrics { cagr_pct: number; volatility_pct: number; sharpe: number; sortino: number; max_drawdown_pct: number; calmar: number }
export interface StrategyRunResult {
  /** Where the data came from (internal/provenance.Fact), or absent when the server recorded none. */
  provenance?: Provenance;
  symbol: string; provider: string; range: string; generated_at: string; strategy: StrategyDef;
  config: { cost_bps: number; slippage_bps?: number; spread_bps?: number }; parameters?: FitParameter[]; base_price: number; from: string; to: string; data_start?: string; data_end?: string; notice?: string;
  metrics: StrategyRunMetrics; buy_hold: StrategyRunMetrics; total_return_pct: number; buy_hold_return_pct: number;
  stats: { trades: number; win_rate_pct: number; avg_win_pct: number; avg_loss_pct: number; profit_factor?: number; expectancy_pct: number; avg_r?: number; avg_bars: number; exposure_pct: number };
  curve: { date: string; close: number; equity: number; in_position?: boolean }[];
  trades: StrategyTrade[]; warnings: string[];
  /** Named market episodes the tested period fully covers, and the ones it does not reach. */
  stress?: BacktestStress[]; stress_not_covered?: string[]; stress_note?: string;
  /** Every tested day in one regime of the instrument's own trend and volatility; unlabeled days are counted. */
  regimes?: BacktestRegime[]; regime_unlabeled_days?: number; regime_note?: string;
  walk_forward?: { from: string; to: string; trades: number; win_rate_pct: number; return_pct: number; buy_hold_return_pct: number; expectancy_pct: number }[];
  latest: { date: string; close: number; in_position: boolean; entry_signal: boolean; exit_signal: boolean; has_leg: boolean;
    leg_low?: number; leg_high?: number; leg_low_date?: string; leg_high_date?: string; levels?: { label: string; ratio: number; price: number }[] };
}
/** POST /api/strategies/montecarlo: the replay's observed figures next to simulated trade-level outcomes. */
export interface StrategyMonteCarloRequest extends StrategyRunRequest {
  method?: string; runs?: number; trades?: number; seed?: number; drawdown_limit?: number; extra_cost_bps?: number; dispersion_scale?: number;
  delay_bars?: number; regime_share_pct?: number; regime_scale?: number; position_scale?: number;
}
export interface StrategyMonteCarloResult {
  symbol: string; provider: string; range: string; generated_at: string; strategy: string; strategy_id?: string; cost_bps: number;
  from: string; to: string; data_start: string; data_end: string; notice?: string;
  observed: { total_return_pct: number; buy_hold_return_pct: number; closed_trades: number; open_trade_excluded?: boolean; warnings: string[] };
  simulation: {
    model_version: string;
    config: { method: string; runs: number; trades: number; seed: number; drawdown_limit_pct?: number; extra_cost_bps_per_trade?: number; dispersion_scale: number;
      delay_bars?: number; instrument_daily_vol_pct?: number; exposure?: number; regime_share_pct?: number; regime_scale?: number; position_scale?: number };
    concentration?: { best_trades_removed: number; return_without_best_pct: number; largest_loss_pct: number };
    observed_trades: number; observed: { days: number; total_return_pct: number; max_drawdown_pct: number };
    terminal_return_pct: MonteCarloDist; max_drawdown_pct: MonteCarloDist;
    prob_loss_pct: number; prob_breach_pct?: number; prob_ruin_pct: number; assumptions: string[]; limitations: string[];
  };
  notes: string[];
}
export interface StrategyRunRequest { strategy_id?: string; definition?: StrategyDef; symbol: string; provider?: string; range?: '1Y' | '3Y' | '5Y' | '10Y' | 'YTD' | 'MAX'; from?: string; to?: string; cost_bps: number; slippage_bps?: number; spread_bps?: number }

/** A number of a strategy that a walk-forward fit can change. */
export interface FitParameter { path: string; label: string; value: number; integer: boolean; min: number; max: number }
export type FitMode = 'rolling' | 'anchored';
export type FitObjective = 'return' | 'sharpe' | 'expectancy';
export interface FitRequest extends StrategyRunRequest { params: string[]; folds: number; mode: FitMode; objective: FitObjective }
export interface FitFoldResult {
  index: number; train_from: string; train_to: string; test_from: string; test_to: string;
  chosen?: { path: string; label: string; value: number }[]; tried: number; qualified: number;
  train_score: number; train_return_pct: number; train_trades: number;
  test_return_pct: number; test_trades: number; test_max_drawdown_pct: number; unfitted_return_pct: number; buy_hold_return_pct: number;
  skipped?: string;
}
export interface FitSummary {
  folds_used: number; return_pct: number; unfitted_return_pct: number; buy_hold_return_pct: number; trades: number;
  mean_train_return_pct: number; mean_test_return_pct: number; efficiency?: number; distinct_choices: number;
}
/** The answer of POST /api/strategies/walkforward. */
export interface FitResult {
  symbol: string; provider: string; range: string; generated_at: string; data_start?: string; data_end?: string; notice?: string;
  warning_texts: Record<string, string>;
  strategy: string; objective: FitObjective; mode: FitMode; from: string; to: string;
  params: { path: string; label: string; base: number; values: number[] }[];
  combos_per_fold: number; folds: FitFoldResult[]; out_of_sample?: FitSummary; warnings: string[]; note: string; parameters_fitted: boolean;
  config: { cost_bps: number; slippage_bps?: number };
}

export interface ReferencePosition { symbol: string; name: string; weight_pct: number; shares?: number; shares_change_pct?: number }
export interface ReferencePortfolio {
  persona: string; kind: '13f' | 'etf_daily' | 'rules'; filer: string; form?: string; period: string; filed?: string; accession?: string; source_url: string;
  total_value_usd?: number; stats: { positions: number; top1_pct: number; top5_pct: number; top10_pct: number; hhi: number };
  positions: ReferencePosition[]; exited_since_previous?: string[]; notes: string[]; rules?: string[];
}
export type WorldRole = 'benefits' | 'hurt' | 'mixed';
export interface WorldExposure { kind: 'sector' | 'commodity' | 'macro'; name: string; role: WorldRole; why: string }
export interface WorldHeadline { provenance?: Provenance; title: string; url: string; source?: string; published?: number; age?: string; summary?: string; via: string; themes?: string[] }
export interface WorldTheme { id: string; label: string; driver: string; exposures: WorldExposure[]; headlines: WorldHeadline[]; count: number }
export interface WorldSector { name: string; benefits_from?: string[]; hurt_by?: string[]; mixed?: string[] }
export interface WorldView { generated_at: string; sources: string[]; notes?: string[]; themes: WorldTheme[]; sectors: WorldSector[] }
export interface CompanyWorldTheme {
  id: string; label: string; driver: string; role?: WorldRole; why?: string; basis: string; count: number;
  headlines: WorldHeadline[]; own_headlines?: WorldHeadline[];
}
export interface CompanyWorld {
  symbol: string; sector?: string; sector_source?: 'chosen' | 'config' | 'built-in'; generated_at: string;
  themes: CompanyWorldTheme[]; news: WorldHeadline[]; notes?: string[];
}

/** A model choice an administrator prepared. Unknown capabilities and costs are null, and shown as unknown. */
export interface ModelProfile {
  id: number; name: string; description: string; model: string; fallback_model?: string;
  supports_tools: boolean | null; context_window: number | null;
  cost_input_per_mtok: number | null; cost_output_per_mtok: number | null;
  max_steps?: number | null; max_tool_calls?: number | null;
  enabled?: boolean; is_default: boolean; position?: number;
}
export interface SavedTeamMember { persona: string; profile_id: number | null }
export interface SavedTeam {
  id: number; name: string; chair: boolean; members: SavedTeamMember[]; updated_at?: string;
  /** The team's budget ceilings for one run; null for none. */
  max_tokens?: number | null; max_cost?: number | null; max_tool_calls?: number | null;
}

export interface AboutInfo {
  name: string;
  license: string;
  development: boolean;
  commit?: string;
  /** RFC 3339, UTC. */
  build_time?: string;
}

/** One way FloweySales can get data (GET /api/providers/routing). It never carries a credential. */
export interface RoutingSource {
  id: string; label: string; kind: 'plugin' | 'builtin' | 'user'; domains: string[]; scope: 'system' | 'user';
  needs_key: boolean; configured: boolean; enabled: boolean; regions?: string[]; restrictions?: string; origin?: string;
  experimental?: boolean; managed_by?: string;
}

export interface RoutingDomain { id: string; label: string; group: string; no_fallback: boolean; why?: string; sources: number; routed: boolean }

export interface RoutingPolicy { domain: string; primary: string; fallback?: string[]; no_fallback?: boolean }

export interface RoutingView { domains: RoutingDomain[]; sources: RoutingSource[]; policies: Record<string, RoutingPolicy>; can_edit: boolean; notice: string }

/** A variable a user may supply for their own instance of a provider; the value is never returned. */
export interface OwnKeyVariable { name: string; description: string; required: boolean; saved: boolean }
export interface OwnKeyPlugin { plugin_id: string; provider_id: string; label: string; allowed: boolean; variables: OwnKeyVariable[] }
/** GET /api/providers/own-keys */
export interface OwnKeysView { plugins: OwnKeyPlugin[]; can_edit: boolean; signed_in: boolean; notice: string }
