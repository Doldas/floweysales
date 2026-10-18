# GitHub operations and deployment

## Repository ruleset to enable (maintainer action)

Go to **Settings → Rules → Rulesets → New branch ruleset** on `Doldas/floweysales`. Target default branch `main`, enable:

- Require a pull request before merging; block force pushes and branch deletion.
- Require status checks `policy`, `go`, `frontend` after CI has run at least once. (GitHub can only select registered checks.)
- Require resolution of review conversations. Enable one independent approving review once other maintainers are available; a solo maintainer cannot approve their own PR.
- Allow squash merge as default. Protect any future `release/*` branches separately when needed.

The connector available during initial bootstrap cannot alter repository rulesets or configure Projects directly. Enabling these settings is a one-time administrative UI step; `policy` is a CI check, not a substitute for branch protection.

## GitHub Pages

Go to **Settings → Pages → Build and deployment → Source: GitHub Actions**. Merge the documentation workflow into `main`, then trigger **Documentation Pages** (or push a change under `site/`). The resulting URL should be `https://doldas.github.io/floweysales/`.

Pages only serves `site/`: a static project page. The backend, local model, private financial data and trading dashboard are **not** deployed to public Pages.

## CI / pre-release

The `CI` workflow builds and tests Go, vet/race checks and Angular. Artifacts: backend + MCP binaries and a frontend preview bundle. The `Release build` workflow builds platform-specific binaries on `v*` tags; artifacts are for download from the Actions run, not an automatic production deployment. Version tagging should only happen after passing checks and human review.

## Issue tracking and project board

Issues and milestone links are the source of truth in this bootstrap. To make a GitHub Projects (v2) board, use the GitHub Projects UI and add issues with columns **Backlog → Ready → In progress → Review → Done**. An actual Projects board is not created by these workflows.
