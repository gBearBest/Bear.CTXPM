---
name: ctxpm-release
description: Use this skill when publishing a Bear.CTXPM release, preparing a semantic version tag, updating GitHub release notes, or verifying that latest points at the newest stable release.
---

# Bear.CTXPM release workflow

Use this skill for Bear.CTXPM version publishing work.

This repository publishes stable releases from semantic version tags such as `v0.1.0`. The public release mechanics live in `.github/workflows/release.yml`, while the human procedure should live in this skill rather than in README or general docs unless the user explicitly asks for public documentation.

## What this skill is for

- publishing a new stable release such as `v0.1.0`
- publishing a pre-release tag such as `v0.2.0-rc.1`
- updating the GitHub Release body after assets are published
- verifying that `latest` points at the newest stable release

## Repository release rules (Git Flow)

- `main` must continue to represent the newest stable release line
- `develop` is the ongoing integration branch for the next release
- stable releases are prepared on `release/vX.Y.Z` branches cut from `develop`
- release candidates and other pre-release tags are created from the `release/*` branch, not from `main`
- the final stable tag `vX.Y.Z` is created from `main` only after the release branch is finished into `main`
- hotfixes are prepared on `hotfix/vX.Y.Z` branches cut from `main`
- release tags must use semantic version format: `vX.Y.Z`
- pre-release tags may use a hyphen suffix such as `vX.Y.Z-rc.1`
- the installer supports `latest` or an explicit version tag such as `v0.1.0`
- do not use `main` as a release selector

## Important repository-specific pitfalls

### 1. Keep the procedure in this skill, not public docs

If future sessions need the release workflow, update this skill. Do not add release-flow explanations to README, INSTALL, or other public docs unless the user explicitly requests that.

### 2. Release notes must be curated

The workflow creates a GitHub Release automatically, but its default generated notes are not enough. After the release appears, replace or update the release body with a curated summary.

### 3. Do not move `main` forward for pre-releases

If you want to publish `vX.Y.Z-rc.N`, keep that work on `release/vX.Y.Z`. Tagging a release candidate from `main` would make `main` contain code that is not yet the latest stable release, which breaks this repository's Git Flow contract.

## Standard stable release procedure (Git Flow)

### 1. Verify local state

From the repository root:

```sh
git status --short
git rev-parse --abbrev-ref HEAD
git log --oneline --decorate -5
```

Make sure:

- the working tree is clean
- `develop` contains the intended release content
- `main` still reflects the previous stable release
- the target version does not already exist as a local or remote tag

Optional checks:

```sh
git fetch --tags origin
git tag --list 'v*'
git ls-remote --tags origin 'v*'
```

### 2. Create the release branch from `develop`

Example for `v0.1.0`:

```sh
git switch develop
git pull --ff-only origin develop
git switch -c release/v0.1.0
```

On `release/v0.1.0`, do only release-hardening work (version bumps, changelog/release-note curation, last-minute fixes allowed by release policy).

### 3. Run the existing validation commands on the release branch

Mirror the workflow's local checks before tagging:

```sh
(cd cli && make test)
(cd cli && make build)
```

If the change is docs-only and the user explicitly says not to run these again, follow that instruction. Otherwise use the same validation path as CI.

### 4. Push the release branch

```sh
git push -u origin release/v0.1.0
```

### 5. Create and push the stable tag on the release branch

**IMPORTANT**: Tag the release branch BEFORE merging to main, so both main and develop can see the tag after completion.

```sh
git switch release/v0.1.0
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0"
git push origin v0.1.0
```

### 6. Merge the tagged release branch into `main`

Preferred model when branch protection or review is enabled:

1. open a PR from `release/v0.1.0` into `main`
2. merge it with a merge commit after approval

Equivalent direct git commands:

```sh
git switch main
git pull --ff-only origin main
git merge --no-ff release/v0.1.0 -m "Merge release/v0.1.0 into main"
git push origin main
```

### 7. Back-merge the same release branch into `develop`

Preferred model when branch protection or review is enabled:

1. open a PR from `release/v0.1.0` into `develop`
2. merge it with a merge commit after approval

Equivalent direct git commands:

```sh
git switch develop
git pull --ff-only origin develop
git merge --no-ff release/v0.1.0 -m "Merge release/v0.1.0 back into develop"
git push origin develop
```

### 8. Delete the release branch after both merges are complete

```sh
git branch -d release/v0.1.0
git push origin --delete release/v0.1.0
```

### 9. Verify tag visibility from both branches

Confirm the tag is reachable from both main and develop:

```sh
git fetch --tags origin
git switch main
git tag --merged | grep v0.1.0
git switch develop
git tag --merged | grep v0.1.0
```

Both commands should show `v0.1.0`. If develop doesn't show it, the tag was created on the wrong commit.

### 10. Wait for the GitHub Actions release workflow

The release workflow will:

- run `go test ./...`
- build assets for:
  - `darwin/arm64`
  - `darwin/amd64`
  - `linux/arm64`
  - `linux/amd64`
  - `windows/amd64`
- generate `checksums.txt`
- create the GitHub Release for the tag
- move `latest` to the same commit for stable releases only

Pre-release tags should not advance `latest`.

### 11. Update the GitHub Release notes

After the release appears, replace the default generated notes with a curated body.

Recommended structure:

~~~md
## Bear.CTXPM v0.1.0

Short summary of what this version represents.

## Highlights

- Core protocol or CLI changes
- Installation or release behavior changes
- Important compatibility notes

## Installation

Install latest stable:

```sh
curl -fsSL https://raw.githubusercontent.com/gBearBest/Bear.CTXPM/latest/cli/install.sh | sh -s -- --scope global
```

Pin this release:

```sh
curl -fsSL https://raw.githubusercontent.com/gBearBest/Bear.CTXPM/latest/cli/install.sh | sh -s -- --scope global --version v0.1.0
```

## Release assets

- platform archives
- checksum file
~~~

Focus the notes on what users need to know, not on a raw commit dump.

If GitHub CLI is available, a practical edit command is:

```sh
gh release edit v0.1.0 --notes-file /path/to/release-notes.md
```

### 12. Verify published results

Confirm all of the following:

- the `vX.Y.Z` release exists
- the release is not marked as draft
- stable releases are not marked as pre-release
- expected assets are uploaded
- `latest` points at the same commit as the stable tag
- **the tag is visible from both `main` and `develop` branches**

Useful checks:

```sh
git fetch --tags --force origin
git rev-list -n 1 v0.1.0
git rev-list -n 1 latest
git switch main && git tag --merged | grep v0.1.0
git switch develop && git tag --merged | grep v0.1.0
gh release view v0.1.0 --json tagName,isDraft,isPrerelease,assets,url
```

## Pre-release procedure (Git Flow-compatible)

Use pre-release tags to validate a release branch before the final stable finish. Keep the release branch open while iterating on release candidates.

Example for `v0.2.0-rc.1`:

1. cut the release branch from `develop`:

```sh
git switch develop
git pull --ff-only origin develop
git switch -c release/v0.2.0
```

2. run release-hardening changes and validation on `release/v0.2.0`

3. create and push the annotated pre-release tag from the release branch:

```sh
git switch release/v0.2.0
git tag -a v0.2.0-rc.1 -m "Bear.CTXPM v0.2.0-rc.1"
git push origin v0.2.0-rc.1
```

4. verify the resulting GitHub Release is marked as a pre-release

5. continue hardening on the same `release/v0.2.0` branch and create `v0.2.0-rc.2`, `v0.2.0-rc.3`, and so on as needed

6. when the branch is ready for final release, continue with the stable release finish flow:
   merge `release/v0.2.0` into `main`, tag `v0.2.0` from `main`, then back-merge the same release branch into `develop`

Expected release-workflow behavior:

- GitHub Release is created as a pre-release
- assets are still built and uploaded
- `latest` must not move

Do not tag release candidates from `main`.

## Hotfix procedure (Git Flow)

For urgent production fixes:

1. branch from `main`: `hotfix/vX.Y.Z`
2. apply only the urgent fix plus any necessary release-hardening updates
3. run the same validation commands used for stable releases
4. push the hotfix branch
5. create and push the annotated stable tag on the hotfix branch
6. merge the tagged hotfix branch into `main`
7. merge the same hotfix branch back into `develop`
8. delete the hotfix branch after both merges complete

Example:

```sh
git switch main
git pull --ff-only origin main
git switch -c hotfix/v0.1.1

# apply fix, then validate
(cd cli && make test)
(cd cli && make build)

git push -u origin hotfix/v0.1.1

git tag -a v0.1.1 -m "Bear.CTXPM v0.1.1"
git push origin v0.1.1

git switch main
git pull --ff-only origin main
git merge --no-ff hotfix/v0.1.1 -m "Merge hotfix/v0.1.1 into main"
git push origin main

git switch develop
git pull --ff-only origin develop
git merge --no-ff hotfix/v0.1.1 -m "Merge hotfix/v0.1.1 back into develop"
git push origin develop

git branch -d hotfix/v0.1.1
git push origin --delete hotfix/v0.1.1
```

## Right and wrong examples

Wrong:

```sh
git push origin main
git tag v0.1.0
```

Why wrong:

- lightweight tags are less explicit than the annotated tags used here
- this skips the required Git Flow release-branch finish sequence (`main` + back-merge to `develop`)

Right:

```sh
git switch -c release/v0.1.0 develop
git push -u origin release/v0.1.0
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0"
git push origin v0.1.0
git switch main
git merge --no-ff release/v0.1.0
git push origin main
git switch develop
git merge --no-ff release/v0.1.0
git push origin develop
```

Wrong:

```sh
git switch main
git merge --no-ff release/v0.1.0
git push origin main
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0" main
git push origin v0.1.0
```

Why wrong:

- tagging on `main` after the merge means the tag is on the merge commit, not on the release branch
- when the release branch merges back to `develop`, the tag is not visible from `develop`

Wrong:

```sh
git switch main
git tag -a v0.2.0-rc.1 -m "Bear.CTXPM v0.2.0-rc.1"
git push origin v0.2.0-rc.1
```

Why wrong:

- it makes `main` point at a release candidate instead of the latest stable line
- it breaks the contract that pre-release tags come from `release/*`

Right:

```sh
git switch release/v0.2.0
git tag -a v0.2.0-rc.1 -m "Bear.CTXPM v0.2.0-rc.1"
git push origin v0.2.0-rc.1
```

Wrong:

- leaving the auto-generated release notes unchanged
- documenting the full release procedure in public docs without user approval
- tagging directly from `develop` for stable release

Right:

- curating the release body after the release is published
- keeping the reusable procedure in this skill
- finishing `release/*` into both `main` and `develop`

## Learnings

- `latest` is maintained by the release workflow as the moving stable tag; release operators should verify it after every stable publication.
- The project owner wants the release workflow preserved as a project-level skill rather than as general version-controlled documentation pages.
- Git Flow is the canonical release model:
  stable release: `develop` -> `release/*` -> **tag on `release/*`** -> merge to `main` -> back-merge to `develop`
  pre-release: `develop` -> `release/*` -> tag `-rc.N` on `release/*` -> finish to `main` only when ready for stable
  hotfix: `main` -> `hotfix/*` -> **tag on `hotfix/*`** -> merge to `main` -> back-merge to `develop`
- Tags must be created on the release/hotfix branch BEFORE merging to main, so that both `main` and `develop` can see the tag after the back-merge completes.
- **Critical mistake**: tagging `main` after merging creates the tag on the merge commit, making it invisible to `develop` after back-merge. Always tag the branch, not the merge result.
