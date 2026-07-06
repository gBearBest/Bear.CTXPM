# ctxpm.yaml Format

This document defines the `ctxpm.yaml` format used by Bear.CTXPM projects.

AI agents must read this format before creating, editing, auditing, or deleting `ctxpm.yaml` entries.

## Top-Level Structure

```yaml
version: 1

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
| `version` | Yes | Format version. Must be `1` for Bear.CTXPM v0.1. |
| `project.name` | Yes | Project name. Use the repository or root directory name when no explicit project name exists. |
| `agents` | Yes | List of confirmed agent profiles used by the project. |
| `update_policy` | No | Policy for periodic dependency update detection. |
| `dependencies` | Yes | External AI resources managed under `.ctxpm/dependencies/`. Use an empty list when none exist. |
| `packages` | Yes | Project-local AI resources managed under `.ctxpm/packages/`. Use an empty list when none exist. |
| `entrypoints` | Yes | Root Markdown entrypoint files managed for each agent. |

## `update_policy`

`update_policy` configures periodic dependency update detection.

Example:

```yaml
update_policy:
  enabled: true
  interval: 1d
  include_self: true
```

Fields:

| Field | Description |
| --- | --- |
| `enabled` | Whether periodic dependency update checks are enabled. Defaults to `true` when omitted. |
| `interval` | Check cadence using forms like `12h`, `1d`, or `7d`. Defaults to `1d` when omitted. |
| `include_self` | Whether the bundled `ctxpm` dependency participates in periodic checks and update prompts. Defaults to `true` when omitted. |

Rules:

- `update_policy` applies to `dependencies` only, not to project-local `packages`.
- `update_policy` controls detection cadence, not automatic update permission.
- AI must still ask the user before applying any dependency update.
- If the user does not answer an update prompt, leave dependencies unchanged.
- Companion update scripts may store runtime check state under `.ctxpm/state/update-checks.json`, but that runtime state does not belong in `ctxpm.yaml`.

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

## `dependencies` Entries

Each `dependencies` entry describes an external AI resource.

Required fields:

| Field | Description |
| --- | --- |
| `name` | Stable resource name. |
| `type` | Resource type. Must use one of the allowed resource types. |
| `path` | Canonical local path under `.ctxpm/dependencies/`. |

Recommended fields:

| Field | Description |
| --- | --- |
| `source` | Upstream source metadata for update detection. |
| `version` | Hash-based installed version. Required when the resource comes from Git or a direct URL. |
| `compatibility` | List of compatibility symlink paths outside `.ctxpm`, normally covering each confirmed agent's recognizable discovery directories for the resource type. |

Git dependency example:

```yaml
dependencies:
  - name: example-skill
    type: skill
    path: .ctxpm/dependencies/skills/example-skill
    source:
      type: git
      url: https://github.com/example/example-ai-resources
      path: skills/example-skill
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/example-skill
```

Rules:

- `version` is the installed commit SHA.
- Do not duplicate the same commit SHA under `source.commit`.
- `source.path` is the path to the resource inside the upstream repository.
- If a specific branch, tag, or ref must be used for update checks, record it as `source.ref`.

Optional Git ref example:

```yaml
source:
  type: git
  url: https://github.com/example/example-ai-resources
  ref: main
  path: skills/example-skill
```

Legacy manifests that still use `source.type: github` remain valid and should be interpreted the same way as `source.type: git`.

Direct URL dependency example:

```yaml
dependencies:
  - name: example-rule
    type: rule
    path: .ctxpm/dependencies/rules/example-rule.md
    source:
      type: url
      url: https://example.com/ai/rules/example-rule.md
      entry: example-rule.md
    version: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Rules:

- `source.url` is the URL used to fetch the resource or entry file.
- `source.entry` is the AI resource entry file used for hashing and reading.
- `version` must be the SHA-256 hash of the entry file content in the format `sha256:<hex>`.
- If there is no single clear entry file, ask the user to choose one before recording `version`.

## `packages` Entries

Each `packages` entry describes a project-local AI resource.

Required fields:

| Field | Description |
| --- | --- |
| `name` | Stable resource name. |
| `type` | Resource type. Must use one of the allowed resource types. |
| `path` | Canonical local path under `.ctxpm/packages/`. |

Optional fields:

| Field | Description |
| --- | --- |
| `compatibility` | List of compatibility symlink paths outside `.ctxpm`, normally covering each confirmed agent's recognizable discovery directories for the resource type. |

Example:

```yaml
packages:
  - name: project-rules
    type: rule
    path: .ctxpm/packages/rules/project-rules
    compatibility:
      - rules/project-rules
```

Rules:

- Project-local packages normally do not use `source` or hash-based `version`.
- Do not place external resources under `packages` unless the user explicitly confirms they are now project-maintained assets.

## `entrypoints`

`entrypoints` records root Markdown files that contain managed `ctxpm` blocks.

Example:

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
- The referenced file's managed block should begin with `<!-- ctxpm:begin agent=<entrypoint-key> -->`.
- Managed entrypoint blocks should use the canonical `ctxpm` template defined by the protocol; only the `agent` value in the opening marker changes per entrypoint file.

## Compatibility Paths

Use `compatibility` when an AI agent or tool expects resources in a default discovery location.

Example:

```yaml
compatibility:
  - .agents/skills/example-skill
```

Rules:

- Compatibility paths point from agent-recognizable discovery locations to canonical `.ctxpm` locations.
- By default, create compatibility paths for every confirmed agent that has a recognizable discovery directory for the resource type.
- Do not create reverse symlinks from `.ctxpm` back to old/default locations.
- Add compatibility paths to `.gitignore` when they are symlinks or compatibility facades.
- Prefer one safe directory-level `.gitignore` rule over many per-resource rules when a directory contains only compatibility facades.
  - Example: use `.agents/skills/` instead of many `.agents/skills/<name>` entries.
  - Use `.agents/` only when the whole `.agents/` directory is a compatibility surface and contains no project-owned files that should remain tracked.
- If a directory mixes compatibility facades with real project-owned files, use narrower child-directory rules or individual compatibility path rules.
- Even when `.gitignore` uses a consolidated directory rule, keep `compatibility` entries precise in `ctxpm.yaml`.
- If a confirmed agent has no recognizable discovery directory for a resource type, omit compatibility for that agent/type pair.

## Editing Rules

When modifying `ctxpm.yaml`:

1. Preserve unknown fields unless they directly conflict with the requested change.
2. Do not delete unrelated resources.
3. Do not reorder unrelated entries just for formatting.
4. Keep paths relative to the project root.
5. Keep `dependencies` and `packages` semantically separate.
6. Update `compatibility` whenever compatibility symlinks are added or removed.
7. Update `version` whenever an external dependency is updated.
8. If source or version metadata cannot be determined, report the resource as unresolved instead of inventing values.

## Minimal Complete Example

```yaml
version: 1

project:
  name: example-project

agents:
  - codex

update_policy:
  enabled: true
  interval: 1d
  include_self: true

dependencies:
  - name: ctxpm
    type: skill
    path: .ctxpm/dependencies/skills/ctxpm
    compatibility:
      - .agents/skills/ctxpm
  - name: example-skill
    type: skill
    path: .ctxpm/dependencies/skills/example-skill
    source:
      type: github
      url: https://github.com/example/example-ai-resources
      path: skills/example-skill
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/example-skill

packages:
  - name: project-rules
    type: rule
    path: .ctxpm/packages/rules/project-rules

entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
```
