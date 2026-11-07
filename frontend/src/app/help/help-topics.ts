/**
 * The in-app help articles. Pure and import-free so node --test can run it.
 * This is documentation written by people; the agent's answers are separate
 * and labeled as AI-generated wherever they appear.
 */
export interface HelpTopic {
  id: string;
  title: string;
  /** Where in the app this belongs, for grouping. */
  area: 'Start' | 'Research' | 'Automation' | 'Markets' | 'Reference';
  summary: string;
  /** Markdown. Plain text and external links only. */
  body: string;
  keywords?: string;
  /** The user-guide page that covers this topic in full (a file name in docs/guide, without .html). */
  guide?: string;
}

export const DOCS_URL = 'https://doldas.github.io/floweysales/';
/** Address of a topic's page in the user guide, or the guide's front page when it has none. */
export function guideUrl(topic: HelpTopic | undefined): string {
  return topic?.guide ? `${DOCS_URL}guide/${topic.guide}.html` : `${DOCS_URL}guide/`;
}

export const REPO_URL = 'https://github.com/Doldas/floweysales';

export const HELP_TOPICS: HelpTopic[] = [
  {
    id: 'getting-started', title: 'Getting started', area: 'Start',
    summary: 'What FloweySales does and a first research question.',
    body: `FloweySales is a research workspace for investors: market data, an AI research agent, ready-made strategies and automations in one place. It does research. It does not give investment advice and does not place real orders.

**A first session**

1. Pick an instrument with the search box at the top, for example a company name or ticker.
2. Open **Markets** to see its price history and indicators. Pick a chart style above the chart: candles, hollow candles, OHLC bars, Heikin Ashi, line, area, step or baseline. Draw on the chart with the tools above it: a price level, a trend line or a Fibonacci retracement (saved in this browser for each instrument). Merge the provider bars into longer ones with the bar length row (for example 30-minute bars into 1h, 4h, 1D or 1W); shorter lengths are off because the provider does not send them.
3. Open **Research Agent** and ask a question such as "What changed in the latest quarterly report?".
4. Check the sources the agent names before relying on an answer.

**Documentation vs. the agent.** This Help is written documentation. The Research Agent is an AI model: it can be wrong, so its answers are labeled as AI-generated.`,
    keywords: 'start begin first tutorial introduction',
    guide: 'getting-started',
  },
  {
    id: 'research-agent', title: 'Research Agent', area: 'Research',
    summary: 'Ask questions; the agent uses data tools and shows its work.',
    body: `The Research Agent answers questions by calling tools for prices, reports, filings and news, then writing up what it found.

- **Tool calls** are shown in the conversation. Open one to see exactly what was asked and returned.
- **Context.** The agent knows the instrument you have selected in the header.
- **Limits.** Each answer has a budget of reasoning rounds and tool calls, shown at the top of the page. If it runs out it says so.
- **Verify.** An answer is an AI summary. Check dates and sources, especially for numbers.`,
    keywords: 'agent chat ai assistant tools model',
    guide: 'agents',
  },
  {
    id: 'agent-team', title: 'Agent team', area: 'Research',
    summary: 'Several fictional investor characters look at one question.',
    body: `Pick investor characters, ask one question, and read how each of them would approach it. The characters are fictional and work from the same real data.

- Each character can use a different **model profile** prepared by your administrator.
- You can save a team and reuse it.
- The result is research for you to weigh, not advice.`,
    keywords: 'team personas characters investors profile',
    guide: 'agents',
  },
  {
    id: 'flows', title: 'Flows', area: 'Automation',
    summary: 'Chain data steps, agents and approvals into repeatable work.',
    body: `A flow is a saved sequence of steps: fetch data, rank companies, ask an agent, wait for your approval, send an email. Flows are read-only research, so none can place a live trade.

- Start from a **template** or build your own on the canvas.
- A flow can run on a schedule, on an event, or when you press Run.
- Steps that need a person land in the **Inbox**.
- A step's output has a size limit set by the administrator; a step over it fails with a message naming the limit.`,
    keywords: 'automation workflow schedule inbox template node',
    guide: 'flows',
  },
  {
    id: 'markets', title: 'Markets and the price feed', area: 'Markets',
    summary: 'The chart, the status badge and what "Live" does and does not mean.',
    body: `**Markets** shows the selected instrument: price history, indicators and key facts.

**The feed status badge** in the top bar describes four separate things: the connection, how fresh the last update is, what kind of data the provider delivers, and the market session. Open it for details.

- **Live prices** appears only when the provider says its quotes are real time and an update arrived recently.
- **Last update 14:32 UTC** means prices are arriving but are not confirmed real time, for example a bar close.
- **No new prices since …** means the connection is open but nothing recent came in.
- The market session is shown as unknown unless a provider reports it.

All times are UTC.`,
    keywords: 'live status feed stale delayed price chart connection reconnect',
    guide: 'markets',
  },
  {
    id: 'leaderboard', title: 'Leaderboard', area: 'Markets',
    summary: 'A transparent ranking of companies worth a look.',
    body: `The leaderboard scores a list of companies on quarterly reports and a year of prices, then ranks them.

- **Strategy** changes which factors count: quality, value, growth, momentum, safety.
- **Why it ranks** names the biggest contributors for each company.
- **Where the companies come from** is shown above the table. The lists are curated, and membership of an index is not implied.
- A high score means "worth a closer look", not "buy".`,
    keywords: 'ranking score factors universe screen strategy grade',
    guide: 'leaderboard',
  },
  {
    id: 'strategies', title: 'Strategies and backtests', area: 'Markets',
    summary: 'Replay an idea over past daily prices.',
    body: `A backtest replays rules over past daily prices to show what they would have done. It tests an idea; it does not forecast.

- Past results do not predict future results. Costs, taxes and slippage may differ from what is assumed.
- Compare against simply holding the instrument.
- Set slippage and, for leaderboard backtests, a fill delay: a result that vanishes with a little of either was not tradable as measured.
- A walk-forward fit tests chosen numbers on a window they were not chosen on; it still uses history you have already seen.
- Nothing is ordered: strategies here only run on historical data.`,
    keywords: 'backtest fibonacci rules return drawdown',
    guide: 'strategies',
  },
  {
    id: 'world', title: 'World', area: 'Markets',
    summary: 'News and themes, with the publication date on every item.',
    body: `The World view collects headlines and themes. Every item shows the publisher's **publication date in UTC**, or "Date unavailable" when the source does not give one. A date is never guessed. When only a relative age such as "2 days ago" is known, it is labeled approximate.`,
    keywords: 'news headlines date published themes',
    guide: 'markets',
  },
  {
    id: 'wealth-plan', title: 'Wealth plan', area: 'Markets',
    summary: 'Your phase, a target allocation and how likely a goal is.',
    body: `Enter your situation and goal to see which phase of building wealth you are in, what that phase usually needs, a target allocation and a seeded projection of how likely the goal is. It is a plan to discuss, not advice.`,
    keywords: 'plan goal allocation projection retirement',
    guide: 'more-areas',
  },
  {
    id: 'glossary', title: 'Glossary', area: 'Reference',
    summary: 'Terms used across the app.',
    body: `- **Ticker / symbol**: a short code for a listed instrument. The same code can mean different companies on different exchanges.
- **Provider**: the data source for prices and reports. It is shown next to the instrument.
- **Backtest**: replaying rules over past prices.
- **Drawdown**: the fall from a previous high to a later low.
- **P/E**: price divided by earnings per share.
- **RSI**: a momentum indicator from 0 to 100.
- **EMA**: an exponential moving average of price.
- **Stale**: data older than expected for how often it should update.
- **Flow**: a saved, repeatable sequence of research steps.`,
    keywords: 'terms definitions pe rsi ema drawdown ticker provider',
  },
  {
    id: 'troubleshooting', title: 'Troubleshooting and FAQ', area: 'Reference',
    summary: 'When prices, search or the agent do not respond.',
    body: `**Prices say "No new prices".** The connection is open but no update arrived. Open the status badge and press Reconnect. The market may also be closed.

**Search finds nothing.** Check the provider next to the search box. Some tickers exist on one provider only. Press Enter to try the exact symbol.

**The agent says it is out of steps.** It reached its budget for reasoning rounds or tool calls. Ask a narrower question.

**The agent has no model.** An administrator must configure a model endpoint and at least one model profile.

**Is this investment advice?** No. FloweySales is a research aid.`,
    keywords: 'problem error help faq not working empty search reconnect model',
  },
];

export function topicById(id: string | null | undefined): HelpTopic | undefined {
  return HELP_TOPICS.find(t => t.id === id);
}

/** Topics the app links to from its screens; a test checks that each exists. */
export const CONTEXT_TOPICS = ['research-agent', 'agent-team', 'flows', 'markets', 'leaderboard', 'strategies'] as const;

/** Ranks topics by title, summary, keywords and body; best first. An empty query returns all. */
export function searchHelp(query: string, topics: HelpTopic[] = HELP_TOPICS): HelpTopic[] {
  const words = query.toLocaleLowerCase('en-US').split(/[^\p{L}\p{N}_]+/u).filter(Boolean);
  if (!words.length) return topics;
  const scored: { topic: HelpTopic; score: number }[] = [];
  for (const topic of topics) {
    let total = 0;
    for (const w of words) {
      let s = 0;
      if (topic.title.toLowerCase().includes(w)) s += 10;
      if ((topic.keywords ?? '').toLowerCase().includes(w)) s += 5;
      if (topic.summary.toLowerCase().includes(w)) s += 3;
      if (topic.body.toLowerCase().includes(w)) s += 1;
      if (!s) { total = 0; break; }
      total += s;
    }
    if (total) scored.push({ topic, score: total });
  }
  return scored.sort((a, b) => b.score - a.score).map(s => s.topic);
}
