# Live end-to-end testing of the deployed instance

The repository has **two separate** Playwright suites:

- `frontend/playwright.config.ts` runs deterministic, **mock-backed** tests of the locally built Angular app. It needs no credentials or internet-facing server.
- `frontend/playwright.live.config.ts` runs only against **https://floweysales.gamitronservers.com/** with real backend responses and a maintainer-provisioned autotest account. It never starts the mock server.

The live tests are a **manual GitHub Actions workflow**, not a deployment. They do not push code to the server or change databases. They help detect differences between the version actually deployed and CI fixtures.

## Maintainer setup (one time)

1. Create a separate autotest user in the deployed FloweySales installation. Use a strong unique password, **minimum read-only permissions**, and only disposable/non-sensitive test data. It should be able to open Research Agent, Markets and Help. No admin permissions, no emails/notifications, no order-placement permissions, no flow editing/running or external write tools. Keep AI tool permissions read-only.
2. In GitHub, open `Doldas/floweysales` → **Settings → Environments**, create an environment named **`live-e2e`**. Add deployment branch restrictions (only `main`) and **required reviewers** if your plan supports them. GitHub environments are separate from GitHub Actions permissions.
3. Within that environment, configure **two environment secrets** (not repo files and not PR variables):
   - `LIVE_E2E_EMAIL`: test user's login email.
   - `LIVE_E2E_PASSWORD`: test user's password.
4. After the live test workflow has been merged to `main`, go to **Actions → Live Playwright E2E (deployed FloweySales) → Run workflow**, select `main`. Keep **run_ai_probe = false** for the first rehearsal.
5. Check the job's **live step-by-step GitHub Actions logs**, annotations and the short-lived `live-playwright-report-<run-id>` artifact for JUnit/HTML results and failure screenshots/video.

**Never paste a password in an issue, commit, PR comment or chat.** GitHub secrets and environment access must be restricted to trusted maintainers.

## What runs

Default: desktop Chromium and an emulated Pixel 7 viewport, **one worker at a time**:

- Login with the real `/api/auth/login` endpoint; verify the session with `/api/auth/me`.
- Load Help, Research Agent and Markets through the real running app and detect uncaught page exceptions.
- Verify Research Agent shows a configured model and an enabled message composer.

The optional **run_ai_probe** performs **one** short, low-cost, general-knowledge question on desktop only. This requires maintainer opt-in for each run and uses the test user's real configured model. It should not need provider tools and explicitly asks the agent not to make changes, but **prompt instructions alone are not a security boundary**: use an account without write capabilities, review the role's permissions, control API costs and ensure the configured agent/tool policy cannot perform dangerous actions. Do not use this probe against a privileged account. It might still create a chat entry/activity record in the test account. Do not assume it performs zero writes.

A Playwright job runs against the *currently deployed* version. The job's checkout SHA is the test code revision, **not proof that the deployment is on the same version**.

## Safety and failure evidence

- This workflow is `workflow_dispatch` only, runs only through a protected GitHub environment, has `contents: read` permissions and cannot deploy code. Do **not** put these credentials into a pull-request-triggered workflow or enable arbitrary external base URLs.
- Credentials are passed to the test process only as masked secrets and sent directly to the real login API through the browser context. The test does not save browser storage state to disk.
- **Tracing is disabled** for live runs because Playwright traces can contain cookies, credentials, request/response data, model output and financial information. On failure, screenshot/video and HTML/JUnit reports are retained for **2 days**; these can still expose the autotest account's visible data. Restrict repository Actions/artifact access, do not put sensitive personal information in the autotest account, and delete artifacts promptly if they capture something sensitive.
- No flow execution, plugins install/update, emails, paper orders, workspace writes, deletion or admin settings changes are exercised by the default suite.
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
# To enable exactly one additional AI question:
LIVE_E2E_AI=true npx playwright test --config playwright.live.config.ts
```

Only use the live test account, never a personal/admin account. The URL is hardcoded in `playwright.live.config.ts` and cannot be redirected to an arbitrary host by a PR variable.

## Follow-ups

See #274. Extend coverage only with stable, idempotent and observable test fixtures, redaction guarantees, a version marker for the deployed binary, bounded costs, permission checks, optional daily/scheduled smoke, and clean test data lifecycle. Destructive E2E must use a **separate disposable staging instance**, never this deployed production-like endpoint.
