# ctxpm

Read `ctxpm-yaml.md` in this directory before editing `ctxpm.yaml`.

## Purpose

`ctxpm` manages the AI resource lifecycle for a Bear.CTXPM project:

- install
- add
- list
- validate
- check-updates
- update
- remove

## Resource Model

Treat each managed resource as:

1. a **resource root**
2. an **entry file**
3. an optional **upstream source**

The canonical root lives under `.ctxpm/packages/` or `.ctxpm/dependencies/`. Compatibility paths only expose that root to agent-specific discovery directories.

## Dependency Version Rules

- Git resources use the installed commit SHA.
- Single-file non-Git resources use `sha256:<hex>`.
- Directory non-Git resources use `sha256tree:<hex>`.

Always version the whole resolved resource root, not only one guessed companion file.

## Installation Rules

1. Materialize the resource root from its source.
2. Validate that the result matches `layout`.
3. Validate that `entry` exists inside the root.
4. Install or replace the canonical root.
5. Repair compatibility links.
6. Only rewrite `ctxpm.yaml` when a recorded dependency version actually changed.

## Bundled `ctxpm` Registration

The bundled `ctxpm` skill should itself be modeled as a directory dependency:

```yaml
- name: ctxpm
  type: skill
  layout: dir
  path: .ctxpm/dependencies/skills/ctxpm
  entry: SKILL.md
  source:
    type: git
    url: https://github.com/gBearBest/Bear.CTXPM
    path: resources/skills/ctxpm
    entry: SKILL.md
```
