#!/usr/bin/env bash
# Fails when frontend code formats dates or numbers with the browser's time
# zone or language. Every date in the app is UTC and every number en-US; use
# the helpers in frontend/src/app/ui/datetime.ts.
set -u
cd "$(dirname "$0")/.."
status=0
check() {
  local label=$1 pattern=$2
  local hits
  hits=$(grep -rnP --include='*.ts' --include='*.html' --exclude='*.spec.ts' "$pattern" frontend/src/app | grep -v 'ui/datetime.ts' || true)
  if [ -n "$hits" ]; then
    echo "::error::$label"
    echo "$hits"
    status=1
  fi
}
check "toLocale*String() without the en-US locale (use ui/datetime.ts)" "toLocale(Date|Time)?String\((?!'en-US')"
check "Intl formatter without an explicit locale (use 'en-US')" "Intl\.(DateTimeFormat|NumberFormat)\((?!'en-US'|LOCALE)"
check "local-time getters (use the getUTC* ones)" "\.get(Hours|Minutes|Seconds|Day|Date|Month|FullYear|TimezoneOffset)\("
exit $status
