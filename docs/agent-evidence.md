# Agent sources and evidence mode

Issues #197, #201 and #207. Plan: [Plan 3](plans/issues/3-evidence-and-provenance.md).

## What the agent records

Each chat run keeps a registry of sources (`internal/sources`). `search_web` hits are **listed**: the agent saw a title and snippet, not the page. `get_sec_section` results are **read** and carry the filing URL and accession. The model receives a `source_id` (S1, S2, ...) with each result and is asked to end a sentence with `[S3]` when the sentence rests on that source.

The stream carries a `source` event for each source and a final `sources` event with all of them and the audit findings. The chat shows them in a Sources panel: findings first, then sources the agent read, then search results only.

## The citation check

A mechanical check, per sentence of the final answer:

| Finding | Meaning |
| --- | --- |
| `fabricated` | A `[Sn]` marker or URL that no tool returned in this run |
| `snippet_only` | A figure that rests on a search result the agent never opened |
| `figure_mismatch` | A figure that is not in the cited excerpt |
| `uncited` | A figure that appears in a retrieved source but is stated without a marker. Figures from other tools (prices, a DCF) need no marker |
| `not_primary` | A secondary source cited in Primary sources only mode |

A passing answer is "citation checked". It is not "true": the check does not read whether prose supports a claim.

## Evidence mode

Chosen per browser in the chat (Evidence), sent as `evidence_mode` on `POST /api/ai/chat`. Every user sets their own; administrators have no say.

| Mode | Behavior |
| --- | --- |
| `standard` (default) | The answer streams as written; the check runs afterward and flags. |
| `evidence_only` | The answer is held until the check passes. A failing draft goes back to the model with its findings, at most twice. If problems remain, the last draft is shown with a warning that the flagged sentences are unverified. |
| `primary_only` | As `evidence_only`, and only primary filings count as support. |

The prompt in these modes asks the model to say "insufficient verifiable evidence" instead of filling gaps from memory, and to keep calculation, inference and opinion apart from reported fact.

## Not built yet

A tool to open web pages (`read_source`), quote and reachability checks, claim labels checked by the server, a block option that removes unsupported sentences, evidence mode for agent teams and flow nodes, a recency limit, and saved defaults per user or per saved team. Evaluation cases for contradictory sources and stale facts belong in `internal/eval`.
