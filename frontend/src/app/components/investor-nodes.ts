/**
 * Investor nodes and playbooks for Flow Studio: the node catalogue (labels,
 * canvas tags, defaults) and ready-made automations built from it. Mirrors
 * the server's flow_investor.go. Free of Angular so it runs under node --test.
 */

export type InvestorNodeType =
  'watchlist_quotes' | 'technical_scan' | 'fundamentals' | 'sec_filings' | 'news' |
  'screen' | 'only_new' | 'report_table' | 'notify';

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
  { type: 'screen', label: 'Screen (filter rows)', tag: '⧩ SCREEN', admin: false, defaults: { rule: 'change_1d_pct <= -3' },
    description: 'Keeps the rows that match a rule such as rsi14 < 30 and above_sma200, then sorts and limits them.' },
  { type: 'only_new', label: 'Only new since last run', tag: '✓ ONLY NEW', admin: false, defaults: {},
    description: 'Remembers what earlier runs passed on and lets through only new rows, or rows again after a cooldown.' },
  { type: 'report_table', label: 'Report table', tag: '▦ REPORT', admin: false, defaults: { title: 'Watchlist report' },
    description: 'Renders rows as a Markdown table. No rows renders nothing, so later nodes can skip.' },
  { type: 'notify', label: 'Notify (email / chat)', tag: '✉ NOTIFY', admin: true, defaults: { channel: 'email', subject: '{{flow}}: {{count}} item(s) on {{date}}' },
    description: 'Emails the flow owner or posts to the configured Slack, Discord or Teams webhook. Empty input sends nothing.' },
];

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

export interface PlaybookInfo { id: string; label: string; description: string; icon: string; schedule: string }

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
];

interface BuildOptions { admin: boolean; symbols?: string }

const GROUNDED = 'Use only figures that appear in the input data and name the date they are as of. ';

/** Builds a playbook as a disabled, versioned flow graph. */
export function buildPlaybook(id: string, opts: BuildOptions): Playbook | null {
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
    default:
      return null;
  }
  return { name, intervalMinutes: interval, graph: { version: 2, nodes, edges }, note };
}
