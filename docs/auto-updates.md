# Automatic updates — Docker and Podman

## Deployment modes

- Local development: `compose.yml` + `docker build` or `podman build`. Keep this for debugging and source development.
- **Unattended image updates (opt-in)**: `compose.deploy.yml` with public/authorized `ghcr.io/doldas/floweysales:stable` and `ghcr.io/doldas/floweysales-migrate:stable` images. On green main CI, GitHub Actions publishes both. For a pinned version, set `FLOWEYSALES_TAG=vX.Y.Z`.
- MySQL is pinned to `mysql:8.4`. It is **not** pulled or recreated by the updater: database upgrades require an explicit backup, maintenance window and validation.

## One-time installation

```bash
cp .env.container.example .env
# Set your own MySQL and bootstrap credentials; do not commit .env.
docker login ghcr.io       # only if GHCR package is private
docker compose -f compose.deploy.yml up -d mysql
docker compose -f compose.deploy.yml --profile migration run --rm migrate update
docker compose -f compose.deploy.yml up -d app
bash scripts/install-auto-update.sh docker            # add --no-timer for updates only on request
docker compose -f compose.deploy.yml up -d app        # once, so the app mounts update-control/
```

For Podman, substitute `podman` for `docker`, including the timer installer:
`bash scripts/install-auto-update.sh podman`. Podman Compose needs an installed
compose provider (Docker Compose plugin or podman-compose). The timer is a
**user-level systemd timer** that checks images four times daily and calls
the same Compose logic for Docker and Podman. For rootless Podman, configure
`loginctl enable-linger $(whoami)` if you need updates when logged out.

`podman auto-update` **without Compose/systemd integration does not restart
ordinary Compose containers**; that is why this repo includes a dedicated
timer instead of merely placing an auto-update label on Compose services.

The installer creates `update-control/` (shared with the app container) and
`backups/` in the project directory and installs three user units:
`floweysales-update.service` runs the updater, `floweysales-update.path` starts
it when an administrator requests an update in the UI, and
`floweysales-update.timer` (unless `--no-timer`) also runs it four times daily.

## Settings → Updates

SYS_ADMIN can open **Settings → Updates** to:

- see the running build (commit and build time baked into the image) and the
  update channel (`FLOWEYSALES_TAG`, usually `stable`);
- **Check for updates**: the app reads the commit of the image the tag points
  to from GHCR (anonymous, public metadata) and, when it differs, lists the
  commits and the **Liquibase migration files** the update adds from the
  GitHub compare API. `UPDATE_CHECK_INTERVAL_HOURS` (default 12 in
  `compose.deploy.yml`, 0 = only on click) also checks in the background;
- **Download and install update**: after confirmation the app writes
  `update-control/inbox/request.json`. The host path unit starts the updater,
  which reports each step back to `update-control/status.json`; the page shows
  progress, reconnects across the restart and keeps the last result and log.

The application never pulls images, runs migrations or restarts itself and has
no container-engine socket. The request file only triggers the host's fixed
procedure below; its contents are not executed. A request is refused while
another is queued or running, or if a newer image appeared after the
administrator reviewed the update. Images built before this feature carry no
commit label; the page then says the version is unknown and still allows
installing the latest image, which enables tracking from then on.

## Safe update sequence

The script `scripts/auto-update.sh`:
1. Pulls only the application and Liquibase images for the configured tag.
   If the app already runs that image it stops here (`up_to_date`).
2. **Backs up MySQL** with `mysqldump --single-transaction` to
   `backups/pre-update-<time>-<commit>.sql.gz` (mode 0600; newest
   `UPDATE_BACKUP_KEEP`, default 5, are kept). If the backup fails, nothing is
   migrated. Set `UPDATE_BACKUP=false` in `.env` to skip (not recommended).
3. Runs the new image's **additive Liquibase migrations** before restarting
   the app. On failure it exits, leaving the current app running.
4. Runs `compose up -d --no-deps app`. Compose detects changed images and
   recreates app as needed. It does **not** destroy the MySQL/SQLite volumes.
5. Waits up to 90 seconds for `/healthz`. If the new version is unhealthy it
   re-tags and starts the **previous image** (`rolled_back`); migrations stay
   applied, which is why they must remain additive and backward-compatible.
6. Preserves `.env` (on the host), `mysql_data`, `trading_data`, existing
   sessions/users and persisted `ai_configuration` values.

To restore a backup after a bad migration (with the app stopped):
`gunzip -c backups/<file>.sql.gz | docker compose -f compose.deploy.yml exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE"'`.

Never use `down -v` when preserving data. Image updates do not overwrite
`.env` or the MySQL values persisted by SYS_ADMIN. If the stack is already
configured and authenticated, do not re-bootstrap the owner credentials.

### Important limits

Unattended software updates still carry migration and downtime risk. Perform
regular tested backups (MySQL dump plus SQLite backup). SQL migrations in
automatic releases **must** be backward-compatible and additive. A migration
can be irreversible; rolling back the container does not roll back schema.
Review release notes and set a fixed `FLOWEYSALES_TAG` to freeze updates.

The updater is opt-in and intentionally lacks a privileged third-party
Docker-socket watcher. Image publishing and CI validate Docker. **Podman
host/timer behavior must also be validated on your installation**.

## Monitor and control

```bash
systemctl --user status floweysales-update.timer floweysales-update.path
cat update-control/status.json            # what Settings → Updates shows
journalctl --user -u floweysales-update.service -n 100 --no-pager
systemctl --user start floweysales-update.service
systemctl --user disable --now floweysales-update.timer
```
