# Agent sources and evidence mode

Issues #197, #201 and #207. Plan: [Plan 3](plans/issues/3-evidence-and-provenance.md).

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

A passing answer is "citation checked". It is not "true": the check does not read whether prose supports a claim.

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

`GET`, `PUT` and `DELETE /api/ai/evidence-default` read, save and reset the signed-in user's own default (table `user_evidence_defaults`). It applies to chat and team requests that send no `evidence_mode` or `evidence`, and to the members of a team that the agent delegates to with `delegate_to_team`. A request that names `evidence_mode` alone gets that mode with no settings, so choosing Standard in the chat turns a saved default off for that request. Only the user can change it; there is no administrator endpoint, and deleting an account deletes it. A saved default that no longer validates is ignored.

## Teams, flows and tasks

- **Agent teams.** `POST /api/ai/team` takes `evidence_mode` or `evidence` (else the saved default). Every member answers under the same policy; each member's `sources` event carries its agent id. The chair has no tools and no sources of its own, so it runs without an evidence mode: its summary is not citation checked, and it is told to use only what the members wrote.
- **Flow agent nodes.** An agent node can carry an `evidence` object (same shape). Its answer is held until checked, with two extra model rounds for corrections. A node whose answer still fails the check **fails the attempt** (so the node's retries apply), unless the policy blocks the unsupported sentences. A node with an evidence mode cannot use JSON output. Flows are saved with the flow, so a flow's evidence settings belong to whoever owns the flow, not to the running user's default.
- **Sources with runs.** A task run's checkpoint keeps the sources, the audit, the removed sentences and the policy; `GET /api/ai/tasks/{id}` returns them (excerpts cut to 600 bytes). A resumed task restores the registry, so `[S3]` from the first round still means the same source, and keeps the policy it started with. Each flow agent step stores its sources (at most 40, excerpts cut to 400 bytes and redacted), the findings, the removed sentences and the policy in the step record (`sources` in the run history API). They are not part of the node's output, so downstream nodes and JSON contracts do not see them. The sources of a failed step are kept for the last attempt.

## Reachability check

In `evidence_only` and `primary_only`, a source the answer cites by marker and the agent never opened gets a request to see whether the link still resolves (an opened page was fetched, so it resolved). A dead link is flagged `unreachable` and the draft goes back to the model like any other finding.

- Same address rules as `read_source` and the flow HTTP node: public `http`/`https` only, no credentials, fragments or custom ports, resolved address pinned, redirects (at most three) checked again. An address those rules refuse is **not checked** rather than reported dead.
- Bounded: a HEAD request (GET when the server does not support HEAD), no body read, 8 seconds each, at most 8 distinct addresses per run, each address once even across corrections. Links beyond the cap are not checked and not flagged.
- 2xx means resolves. 401, 403 and 429 also count as resolving: the server answered but does not talk to automated clients. 404, other 4xx, 5xx and connection failures are `unreachable`.
- Standard mode makes no requests. A link that resolves says nothing about whether the page supports the sentence.

## Not built yet

Claim labels checked by the server (the model is asked to keep fact, calculation and opinion apart, nothing verifies it), an evidence mode saved per team, the check on a team chair's summary, and a publication date for web search hits (so a recency limit can accept them). Sources of tools other than `search_web`, `read_source` and `get_sec_section` are not registered.

## Where to set it

- **Chat.** The Evidence selector sets the mode; for the two strict modes an Evidence settings section sets what happens to a failing statement, the age limit and the source types that count, with Save as my default and Remove my default. Opening the chat fills the settings from the saved default; changing them afterwards affects only that browser session until saved. The chat always sends its settings, so choosing Standard is not replaced by the saved default.
- **Teams.** The Evidence selector on the Team page offers My default or a mode. A mode picked there brings none of the default's limits. Each member's card shows its citation check result and what was removed.
- **Flow agent nodes.** The node settings have Evidence mode, Remove statements that fail the citation check and the age limit. Run history shows the sources, findings and policy of each agent step.

## Opening a page (`read_source`)

`read_source` takes only a `source_id` from a `search_web` result of the same run. The server looks up the address in the run's registry, so the model cannot make it fetch a URL no search returned. The fetch uses the same guards as the flow HTTP and crawler nodes:

- public `http`/`https` hosts only; no credentials, fragments, custom ports, local or private addresses, and the resolved address is pinned for the connection;
- up to three redirects, each checked again before it is followed;
- HTML, plain text and text-based PDF. A PDF is read up to 8 MB and its first 40 pages; a scanned PDF has no text layer and, like a login page or a page that needs JavaScript, returns an error the agent sees while the source stays **listed**;
- at most 512 KB of HTML is read and 12,000 characters of text reach the model; the result says when it was cut.

The tool counts toward the per-minute tool budget of ordinary users. It is not exposed through the MCP server, which has no run to resolve a `source_id` against. Reading a page does not make it a primary source: in Primary sources only mode a web page still does not count.
