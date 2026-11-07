# Source trust, dates and ranking

Issue #312. Builds on [multi-provider web search](web-search.md) (#311), [PDF reading](pdf-vision.md) (#310) and [agent evidence](agent-evidence.md). Code: `internal/sourcetrust`, wired into `internal/websearch` (rendering and ranking of results), `internal/sources` (the per-run registry and the audit) and `internal/server/ai_read_page.go` (dates read from pages and PDFs).

The principle: popularity and recency are not accuracy. A source has a **type** (what kind of publisher), an **evidence role** (what it can be evidence for), **dates** that are kept apart, and an **evidence state** (how much of it was actually seen). Ranking is explained with named factors, never a credibility percentage.

## Type and role

Classification is rule based (host lists, investor-relations subdomains and paths, "sponsored" markers). A host no rule names is `unknown`; nothing is guessed and no model is used.

| Type | Role | Examples |
| --- | --- | --- |
| `official_filing` | primary | SEC, SEDAR+, Companies House |
| `exchange_announcement` | primary | NYSE, Nasdaq Nordic, LSE |
| `government_data` | primary | BLS, Fed, ECB, SCB, `.gov` |
| `peer_reviewed` | primary | Nature, PubMed, DOI links |
| `first_party_dataset` | primary | a data provider's own records |
| `company_ir` | self_reported | investor sites, press-release wires |
| `marketing` | self_reported | pages marked sponsored or advertisement |
| `established_news` | secondary | Reuters, Bloomberg, FT |
| `professional_analysis` | opinion | Seeking Alpha, Morningstar |
| `community_social` | sentiment | Reddit, forums, X, Stocktwits |
| `unknown` | unknown | aggregators and everything else |

A company press release is **primary evidence of what the company stated**, not independent verification (`self_reported`). Community posts are never blocked. They are labeled `sentiment`: they may be cited for what people think or say, with their timestamp, and the citation check raises `sentiment_as_fact` when a sentence states a reported financial figure (revenue, EPS, ...) and every source it cites is sentiment or opinion. A sentence that attributes the figure to the discussion ("Redditors argue revenue...") is not flagged.

## Dates

Each date is stored in its own field, in the form the source gave it (time zone and precision kept); comparisons use UTC.

| Field | Meaning |
| --- | --- |
| `published_at` | When the statement was published, with `date_source` (search provider, page JSON-LD, meta tag, time element, filing index, report cover) and `date_confidence` (high, medium, low, none) |
| `modified_at` | When the page says it was last changed. **Never used as freshness.** |
| `effective_at` | Date the data is as of, when stated |
| `retrieved_at` | When this run fetched it. Never a publication date |
| `file_created_at`, `file_modified_at` | PDF information dictionary dates. Weak hints about the file |
| `http_last_modified` | The HTTP header. A weak hint about the file |
| `period` | The financial period the document names (`FY2024`, `Q3 2024`, `H1 2025`) and where it was read (`period_source`) |
| `date_notes` | Why a date was rejected, a conflict, or what is only known |

Rules: a date in the future (more than a day ahead, for time zones) or before 1990 is metadata noise and is ignored with a note. Two publication dates more than a day apart are both reported (`dates conflict`), confidence drops to low, and the better-sourced one stays. A page with only an update date says that when it was first published is unknown. A PDF with only file dates has **no** report date: the report date comes from a labeled line on the cover ("Published: October 30, 2024"), a filing index, or the page's structured data. An unknown date is shown as unknown. A quarter derived from "quarter ended September 30" assumes a calendar quarter and is marked derived. A report for FY2024 published yesterday still has period FY2024.

## Evidence states

| State | Meaning |
| --- | --- |
| `snippet_only` | A search hit: title and snippet. Not a read page, not a verified fact |
| `page_read` | An HTML or text page whose content reached the model |
| `pdf_text` | Text from a PDF's text layer |
| `vision_derived` | Some content was transcribed by the vision model (see [PDF reading](pdf-vision.md)) |
| `access_denied` | The site refused (HTTP 403 or 429). Nothing was read and the source stays a lead |
| `read_failed` | The page could not be read for another reason |

## Ranking

`sourcetrust.DetectIntent` reads the query with fixed rules: **financial_fact** (revenue, EPS, filings, a period), **sentiment** (reddit, "what do people think"), or **general**; "latest/today/current" marks it latest, a stated period or "historical" marks it historical. Each result then gets named factors:

- query relevance (the provider's order),
- source authority for this kind of question (filings first for facts; community first for sentiment),
- reporting-period match (match, related, mismatch: the requested quarter outranks a page modified today),
- freshness from the publication or as-of date with a **category-specific half-life** (filing 400 days, news 21, forum thread 7, ...). Latest questions weigh it most, historical ones almost not at all,
- the user's freshness window for latest questions (older sources are ranked lower and kept as a fallback, never hidden),
- evidence state (read pages gain, denied or failed ones lose).

`trust.why_ranked` lists the factors in words; the search result's `ranking` block gives the profile, the policy sentence, the window and every result **filtered** with the reason (syndicated copy, below the top 5). Syndicated or copied variants (same headline or opening text on different addresses) are merged into one source that lists the others in `also_seen_at`.

**Profiles** (a user setting in the agent's Evidence settings, saved with the evidence policy as `ranking_profile`; the freshness window is `search_window_days`; a SYS_ADMIN has no say):

| Profile | Effect |
| --- | --- |
| `facts_first` (default) | Official and primary sources first; community posts rank high only when the question asks for reaction |
| `balanced` | Community discussion counts more; filings still win for reported figures |
| `sentiment_first` | Recent discussion ranks higher; its type and role never change, and it never supports a reported figure |

The same ranking applies to Brave, Tavily, SearXNG and Google grounding, because it works on the normalized result. Providers fill in what they know (Tavily/SearXNG `published_date`, Brave `page_age`); a relative age such as "2 days ago" is kept as text and not turned into a date.

## Where it applies

- `search_web` (research agent, agent teams, delegated members, flow agent nodes): results carry `trust`; the agent system prompt tells the model to label sentiment, not treat a modified date as publication, and say when a date is unknown or the period differs.
- `read_source` adds a `read_meta` block (dates from the page and the file, period, state) that is merged into the source; a refused read marks the source `access_denied`.
- `get_sec_section` sources are `official_filing`/primary with the filing date from the filing index.
- The Flow Studio web search node returns the same classified and ranked result with the default facts-first policy.
- The Sources panel shows each source as a card: type, role, evidence state, and **Why this source?** (dates, notes, ranking factors, merged copies). Sources can be sorted by relevance or date, and the layout fits a phone.

## Limits

Classification covers listed hosts and a few path patterns; everything else is `unknown`, which is honest rather than wrong. Dates are read from structured data and labeled lines only; no model infers a date. The cross-verification of one value across independent sources is the claim check's conflict detection ([agent evidence](agent-evidence.md)), not a separate state here. Live checks against real providers remain a deployment task.
