# Live end-to-end testing of the deployed instance

The repository has **two separate** Playwright suites:

- `frontend/playwright.config.ts` runs deterministic, **mock-backed** tests of the locally built Angular app. It needs no credentials or internet-facing server.
- `frontend/playwright.live.config.ts` runs only against **https://floweysales.gamitronservers.com/** with real backend responses and a maintainer-provisioned autotest account. It never starts the mock server.

The live tests are a **manual GitHub Actions workflow**, not a deployment. They do not push code to the server or change databases. They help detect differences between the version actually deployed and CI fixtures.

## Maintainer setup (one time)

1. Create a separate autotest user in the deployed FloweySales installation. Use a strong unique password, **least-privilege permissions**, and only disposable/non-sensitive test data. It should be able to open Research Agent, Markets and Help. For opted-in real workflow runs, it additionally needs permissions to read and manually execute its own test flows; use a dedicated account whose flows contain only non-side-effecting nodes. No SYS_ADMIN, emails/notifications, order placement, external writes or unattended schedules. Keep AI tool grants disabled for live E2E workflow fixtures.
2. In GitHub, open `Doldas/floweysales` → **Settings → Environments**, create an environment named **`live-e2e`**. Add deployment branch restrictions (only `main`) and **required reviewers** if your plan supports them. GitHub environments are separate from GitHub Actions permissions.
3. Within that environment, configure **two environment secrets** (not repo files and not PR variables):
   - `LIVE_E2E_EMAIL`: test user's login email.
   - `LIVE_E2E_PASSWORD`: test user's password.
4. In the same environment add a **non-secret variable** `LIVE_E2E_FLOW_IDS` containing the IDs of one or two dedicated disabled `[E2E]` Flow Studio fixtures, e.g. `41,42`. Each must have a name beginning `[E2E] `, no schedule/event trigger, and only approved node types: trigger, constant, transform, json_get, condition, merge, output or agent (agent nodes without tool grants). Suitable fixture angles: deterministic data transformation with expected result, and an AI research-to-output chain with a real model. Do not put real private financial data into fixture inputs. The fixture must already exist; tests never generate arbitrary flows or edit user flows.
5. After the live test workflow has been merged to `main`, go to **Actions → Live Playwright E2E (deployed FloweySales) → Run workflow**, select `main`. Keep **run_ai_probe = false** for the first rehearsal.
6. Check the job's **live step-by-step GitHub Actions logs**, annotations and the short-lived `live-playwright-report-<run-id>` artifact for JUnit/HTML results and failure screenshots/video.

**Never paste a password in an issue, commit, PR comment or chat.** GitHub secrets and environment access must be restricted to trusted maintainers.

## What runs

Default: desktop Chromium and an emulated Pixel 7 viewport, **one worker at a time**:

- Login with the real `/api/auth/login` endpoint; verify the session with `/api/auth/me`.
- Load Help, Research Agent and Markets through the real running app and detect uncaught page exceptions.
- Verify Research Agent shows a configured model and an enabled message composer.

The optional **run_ai_probe** performs a **two-turn real Research Agent conversation** on desktop, checks both answers and confirms the interaction completes. This requires maintainer opt-in for each run and uses the real configured model. Research Agent may persist conversation history: do not assume this is read-only. Prompts alone are not a security boundary. Keep the test user's available tools, permissions and costs limited.

The optional **run_workflows** performs **up to two real saved Flow Studio executions** (desktop only) against IDs provided via `LIVE_E2E_FLOW_IDS`. Before running anything, the tests verify each is explicitly named `[E2E]`, disabled, unscheduled and made exclusively from the allowed node types listed above, with no agent tool grants. Playwright presses **Run now** in the real UI; it then polls the real `/api/admin/flows/{id}/runs` endpoint until `SUCCESS`, checks that the run is new, and verifies node-step traces exist. A failing run, missing history, wrong fixture or unexpected node type fails the job and needs investigation. Never use production/user flows as fixtures. A graph could still be configured to consume significant AI tokens, so use small prompts and narrow timeouts in the fixtures.

**Different angles to cover incrementally:** deterministic node chaining, branching/merge, invalid-graph validation (without executing), agent-to-output, explicit saved versus draft behavior (#273), trace auditability and retry/timeout cases. The first PR adds the framework plus a real persisted execution check; do not interpret this as full coverage of all Flow Studio features.

A Playwright job runs against the *currently deployed* version. The job's checkout SHA is the test code revision, **not proof that the deployment is on the same version**.

## Safety and failure evidence

- This workflow is `workflow_dispatch` only, runs only through a protected GitHub environment, has `contents: read` permissions and cannot deploy code. Do **not** put these credentials into a pull-request-triggered workflow or enable arbitrary external base URLs.
- Credentials are passed to the test process only as masked secrets and sent directly to the real login API through the browser context. The test does not save browser storage state to disk.
- **Tracing is disabled** for live runs because Playwright traces can contain cookies, credentials, request/response data, model output and financial information. On failure, screenshot/video and HTML/JUnit reports are retained for **2 days**; these can still expose the autotest account's visible data. Restrict repository Actions/artifact access, do not put sensitive personal information in the autotest account, and delete artifacts promptly if they capture something sensitive.
- By default, no flows are executed and no AI requests are made. The two opt-in test modes intentionally send messages or run **only dedicated, preverified** test flows; these may persist test-account run logs and incur real model costs. Plugin install/update, emails, paper orders, workspace writes, deletion and admin settings changes are not part of the suite.
- External APIs/AI/model providers can be flaky, slow, unavailable or rate-limited. A failing live E2E run means **investigate**; it is not necessarily a code regression. Never expose provider API keys through the test account.
- No automated schedules or post-deployment checks are enabled in this initial implementation. Add those only after validating authentication, test budgets, access controls, cost caps and reliability.
- GitHub Actions shows live **test progress/logs**; failure videos can be downloaded after the run. It is not an interactive stream of the browser screen. For a live browser video stream, a separate secured observer infrastructure would be required.

## Local rehearsal (never commit credentials)

```sh
cd frontend
npm ci
npx playwright install chromium
export LIVE_E2E_EMAIL='autotest@example.invalid'
# Provide your own real secret via a secure shell secret manager:
export LIVE_E2E_PASSWORD='...'
npx playwright test --config playwright.live.config.ts
# To enable a real two-turn conversation:
LIVE_E2E_AI=true npx playwright test --config playwright.live.config.ts
# To run approved [E2E] fixture workflows (up to two):
LIVE_E2E_FLOW_IDS=41,42 LIVE_E2E_WORKFLOWS=true npx playwright test --config playwright.live.config.ts
```

Only use the live test account, never a personal/admin account. The URL is hardcoded in `playwright.live.config.ts` and cannot be redirected to an arbitrary host by a PR variable.

## Follow-ups

See #274. Extend coverage only with stable, idempotent and observable test fixtures, redaction guarantees, a version marker for the deployed binary, bounded costs, permission checks, optional daily/scheduled smoke, and clean test data lifecycle. Destructive E2E must use a **separate disposable staging instance**, never this deployed production-like endpoint.
