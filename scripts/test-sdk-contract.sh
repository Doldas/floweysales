#!/usr/bin/env bash
set -euo pipefail
script="$(cd "$(dirname "$0")" && pwd)/check-sdk-contract.sh"
repo="$(mktemp -d)"
trap 'rm -rf "$repo"' EXIT
cd "$repo"
git init -q .
git config user.email t@example.com
git config user.name t
mkdir -p pluginsdk/testdata/contract pluginsdk/plugintest docs
echo a > pluginsdk/types.go
echo a > pluginsdk/types_test.go
echo a > docs/x.md
git add -A
git commit -qm base
base="$(git rev-parse HEAD)"

passed=0
run_case() { # expected description body files...
  local expected="$1" desc="$2" body="$3"
  shift 3
  git checkout -q -B case "$base"
  for f in "$@"; do mkdir -p "$(dirname "$f")"; echo "$RANDOM" >> "$f"; done
  git add -A
  git commit -qm case
  local rc=0
  BASE_SHA="$base" HEAD_SHA="$(git rev-parse HEAD)" PR_BODY="$body" bash "$script" >/dev/null 2>&1 || rc=$?
  if [[ "$rc" != "$expected" ]]; then echo "FAIL: $desc (exit $rc, expected $expected)"; exit 1; fi
  echo "PASS: $desc"
  passed=$((passed + 1))
}

run_case 0 "docs-only change is not applicable" "" docs/x.md
run_case 1 "API change alone fails" "" pluginsdk/types.go
run_case 1 "API change with test but no changelog fails" "" pluginsdk/types.go pluginsdk/types_test.go
run_case 1 "API change with changelog but no test fails" "" pluginsdk/types.go pluginsdk/CHANGELOG.md
run_case 0 "API change with fixture and changelog passes" "" pluginsdk/types.go pluginsdk/testdata/contract/new.json pluginsdk/CHANGELOG.md
run_case 0 "API change with test and changelog passes" "" pluginsdk/types.go pluginsdk/types_test.go pluginsdk/CHANGELOG.md
run_case 0 "explicit contract-neutral opt-out passes" "SDK-Contract: none (comment typo)" pluginsdk/types.go
run_case 1 "opt-out without reason fails" "SDK-Contract: none ()" pluginsdk/types.go
run_case 1 "plugintest change without changelog fails" "" pluginsdk/plugintest/k.go
echo "$passed cases passed"
