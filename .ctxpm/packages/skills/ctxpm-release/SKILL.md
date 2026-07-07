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

## Repository release rules

- `main` is the latest stable release branch
- `develop` is the ongoing integration branch
- stable releases are created from `refs/heads/main`
- release tags must use semantic version format: `vX.Y.Z`
- pre-release tags may use a hyphen suffix such as `vX.Y.Z-rc.1`
- the installer supports `latest` or an explicit version tag such as `v0.1.0`
- do not use `main` as a release selector

## Important repository-specific pitfalls

### 1. Do not rely on implicit `main` ref resolution

This repository still has a historical tag named `main`. Because of that, commands like:

```sh
git push origin main
```

can be ambiguous.

Use explicit refs instead:

```sh
git push origin refs/heads/main:refs/heads/main
git push origin refs/heads/develop:refs/heads/develop
git push origin refs/tags/v0.1.0:refs/tags/v0.1.0
```

### 2. Keep the procedure in this skill, not public docs

If future sessions need the release workflow, update this skill. Do not add release-flow explanations to README, INSTALL, or other public docs unless the user explicitly requests that.

### 3. Release notes must be curated

The workflow creates a GitHub Release automatically, but its default generated notes are not enough. After the release appears, replace or update the release body with a curated summary.

## Standard stable release procedure

### 1. Verify local state

From the repository root:

```sh
git status --short
git rev-parse --abbrev-ref HEAD
git log --oneline --decorate -5
```

Make sure:

- the working tree is clean
- the stable commit is on `main`
- `develop` is in the expected state for post-release continuation

### 2. Run the existing validation commands

Mirror the workflow's local checks before tagging:

```sh
(cd cli && make test)
(cd cli && make build)
```

If the change is docs-only and the user explicitly says not to run these again, follow that instruction. Otherwise use the same validation path as CI.

### 3. Push branch refs explicitly

Because `main` is ambiguous in this repository, push explicit branch refs:

```sh
git push origin refs/heads/main:refs/heads/main
git push -u origin refs/heads/develop:refs/heads/develop
```

Only push `develop` if the local branch is intended to become or remain the tracked integration branch.

### 4. Create and push the release tag

Create an annotated tag from `main`:

```sh
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0" refs/heads/main
git push origin refs/tags/v0.1.0:refs/tags/v0.1.0
```

Do not tag from `develop` unless `develop` and `main` intentionally point at the same release commit and that state has already been pushed to `main`.

### 5. Wait for the GitHub Actions release workflow

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

### 6. Update the GitHub Release notes

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

### 7. Verify published results

Confirm all of the following:

- the `vX.Y.Z` release exists
- the release is not marked as draft
- stable releases are not marked as pre-release
- expected assets are uploaded
- `latest` points at the same commit as the stable tag

## Pre-release procedure

Use the same workflow, but with a pre-release tag such as:

```sh
git tag -a v0.2.0-rc.1 -m "Bear.CTXPM v0.2.0-rc.1" refs/heads/main
git push origin refs/tags/v0.2.0-rc.1:refs/tags/v0.2.0-rc.1
```

Expected behavior:

- GitHub Release is created as a pre-release
- assets are still built and uploaded
- `latest` must not move

## Right and wrong examples

Wrong:

```sh
git push origin main
git tag v0.1.0
```

Why wrong:

- `main` can resolve ambiguously in this repository
- lightweight tags are less explicit than the annotated tags used here

Right:

```sh
git push origin refs/heads/main:refs/heads/main
git tag -a v0.1.0 -m "Bear.CTXPM v0.1.0" refs/heads/main
git push origin refs/tags/v0.1.0:refs/tags/v0.1.0
```

Wrong:

- leaving the auto-generated release notes unchanged
- documenting the full release procedure in public docs without user approval

Right:

- curating the release body after the release is published
- keeping the reusable procedure in this skill

## Learnings

- This repository has a legacy `main` tag, so release pushes must use explicit `refs/heads/...` and `refs/tags/...` syntax to avoid ambiguity.
- `latest` is maintained by the release workflow as the moving stable tag; release operators should verify it after every stable publication.
- The project owner wants the release workflow preserved as a project-level skill rather than as general version-controlled documentation pages.
