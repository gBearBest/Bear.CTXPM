# ctxpm.yaml Format

This document defines the `ctxpm.yaml` formats used by Bear.CTXPM projects.
The original sections describe schema 1.0. The schema 2.0 section below defines
source groups and derived local paths; use the rules for the declared version.

AI agents must read this format before creating, editing, auditing, or deleting `ctxpm.yaml` entries.

## Top-Level Structure

```yaml
version: 1.0

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
```

## Top-Level Fields

| Field | Required | Description |
| --- | --- | --- |
| `version` | Yes | Format version. Use `1.0`. |
| `project.name` | Yes | Project name. |
| `agents` | Yes | Confirmed agent profiles used by the project. |
| `update_policy` | No | Policy for conversational dependency update checks and confirmation-driven application. |
| `dependencies` | Yes | External AI resources managed under `.ctxpm/dependencies/`. |
| `packages` | Yes | Project-local AI resources managed under `.ctxpm/packages/`. |

`update_policy.include_self` controls whether routine `check-updates` results include the registered `ctxpm` release unit. That single dependency result covers the CLI, bundled skill, and managed entrypoint; update it with `ctxpm update ctxpm` or `ctxpm update --all`.

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
- `memory`
- `mcp`

Use these directory mappings:

| Type | Package Directory | Dependency Directory |
| --- | --- | --- |
| `skill` | `.ctxpm/packages/skills/` | `.ctxpm/dependencies/skills/` |
| `rule` | `.ctxpm/packages/rules/` | `.ctxpm/dependencies/rules/` |
| `spec` | `.ctxpm/packages/specs/` | `.ctxpm/dependencies/specs/` |
| `prompt` | `.ctxpm/packages/prompts/` | `.ctxpm/dependencies/prompts/` |
| `memory` | `.ctxpm/packages/memories/` | `.ctxpm/dependencies/memories/` |
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
| `source` | Dependencies only | Upstream source metadata. |
| `version` | Dependencies only | Installed dependency version metadata for the resolved resource root. |

Rules:

- `layout: file` means `path` points to a file resource root.
- `layout: dir` means `path` points to a directory resource root.
- For `layout: file`, `entry` should match the file name in `path`.
- For `layout: dir`, `entry` must exist inside the directory root.
- `memory` directory resources should use `MEMORY.md` as the canonical entry file.

## Dependency Sources

`source.type` supports:

- `git`
- `url`
- `archive`

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
```

Rules:

- `source.path` is the upstream resource root inside the repository.
- `source.entry` is the upstream entry file relative to `source.path`.
- `version` is the most recent commit SHA at or before the installed checkout that changed the resolved `source.path`.
- For Git dependencies, `version` is path-scoped to the resource root and may differ from the checkout `HEAD` commit when later commits did not modify that resource path.

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
| Registered `ctxpm` release unit | Normalized GitHub Release tag, such as `v0.1.14` |
| Git file or directory root | Full commit SHA of the latest commit that changed `source.path` |
| Non-Git single-file root | `sha256:<hex>` |
| Non-Git directory root | `sha256tree:<hex>` |

During normal AI work, evaluate `update_policy` at a session boundary. If checks are enabled and due, the agent should call the bundled CLI to check for updates, then ask for confirmation only when an update is available and before invoking `ctxpm update`. A no-update result should remain silent in user-facing progress and final responses unless the user explicitly asks about `ctxpm` status. Updating the registered `ctxpm` dependency by name or through `--all` upgrades its complete release unit.

`sha256tree` is computed from the full directory tree:

1. Materialize the resource root.
2. Recursively collect every file.
3. Sort by relative path.
4. Hash each file content with SHA-256.
5. Hash the resulting path-and-hash manifest.

Do not use branch names, tags, timestamps, filenames, or vague labels as ordinary dependency versions. The registered `ctxpm` release unit is the explicit exception: its version is a stable GitHub Release tag.

The registered `ctxpm` dependency keeps the normalized Release tag installed for the project in its normal `version` field. Its update availability is determined by the latest stable GitHub Release because the CLI, complete bundled skill directory, and entrypoint template must advance together. `ctxpm install` installs exactly the recorded Release instead of discovering a newer one; legacy commit-SHA registrations remain installable until `ctxpm update ctxpm` migrates them to Release-version semantics.

## Packages

`packages` are project-local resources and use the same root model:

```yaml
packages:
  - name: project-review-rules
    type: rule
    layout: dir
    path: .ctxpm/packages/rules/project-review-rules
    entry: README.md
```

Rules:

- Packages normally omit `source` and `version`.
- Use the same `layout` / `path` / `entry` model as dependencies.

## Compatibility Paths

Compatibility paths expose a canonical `.ctxpm` resource root at the agent-recognizable discovery location expected by each agent profile.

`compatibility` is not a field in `ctxpm.yaml`. Paths are derived automatically from all declared agents in `agents` and are never written into the manifest. The tool manages symlinks based on derivation alone.

Derived paths follow this formula:

- `layout: dir` → `<agent-dir-prefix>/<type-plural>/<name>`
- `layout: file` → `<agent-dir-prefix>/<type-plural>/<filename>` (last segment of `path`)

Agent profile to directory prefix:

| Agent profile | Directory prefix |
| --- | --- |
| `generic` | `.agents/` |
| `codex` | `.agents/` |
| `claude-code` | `.claude/` |
| `antigravity` | `.antigravity/` |
| `gemini-cli` | `.gemini/` |
| `cursor` | `.cursor/` |
| `windsurf` | `.windsurf/` |
| `kiro` | `.kiro/` |
| `opencode` | `.opencode/` |
| `grok` | `.grok/` |

Resource type to subdirectory:

| Type | Subdirectory |
| --- | --- |
| `skill` | `skills` |
| `rule` | `rules` |
| `spec` | `specs` |
| `prompt` | `prompts` |
| `memory` | `memories` |
| `mcp` | `mcp` |

Rules:

- Compatibility paths point from agent-recognizable discovery locations to canonical `.ctxpm` roots.
- `ctxpm install` creates and repairs compatibility symlinks derived from `agents` for both dependencies and packages.
- `ctxpm install` also ensures `.gitignore` contains safe compatibility ignore rules.

## Editing Rules

When modifying `ctxpm.yaml`:

1. Preserve unknown fields unless they directly conflict with the requested change.
2. Do not delete unrelated resources.
3. Do not reorder unrelated entries just for formatting.
4. Keep paths relative to the project root.
5. Keep `dependencies` and `packages` semantically separate.
6. Prefer minimal text edits over full-file rewrites when only a small field changes.
7. Do not add `compatibility` or `entrypoints` fields — they are not part of the schema. Compatibility paths are derived automatically; entrypoints always use `.ctxpm/AGENTS.md` as the managed source plus root-level alias symlinks.

## Schema 2.0: Dependencies Grouped by Source

Schema `2.0` is supported alongside `1.0`. Existing projects keep their current
schema during ordinary lifecycle commands. To adopt grouping, use the explicit
manifest migration command after upgrading the project's locked ctxpm release.

```yaml
version: "2.0"
project:
  name: example
agents:
  - codex

dependencies:
  - source:
      type: git
      url: https://github.com/example/ai-resources.git
      ref: main
    type: skill
    path: skills/{name}
    resources:
      - name: reviewer
        version: 0123456789abcdef0123456789abcdef01234567
      - name: legacy-helper
        path: extras/legacy-helper
        version: 89abcdef0123456789abcdef0123456789abcdef01

  - source:
      type: git
      url: https://github.com/example/ai-resources.git
      ref: main
    type: rule
    path: rules/{name}.md
    resources:
      - name: security
        version: 0123456789abcdef0123456789abcdef01234567
      - name: notice
        path: NOTICE
        layout: file
        version: 0123456789abcdef0123456789abcdef01234567

packages:
  - name: create-moment
    type: skill
  - name: project-policy
    type: rule
    layout: file
    entry: project-policy.md
```

### Group and resource fields

- `source.type`, `url`, and `ref` define the group's common upstream source.
  Different sources or refs require separate groups. `source.entry` and `files`
  retain their existing meanings; they are optional when the entry is inferable.
- Group `type` supplies the default resource type; an individual resource's
  `type` overrides it. `source.type` is the transport, not the AI resource type.
- Group `path` is an upstream path template for Git/archive resources. It
  defaults to `{name}`. Only the literal `{name}` placeholder is supported.
- Resource `path`, when present, is a concrete upstream path that completely
  overrides the template. It is relative to the repository/extracted root,
  never relative to the group template. `.` selects that root itself.
- Every resource declares a unique `name` and keeps its own `version`.
  Grouping does not synchronize updates or introduce a repository-wide version.
- `layout` and `entry` are optional resource fields. A filename extension
  normally implies `file`; otherwise the resource is a directory. Use explicit
  `layout` for extensionless files or dotted directory names. Skill directories
  default to `SKILL.md`, memory directories to `MEMORY.md`; other directories
  require `entry`. Files default to their filename.
- Empty explicit paths/types/layouts/entries, unknown placeholders, traversal,
  duplicate names, and colliding local/agent targets are rejected. Portable
  target collision checks also reject case-only collisions.

### Local paths are derived

Schema 2.0 has no local-path override:

| Resource | Derived canonical local root |
| --- | --- |
| Dependency directory | `.ctxpm/dependencies/<type-directory>/<name>` |
| Dependency file | `.ctxpm/dependencies/<type-directory>/<upstream-filename>` |
| Package directory | `.ctxpm/packages/<type-directory>/<name>` |
| Package file | `.ctxpm/packages/<type-directory>/<entry-filename>` |

For example, a rule named `legacy-policy` with `path: legacy/policy.txt` installs
as `.ctxpm/dependencies/rules/policy.txt`. Its name remains `legacy-policy` for
CLI selection. Two differently named rules cannot both install as `policy.txt`.
File packages specify `layout: file` and a filename-only `entry`.

The runtime resource model and CLI JSON outputs still report computed local
paths. In schema 1.0, resource `path` continues to mean the local path. Never
copy that field into a schema 2.0 dependency resource's upstream `path`.

### URL and archive compatibility

Git and archive groups support the same templates. Archives preserve existing
extraction-root semantics. URL sources retain their exact file URL or multi-file
base URL and `source.files`/`entry`; use one resource per URL group. URL groups
must not declare a group/resource `path`: no implicit URL joining is introduced.
File URLs require a concrete filename. URL/archive version hashes and the special
ctxpm Release version behavior are unchanged.

### Explicit migration

```sh
ctxpm manifest capabilities --json
ctxpm manifest migrate --to 2.0 --dry-run
ctxpm manifest migrate --to 2.0
```

Migration previews the converted document, then writes it as one recoverable
transaction when applied. It does not fetch dependencies, discover newer
versions, move resource files, or change sources. Versions and resolved resource
semantics must remain equivalent. Unrelated top-level metadata and resource
extensions are preserved; unknown source extensions require explicit handling.

The project-local ctxpm executable must advertise schema 2.0 support and match
its locked Release version before conversion. Upgrade ctxpm while still using
schema 1.0 if this check fails. No minimum release number is guessed. Projects
without a registered ctxpm dependency do not require this release check.

Nonstandard legacy local paths, dependencies with unknown sources, and target
conflicts prevent conversion. Keep those projects on 1.0 until organized; ordinary
commands continue to support that schema. New project initialization currently
uses 1.0; adoption of 2.0 is explicit. Old published CLIs cannot be made safe by a
new version field: use the supporting project CLI after migration. This CLI also
checks downloaded replacement CLIs before invoking them against schema 2.0.
