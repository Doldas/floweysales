# Contributing to FloweySales

FloweySales is an open-source, local-first investment research platform. We use the same checks for human and agent-authored changes.

## Branch policy

- `main`: releasable integration branch; no direct pushes once repository rulesets are enabled.
- `feature/<slug>`: product capabilities.
- `fix/<slug>` or `bugfix/<slug>`: defect fixes.
- `docs/<slug>`: documentation.
- `chore/<slug>`: infrastructure / dependency updates.
- `release/<slug>`: stabilization only when a milestone needs a freeze.

Keep branches short-lived and open PRs **into `main`**. Every PR must reference an issue (`Relates to #N`) and pass `policy`, `go`, and `frontend` checks. Prefer squash merge; versioned `v*` tags trigger cross-platform binary artifacts. Unlike flowerly_os, FloweySales deliberately starts with lightweight trunk-based development rather than a mandatory chain of release branches.

## Local checks

Use Go version in `go.mod` and Node.js 24+:

```sh
go fmt ./...
go vet ./...
go test -race ./...
go build ./...
cd frontend && npm ci && npm run build
```

`npm run build` creates the Angular bundle; the GitHub Pages site in `site/` is **static documentation**, not a hosted trading engine.

## Safety rules

- Never commit provider credentials, account data, portfolio details, or trading tokens.
- Financial figures must show provider, timestamp and reporting period. Missing data must not become `0`.
- Distinguish prices, end-of-day closes, historical bars, simulations and forecasts.
- Every financial recommendation must be reproducible, dated, and include risk and uncertainty.
- Do not introduce unrestricted live-order execution or unattended trading. Such a change requires a separate security review, paper-trading tests and explicit approval.
- Changes to unofficial provider endpoints must follow terms, authentication limitations and rate limits; never bypass access controls.

## Pull requests

Describe the intent, linked issue, implementation, test evidence and any relevant provider/API or investment safety considerations. For larger decisions add an ADR in `docs/adr/`.
