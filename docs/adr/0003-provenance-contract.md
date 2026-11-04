# ADR 0003: A shared provenance contract for displayed data

- **Status:** Accepted
- **Date:** 2026-11-04

## Context

FloweySales shows prices, news, filings, calculated indicators and AI text. Each
surface describes its origin in its own way, or not at all. Users cannot tell
what was reported by a provider, what was calculated, and what a model wrote.
Issue #204 asks for one answer to "where does this come from?" on every
displayed datum. Later work (#197 citations, #201 and #207 evidence modes)
needs the same vocabulary.

## Decision

1. **One type, `provenance.Fact`** (`internal/provenance`), with a *kind*
   (`observed`, `calculated`, `assumed`, `estimated`, `ai_generated`) and a
   *status* (`known`, `partial`, `unknown`). Optional fields hold provider,
   whether the provider is official, URL, document id, published / as-of /
   retrieved times, the method for calculated values, and references to the
   facts a value was built from.
2. **Unknown stays explicit.** Constructors never produce `known` without an
   origin a reader can follow, and `Validate` refuses it. A value with no
   recorded source says so; the UI never fills the gap.
3. **A sidecar, not a wrapper.** A response carries `provenance` next to the
   data, so existing JSON contracts and clients stay valid.
4. **Dates are never invented.** A relative age from a search result ("2 days
   ago") is not turned into a publication time. All times are UTC.
5. **AI text is not a source.** `ai_generated` facts are shown as such and
   never count as evidence.
6. **The UI component is `fs-source`.** It opens on click or keyboard, closes
   on Escape or an outside click, renders `unknown` plainly, and only links to
   `http(s)` addresses.
7. **Coverage is measured.** [`docs/provenance-coverage.md`](../provenance-coverage.md)
   lists every surface and its state. Issue #204 stays open until the major
   surfaces are covered.

## Consequences

- Adopting a surface means adding a `provenance` field in its API and an
  `fs-source` in its view; the first adopter is news headlines in World.
- Calculated values will need a method id and version, which pushes indicator
  and ranking code to name its formulas.
- The contract states what was recorded. It does not claim that the source is
  true.
