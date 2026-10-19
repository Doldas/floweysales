#!/usr/bin/env bash
# Docker/Podman Compose self-update. Updates application images only, then runs
# additive Liquibase migrations before recreating app. No volumes are removed.
set -Eeuo pipefail
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"
ENGINE="${CONTAINER_ENGINE:-docker}"
if [[ "$ENGINE" != "docker" && "$ENGINE" != "podman" ]]; then
  echo "CONTAINER_ENGINE must be docker or podman" >&2
  exit 2
fi
if [[ ! -f .env ]]; then
  echo "Missing .env. Copy .env.container.example and add your secrets." >&2
  exit 2
fi
if command -v flock >/dev/null 2>&1; then
  exec 9>"${XDG_RUNTIME_DIR:-/tmp}/floweysales-${UID}.update.lock"
  flock -n 9 || { echo "Another update is already in progress"; exit 0; }
fi
compose() { "$ENGINE" compose -f compose.deploy.yml "$@"; }

echo "[update] Pulling app and migration images only (MySQL excluded)..."
compose pull app migrate

echo "[update] Applying forward-only Liquibase migrations before app restart..."
compose --profile migration run --rm --no-deps migrate update

echo "[update] Updating application only; Compose keeps same volume, env and MySQL service..."
compose up -d --no-deps app

echo "[update] Done. Inspect status with: $ENGINE compose -f compose.deploy.yml ps"
