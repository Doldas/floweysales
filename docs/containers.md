# Docker and Podman development stack

The repository root `compose.yml` is shared by Docker Compose and Podman Compose:
a Go/Angular app image (multi-stage build), MySQL 8.4 for **all live application
data**, and a one-shot Liquibase migration service. SQLite is **not** used for
runtime storage. The legacy `trading_data` volume is mounted read-only so an
explicit, opt-in historical SQLite import can be performed when necessary.
Do not delete legacy volumes until their data has been verified in MySQL.

## Docker Compose

```sh
cp .env.container.example .env
# Set unique MYSQL_PASSWORD, MYSQL_ROOT_PASSWORD, AUTH_BOOTSTRAP_EMAIL
# and AUTH_BOOTSTRAP_PASSWORD (14+ characters) in .env; never commit it.
docker compose -f compose.yml up -d mysql
docker compose -f compose.yml --profile migration run --build --rm migrate
docker compose -f compose.yml up -d --build app
```

Visit http://localhost:8080. First startup creates SYS_ADMIN only when
the MySQL identity tables have no users, using the bootstrap credentials in .env.
The application also reads/writes market data, flows and paper orders in MySQL.
The `migrate` step is mandatory before starting the application; `compose up`
does not apply Liquibase migrations automatically.
**Remove AUTH_BOOTSTRAP_PASSWORD from .env after provisioning**, then restart
app with `docker compose up -d app`. The existing account is not reset by
environment changes. Only expose the port through HTTPS when deploying off-host.

### Market-data plugins

The app starts without any market-data provider. Sign in as SYS_ADMIN and
install providers from **Settings → Plugins**, or from the host:

```sh
docker compose -f compose.yml exec app /app/floweysales_backend plugins install yahoo --accept-terms
docker compose -f compose.yml exec app /app/floweysales_backend plugins install binance
docker compose -f compose.yml restart app   # only needed after CLI changes
```

Plugins live in the `plugins_data` volume (`/plugins`), so they survive image
updates and container recreation. See [provider plugins](plugins.md).

## Podman Compose

Requires Podman and a supported Compose provider (`podman compose` delegates
to an installed `docker-compose` or `podman-compose`):

```sh
podman compose -f compose.yml up -d mysql
podman compose -f compose.yml --profile migration run --build --rm migrate
podman compose -f compose.yml up -d --build app
```

If your Podman Compose provider lacks profiles or healthy dependency conditions,
first start MySQL, execute the Liquibase container manually, then start the
app. Rootless Podman may need SELinux relabeling (`:Z` on bind mounts), and
UID-mapped persistent volumes must be writable by container UID 10001.
Check `podman logs` and `podman volume inspect`; do not use privileged
containers or disable SELinux as a workaround.

## Operations

```sh
docker compose -f compose.yml logs -f app
docker compose -f compose.yml --profile migration run --rm migrate status --verbose
docker compose -f compose.yml down             # preserves named volumes
# Destructive: docker compose -f compose.yml down -v (deletes MySQL, legacy-import, secret-key and plugin volumes)
```

`make stack-up` and `make stack-down` accept `COMPOSE='podman compose'`.
Do not publish the MySQL port or expose app port outside localhost while
`AUTH_COOKIE_SECURE=false`. Production needs HTTPS, cookie secure flag
`true`, proper secret management (Compose env is development only) and a verified
restore process. Back up the `mysql_data` volume **together with**
`secrets_data` (the encryption key) and `plugins_data`. The `trading_data`
volume is only for legacy SQLite imports; it is not the live order database.
Provider keys and AI server secrets must never be committed or logged.

For SYS_ADMIN-managed AI model settings see `docs/database-and-auth.md`.

## Published release images and opt-in updates

The release workflow publishes **both** the application and Liquibase migration images to GHCR with matching immutable version tags, for example `v0.6.0`. Prefer pinned versions for reproducible deployments:

```sh
# Set FLOWEYSALES_TAG=vX.Y.Z in the private .env first.
docker compose -f compose.deploy.yml pull app migrate
# Back up your data and use the checked host updater for the migration/restart.
bash scripts/auto-update.sh
```

The deployed images are defined by `compose.deploy.yml`, whereas `compose.yml`
builds them from the working tree. `compose.identity.yml` is a separate
MySQL-only development stack that publishes port 3306 on localhost; the full
stack keeps MySQL private to the Compose network.

The app's `stable` tag is advanced **only** through the manually approved GitHub workflow `Promote tested release to stable containers`; publishing a release candidate or stable GitHub Release does not automatically change running containers. The host updater checks for new app images, backs up MySQL, applies forward-only Liquibase migrations and verifies the app's health. It refuses to run migrations if **known app and migration image commit labels conflict**. Settings → Updates and the optional timer use the same host-side procedure (see `scripts/install-auto-update.sh`).

**Do not use** `docker compose down -v` to update; it destroys persistent data. A rolled-back application image does not undo the database migration: migrations must remain backward compatible or require a separately tested restore plan. Review [the release policy](releasing.md) and rehearse upgrades/rollback in an isolated environment before production.
