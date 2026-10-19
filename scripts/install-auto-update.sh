#!/usr/bin/env bash
# Opt-in user-level systemd timer for Docker or rootless Podman Compose.
set -Eeuo pipefail
ENGINE="${1:-docker}"
if [[ "$ENGINE" != docker && "$ENGINE" != podman ]]; then
  echo "Usage: bash scripts/install-auto-update.sh [docker|podman]" >&2
  exit 2
fi
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ ! -f "$project_dir/.env" ]]; then
  echo "Create $project_dir/.env first (not committed)." >&2
  exit 2
fi
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
TimeoutStartSec=1200
UNIT
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
echo "Enabled FloweySales update timer for $ENGINE."
echo "For boot without a login session, a host administrator can enable linger:"
echo "  loginctl enable-linger $(id -un)"
echo "Inspect: systemctl --user list-timers floweysales-update.timer"
echo "Manual run: systemctl --user start floweysales-update.service"
