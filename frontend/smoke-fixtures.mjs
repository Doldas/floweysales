// Shared mock backend for the browser smoke tests. Deterministic data only:
// no credentials, no trading and no database. Route handlers return the same
// JSON shapes as the Go engine so the compiled SPA runs against a static server.

const DAY = 86400;
const NOW = Math.floor(Date.UTC(2026, 9, 28, 15, 30) / 1000);

/** Deterministic random walk so screenshots and assertions stay stable. */
function series(start, count, step, seed = 7) {
  let x = seed;
  const rand = () => ((x = (x * 9301 + 49297) % 233280) / 233280);
  let price = start;
  const points = [];
  for (let i = count - 1; i >= 0; i--) {
    const drift = (rand() - 0.45) * start * 0.025;
    const o = price;
    price = Math.max(1, price + drift);
    const h = Math.max(o, price) * (1 + rand() * 0.01);
    const l = Math.min(o, price) * (1 - rand() * 0.01);
    points.push({ t: NOW - i * step, o: +o.toFixed(2), h: +h.toFixed(2), l: +l.toFixed(2), c: +price.toFixed(2), v: Math.round(2e6 + rand() * 3e6) });
  }
  return points;
}

export const graph = {
  version: 2,
  nodes: [
    { id: 'start', type: 'trigger', label: 'Start', x: 60, y: 140 },
    { id: 'agent', type: 'agent', label: 'Analyst', prompt: 'Summarise the filing.', tools: ['get_sec_facts'], max_tool_calls: 4, require_grounding: true, x: 300, y: 140 },
    { id: 'report', type: 'output', label: 'Report', x: 540, y: 140 },
  ],
  edges: [{ from: 'start', to: 'agent' }, { from: 'agent', to: 'report' }],
};

const iso = (secondsAgo) => new Date((NOW - secondsAgo) * 1000).toISOString();

export const flows = [
  { id: 7, name: 'Filing summary', description: 'Accessibility fixture', enabled: true, interval_minutes: 0, event_key: 'filing.published', graph, owner_id: 1, created_at: iso(9 * DAY), updated_at: iso(2 * DAY), active_revision: 3 },
  { id: 8, name: 'Daily risk screen', enabled: true, interval_minutes: 1440, event_key: '', graph, owner_id: 1, created_at: iso(20 * DAY), updated_at: iso(DAY), active_revision: 1 },
  { id: 9, name: 'Earnings preview draft', enabled: false, interval_minutes: 0, event_key: '', graph, owner_id: 1, created_at: iso(3 * DAY), updated_at: iso(3 * 3600) },
];

export const runs = {
  7: [
    { id: 41, flow_id: 7, status: 'SUCCESS', trigger: 'manual', output: 'Revenue grew 12% (get_sec_facts, FY2025).', error: '', started_at: iso(1800), finished_at: iso(1791), duration_ms: 9000, created_at: iso(1800) },
    { id: 40, flow_id: 7, status: 'AWAITING_APPROVAL', trigger: 'event', output: '', error: '', started_at: iso(5400), finished_at: null, duration_ms: null, created_at: iso(5400) },
  ],
  8: [
    { id: 52, flow_id: 8, status: 'FAILED', trigger: 'schedule', output: '', error: 'agent node "Risk": provider timeout after 45s', started_at: iso(3 * 3600), finished_at: iso(3 * 3600 - 46), duration_ms: 46000, failed_node: 'agent', created_at: iso(3 * 3600) },
    { id: 51, flow_id: 8, status: 'SUCCESS', trigger: 'schedule', output: 'No positions above the risk limit.', error: '', started_at: iso(27 * 3600), finished_at: iso(27 * 3600 - 12), duration_ms: 12000, created_at: iso(27 * 3600) },
  ],
  9: [],
};

export const sideEffects = [
  { id: 3, flow_id: 7, flow_name: 'Filing summary', run_id: 40, node_id: 'save', node_label: 'Save report', capability: 'workspace.write', target: '/research/nvda-filing.md', status: 'PENDING', approval_required: true, payload_bytes: 1840, payload_sha256: 'b'.repeat(64), payload_preview: '# NVDA 10-K summary\nRevenue grew 12%…', requested_at: iso(5300) },
];

export const paperApprovals = [
  { client_order_id: 'flow-7-run-41-nvda', request_hash: 'c'.repeat(64), request_payload: JSON.stringify({ client_order_id: 'flow-7-run-41-nvda', symbol: 'NVDA', side: 'BUY', type: 'LIMIT', tif: 'DAY', quantity: 10, limit_price: 118.5 }),
    rationale: 'Filing summary flagged accelerating data-center revenue; limit order below 20-day average.', decision_source: 'flow-run:41', requested_by: 2, created_at: iso(1700), expires_at: iso(-3600 * 20), approved_by: null, rejected_by: null, consumed_order_id: null, status: 'pending' },
];

export function history(symbol = 'NVDA', range = '1M') {
  const step = range === '1D' ? 300 : range === '5D' ? 1800 : DAY;
  const count = range === '1D' ? 78 : range === '5D' ? 65 : range === '3M' ? 63 : range === '1Y' ? 250 : 22;
  return { symbol, provider: 'YAHOO', range, interval: range === '1D' ? '5m' : range === '5D' ? '30m' : '1d', currency: 'USD', points: series(120, count, step), emaConfig: { fastPeriod: 12, slowPeriod: 26 } };
}

export function financials(symbol = 'NVDA') {
  const quarters = ['CY2025 Q4', 'CY2025 Q3', 'CY2025 Q2', 'CY2025 Q1', 'CY2024 Q4', 'CY2024 Q3'];
  return {
    symbol, provider: 'YAHOO', source: 'yahoo',
    reports: quarters.map((period, i) => ({
      period, period_end: ['2025-12-31', '2025-09-30', '2025-06-30', '2025-03-31', '2024-12-31', '2024-09-30'][i], form: '10-Q',
      revenue: 100e6 - i * 7e6, gross_profit: 72e6 - i * 5e6, net_income: 25e6 - i * 2e6, eps: +(2.5 - i * 0.2).toFixed(2), free_cash_flow: 15e6 - i * 1e6, currency: 'USD',
    })),
  };
}

const steps = { steps: [
  { id: 1, node: 'start', label: 'Start', status: 'SUCCESS', output: '', duration_ms: 1, created_at: iso(1800) },
  { id: 2, node: 'agent', label: 'Analyst', status: 'SUCCESS', input: 'Summarise the filing.', output: 'Revenue grew 12%.', duration_ms: 8000, attempts: 1, created_at: iso(1792),
    tool_calls: [
      { attempt: 1, name: 'get_sec_facts', arguments: '{"symbol":"NVDA"}', ok: true, result_bytes: 2048, retrieved_at: iso(1795), sha256: 'a'.repeat(64), sources: ['https://www.sec.gov/cgi-bin/browse-edgar?CIK=0001045810', '0001045810-25-000023'] },
      { attempt: 1, name: 'search_web', arguments: '{}', ok: false, error: 'tool search_web is not granted to this agent', result_bytes: 0 },
    ] },
] };

/**
 * Builds the JSON body for an API path, or undefined when unmocked.
 * `role` is SYS_ADMIN or USER; `permissions` are the USER role's grants.
 */
const PROVIDER_LABELS = { YAHOO: 'Yahoo Finance', BINANCE: 'Binance', AVANZA: 'Avanza', FINNHUB: 'Finnhub' };
function provider(id) {
  return { id, label: PROVIDER_LABELS[id], plugin_id: id.toLowerCase(), plugin_version: '1.0.0', official_api: id !== 'YAHOO' && id !== 'AVANZA',
    search: true, ohlc: true, fundamentals: true, capabilities: ['quote', 'history', 'search', 'financials'], quote_kind: 'ohlc_bar_close',
    requires_key: id === 'FINNHUB', experimental: id === 'AVANZA', enabled: true, min_poll_seconds: 5, cache_ttl_seconds: 60, notes: '' };
}
function pluginManifest(id, version = '1.0.0') {
  const p = provider(id.toUpperCase());
  return { protocol: 1, id, name: p.label, version, description: `${p.label} market data.`, publisher: 'FloweySales',
    homepage: `https://github.com/Doldas/floweysales/tree/main/plugins/${id}`, terms: p.official_api ? undefined : 'Unofficial public interface.',
    provider: { id: p.id, label: p.label, official_api: p.official_api, experimental: p.experimental, capabilities: p.capabilities,
      quote_kind: p.quote_kind, min_poll_seconds: 5, cache_ttl_seconds: 60, notes: 'Not guaranteed real-time.' },
    env: id === 'finnhub' ? [{ name: 'FINNHUB_API_KEY', secret: true, required: true, description: 'Finnhub API token' }] : [] };
}
const pluginOverview = {
  platform: 'linux-amd64', protocol: 1, store_url: 'https://doldas.github.io/floweysales-plugins/index.json', dir: '/plugins',
  installed: [
    { manifest: pluginManifest('yahoo'), installed_at: '2026-10-30T10:00:00Z', source: 'store', sha256: 'a'.repeat(64), running: true, env: [], ready: true, latest_version: '1.1.0', update_available: true, log: ['started'] },
    { manifest: pluginManifest('finnhub'), installed_at: '2026-10-30T10:00:00Z', source: 'store', sha256: 'b'.repeat(64), running: false,
      env: [{ name: 'FINNHUB_API_KEY', secret: true, required: true, description: 'Finnhub API token', configured: false }], ready: false, latest_version: '1.0.0', update_available: false, log: [] },
  ],
  store: { checked_at: '2026-10-31T08:00:00Z', plugins: [
    { manifest: pluginManifest('yahoo', '1.1.0'), versions: ['1.1.0', '1.0.0'], latest_version: '1.1.0', installable: true, installed_version: '1.0.0', update_available: true, released_at: '2026-10-31T07:00:00Z', notes: 'Faster search.' },
    { manifest: pluginManifest('finnhub'), versions: ['1.0.0'], latest_version: '1.0.0', installable: true, installed_version: '1.0.0', update_available: false },
    { manifest: pluginManifest('binance'), versions: ['1.0.0'], latest_version: '1.0.0', installable: true, update_available: false },
    { manifest: pluginManifest('avanza'), versions: ['1.0.0'], latest_version: '1.0.0', installable: true, update_available: false },
  ] },
  trackers: { yahoo: ['NVDA'], finnhub: [] },
};

const boardUniverses = [
  { id: 'us_mega', label: 'US mega caps', description: 'The 25 largest US-listed companies across sectors.' },
  { id: 'us_tech', label: 'US technology', description: 'Large US software, semiconductor and internet companies.' },
  { id: 'us_dividend', label: 'US dividend & value', description: 'Established US dividend payers.' },
  { id: 'se_large', label: 'Sweden large caps', description: 'Stockholm large caps in the style of OMXS30.' },
  { id: 'eu_leaders', label: 'European leaders', description: 'Large European companies.' },
];
const boardStrategies = [
  { id: 'best', label: 'Best buy now', description: 'Balanced: reasonably priced, profitable, growing companies in an uptrend that are not overextended.' },
  { id: 'value', label: 'Cheap & solid', description: 'High earnings yield and low PEG, backed by margins, at a calm entry.' },
  { id: 'growth', label: 'Growth at a fair price', description: 'Fast, accelerating growth with value as a brake.' },
  { id: 'momentum', label: 'Momentum leaders', description: 'The strongest 12-month and 3-month trends.' },
  { id: 'quality', label: 'Quality compounders', description: 'The highest and steadiest margins.' },
  { id: 'dip', label: 'Quality on a dip', description: 'Profitable companies that pulled back while the trend holds.' },
];
/** Same shape as GET /api/market/leaderboard; the order shifts with the strategy. */
function leaderboard(universe, strategy) {
  const base = [
    ['GOOGL', 84, 'A', [82, 88, 74, 79, 70, 66], 172.4, 21.3, 14, 31, -6, 52, 'value: P/E 21.3, PEG 1.1; quality: net margin 28%, FCF margin 22%; momentum: 12-1M +31%, above 200-day', ''],
    ['META', 79, 'A', [70, 90, 85, 76, 52, 44], 588.1, 26.8, 22, 44, -9, 58, 'quality: net margin 34%, FCF margin 30%; growth: revenue +22% y/y, EPS +35%; momentum: 12-1M +44%, above 200-day', ''],
    ['MSFT', 74, 'B', [48, 92, 70, 66, 61, 80], 431.9, 34.1, 15, 18, -4, 49, 'quality: net margin 36%, FCF margin 28%; growth: revenue +15% y/y, EPS +18%; risk: volatility 19%, max drawdown -14%', ''],
    ['JPM', 71, 'B', [90, 60, 55, 70, 64, 72], 214.6, 12.4, 9, 26, -3, 55, 'value: P/E 12.4, PEG 0.9; momentum: 12-1M +26%, above 200-day; risk: volatility 18%, max drawdown -12%', ''],
    ['NVDA', 68, 'B', [36, 96, 98, 94, 18, 22], 121.4, 48.9, 94, 120, -2, 74, 'growth: revenue +94% y/y, EPS +110%; quality: net margin 55%, FCF margin 48%; momentum: 12-1M +120%, above 200-day', 'overbought'],
    ['V', 64, 'B', [44, 86, 52, 58, 72, 88], 282.3, 29.5, 10, 14, -7, 46, 'quality: net margin 54%, FCF margin 52%; risk: volatility 15%, max drawdown -10%', ''],
    ['AMZN', 61, 'C', [52, 58, 66, 72, 46, 50], 186.2, 38.2, 11, 29, -11, 61, 'momentum: 12-1M +29%, above 200-day; growth: revenue +11% y/y, EPS +56%', ''],
    ['XOM', 55, 'C', [80, 52, 18, 40, 62, 60], 112.8, 13.9, -6, 4, -12, 43, 'value: P/E 13.9; timing: 12% below 52w high, RSI 43', 'shrinking_revenue'],
    ['UNH', 47, 'D', [66, 54, 38, 12, 40, 46], 498.7, 18.2, 7, -18, -24, 33, 'value: P/E 18.2, PEG 1.6', 'below_sma200'],
    ['TSLA', 38, 'D', [6, 30, 22, 48, 30, 10], 214.2, 66.1, -3, 8, -31, 39, 'momentum: 3M +12%', 'shrinking_revenue'],
    ['INTC', 24, 'E', [12, 8, 6, 10, 44, 34], 21.6, undefined, -8, -42, -48, 41, '', 'below_sma200, unprofitable, shrinking_revenue'],
  ];
  const factors = ['value', 'quality', 'growth', 'momentum', 'timing', 'risk'];
  const weight = { best: null, value: 0, growth: 2, momentum: 3, quality: 1, dip: 4 }[strategy] ?? null;
  const rows = base.map(([symbol, score, grade, f, price, pe, rev, mom, high, rsi, reasons, flags]) => {
    const s = weight === null ? score : Math.round((score + f[weight]) / 2);
    const row = { symbol, score: s, grade, coverage_pct: 100, price, currency: 'USD', as_of: '2026-10-28', pe_ttm: pe, revenue_growth_pct: rev,
      momentum_12_1_pct: mom, from_high_pct: high, rsi14: rsi, above_sma200: !flags.includes('below_sma200'), reasons, flags, ttm_through: '2026-09-30' };
    factors.forEach((k, i) => { row[k] = f[i]; });
    return row;
  }).sort((a, b) => b.score - a.score).map((r, i) => ({ ...r, rank: i + 1 }));
  return { universe, strategy, provider: 'YAHOO', as_of: '2026-10-28', generated_at: '2026-10-28T15:30:00Z', rows,
    unranked: [{ symbol: 'BRK-B', error: 'fewer than four reported quarters' }], universes: boardUniverses, strategies: boardStrategies };
}

export function mockResponse(url, { role = 'SYS_ADMIN', permissions = ['flows.read'], email = 'analyst@example.test' } = {}) {
  const u = new URL(url);
  const p = u.pathname;
  const q = u.searchParams;
  const admin = role === 'SYS_ADMIN';
  const routes = {
    '/api/auth/me': { enabled: true, user: { id: 1, email, role }, permissions: admin ? [] : permissions },
    '/api/config': { symbol: 'NVDA', provider: 'YAHOO', trackers: [{ symbol: 'NVDA', provider: 'YAHOO' }, { symbol: 'BTCUSDT', provider: 'BINANCE' }, { symbol: 'VOLV-B', provider: 'AVANZA' }] },
    '/api/trackers': { trackers: [{ symbol: 'NVDA', provider: 'YAHOO' }, { symbol: 'BTCUSDT', provider: 'BINANCE' }, { symbol: 'VOLV-B', provider: 'AVANZA' }], max_trackers: 16 },
    '/api/trackers/status': { trackers: [
      { symbol: 'NVDA', provider: 'YAHOO', has_tick: true, last_tick_timestamp: NOW - 20, age_seconds: 20 },
      { symbol: 'BTCUSDT', provider: 'BINANCE', has_tick: true, last_tick_timestamp: NOW - 900, age_seconds: 900 },
      { symbol: 'VOLV-B', provider: 'AVANZA', has_tick: false, last_tick_timestamp: null, age_seconds: null },
    ], checked_at: NOW },
    '/api/history': history(q.get('symbol') ?? 'NVDA', q.get('range') ?? '1M'),
    '/api/financials': financials(q.get('symbol') ?? 'NVDA'),
    '/api/market/leaderboard': leaderboard(q.get('universe') ?? 'custom', q.get('strategy') ?? 'best'),
    '/api/search': [{ symbol: 'NVDA', name: 'NVIDIA Corporation', type: 'EQUITY', exchange: 'NMS' }, { symbol: 'NVDL', name: 'GraniteShares 2x Long NVDA', type: 'ETF', exchange: 'NGM' }],
    '/api/orders': [],
    '/api/positions': { positions: [{ symbol: 'NVDA', quantity: 10, avg_entry: 110, cost_basis: 1100, last_price: 121.4, market_value: 1214, unrealized_pnl: 114, realized_pnl: 0, open_orders: 0, first_fill_at: NOW - 5 * DAY, last_fill_at: NOW - 5 * DAY }],
      summary: { starting_cash: 100000, cash: 98900, buying_power: 98900, equity: 100114, market_value: 1214, unrealized_pnl: 114, realized_pnl: 0, commission_paid: 1, position_count: 1, long_count: 1, short_count: 0, open_order_count: 0 } },
    '/api/forum': [{ id: 1, author: 'Ada', title: 'Data-center demand still accelerating', content: 'Q4 guidance implies another quarter of >10% sequential growth.', symbol: 'NVDA', timestamp: NOW - 7200, upvotes: 4 }],
    '/api/ai/config': { configured: true, base_url: 'http://127.0.0.1:11434/v1', model: 'local-test', max_steps: 12, max_tool_calls: 48, can_configure: admin, has_server_key: false, presets: [], tools: admin ? ['get_quote', 'get_history', 'get_sec_facts', 'calculate_dcf'] : [] },
    '/api/ai/models/available': { models: [{ model: 'local-test', label: 'Local test' }], default_model: 'local-test' },
    '/api/admin/ai/models': { models: [] },
    '/api/admin/flows': { flows },
    '/api/admin/flow-side-effects': { side_effects: q.get('status') && q.get('status') !== 'PENDING' ? [] : sideEffects },
    '/api/paper-approvals': { status: 'pending', approvals: paperApprovals },
    '/api/userspace/files': { files: [
      { path: '/research/nvda-filing.md', content_type: 'text/markdown', size: 1840 },
      { path: '/research/screen.json', content_type: 'application/json', size: 920 },
      { path: '/notes/ideas.md', content_type: 'text/markdown', size: 412 },
    ] },
    '/api/me/tracker-emails': { subscriptions: [{ symbol: 'NVDA', provider: 'YAHOO', golden_cross: true, death_cross: true, stop_loss: false, cooldown_minutes: 30 }] },
    '/api/admin/blueprint-functions': { functions: [] },
    '/api/admin/blueprint-triggers': { triggers: [{ id: 1, event_key: 'filing.published', payload_type: 'text' }] },
    '/api/admin/flow-event-adapters': { adapters: [] },
    '/api/admin/flow-notify-channels': { email: { configured: true }, webhook: { configured: false } },
    '/api/admin/flow-audit': { entries: [] },
    '/api/admin/flow-agent-tools': { tools: [{ name: 'get_sec_facts', description: 'SEC XBRL facts' }, { name: 'search_web', description: 'Web search' }], max_tools: 8, max_tool_calls: 16, default_tool_calls: 6 },
    '/api/admin/integrations': { encryption_ready: true, ai: { has_key: false }, web_search: { provider: 'brave', has_key: false } },
    '/api/admin/users/list': { users: [{ id: 1, email, role, active: true }] },
    '/api/admin/roles': { roles: [{ name: 'SYS_ADMIN', permissions: [] }, { name: 'USER', permissions: ['research.read', 'ai.chat'] }], permissions: ['research.read', 'ai.chat', 'flows.read', 'flows.write', 'flows.run'] },
    '/api/admin/settings/smtp': { configuration: {}, has_password: false },
    '/api/admin/updates': { current: { commit: 'abc123def456', image: 'ghcr.io/doldas/floweysales', tag: 'latest' }, can_apply: false, status: { state: 'idle' } },
    '/api/providers': ['YAHOO', 'BINANCE', 'AVANZA'].map(id => provider(id)),
    '/api/admin/plugins': pluginOverview,
    '/api/admin/plugins/check': pluginOverview,
  };
  const flowMatch = /^\/api\/admin\/flows\/(\d+)(?:\/(.*))?$/.exec(p);
  if (flowMatch) {
    const id = Number(flowMatch[1]);
    const rest = flowMatch[2] ?? '';
    const flow = flows.find(f => f.id === id);
    if (!rest) return flow;
    if (rest === 'runs') return { runs: runs[id] ?? [] };
    if (rest === 'revisions') return { revisions: [] };
    if (rest === 'readiness') return { applies: false, ready: true, checks: [] };
    if (/^runs\/\d+\/snapshot$/.test(rest)) return { graph };
    if (/^runs\/\d+\/steps$/.test(rest)) return steps;
    if (rest === 'run') return { status: 'queued', run_id: 99 };
  }
  return routes[p];
}

/** Server-sent events for one agent answer that calls two tools. */
export const agentStream = [
  { type: 'tool_start', name: 'get_quote', arguments: '{"symbol":"NVDA","provider":"YAHOO"}' },
  { type: 'tool_result', name: 'get_quote', result: '{"symbol":"NVDA","price":118.16,"change_percent":-1.63}' },
  { type: 'tool_start', name: 'get_sec_facts', arguments: '{"symbol":"NVDA"}' },
  { type: 'tool_result', name: 'get_sec_facts', result: '{"revenue_growth_yoy":0.12,"period":"FY2025"}' },
  { type: 'delta', content: '**NVDA** trades at 118.16 USD (−1.63% over 1M).\n\n' },
  { type: 'delta', content: '| Metric | Value | Source |\n|---|---|---|\n| Revenue growth | +12% | get_sec_facts, FY2025 |\n' },
  { type: 'done' },
].map(e => `data: ${JSON.stringify(e)}\n\n`).join('');

/** Installs the mock backend on a Playwright page. Returns the set of unmocked paths. */
export async function installMocks(page, options = {}) {
  const missing = new Set();
  await page.route('**/api/**', route => {
    const url = route.request().url();
    const path = new URL(url).pathname;
    if (path === '/api/prices/stream') return route.fulfill({ status: 204, body: '' });
    if (path === '/api/ai/chat' && route.request().method() === 'POST') {
      return route.fulfill({ status: 200, contentType: 'text/event-stream', body: agentStream });
    }
    const body = mockResponse(url, options);
    if (body === undefined) {
      missing.add(new URL(url).pathname);
      return route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"mock backend unavailable"}' });
    }
    return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(body) });
  });
  return missing;
}
