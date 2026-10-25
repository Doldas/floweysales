#!/usr/bin/env bash
# Exercises scripts/auto-update.sh against a stub container engine and curl:
# success from a UI request, no-op when already current, failed backup,
# failed migration, health-check rollback and backup retention.
set -Eeuo pipefail
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
OLD=1111111111111111111111111111111111111111
NEW=2222222222222222222222222222222222222222

mkdir -p "$work/bin"
cat > "$work/bin/fake-engine" <<'STUB'
#!/usr/bin/env bash
# State: $STATE/tag_id (image the tag points to), $STATE/running_id (app container image).
echo "$*" >> "$STATE/calls.log"
tag_id() { cat "$STATE/tag_id"; }
case "$*" in
  "compose -f compose.deploy.yml pull app migrate")
    [[ "$SCENARIO" == same ]] || echo sha256:new > "$STATE/tag_id"; echo "pulled";;
  "compose -f compose.deploy.yml ps -q app") echo cid;;
  "inspect --format {{.Image}} cid") cat "$STATE/running_id";;
  "image inspect --format {{.Id}} "*) tag_id;;
  "image inspect --format {{ index .Config.Labels \"org.opencontainers.image.revision\" }} "*)
    target="${*: -1}"; [[ "$target" == sha256:* ]] || target="$(tag_id)"
    [[ "$target" == sha256:old ]] && echo "$OLD_COMMIT" || echo "$NEW_COMMIT";;
  "compose -f compose.deploy.yml exec -T mysql "*)
    [[ "$SCENARIO" == backupfail ]] && { echo "mysqldump: access denied" >&2; exit 2; }
    echo "-- MySQL dump";;
  "compose -f compose.deploy.yml --profile migration run --rm --no-deps migrate update")
    [[ "$SCENARIO" == migratefail ]] && { echo "Liquibase: changeset failed" >&2; exit 1; }
    echo "Liquibase: update successful";;
  "compose -f compose.deploy.yml up -d --no-deps app") tag_id > "$STATE/running_id";;
  "tag "*) echo "$2" > "$STATE/tag_id";;
  *) echo "unexpected engine call: $*" >&2; exit 99;;
esac
STUB
cat > "$work/bin/curl" <<'STUB'
#!/usr/bin/env bash
# Healthy unless the unhealthy scenario runs the new image.
[[ "$SCENARIO" == unhealthy && "$(cat "$STATE/running_id")" == sha256:new ]] && exit 7
exit 0
STUB
chmod +x "$work/bin/fake-engine" "$work/bin/curl"

failures=0
fail() { echo "FAIL [$scenario]: $*" >&2; failures=$((failures + 1)); }
status_field() { python3 -c "import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]])" "$project/update-control/status.json" "$1"; }

run_scenario() { # scenario [with_request]
  scenario="$1"
  project="$work/$scenario"
  export STATE="$project/state"
  mkdir -p "$project/scripts" "$STATE" "$project/update-control/inbox"
  cp "$repo/scripts/auto-update.sh" "$project/scripts/"
  printf 'FLOWEYSALES_TAG=stable\nAPP_PORT=18080\nUPDATE_BACKUP_KEEP=5\n' > "$project/.env"
  : > "$project/compose.deploy.yml"
  echo sha256:old > "$STATE/tag_id"
  echo sha256:old > "$STATE/running_id"
  if [[ "${2:-}" == with_request ]]; then
    echo '{"id":"abc123","requested_by":1,"requested_at":"2026-10-25T09:00:00Z","expected_commit":"'"$NEW"'"}' > "$project/update-control/inbox/request.json"
  fi
  set +e
  SCENARIO="$scenario" OLD_COMMIT="$OLD" NEW_COMMIT="$NEW" PATH="$work/bin:$PATH" XDG_RUNTIME_DIR="$project" \
    UPDATE_ENGINE_BIN="$work/bin/fake-engine" UPDATE_HEALTH_TIMEOUT=1 UPDATE_HEALTH_INTERVAL=0.1 \
    bash "$project/scripts/auto-update.sh" > "$project/out.log" 2>&1
  code=$?
  set -e
  python3 -m json.tool "$project/update-control/status.json" >/dev/null || fail "status.json is not valid JSON"
  python3 -m json.tool "$project/update-control/agent.json" >/dev/null || fail "agent.json is not valid JSON"
}
called() { grep -qF -- "$1" "$STATE/calls.log"; }

run_scenario success with_request
[[ $code -eq 0 ]] || fail "exit $code: $(cat "$project/out.log")"
[[ "$(status_field state)" == succeeded ]] || fail "state $(status_field state)"
[[ "$(status_field trigger)" == ui && "$(status_field request_id)" == abc123 ]] || fail "request not attributed"
[[ "$(status_field from_commit)" == "$OLD" && "$(status_field to_commit)" == "$NEW" ]] || fail "commits not reported"
[[ ! -e "$project/update-control/inbox/request.json" ]] || fail "request not consumed"
backup="$project/$(status_field backup)"
[[ -s "$backup" ]] && gzip -t "$backup" || fail "backup missing or corrupt"
[[ "$(stat -c %a "$backup")" == 600 ]] || fail "backup is not private"
migrate_line=$(grep -n "migrate update" "$STATE/calls.log" | cut -d: -f1)
up_line=$(grep -n "up -d --no-deps app" "$STATE/calls.log" | cut -d: -f1)
[[ -n "$migrate_line" && -n "$up_line" && $migrate_line -lt $up_line ]] || fail "migrations must run before restart"

run_scenario same
[[ $code -eq 0 && "$(status_field state)" == up_to_date ]] || fail "expected up_to_date, got $(status_field state)"
called "migrate update" && fail "no-op update ran migrations"
called "up -d" && fail "no-op update restarted the app"
[[ "$(status_field trigger)" == timer ]] || fail "timer trigger not reported"

run_scenario backupfail
[[ $code -ne 0 && "$(status_field state)" == failed ]] || fail "backup failure not reported"
called "migrate update" && fail "migrated without a backup"
ls "$project"/backups/*.sql.gz >/dev/null 2>&1 && fail "empty backup left behind"

run_scenario migratefail
[[ $code -ne 0 && "$(status_field state)" == failed ]] || fail "migration failure not reported"
called "up -d" && fail "restarted after a failed migration"
grep -q "changeset failed" "$project/update-control/status.json" || fail "log tail missing from status"

run_scenario unhealthy
[[ $code -ne 0 && "$(status_field state)" == rolled_back ]] || fail "expected rolled_back, got $(status_field state)"
called "tag sha256:old ghcr.io/doldas/floweysales:stable" || fail "previous image not re-tagged"
[[ "$(cat "$STATE/running_id")" == sha256:old ]] || fail "previous image not running after rollback"

# Retention: six older backups plus the new one leave the newest five.
scenario=retention
project="$work/retention"; mkdir -p "$project/backups"
for i in 1 2 3 4 5 6; do echo x | gzip > "$project/backups/pre-update-2026010${i}T000000Z.sql.gz"; touch -d "2026-01-0$i" "$project/backups/pre-update-2026010${i}T000000Z.sql.gz"; done
run_scenario retention
[[ $code -eq 0 ]] || fail "exit $code"
[[ $(ls "$project"/backups/pre-update-*.sql.gz | wc -l) -eq 5 ]] || fail "expected 5 backups, found $(ls "$project"/backups | wc -l)"
[[ -e "$project/backups/pre-update-20260101T000000Z.sql.gz" ]] && fail "oldest backup kept"

if [[ $failures -gt 0 ]]; then
  echo "$failures auto-update check(s) failed" >&2
  exit 1
fi
echo "auto-update.sh: all scenarios passed"
