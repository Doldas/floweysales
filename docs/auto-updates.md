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
bash scripts/install-auto-update.sh docker
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

## Safe update sequence

The script `scripts/auto-update.sh`:
1. Pulls only the application and Liquibase images for the configured tag.
2. Runs the new image's **additive Liquibase migrations** before restarting
   the app. On failure it exits, leaving the current app running.
3. Runs `compose up -d --no-deps app`. Compose detects changed images and
   recreates app as needed. It does **not** destroy the MySQL/SQLite volumes.
4. Preserves `.env` (on the host), `mysql_data`, `trading_data`, existing
   sessions/users and persisted `ai_configuration` values.

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
systemctl --user status floweysales-update.timer
journalctl --user -u floweysales-update.service -n 100 --no-pager
systemctl --user start floweysales-update.service
systemctl --user disable --now floweysales-update.timer
```
