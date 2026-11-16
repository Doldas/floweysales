# AI contributor guidance — FloweySales

This file applies to Codex and other coding agents working in this repository. Read [CONTRIBUTING.md](CONTRIBUTING.md) and [CLAUDE.md](CLAUDE.md) for build, security and review conventions. The human maintainer retains ownership and final approval.

## Commit attribution

For commits that **actually contain substantive assistance** from an AI coding agent, include a standard Git co-author trailer at the end of the commit message, after a blank line.

For Codex/OpenAI-assisted changes:

```text
Co-authored-by: Codex <codex@openai.com>
```

For Claude-assisted changes:

```text
Co-authored-by: Claude <noreply@anthropic.com>
```

- Credit only assistants that actually contributed to that commit. Do not add both names by default.
- Keep the authenticated human/maintainer Git author and committer unchanged unless they explicitly request otherwise; never impersonate a bot account.
- Preserve human co-author trailers and existing sign-offs; do not rewrite previously published history just to add AI credit.
- When preparing a squash-merge message, retain the relevant trailers from the included commits so the attribution survives the squash.
- If a connector or tool cannot set Git metadata or the co-author trailer, disclose the assistance in the PR description instead of claiming that GitHub recognized a co-author.
- A repository instruction can guide Git clients and coding agents but cannot override GitHub's authenticated PR author. GitHub contributor attribution depends on an email associated with the intended account.

These rules are provenance guidance, **not** permission to bypass reviews, protections, tests or the project's restrictions on financial data and live trading.
