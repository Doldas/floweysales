# Agent sources and evidence mode

Issues #197, #201, #207 and #255. Plan: [Plan 3](plans/issues/3-evidence-and-provenance.md).

## What the agent records

Each chat run keeps a registry of sources (`internal/sources`). `search_web` hits are **listed**: the agent saw a title and snippet, not the page. The `read_source` tool opens one hit by its `source_id`; the hit then becomes **read**, keeps its id, and the page text the model received is what the citation check compares figures against. `get_sec_section` results are **read** and carry the filing URL and accession. The model receives a `source_id` (S1, S2, ...) with each result and is asked to end a sentence with `[S3]` when the sentence rests on that source.

The stream carries a `source` event for each source and a final `sources` event with all of them and the audit findings. The chat shows them in a Sources panel: findings first, then sources the agent read, then search results only.

## The citation check

A mechanical check, per sentence of the final answer:

| Finding | Meaning |
| --- | --- |
| `fabricated` | A `[Sn]` marker or URL that no tool returned in this run |
| `snippet_only` | A figure that rests on a search result the agent never opened |
| `figure_mismatch` | A figure that is not in the cited excerpt |
| `uncited` | A figure that appears in a retrieved source but is stated without a marker. Figures from other tools (prices, a DCF) need no marker |
| `quote_mismatch` | A passage in quotation marks (five words or more) that is not in the text of the source cited in the same sentence. Case, spacing and curly quotes are ignored; an ellipsis splits the quote and each part of five words or more must be found. Short quotes are treated as terms and skipped. A quote that contains a sentence break is not seen |
| `not_primary` | A secondary source cited in Primary sources only mode |
| `excluded_source` | A source of a type the evidence settings do not accept (see source types below) |
| `stale` | A cited source published longer ago than the recency limit, or with no publication date to check |
| `unreachable` | A cited link that no longer resolves (strict modes only) |

A passing answer is "citation checked". It is not "true": the check does not read whether prose supports a claim. The claim check below goes one step further for the cases it can decide with fixed rules.

## The claim check (semantic verification)

Issue #255. A real source that contains a number does not prove the sentence means what it says: the number may belong to another metric, period, company, unit or direction. After the citation check, `internal/sources/claims.go` builds a **claim-level evidence record** for every sentence that has a citation, a figure, a stated cause, a forecast or a recommendation, and compares it with the cited text. It is deterministic string and arithmetic work: **no model is called**, so it is repeatable, free and cannot be talked into a verdict by text in a source.

### The record

| Field | Meaning |
| --- | --- |
| `id`, `sentence`, `citations` | C1, C2, ...; the sentence as written; the `[Sn]` markers in it |
| `kind` | `fact`, `calculation`, `inference`, `forecast` or `recommendation`; `causal` is set when the sentence states a reason |
| `entity`, `metric`, `period`, `unit`, `direction`, `qualifiers` | What the sentence asserts, as the verifier read it (company before the first metric or figure; a canonical metric such as `revenue` or `net income`; `FY2024`, `Q3 2024`; `USD` or `percent`; `up` or `down`; `non-GAAP`, `adjusted`, `diluted`, ...) |
| `assertion` | The normalized form, for example `Acme \| revenue \| FY2024 \| = 4.5 billion` |
| `verdict`, `code`, `explanation` | See below |
| `figures` | One verdict per figure, with the metric, period and unit read next to it in the source |
| `spans` | The exact passage of the source each verdict rests on: `source`, byte offsets `start`/`end` into the stored excerpt, the matched token (`match`) and the text around it (`before`, `after`), with the source's URL, title, accession (`document_id`), `published_at` and `retrieved_at` |
| `calculation` | The recomputation of a stated percentage change, for example `(4500000000 - 3900000000) / 3900000000 = 15.4%` |
| `conflicts` | Other retrieved sources that give a different figure for the same metric and period |
| `limitations`, `injection_suspected` | What the verdict does not establish; whether a cited source contained text that reads as an instruction to an AI |
| `verifier_version`, `verified_at` | The rules that produced the record (`claims-v1`) and the UTC time (RFC 3339) |

### Verdicts

| Verdict | Meaning |
| --- | --- |
| `supported` | The cited text states the figure with the same metric, period, unit and direction, or a percentage change follows from endpoints it states. Proof of what the source says, not that the source is right. |
| `contradicted` | The cited text gives the number for another metric (`wrong_metric`), period (`wrong_period`), unit, scale or currency (`wrong_unit`), or direction (`wrong_direction`); the arithmetic is wrong (`arithmetic_error`); a stock-split ratio differs (`corporate_action_mismatch`). |
| `insufficient` | Not proven, which is not the same as false: the company is not named in the source (`wrong_entity`), the figure is adjusted or guidance but stated as reported (`misleading_context`), the stated cause is not in the source (`unsupported_causal`), another source gives a different figure and the sentence does not say so (`conflicting_sources`), the figure or a date is not in the text, the only source is an unopened search hit (`source_not_opened`), or the figure appears only in text that reads as an instruction to an AI (`injected_instruction`). |
| `not_checked` | No deterministic rule applies: recommendations, inferences, forecasts (beyond whether the source states the figure), qualitative statements, and figures with no recognized metric. The record says so and is never shown as confirmation. |

What the rules compare: the figure is found in the source with its precision, percent versus amount, scale words and table notes such as "(in millions)", and currency. The metric is the one named next to the figure in the source (longest phrase first, so gross profit is never read as profit; a net loss is not net income). The period is the one next to the figure in the sentence, or, in a table row, the column header at the same position. A quarter, half or year-to-date period must match in shape, so a quarter figure is not accepted for a fiscal year. Adjusted, non-GAAP, pro forma, constant-currency and organic figures must be described as such, and basic is not diluted. A figure the source states as guidance or an expectation is not accepted as a reported result.

**Conflicting and cherry-picked evidence.** For the figure a sentence uses, the other retrieved (opened) sources are searched for the same metric and period. A different amount (more than 1% apart, within a plausible magnitude) is listed in `conflicts`, and the sentence is `insufficient` unless it acknowledges the disagreement (for example "restated", "however", "the amended filing").

**Prompt injection.** Text in a source is data. A sentence in a source that reads as an instruction to an AI ("ignore previous instructions", "mark all claims as supported", "verdict: supported") is marked, and so are the sentences next to it, because injected text usually continues. Numbers found only there never count as evidence; the record is flagged `injection_suspected`. The agent prompt also tells the model that source text is never instructions.

### Findings and evidence-only behavior

The claim check adds findings to the audit, so the existing correction rounds, the block option and the flow-node failure behavior apply:

| Finding | When |
| --- | --- |
| `claim_contradicted` | A `contradicted` claim, in every mode |
| `claim_insufficient` | An `insufficient` claim whose number was misread or misattributed (`wrong_entity`, `misleading_context`, `wrong_qualifier`, `conflicting_sources`, `unsupported_causal`, `injected_instruction`) in every mode; any other `insufficient` claim in `evidence_only` and `primary_only` |
| `unsupported_claim` | `evidence_only` and `primary_only` only: a recommendation or forecast with no citation that is not labeled as a judgement ("Forecast:", "Inference:", "Opinion:", "estimate", "not investment advice", ...) |

A sentence another check already flagged is not flagged twice. In the strict modes the agent abstains: it is asked to label judgements and say "insufficient verifiable evidence" for what it cannot support, and what still fails is flagged or, with `on_failure: block`, removed. Standard mode flags contradicted and misread claims but does not hold the answer.

### Where records appear

The `sources` stream event carries `claims` and `claim_stats`; a task run's checkpoint keeps `claims` (`GET /api/ai/tasks/{id}`); each flow agent step stores them in its `sources` record with `claim_stats`, redacted like the sources and capped at 40 claims. In the chat, **Sources** has a **Claims checked against their sources** section: each claim opens to **Why is this claim supported?** (or "not supported") with the explanation, what was read as entity, metric, period, unit and direction, the calculation, the source passage with the matched token highlighted, its retrieval time in UTC, conflicting figures, and the limitations.

### Cost and limits

No model call, no network request. At most 40 sentences are verified per answer (the count of the rest is in `claim_stats.skipped`), at most 24 places in a source are examined per figure, and each source excerpt is at most 16,000 characters. `claim_stats` reports the claims by verdict, `elapsed_ms` and `model_calls` (always 0); the chat shows them.

### What it does not do

It reads English filing and press-release phrasing and a fixed metric list (revenue, gross profit, margins, operating and net income, EPS, EBITDA, cash flows, capex, R&D, SG&A, assets, liabilities, debt, cash, equity, shares, dividends, repurchases, employees). A claim it cannot read is `not_checked`, not supported. Entity matching is by name, not by identifier, and an entity that only appears in a part of the source the agent did not receive is reported `insufficient`. It does not compare against data-provider records or other tools' results, and an AI or NLI review as a secondary signal is not built (see below).

## Evidence mode

Chosen in the chat (Evidence), sent as `evidence_mode` on `POST /api/ai/chat` and `POST /api/ai/team`, or as a full `evidence` object (mode and the settings below). A request that sends neither gets the caller's saved default (see below). Every user sets their own; administrators have no say.

| Mode | Behavior |
| --- | --- |
| `standard` (default) | The answer streams as written; the check runs afterward and flags. |
| `evidence_only` | The answer is held until the check passes. A failing draft goes back to the model with its findings, at most twice. If problems remain, the last draft is shown with a warning that the flagged sentences are unverified, or, with the block option, without them. |
| `primary_only` | As `evidence_only`, and only primary filings count as support. |

The prompt in these modes asks the model to say "insufficient verifiable evidence" instead of filling gaps from memory, and to keep calculation, inference and opinion apart from reported fact.

### Settings under the mode

| Setting | Meaning |
| --- | --- |
| `on_failure` | `flag` (default) shows the last draft with a warning. `block` removes every sentence the check still flags, ends the answer with a line saying how many were removed and which checks failed, and lists the removed sentences with their findings in Sources (`removed`). If no sentence is left, the answer is "Insufficient verifiable evidence: ..." and names the failed checks. `block` needs `evidence_only` or `primary_only`: a standard answer has already been streamed when it is checked. |
| `max_age_days` | Recency limit, 1 to 3650 days; 0 means none. A cited source published longer ago is flagged `stale`. A source with **no publication date** is flagged too, since it cannot be shown to be recent: web search hits carry no machine-readable date today, so under a recency limit only filings (which carry their filing date) can support a sentence. |
| `allow_classes`, `deny_classes` | Source types that count (`primary_filing`, `web_page`). With an allow list only those types count; a deny list is never accepted and wins over the allow list. In `primary_only` with no allow list only primary filings count; an allow list replaces that default (for example filings and web pages both). A type outside the settings is flagged `excluded_source` (or `not_primary` for the default). |

### Saved default

`GET`, `PUT` and `DELETE /api/ai/evidence-default` read, save and reset the signed-in user's own default (table `user_evidence_defaults`). It applies to chat and team requests that send no `evidence_mode` or `evidence`, and, only when the delegating run carries no policy of its own, to the members of a team that the agent delegates to with `delegate_to_team`. Delegated members otherwise answer under the policy of the run that delegates: the chat's choice, or the flow node's `evidence` setting. A request that names `evidence_mode` alone gets that mode with no settings, so choosing Standard in the chat turns a saved default off for that request. Only the user can change it; there is no administrator endpoint, and deleting an account deletes it. A saved default that no longer validates is ignored.

## Teams, flows and tasks

- **Agent teams.** `POST /api/ai/team` takes `evidence_mode` or `evidence` (else the saved default). Every member answers under the same policy; each member's `sources` event carries its agent id. The chair has no tools and no sources of its own, so it runs without an evidence mode: its summary is not citation checked, and it is told to use only what the members wrote.
- **Flow agent nodes.** An agent node can carry an `evidence` object (same shape). Its answer is held until checked, with two extra model rounds for corrections. A node whose answer still fails the check **fails the attempt** (so the node's retries apply), unless the policy blocks the unsupported sentences. A JSON node can have an evidence mode (see Flow output below), but not the option to remove statements. Flows are saved with the flow, so a flow's evidence settings belong to whoever owns the flow, not to the running user's default.
- **Sources with runs.** A task run's checkpoint keeps the sources, the audit, the removed sentences and the policy; `GET /api/ai/tasks/{id}` returns them (excerpts cut to 600 bytes). A resumed task restores the registry, so `[S3]` from the first round still means the same source, and keeps the policy it started with. Each flow agent step stores its sources (at most 40, excerpts cut to 400 bytes and redacted), the findings, the removed sentences and the policy in the step record (`sources` in the run history API). They are not part of the node's output, so downstream nodes and JSON contracts do not see them. The sources of a failed step are kept for the last attempt.

## Flow output: Markdown and JSON

Issue #197. The step record keeps the sources, so the node's output renders them:

- **Markdown.** After the node's answer passes its checks, each `[Sn]` marker becomes a link (`[[S1]](url)`) to the source's address and a **Sources** list ends the report. The list has only the sources the report cites: title, address, accession, publication date (or "publication date unknown"), retrieval time in UTC, whether the agent read it or saw a search result only, and the team member that found it. A marker with no source in the run stays as written and is listed as unresolved. Addresses lose credentials and fragments; credential-like query values become `REDACTED`. The whole output stays within the 6,000 byte node limit (the report is shortened, not the list). Downstream nodes, notifications and the run history get this text.
- **JSON.** The check reads the string values of the document as sentences (`AgentOptions.JSONAnswer`). Markers at the end of a string are removed from the data and stored in the step's `sources` record as `citations`: JSON Pointer `path`, the `sources` cited and the `text` (redacted, 300 characters). Then the JSON contract is checked on the data without markers, so the declared structure never changes and no source field is added. Object keys of a re-encoded document are sorted; a document without markers is passed on unchanged. `on_failure: block` is refused for JSON, since removing a sentence would break the structure: a failing node fails and its retries apply.

## Sources of more tools

`get_sec_filings` registers each filing (up to 10) as a read, primary source with its accession, filing date and official URL; `get_sec_facts` registers one source per filing accession whose excerpt lists the facts the model received from it (metric, value, unit, period, up to 2,000 characters), and gives every fact the `source_id` of its filing. Search hits now keep the `published_at` the search provider returned (parsed from ISO, RFC 1123 and common day-month-year formats), so a recency limit accepts a dated hit and still flags an undated one. Provider records (prices, quarterly financials, calculators) are still not sources: their figures need no marker.

## Exporting team and task runs

The team page offers **Export Markdown** and **Export JSON** after a run (`floweysales.team-export/v1`): the question, the chair summary and each member's answer, stance, model, sources and findings, built from the same answer export as the chat. `GET /api/ai/tasks/{id}` returns the task's last `answer`, and the Saved tasks list exports a task with its sources, findings and policy. Times are UTC and addresses are redacted as in the chat export.

## Team chair summary check

The chair has no sources of its own: it may only restate what the members wrote. After the chair answers, `sources.CheckSummary` (`internal/sources/summary.go`) traces every notable figure in the summary (percentages, currency and scaled amounts, numbers of 100 or more; years and small counts are skipped) to the member answers. It adds a `summary_figure_not_in_members` finding when no member wrote the figure, and `summary_misattributed` when a sentence ties the figure to one member but only other members wrote it. Rounding to a scale word ("$4.2 billion" for 4,203,000,000) is accepted; a changed scale is not. It is deterministic, calls no model, and checks that a figure was said, not that it is true. The findings arrive as a `sources` event for the chair and show under the chair's answer.

## Reachability check

In `evidence_only` and `primary_only`, a source the answer cites by marker and the agent never opened gets a request to see whether the link still resolves (an opened page was fetched, so it resolved). A dead link is flagged `unreachable` and the draft goes back to the model like any other finding.

- Same address rules as `read_source` and the flow HTTP node: public `http`/`https` only, no credentials, fragments or custom ports, resolved address pinned, redirects (at most three) checked again. An address those rules refuse is **not checked** rather than reported dead.
- Bounded: a HEAD request (GET when the server does not support HEAD), no body read, 8 seconds each, at most 8 distinct addresses per run, each address once even across corrections. Links beyond the cap are not checked and not flagged.
- 2xx means resolves. 401, 403 and 429 also count as resolving: the server answered but does not talk to automated clients. 404, other 4xx, 5xx and connection failures are `unreachable`.
- Standard mode makes no requests. A link that resolves says nothing about whether the page supports the sentence.

## Strict-mode prompt rules

In both strict modes the model is asked to start every sentence that is not a reported fact with its kind (`Calculation:`, `Estimate:`, `Assumption:`, `Interpretation:`, `Opinion:`), to give the method and each input (with its `[Sn]` or tool) for any calculation, to name conflicting sources and stale or missing data, to treat a page that calls an investment good as that page's claim, and to say "The evidence policy cannot be met" instead of recommending something the allowed sources do not support. These labels are requests to the model. The server does not verify them.

## Export

The Sources panel offers **Export Markdown** and **Export JSON** for the latest answer. Both carry the same fields: the schema (`floweysales.answer-export/v1`), the UTC export time, the question, the answer, the evidence policy the run was sent with and a one-line label of it, the citation check result, and every source (tool, type, opened or search result only, address, search input, document id, publication and retrieval times, excerpt cut to 600 characters, retrieving team member), plus findings and removed statements. Addresses lose credentials and fragments, and the value of a parameter that looks like a credential (`key`, `token`, `secret`, `sig`, `auth`, `session`, ...) becomes `REDACTED`. A field is only removed or renamed with a new schema version. The Markdown starts with the policy and states that the answer is research decision support, not a guarantee and not authorization to trade.

## Not built yet

Cross-checks against data-provider records and calculation tool inputs, an independent AI or NLI review recorded as an auditable secondary signal (never as proof), claim labels checked by the server (the model is asked to keep fact, calculation and opinion apart, nothing verifies it), a per-claim view in team and flow-run screens (the data is stored with each flow step), and an evidence mode saved per team. Provider records (prices, quarterly financials) are not registered as sources.

## Where to set it

- **Chat.** The Evidence selector sets the mode; for the two strict modes an Evidence settings section sets what happens to a failing statement, the age limit and the source types that count, with Save as my default and Remove my default. Opening the chat fills the settings from the saved default; changing them afterwards affects only that browser session until saved. The chat always sends its settings, so choosing Standard is not replaced by the saved default.
- **Teams.** The Evidence selector on the Team page offers My default or a mode. A mode picked there brings none of the default's limits. Each member's card shows its citation check result and what was removed.
- **Flow agent nodes.** The node settings have Evidence mode, Remove statements that fail the citation check and the age limit. Run history shows the sources, findings and policy of each agent step.

## Opening a page (`read_source`)

`read_source` takes only a `source_id` from a `search_web` result of the same run. The server looks up the address in the run's registry, so the model cannot make it fetch a URL no search returned. The fetch uses the same guards as the flow HTTP and crawler nodes:

- public `http`/`https` hosts only; no credentials, fragments, custom ports, local or private addresses, and the resolved address is pinned for the connection;
- up to three redirects, each checked again before it is followed;
- HTML, plain text and text-based PDF. A PDF is read up to 8 MB and its first 40 pages; a PDF is read page by page (see [pdf-vision.md](pdf-vision.md)). A scanned PDF with no usable vision model, like a login page or a page that needs JavaScript, returns an error the agent sees while the source stays **listed**;
- at most 512 KB of HTML is read and 12,000 characters of text reach the model; the result says when it was cut.

The tool counts toward the per-minute tool budget of ordinary users. It is not exposed through the MCP server, which has no run to resolve a `source_id` against. Reading a page does not make it a primary source: in Primary sources only mode a web page still does not count.


### Source dates and social discussion

Search-hit evidence now retains a valid `published_at` and, when supplied, a separate `modified_at` timestamp. Invalid and absent dates remain unknown; neither should be substituted by the retrieval time. URL-based classification marks Reddit, Stocktwits, X/Twitter, Threads and Bluesky as `social_discussion` rather than generic `web_page` sources. Social discussion can support claims about **sentiment or opinions**, not independent proof of company revenue, cash flow or regulatory figures. The original hit remains `listed` (snippet-only) until a tool actually reads its content. Agent Team delegation keeps these source labels and dates. This is a groundwork improvement; issue #312 still needs query-specific ranking, report-period metadata and freshness-aware selection before analysis.
