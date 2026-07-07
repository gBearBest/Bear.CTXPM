# ctxpm

`ctxpm` is the bundled Bear.CTXPM management skill. It gives future AI agents one explicit workflow for creating, installing, validating, updating, and removing AI resources in a project.

## Canonical Resource Root

Treat the bundled `ctxpm` skill as a **directory resource root**, not a single Markdown file.

Canonical install root:

```text
.ctxpm/dependencies/skills/ctxpm/
  SKILL.md
  ctxpm-yaml.md
  cli/
    ctxpm
```

Rules:

- The resource root is `.ctxpm/dependencies/skills/ctxpm/`.
- The entry file is `SKILL.md`.
- `ctxpm-yaml.md` must live beside the entry file so the skill remains self-contained.
- When the companion CLI is prepared for project-local use, place it at `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`.

## Compatibility Exposure

After installing the canonical root, immediately expose it through every confirmed agent's default skill discovery directory.

Example:

```text
.agents/skills/ctxpm -> ../../.ctxpm/dependencies/skills/ctxpm
```

Apply the same compatibility rule to other managed resources: keep the canonical content under `.ctxpm/...`, then expose it through agent-recognizable discovery paths.

## `ctxpm.yaml` Registration

Record `ctxpm` as an external `dependency` of type `skill`.

Recommended v2 entry:

```yaml
dependencies:
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
      ref: main
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/ctxpm
```

Rules:

- Do not model the bundled `ctxpm` skill as a single-file URL dependency.
- Register the full skill directory as the resource root.
- Do not omit `compatibility` for `ctxpm`.

## Version Rules

External dependency versions must describe the resolved resource root:

- Git resources use the installed commit SHA.
- Single-file non-Git resources use `sha256:<hex>`.
- Directory non-Git resources use `sha256tree:<hex>`.

For multi-file URL or archive resources, compute the version from the full directory tree, not only from the entry file.

## Preferred CLI Workflow

When the companion CLI is available, prefer it for routine lifecycle operations:

- `ctxpm install`
- `ctxpm add`
- `ctxpm list`
- `ctxpm validate`
- `ctxpm check-updates`
- `ctxpm update`
- `ctxpm remove`

If the CLI is unavailable, follow the same protocol manually instead of inventing a partial workflow.

## Compact `ctxpm.yaml` Reference

The complete schema is defined in [`ctxpm-yaml.md`](ctxpm-yaml.md). `SKILL.md` should tell agents to read that sibling file before editing `ctxpm.yaml`.

Compact v2 example:

```yaml
version: 2

project:
  name: your-project-name

agents:
  - codex

dependencies:
  - name: external-resource
    type: skill
    layout: dir
    path: .ctxpm/dependencies/skills/external-resource
    entry: SKILL.md
    source:
      type: git
      url: https://github.com/example/example-ai-resources
      path: skills/external-resource
      entry: SKILL.md
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/external-resource

packages:
  - name: project-resource
    type: rule
    layout: file
    path: .ctxpm/packages/rules/project-resource.md
    entry: project-resource.md

entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
```

## Lifecycle Rules

### Create / Install

1. Classify the resource as `dependency` or `package`.
2. Install the canonical resource root under `.ctxpm/dependencies/` or `.ctxpm/packages/`.
3. Record `layout`, `path`, and `entry`.
4. For dependencies, record `source` and `version`.
5. Repair compatibility exposure paths.

### Validate

1. Check that the resource root exists.
2. Check that the shape matches `layout`.
3. Check that the declared `entry` exists inside the root.
4. Check that compatibility links exist.

### Update

1. Resolve the upstream resource root.
2. Recompute the root version.
3. Replace the canonical root.
4. Repair compatibility links.
5. Only update `ctxpm.yaml` when the dependency version actually changed.
