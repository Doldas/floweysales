# Docker and Podman development stack

The repository root `compose.yml` is shared by Docker Compose and Podman Compose:
a Go/Angular app image (multi-stage build), MySQL 8.4 for identity, and a one-shot
Liquibase migration service. The existing SQLite trading database is still
persisted separately in the `trading_data` volume. No portfolio migration occurs.

## Docker Compose

```sh
cp .env.container.example .env
# Set unique credentials in .env; never commit this file.
docker compose -f compose.yml up -d mysql
docker compose -f compose.yml --profile migration run --build --rm migrate
docker compose -f compose.yml up -d --build app
```

Visit http://localhost:8080. First startup creates SYS_ADMIN only when the
identity database has no users, using the bootstrap credentials in .env.
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
# Destructive: docker compose -f compose.yml down -v (deletes trading, identity and plugin data)
```

`make stack-up` and `make stack-down` accept `COMPOSE='podman compose'`.
Do not publish the MySQL port or expose app port outside localhost while
`AUTH_COOKIE_SECURE=false`. Production needs HTTPS, cookie secure flag
`true`, proper secret management (Compose env is development only), a verified
restore process, session protection and separate user-owned portfolios.
Provider keys and AI server secrets must never be committed or logged.

For SYS_ADMIN-managed AI model settings see `docs/database-and-auth.md`.
