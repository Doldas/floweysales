# MySQL-only multi-tracker runtime

All live storage now uses MySQL 8.4: auth/users, sessions, persisted AI config,
trading_history, orders, forum_posts and the `tracked_assets` watchlist.
Liquibase `004-mysql-trading.xml` defines the new tables. The runtime has no
SQLite driver, file path, writable SQLite volume or fallback. Run Liquibase
before starting the new app or startup fails intentionally.

A single poll loop manages **up to 12 active symbols**. Each has independent
EMA/stop-loss state, and changing the selected chart ticker no longer stops
other trackers. Tracking selections survive app restarts in MySQL. Polls are
round-robin: with 12 tracked symbols and a global 1s poll interval, each symbol
is queried roughly every 12 seconds. Duplicate symbols on different providers
are rejected because the shared orderbook still matches by symbol.

REST:
- `GET /api/trackers` lists active instruments, last observed prices, status.
- `POST /api/trackers` accepts `{"symbol":"NVDA","provider":"YAHOO"}`.
- `DELETE /api/trackers?symbol=NVDA&provider=YAHOO` removes a tracker,
  except the last active one. Only SYS_ADMIN can change this watchlist.
- Existing `POST /api/config` selects/adds a ticker but does not remove
  existing watchers. USER stays read-only.

## Memory and runtime

- Exactly one scheduler worker, bounded at 12 assets and one upstream fetch
  per global interval (minimum 250ms), no ticker streaming stdout spam.
- SSE subscribers capped at 32, buffers hold at most 2 frames per client.
- Cached provider OHLC sets capped at 16, financials sets at 12 with
  oldest-entry eviction. Raw market payload per emitted tick capped at 32 KiB.
  Frontend chart already caps live points.
- Container defaults: `GOMEMLIMIT=384MiB`, `GOGC=75`, app memory limit
  768 MiB, process limit 128. Adjust based on actual `docker stats` or
  `podman stats` and OOMKilled state; do not assume this proves the
  original memory failure is fixed.
- MySQL volume `mysql_data` and host `.env` remain untouched by updater.

## Existing SQLite data: explicit one-time transition

Old `trading_data` Docker/Podman volume is **no longer mounted**, but it is
**not automatically deleted** because it might contain irreplaceable history.
Back it up before changing versions and avoid `down -v`.

1. Stop the old server, back up MySQL and old SQLite database consistently
   (include WAL if live). Copy SQLite to an offline `backup.db`.
2. Apply Liquibase migrations:
   `docker compose -f compose.deploy.yml --profile migration run --rm migrate update`.
3. Review source counts:
   `python3 scripts/import-legacy-sqlite.py backup.db --dry-run`.
4. **Before starting the new tracking app**, import history/orders/forum
   data if desired. This optional offline script requires
   `python3 -m pip install pymysql`, plus the environment variables
   `MIGRATE_MYSQL_HOST`, `MIGRATE_MYSQL_PORT`,
   `MIGRATE_MYSQL_USER`, `MIGRATE_MYSQL_PASSWORD`,
   `MIGRATE_MYSQL_DATABASE`. Run
   `python3 scripts/import-legacy-sqlite.py backup.db`.
   It refuses nonempty target trading tables, copies original row IDs,
   uses one transaction and verifies counts; never deletes the source.
   The new DB must be empty of app trading/forum records before import.
5. Verify positions/history/forum, then deploy the new application image.
   Only after verifying backups and import may you manually delete the old
   SQLite volume. We do not destroy data silently.

**Security caveat:** USER research accounts still share SYS_ADMIN's trading
data implicitly if granted trading permissions in the future; tenant isolation
is a separate change. Current USER restrictions remain enforced. No automatic
external brokerage orders are implemented.
