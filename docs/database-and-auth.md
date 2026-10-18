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
The new MySQL database only stores the owner identity and login sessions.

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

On first boot the configured owner account is created **only if no account exists**.
After that, remove the bootstrap password from the environment; subsequent
runs do not overwrite it. Never commit these values or paste them in issues.
The bootstrap password must be >= 14 characters.

**Deployment:** Use TLS on the public endpoint, `AUTH_COOKIE_SECURE=true`
(the default), an HTTPS reverse proxy, and a TLS-authenticated MySQL
connection (`tls=true` and valid server CA). Do not publish MySQL's port
on a public interface. Set `AUTH_ENABLED=true` before exposing the web
dashboard; with auth disabled the old local development mode remains open.
There is intentionally no public registration or password-reset endpoint.

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

- `GET /api/auth/me`: returns authenticated owner when enabled, or
  `{ "enabled": false }` in local legacy mode.
- `POST /api/auth/login`: email + password; returns opaque HttpOnly session
  cookie, `SameSite=Strict`, time-limited; no JWT in browser storage.
- `POST /api/auth/logout`: deletes DB session and clears cookie.
- All other `/api/*` calls require a session when `AUTH_ENABLED=true`.
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
