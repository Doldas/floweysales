# Database architecture: Liquibase + MySQL identity store

## Decision

**Use a relational database** for users, sessions, future tenant-scoped portfolios,
holdings, orders and audit history. MySQL 8.4 is selected for the first identity
service because it provides referential integrity, transactions and mature
Liquibase support. PostgreSQL is a similarly good alternative; MongoDB is
not needed for this core relational model.

This is an **incremental migration**. The existing SQLite database continues
to store the current single-account paper-trading/market history. It is NOT
migrated by this change, and its tables are not altered by Liquibase.
The new MySQL database stores SYS_ADMIN and USER identities, login sessions, and persisted nonsecret AI model settings.

**USER accounts are research-only.** SYS_ADMIN may provision them through a secured endpoint, but regular users cannot access shared portfolio, order, tick-history, configuration, or mutating endpoints. The AI agent's non-user-scoped tools are also disabled for USER until proper tenant scoping exists. No public self-registration is offered.

**No multi-tenant accounts are exposed yet.** The paper portfolio and trading
engine are currently shared process-wide; offering public sign-up would
leak financial data across users. Implement per-user ownership, row-level
authorization and a tenant migration before enabling additional accounts.

## Local MySQL and schema migration

Requires Docker with Compose.

```sh
cp .env.identity.example .env
# Edit .env, use unique development-only passwords
docker compose -f compose.identity.yml up -d mysql
docker compose -f compose.identity.yml --profile migration run --rm migrate validate
docker compose -f compose.identity.yml --profile migration run --rm migrate update
```

The Liquibase changelog is `db/changelog/db.changelog-master.xml`.
Its `DATABASECHANGELOG` and `DATABASECHANGELOGLOCK` tracking tables live in
MySQL. Migrations run explicitly before starting the Go process, not
opportunistically from HTTP handlers. Treat applied changeSets as immutable.

Configure the backend (example only; use your own passwords):

```sh
export AUTH_ENABLED=true
export AUTH_MYSQL_DSN='flowey_auth:YOUR_PASSWORD@tcp(127.0.0.1:3306)/floweysales_auth?parseTime=true&loc=UTC&tls=false'
export AUTH_BOOTSTRAP_EMAIL='you@example.org'
export AUTH_BOOTSTRAP_PASSWORD='strong-and-unique-password'
# Only for local plain-HTTP development:
export AUTH_COOKIE_SECURE=false
make run
```

On first boot the configured SYS_ADMIN account is created **only if no account exists**.
After that, remove the bootstrap password from the environment; subsequent
runs do not overwrite it. Never commit these values or paste them in issues.
The bootstrap password must be >= 14 characters.

**Deployment:** Use TLS on the public endpoint, `AUTH_COOKIE_SECURE=true`
(the default), an HTTPS reverse proxy, and a TLS-authenticated MySQL
connection (`tls=true` and valid server CA). Do not publish MySQL's port
on a public interface. Set `AUTH_ENABLED=true` before exposing the web
dashboard; with auth disabled the old local development mode remains open.
There is intentionally no public registration or password-reset endpoint.

## Roles and AI administration

- `SYS_ADMIN`: owner role with protected access to AI configuration, orders,
  global market tracking and user provisioning.
- `USER`: safe research endpoints, fundamental reports and an AI chat with the
  server-selected model. Cannot override model/URL/API key/system prompt in chat
  requests, call agent tools on shared records, or access global orderbook.
- Legacy `owner` rows are updated to `SYS_ADMIN` by migration
  `db/changelog/002-rbac-ai.xml`. The original 001 migration stays immutable.
- AI settings `base_url` and `model` are persisted to MySQL by
  `PUT /api/ai/config`; secret `OPENAI_API_KEY` remains environment-only.
  These settings persist after application restart.
- A SYS_ADMIN provisions a USER by sending
  `POST /api/admin/users` with `{"email":"person@example.org","password":"14+ unique characters"}`.
  This does **not** create a SYS_ADMIN. There is no public registration.
- Creating users is a deliberately administrative API operation for now. Use
  an authenticated browser/session or a same-origin admin client. Avoid printing
  credentials in shell history. Prefer future invitation-based onboarding.

## Containers

Both Docker Compose and Podman Compose can run the full application, database
and Liquibase. See [containers.md](containers.md) and
`compose.yml`. The existing `compose.identity.yml` is for DB-only local
development. Never use `down -v` on a volume containing financial history.

## CLI

```sh
make db-up
make db-validate
make db-migrate
make db-status
```

The Compose `migrate` container defaults to `update`. For status:
`docker compose -f compose.identity.yml --profile migration run --rm migrate status --verbose`.

## Auth contracts

- `GET /api/auth/me`: returns authenticated user with role when enabled, or
  `{ "enabled": false }` in local legacy mode.
- `POST /api/auth/login`: email + password; returns opaque HttpOnly session
  cookie, `SameSite=Strict`, time-limited; no JWT in browser storage.
- `POST /api/auth/logout`: deletes DB session and clears cookie.
- All other `/api/*` calls require a session when `AUTH_ENABLED=true`; individual requests are also authorized by role.
- Browser login and logout operate over the same origin, with CSRF origin
  checking on mutating requests.

**Limitations before any internet-facing deployment:** implement centralized
rate limits and account recovery/MFA, external penetration testing, and
isolate all portfolio, forum and research data per account. Sessions are
single-server compatible via MySQL, but login-attempt throttling is initially
per process.

## SQLite and backfills

Existing SQLite DDL and old in-process migrations remain active for existing
users. A separate future migration project will baseline that schema into
Liquibase before retiring Go DDL. Do not run MySQL changelogs against SQLite.

## Configurable AI budgets and sourced search

As `SYS_ADMIN`, set **model rounds (1–32)** and **tool calls (1–128)**
in the AI settings panel or `PUT /api/ai/config` with
`base_url`, `model`, `max_steps` and `max_tool_calls`.
Both budgets are stored in Liquibase-managed `ai_configuration` and
restored on application restart. The default is 12 rounds / 48 tool calls.
`AI_MAX_STEPS` and `AI_MAX_TOOL_CALLS` are environment defaults until
the administrator saves overriding values. A model round is one completion,
not necessarily one tool invocation; multiple tool calls in a round count
individually. The agent reports a partial/incomplete task upon exhaustion.

To enable web search configure `BRAVE_SEARCH_API_KEY` as a **server-side
environment variable** (API subscription/token from Brave Search). The
`search_web` tool searches public pages and includes URLs, date hints and
short snippets. Web results are untrusted and must be cross-checked against
primary official financial filings. No browser or AI chat field can change
the Brave API key. Without a key the tool gives an explicit unavailable error.

Correct valuation: `calculate_financial_ratios` requires a reporting
`period` (quarter/ttm/annual). Quarterly net income or FCF never yields a
P/E or P/FCF. `calculate_ttm_valuation` requires exactly four quarterly EPS
values and share price for P/E, or four quarterly FCF values and market cap
for P/FCF. All inputs must use matching currency and financial periods.
Incomplete quarters do not produce invented numbers.

See [automatic Docker/Podman updates](auto-updates.md). The updater preserves
MySQL identities, sessions, persisted AI settings, local `.env` and SQLite data.


## Optional paper-trading order-size guard

Set `PAPER_MAX_ORDER_NOTIONAL=1000` in the backend environment to reject
simulated orders whose worst-case quantity × execution/reference price exceeds
1000 units of that instrument's quote currency. The default `0` disables this
new guard to preserve existing behavior. Negative or non-finite values are
invalid. This is **paper trading only**, not a broker risk engine. No currency
conversion is performed, so the cap is per-order in the symbol's quote currency,
**not** a portfolio-wide USD or SEK limit. Existing orders are not altered.
This guard is a first incremental portion of issue #7; it does not yet
implement portfolio drawdown limits, exposure concentration or a kill switch.

## Emergency paper-trading halt (issue #7, partial)

Set `PAPER_TRADING_HALT=true` in the app environment and **restart/recreate the app** to stop all new simulated order submissions and prevent resting simulated orders from matching subsequent ticks. Reading existing orders and manually cancelling OPEN orders remain available. Set back to `false` and restart only after investigating the problem. The toggle is read at startup; changing a Compose env file alone does **not** affect a running process.

This is a software-only safety stop for paper trading. It does **not** cancel existing orders automatically, stop external market data polling, or provide broker-side controls. Do not use it to authorize real-money execution. The issue also requires persisted approvals, request idempotency, risk concentration/drawdown controls and failure testing before it can be closed.

## Order lifecycle safety (issue #7, incremental)

`PATCH /api/orders` permits cancellation and rejection of *OPEN* simulated
orders only. Manual transitions to `FILLED` are blocked: marketable execution
must come from the paper trading match engine, including fill price and fees.
MySQL uses a conditional update (`WHERE status = 'OPEN'`) to prevent a
late cancellation request from changing an order already filled by a tick.
Clients receive HTTP 409 when their requested lifecycle transition is stale.
This is not yet a complete audit trail or a durable idempotency protocol.

## Idempotent paper-order retries (issue #7, partial)

Send a stable `client_order_id` (1–128 ASCII letters/digits, `.`, `_`, `:`,
or `-`) in `POST /api/orders`. The request is hashed excluding the key,
and MySQL atomically commits the simulated order and the unique key in the
same transaction. Repeating the same payload/key returns the original order's
**current** lifecycle with `replayed: true`, without executing a second
purchase. Reusing a key for different parameters returns HTTP 409. Keep the
same key when retrying after a lost response, including after an app restart.
Requests without a key retain legacy non-idempotent behavior; clients requiring
retry safety **must** send one. Keys apply to the shared paper account, not per
user. The migration is additive and requires Liquibase update before deploying
the application. This is not an approval system or a replacement for
transactional portfolio-wide buying-power reservation.

## Per-symbol paper portfolio allocation cap

Set `PAPER_MAX_SYMBOL_ALLOCATION_PCT=20` to reject new **BUY** paper orders
whose worst-case notional, added to existing long symbol market value and
other OPEN BUY orders for the same symbol, exceeds 20% of current paper
portfolio equity. The default is `0` (disabled). Valid enabled range is
greater than 0 through 100. Sell orders remain permitted so users can reduce
risk. This is a *best-effort application-layer* guard. It does not provide
atomic buying-power reservation for different simultaneous requests, and
assumes portfolio values have comparable currencies; it is **not** a safe
cross-currency consolidated portfolio limit. Set only when instrument and
portfolio currencies are comparable. Further portfolio-wide atomic limits,
sector/liquidity controls, drawdown gating and explicit approvals remain #7.


## Paper-order lifecycle audit trail (issue #7, partial)

Liquibase change 006 adds `paper_order_events`. The application records
order creation and status changes in the **same MySQL transaction** as the
corresponding order write, without requiring privileged MySQL triggers. `GET /api/orders/{id}/events` returns the newest 100
events for an existing order (404 if missing, 503 if audit storage is down).
Events include old/new status and the stored cancellation reason. This also
covers orders created using idempotency keys and matching-engine fills; replay
without a new order does not create a duplicate creation event.

**Scope:** the audit shows persisted lifecycle changes, not who authorized
them, nor a complete decision-to-recommendation-to-approval chain. No
human-approval workflow or real-money execution is introduced. Events from
before deployment of this additive migration cannot be reconstructed
automatically. Do not edit/delete the audit table manually.


## Opt-in paper order approval gate (#7)

Set `PAPER_REQUIRE_APPROVAL=true` and **enable authenticated SYS_ADMIN sessions**.
When enabled, requests to `POST /api/orders` fail closed without an
authenticated administrator and a valid `client_order_id`. The workflow is:

1. Authenticate as SYS_ADMIN. Send the intended **exact order JSON**, including
   `client_order_id`, to `POST /api/orders/approval-requests`. The response
   gives a SHA-256 request hash. This stage creates no executable order.
2. After explicitly reviewing the request, an authenticated SYS_ADMIN calls
   `POST /api/orders/approval-requests/{client_order_id}/approve`. The actor ID
   is durably stored. `GET /api/orders/approval-requests/{client_order_id}`
   shows requester, approval actor and consumption status.
3. Send **the identical JSON** to `POST /api/orders`. Risk validation runs
   against current portfolio and prices. MySQL atomically consumes the matching
   unused approval and inserts the idempotent paper order. Replays with the
   same key return the existing order without another execution.

Changing any field after approval invalidates the request hash. A failed risk
check does not consume approval. There is **no live broker integration**.
The default is disabled to retain the legacy paper-ticket behavior. When
enabled without authentication, order creation fails closed rather than
bypassing approval. This first workflow does not enforce a second distinct
approver, expiry windows, or transaction-wide cash/exposure reservations;
those are required before issue #7 can be considered fully complete.
