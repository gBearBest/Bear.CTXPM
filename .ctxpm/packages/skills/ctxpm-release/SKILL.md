---
name: ctxpm-release
description: Prepare and publish Bear.CTXPM releases with the repository's strict Git Flow lifecycle, including release candidates, hotfixes, GitHub Release notes, assets, and latest-tag verification.
---

# Bear.CTXPM release workflow

Use this skill for Bear.CTXPM release preparation and publication. Keep the
operator procedure here; do not copy it into public documentation unless the
user explicitly requests that.

The canonical stable topology is:

```text
develop -> release/vX.Y.Z -> main + vX.Y.Z -> back-merge vX.Y.Z into develop
```

Use `git-flow` (AVH Edition) for `start`, `publish`, and `finish`. Do not replace
`git flow ... finish` with an improvised sequence of merges and tags when direct
pushes are allowed.

## Authorization boundary

Determine which outcome the user requested:

- **Prepare**: start or continue the release branch, harden it, validate it,
  draft release notes, and optionally publish the release branch. Stop before
  `git flow release finish` and before pushing any stable tag.
- **Publish**: perform the complete finish, push, GitHub Actions, curated release
  notes, and verification sequence in the same session.
- **Pre-release**: publish an `-rc.N` tag from the open release branch without
  finishing the release or changing `main`.
- **Hotfix**: start from `main` and use the hotfix lifecycle below.

Do not infer permission to publish a stable release from a request to prepare,
inspect, validate, or draft notes.

## Repository invariants

- Production branch: `main`
- Integration branch: `develop`
- Release prefix: `release/`
- Hotfix prefix: `hotfix/`
- Version tag prefix: empty; version arguments already include `v`
- Stable versions: `vX.Y.Z`
- Pre-release versions: `vX.Y.Z-rc.N`
- Stable tags are created by `git flow release finish` or
  `git flow hotfix finish` on the merge commit in `main`.
- The stable tag is back-merged into `develop` by the same finish operation.
- Release and hotfix merges must be merge commits. Do not squash or rebase them.
- `latest` is moved only by `.github/workflows/release.yml`, and only for stable
  tags.

The version passed to Git Flow is `vX.Y.Z`, not the full branch name. For
example, use `git flow release finish v0.2.0`, never
`git flow release finish release/v0.2.0`.

## Preflight

Run from the repository root before changing release state:

```sh
git status --short
git branch --show-current
git remote -v
git flow version
gh auth status
test "$(gh api user --jq .login)" = "gBearBest"
git fetch --prune origin
git fetch --tags --force origin
```

Require a clean working tree. Do not stash, discard, or absorb unrelated work
to make it clean.

If Git Flow is not initialized, initialize it once with repository defaults:

```sh
git flow init -d
```

Then verify the effective configuration. Stop if any value differs; do not use
`git flow init -f` to overwrite configuration silently.

```sh
test "$(git config --get gitflow.branch.master)" = "main"
test "$(git config --get gitflow.branch.develop)" = "develop"
test "$(git config --get gitflow.prefix.release)" = "release/"
test "$(git config --get gitflow.prefix.hotfix)" = "hotfix/"
test -z "$(git config --get gitflow.prefix.versiontag)"
```

Bring both long-lived branches to their remote state with fast-forward-only
updates:

```sh
git switch main
git pull --ff-only origin main
git switch develop
git pull --ff-only origin develop
```

Before selecting `vX.Y.Z`, inspect existing releases and the changes since the
last stable tag. Confirm that the target tag and release or hotfix branch do not
already exist locally or remotely unless the task is explicitly to continue
that branch.

```sh
git tag --list 'v*' --sort=-version:refname
git log --oneline main..develop
git show-ref --verify --quiet refs/tags/vX.Y.Z
git ls-remote --exit-code --tags origin refs/tags/vX.Y.Z
git show-ref --verify --quiet refs/heads/release/vX.Y.Z
git ls-remote --exit-code --heads origin refs/heads/release/vX.Y.Z
```

The `show-ref` and `ls-remote` checks should report absence for a new version.
Treat an existing tag as a hard stop; never move or replace a published version
tag.

## Stable release

### 1. Start and publish the release branch

Start from the updated `develop` branch:

```sh
git switch develop
git flow release start vX.Y.Z
```

On `release/vX.Y.Z`, make only release-hardening changes: version metadata,
curated notes, packaging corrections, and necessary release fixes. Commit all
intended changes before validation.

Publish the release branch so it is backed up and reviewable:

```sh
git flow release publish vX.Y.Z
```

If the branch already exists, switch to it and verify it is the intended open
Git Flow release instead of running `start` again.

### 2. Validate the exact release commit

Run the repository checks from the release branch:

```sh
(cd cli && make test)
(cd cli && make build)
.ctxpm/dependencies/skills/ctxpm/cli/ctxpm validate
git status --short
```

Require every check to pass and the tree to remain clean. Skip validation only
when the user explicitly narrows it; record any skipped check in the handoff.

Draft curated release notes now, before finishing. Summarize user-visible
changes and compatibility concerns rather than copying a raw commit list. The
body should normally contain:

```markdown
## Bear.CTXPM vX.Y.Z

Short release summary.

## Highlights

- Important CLI or protocol changes
- Installation or update behavior changes
- Compatibility notes

## Installation

Install latest stable:

`curl -fsSL https://raw.githubusercontent.com/gBearBest/Bear.CTXPM/latest/cli/install.sh | sh -s -- --scope global`

Pin this release:

`curl -fsSL https://raw.githubusercontent.com/gBearBest/Bear.CTXPM/latest/cli/install.sh | sh -s -- --scope global --version vX.Y.Z`

## Release assets

See the attached archives and `checksums.txt`.
```

Keep the notes in a temporary file such as
`/tmp/bear-ctxpm-release-vX.Y.Z.md` for the later `gh release edit` command.

### 3. Finish locally with Git Flow

Only enter this stage for an explicit stable-publication request. Re-fetch and
ensure `release/vX.Y.Z`, `main`, and `develop` still match their remote refs.
Then run:

```sh
git switch release/vX.Y.Z
git flow release finish --fetch --nopush --keepremote \
  -m "Bear.CTXPM vX.Y.Z" vX.Y.Z
```

Do not add `--notag`, `--nobackmerge`, `--nodevelopmerge`, or `--squash`.
`--keepremote` deliberately retains the remote release branch until the
completed topology has been published successfully.

The finish command must perform all of these local operations:

1. merge `release/vX.Y.Z` into `main` with `--no-ff`
2. create annotated tag `vX.Y.Z` on the resulting `main` merge commit
3. merge that stable tag into `develop` with `--no-ff`
4. delete the local release branch

Verify that exact topology before any release ref is pushed:

```sh
test "$(git rev-parse vX.Y.Z^{})" = "$(git rev-parse main)"
git merge-base --is-ancestor vX.Y.Z main
git merge-base --is-ancestor vX.Y.Z develop
test -z "$(git branch --list release/vX.Y.Z)"
git status --short
```

### 4. Publish the completed topology atomically

Push the two long-lived branches and the exact stable tag together. Fully
qualified refs avoid ambiguity and `--atomic` prevents a partial publication:

```sh
git push --atomic origin \
  refs/heads/main:refs/heads/main \
  refs/heads/develop:refs/heads/develop \
  refs/tags/vX.Y.Z:refs/tags/vX.Y.Z
```

After that succeeds, remove the retained remote release branch:

```sh
git push origin --delete release/vX.Y.Z
```

Never push the stable tag before `main` and `develop` contain the completed
Git Flow topology.

### 5. Complete the GitHub Release

The tag push starts `.github/workflows/release.yml`. It tests the CLI, builds
five platform archives, creates `checksums.txt`, creates the GitHub Release,
and moves `latest` for a stable tag.

Find the run for the exact tag and wait for it to finish successfully. A run
normally takes 1-3 minutes:

```sh
gh run list --workflow release.yml --event push --limit 20 \
  --json databaseId,headBranch,status,conclusion,url
gh run watch RUN_ID --exit-status
```

Do not run `gh release edit` until the release object exists. Then apply the
curated notes during the same release session:

```sh
gh release view vX.Y.Z --repo gBearBest/Bear.CTXPM \
  --json tagName,isDraft,isPrerelease,assets,url
gh release edit vX.Y.Z --repo gBearBest/Bear.CTXPM \
  --notes-file /tmp/bear-ctxpm-release-vX.Y.Z.md
```

Remove the temporary notes file only after the edit succeeds.

### 6. Verify the published release

```sh
git fetch --prune origin
git fetch --tags --force origin
test "$(git rev-parse vX.Y.Z^{})" = "$(git rev-parse origin/main)"
git merge-base --is-ancestor vX.Y.Z origin/develop
test "$(git rev-parse latest^{})" = "$(git rev-parse vX.Y.Z^{})"
git ls-remote --exit-code --heads origin refs/heads/release/vX.Y.Z
gh release view vX.Y.Z --repo gBearBest/Bear.CTXPM \
  --json tagName,isDraft,isPrerelease,assets,url
```

The remote release-branch lookup should report absence. Also verify that the
GitHub Release is not a draft or pre-release and contains these six assets:

- `ctxpm_X.Y.Z_darwin_arm64.tar.gz`
- `ctxpm_X.Y.Z_darwin_amd64.tar.gz`
- `ctxpm_X.Y.Z_linux_arm64.tar.gz`
- `ctxpm_X.Y.Z_linux_amd64.tar.gz`
- `ctxpm_X.Y.Z_windows_amd64.zip`
- `checksums.txt`

Do not report success merely because the tag exists. Publication is complete
only after the workflow, notes, assets, `latest`, branch topology, and release
branch cleanup have all been verified.

## Pre-release candidates

Release candidates remain on an open `release/vX.Y.Z` branch. Start and publish
that branch with the stable procedure, validate it, and record the current
`latest` commit before tagging:

```sh
latest_before=$(git rev-parse latest^{})
git switch release/vX.Y.Z
git tag -a vX.Y.Z-rc.N -m "Bear.CTXPM vX.Y.Z-rc.N"
git push origin refs/tags/vX.Y.Z-rc.N:refs/tags/vX.Y.Z-rc.N
```

Wait for the exact Actions run, apply curated notes, and verify that the GitHub
Release is marked as a pre-release. Then verify that `latest` did not move:

```sh
git fetch --tags --force origin
test "$(git rev-parse latest^{})" = "$latest_before"
gh release view vX.Y.Z-rc.N --repo gBearBest/Bear.CTXPM \
  --json tagName,isDraft,isPrerelease,assets,url
```

Do not run `git flow release finish` for an RC. Continue hardening the same
release branch for later candidates. When the stable release is approved, run
the normal stable finish with version `vX.Y.Z`; Git Flow creates the separate
stable tag on `main`.

## Hotfixes

Hotfixes use the same validation, notes, atomic publication, Actions, and final
verification requirements as stable releases, but start from `main`:

```sh
git switch main
git pull --ff-only origin main
git flow hotfix start vX.Y.Z

# Apply and commit the focused production fix, then validate.

git flow hotfix publish vX.Y.Z
git flow hotfix finish --fetch --nopush --keepremote \
  -m "Bear.CTXPM vX.Y.Z" vX.Y.Z
```

Verify that the stable tag points to `main` and is an ancestor of `develop`,
then atomically push `main`, `develop`, and the exact tag. Delete
`hotfix/vX.Y.Z` remotely only after that push succeeds.

Do not put unrelated development work into a hotfix.

## Protected-branch fallback

If branch protection prevents direct pushes, stop before running the local
finish command and use reviewed merge commits that preserve the same topology:

1. merge a PR from `release/vX.Y.Z` (or `hotfix/vX.Y.Z`) into `main` using a
   merge commit, never squash or rebase
2. fetch the resulting `main` and create annotated tag `vX.Y.Z` locally on that
   exact merge commit; do not push it yet
3. merge `main` into `develop` using a merge commit
4. fetch both branches, verify the tag is reachable from each, push the exact
   tag, and then delete the release or hotfix branch

Do not open a second PR from the release or hotfix branch directly into
`develop`; Git Flow back-merges the production result so the stable merge commit
and tag remain in the integration history.

## Failure handling

- If `finish` reports a merge conflict, inspect the conflicted files. Do not
  invent a conflict policy. Resolve only when the correct result is clear;
  otherwise ask the user. Stage the resolution, complete the pending merge with
  its existing message, and rerun the same `git flow ... finish` command so it
  can resume its remaining steps.
- If `finish` partially succeeded, inspect `main`, `develop`, the release or
  hotfix branch, and the tag before retrying. Git Flow is designed to resume; do
  not create a replacement tag or repeat merges manually.
- If the atomic push fails, leave the remote release or hotfix branch intact and
  inspect remote divergence. Never force-push `main`, `develop`, or a version
  tag.
- If Actions fails, do not move tags or create a second release for the same
  version. Diagnose and fix through the appropriate Git Flow branch, then choose
  a new version if a published tag would need different content.
- Release notes are not optional cleanup. Apply and verify them before ending a
  successful publication session.

## Prohibited shortcuts

Do not:

- create the stable tag on `release/*`, `hotfix/*`, or `develop`
- manually tag `main` before the release or hotfix branch has been merged into
  it
- pass `release/vX.Y.Z` or `hotfix/vX.Y.Z` as the Git Flow version argument
- squash or rebase a release/hotfix merge
- merge the release or hotfix branch directly back into `develop` after
  finishing `main`
- push a stable tag before the completed `main` and `develop` topology
- force-move a semantic version tag
- leave auto-generated GitHub release notes uncurated
- advance `latest` for a pre-release
