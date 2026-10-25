#!/usr/bin/env bash
# Docker/Podman Compose self-update. Updates application images only, backs
# up MySQL, runs additive Liquibase migrations, then recreates the app and
# verifies it is healthy, rolling the image back if it is not. No volumes are
# removed. Progress is reported to update-control/status.json, which
# Settings → Updates in the application displays.
#
# Runs from the systemd timer, from the path unit when an administrator
# requests an update in the UI (update-control/inbox/request.json), or
# manually. The request file only triggers this fixed procedure; its contents
# are never executed.
set -Eeuo pipefail
PROJECT_DIR="${UPDATE_PROJECT_DIR:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
cd "$PROJECT_DIR"
ENGINE="${CONTAINER_ENGINE:-docker}"
if [[ "$ENGINE" != "docker" && "$ENGINE" != "podman" ]]; then
  echo "CONTAINER_ENGINE must be docker or podman" >&2
  exit 2
fi
# Tests substitute a stub for the engine binary.
ENGINE_BIN="${UPDATE_ENGINE_BIN:-$ENGINE}"
if [[ ! -f .env ]]; then
  echo "Missing .env. Copy .env.container.example and add your secrets." >&2
  exit 2
fi
if command -v flock >/dev/null 2>&1; then
  exec 9>"${XDG_RUNTIME_DIR:-/tmp}/floweysales-${UID}.update.lock"
  flock -n 9 || { echo "Another update is already in progress"; exit 0; }
fi

CONTROL_DIR="$PROJECT_DIR/update-control"
BACKUP_DIR="$PROJECT_DIR/backups"
env_value() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2- | tr -d '"'"'" || true; }
BACKUPS="${UPDATE_BACKUP:-$(env_value UPDATE_BACKUP)}"; BACKUPS="${BACKUPS:-true}"
BACKUP_KEEP="${UPDATE_BACKUP_KEEP:-$(env_value UPDATE_BACKUP_KEEP)}"; BACKUP_KEEP="${BACKUP_KEEP:-5}"
[[ "$BACKUP_KEEP" =~ ^[1-9][0-9]?$ ]] || { echo "UPDATE_BACKUP_KEEP must be 1-99" >&2; exit 2; }
HEALTH_TIMEOUT="${UPDATE_HEALTH_TIMEOUT:-90}"
TAG="$(env_value FLOWEYSALES_TAG)"; TAG="${TAG:-stable}"
APP_PORT_VALUE="$(env_value APP_PORT)"; APP_PORT_VALUE="${APP_PORT_VALUE:-8080}"
APP_IMAGE="ghcr.io/doldas/floweysales:$TAG"
HEALTH_URL="${UPDATE_HEALTH_URL:-http://127.0.0.1:$APP_PORT_VALUE/healthz}"

compose() { "$ENGINE_BIN" compose -f compose.deploy.yml "$@"; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }

mkdir -p "$CONTROL_DIR/inbox"
LOG_FILE="$CONTROL_DIR/last-update.log"
: > "$LOG_FILE"
log() { echo "[update] $*" | tee -a "$LOG_FILE"; }
run() { "$@" 2>&1 | tee -a "$LOG_FILE"; return "${PIPESTATUS[0]}"; }

# json_str prints a JSON string literal; control characters (including the
# ANSI escapes in pull progress) are dropped.
json_str() {
  local s
  s="$(printf '%s' "$1" | tr -d '\000-\010\013-\037')"
  s="${s//\\/\\\\}"; s="${s//\"/\\\"}"; s="${s//$'\t'/ }"; s="${s//$'\n'/\\n}"
  printf '"%s"' "$s"
}

STARTED_AT="$(now)"; REQUEST_ID=""; TRIGGER="${UPDATE_TRIGGER:-timer}"
FROM_COMMIT=""; TO_COMMIT=""; BACKUP_FILE=""
write_status() { # state message [finished]
  local tail_json="" line first=1 finished=""
  [[ "${3:-}" == finished ]] && finished="$(now)"
  while IFS= read -r line; do
    [[ $first -eq 1 ]] || tail_json+=","
    tail_json+="$(json_str "$line")"; first=0
  done < <(tail -n 20 "$LOG_FILE" 2>/dev/null)
  local tmp="$CONTROL_DIR/.status.json.tmp"
  printf '{"state":%s,"message":%s,"request_id":%s,"trigger":%s,"started_at":%s,"finished_at":%s,"from_commit":%s,"to_commit":%s,"backup":%s,"log_tail":[%s]}\n' \
    "$(json_str "$1")" "$(json_str "$2")" "$(json_str "$REQUEST_ID")" "$(json_str "$TRIGGER")" \
    "$(json_str "$STARTED_AT")" "$(json_str "$finished")" "$(json_str "$FROM_COMMIT")" \
    "$(json_str "$TO_COMMIT")" "$(json_str "$BACKUP_FILE")" "$tail_json" > "$tmp"
  chmod 0644 "$tmp"
  mv -f "$tmp" "$CONTROL_DIR/status.json"
}
write_agent() {
  local installed=""
  [[ -f "$CONTROL_DIR/agent.json" ]] && installed="$(sed -n 's/.*"installed_at":"\([^"]*\)".*/\1/p' "$CONTROL_DIR/agent.json")"
  local timer=false; [[ -f "${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/floweysales-update.timer" ]] && timer=true
  local backups=true; [[ "$BACKUPS" == true ]] || backups=false
  printf '{"engine":%s,"installed_at":%s,"last_run_at":%s,"timer":%s,"backups":%s}\n' \
    "$(json_str "$ENGINE")" "$(json_str "${installed:-$(now)}")" "$(json_str "$(now)")" "$timer" "$backups" > "$CONTROL_DIR/.agent.json.tmp"
  chmod 0644 "$CONTROL_DIR/.agent.json.tmp"
  mv -f "$CONTROL_DIR/.agent.json.tmp" "$CONTROL_DIR/agent.json"
}

# Consume a request from the application, if there is one.
if [[ -f "$CONTROL_DIR/inbox/request.json" ]]; then
  REQUEST_ID="$(sed -n 's/.*"id":"\([0-9a-f]*\)".*/\1/p' "$CONTROL_DIR/inbox/request.json" | head -c 32)"
  TRIGGER="ui"
  rm -f "$CONTROL_DIR/inbox/request.json"
fi

CURRENT_STATE="starting"
on_error() {
  local code=$?
  log "Failed during $CURRENT_STATE (exit $code)"
  write_status failed "Update failed while $CURRENT_STATE. The previously running application was kept where possible; see the log and backups/." finished
  write_agent || true
  exit "$code"
}
trap on_error ERR

image_id() { "$ENGINE_BIN" image inspect --format '{{.Id}}' "$1" 2>/dev/null || true; }
image_commit() { "$ENGINE_BIN" image inspect --format '{{ index .Config.Labels "org.opencontainers.image.revision" }}' "$1" 2>/dev/null | grep -E '^[0-9a-f]{7,40}$' || true; }
running_image() {
  local id; id="$(compose ps -q app 2>/dev/null | head -n1)"
  [[ -n "$id" ]] && "$ENGINE_BIN" inspect --format '{{.Image}}' "$id" 2>/dev/null || true
}
healthy() {
  local deadline=$((SECONDS + HEALTH_TIMEOUT))
  while (( SECONDS < deadline )); do
    if curl -fsS --max-time 5 "$HEALTH_URL" >/dev/null 2>&1; then return 0; fi
    sleep "${UPDATE_HEALTH_INTERVAL:-3}"
  done
  return 1
}

PREVIOUS_ID="$(running_image)"
[[ -n "$PREVIOUS_ID" ]] || PREVIOUS_ID="$(image_id "$APP_IMAGE")"
[[ -n "$PREVIOUS_ID" ]] && FROM_COMMIT="$(image_commit "$PREVIOUS_ID")"

CURRENT_STATE="pulling"
write_status pulling "Downloading application and migration images ($APP_IMAGE)"
log "Pulling app and migration images only (MySQL excluded)..."
run compose pull app migrate
NEW_ID="$(image_id "$APP_IMAGE")"
TO_COMMIT="$(image_commit "$APP_IMAGE")"

if [[ -n "$PREVIOUS_ID" && "$NEW_ID" == "$PREVIOUS_ID" ]]; then
  log "Already running the latest image; nothing to do."
  write_status up_to_date "Already running the latest $TAG image" finished
  write_agent
  exit 0
fi

if [[ "$BACKUPS" == true ]]; then
  CURRENT_STATE="backing up the database"
  write_status backing_up "Backing up MySQL before migrating"
  mkdir -p "$BACKUP_DIR"
  chmod 0700 "$BACKUP_DIR"
  BACKUP_FILE="backups/pre-update-$(date -u +%Y%m%dT%H%M%SZ)${FROM_COMMIT:+-${FROM_COMMIT:0:12}}.sql.gz"
  log "Writing $BACKUP_FILE"
  partial="$PROJECT_DIR/$BACKUP_FILE.partial"
  # Handle failure here so no partial dump is ever left looking like a backup.
  if ! (umask 077; compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" exec mysqldump -u "$MYSQL_USER" --single-transaction --no-tablespaces --routines --triggers "$MYSQL_DATABASE"' \
      2>>"$LOG_FILE" | gzip > "$partial") || [[ ! -s "$partial" ]] || ! gzip -t "$partial"; then
    rm -f "$partial"
    BACKUP_FILE=""
    log "Database backup failed; not migrating"
    false # handled by on_error; never migrate without a backup
  fi
  chmod 0600 "$partial"
  mv -f "$partial" "$PROJECT_DIR/$BACKUP_FILE"
  # Keep only the newest backups.
  { ls -1t "$BACKUP_DIR"/pre-update-*.sql.gz 2>/dev/null || true; } | tail -n "+$((BACKUP_KEEP + 1))" | xargs -r rm -f
fi

CURRENT_STATE="migrating the database"
write_status migrating "Applying forward-only Liquibase migrations"
log "Applying forward-only Liquibase migrations before app restart..."
run compose --profile migration run --rm --no-deps migrate update

CURRENT_STATE="restarting the application"
write_status restarting "Restarting the application with the new image"
log "Updating application only; Compose keeps same volume, env and MySQL service..."
run compose up -d --no-deps app

CURRENT_STATE="verifying the application"
write_status verifying "Waiting for the application health check"
if healthy; then
  log "Application is healthy on $HEALTH_URL"
  target="the latest $TAG image"; [[ -n "$TO_COMMIT" ]] && target="${TO_COMMIT:0:12}"
  write_status succeeded "Updated to $target" finished
  write_agent
  log "Done. Inspect status with: $ENGINE compose -f compose.deploy.yml ps"
  exit 0
fi

log "Health check failed after ${HEALTH_TIMEOUT}s"
if [[ -z "$PREVIOUS_ID" ]]; then
  write_status failed "The new application did not become healthy and there is no previous image to roll back to" finished
  write_agent
  exit 1
fi
CURRENT_STATE="rolling back the application image"
log "Rolling back the application image; database migrations stay applied (they are additive)"
run "$ENGINE_BIN" tag "$PREVIOUS_ID" "$APP_IMAGE"
run compose up -d --no-deps app
if healthy; then
  write_status rolled_back "The new version failed its health check; the previous version is running again. Migrations were kept." finished
else
  write_status failed "The new version failed its health check and the rollback is not healthy either; see the log" finished
fi
write_agent
exit 1
