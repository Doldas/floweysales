# Quarterly financial year ranges

The quarterly financial endpoint accepts an **optional, inclusive calendar-year
range of the reported period end date**. A fiscal year label (e.g. FY2027)
can differ from the calendar year of its period_end; the filter uses
`period_end` exclusively, not an inferred fiscal year.

```http
GET /api/financials?symbol=NVDA&provider=YAHOO&from_year=2019&to_year=2025
GET /api/financials?symbol=GIB&provider=YAHOO
```

- Both `from_year` and `to_year` must be supplied together, as integer
  years from 1990 through next calendar year, start <= end, maximum 20 years
  inclusive. Bad requests return 400. When omitted, prior recent-quarter
  behaviour is preserved without new mandatory parameters.
- Yahoo fundamentals-timeseries is queried with `period1` at UTC midnight
  January 1 of `from_year` and `period2` at UTC midnight January 1 of the
  year **after** `to_year`; returned records are also filtered by
  `period_end`. Historical depth depends on the symbol and provider.
- Finnhub's quarterly financials-reported response is parsed/normalized for
  all available periods **before** filtering. The legacy eight-quarter cap
  applies only to requests without year range. This prevents dropping
  older reports and preserves earlier YTD facts for quarterly deltas.
- Avanza (when its plugin is installed) uses the same Yahoo reporting dates through its
  ticker mapping. Binance reports no issuer financials.
- Responses return `from_year`, `to_year` when a range is supplied,
  report rows newest-first, and an explanatory note if nothing is available.
  Empty report sets stay `[]`, not fabricated zero values.
- The endpoint cache key includes provider, canonical symbol and the
  requested year bounds; the latest and historical report caches do not
  overwrite one another.
- `get_quarterly_financials` in the internal AI agent and standalone MCP
  accepts the same optional year fields. The dashboard has a **Recent /
  Year range** selector, with separate From year and To year dropdowns
  that remain usable on phone, tablet and desktop.

Examples for the agent: “Show NVDA quarterly revenue from 2019 to 2023”.
A provider may not have the entire requested history; an empty or partial
result is not automatically filled with estimates. For audited comparisons,
prefer filings/source-specific disclosure and verify differing currencies.
