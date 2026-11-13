# Provider coupling audit (#421)

Where FloweySales still depends on a named provider, which capabilities already go through installed plugins, and what the first routing slice changes. Code references are to `main` at the time of the audit; counts of files that merely mention a name (tests, docs, labels) are not coupling.

## Capability matrix

| Domain | Served today by | How a provider is chosen | Provider-specific coupling in business logic | Stage |
| --- | --- | --- | --- | --- |
| Market quote, history, search, financials, research | Installed plugins through `market.Backend` (`internal/plugins/backend.go`) | The caller passes `provider`; when absent the single process default `DEFAULT_MARKET_PROVIDER` (`main.go:88`, `internal/server/server.go:248`) is used. Leaderboard adds a manual `fallback_provider`. | a Yahoo quote link is built when the provider is YAHOO (`internal/server/leaderboard_quality.go:35`); Yahoo-style ticker suffixes are the app's symbol format (`leaderboard.go:68`, `market/session.go:68`, `indexsource/csv.go`). | 1 (this slice routes quotes) |
| News, corporate events | Plugins with the `news` / `events` capability (`internal/market/news.go`, `events.go`) | A named provider wins. Otherwise the saved `news` / `events` policy (primary and fallback, `newsFor` and `eventsFor` in `internal/server/provider_routing.go`); with no policy, the first installed provider that offers it. | None by name. | done |
| Forum posts | Plugins with the `forum` capability | Every installed provider is asked and the posts are merged; `include_providers=false` turns them all off (`internal/server/forum.go`). | None by name. Not routed: this domain merges sources, which is enrichment rather than fallback. | 2 |
| Web search | Built-in adapters Brave, Tavily, SearXNG, Google grounding (`internal/websearch`) | Own router and policy (auto order or manual pick, attempts, retries, budgets, circuit breaker) stored sealed as `ws_policy` (`internal/server/web_search.go`). It publishes itself to the shared registry (`internal/websearch/family.go`): its providers are listed as managed sources, the registry shows the order it will use, and every search reports the shared `Outcome` as `routing`. | Provider names appear in `internal/ai/websearch.go`, `internal/server/world.go`, `internal/sources/sources.go`, `api_integration_settings.go`. | done for the registry and `Outcome`; the names remain |
| Index membership | User-supplied CSV or the user's own FMP key (`internal/indexsource`, #258) | The user chooses per index; adapter registry with licence notices. | `FMPIndexes` maps registry ids to FMP endpoints. | 3 (listed in the shared registry by this slice) |
| SEC filings and XBRL | `internal/sec` calls EDGAR directly | Not pluggable. | A privileged internal integration, kept internal on purpose (public regulatory source, no credentials); it is shown in the registry so it is visible, not routable. | internal |
| Macro series | Plugins that declare the `macro` capability, whose `history` answers the regime's series | The saved `macro` policy; with none, the first enabled provider that declares it. One provider answers the whole regime, so series are never combined (`runMacroNode`, `internal/server/flow_investor_macro.go`). | The series ids are FRED's and are the contract (`docs/plugins.md`). `legacyMacroProviders` keeps the released FRED plugin working until it declares `macro`. Help texts in Flow Studio still name FRED. | done, except that shim and the texts |
| FX | None integrated; mixed currencies are disclosed, never converted | n/a | n/a | 3 |
| AI text and vision | One OpenAI-compatible endpoint per model profile (`internal/ai`, `internal/vision`) | Profiles chosen by users; no capability routing. | Endpoint-agnostic already. | 4 |
| Notifications | SMTP (`internal/mailer`) and webhooks (`internal/server/flow_notify.go`) | Configured by SYS_ADMIN. | None. | 4 |

## What already works through plugins

Yahoo, Finnhub, Avanza, Binance, Stooq, CoinGecko, FRED and ECB are plugins (ADR 0002); the manifest declares capabilities (`quote`, `history`, `search`, `financials`, `research`, `forum`, `news`, `events`), required environment variables and bounds on the app version. `GET /api/providers` lists them. What is missing is one place that also lists the built-in and user-scoped sources, and a policy that says which source serves a domain.

## The first slice

`internal/routing` is a small, domain-neutral layer:

- **Sources** (`Source`): one record per way to obtain data, whether an installed plugin, a built-in adapter or something a user supplied. It states its domains, scope (system or user, with the owner), whether credentials are needed and present, coverage, and restrictions.
- **Policy per domain** (`Policy`): a primary, an ordered fallback list, and a `no_fallback` switch for domains whose semantics or licence forbid substitution. Validated against the sources that actually serve the domain.
- **Plan and execution** (`Plan`, `Run`): user-scoped sources are visible only to their owner, disabled and unconfigured sources are skipped, a workflow can pin one source (no fallback), an open circuit breaker skips a failing source for a cool-down. Every result carries an `Outcome` with requested and actual source, each attempt and why it failed, and limitations. Fallback is never silent and an error that says "this must not be substituted" stops the chain.
- **Own keys** (`internal/server/provider_own_keys.go`, `internal/plugins/user_instances.go`): an administrator allows them per plugin; a user saves values for that plugin's declared secret variables, sealed under a kind that names the user. The user's source (`user:<user>:<provider>`, so one user's failing key never pauses another's) comes first in `MakePlan`; with no policy saved, the caller's default follows it as a disclosed fallback. It runs in a plugin process of its own that never sees the system's secret values; processes are bounded and stopped when idle. Requests that name no user (flows, the agent, the shared world view) use the system chain only.
- **Agent prompt**: the prompt and tool descriptions name no provider. When an administrator saved a policy for a market domain whose tools take a provider (quotes, history, search, financials, research), `ProviderGuidance` adds one line with that order, computed by `MakePlan` so only sources that can answer now appear. Only provider ids reach the prompt, never text a plugin wrote.
- **Web search** keeps its own router, because retries and budgets go beyond a shared policy. It is published to the registry as a family of managed sources and cannot be set through the shared policy endpoints.
- **Consumers**: `GET /api/quote`, `GET /api/events`, the news and events nodes in Flow Studio, the agent's corporate events tool, the world view and Today use the policy of their domain when the caller names no provider. With no policy configured the behavior is unchanged. A saved policy fails closed: when its sources are gone or fail, no other installed provider answers. `GET /api/events` and the agent tool return the `Outcome`; flow rows and headlines name the provider that answered.
- **Settings**: SYS_ADMIN edits the policy per domain in Settings → Providers; everyone signed in can read which sources exist without any secret.

Not in this slice and not claimed: enrichment (merging several sources), per-region routing, per-source budgets. Own keys came in a later slice (below).

## Implementation notes (this slice)

- `internal/routing`: `Source`, `Policy`, `MakePlan`, `Run`, `Health` and `Outcome`, with unit tests for validation, user isolation, pinning, fallback with an audit trail, no-substitute errors, bounded attempts, the circuit breaker and benign errors.
- `internal/server/provider_routing.go`: builds the registry from installed plugins, the web search catalog, the index adapters and SEC EDGAR; stores the policy sealed under the `provider_routing` secret kind (the same store as the web search policy; the policy itself is not secret, the store is simply the existing encrypted settings table); serves `GET /api/providers/routing` and the SYS_ADMIN `PUT`/`DELETE /api/admin/providers/routing/{domain}`; routes `GET /api/quote` when no provider is named. The result gains a `routing` object: requested and actual source, every attempt with its reason, skipped sources and limitations.
- A plugin's market quote is read from price bars, so a provider that offers history serves the `market_quote` domain.
- Frontend: Settings → Providers (`fse-provider-routing`), with Playwright and axe coverage in both themes.

## Next steps, in order

1. FRED 1.1.0 (with `macro`) is released; remove `legacyMacroProviders` once installations have updated.
2. An SDK capability for provider-side coverage (regions, exchanges) so a policy can route per region.
