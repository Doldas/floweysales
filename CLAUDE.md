# Claude Code navigation — FloweySales

Read [`AGENTS.md`](AGENTS.md) for shared, truthful AI commit co-author attribution. Credit Claude only for commits it actually helped create; preserve the maintainer's author identity and all review requirements.

## Efficient navigation
- Go backend and agents: `cmd/`, Go packages and `pluginsdk/`. Angular UI: `frontend/`. Deployment: `compose*.yml`, `Dockerfile`; architecture in `docs/architecture.md`.
- Prefer relevant `docs/` pages over repeatedly loading the 23 KB README.
- Delegate nontrivial read-only search to `Explore` (Haiku). Request exact symbols and paths, not entire files.
- Use `log-triage` for existing long Go/Angular/Playwright logs. It must not run suites itself.
- For a Go-only change, test the affected package(s) first; run `make test` when broader validation is appropriate.
- For frontend changes, start with targeted Angular checks and Playwright specs; use documented full checks before declaring a broader change verified.
- Do not dump `node_modules/`, `www/browser/`, coverage, generated assets, entire test traces or lockfiles.
- For handoff use `/context-handoff`; `docs/agent-context/active-handoff.md` is a working note, not a substitute for checking Git and tests.

## Safety
- Treat authentication, approval paths, user data, and trading restrictions as security-sensitive. Do not assume live trading is supported.
- Keep external provider credentials out of prompts, logs, and documentation.
