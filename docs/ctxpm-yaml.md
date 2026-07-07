# ctxpm.yaml Format

This document defines the `ctxpm.yaml` format used by Bear.CTXPM projects.

AI agents must read this format before creating, editing, auditing, or deleting `ctxpm.yaml` entries.

## Top-Level Structure

```yaml
version: 2

project:
  name: your-project-name

agents:
  - codex

update_policy:
  enabled: true
  interval: 1d
  include_self: true

dependencies: []

packages: []

entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
```

## Top-Level Fields

| Field | Required | Description |
| --- | --- | --- |
| `version` | Yes | Format version. Use `2`. |
| `project.name` | Yes | Project name. |
| `agents` | Yes | Confirmed agent profiles used by the project. |
| `update_policy` | No | Policy for dependency update checks. |
| `dependencies` | Yes | External AI resources managed under `.ctxpm/dependencies/`. |
| `packages` | Yes | Project-local AI resources managed under `.ctxpm/packages/`. |
| `entrypoints` | Yes | Root Markdown entrypoint files managed for each agent. |

## Core Concepts

### Resource Root

The unit of install, update, validate, versioning, and compatibility is the **resource root**.

- A resource root can be a single file or a directory.
- `path` always points to the canonical local resource root.
- Compatibility paths must point to the same root.

### Entry File

The **entry file** is the file an AI agent should read first inside the resource root.

- For `layout: dir`, `entry` is required and is relative to the resource root.
- For `layout: file`, `entry` should match the file name.
- `source.entry` describes the upstream entry file inside the upstream source root.

## Resource Types

`type` must be one of:

- `skill`
- `rule`
- `spec`
- `prompt`
- `mcp`

Use these directory mappings:

| Type | Package Directory | Dependency Directory |
| --- | --- | --- |
| `skill` | `.ctxpm/packages/skills/` | `.ctxpm/dependencies/skills/` |
| `rule` | `.ctxpm/packages/rules/` | `.ctxpm/dependencies/rules/` |
| `spec` | `.ctxpm/packages/specs/` | `.ctxpm/dependencies/specs/` |
| `prompt` | `.ctxpm/packages/prompts/` | `.ctxpm/dependencies/prompts/` |
| `mcp` | `.ctxpm/packages/mcp/` | `.ctxpm/dependencies/mcp/` |

## Shared Resource Fields

Every `dependencies` or `packages` item uses the same root-level shape:

| Field | Required | Description |
| --- | --- | --- |
| `name` | Yes | Stable resource name. |
| `type` | Yes | Resource type. |
| `layout` | Yes | `file` or `dir`. |
| `path` | Yes | Canonical local resource root under `.ctxpm/dependencies/` or `.ctxpm/packages/`. |
| `entry` | Yes | Entry file relative to the canonical root. |
| `compatibility` | No | Compatibility exposure paths outside `.ctxpm`. |
| `source` | Dependencies only | Upstream source metadata. |
| `version` | Dependencies only | Installed dependency version. |

Rules:

- `layout: file` means `path` points to a file resource root.
- `layout: dir` means `path` points to a directory resource root.
- For `layout: file`, `entry` should match the file name in `path`.
- For `layout: dir`, `entry` must exist inside the directory root.

## Dependency Sources

`source.type` supports:

- `git`
- `url`
- `archive`

Legacy `source.type: github` may be treated as `git` during transition, but new manifests should write `git`.

### Git Source

Use Git for single-file or directory resources stored in a repository.

```yaml
dependencies:
  - name: reviewer
    type: skill
    layout: dir
    path: .ctxpm/dependencies/skills/reviewer
    entry: SKILL.md
    source:
      type: git
      url: https://github.com/example/ai-resources
      ref: main
      path: skills/reviewer
      entry: SKILL.md
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/reviewer
```

Rules:

- `source.path` is the upstream resource root inside the repository.
- `source.entry` is the upstream entry file relative to `source.path`.
- `version` is the installed commit SHA for the resource root.

### Single-File URL Source

```yaml
dependencies:
  - name: release-rule
    type: rule
    layout: file
    path: .ctxpm/dependencies/rules/release-rule.md
    entry: release-rule.md
    source:
      type: url
      url: https://example.com/ai/rules/release-rule.md
      entry: release-rule.md
    version: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Rules:

- When `source.files` is omitted, the URL source is a single-file resource.
- Single-file URL sources must use `layout: file`.
- `version` must use `sha256:<hex>`.

### Multi-File URL Source

```yaml
dependencies:
  - name: reviewer
    type: skill
    layout: dir
    path: .ctxpm/dependencies/skills/reviewer
    entry: SKILL.md
    source:
      type: url
      url: https://example.com/ai/reviewer/
      files:
        - SKILL.md
        - prompts/release.md
        - rules/review.md
      entry: SKILL.md
    version: sha256tree:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789
```

Rules:

- `url` is the base URL or directory prefix.
- `files` lists every file that belongs to the resource root.
- `files` entries must be relative, must not be absolute, and must not contain `..`.
- `source.entry` must be one of the listed files.
- Multi-file URL sources must use `layout: dir`.
- `version` must use `sha256tree:<hex>`.

### Archive Source

```yaml
dependencies:
  - name: analyzer
    type: skill
    layout: dir
    path: .ctxpm/dependencies/skills/analyzer
    entry: SKILL.md
    source:
      type: archive
      url: https://example.com/downloads/analyzer.zip
      path: skills/analyzer
      entry: SKILL.md
    version: sha256tree:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210
```

Rules:

- The archive is downloaded and extracted before resolving the resource root.
- `source.path` is the resource root inside the extracted archive.
- `source.entry` is relative to `source.path`.
- Use `sha256:<hex>` for file roots and `sha256tree:<hex>` for directory roots.

## Version Semantics

Dependency versions must describe the resolved **resource root**, not just a single guessed file.

| Source shape | Version format |
| --- | --- |
| Git file or directory root | Full commit SHA |
| Non-Git single-file root | `sha256:<hex>` |
| Non-Git directory root | `sha256tree:<hex>` |

`sha256tree` is computed from the full directory tree:

1. Materialize the resource root.
2. Recursively collect every file.
3. Sort by relative path.
4. Hash each file content with SHA-256.
5. Hash the resulting path-and-hash manifest.

Do not use branch names, tags, timestamps, filenames, or vague labels as dependency versions.

## Packages

`packages` are project-local resources and use the same root model:

```yaml
packages:
  - name: project-review-rules
    type: rule
    layout: dir
    path: .ctxpm/packages/rules/project-review-rules
    entry: README.md
    compatibility:
      - .agents/rules/project-review-rules
```

Rules:

- Packages normally omit `source` and `version`.
- Use the same `layout` / `path` / `entry` model as dependencies.

## `entrypoints`

`entrypoints` records root Markdown files that contain managed `ctxpm` blocks.

```yaml
entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
  claude-code:
    file: CLAUDE.md
    mode: managed
```

Rules:

- Keys should match entries in `agents`.
- `file` is the root entrypoint Markdown file.
- `mode` should be `managed` when the file contains a managed `ctxpm` block.

## Compatibility Paths

Use `compatibility` when an agent expects resources in a default discovery location.

Rules:

- Compatibility paths point from agent-recognizable discovery locations to canonical `.ctxpm` roots.
- Record precise paths in `ctxpm.yaml` even when `.gitignore` uses broader ignore rules.
- `ctxpm install` should repair missing compatibility links for both dependencies and packages.

## Editing Rules

When modifying `ctxpm.yaml`:

1. Preserve unknown fields unless they directly conflict with the requested change.
2. Do not delete unrelated resources.
3. Do not reorder unrelated entries just for formatting.
4. Keep paths relative to the project root.
5. Keep `dependencies` and `packages` semantically separate.
6. Prefer minimal text edits over full-file rewrites when only a small field changes.
