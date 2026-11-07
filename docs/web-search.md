# Web search providers

Issue #311. The research agent, agent teams, the world view and the Flow Studio **Web search** node all search through one policy: `internal/websearch` (adapters and router) and `internal/server/web_search.go` (settings, routes and persistence).

## Providers

| Id | Provider | Cost | Notes |
| --- | --- | --- | --- |
| `searxng` | SearXNG, self-hosted | Free (your hosting; upstream engine limits and terms apply) | Keyless. Admin sets the instance address. Needs `json` in `search.formats` of the instance. |
| `tavily` | Tavily Search API | Limited free allowance, then paid (verify with Tavily) | Existing adapter and saved key kept. |
| `brave` | Brave Search API | Paid with a free credit (verify with Brave) | Existing adapter and saved key kept. |
| `google_grounding` | Gemini API, Grounding with Google Search | **Costs money.** Published at the time of writing: 5,000 grounded queries per month free for Gemini 3 and later on eligible paid tiers, then $14 per 1,000 search queries, plus Gemini tokens | Model-mediated, see below. Used only after explicit admin approval. Price, free allowance and token prices are editable estimates. |
| `google_cse` | Google Custom Search JSON API | Closed to new customers; existing customers must migrate by January 1, 2027 | Legacy, existing customers only. Shown with a warning. |

Not done: no scraping of Google result pages, no CAPTCHA bypass. More providers can be added by implementing `websearch.Adapter` and a catalog entry.

## Result shape

Every caller gets the same JSON (`websearch.Render`). The Flow Studio fields (`provider`, `query`, `results[].title|url|description`) are unchanged; `snippet` is the same text under the name the agent's source registry reads.

- `result_type`: `web_results` or `grounded_answer`.
- `snippet_only: true` and a `note`: a search hit is a title and snippet, not a page that was read and not a verified fact. A source only becomes "read" after `read_source` succeeds.
- `results[].type`: `web_result` or `grounded_source`, with `provider` on each.
- `fallback` is present when more than one provider was involved: each provider tried or skipped, with the reason (never the query).
- `usage`: searches, prompt and output tokens, and the estimated cost of this search.
- Duplicate URLs (ignoring fragments, `utm_*`, `gclid`, `fbclid`, trailing slashes) are removed; only public `https` addresses are shown; at most 5 results; output is capped at 7,000 bytes by shortening, never by failing.

### Google grounding

Gemini receives the query with the `google_search` tool and `x-goog-api-key` as a header. What comes back is passed on as two distinct things:

- `grounded_answer.text`: Gemini's own text. It is model-generated, labeled as such, and never presented as a quotation.
- `results[]` of type `grounded_source`: the pages in `groundingMetadata.groundingChunks`, deduplicated, with the title Google gave and **no snippet**. `grounded_answer.supports` maps passages of the answer to those sources, and `search_queries` lists the searches Google ran. The addresses are Google redirect URLs; they are not fetched here.

Usage is tracked as upstream reports it: billed searches are the number of `webSearchQueries` (a prompt can run several), model tokens come from `usageMetadata` (thinking tokens count as output). Cost is estimated as billable searches (beyond the monthly free allowance) times the price per 1,000, plus tokens times the token prices you enter. If you enter no token prices, the model charge is not estimated.

## Policy

Set by a SYS_ADMIN under **Settings → API integrations → Web search**:

- **Manual** uses exactly one provider and never falls back.
- **Automatic** tries enabled, configured providers in the saved order, at most `max_attempts` (1 to 5, default 3) per search, with `max_retries` (0 to 2, default 1) extra tries after a timeout or 5xx. An empty answer moves on to the next provider; if all are empty the empty answer is returned.
- A provider that costs money (`google_grounding`, `google_cse`) is skipped unless it is **approved** and has a daily or monthly ceiling. Before each call the router checks the ceilings with a worst-case estimate (3 searches for grounding) and refuses when the estimate would exceed the daily, monthly or per-run budget. Free allowance searches are not charged against ceilings. If the usage ledger cannot be read, a priced provider is not used.
- **Per-run budget**: one agent run (`ai.Run`) or one flow run (nested flows included) shares a spend counter.
- **Circuit breaker**: HTTP 401/403 pauses a provider for 10 minutes, 429 backs off 30 seconds doubling up to 15 minutes, three consecutive timeouts or server errors pause it for a minute or more. A paused provider is skipped and reported. Saving a provider's settings or a successful test closes its circuit.
- Every decision (answered, empty, failed, skipped and why) is kept in memory (last 100) and shown under **Recent decisions**. It never contains the query.

Errors name the provider and the cause (`credentials rejected (HTTP 401)`, `quota or rate limit reached (HTTP 429)`, `the request timed out`, `no API key is configured`) and never contain the provider body, the query or a key. With nothing configured the error says a SYS_ADMIN can add a provider.

## Security

- Settings are encrypted with the server key (`FLOWEYSALES_SECRETS_KEY`) in `api_integration_secrets` (`ws_<provider>` rows and `ws_policy`). The browser never receives a key; a blank key field on save keeps the saved one. Ordinary users get `GET /api/web-search/status` (available, mode, per-provider state) and nothing else.
- Admin changes go through the existing audited `/api/admin/` path.
- SearXNG address: set only by a SYS_ADMIN, so private addresses such as `http://searxng:8080` are allowed. Credentials, queries and fragments in the URL, cloud metadata names and link-local, unspecified and multicast addresses are refused, and the dialer refuses them again after DNS resolution. Redirects are never followed (a redirect is an error), no proxy is used, and a result URL is never fetched by search code or sent a credential. Fetching untrusted result pages still goes through `read_source` with the public-URL check and DNS pinning.
- The Google key travels in a header (Gemini) or the query string Google requires (legacy Custom Search); error text never includes the URL.

## API

| Method | Path | Who |
| --- | --- | --- |
| GET | `/api/web-search/status` | any signed-in user |
| GET | `/api/admin/web-search` | SYS_ADMIN: catalog, providers (no keys), policy, usage today and this month, recent decisions |
| PUT / DELETE | `/api/admin/web-search/providers/{id}` | SYS_ADMIN: save or remove one provider (blank `api_key` keeps the saved key) |
| POST | `/api/admin/web-search/providers/{id}/test` | SYS_ADMIN; a paid provider needs `{"confirm_paid":true}` because the test is billed |
| PUT | `/api/admin/web-search/policy` | SYS_ADMIN |
| PUT / DELETE | `/api/admin/integrations/web_search` | Unchanged contract for Brave or Tavily |

## Compatibility and migration

An existing install's single Brave or Tavily key (`web_search` row) is read at startup and becomes an enabled provider of that name, first in the order, so behavior does not change. Saving a Brave or Tavily key still writes the legacy row too, so a rollback keeps working. `BRAVE_SEARCH_API_KEY` in the environment is still used by the research agent when no provider is configured here. Liquibase `045-web-search-usage` adds `web_search_usage` (counts and estimated cost per provider and UTC day; no queries, results or credentials).

## Status

Done: all of the above, with Go tests for each adapter (httptest fakes: success, empty, malformed, 401/403/429/5xx, timeout, redirect, oversized, key leakage), the router (order, fallback, retries, bounds, circuit breaker, approval, daily/monthly/per-run budgets, ledger failure, manual mode, test connection), rendering (dedupe, filtering, grounded type, length) and endpoint validation; frontend unit tests; Playwright desktop and mobile tests for configuration and source attribution; the MySQL-backed storage test runs in CI only.

Remaining: live checks against real SearXNG, Tavily, Brave and Google accounts (nothing here calls a real provider in tests), confirming Google's prices and eligibility against the current pricing page when deploying, and a monthly/daily spending summary beyond the per-provider usage lines.
