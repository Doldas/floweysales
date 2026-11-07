#!/usr/bin/env bash
# Fail a PR that changes the SDK public API, wire protocol or manifest without
# contract fixtures/tests and a changelog entry. Usage: BASE_SHA=... HEAD_SHA=...
# Opt out with "SDK-Contract: none (reason)" in the PR body (PR_BODY).
set -euo pipefail

: "${BASE_SHA:?Missing base commit}"
: "${HEAD_SHA:?Missing head commit}"
PR_BODY="${PR_BODY:-}"

mapfile -t files < <(git diff --name-only "$BASE_SHA" "$HEAD_SHA")

api=0 evidence=0 changelog=0
for f in "${files[@]}"; do
  case "$f" in
    pluginsdk/*_test.go|pluginsdk/*/*_test.go|pluginsdk/testdata/contract/*) evidence=1 ;;
    pluginsdk/plugintest/*.go) api=1 ;;
    pluginsdk/CHANGELOG.md) changelog=1 ;;
    pluginsdk/*.go|pluginsdk/*/*.go) api=1 ;;
  esac
done

if [[ "$api" == 0 ]]; then
  echo "No SDK API, protocol or manifest change; contract gate not applicable."
  exit 0
fi

if grep -Eiq '^SDK-Contract: none \(.+\)' <<<"$PR_BODY"; then
  echo "SDK change declared contract-neutral in the PR body."
  exit 0
fi

fail=0
if [[ "$evidence" == 0 ]]; then
  echo "::error::SDK source changed without a contract fixture or test (pluginsdk/testdata/contract/ or *_test.go)."
  fail=1
fi
if [[ "$changelog" == 0 ]]; then
  echo "::error::SDK source changed without a pluginsdk/CHANGELOG.md entry (Breaking/Additive/Fix, with migration notes)."
  fail=1
fi
[[ "$fail" == 0 ]] && echo "SDK contract gate passed: tests/fixtures and changelog updated."
exit "$fail"
