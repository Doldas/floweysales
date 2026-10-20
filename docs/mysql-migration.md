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
5. Run `MYSQL_DSN=... SQLITE_IMPORT_PATH=/path/floweysales.db go run ./cmd/migrate-sqlite`
   on a host with database access, or build the command as an explicit
   one-time container. Do not run it while the old app is writing.
6. Confirm row counts in MySQL `trading_history`, `orders` and
   `forum_posts`, orders and positions, then start the new application.

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
