/**
 * Investor nodes and playbooks for Flow Studio: the node catalogue (labels,
 * canvas tags, defaults) and ready-made automations built from it. Mirrors
 * the server's flow_investor.go. Free of Angular so it runs under node --test.
 */

export type InvestorNodeType =
  'watchlist_quotes' | 'technical_scan' | 'fundamentals' | 'sec_filings' | 'news' | 'leaderboard' |
  'screen' | 'only_new' | 'report_table' | 'notify' | 'insider_activity' | 'macro_regime' | 'portfolio_check' | 'corporate_events' | 'wealth_plan' | 'backtest';

export interface InvestorNodeSpec {
  type: InvestorNodeType;
  label: string;
  tag: string;
  admin: boolean;
  description: string;
  defaults: Record<string, string | number | boolean>;
}

export const DEFAULT_WATCHLIST = 'AAPL, MSFT, NVDA, AMZN, GOOGL';

export const INVESTOR_NODES: readonly InvestorNodeSpec[] = [
  { type: 'watchlist_quotes', label: 'Watchlist quotes', tag: '$ QUOTES', admin: false, defaults: { symbols: DEFAULT_WATCHLIST },
    description: 'Last close, 1D/5D/1M/3M/YTD/1Y change and the 52-week range for up to 25 symbols, computed from daily bars.' },
  { type: 'technical_scan', label: 'Technical scan', tag: '∿ TECHNICALS', admin: false, defaults: { symbols: DEFAULT_WATCHLIST },
    description: 'RSI, MACD, EMA stack, 50/200-day averages and crosses, Bollinger position and volatility per symbol.' },
  { type: 'fundamentals', label: 'Fundamentals', tag: '≡ FUNDAMENTALS', admin: false, defaults: { symbols: DEFAULT_WATCHLIST },
    description: 'Trailing-twelve-month revenue, margins, EPS, growth and P/E from reported quarters. Missing figures stay absent.' },
  { type: 'sec_filings', label: 'SEC filings', tag: '§ SEC FILINGS', admin: false, defaults: { symbols: DEFAULT_WATCHLIST, forms: '8-K, 10-Q, 10-K', lookback_days: 3 },
    description: 'Filings made in the last days, from SEC EDGAR, with links to the documents.' },
  { type: 'news', label: 'News headlines', tag: '✎ NEWS', admin: false, defaults: { symbols: DEFAULT_WATCHLIST, limit: 5, lookback_days: 3 },
    description: 'Recent headlines per symbol from an installed provider plugin with the news capability.' },
  { type: 'leaderboard', label: 'Leaderboard (score & rank)', tag: '★ LEADERBOARD', admin: false, defaults: { universe: 'us_mega', strategy: 'best', limit: 10 },
    description: 'Scores a universe on value, quality, growth, momentum, entry timing and risk from quarterly reports and prices, then ranks it for a strategy.' },
  { type: 'insider_activity', label: 'Insider activity', tag: '⚑ INSIDERS', admin: false, defaults: { symbols: DEFAULT_WATCHLIST, lookback_days: 30 },
    description: 'Open-market insider purchases and sales from SEC Form 4 filings: counts, buyers, net value, officer buys and clusters of three or more buyers.' },
  { type: 'macro_regime', label: 'Macro regime', tag: '◎ MACRO', admin: false, defaults: {},
    description: 'Yield curve, unemployment, credit spreads and volatility from FRED read as risk-on, caution or risk-off. Needs the FRED plugin.' },
  { type: 'portfolio_check', label: 'Portfolio check', tag: '◔ PORTFOLIO', admin: true, defaults: {},
    description: 'Concentration, correlation, drawdown from peak and drift from your wealth plan for the paper portfolio, with rebalancing suggestions as rows.' },
  { type: 'corporate_events', label: 'Dividends & earnings', tag: '☷ EVENTS', admin: false, defaults: { symbols: DEFAULT_WATCHLIST, limit: 30 },
    description: 'Upcoming dividends, splits and earnings dates from an installed provider plugin with the events capability.' },
  { type: 'wealth_plan', label: 'Wealth plan', tag: '◈ PLAN', admin: false, defaults: {},
    description: 'Your saved wealth plan as one row: phase, target allocation, probability of reaching the goal, years to goal and the next checklist item.' },
  { type: 'backtest', label: 'Strategy backtest', tag: '⏱ BACKTEST', admin: false, defaults: { universe: 'us_mega', strategy: 'best', limit: 10, years: 3 },
    description: 'Replays a leaderboard strategy month by month over past prices and published quarters. One row per strategy, with the bias warnings. Slow on a cold cache; one per flow.' },
  { type: 'screen', label: 'Screen (filter rows)', tag: '⧩ SCREEN', admin: false, defaults: { rule: 'change_1d_pct <= -3' },
    description: 'Keeps the rows that match a rule such as rsi14 < 30 and above_sma200, then sorts and limits them.' },
  { type: 'only_new', label: 'Only new since last run', tag: '✓ ONLY NEW', admin: false, defaults: {},
    description: 'Remembers what earlier runs passed on and lets through only new rows, or rows again after a cooldown.' },
  { type: 'report_table', label: 'Report table', tag: '▦ REPORT', admin: false, defaults: { title: 'Watchlist report' },
    description: 'Renders rows as a Markdown table. No rows renders nothing, so later nodes can skip.' },
  { type: 'notify', label: 'Notify (email / chat)', tag: '✉ NOTIFY', admin: true, defaults: { channel: 'email', subject: '{{flow}}: {{count}} item(s) on {{date}}' },
    description: 'Emails the flow owner or posts to the configured Slack, Discord or Teams webhook. Empty input sends nothing.' },
];

/** Mirrors leaderboardUniverses and leaderboardStrategies in leaderboard.go. */
export const LEADERBOARD_UNIVERSES: readonly { id: string; label: string }[] = [
  { id: 'us_mega', label: 'US mega caps' },
  { id: 'us_tech', label: 'US technology' },
  { id: 'us_dividend', label: 'US dividend & value' },
  { id: 'se_large', label: 'Sweden large caps' },
  { id: 'eu_leaders', label: 'European leaders' },
];

export const LEADERBOARD_STRATEGIES: readonly { id: string; label: string }[] = [
  { id: 'best', label: 'Best buy now' },
  { id: 'value', label: 'Cheap & solid' },
  { id: 'growth', label: 'Growth at a fair price' },
  { id: 'momentum', label: 'Momentum leaders' },
  { id: 'quality', label: 'Quality compounders' },
  { id: 'dip', label: 'Quality on a dip' },
];

export const LEADERBOARD_FACTORS = ['value', 'quality', 'growth', 'momentum', 'timing', 'risk'] as const;

const byType = new Map(INVESTOR_NODES.map(n => [n.type, n]));

export function investorNode(type: string): InvestorNodeSpec | undefined {
  return byType.get(type as InvestorNodeType);
}

export function isInvestorNode(type: string): boolean {
  return byType.has(type as InvestorNodeType);
}

/** Field names a screen rule can use, per node that produces rows. */
export const SCREEN_FIELDS: Record<string, string[]> = {
  watchlist_quotes: ['price', 'change_1d_pct', 'change_5d_pct', 'change_1m_pct', 'change_3m_pct', 'change_ytd_pct', 'change_1y_pct', 'high_52w', 'low_52w', 'from_high_pct', 'stale'],
  technical_scan: ['rsi14', 'rsi_state', 'macd_trend', 'macd_hist', 'ema_stack', 'sma50', 'sma200', 'above_sma50', 'above_sma200', 'cross', 'bollinger', 'volatility_pct', 'atr_pct'],
  fundamentals: ['revenue_ttm', 'revenue_growth_pct', 'gross_margin_pct', 'operating_margin_pct', 'net_margin_pct', 'fcf_margin_pct', 'eps_ttm', 'eps_growth_pct', 'pe_ttm'],
  sec_filings: ['form', 'filed', 'company', 'period_end'],
  news: ['title', 'source', 'published', 'summary'],
  insider_activity: ['buy_count', 'sell_count', 'buyers', 'net_shares', 'net_value', 'largest_buy_value', 'largest_buyer', 'officer_buy', 'cluster_buy', 'latest'],
  macro_regime: ['regime', 'signals', 'reasons', 'curve_spread', 'curve_inverted', 'curve_uninverting', 'unemployment', 'sahm_rule', 'fed_funds', 'hy_spread', 'credit_stress', 'vix', 'vol_regime'],
  portfolio_check: ['kind', 'drawdown_from_peak_pct', 'total_value', 'top_weight_pct', 'volatility_pct', 'beta', 'max_drawdown_1y_pct', 'effective_holdings', 'side', 'amount', 'symbol', 'reason'],
  corporate_events: ['kind', 'date', 'days_until', 'amount', 'currency', 'ratio', 'confirmed'],
  wealth_plan: ['phase', 'probability_goal_pct', 'years_to_goal_p50', 'required_contribution_75', 'fi_ratio_pct', 'invested', 'monthly_contribution', 'core_equity_pct', 'bonds_pct', 'cash_pct', 'satellite_pct'],
  backtest: ['strategy', 'cagr_pct', 'excess_cagr_pct', 'volatility_pct', 'sharpe', 'sortino', 'max_drawdown_pct', 'calmar', 'hit_rate_pct', 'avg_turnover_pct', 'rebalances'],
  leaderboard: ['rank', 'score', 'grade', ...LEADERBOARD_FACTORS, 'coverage_pct', 'flags', 'pe_ttm', 'peg', 'earnings_yield_pct', 'revenue_growth_pct', 'eps_growth_pct',
    'net_margin_pct', 'fcf_margin_pct', 'change_3m_pct', 'momentum_12_1_pct', 'from_high_pct', 'rsi14', 'above_sma200', 'volatility_pct', 'max_drawdown_pct'],
};

/** Splits a watchlist the way the server does; returns the problem, if any. */
export function watchlistProblem(raw: string): string {
  const symbols = raw.split(/[,;\s]+/).map(s => s.trim()).filter(Boolean);
  if (!symbols.length) return '';
  if (new Set(symbols.map(s => s.toUpperCase())).size > 25) return 'At most 25 symbols per node.';
  const bad = symbols.find(s => s.length > 40 || !/^[A-Za-z0-9.\-_:/]+$/.test(s));
  return bad ? `"${bad}" is not a valid ticker.` : '';
}

export interface PlaybookNode { id: string; type: string; label: string; x: number; y: number; [field: string]: unknown }
export interface PlaybookEdge { from: string; to: string; from_port?: string; to_port?: string }
export interface Playbook {
  name: string;
  intervalMinutes: number;
  graph: { version: 2; nodes: PlaybookNode[]; edges: PlaybookEdge[] };
  note: string;
}

export interface PlaybookInfo { id: string; label: string; description: string; icon: string; schedule: string; adminOnly?: boolean }

export const INVESTOR_PLAYBOOKS: readonly PlaybookInfo[] = [
  { id: 'morning-brief', label: 'Morning watchlist brief', icon: 'bi-sunrise', schedule: 'Daily',
    description: 'Prices, returns and technicals for your watchlist, written up by an agent that may only cite figures from the data.' },
  { id: 'price-alerts', label: 'Price move alerts', icon: 'bi-bell', schedule: 'Hourly',
    description: 'Alerts once per day per symbol when a stock drops 4% in a day or falls 20% below its 52-week high.' },
  { id: 'technical-setups', label: 'Technical setups scanner', icon: 'bi-graph-up-arrow', schedule: 'Daily',
    description: 'Finds oversold stocks in an uptrend and fresh golden crosses, reported once per setup.' },
  { id: 'filings-monitor', label: 'SEC filing monitor', icon: 'bi-file-earmark-text', schedule: 'Every 6 hours',
    description: 'New 8-K, 10-Q and 10-K filings for your holdings, summarized with links. Silent when nothing was filed.' },
  { id: 'news-digest', label: 'News catalyst digest', icon: 'bi-newspaper', schedule: 'Every 4 hours',
    description: 'New headlines for your watchlist, grouped into catalysts and risks with links. Silent when nothing is new.' },
  { id: 'quality-screen', label: 'Quality & value screen', icon: 'bi-funnel', schedule: 'Weekly',
    description: 'Screens for growth, margins and a reasonable P/E, then drafts memo bullets that separate facts from assumptions.' },
  { id: 'top10-buy-now', label: 'Top 10 buy-now leaderboard', icon: 'bi-trophy', schedule: 'Daily',
    description: 'Scores 25 US mega caps on quarterly reports, valuation, trend, entry timing and risk, ranks the top 10 and writes a grounded memo per pick.' },
  { id: 'leaderboard-entrants', label: 'New leaderboard entrants', icon: 'bi-box-arrow-in-up-right', schedule: 'Daily',
    description: 'Alerts when a company enters the top 10 of the buy-now leaderboard, at most once per company every two weeks.' },
  { id: 'value-bargains', label: 'Cheap & solid bargains', icon: 'bi-piggy-bank', schedule: 'Weekly',
    description: 'Low P/E dividend and value stocks that are still profitable and above their 200-day average, with a facts-vs-assumptions memo.' },
  { id: 'garp-growth', label: 'Growth at a reasonable price', icon: 'bi-rocket-takeoff', schedule: 'Weekly',
    description: 'Tech companies growing revenue 10%+ with a PEG under 2, ranked for growth, with what has to keep going right.' },
  { id: 'momentum-leaders', label: 'Momentum leaders alert', icon: 'bi-speedometer2', schedule: 'Daily',
    description: 'The strongest uptrends that are not overbought, reported once a week per company.' },
  { id: 'quality-dip', label: 'Quality on a dip (Sweden)', icon: 'bi-arrow-down-right-circle', schedule: 'Daily',
    description: 'Profitable Stockholm large caps that pulled back 8%+ while the 200-day trend holds, with an agent note on why each fell.' },
  { id: 'earnings-scored', label: 'Quarterly reports, scored', icon: 'bi-clipboard-data', schedule: 'Every 6 hours',
    description: 'When a watchlist company files a 10-Q or 10-K, ranks it against the watchlist on the new numbers and summarizes what changed.' },
  { id: 'watchlist-health', label: 'Watchlist health check', icon: 'bi-heart-pulse', schedule: 'Weekly',
    description: 'Finds laggards in your watchlist: low scores, broken trends, losses or shrinking revenue, and what would repair the thesis.' },
  { id: 'insider-cluster', label: 'Insider buying clusters', icon: 'bi-person-check', schedule: 'Daily',
    description: 'Alerts when three or more insiders buy the same stock on the open market, or an officer buys more than $250,000, from SEC Form 4 filings.' },
  { id: 'macro-regime', label: 'Macro regime shift', icon: 'bi-globe2', schedule: 'Daily',
    description: 'Reads the yield curve, unemployment, credit spreads and volatility from FRED and explains what changed when the regime moves. Needs the FRED plugin.' },
  { id: 'drawdown-guard', label: 'Portfolio drawdown guard', icon: 'bi-shield-exclamation', schedule: 'Hourly', adminOnly: true,
    description: 'Alerts when the paper portfolio is 10% or more below its peak, or its holdings would have fallen more than 20% over the past year.' },
  { id: 'rebalance-check', label: 'Monthly rebalance check', icon: 'bi-arrow-left-right', schedule: 'Monthly', adminOnly: true,
    description: 'Compares the paper portfolio with your wealth plan and writes up the moves that bring it back to target. Suggestions only; nothing is ordered.' },
  { id: 'dividend-calendar', label: 'Dividend & earnings calendar', icon: 'bi-calendar-event', schedule: 'Weekly',
    description: 'Upcoming dividends, splits and earnings dates for your watchlist over the next 30 days, each reported once. Needs a provider plugin with events.' },
  { id: 'phase-checkin', label: 'Monthly phase check-in', icon: 'bi-bullseye', schedule: 'Monthly',
    description: 'A short progress note on your wealth plan: your phase, the probability of reaching your goal and the next item on your checklist.' },
  { id: 'earnings-week', label: 'Earnings this week, scored', icon: 'bi-calendar-check', schedule: 'Weekly',
    description: 'Companies on your watchlist reporting earnings in the next 7 days, ranked on the leaderboard with a memo on what to look for.' },
  { id: 'strategy-scorecard', label: 'Strategy scorecard', icon: 'bi-clipboard-pulse', schedule: 'Monthly',
    description: 'Backtests all six leaderboard strategies on US mega caps and compares them, with the survivorship and benchmark caveats stated.' },
];

interface BuildOptions { admin: boolean; symbols?: string }

const GROUNDED = 'Use only figures that appear in the input data and name the date they are as of. ';
const LEADERBOARD_GUIDE = 'Leaderboard scores are 0–100 percentiles within the ranked universe (value, quality, growth, momentum, timing, risk), not price targets or predictions. ';
const LEADERBOARD_COLUMNS = 'rank,symbol,score,grade,pe_ttm,revenue_growth_pct,momentum_12_1_pct,from_high_pct,reasons,flags';

/** Builds a playbook as a disabled, versioned flow graph. */
export function buildPlaybook(id: string, opts: BuildOptions): Playbook | null {
  if (INVESTOR_PLAYBOOKS.find(p => p.id === id)?.adminOnly && !opts.admin) return null;
  const symbols = (opts.symbols || '').trim() || DEFAULT_WATCHLIST;
  const nodes: PlaybookNode[] = [{ id: 'start', type: 'trigger', label: 'Start', x: 40, y: 160 }];
  const edges: PlaybookEdge[] = [];
  let col = 1;
  const add = (node: Omit<PlaybookNode, 'x' | 'y'>, from: string | string[], row = 0): string => {
    const id = node['id'] as string;
    nodes.push({ ...node, x: 40 + col * 175, y: 160 + row * 110 } as PlaybookNode);
    for (const f of Array.isArray(from) ? from : [from]) edges.push({ from: f, to: id });
    if (row === 0) col++;
    return id;
  };
  // Admins get a Notify node; everyone else ends in an Output they can read in Run history.
  const deliver = (from: string, subject: string) => {
    if (opts.admin) add({ id: 'notify', type: 'notify', label: 'Email me', channel: 'email', subject }, from);
    else add({ id: 'output', type: 'output', label: 'Result' }, from);
  };
  let name = '', interval = 0, note = '';
  switch (id) {
    case 'morning-brief': {
      name = 'Morning watchlist brief'; interval = 1440;
      add({ id: 'quotes', type: 'watchlist_quotes', label: 'Watchlist quotes', symbols }, 'start');
      nodes.push({ id: 'tech', type: 'technical_scan', label: 'Technicals', x: 40 + col * 175, y: 270 });
      edges.push({ from: 'quotes', to: 'tech' });
      col++;
      add({ id: 'data', type: 'merge', label: 'Combine data' }, ['quotes', 'tech']);
      add({ id: 'brief', type: 'agent', label: 'Write brief', require_grounding: true,
        prompt: GROUNDED + 'Write a concise morning brief for an investor: the biggest movers, stocks near their 52-week high or low, notable technical signals (RSI, trend, crosses) and what to watch today. End with a short risk note. Markdown, under 300 words.' }, 'data');
      deliver('brief', 'Morning brief {{date}}');
      note = 'Runs daily once enabled. The agent can only state figures found in the quotes and technicals.';
      break;
    }
    case 'price-alerts': {
      name = 'Price move alerts'; interval = 60;
      add({ id: 'quotes', type: 'watchlist_quotes', label: 'Watchlist quotes', symbols }, 'start');
      add({ id: 'moves', type: 'screen', label: 'Big moves', rule: 'change_1d_pct <= -4 or from_high_pct <= -20', sort_by: 'change_1d_pct' }, 'quotes');
      add({ id: 'new', type: 'only_new', label: 'Once a day', key_fields: 'symbol', cooldown_hours: 24 }, 'moves');
      add({ id: 'report', type: 'report_table', label: 'Alert table', title: 'Price alerts', columns: 'symbol,price,currency,change_1d_pct,from_high_pct,as_of', skip_empty: true }, 'new');
      deliver('report', '{{count}} price alert(s) {{date}}');
      note = 'Checks hourly once enabled and alerts at most once per symbol per day. Edit the screen rule to change the thresholds.';
      break;
    }
    case 'technical-setups': {
      name = 'Technical setups scanner'; interval = 1440;
      add({ id: 'tech', type: 'technical_scan', label: 'Technical scan', symbols }, 'start');
      add({ id: 'setups', type: 'screen', label: 'Setups', rule: '(rsi14 < 35 and above_sma200) or cross == "golden"', sort_by: 'rsi14' }, 'tech');
      add({ id: 'new', type: 'only_new', label: 'New setups', key_fields: 'symbol', cooldown_hours: 72 }, 'setups');
      add({ id: 'report', type: 'report_table', label: 'Setup table', title: 'Technical setups', columns: 'symbol,price,rsi14,ema_stack,sma200,cross,as_of', skip_empty: true }, 'new');
      deliver('report', 'Technical setups {{date}}');
      note = 'Oversold stocks still above their 200-day average, and fresh golden crosses. A setup is reported again after 3 days at most.';
      break;
    }
    case 'filings-monitor': {
      name = 'SEC filing monitor'; interval = 360;
      add({ id: 'filings', type: 'sec_filings', label: 'Recent filings', symbols, forms: '8-K, 10-Q, 10-K', lookback_days: 3 }, 'start');
      add({ id: 'new', type: 'only_new', label: 'New filings' }, 'filings');
      const agent: Omit<PlaybookNode, 'x' | 'y'> = { id: 'summary', type: 'agent', label: 'Summarize', skip_empty: true,
        prompt: 'For each new SEC filing in the input, say what kind of filing it is and what an investor should check in it. Flag 8-Ks that may be material (results, guidance, leadership, deals, financing). Link every filing URL. Do not guess the contents of a filing you have not read.' };
      if (opts.admin) Object.assign(agent, { tools: ['get_sec_section'], max_tool_calls: 6, timeout_seconds: 90 });
      add(agent, 'new');
      deliver('summary', 'New SEC filings {{date}}');
      note = 'Needs SEC_USER_AGENT on the server. Nothing runs past "New filings" when there is nothing new, so the agent costs nothing on quiet days.';
      break;
    }
    case 'news-digest': {
      name = 'News catalyst digest'; interval = 240;
      add({ id: 'news', type: 'news', label: 'Headlines', symbols, limit: 5, lookback_days: 2 }, 'start');
      add({ id: 'new', type: 'only_new', label: 'New headlines' }, 'news');
      add({ id: 'digest', type: 'agent', label: 'Digest', skip_empty: true,
        prompt: 'Group the new headlines in the input by symbol into likely catalysts and risks. One line each, with the link. Treat headlines as claims from their source, not facts, and skip duplicates.' }, 'new');
      deliver('digest', 'News digest {{date}}');
      note = 'Needs a provider plugin with the news capability (Yahoo or Finnhub). Silent when there are no new headlines.';
      break;
    }
    case 'quality-screen': {
      name = 'Quality & value screen'; interval = 10080;
      add({ id: 'fund', type: 'fundamentals', label: 'Fundamentals', symbols }, 'start');
      add({ id: 'screen', type: 'screen', label: 'Quality & value', rule: 'revenue_growth_pct >= 10 and net_margin_pct >= 15 and pe_ttm < 35', sort_by: '-revenue_growth_pct', limit: 10 }, 'fund');
      add({ id: 'table', type: 'report_table', label: 'Shortlist', title: 'Quality & value shortlist', columns: 'symbol,price,pe_ttm,revenue_growth_pct,net_margin_pct,eps_growth_pct,ttm_through', skip_empty: true }, 'screen');
      add({ id: 'memo', type: 'agent', label: 'Memo bullets', require_grounding: true,
        prompt: GROUNDED + 'For each company in the shortlist write three bullets: what the numbers show (facts), what would have to be true for the stock to work (assumptions), and the main risk. Keep the table at the top. This is research, not a recommendation.' }, 'table');
      deliver('memo', 'Quality & value shortlist {{date}}');
      note = 'Weekly screen on trailing-twelve-month figures. Adjust the thresholds in the screen rule.';
      break;
    }
    case 'top10-buy-now': {
      name = 'Top 10 buy-now leaderboard'; interval = 1440;
      add({ id: 'board', type: 'leaderboard', label: 'Score & rank', universe: 'us_mega', strategy: 'best', limit: 10 }, 'start');
      add({ id: 'table', type: 'report_table', label: 'Leaderboard', title: 'Top 10 to research now', columns: LEADERBOARD_COLUMNS, skip_empty: true }, 'board');
      add({ id: 'memo', type: 'agent', label: 'Pick memos', require_grounding: true,
        prompt: GROUNDED + LEADERBOARD_GUIDE + 'Keep the table at the top. Then for each company, in rank order, write two lines: why it ranks there (cite the strongest factor scores and their figures from reasons) and what to check before buying (flags, weakest factor, the next quarterly report). End with one paragraph on what the top 10 have in common. Research, not a recommendation.' }, 'table');
      deliver('memo', 'Top 10 leaderboard {{date}}');
      note = 'Ranks the US mega caps universe; change the universe or strategy on the leaderboard node, or set your own watchlist of up to 25 symbols.';
      break;
    }
    case 'leaderboard-entrants': {
      name = 'New leaderboard entrants'; interval = 1440;
      add({ id: 'board', type: 'leaderboard', label: 'Top 10', universe: 'us_mega', strategy: 'best', limit: 10 }, 'start');
      add({ id: 'new', type: 'only_new', label: 'New in top 10', key_fields: 'symbol', cooldown_hours: 336 }, 'board');
      add({ id: 'table', type: 'report_table', label: 'Entrants', title: 'New in the top 10', columns: 'rank,symbol,score,grade,reasons,flags,price,as_of', skip_empty: true }, 'new');
      deliver('table', '{{count}} new leaderboard entrant(s) {{date}}');
      note = 'The first run reports the whole top 10; after that only companies that enter it, at most once every 14 days each.';
      break;
    }
    case 'value-bargains': {
      name = 'Cheap & solid bargains'; interval = 10080;
      add({ id: 'board', type: 'leaderboard', label: 'Rank for value', universe: 'us_dividend', strategy: 'value', limit: 25 }, 'start');
      add({ id: 'screen', type: 'screen', label: 'Bargains', rule: 'pe_ttm < 20 and net_margin_pct >= 8 and above_sma200 and not (flags contains "shrinking_revenue")', sort_by: '-score', limit: 10 }, 'board');
      add({ id: 'table', type: 'report_table', label: 'Shortlist', title: 'Cheap & solid shortlist', columns: 'rank,symbol,score,pe_ttm,earnings_yield_pct,net_margin_pct,revenue_growth_pct,from_high_pct,ttm_through', skip_empty: true }, 'screen');
      add({ id: 'memo', type: 'agent', label: 'Value memo', require_grounding: true,
        prompt: GROUNDED + LEADERBOARD_GUIDE + 'Keep the table at the top. For each company: the facts that make it cheap (P/E, earnings yield, margins), the assumption that must hold for the discount to close, and the value-trap risk (low growth, falling margins). Research, not a recommendation.' }, 'table');
      deliver('memo', 'Value bargains {{date}}');
      note = 'Weekly. A low P/E alone is not a bargain: the screen also asks for margins, an intact trend and growing revenue.';
      break;
    }
    case 'garp-growth': {
      name = 'Growth at a reasonable price'; interval = 10080;
      add({ id: 'board', type: 'leaderboard', label: 'Rank for growth', universe: 'us_tech', strategy: 'growth', limit: 25 }, 'start');
      add({ id: 'screen', type: 'screen', label: 'Fair-priced growth', rule: 'revenue_growth_pct >= 10 and peg < 2 and not (flags contains "unprofitable")', sort_by: '-score', limit: 10 }, 'board');
      add({ id: 'table', type: 'report_table', label: 'Shortlist', title: 'Growth at a reasonable price', columns: 'rank,symbol,score,revenue_growth_pct,eps_growth_pct,pe_ttm,peg,net_margin_pct,ttm_through', skip_empty: true }, 'screen');
      add({ id: 'memo', type: 'agent', label: 'Growth memo', require_grounding: true,
        prompt: GROUNDED + LEADERBOARD_GUIDE + 'Keep the table at the top. For each company: the growth the numbers show, what has to keep going right for the PEG to stay reasonable, and the first sign that would break the thesis. Research, not a recommendation.' }, 'table');
      deliver('memo', 'Growth at a reasonable price {{date}}');
      note = 'PEG is the trailing P/E divided by trailing EPS growth; companies with shrinking EPS have none and do not pass.';
      break;
    }
    case 'momentum-leaders': {
      name = 'Momentum leaders alert'; interval = 1440;
      add({ id: 'board', type: 'leaderboard', label: 'Rank for momentum', universe: 'us_mega', strategy: 'momentum', limit: 10 }, 'start');
      add({ id: 'screen', type: 'screen', label: 'Healthy trends', rule: 'above_sma200 and momentum >= 60 and not (flags contains "overbought")', sort_by: 'rank' }, 'board');
      add({ id: 'new', type: 'only_new', label: 'Once a week', key_fields: 'symbol', cooldown_hours: 168 }, 'screen');
      add({ id: 'table', type: 'report_table', label: 'Leaders', title: 'Momentum leaders', columns: 'rank,symbol,score,momentum_12_1_pct,change_3m_pct,rsi14,from_high_pct,as_of', skip_empty: true }, 'new');
      deliver('table', 'Momentum leaders {{date}}');
      note = 'Leaves out overbought names (RSI 70+) so you hear about strong trends at a calmer entry.';
      break;
    }
    case 'quality-dip': {
      name = 'Quality on a dip (Sweden)'; interval = 1440;
      add({ id: 'board', type: 'leaderboard', label: 'Rank for dips', universe: 'se_large', strategy: 'dip', limit: 25 }, 'start');
      add({ id: 'screen', type: 'screen', label: 'Real dips', rule: 'from_high_pct <= -8 and quality >= 60 and above_sma200', sort_by: '-score', limit: 8 }, 'board');
      add({ id: 'new', type: 'only_new', label: 'New dips', key_fields: 'symbol', cooldown_hours: 72 }, 'screen');
      add({ id: 'table', type: 'report_table', label: 'Dips', title: 'Quality on a dip', columns: 'rank,symbol,score,from_high_pct,rsi14,quality,net_margin_pct,pe_ttm,as_of', skip_empty: true }, 'new');
      const agent: Omit<PlaybookNode, 'x' | 'y'> = { id: 'why', type: 'agent', label: 'Why it fell', skip_empty: true,
        prompt: LEADERBOARD_GUIDE + 'Keep the table at the top. For each company, say how far it is below its high and what its quality score rests on. Then give the likely reason for the pullback only if you can source it, otherwise say it is unexplained and what to look up. Research, not a recommendation.' };
      if (opts.admin) Object.assign(agent, { tools: ['search_web'], max_tool_calls: 6, timeout_seconds: 90 });
      add(agent, 'table');
      deliver('why', 'Quality on a dip {{date}}');
      note = 'Uses the Sweden large caps universe. A company is reported again after 3 days at most.';
      break;
    }
    case 'earnings-scored': {
      name = 'Quarterly reports, scored'; interval = 360;
      add({ id: 'filings', type: 'sec_filings', label: 'New reports', symbols, forms: '10-Q, 10-K', lookback_days: 7 }, 'start');
      add({ id: 'new', type: 'only_new', label: 'Not seen yet' }, 'filings');
      add({ id: 'board', type: 'leaderboard', label: 'Score the filers', symbols, strategy: 'best', limit: 25, skip_empty: true }, 'new');
      add({ id: 'table', type: 'report_table', label: 'Scores', title: 'Fresh quarterly reports, scored', columns: 'rank,symbol,score,grade,reasons,revenue_growth_pct,eps_growth_pct,net_margin_pct,pe_ttm,ttm_through,flags', skip_empty: true }, 'board');
      add({ id: 'memo', type: 'agent', label: 'What changed', require_grounding: true,
        prompt: GROUNDED + LEADERBOARD_GUIDE + 'These companies just filed a quarterly or annual report; their rank is within the whole watchlist. Keep the table at the top. For each: what the trailing-twelve-month figures show now, how it ranks against the watchlist and why, and what to read in the filing next. Research, not a recommendation.' }, 'table');
      deliver('memo', 'Quarterly reports scored {{date}}');
      note = 'Needs SEC_USER_AGENT. The leaderboard ranks your whole watchlist and keeps only the companies that just filed, so nothing is scored on quiet days.';
      break;
    }
    case 'watchlist-health': {
      name = 'Watchlist health check'; interval = 10080;
      add({ id: 'board', type: 'leaderboard', label: 'Score watchlist', symbols, strategy: 'best', limit: 25 }, 'start');
      add({ id: 'laggards', type: 'screen', label: 'Laggards', rule: 'score < 45 or flags contains "below_sma200" or flags contains "unprofitable" or flags contains "shrinking_revenue"', sort_by: 'score' }, 'board');
      add({ id: 'table', type: 'report_table', label: 'Laggards', title: 'Watchlist laggards', columns: 'rank,symbol,score,grade,reasons,flags,from_high_pct,revenue_growth_pct,net_margin_pct', skip_empty: true }, 'laggards');
      add({ id: 'memo', type: 'agent', label: 'Review notes', require_grounding: true,
        prompt: GROUNDED + LEADERBOARD_GUIDE + 'Keep the table at the top. For each laggard: which factor or flag pulled it down, what would repair the thesis, and what would confirm it is broken. Do not tell anyone to sell; this is a review list.' }, 'table');
      deliver('memo', 'Watchlist health check {{date}}');
      note = 'Weekly. Silent when every company on the watchlist scores 45 or more with no warning flags.';
      break;
    }
    case 'insider-cluster': {
      name = 'Insider buying clusters'; interval = 1440;
      add({ id: 'insiders', type: 'insider_activity', label: 'Insider activity', symbols, lookback_days: 30 }, 'start');
      add({ id: 'clusters', type: 'screen', label: 'Clusters & big officer buys', rule: 'cluster_buy or (officer_buy and net_value > 250000)', sort_by: '-largest_buy_value' }, 'insiders');
      add({ id: 'new', type: 'only_new', label: 'Once per two weeks', key_fields: 'symbol', cooldown_hours: 336 }, 'clusters');
      add({ id: 'table', type: 'report_table', label: 'Insider buys', title: 'Insider buying', columns: 'symbol,buyers,buy_count,net_value,largest_buy_value,largest_buyer,officer_buy,latest,url', skip_empty: true }, 'new');
      deliver('table', 'Insider buying {{date}}');
      note = 'Needs SEC_USER_AGENT. Counts open-market purchases only (Form 4 code P). Insiders buy for many reasons; treat a cluster as a reason to read the filings, not as a signal to buy.';
      break;
    }
    case 'macro-regime': {
      name = 'Macro regime shift'; interval = 1440;
      add({ id: 'macro', type: 'macro_regime', label: 'Macro regime' }, 'start');
      add({ id: 'new', type: 'only_new', label: 'Regime changed', key_fields: 'regime', cooldown_hours: 336 }, 'macro');
      add({ id: 'why', type: 'agent', label: 'What changed', skip_empty: true, require_grounding: true,
        prompt: GROUNDED + 'The input is the current macro regime (risk_on, caution or risk_off) with the signals behind it. Explain in under 150 words what changed and what such a regime has usually meant for a long-term investor: keep contributions going, check the plan, avoid reacting to one reading. Do not predict markets.' }, 'new');
      deliver('why', 'Macro regime: {{date}}');
      note = 'Needs the FRED plugin and FRED_API_KEY. Reports when the regime changes and reminds you every two weeks while it holds.';
      break;
    }
    case 'drawdown-guard': {
      name = 'Portfolio drawdown guard'; interval = 60;
      add({ id: 'check', type: 'portfolio_check', label: 'Portfolio check' }, 'start');
      add({ id: 'guard', type: 'screen', label: 'Drawdowns', rule: 'drawdown_from_peak_pct <= -10 or kind == "deep_drawdown"' }, 'check');
      add({ id: 'new', type: 'only_new', label: 'Once per 3 days', key_fields: 'id', cooldown_hours: 72 }, 'guard');
      add({ id: 'table', type: 'report_table', label: 'Drawdown', title: 'Portfolio drawdown', columns: 'kind,total_value,peak_equity,drawdown_from_peak_pct,max_drawdown_1y_pct', skip_empty: true }, 'new');
      deliver('table', 'Portfolio drawdown {{date}}');
      note = 'Administrators only, like the paper portfolio. Checks hourly and alerts at most once every 3 days.';
      break;
    }
    case 'rebalance-check': {
      name = 'Monthly rebalance check'; interval = 43200;
      add({ id: 'check', type: 'portfolio_check', label: 'Portfolio check' }, 'start');
      add({ id: 'moves', type: 'screen', label: 'Suggested moves', rule: 'kind == "suggestion"' }, 'check');
      add({ id: 'table', type: 'report_table', label: 'Moves', title: 'Rebalancing suggestions', columns: 'side,symbol,amount,reason', skip_empty: true }, 'moves');
      add({ id: 'notes', type: 'agent', label: 'Rebalance notes', require_grounding: true,
        prompt: GROUNDED + 'Keep the table at the top. Then, in a short paragraph, explain what the moves would do using the reason column. Do not state drift or allocation figures that are not in the table. Mention that selling can trigger tax and costs. These are suggestions to review, not orders.' }, 'table');
      deliver('notes', 'Rebalance check {{date}}');
      note = 'Administrators only. Needs a saved wealth plan; without one there are no suggestions. Runs every 30 days.';
      break;
    }
    case 'dividend-calendar': {
      name = 'Dividend & earnings calendar'; interval = 10080;
      add({ id: 'events', type: 'corporate_events', label: 'Upcoming events', symbols, limit: 30 }, 'start');
      add({ id: 'new', type: 'only_new', label: 'New events', key_fields: 'id' }, 'events');
      add({ id: 'table', type: 'report_table', label: 'Calendar', title: 'Dividends & earnings, next 30 days', columns: 'date,symbol,kind,amount,currency,ratio,days_until,confirmed', skip_empty: true }, 'new');
      deliver('table', 'Dividend & earnings calendar {{date}}');
      note = 'Needs a provider plugin with the events capability (Yahoo or Finnhub). Each event is reported once.';
      break;
    }
    case 'phase-checkin': {
      name = 'Monthly phase check-in'; interval = 43200;
      add({ id: 'plan', type: 'wealth_plan', label: 'Wealth plan' }, 'start');
      add({ id: 'note', type: 'agent', label: 'Progress note', skip_empty: true, require_grounding: true,
        prompt: GROUNDED + 'Write a short progress note (under 120 words) from the wealth plan in the input: the phase and what it is for, the probability of reaching the goal (say what the percentage means using probability_label), and the next checklist item. Be encouraging and plain; no predictions.' }, 'plan');
      deliver('note', 'Phase check-in {{date}}');
      note = 'Save your plan on the Wealth plan page first; without one this flow stays silent.';
      break;
    }
    case 'earnings-week': {
      name = 'Earnings this week, scored'; interval = 10080;
      add({ id: 'events', type: 'corporate_events', label: 'Earnings dates', symbols, kinds: 'earnings', limit: 7 }, 'start');
      add({ id: 'board', type: 'leaderboard', label: 'Score the reporters', symbols, strategy: 'best', limit: 25, skip_empty: true }, 'events');
      add({ id: 'table', type: 'report_table', label: 'Scores', title: 'Reporting this week, scored', columns: 'rank,symbol,score,grade,reasons,pe_ttm,revenue_growth_pct,eps_growth_pct,flags,ttm_through', skip_empty: true }, 'board');
      add({ id: 'memo', type: 'agent', label: 'What to look for', require_grounding: true,
        prompt: GROUNDED + LEADERBOARD_GUIDE + 'These companies report earnings in the next 7 days. Keep the table at the top. For each: where it ranks in the watchlist and why, and the one figure in the next report that would change that picture. Research, not a recommendation.' }, 'table');
      deliver('memo', 'Earnings this week {{date}}');
      note = 'Needs a provider plugin with the events capability. The leaderboard ranks your whole watchlist and keeps only the companies reporting this week.';
      break;
    }
    case 'strategy-scorecard': {
      name = 'Strategy scorecard'; interval = 43200;
      add({ id: 'backtest', type: 'backtest', label: 'Backtest all strategies', universe: 'us_mega', strategy: 'all', limit: 10, years: 3 }, 'start');
      add({ id: 'table', type: 'report_table', label: 'Scorecard', title: 'Strategy scorecard (US mega caps, 3 years, monthly)', columns: 'strategy_label,cagr_pct,benchmark_cagr_pct,excess_cagr_pct,max_drawdown_pct,sharpe,hit_rate_pct,avg_turnover_pct,from,to', skip_empty: true }, 'backtest');
      add({ id: 'compare', type: 'agent', label: 'Compare strategies', require_grounding: true,
        prompt: GROUNDED + 'Keep the table at the top. Compare the strategies: which returned most, which fell least, which turned over most, and how the best one compares with the benchmark, using the benchmark cagr pct column. Do not calculate new figures. State the caveats in plain words: the universe is today\'s constituents (survivorship bias) and the benchmark is a price index without dividends. Say that past results do not predict future ones.' }, 'table');
      deliver('compare', 'Strategy scorecard {{date}}');
      note = 'Replays six strategies, so the first run takes a while; later runs reuse results for six hours. Change the universe or years on the backtest node.';
      break;
    }
    default:
      return null;
  }
  return { name, intervalMinutes: interval, graph: { version: 2, nodes, edges }, note };
}
