# FloweySales

An open-source financial research workspace with a Go backend, an Angular frontend, Flow Studio automations, Research Agent, market-data plugins and a simulated paper portfolio. The Go binary serves the API, streaming events and built web UI.

**FloweySales now uses MySQL 8.4 for live application data. SQLite is only supported as an optional source for legacy imports.** A running MySQL database and applied Liquibase schema are required whether you use Docker, `make run` or the frontend dev server. The app does **not** automatically create a database or run migrations.

> Research and simulated trading only. Fully automated real-money trading is not enabled. External provider APIs may require credentials and permission to use the data.

## Quick start — Docker Compose (recommended)

Requires Docker Engine with the Compose plugin. This runs **MySQL, a one-shot Liquibase migration and the FloweySales app**. Go and Node.js are not required on the host.

```sh
cp .env.container.example .env
# Edit .env: set unique MYSQL_PASSWORD and MYSQL_ROOT_PASSWORD,
# AUTH_BOOTSTRAP_EMAIL and a strong AUTH_BOOTSTRAP_PASSWORD (14+ characters).
# Do not commit .env.
docker compose -f compose.yml up -d mysql
docker compose -f compose.yml --profile migration run --build --rm migrate
docker compose -f compose.yml up -d --build app
docker compose -f compose.yml ps
```

Open **http://localhost:8080** and sign in with the bootstrap admin credentials configured in `.env`. The admin account is created only when the identity database has no users. After the first successful login, **remove `AUTH_BOOTSTRAP_PASSWORD` from `.env`** and recreate the app:

```sh
docker compose -f compose.yml up -d --force-recreate app
```

The app also starts without any market-data plugin. Sign in as `SYS_ADMIN` and install your preferred providers under **Settings → Plugins**; not all sources work without credentials or acceptance of their terms.

Useful commands:

```sh
docker compose -f compose.yml logs -f app
docker compose -f compose.yml ps
docker compose -f compose.yml --profile migration run --rm migrate status --verbose
docker compose -f compose.yml down           # stops containers; keeps data
```

**Never use `docker compose down -v` unless you intend to delete the named data and encryption-key volumes.** Back up `mysql_data`, `secrets_data` and `plugins_data` before upgrades or restores. The app port is bound to **127.0.0.1** by default and `AUTH_COOKIE_SECURE=false` is only suitable for local HTTP. Use an HTTPS reverse proxy, secure cookies, and reviewed secret management before exposing the app remotely.

`make stack-up`, `make stack-status` and `make stack-down` are Makefile shortcuts for the same build-from-source `compose.yml` stack (after configuring `.env`):

```sh
make stack-up
make stack-status
make stack-down
```

For Podman, use `make stack-up COMPOSE='podman compose'` (and the same `COMPOSE` override for other stack targets). See [container operations and troubleshooting](docs/containers.md).

### Prebuilt images instead of local Docker builds

`compose.deploy.yml` is the **image-based deployment** configuration; `compose.yml` builds images from source. Prebuilt app and migration images are published to GHCR with release tags. Review [release/container deployment instructions](docs/containers.md) and [safe updates and backup procedures](docs/auto-updates.md) before running a deployed stack. Do not assume a Docker Compose `up` command applies database migrations automatically.

## Local development — Go backend + Angular hot reload

Requires Go **1.27.1+**, Node.js **24+**, npm, Docker Compose and a local MySQL database. The database is the same MySQL 8.4 schema used by containers.

### 1. Start MySQL and apply migrations

```sh
cp .env.identity.example .env
# Edit .env with unique development-only MYSQL_PASSWORD and MYSQL_ROOT_PASSWORD.
docker compose -f compose.identity.yml up -d mysql
docker compose -f compose.identity.yml --profile migration run --build --rm migrate
```

Unlike the full stack, `compose.identity.yml` publishes MySQL at **127.0.0.1:3306**, so the locally running Go binary can connect. The `.env` file is consumed by Compose, **not automatically by the host Go process**.

### 2. Configure the local Go process

In the shell that will run the backend, set credentials matching the MySQL user in `.env`:

```sh
export MYSQL_DSN='flowey_auth:YOUR_MYSQL_PASSWORD@tcp(127.0.0.1:3306)/floweysales_auth?parseTime=true&loc=UTC&tls=false'
export AUTH_ENABLED=true
export AUTH_BOOTSTRAP_EMAIL='admin@example.org'
export AUTH_BOOTSTRAP_PASSWORD='a-unique-development-password'
export AUTH_COOKIE_SECURE=false  # local HTTP ONLY
```

Choose the example password values yourself; replace `YOUR_MYSQL_PASSWORD` with the actual `MYSQL_PASSWORD` from `.env`. If your DB password contains reserved DSN characters, encode/configure the DSN correctly rather than copying it literally. **Do not commit credentials.** The `AUTH_BOOTSTRAP_*` values are only needed to create the first `SYS_ADMIN` account. Unset them after provisioning; existing users are not recreated.

### 3. Build and run

```sh
make deps      # install frontend npm packages and download Go modules
make run       # build Angular + Go + MCP; run Go app on :8080
```

Open **http://localhost:8080**.

**Important:** `make run` does not start MySQL, apply Liquibase migrations, or load `.env`; it works only after steps 1–2. For backend-only rebuilds use `make dev` (same database/environment requirements).

For frontend hot reload, keep the Go API running and in a second terminal execute:

```sh
cd frontend
npm ci
npm start
```

Open **http://localhost:4200**. Angular proxies `/api/*` to the Go backend on port 8080. Stop the database with `docker compose -f compose.identity.yml down` (the volume is retained). For this database-only stack, `make db-up`, `make db-migrate`, `make db-status` and `make db-down` are also available; see [database and auth](docs/database-and-auth.md).

## Common development commands

| Command | Purpose |
| --- | --- |
| `make help` | Show all Makefile targets |
| `make deps` | Install npm dependencies and download Go modules |
| `make frontend` | Build Angular to `www/browser` |
| `make backend` | Build the Go application binary `floweysales_backend` |
| `make mcp` | Build the optional MCP server `floweysales_mcp` |
| `make build` | Build Angular, Go backend and MCP server |
| `make run` | Build and run Go app; **requires configured MySQL and migrations** |
| `make dev` | Rebuild and run Go only; **requires configured MySQL** |
| `make test` | Run Go and plugin SDK unit tests |
| `make clean` | Remove local binaries and Angular build output |
| `make stack-up` / `make stack-down` | Start/stop the full Compose application stack |

`make run-mcp` starts the MCP server against a running backend on `:8080`. It does not start the backend itself.

## Tests

The mock-backed Playwright suite does **not** need a live MySQL database, credentials or external providers:

```sh
cd frontend
npm ci
npx playwright install chromium
npm run build
npm run e2e
```

`npm run e2e:ui` opens the interactive runner; `npm run e2e:report` opens the HTML report. Browser tests live in `frontend/e2e/` and use deterministic API fixtures. Some live or non-mock smoke tests require a running server and explicitly configured test data; see [testing and contributing](CONTRIBUTING.md).

Backend and SDK checks:

```sh
make test
go vet ./...
```

## Features

| Area | Description |
| --- | --- |
| **Command Center** | Today briefing, recent activity, tracking and shortcuts |
| **Markets / Financials** | Instrument lookup, charts, indicators, statements and research |
| **Market Collections** | User-curated instrument groups, research scopes and dashboards |
| **Research Agent** | Conversational analysis with inspectable (collapsible) tool traces |
| **Flow Studio** | Versioned multi-step research automations, history and review |
| **Wealth plan** | Goals, allocations and scenario analysis |
| **Paper portfolio** | Simulated orders, positions and risk views (not live brokerage trading) |
| **Files** | Private research workspace and flow artifacts |

See the [user guide](docs/guide/index.md), [architecture](docs/architecture.md), [roadmap](docs/roadmap.md) and [technical documentation index](docs/README.md).

## Providers, AI and configuration

Market data comes from separately installed plugins, maintained in [Doldas/floweysales-plugins](https://github.com/Doldas/floweysales-plugins). Install/manage them under **Settings → Plugins** or with the backend CLI, for example:

```sh
./floweysales_backend plugins install binance
./floweysales_backend plugins list
```

Plugins must comply with their API terms; some need user-provided keys and may be unavailable in certain regions. The app does not bundle live brokerage APIs. See [plugin architecture](docs/plugins.md).

The Go server requires `MYSQL_DSN` (or `AUTH_MYSQL_DSN` as a fallback). Other useful environment variables include:

| Variable | Purpose |
| --- | --- |
| `MYSQL_DSN` / `AUTH_MYSQL_DSN` | **Required** runtime MySQL connection (database must be migrated) |
| `AUTH_ENABLED` | Enable account sign-in; full Compose sets it to `true` |
| `AUTH_BOOTSTRAP_EMAIL` / `AUTH_BOOTSTRAP_PASSWORD` | One-time initial SYS_ADMIN setup |
| `AUTH_COOKIE_SECURE` | `false` only for local HTTP; `true` behind HTTPS |
| `FLOWEYSALES_SECRETS_KEY` / `FLOWEYSALES_SECRETS_KEY_FILE` | Integration secret encryption; preserve across restarts and backups |
| `REST_API_HOST` / `REST_API_PORT` | Bind address and port (default port 8080) |
| `PLUGIN_DIR` / `PLUGIN_STORE_URL` | Plugin storage and catalog |
| `OPENAI_BASE_URL` / `OPENAI_MODEL` / `OPENAI_API_KEY` | Optional OpenAI-compatible model configuration |
| `SEC_USER_AGENT` | Contact identification for SEC requests |
| `PAPER_TRADING_HALT` / `PAPER_REQUIRE_APPROVAL` | Simulated trading safety settings |

`config.properties.example` contains additional non-secret defaults, but the MySQL DSN and authentication bootstrap values are read from **environment variables**. `SQLITE_STORAGE_PATH` is **not** a runtime setting anymore. Legacy SQLite imports use a separately configured `LEGACY_SQLITE_PATH`/`SQLITE_IMPORT_PATH` and are not part of normal startup.

For production secrets, backups, authentication, migrations and deployments, read [database and authentication](docs/database-and-auth.md), [container setup](docs/containers.md) and [update operations](docs/auto-updates.md).

## License

FloweySales is released under the [MIT License](LICENSE). Third-party dependencies have their own licenses.
