# Plan 4: Leaderboard

**Issues:** #196, #208, #209, and what remains of #6.

**Why one plan:** one panel answers the three questions from #196 and #208 ("Why is this company here? Where do
its numbers come from? Why is it ranked here?"). #209 adds markets, and would be misleading without that panel.

#196's "Additional requirement" and #208 are the same work. Build it once under #208, and close both together.

## What happens today

- **The server already knows the method.** `leaderboardProvenance` returns factors, metrics, directions, weights,
  caps, missing-factor value, version and hashes. The UI shows it only as one line of hashes under the table, and
  otherwise relies on `title` tooltips (`frontend/src/app/components/leaderboard.ts`).
- **The per-company breakdown is partial.** Rows carry factor scores, reasons, flags, confidence and alternatives,
  but not each metric's percentile.
- **Universes are hand-written in Go** (`leaderboardUniverses`, `internal/server/leaderboard.go:46`). The honest
  answer to "where do these come from?" today is "a list in the source code". There is no date, source or rule.
  Descriptions such as "the 25 largest US-listed companies" and "in the style of OMXS30" cannot be verified from
  the code and may have drifted.
- **Reference-portfolio universes are the exception:** they carry filer, period and source.
- **Coverage is North America and Europe.** Tickers are Yahoo-style, in one list per universe.
- **Identity is a bare string.** `SearchResult.Exchange` is free text, and the SDK has no MIC or ISIN.
- **There is no FX source** anywhere in `internal/`. Prices are shown in local currency.
- Scores are percentiles within the universe, so widening the universe changes what a score means.
- `leaderboardMaxCache = 16`, and a cold universe is slow, so global scope needs thought.

## Work packages

### WP1: Explain the ranking (M, `feature/leaderboard-method`) — #196

No new data needed for most of this.

- New `fs-leaderboard-method` between controls and table: in plain words what the strategy looks for; factor
  weights as a small bar list; for each factor, its metrics and direction; caps; how missing data counts; the
  universe; price dates; coverage. Open by default the first time, then remembers its state. Refreshes with the
  selected strategy and universe.
- Expandable formula and a worked example computed from the first row.
- Row expansion, a real button that works on touch and keyboard: raw value, percentile and weight per metric,
  giving the score step by step. Server returns `breakdown` per row, gated by `?explain=1` to keep responses small.
- Tooltips stay as a supplement.

**Tests:** Go test that factor contributions sum to the score, including caps and missing factors. This also guards
the method's honesty. Spec for the panel's contents per strategy, and a keyboard check.

### WP2: Where the companies come from (L, `feature/leaderboard-universe-provenance`) — #208, #196, #6

**Data.** Move universes into an embedded, versioned `internal/server/universes/*.json`:

```json
{ "id": "se_large", "version": "2026-11-1", "region": "europe", "countries": ["SE"],
  "source": { "kind": "curated", "name": "Curated by FloweySales", "url": null, "as_of": "2026-11-03",
              "rule": "Hand-picked Stockholm large caps, not the official index", "limitations": "…" },
  "members": [ { "symbol": "VOLV-B.ST", "mic": "XSTO", "name": "Volvo B", "isin": null } ] }
```

- `source.kind`: `curated`, `index_snapshot`, `disclosure`, `user_watchlist`, `screener`, `ai_suggested`.
- **Correct the wording first** (owner decision 4): lists are labeled curated and no longer imply index membership
  or a ranking we have not verified. Set `as_of` to the date someone actually reviews a list.
- Only record an ISIN or URL that has been verified. Leave it null otherwise; the UI says "not recorded".

**Pipeline.** Each response gains a `selection` block, using the provenance contract from Plan 3 WP2:

```
initial 30 -> no price data 1 -> no fundamentals 2 -> ranked 27
```

with `excluded: [{symbol, reason}]` from the existing unranked rows. No exclusion is silent. Filter and market
changes rebuild it, because it is computed per request.

**UI.** A "Where do these companies come from?" panel beside the method panel. Each row's expansion has three
separate sections: *Why it's here* (discovery), *Where the numbers come from* (provider, as-of, statement period
per metric) and *Why it ranks here* (from WP1). Export as JSON and CSV.

**Tests (the issue's list):** merging providers; the same ticker on two exchanges stays separate; unknown provenance
is flagged rather than omitted; a stale dataset warns; filters update disclosures. Also a test that no universe has a
`known` source without a name and date.

### WP3: Global coverage (XL, several PRs) — #209

**Step 1: find out what really works (spike, no product code).** Write `cmd/universe-coverage`, which runs candidates
through `leaderboardMetrics` and reports per market the share of symbols with prices and with enough quarterly
reports, the age of the data, and the currency. Candidates to try: Tokyo (`.T`), Hong Kong (`.HK`), India (`.NS`),
Korea (`.KS`), Johannesburg (`.JO`), Lagos, Nairobi, Cairo, São Paulo (`.SA`), Sydney (`.AX`), Toronto (`.TO`).

**To verify:** none of this was tested. Provider behavior lives in `Doldas/floweysales-plugins`, outside what was read
here. Yahoo is unofficial, so check its terms too (`CONTRIBUTING.md` safety rules). **African coverage is the
real risk:** fundamentals may be thin, and if so the honest outcome is prices-only ranking with low confidence, or
stating that a market does not qualify. Set the bar before seeing results: at least 80% of members with a year of prices
and 4 quarters of reports for full ranking.

**Step 2: scope model.** Region, country and exchange become filters over universe metadata. Global is the union of
qualifying universes, and several can be combined. Regions without a qualifying market are shown as "No supported market
yet", never hidden and never padded.

**Step 3: identity.** Use `InstrumentRef` (Plan 6 WP3). Rows key on provider + symbol. Dual listings: both are kept by
default and flagged, with an option to show one per ISIN only when ISINs are known.

**Step 4: comparability, the hardest part.**
- Comparison mode as an explicit choice: *Within region* (percentile per region; the default for global scope) or *Across all*.
- Most metrics are ratios, so currency affects them little. Returns are the exception: momentum in a weak currency looks
  better than it is. For Across-all, add an FX source with a named provider and date, or show a warning that returns are in
  local currency. Never convert with an undated rate.
- Mixed accounting standards and fiscal periods are disclosed, with fiscal period shown per row.
- Sparse markets: existing `coverage_pct` and `confidence` apply, and low coverage cannot produce a high grade. Data that a
  region lacks is not counted against the company at the 40 used for gaps within a covered market: report the factor as
  not comparable instead, and document this in a new methodology version.
- Show counts per region and exchange, with data freshness and provider.

**Step 5: performance.** A global ranking might be 150+ symbols. Compute per-universe metrics and combine, so the existing
per-universe cache still works, and raise `leaderboardMaxCache`. First load shows progress (Plan 7). Flow nodes keep their
row limit.

**Tests:** multi-region ranking; different currencies; sparse data; date and time-zone differences at market boundaries;
no merge of colliding tickers. **Minimum delivery:** one Asian and one African market, validated end to end, with coverage disclosed.

### WP4: Bias measurement (M, `feature/ranking-bias`) — #6

- Today's backtest warns about survivorship, but does not measure it. Add a comparison to an equal-weight universe and
  to random selections (seeded, with results reported as a distribution).
- Point-in-time universes: use dated WP2 versions when they exist. Otherwise keep the warning and say so.
- Investor objective and constraints as a ranking input, using the existing investor profile. **To verify:** which fields
  the profile holds.
- Then review #6's acceptance and close it.

## Acceptance mapping

| Issue | Covered by |
| --- | --- |
| #196 method, breakdown, dates | WP1 |
| #196 additional requirement, #208 | WP2 |
| #209 | WP3 |
| #6 | WP2 (versioned universes), WP4 |

## Out of scope

Paid index constituent feeds, until a license decision. User-adjustable weights beyond existing strategies, which the issue
marks as "consider": `strategy-builder` could support it later.
