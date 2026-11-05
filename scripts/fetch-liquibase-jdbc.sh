#!/usr/bin/env bash
# Fetch the pinned JDBC artifact once, verify it on every use, and let CI
# cache the verified bytes across independent GitHub-hosted runners.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"
file="docker/vendor/mysql-connector-j-9.5.0.jar"
checksum="docker/mysql-connector-j-9.5.0.sha256"
url="https://repo.maven.apache.org/maven2/com/mysql/mysql-connector-j/9.5.0/mysql-connector-j-9.5.0.jar"

test -f "$checksum" || { echo "Missing pinned JDBC checksum: $checksum" >&2; exit 1; }
if [[ -f "$file" ]] && sha256sum --check --status "$checksum"; then
  echo "Verified cached MySQL Connector/J 9.5.0; no external request needed."
  exit 0
fi

if [[ -f "$file" ]]; then
  echo "JDBC cache checksum mismatch; discarding untrusted bytes." >&2
  rm -f "$file"
fi

mkdir -p "$(dirname "$file")"
tmp="$(mktemp "${file}.part.XXXXXX")"
trap 'rm -f "$tmp"' EXIT

echo "Fetching pinned MySQL Connector/J 9.5.0 on cache miss."
curl --fail --location --show-error --silent \
  --retry 5 --retry-all-errors --retry-delay 5 \
  --connect-timeout 20 --max-time 120 --output "$tmp" "$url"

# Never populate a CI cache or Docker context before verifying exact bytes.
expected="$(cut -d' ' -f1 "$checksum")"
actual="$(sha256sum "$tmp" | cut -d' ' -f1)"
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ || "$actual" != "$expected" ]]; then
  echo "JDBC SHA256 mismatch: refusing downloaded bytes." >&2
  exit 1
fi

chmod 0644 "$tmp"
mv -f "$tmp" "$file"
sha256sum --check "$checksum"
echo "Verified JDBC artifact is ready for Docker builds."
