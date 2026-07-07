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

- `main` is the latest stable release branch
- `develop` is the ongoing integration branch
- stable releases are prepared on `release/vX.Y.Z` branches cut from `develop`
- stable tags are created from `main` after finishing the release branch
- release tags must use semantic version format: `vX.Y.Z`
- pre-release tags may use a hyphen suffix such as `vX.Y.Z-rc.1`
- the installer supports `latest` or an explicit version tag such as `v0.1.0`
- do not use `main` as a release selector

## Important repository-specific pitfalls

### 1. Keep the procedure in this skill, not public docs

If future sessions need the release workflow, update this skill. Do not add release-flow explanations to README, INSTALL, or other public docs unless the user explicitly requests that.

### 2. Release notes must be curated

The workflow creates a GitHub Release automatically, but its default generated notes are not enough. After the release appears, replace or update the release body with a curated summary.

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

### 5. Finish release: merge to `main`, tag, then back-merge to `develop`

1) Merge release into `main` and push:

```sh
git switch main
git pull --ff-only origin main
git merge --no-ff release/v0.1.0 -m "Merge release/v0.1.0 into main"
git push origin main
```

2) Create and push the annotated tag from `main`:

```sh
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0" main
git push origin v0.1.0
```

3) Merge the same release branch back into `develop` and push:

```sh
git switch develop
git pull --ff-only origin develop
git merge --no-ff release/v0.1.0 -m "Merge release/v0.1.0 back into develop"
git push origin develop
```

4) Delete release branch after both merges are complete:

```sh
git branch -d release/v0.1.0
git push origin --delete release/v0.1.0
```

Do not tag from `develop`. In Git Flow, stable tags are created from `main` after the release-branch merge.

### 6. Wait for the GitHub Actions release workflow

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

### 7. Update the GitHub Release notes

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

### 8. Verify published results

Confirm all of the following:

- the `vX.Y.Z` release exists
- the release is not marked as draft
- stable releases are not marked as pre-release
- expected assets are uploaded
- `latest` points at the same commit as the stable tag

## Pre-release procedure (Git Flow-compatible)

Use the same Git Flow release-branch flow, but with a pre-release tag such as:

```sh
git tag -a v0.2.0-rc.1 -m "Bear.CTXPM v0.2.0-rc.1" main
git push origin v0.2.0-rc.1
```

Expected behavior:

- GitHub Release is created as a pre-release
- assets are still built and uploaded
- `latest` must not move

## Hotfix procedure (Git Flow)

For urgent production fixes:

1) branch from `main`: `hotfix/vX.Y.Z`
2) apply and validate fix
3) merge hotfix into `main`
4) tag on `main` and push tag
5) merge hotfix back into `develop`
6) delete hotfix branch

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
git switch main
git merge --no-ff release/v0.1.0
git push origin main
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0" main
git push origin v0.1.0
git switch develop
git merge --no-ff release/v0.1.0
git push origin develop
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
- Git Flow is now the canonical release model: `develop` -> `release/*` -> merge to `main` (tag) -> back-merge to `develop`.
