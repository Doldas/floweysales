# Market Collections

Market Collections are user-owned, reusable sets of instruments: a
Technology watch list, a Medicine research group, a hypothetical model
portfolio. They are stored in FloweySales itself, belong to one user, and are
meant to become a scope for dashboards, news, the Research Agent and Flow
Studio. The design decisions are in
[ADR 0004](adr/0004-market-collections.md).

**Status:** Phase 1 (domain, storage, API), the collection manager and
dashboard of Phase 3 and Phase 2 (profiles, taxonomy, classification,
identity links, with their screens) and Phase 4 (collection news and
read-only Research Agent tools) are implemented. Profiles come from
provider plugins that offer the `profile` capability, headlines from those
that offer `news`. Flow Studio integration is planned.
The user guide page is [Collections](guide/collections.md).

## What exists today

| Piece | Where |
| --- | --- |
| Domain types, validation, kind rules, export format | `internal/collections` |
| Store contract and conformance suite | `collections.Store`, `internal/collections/collectionstest` |
| MySQL store | `internal/storage/market_collections.go`, changeset `046-market-collections.xml` |
| HTTP API | `internal/server/collections.go`, listed in [api-reference.md](api-reference.md#market-collections) |
| Account deletion removes a user's collections and classifications | `identity.Service.DeleteUser` |
| Profiles, taxonomy, classification, identity links | `internal/instruments`, `internal/storage/instrument_intelligence.go`, changeset `047-instrument-intelligence.xml`, `internal/server/instruments_api.go` |
| Collection news and relevance | `instruments.RelateNews`, `internal/server/collection_news.go`, `frontend/src/app/pages/collection-news.ts` |
| Research Agent collection tools | `ai.CollectionSource`, `internal/server/ai_collection_tools.go` |

### Kinds

| Kind | Meaning | Weights |
| --- | --- | --- |
| `watch` (default) | Instruments followed for monitoring and research | None. Charts must show counts, never an allocation. |
| `model` | Hypothetical allocation, no claim of ownership | Optional, percent, above 0 and at most 100 each, never more than 100 in total |
| `linked` | View derived from real portfolio positions | Reserved; refused (422) until a portfolio source with explicit permissions exists |

The kind is fixed at creation.

### Instrument identity

A member is identified by provider and symbol (`YAHOO:VOLV-B.ST`), the same
identity the tracker, charts and search use. The same ticker from two
providers, or on two exchanges, is two members. Name, type, exchange, MIC,
ISIN, currency and country are stored when the caller knows them (for
example from a search result) and are never used to merge members. Codes are
validated: ISIN by its check digit, MIC, currency and country by their ISO
shapes. Unknown values stay empty.

Symbols accept the tracker's character set plus `^` and `=`, so indices
(`^GSPC`) and futures (`GC=F`) can be collected even though the tracker
cannot follow them.

### Limits

200 collections per user, 1,000 instruments per collection, 20 tags per
collection or member, member pages of 100 by default and 500 at most.
Names are unique per user, ignoring case.

### Export format

`GET /api/collections/export` writes, and `POST /api/collections/import`
reads, this document:

```json
{
  "format": "floweysales.collections",
  "version": 1,
  "exported_at": "2026-11-14T12:00:00Z",
  "collections": [
    {
      "name": "Technology",
      "description": "Long-term ideas",
      "kind": "watch",
      "category": "Sector",
      "tags": ["AI"],
      "archived": false,
      "members": [
        {
          "instrument": {"provider": "YAHOO", "symbol": "NVDA", "name": "NVIDIA", "exchange": "NASDAQ", "currency": "USD", "type": "equity"},
          "note": "Data centers",
          "tags": ["chips"]
        }
      ]
    }
  ]
}
```

- Ids are not exported; an import always creates new collections.
- The whole file is validated before anything is written, with the same
  rules as the API (a weight in a watch collection rejects the file).
- `on_conflict=rename` (default) saves "Technology" as "Technology (2)" when
  the name is taken; `on_conflict=skip` leaves it out. Nothing is merged.
- At most 50 collections per file. A future version 2 will be read alongside
  version 1.

## Instrument intelligence

### Profiles

A plugin that declares the `profile` capability describes an instrument as
the provider states it (shape in [plugins.md](plugins.md)). FloweySales asks
only the instrument's own provider: `FINNHUB:NVDA` is described by Finnhub,
`YAHOO:NVDA` by Yahoo, and when Yahoo offers no profiles the answer is
`no_source`, never Finnhub's. A symbol means something only to its own
provider, so another provider's answer could describe a different
instrument. The `instrument_profile` routing domain is listed in the
provider registry with fallback off for that reason.

Profiles are provider facts shared by every user and read with the system
plugin instance, never a user's own key. They are cached in
`instrument_profiles`: an answer for 7 days, "not found" for 1 day. Errors
are not cached, and a failed refresh keeps the profile already stored.
Opening a collection reads the cache only; the provider is asked when one
instrument's profile is opened, or when the user refreshes a collection's
profiles (20 per call, at most once a minute per instrument). Every cached
answer carries its provider, retrieval time, as-of time and source link as a
provenance fact. Output is cleaned before it is stored: provider and symbol
come from the request, malformed codes, non-http links and impossible numbers
are dropped, never repaired.

### Taxonomy and classification

The platform taxonomy is fixed in `internal/instruments/taxonomy.go`:
asset class, sector (11 ids), industry (free text), country (ISO code),
region (derived from the country) and theme (the user's own words).
Provider sector and industry wording is mapped by one word table; wording
it does not know stays unmapped and is shown as the provider wrote it.

Each value of an instrument's classification says where it came from:
the user's override (`user`), the mapped profile (`profile`, with the
provider) or what was stored with the member (`member`). A user's override,
in `instrument_classifications`, is private to them, wins only for the
fields they set and never changes the profile. Themes come only from
overrides. Manual classification works without any profile source.
Account deletion removes a user's overrides.

### Identity links

Members are never merged. `GET /api/collections/{id}/intelligence` lists
how members relate, from the ISIN and MIC stored with each member or, when
missing, from its profile:

| Link | Meaning |
| --- | --- |
| `same_listing` | Same ISIN and MIC: the same listing from two providers |
| `same_security` | Same ISIN on different or unknown venues |
| `same_ticker` | Same ticker, nothing confirms they are the same instrument |
| `different_instruments` | Same ticker, different ISINs |

### Migration from the tracker list

The tracker list stays as it is. A SYS_ADMIN can copy it into a new watch
collection with `POST /api/collections/from-trackers`; the description
records when the copy was made (UTC).

## Collection news

`GET /api/collections/{id}/news?limit=` (owner only; another user's
collection is 404) returns headlines for the members, newest first, undated
last, at most 50.

- **Own provider only.** Each member is asked through its own provider's
  `news` capability (`market.NewsFor`, cleaned by `market.CleanNews`). A
  member whose provider has no news counts as `no_source`; FloweySales never
  substitutes another provider's headlines for the same ticker.
- **Bounded.** At most 25 members per request (the rest count as
  `skipped`), 5 headlines each, 4 providers asked at a time. Headlines are
  cached per instrument key for 10 minutes, shared across users because
  they are provider data, not user data; failures are not cached.
- **Relevance.** `instruments.RelateNews` matches the title and summary
  against each member's symbol and short name (`direct`), its themes and
  industry from the user's classification or the provider profile
  (`thematic`), and otherwise records that the provider filed it under the
  member (`listed`). Every match carries the term, the members and a reason
  string. Terms match on word boundaries; tickers shorter than three
  letters are not matched, and terms of three letters or fewer match
  case-sensitively, so a ticker like `ALL` does not match the word "all".
- **Response.** `items[]` (provider news item plus `relevance` and
  `matches`), `coverage` (`members`, `fetched`, `cached`, `failed`,
  `no_source`, `skipped`), `news_providers` and `retrieved_at` (UTC).

## Research Agent tools

`list_collections`, `get_collection` and `get_collection_news` read
through `ai.CollectionSource`, implemented by the server for the user the
chat acts for (`agentUser`). They are classed as own-data tools
(`toolOwn`), so every user role has them; `get_collection_news` is metered
because it asks providers. A collection is resolved by id or by name among
the caller's collections only. Without a signed-in user, such as a flow's
agent node, the tools refuse, and they are left out of the flow tool
catalog and the MCP server. There is no tool that writes a collection or
places an order.

## Plan

Each phase is a separate, reviewable change with tests.

1. **Foundations (done):** domain, MySQL store, API, ownership checks,
   import/export, tracker copy.
2. **Instrument intelligence (done):** a `profile` plugin capability in
   the SDK (company, fund, commodity and crypto shapes, with source and
   as-of time), routed through the #421 provider registry; a cache with TTL;
   a canonical listing identity that links members across providers and
   shows ambiguity; a platform taxonomy (asset class, sector, industry,
   geography, theme) with provider mappings and user overrides kept apart
   from provider facts. No provider means an explicit "no profile source"
   state; manual classification always works.
3. **User experience (done, except the scope):** collection manager on the
   Collections page (`frontend/src/app/pages/collections-page.ts`), add from
   provider search, the selected instrument or by hand, category and tag
   filters, archive, copy and move, export and import, and a per-collection
   dashboard: composition by count for watch collections and by weight for
   models, with unallocated weight and Unknown values shown and coverage
   labeled. Deferred: the active collection scope moves to phase 4 with news;
   composition by asset class, sector, region and theme, profile
   details with provenance and the classification form are in
   `frontend/src/app/pages/collection-intelligence.ts`; importing the browser-local
   research watchlist into a collection is still open.
4. **News and AI (done):** collection news through the provider-neutral
   news interface, with direct, thematic and listed relevance and a reason
   per match; read-only Research Agent tools for the caller's own
   collections. See [Collection news](#collection-news) and
   [Research Agent tools](#research-agent-tools).
5. **Automation (planned):** Flow Studio nodes that read collection members,
   reusing existing node contracts and budgets.
6. **Quality (planned):** Playwright journeys, caching, mobile layouts and
   the user guide page.
