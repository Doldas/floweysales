#!/usr/bin/env bash
set -euo pipefail

passed=0
run_case() {
  local expected="$1" description="$2" author="$3" branch="$4" head_repo="$5" body="$6"
  local rc=0
  GITHUB_REPOSITORY="Doldas/floweysales" \
  HEAD_BRANCH="$branch" \
  PR_AUTHOR="$author" \
  PR_HEAD_REPO="$head_repo" \
  PR_BODY="$body" \
  bash scripts/check-pr-policy.sh >/dev/null 2>&1 || rc=$?
  if [[ "$rc" != "$expected" ]]; then
    echo "FAIL: $description (exit $rc, expected $expected)"
    return 1
  fi
  echo "PASS: $description"
  passed=$((passed + 1))
}

# Real Dependabot PR shapes (npm, Go and GitHub Actions).
run_case 0 "Dependabot GitHub Actions" "dependabot[bot]" "dependabot/github_actions/actions/setup-node-7" "Doldas/floweysales" ""
run_case 0 "Dependabot npm" "dependabot[bot]" "dependabot/npm_and_yarn/frontend/typescript-7.0.2" "Doldas/floweysales" ""
run_case 0 "Dependabot Go" "dependabot[bot]" "dependabot/go_modules/sqlite-1.60.2" "Doldas/floweysales" ""

# A lookalike branch, a fork and another bot cannot use the exemption.
run_case 1 "Human Dependabot lookalike" "contributor" "dependabot/github_actions/unsafe" "Doldas/floweysales" ""
run_case 1 "Forked Dependabot branch" "dependabot[bot]" "dependabot/github_actions/unsafe" "other/repo" ""
run_case 1 "Wrong bot identity" "renovate[bot]" "dependabot/github_actions/unsafe" "Doldas/floweysales" ""
run_case 1 "Dependabot wrong branch" "dependabot[bot]" "misc/unsafe" "Doldas/floweysales" ""

# Ordinary contributions still require both a valid branch and linked issue.
run_case 0 "Normal feature PR" "human" "feature/filings-rag" "Doldas/floweysales" "Relates to #16"
run_case 0 "Normal docs PR" "human" "docs/roadmap" "other/repo" "Fixes #16"
run_case 1 "Human missing issue link" "human" "fix/something" "Doldas/floweysales" "No linked issue"
run_case 1 "Human invalid branch" "human" "random/name" "Doldas/floweysales" "Fixes #16"

echo "All $passed PR policy cases passed."
