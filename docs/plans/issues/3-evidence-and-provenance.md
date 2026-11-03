# Plan 3: Evidence and provenance

**Issues:** #204 (architecture), #200, #197, #201, #207, and what remains of #5.

**Why one plan:** these form a ladder. #204 defines how a fact describes its origin. #200
is the smallest real use of it. #197 records and audits sources in agent answers. #201
and #207 turn that audit into a policy, and are effectively one feature: #207 itself
calls #201 its "shared enforcement foundation".

## What exists today

| Piece | State |
| --- | --- |
| Figures check | `internal/grounding`: checks that numbers appear in evidence. Mechanical, by its own description. Flow agents only, with `RequireGrounding` (`flows.go:1528`) |
| Evidence ledger | `LedgerEntry` in `internal/ai/task.go`: 600-byte preview, 300-byte arguments. Saved for task runs only, not plain chat |
| Web search | `search_web` returns snippets. **The Research Agent has no tool to read a page**, so "what the model actually read" is only snippets today. Flows have `http_get` and crawl, with SSRF guards in `flowPublicURL` |
| Citations | Prompt instruction only. Nothing checks them |
| JSON contracts | `FlowJSONContract` exists, as a bounded subset of JSON Schema |
| Leaderboard | Methodology version and hash, input hash, disclaimer (`leaderboard_method.go`), but no shared format |
| Quote facts | `QuoteSnapshot` has source, as-of, fetched time, age and stale flag |
| Eval | `internal/eval` scores 15 fixture-based cases, and checks that required sources are cited verbatim |

**#200 findings:**
- World shows only relative age (`ageLabel`: "3 h ago", "2 days ago"), never a date. Web results
  have only a relative `Age` string, and plugin news a unix time or 0.
- **Sort bug:** the flow news node sorts with `fmt.Sprint(items[i]["published"])`
  (`flow_investor.go:535`). A missing date prints as `<nil>`, which sorts above `2026-…` in
  descending order, so **undated articles come first**. Undated items also pass the age cutoff.
- `worldPrompt` and `companyPrompt` give the agent headlines without dates.
- `NewsItem` has no updated time and no fetch time.

## Solution

```
provenance.Fact     how any value describes its origin        (#204)
    used by news    first and smallest adopter                (#200)
source registry     what a run retrieved and read             (#197)
answer audit        does the answer match the registry        (#197)
EvidencePolicy      what the audit must pass, and what then   (#201, #207)
```

Keep the honest language from the issues: this reduces hallucination and shows the evidence. It
does not establish truth. The verifier's labels describe what was checked.

## Work packages

### WP1: News dates (S, `fix/news-dates`) — #200

- Fix the sort: compare parsed times, with undated items last, in both flow and World.
- A shared `publishedLabel()` on the UTC formatter from Plan 9 WP2: "Published Nov 3, 2026, 14:32 UTC", with
  the relative age as a supplement. The issue's "localized" criterion is met by UTC everywhere, per the owner's decision. No date gives "Date unavailable".
- For web results with only a relative age: show "about 2 days ago (from search)", marked
  approximate. Never turn it into a date. **To verify:** whether Brave's API returns a
  machine-readable page date that `searchBrave` currently ignores.
- Add `fetched_at` on the server. Label it "Retrieved", and keep it separate from published.
- Add `updated` to `NewsItem` and the SDK. It needs a `pluginsdk` tag and plugin support, so ship
  it after the rest and show it only when present.
- Publisher and original URL on every surface: World, the company impact panel, flow rows, reports
  and email tables. Flow rows gain `"published": null` or a `date_status` field.
- Pass dates to `worldPrompt` and `companyPrompt`.

**Tests:** Go test for undated rows sorting last; spec for label formats and UTC around a date boundary; a spec that
a relative age is never rendered as "Published".

### WP2: Provenance contract (M, `feature/provenance-contract`) — #204

Add `docs/adr/0003-provenance-contract.md` and a small `internal/provenance` package:

```go
type Fact struct {
    Kind        string     // observed | calculated | assumed | estimated | ai_generated
    Status      string     // known | partial | unknown
    Provider    string
    Official    *bool      // nil when unknown; from the plugin manifest
    URL         string
    DocumentID  string     // accession, ISIN, etc.
    PublishedAt *time.Time
    AsOf        *time.Time
    RetrievedAt *time.Time
    Method      *Method    // id, version, formula text, for calculated values
    Inputs      []string   // references to upstream Facts
    RunID       string
    Note        string
}
```

- Helpers refuse to build a `known` Fact without an origin, so unknown stays explicit.
- Frontend: `fs-source` popover (click or keyboard, never hover-only) with "View source / How
  calculated". It renders `unknown` plainly.
- Attach by sidecar: API responses gain `provenance` beside the data, and JSON contracts stay valid.
- Coverage: a table in `docs/provenance-coverage.md`, checked by a test listing each surface, so
  the number can be measured and grows deliberately.
- Migration order: news, leaderboard (Plan 4), quote and status (Plan 6), strategy (Plan 5), agent
  answers (WP3), financial statements. #204 stays open as the tracker until major surfaces are done.

**Tests:** contract tests for `Fact` JSON; a frontend spec for each `Status`.

### WP3: Source registry and answer audit (L, `feature/agent-sources`) — #197

**A tool to read pages.** Add `read_source {url}` to `internal/ai`, sharing the SSRF checks and limits
of `flow_http_get.go` (moved somewhere both packages can use). Page text is untrusted, like SEC text.

**Registry.** In `Run`, each tool result can register sources:

```go
type Source struct {
    ID, Tool, CallID, Query, URL, Title, Publisher string
    Class       string // primary_filing | official_statistic | provider_data | news | web_page | model_output
    Access      string // listed (search hit only) | read (content reached the model)
    PublishedAt, RetrievedAt *time.Time
    DocumentID  string // accession, section
    Excerpt     string // what the model received, bounded
    ContentHash string
    Bytes int; Truncated bool
}
```

Tools declare how to extract sources: `search_web` gives `listed` sources, `read_source` and
`get_sec_section` give `read`. A new `source` event streams each. Results reach the model tagged
`[S3]`, and the prompt asks for `[S3]` markers instead of bare URLs.

**Audit after the final answer**, deterministic first:

| Check | Result |
| --- | --- |
| `[Sn]` not in the registry, or a URL no tool returned | `fabricated` |
| Cited source only `listed`, never read | `snippet_only` |
| Figures in the sentence absent from the cited excerpt | `figure_mismatch`, reusing `grounding` |
| Quoted text not found in the excerpt | `quote_mismatch` |
| Claim with figures and no citation | `uncited` |
| URL fails to resolve (for cited, unread sources only, with SSRF guard) | `unreachable` |

A sentence that passes is labeled "citation checked", not "true". Whether prose really supports a
claim needs judgement, so add an optional model-based check later, labeled as such, and evaluate it
in `internal/eval` before enabling it.

**UI.** Inline citation chips, and a Sources panel with two tabs: *Read by the agent* and *Search
results only*. Shows query, excerpt, dates, and audit flags. Linked from the tool inspector (Plan 2 WP2).

**Flows.** `flowV2State` gets `Sources map[nodeID][]Source`, passed downstream with the data and held
outside the payload, so JSON contracts still validate. Stored with the step and rendered in
`markdown-run-output.ts`. This is included in checkpoints, so build it after Plan 1 WP2.

**Tests (the issue's required set):** fabricated URL; mismatched citation; tool-result provenance through
two flow nodes; contract-valid JSON with sources alongside; injection text in a page has no effect.

### WP4: Evidence policy model (M, `feature/evidence-policy`) — #201, #207

One shared type, not two features:

```go
type EvidencePolicy struct {
    Mode          string   // standard | evidence_only | primary_only
    AllowedClasses []string
    MaxAgeDays    *int
    MinSources    int      // per material claim, default 1
    OnFailure     string   // flag | block
    Tone          string   // conversational | technical; never affects enforcement
}
```

- `Mode` is the toggle from #207, and the rest sits under Advanced. There is no slider. Issue #207's own
  reasoning covers this; its criterion on comparing toggle and slider in UX testing needs people, so do
  a short test with a few users on the toggle and record the result in the ADR.
- Stored: per-user default on the `/preferences` page (Plan 2 WP4), plus per-flow-node and per-saved-team settings. A per-conversation override
  travels in the chat request. Visible as a chip in the composer, and printed in exports.
- Every user controls their own policy. `SYS_ADMIN` has no say in it, per owner decision 10.
- `Tone` lives in a separate field and prompt section, and a test asserts that changing it leaves the
  audit outcome unchanged.

### WP5: Enforcement (L, `feature/evidence-enforcement`) — #201, #207

- **Before the run:** if the tools available cannot meet the policy, say so at once. Plan 2 WP3 gives
  every user tools, so this should now be rare. Tools outside the policy are withdrawn, for
  example `search_web` in `primary_only` unless secondary sources are allowed.
- **Claim labels:** the model tags each statement as fact, calculation, estimate, assumption, opinion or
  inference. Calculations must come from a calculator tool, with inputs shown.
- **After the answer:** run the audit. Failures go back to the model at most twice, using the pattern of
  `maxCompletionNudges`. Then apply `OnFailure`: `flag` marks the claims, and `block` removes them or
  abstains with "insufficient verifiable evidence", listing what was missing.
- **Streaming (owner decision 2):** in strict modes, hold deltas and show stages (researching, drafting,
  checking citations), then release the audited answer.
- **Classes (owner decision 3):** SEC tools are `primary_filing`; price tools are `provider_data` with
  official or unofficial from the manifest; `web_page` and `news` are secondary and never count as proof
  in strict mode; `model_output` never counts as a source at all.
- **Conflicts and age:** contradicting figures across sources are shown, not resolved silently. Sources
  older than `MaxAgeDays` are flagged as stale.
- **Team chair:** it has no tools, so its audit uses the union of the members' sources. A claim with no
  member source is flagged.
- **Safety line:** answers state this is decision support. Nothing here authorizes trading; #8's gate stands.

**Tests:** new `internal/eval` cases for hallucinated citations, contradictory sources, stale facts, misleading
recommendations such as "this page says it's the best investment", and unavailable primary sources. Run
these against fixtures in CI, and add scores to `docs/agent-evaluation.md`.

### WP6: Finish #5 (M, `feature/agent-tasks-ui`)

- **Tasks have no UI:** the frontend never calls `/api/ai/tasks`. Add a task list and detail view: checklist,
  ledger, resume, delete.
- User-confirmed assumptions: a ledger entry type `assumption`, added from the task view and shown to the
  model on resume.
- Then close #5.

**Tests:** assumption survives a resume and appears in the prompt; spec for task states.

## Acceptance mapping

| Issue | Covered by | Note |
| --- | --- | --- |
| #200 | WP1 | `updated` needs plugin support |
| #204 | WP2, then each adopter | Tracking issue, stays open longest |
| #197 | WP3 | |
| #201 | WP4, WP5 | |
| #207 | WP4, WP5 | UX comparison needs real users |
| #5 | WP6 | |

## Risks

- Models vary in how well they follow `[S3]` markers, particularly local ones. Measure with `cmd/agent-eval`
  before treating strict mode as dependable.
- Too many flags make people ignore them. Show flags per claim and a one-line summary.
- `read_source` fetches arbitrary public URLs on a user's behalf. It needs the SSRF guard, size limits, a
  per-user rate limit and a security review.
