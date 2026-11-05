# GitHub operations and deployment

## Repository ruleset — current verified gaps (maintainer action)

**Audit on 2026-10-24:** The active `protect-main` [ruleset](https://github.com/Doldas/floweysales/rules/25357610) already targets the default branch and enforces PRs, branch deletion and non-fast-forward restrictions. However, the rule `required_status_checks` currently has an **empty list**, and the owner has an **always** bypass. This means CI is *running* but its success is **not currently mandatory** for all merges. Fix these settings before declaring foundation issue #1 complete.

Go to **Settings → Rules → Rulesets → New branch ruleset** on `Doldas/floweysales`. Target default branch `main`, enable:

- Require a pull request before merging; block force pushes and branch deletion.
- Open existing `protect-main` (do not create an overlapping ruleset). Under Required status checks, add **`policy`, `go`, `frontend`** and require the branch to be up to date before merging. GitHub can select these already registered job names.
- Remove the owner's **Always bypass** (or restrict bypass to an emergency exception with explicit accountable process). Do not bypass required checks during routine development.
- Require resolution of review conversations. Enable one independent approving review once other maintainers are available; a solo maintainer cannot approve their own PR.
- Allow squash merge as default. Protect any future `release/*` branches separately when needed.

The connector available during initial bootstrap cannot alter repository rulesets or configure Projects directly. Enabling these settings is a one-time administrative UI step; `policy` is a CI check, not a substitute for branch protection.

### Read-only automated audit

Run `node --test scripts/audit-repository-gates.test.mjs` to test the gate-audit logic offline. Run `node scripts/audit-repository-gates.mjs` to check the current public GitHub ruleset plus latest Documentation Pages workflow status (set `GITHUB_TOKEN` to avoid API rate limits). Exit status 1 reports failed controls; status 2 means the GitHub API could not be queried. The audit **does not change settings** and cannot prove a Projects board exists. Unit tests run inside the normal `policy` CI job, but the live audit is **manual** so an unrelated GitHub settings issue does not silently rewrite or bypass enforcement.

## GitHub Pages

**Verified 2026-10-24:** The latest [Documentation Pages workflow run](https://github.com/Doldas/floweysales/actions/runs/40078945959) successfully completed both `pages-build` and `pages-deploy`. The expected URL is `https://doldas.github.io/floweysales/`. This verifies GitHub's deploy job; a separate external browser visit is still useful for final site-content acceptance.

Pages only serves `site/`: a static project page. The backend, local model, private financial data and trading dashboard are **not** deployed to public Pages.

## CI / pre-release

The `CI` workflow builds and tests Go, vet/race checks and Angular. Artifacts: backend + MCP binaries and a frontend preview bundle. The `Release build and publish` workflow, triggered by validated `vMAJOR.MINOR.PATCH[-rc.N]` tags on `main`, produces durable GitHub Release binaries/checksums and **matching versioned GHCR app and migration images**. The separate SDK release workflow handles `pluginsdk/v*` tags and checks official plugin consumers. Neither workflow deploys to running servers. Promotion of versioned images to the mutable `stable` channel is a separate, manually dispatched workflow (`Promote tested release to stable containers`) that should use GitHub Environment reviewer protection. See the full [release and rollback policy](releasing.md). Version tagging and stable promotion require passing checks and human review.

## Issue tracking and project board

Issues and milestone links are the source of truth in this bootstrap. To make a GitHub Projects (v2) board, use the GitHub Projects UI and add issues with columns **Backlog → Ready → In progress → Review → Done**. An actual Projects board is not created by these workflows. Its existence was **not verified** during the 2026-10-24 audit. If still required for issue #1, create it under the repository owner's GitHub Projects interface and link the roadmap issues; the currently connected GitHub integration does not expose a Projects v2 creation action.
