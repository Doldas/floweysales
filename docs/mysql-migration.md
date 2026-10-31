# MySQL-only trading storage: manual one-time migration

**This change must not overwrite or discard historical SQLite volumes.**
MySQL 8.4 becomes the one live database for identities, settings, trading
history, orders and forum posts. The app no longer opens SQLite at runtime;
SQLite is used **only** by the offline one-time import tool.

## Before upgrading an existing deployment

1. Pause the **old app** and turn off automated updates until verified.
2. Take and verify a MySQL backup (`mysqldump`) and SQLite backup (use
   SQLite's `.backup` while old app is stopped); keep both offline.
3. Apply the additive Liquibase update:
   `docker compose -f compose.deploy.yml --profile migration run --rm migrate update`
4. Obtain the old file from the existing `trading_data` named volume without
   deleting or recreating the volume. Map it **read-only** to the importer.
5. Run `docker compose -f compose.deploy.yml --profile legacy-import run --build --rm import-legacy` (or `MYSQL_DSN=... SQLITE_IMPORT_PATH=/path/floweysales.db go run ./cmd/migrate-sqlite`)
   on a host with database access, or build the command as an explicit
   one-time container. Do not run it while the old app is writing.
6. Confirm row counts in MySQL `trading_history`, `orders` and
   `forum_posts`, orders and positions, then start the new application.

### Verify the SQLite-to-MySQL import (read-only)

Before starting the new app, while the old app is stopped and no process is
writing to either database, run the importer in **verification mode**:

```sh
# Uses the same read-only SQLite volume mapping and MySQL connection.
docker compose -f compose.deploy.yml --profile legacy-import \
  run --build --rm import-legacy --verify
```

When running the CLI directly:

```sh
SQLITE_IMPORT_PATH=/path/to/floweysales.db MYSQL_DSN='...' \
  go run ./cmd/migrate-sqlite --verify
```

This mode **does not import or modify** anything. It compares record counts,
minimum ID and maximum ID in `trading_history`, `orders` and `forum_posts`
and exits unsuccessfully on any mismatch or database error. It is a
**structural check, not a full row-by-row or financial reconciliation**:
values can differ even when counts and ID bounds match. Independently inspect
sample prices, orders, timestamps, positions and balances before retiring the
legacy volume, and retain verified backups. Avoid running verification while
trackers are adding new MySQL ticks, since counts would legitimately differ.

The importer operates in one MySQL transaction, preserves original IDs,
checks that all three MySQL target tables are empty first, and uses a
**read-only SQLite connection**. If any target contains data, it refuses
the import rather than risking a duplicate or partial merge. The source
file is **never deleted or mutated** by the migration.

The new application has a fail-closed startup check: if an old SQLite
file is still mounted, but all MySQL trading tables are empty, it will
not silently present a blank portfolio. An empty *new* deployment with
no SQLite file is allowed.

The Compose `trading_data` volume is retained, read-only, solely for
legacy detection/migration. Once row counts, data and backups have been
verified, an administrator can remove the legacy volume via a separate
documented maintenance operation. Never issue `docker compose down -v`
or an automatic destructive cleanup.

## Fresh install

Set `MYSQL_DSN` and `AUTH_MYSQL_DSN` to the same MySQL database
(the provided Compose environment does this), run Liquibase, start app.
Both identity and trading tables share the persistent `mysql_data` volume.
There is no required SQLite volume for fresh installations.

**Caution:** migrating the storage schema alone does not eliminate all
possible pod memory pressure: the application cache and multi-tracker
work are managed and tested independently in the same change series.

## Memory, email and concurrency

- REST history cache has a hard 32-key limit; financial cache 48 keys.
  SSE subscribers are capped at 64 and their channel buffers at four ticks.
- Each symbol+provider has independent EMA/stop state, bounded to 16 active
  streams. Polling is clamped to each provider plugin's declared
  `min_poll_seconds` to avoid a single configuration storm after a pod
  restart (bundled plugins: Finnhub 20s, Avanza 10s, Yahoo 5s, Binance 2s).
- Console per-tick/tracking debug logs are removed. Market ticks and
  simulated orders remain persisted in MySQL.
- Optional SMTP alerts are disabled by default. Set
  `ENABLE_TRACK_ALERT_EMAIL=true` with real SMTP credentials to enable.
  SMTP connections are time-bounded; at most two delivery goroutines.
- The backend Compose service defaults to `GOMEMLIMIT=256MiB` and a
  512 MiB container memory limit. Tune for your workload; production
  Kubernetes manifests should set resource requests/limits separately.
- Tracker lifecycle: `GET /api/trackers`, `POST /api/trackers`
  with `{"symbol":"NVDA","provider":"YAHOO"}`,
  `DELETE /api/trackers?symbol=NVDA&provider=YAHOO`.
  These administrative operations update the MySQL `tracked_assets` table,
  so enabled symbols survive restarts. Existing `POST /api/config`
  selects the primary dashboard asset **and adds it to the active set**
  instead of stopping other trackers.

## Docker / Podman import commands

Use the same `.env`, `mysql_data`, and `trading_data` volumes as the
existing installation. The importer is explicitly opt-in, built from
`Dockerfile --target legacy-importer`, and its source SQLite volume is
mounted **read-only**. It does not start automatically in Compose.

```sh
# Stop writers and take verified backups first.
docker compose -f compose.deploy.yml stop app
docker compose -f compose.deploy.yml --profile migration run --rm migrate update
docker compose -f compose.deploy.yml --profile legacy-import run --build --rm import-legacy
# Check rows and portfolio results; only then restart.
docker compose -f compose.deploy.yml up -d app
```

Replace `docker` with `podman` on hosts supporting `podman compose`.
If you no longer have the old SQLite database, **do not manufacture an
empty placeholder**: fresh MySQL installation seeds the default tracker.
The legacy `trading_data` volume is retained until explicitly retired
after successful import and backups.

## Driftstatus utan tracking-loggar

`GET /healthz` svarar `200 ok` när MySQL går att nå eller `503` när
anslutningen är nere. Den returnerar varken lösenord eller tickhistorik.

`GET /api/admin/runtime` kräver `SYS_ADMIN` när autentisering är
aktiverad och ger aktuell heap, antal goroutiner, MySQL-poolanslutningar,
aktiva trackers, cachenycklar och SSE-klienter. Använd denna vy för att
jämföra minnesanvändning över tid. Den skriver inte tracking-loggar.

`GET /api/orders` har nu en skyddad sidstorlek på 500 (högst 2000)
och stöder `limit` samt `offset`; den fullständiga orderhistoriken
behålls i MySQL och i beräkningarna av positioner och saldon.

### Trackerstatus utan extra provider-anrop

`GET /api/trackers/status` (SYS_ADMIN när inloggning är aktiverad)
returnerar de högst 16 aktiva symbol/provider-strömmarna med
`has_tick`, senaste sparade `last_tick_timestamp` och
`age_seconds` från MySQL. Nya trackers utan lagrade observationer har
`has_tick: false` och null för tid/ålder. Databasfel ger HTTP 503.
Detta anropar inga externa marknadsleverantörer och startar ingen tracker.
Åldern är en observationsålder, inte en garanti för att en leverantör är
nere: stängda marknader kan naturligt sakna färska ticks.
