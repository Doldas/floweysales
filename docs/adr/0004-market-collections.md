# ADR 0004: Market Collections as a core, user-owned domain

- **Status:** Accepted
- **Date:** 2026-11-14

## Context

The only list of instruments in FloweySales was the tracker list
(`tracked_assets`): one list for the whole installation, at most 16 entries,
SYS_ADMIN only, and tied to the polling tracker. Users had no way to group
instruments by theme, sector or intent, and no scope to hand to news, the
Research Agent or Flow Studio. Provider work (#421) is making market data
provider-neutral at the same time.

## Decision

1. **Collections live in core, not in a plugin.** `internal/collections` owns
   the types, validation, the financial rules per kind and the export format.
   Storage is MySQL (changeset 046); `collections.MemoryStore` serves tests.
   Both pass one conformance suite (`collectionstest.Run`).
2. **Every row has one owner.** Store methods take the owner's id and reach
   members only through a collection whose `user_id` matches. Another user's
   collection is indistinguishable from a missing one (404). Sharing, when it
   comes, will be an explicit grant table, not a relaxed query.
3. **Kinds keep their financial meaning.** `watch` has no weights and no
   allocation semantics. `model` carries explicit hypothetical weights that
   never exceed 100%. `linked` (a view of real positions) is reserved and
   refused until a portfolio source with permissions exists. The kind cannot
   change after creation.
4. **Identity stays provider + symbol, with room to grow.** A member is
   identified by `PROVIDER:SYMBOL`, as everywhere else in the app, so two
   listings of one ticker are two members. Exchange, MIC, ISIN, currency,
   country and type are stored as descriptive metadata when known and are
   never used to merge members. A canonical listing identity (Phase 2) will
   link members across providers without rewriting them, and will show
   ambiguity instead of guessing.
5. **Nothing is invented.** Unknown metadata stays empty. ISIN check digits,
   ISO codes and asset types are validated; free-text taxonomy (sector,
   industry, themes) is not stored on members yet: it arrives with the
   profile capability and keeps user overrides separate from provider facts.
6. **No silent migration.** Existing tracker lists, portfolios and
   preferences are untouched. A SYS_ADMIN can copy the tracker list into a
   new collection on request; import never merges into an existing
   collection.

## Consequences

- Agent tools, flow nodes and the news scope call `collections.Service`, so
  validation and ownership checks exist in one place.
- Dashboards must label watch-collection charts as counts, not allocations.
- Cross-provider identity, profile enrichment and taxonomy are separate,
  later changes that build on the stored metadata.
