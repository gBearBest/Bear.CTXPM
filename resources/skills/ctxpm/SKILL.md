# ctxpm

`ctxpm` is the bundled Bear.CTXPM management skill. It gives future AI agents one explicit workflow for creating, installing, validating, updating, and removing AI resources in a project.

Managed resource types include `skill`, `rule`, `spec`, `prompt`, `memory`, and `mcp`.

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

After installing the canonical root, the tool exposes it through every confirmed agent's default skill discovery directory via compatibility symlinks. No explicit `compatibility` field is needed in `ctxpm.yaml` — paths are derived automatically from the declared `agents`.

Example derived link for a project with `agents: [generic]`:

```text
.agents/skills/ctxpm -> ../../.ctxpm/dependencies/skills/ctxpm
```

The same derivation applies to all managed resources: canonical content stays under `.ctxpm/...` and is exposed through agent-recognizable discovery paths without writing the paths into `ctxpm.yaml`.

## `ctxpm.yaml` Registration

Record `ctxpm` as an external `dependency` of type `skill`.

Recommended 1.0 entry:

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
```

Rules:

- Do not model the bundled `ctxpm` skill as a single-file URL dependency.
- Register the full skill directory as the resource root.

## Version Rules

External dependency versions must describe the resolved resource root:

- Git resources use the full commit SHA of the latest commit that changed `source.path` at or before the installed checkout.
- Single-file non-Git resources use `sha256:<hex>`.
- Directory non-Git resources use `sha256tree:<hex>`.

For multi-file URL or archive resources, compute the version from the full directory tree, not only from the entry file.

## Preferred CLI Workflow

When the companion CLI is available, prefer it for routine lifecycle operations:

- `ctxpm install`
- `ctxpm entrypoint sync`
- `ctxpm entrypoint doctor`
- `ctxpm detect`
- `ctxpm migrate`
- `ctxpm add`
- `ctxpm list`
- `ctxpm validate`
- `ctxpm check-updates`
- `ctxpm update`
- `ctxpm remove`
- `ctxpm memory search`
- `ctxpm memory suggest`
- `ctxpm memory capture`
- `ctxpm memory prune`

Run `ctxpm detect` on a shorter cadence than `ctxpm check-updates` so newly added AI resources in non-ctxpm locations are caught early, then migrate them and validate the result after user confirmation.

If the CLI is unavailable, follow the same protocol manually instead of inventing a partial workflow.

## Compact `ctxpm.yaml` Reference

The complete schema is defined in [`ctxpm-yaml.md`](ctxpm-yaml.md). `SKILL.md` should tell agents to read that sibling file before editing `ctxpm.yaml`.

Compact 1.0 example:

```yaml
version: 1.0

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

packages:
  - name: project-resource
    type: memory
    layout: dir
    path: .ctxpm/packages/memories/project-resource
    entry: MEMORY.md
```

Use `AGENTS.md` as the canonical shared root entrypoint file. When a declared agent expects a different root filename such as `CLAUDE.md` or `ANTIGRAVITY.md`, treat that filename as a compatibility symlink back to `AGENTS.md` and keep it out of Git with `.gitignore`.

## Lifecycle Rules

### Create / Install

1. Classify the resource as `dependency` or `package`.
2. Install the canonical resource root under `.ctxpm/dependencies/` or `.ctxpm/packages/`.
3. Record `layout`, `path`, and `entry`. Omit `compatibility` unless the derived paths are wrong.
4. For dependencies, record `source` and `version`.
5. Repair compatibility exposure paths.
6. Ensure `.gitignore` includes safe ignore rules for repaired compatibility paths.
7. Keep the shared root entrypoint topology healthy with `ctxpm entrypoint sync` and inspect drift with `ctxpm entrypoint doctor`.

### Validate

1. Check that the resource root exists.
2. Check that the shape matches `layout`.
3. Check that the declared `entry` exists inside the root.
4. Check that compatibility links exist.
5. For `memory` directory resources, validate any `index.json` or `index.jsonl` references that point to other files inside the resource root.
6. Check that `AGENTS.md` exists as the shared managed root entrypoint, and that other declared root entrypoint filenames are symlinks back to it.

### Update

1. Resolve the upstream resource root.
2. Recompute the root version.
3. Replace the canonical root.
4. Repair compatibility links.
5. Only update `ctxpm.yaml` when the dependency version actually changed.
