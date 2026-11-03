# Plans for the open issues (November 2026)

Solution proposals and implementation plans for all 22 open issues, written
after reading the code on 2026-11-03 and updated the same day with the owner's
decisions. Related issues share one plan. Each plan
says what the code does today, what to build, in which order and how to test it.

Nothing here is implemented, and the code was read, not run. Facts that could
not be checked from this repository are marked **to verify**.

## Plans

| # | Plan | Issues | Size |
| --- | --- | --- | --- |
| 1 | [Flow output capacity](1-flow-output-capacity.md) | #193 | M |
| 2 | [Agent runtime: status, tool traces, teams](2-agent-runtime.md) | #194, #198, #202, #199 | L |
| 3 | [Evidence and provenance](3-evidence-and-provenance.md) | #204, #200, #197, #201, #207, #5 | XL |
| 4 | [Leaderboard: transparent and global](4-leaderboard.md) | #196, #208, #209, #6 | XL |
| 5 | [Strategy validation](5-strategy-validation.md) | #195 | L |
| 6 | [Market context: status and instrument](6-market-context.md) | #210, #211 | M |
| 7 | [Loading states](7-loading-states.md) | #203 | M |
| 8 | [Help, docs and brand](8-help-docs-brand.md) | #205, #206 | L |
| 9 | [License, language and time](9-license-language-time.md) | #205, #206, new issue | M |

#8 (broker execution) gets no implementation plan on purpose. The issue says
"not approved for implementation", and nothing in these plans changes that gate.
It stays open as a research issue.

## Recommended order

Sizes are relative: S is a small PR, M a PR or two, L several PRs, XL a program.

**1. Fix what is broken now (small, independent)**
- Plan 9 WP1: add the MIT license
- Plan 1 WP1: raise the limit that fails flows today (#193)
- Plan 2 WP1: stale error banner (#194)
- Plan 3 WP1: news dates, including a sort bug found on the way (#200)
- Plan 2 WP2: team tool-call inspection, since the data already reaches the browser (#198)

**2. Make what exists understandable**
- Plan 9 WP2: UTC and en-US formatting, before more screens add dates
- Plan 2 WP3: tools for every user (#197, #199, #201 and #207 rely on it)
- Plan 6: Live status and header instrument (#210, #211)
- Plan 4 WP1: show the methodology the server already returns (#196)
- Plan 7 WP1: shared loading components (#203)

**3. Lay the shared foundations**
- Plan 3 WP2: provenance contract (#204)
- Plan 6 WP3: `InstrumentRef`, used by #208, #209 and #211
- Plan 8 WP1: one source for brand and design tokens (#205, #206)

**4. Build on them**
- Plan 3 WP3 to WP5: citations, then evidence modes (#197, #201, #207)
- Plan 4 WP2 and WP3: universe provenance, then global coverage (#208, #209)
- Plan 5: strategy validation (#195)
- Plan 2 WP4 and WP5: model profiles and saved teams, then team delegation (#202, #199)
- Plan 9 WP3 with Plan 8 WP2: English site and docs (#206)
- Plan 8 WP3: in-app help and About (#205)

## How the plans depend on each other

| Needs | From | Why |
| --- | --- | --- |
| #199 delegation | #198, #197, tools for all | Nested traces need call ids; team citations need the source registry; members need tools |
| #201, #207 for normal users | tools for all (Plan 2 WP3) | Without tools an evidence mode could only abstain |
| #202 per-member profiles | model profiles and saved teams (Plan 2 WP4) | Teams are not persisted today, so there is nothing to save a choice on |
| #201, #207 evidence modes | #197 | The modes enforce what the citation audit measures |
| #197, #208, #195, #210 | #204 | All of them emit the shared provenance contract |
| #208, #209, #211 | `InstrumentRef` | A bare ticker cannot tell two listings apart |
| #209 global coverage | #208 | Coverage cannot be disclosed before universes carry their source |
| #205 in-app help | #206 | Help reuses the docs content and the shared tokens |

## Decisions

### Decided by the owner (2026-11-03)

These bind every plan.

1. **Tools are available to all users.** The exceptions are the few tools that read
   server-wide state rather than research data. Plan 2 WP3.
6. **`SYS_ADMIN` sets up model profiles; users choose a profile.** Users also choose
   their agents' personas. Plan 2 WP4.
7. **License: MIT.** Plan 9 WP1.
8. **Language: 100% US English (en-US)** in the app, site, docs and output. Plan 9 WP3.
9. **All dates and times are shown in UTC.** Plan 9 WP2.
10. **`SYS_ADMIN` only does what setting up the system requires.** Everything else is
    the user's choice. New features follow this, and Plan 2 WP6 reviews what is
    admin-only today.

### Still open, with a recommendation

The plans assume the recommendation unless told otherwise.

2. **Strict-mode streaming.** An answer cannot be verified while it streams.
   *Recommendation:* in strict mode, hold the answer back, show stages, and
   release it after the audit. Plan 3 WP5.
3. **What "primary" means for prices.** Yahoo is an unofficial aggregator, so a
   literal primary-only mode could not use the default price source.
   *Recommendation:* a separate "provider data" class, allowed in strict mode and
   labeled official or unofficial. Plan 3 WP5.
4. **Universe sources.** Today's lists are hand-written in Go, and index membership
   data is often licensed. *Recommendation:* label them as curated now and do not
   imply index membership; add licensed or official sources one at a time. Plan 4 WP2.
5. **What changing instrument does.** Selecting a symbol in live mode posts to
   `/api/config`, which changes server state and trackers. *Recommendation:*
   separate "view" from "track". Plan 6 WP2.

## Older issues

- **#5** is almost done: #144, #145, #146, #117 and #142 are closed or merged, and
  #188 added tasks. What remains is a UI for tasks and user-confirmed assumptions
  (Plan 3 WP6). Close it after that.
- **#6** is partly done by #189. Versioned universes and measured bias remain
  (Plan 4 WP2 and WP4).
- **#8** stays open as a research gate, with no implementation.

## Rules for implementers

- One work package is one PR into `main`, using `feature/<slug>`, `fix/<slug>` or
  `docs/<slug>`, with `Relates to #N` as `CONTRIBUTING.md` requires.
- Finance in Go, prose in the model; absent is not zero. See section 0 of
  [the investor growth plan](../investor-growth-plan.md).
- Database-backed Go tests skip locally, so verify them in CI.
- A change to the plugin protocol needs `pluginsdk/testdata/contract/` updates, a
  new `pluginsdk/v<semver>` tag and a `go.mod` bump in `Doldas/floweysales-plugins`.
- Add new frontend specs to the `node --test` list in `CONTRIBUTING.md` and CI.
- All user-facing text is US English. All displayed dates and times are UTC and say so.
- A new setting belongs to the user unless it is needed to set up the system.
