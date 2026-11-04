# Provenance coverage

Which surfaces describe the origin of what they show, using the contract in
[ADR 0003](adr/0003-provenance-contract.md). Issue #204 tracks this table.

| Surface | State | Notes |
| --- | --- | --- |
| World headlines (World page, company impact panel) | Covered | `provenance` on each headline, shown with `fs-source`. Web search results claim no publication time. |
| Flow news rows | Planned | Rows should carry the same fact. |
| Leaderboard | Planned | Methodology and input hash exist (`leaderboard_method.go`); not yet in the shared format. |
| Quotes and feed status | Planned | `QuoteSnapshot` already has source, as-of and fetched time. |
| Strategy results | Planned | Calculated; needs method ids. |
| Agent answers | Planned | Needs the source registry (#197). |
| Financial statements | Planned | |

States: **Covered** (API and UI), **Planned** (not started).
