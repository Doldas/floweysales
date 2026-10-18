#!/usr/bin/env bash
# Validate PR branch/issue policy using trusted event metadata.
# Dependabot is exempt only from human naming and issue-link requirements;
# Go tests, frontend builds, and review policies still apply to bot PRs.
set -euo pipefail

: "${HEAD_BRANCH:?Missing PR head branch}"
: "${PR_AUTHOR:?Missing PR author}"
: "${PR_HEAD_REPO:?Missing PR head repository}"
: "${GITHUB_REPOSITORY:?Missing destination repository}"
PR_BODY="${PR_BODY:-}"

if [[ "$PR_AUTHOR" == "dependabot[bot]" \
      && "$PR_HEAD_REPO" == "$GITHUB_REPOSITORY" \
      && "$HEAD_BRANCH" =~ ^dependabot/[a-z0-9][a-z0-9._/-]*$ ]]; then
  echo "Verified in-repo Dependabot PR: exempt from branch naming and issue link; all build/test checks still apply."
  exit 0
fi

if [[ ! "$HEAD_BRANCH" =~ ^(feature|fix|bugfix|docs|chore|release)/[a-z0-9][a-z0-9._-]*$ ]]; then
  echo "::error::Branch must follow feature/, fix/, bugfix/, docs/, chore/, or release/. Only verified in-repo Dependabot PRs are exempt."
  exit 1
fi

if [[ ! "$PR_BODY" =~ \#[0-9]+ ]]; then
  echo "::error::PR body must link a GitHub issue (e.g. Relates to #16)."
  exit 1
fi

echo "PR branch and issue-reference policy passed."
