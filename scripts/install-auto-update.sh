#!/usr/bin/env bash
# Opt-in user-level systemd units for Docker or rootless Podman Compose:
#   floweysales-update.service  runs scripts/auto-update.sh
#   floweysales-update.path     starts it when Settings → Updates requests an update
#   floweysales-update.timer    also checks four times daily (omit with --no-timer)
set -Eeuo pipefail
ENGINE=docker
TIMER=true
for arg in "$@"; do
  case "$arg" in
    docker|podman) ENGINE="$arg" ;;
    --no-timer) TIMER=false ;;
    *) echo "Usage: bash scripts/install-auto-update.sh [docker|podman] [--no-timer]" >&2; exit 2 ;;
  esac
done
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ ! -f "$project_dir/.env" ]]; then
  echo "Create $project_dir/.env first (not committed)." >&2
  exit 2
fi

# The app container (uid 10001) writes requests to inbox/ and reads status;
# this host user owns everything else. Backups hold database contents.
control="$project_dir/update-control"
mkdir -p "$control/inbox" "$project_dir/backups"
chmod 0755 "$control"
chmod 0777 "$control/inbox"
chmod 0700 "$project_dir/backups"
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '{"engine":"%s","installed_at":"%s","last_run_at":"","timer":%s,"backups":true}\n' "$ENGINE" "$now" "$TIMER" > "$control/agent.json"
chmod 0644 "$control/agent.json"

units="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p "$units"
cat > "$units/floweysales-update.service" <<UNIT
[Unit]
Description=FloweySales checked application image update (preserves volumes/config)
[Service]
Type=oneshot
WorkingDirectory=$project_dir
Environment=CONTAINER_ENGINE=$ENGINE
ExecStart=/bin/bash $project_dir/scripts/auto-update.sh
TimeoutStartSec=1800
UNIT
cat > "$units/floweysales-update.path" <<UNIT
[Unit]
Description=Start a FloweySales update when an administrator requests one in Settings
[Path]
PathExists=$control/inbox/request.json
Unit=floweysales-update.service
[Install]
WantedBy=default.target
UNIT
systemctl --user daemon-reload
systemctl --user enable --now floweysales-update.path

if [[ "$TIMER" == true ]]; then
  cat > "$units/floweysales-update.timer" <<'UNIT'
[Unit]
Description=Check FloweySales application updates four times daily
[Timer]
OnCalendar=*-*-* 04,10,16,22:00:00
RandomizedDelaySec=20m
Persistent=true
Unit=floweysales-update.service
[Install]
WantedBy=timers.target
UNIT
  systemctl --user daemon-reload
  systemctl --user enable --now floweysales-update.timer
  echo "Enabled automatic updates four times daily and on request from Settings → Updates ($ENGINE)."
else
  if [[ -f "$units/floweysales-update.timer" ]]; then
    systemctl --user disable --now floweysales-update.timer || true
    rm -f "$units/floweysales-update.timer"
    systemctl --user daemon-reload
  fi
  echo "Enabled updates on request from Settings → Updates only ($ENGINE); no scheduled updates."
fi
echo "Recreate the app once so it mounts update-control/:"
echo "  $ENGINE compose -f compose.deploy.yml up -d app"
echo "For updates without a login session, a host administrator can enable linger:"
echo "  loginctl enable-linger $(id -un)"
echo "Inspect: systemctl --user list-timers floweysales-update.timer; systemctl --user status floweysales-update.path"
echo "Manual run: systemctl --user start floweysales-update.service"
