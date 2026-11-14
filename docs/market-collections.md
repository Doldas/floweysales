# Market Collections

Market Collections are user-owned, reusable sets of instruments: a
Technology watch list, a Medicine research group, a hypothetical model
portfolio. They are stored in FloweySales itself, belong to one user, and are
meant to become a scope for dashboards, news, the Research Agent and Flow
Studio. The design decisions are in
[ADR 0004](adr/0004-market-collections.md).

**Status:** Phase 1 (domain, storage, API) is implemented. The user
interface, profile enrichment, taxonomy, news, agent and flow integrations
are planned and are listed below as planned.

## What exists today

| Piece | Where |
| --- | --- |
| Domain types, validation, kind rules, export format | `internal/collections` |
| Store contract and conformance suite | `collections.Store`, `internal/collections/collectionstest` |
| MySQL store | `internal/storage/market_collections.go`, changeset `046-market-collections.xml` |
| HTTP API | `internal/server/collections.go`, listed in [api-reference.md](api-reference.md#market-collections) |
| Account deletion removes a user's collections | `identity.Service.DeleteUser` |

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

### Migration from the tracker list

The tracker list stays as it is. A SYS_ADMIN can copy it into a new watch
collection with `POST /api/collections/from-trackers`; the description
records when the copy was made (UTC).

## Plan

Each phase is a separate, reviewable change with tests.

1. **Foundations (done):** domain, MySQL store, API, ownership checks,
   import/export, tracker copy.
2. **Instrument intelligence (planned):** a `profile` plugin capability in
   the SDK (company, fund, commodity and crypto shapes, with source and
   as-of time), routed through the #421 provider registry; a cache with TTL;
   a canonical listing identity that links members across providers and
   shows ambiguity; a platform taxonomy (asset class, sector, industry,
   geography, theme) with provider mappings and user overrides kept apart
   from provider facts. No provider means an explicit "no profile source"
   state; manual classification always works.
3. **User experience (planned):** collection manager, add from global
   search, category and tag filters, active collection scope, per-collection
   dashboard (composition by count for watch collections, by weight for
   models; coverage and freshness labeled).
4. **News and AI (planned):** collection news scope through the
   provider-neutral news interface with direct versus thematic relevance
   and reasons; permission-checked Research Agent tools that read only the
   caller's collections and never trade.
5. **Automation (planned):** Flow Studio nodes that read collection members,
   reusing existing node contracts and budgets.
6. **Quality (planned):** Playwright journeys, caching, mobile layouts and
   the user guide page.
