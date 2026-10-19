# SEC EDGAR filing provenance (#2)

Configure `SEC_USER_AGENT="FloweySales Research you@your-domain.example"` on the Go server and MCP process. SEC explicitly asks for an identifying User-Agent contact and limits fair-use programmatic traffic; this adapter serializes to <=2 requests/second per process and caches the SEC ticker/CIK mapping for one day.

**Use `GET /api/sec/filings?symbol=NVDA` or `get_sec_filings`** for accession, 10-K/10-Q/8-K filings, filing dates, report-period end dates and official archive document URLs. **Use `GET /api/sec/facts?symbol=NVDA` or `get_sec_facts`** for XBRL concepts in SEC filings including revenue, net income, operating income, cash flow, capex, cash, assets, liabilities, diluted EPS and diluted shares when the filed taxonomy exposes them.

Each extracted figure includes: `concept`, `metric`, `value`, `unit`, `start`, `end`, `period_kind` (quarter/annual/instant), `fy`, `fp`, `form`, `accn`, `filed` and EDGAR `filing_url`. A yearly/ytd fact is **not** mislabeled as quarterly; partial-period (6/9 months) rows are excluded. Latest filing wins for matching date/unit, including amended restatements. SEC ticker mappings are not exhaustive and foreign filers (e.g. GIB) may lack 10-K/10-Q XBRL; the client fails clearly.

This is a sourced financial **facts** and filing-index interface, not a complete textual parser of every filing section. Primary filing links allow analysts to read the original narrative text without invented quotes. Annual and interim data are differentiated; no invented figures are filled.

Official docs: https://www.sec.gov/search-filings/edgar-application-programming-interfaces and https://www.sec.gov/about/developer-resources

Development verification: Go test fixtures mock EDGAR ticker index, submission feed, and XBRL companyfacts; they check fiscal-period source data and reject cumulative YTD figures masquerading as quarterly. A live SEC request is not part of CI to respect traffic limits.
