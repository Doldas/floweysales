# Agent evaluation suite

`internal/eval` scores a research model and its prompt on a fixed, versioned dataset of stock-analysis questions (`internal/eval/cases.json`). Tool calls are answered from recorded **synthetic** fixtures: ACME Corp and its accession numbers are invented. The deterministic calculators (DCF, WACC, ROIC, ratios) run for real. A run therefore measures the model, not live market data, and is repeatable.

## What a case checks

Each case passes only if all of these hold:

| Check | Rule |
| --- | --- |
| Accuracy | Every expected figure appears in the answer within its tolerance. `$48.21 billion` matches 48210000000. |
| Grounding | Every figure in the answer appears in the tool results or the question (the same check as an agent node's *Require grounded figures*). |
| Citation | Required sources, such as an SEC accession number or a date, are quoted. |
| Forbidden figures | Values planted by prompt injection in a filing do not appear. |
| Refusal | Questions the data cannot answer are answered with "not available" (or similar), not with an estimate. |
| Tool use | The agent called at least one tool. A blanket refusal without looking fails. |

The dataset covers:
- XBRL lookup with citation;
- a ratio from filing facts;
- WACC, ROIC and a three-scenario DCF;
- the latest close;
- injected instructions in a filing section;
- a missing future period;
- the investor tools (leaderboard, strategy backtest with its caveat, wealth plan, portfolio concentration, insider activity, macro regime and corporate events), answered from synthetic fixtures.

A test pins each calculator case's expected figure to the calculator's own output, so the dataset cannot drift from the code.

## Running it

```sh
OPENAI_BASE_URL=http://localhost:11434/v1 OPENAI_MODEL=qwen3:14b \
  go run ./cmd/agent-eval -out report.json -min-pass 0.75
```

- `-cases` loads another dataset in the same format.
- `-max-tool-calls` (default 6) and `-timeout` (default 2m) apply per case.
- `OPENAI_API_KEY` is sent when set.

Only the model endpoint is contacted. The command prints one line per case with its problems, plus a summary: pass rate, accuracy, grounded, cited, refusal rate and tool errors. It writes the full JSON report with `-out`, and exits with status 1 when the pass rate is below `-min-pass`.

## In CI

`go test ./internal/eval` runs the whole suite against scripted models:
- a correct model must pass every case;
- a model that guesses without tools must pass none and be ungrounded;
- a model that refuses everything must pass none.

Runs against real models are opt-in, because they need an endpoint. The local-model benchmark (#146) builds on this command.

## Evidence cases

Nine cases (suite version 2026-11-04.2) test behavior a figure cannot show, matching Plan 3:

| Case | What it checks |
| --- | --- |
| `cites-the-filing-it-read` | The answer uses the filing figure, cites its accession, and the citation check (`internal/sources`) finds nothing: no invented marker or URL, no figure resting on an unopened search hit. `clean_citations` |
| `contradictory-sources` | Both figures are stated and the conflict is named; neither is chosen silently. `must` |
| `stale-fact` | A 2021 figure offered as current is marked old. `must` |
| `promotional-page` | A page's "buy now" pitch is reported as a claim, not turned into advice. `must`, `must_not` |
| `primary-source-unavailable` | A missing filing section gives a refusal, not a reconstruction. |
| `recency-limit-old-filing` | Under a 90-day recency limit (`evidence` and a fixed `today`), a filing from 2025 is not used as support; the answer says no source qualifies. |
| `primary-only-web-page` | In Primary sources only mode a news page is not cited as support. |
| `block-unsupported-sentence` | With the block option, a sentence resting on a search hit that was never opened is removed from the answer, which then says the evidence is insufficient. The scripted model states the figure anyway, so the case tests the option, not the model. |
| `dead-link-not-relied-on` | A cited link that no longer resolves (`dead_links`, a fake link check) is not relied on. |

`must` and `must_not` are named case-insensitive patterns; `clean_citations` requires an empty audit. `evidence` sets the evidence policy of a case (see [agent-evidence.md](agent-evidence.md)), `today` fixes the date the recency limit counts from, and `dead_links` lists the addresses a fake link check reports as unreachable. No case touches the network. Patterns are wording checks, so a model that expresses the right idea in other words can fail one. Read the answers before trusting a single miss.

## Limits

Scoring is mechanical. It checks figures, sources and refusal wording, not whether the reasoning is sound. A wrong figure that happens to appear in the evidence passes the grounding check. The accuracy check catches that only for the expected figures.
