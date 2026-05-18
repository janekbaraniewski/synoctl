# Branch protection — maintainer reference

Recommended GitHub branch-protection rules for `main`. This is a
maintainer-only reference; users do not need to read it.

## Policy

Apply to `main`:

- **Require pull request reviews before merging** — 1 approving review.
- **Dismiss stale pull request approvals when new commits are pushed** —
  prevents stale approval bypass after force-pushes / squash rewrites.
- **Require status checks to pass before merging**, with these checks
  marked required (names must match exactly what appears on a PR):
  - `Test (ubuntu-latest)`
  - `Test (macos-latest)`
  - `Verify`
  - `Analyze` (CodeQL)
  - `govulncheck`
  - `dependency-review`
  - `Lychee`
- **Require branches to be up to date before merging** — the
  refresh-pr-branches workflow handles this automatically for the
  release PR and Dependabot PRs.
- **Require linear history** — squash or rebase merges only; no
  merge commits on `main`.
- **Require conversation resolution before merging** — keeps review
  threads from being lost.
- **Do not allow bypassing the above settings**, including for admins,
  except for the `dependabot[bot]` actor (auto-merge needs to push
  approvals). Restrict who can push to `main` to maintainers only.
- **Allow force pushes:** off.
- **Allow deletions:** off.

## Apply in one shot

Run the snippet below with `gh` authenticated as a repo admin.
Replace `OWNER/REPO` if the script is reused elsewhere.

```bash
#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-OWNER/REPO}"
BRANCH="${BRANCH:-main}"

gh api \
  --method PUT \
  -H "Accept: application/vnd.github+json" \
  "/repos/${REPO}/branches/${BRANCH}/protection" \
  -f required_status_checks.strict=true \
  -F 'required_status_checks.contexts[]=Test (ubuntu-latest)' \
  -F 'required_status_checks.contexts[]=Test (macos-latest)' \
  -F 'required_status_checks.contexts[]=Verify' \
  -F 'required_status_checks.contexts[]=Analyze' \
  -F 'required_status_checks.contexts[]=govulncheck' \
  -F 'required_status_checks.contexts[]=dependency-review' \
  -F 'required_status_checks.contexts[]=Lychee' \
  -F enforce_admins=true \
  -F required_pull_request_reviews.dismiss_stale_reviews=true \
  -F required_pull_request_reviews.required_approving_review_count=1 \
  -F required_pull_request_reviews.require_code_owner_reviews=false \
  -F restrictions= \
  -F required_linear_history=true \
  -F allow_force_pushes=false \
  -F allow_deletions=false \
  -F required_conversation_resolution=true \
  -F lock_branch=false \
  -F allow_fork_syncing=true
```

The `restrictions=` empty value clears any push restriction list;
remove it if you want to keep an existing allowlist. To inspect the
current configuration, run:

```bash
gh api "/repos/${REPO}/branches/${BRANCH}/protection"
```

## When to revisit

- Adding a new required workflow (anything that should block merge):
  add its job name to `required_status_checks.contexts[]` and rerun
  the script.
- Renaming a job in an existing workflow: update the matching
  context here, otherwise PRs will block waiting for a check that
  never reports.
- Bumping required reviewers above 1: change
  `required_approving_review_count`. Pair with CODEOWNERS if you
  want path-scoped review requirements.
