# ctxpm

`ctxpm` is a bundled external AI resource dependency. It gives future AI agents one explicit workflow for creating, reading, updating, deleting, installing, removing, or reorganizing AI resources in a Bear.CTXPM project.

This skill manages the project's AI resource lifecycle. It can handle both project-local `package` resources and external `dependency` resources, but it must always classify ownership with explicit evidence or user confirmation before changing resource locations.

## Canonical Location

Install the canonical resource at:

```text
.ctxpm/dependencies/skills/ctxpm/
  SKILL.md
  ctxpm-yaml.md
```

This helper skill itself is an external `dependency`, not a project-local `package`. The `ctxpm-yaml.md` companion document must be copied into the skill directory so the skill remains self-contained when an AI agent only discovers the skill directory.

## Compatibility Symlinks

After creating the canonical dependency, immediately expose it through every confirmed agent's default skill discovery directory. This must be part of the `ctxpm` installation step itself, not only part of a previous batch migration pass.

Examples:

```text
.agents/skills/ctxpm -> ../../.ctxpm/dependencies/skills/ctxpm
.agent/skills/ctxpm -> ../../.ctxpm/dependencies/skills/ctxpm
```

Use the actual default skill directories for every confirmed agent. If multiple compatible skill discovery directories exist for an agent, create compatibility symlinks for each one that the project keeps supported.

Apply the same default compatibility rule to other managed AI resources: create compatibility symlinks in every confirmed agent's recognizable discovery directories for the corresponding resource type, while keeping the canonical content under `.ctxpm/...`.

## `ctxpm.yaml` Registration

Record the resource as an external `dependency` of type `skill`.

The entry must include:

- canonical path under `.ctxpm/dependencies/`
- all compatibility symlink paths
- resource type `skill`
- dependency semantics
- the companion `ctxpm-yaml.md` file inside the canonical skill directory

Do not omit `compatibility` for `ctxpm`.

Example:

```yaml
dependencies:
  - name: ctxpm
    type: skill
    path: .ctxpm/dependencies/skills/ctxpm
    compatibility:
      - .agents/skills/ctxpm
```

## External Dependency Versions

When this skill installs or updates an external AI resource, it must record a hash-based `version` in `ctxpm.yaml` whenever the source is known.

- For resources installed from a GitHub repository, resolve the exact commit SHA that provided the installed resource and use that full commit SHA as `version`.
- Do not also duplicate that same GitHub commit SHA under `source.commit`; `version` is the authoritative installed revision.
- For resources installed from a direct URL, compute the SHA-256 hash of the resource entry file content and use `version: sha256:<hex>`.
- The entry file is the file the AI agent should read first for that resource, such as `SKILL.md`, a rule file, a prompt file, a spec entry file, or an MCP configuration file.
- If a URL-installed resource has no single clear entry file, ask the user to choose the entry file before computing the hash.
- Do not use branch names, tags, `latest`, filenames, timestamps, or vague release labels as dependency versions.
- If the source or entry-file hash cannot be confirmed, report the dependency as unresolved for version tracking instead of inventing a value.

GitHub source example:

```yaml
dependencies:
  - name: example-skill
    type: skill
    path: .ctxpm/dependencies/skills/example-skill
    source:
      type: github
      url: https://github.com/example/example-ai-resources
      path: skills/example-skill
    version: 0123456789abcdef0123456789abcdef01234567
```

Direct URL source example:

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

## `ctxpm.yaml` Format Reference

The complete `ctxpm.yaml` format is defined in the Bear.CTXPM source specification [`ctxpm-yaml.md`](ctxpm-yaml.md). The generated `SKILL.md` must include the compact format reference below so AI agents can update `ctxpm.yaml` correctly even when they only read the skill file.

When installing this skill, copy the complete format document into the canonical skill directory:

```text
Bear.CTXPM docs/ctxpm-yaml.md source -> .ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md
```

The generated `SKILL.md` must tell AI agents to read the sibling `ctxpm-yaml.md` before modifying `ctxpm.yaml`.

Compact schema:

```yaml
version: 1

project:
  name: your-project-name

agents:
  - codex

dependencies:
  - name: external-resource
    type: skill
    path: .ctxpm/dependencies/skills/external-resource
    source:
      type: github
      url: https://github.com/example/example-ai-resources
      path: skills/external-resource
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/external-resource

packages:
  - name: project-resource
    type: rule
    path: .ctxpm/packages/rules/project-resource

entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
```

Format rules:

- `version` must be `1`.
- Resource `type` must be one of `skill`, `rule`, `spec`, `prompt`, or `mcp`.
- `dependencies` are external resources under `.ctxpm/dependencies/`.
- `packages` are project-local resources under `.ctxpm/packages/`.
- GitHub dependencies use the installed commit SHA as `version`; do not duplicate it as `source.commit`.
- Direct URL dependencies use `version: sha256:<hex>` from the entry file content.
- `compatibility` lists agent-recognizable discovery paths that point to canonical `.ctxpm` paths, normally one or more per confirmed agent for the resource type.
- Preserve unknown fields and unrelated entries when editing `ctxpm.yaml`.

## Ownership Evidence Checklist

Do not treat repository presence or Git tracking as sufficient evidence that an AI resource is a project-local `package`. A repository may contain vendored or previously copied external AI resources.

Before classifying a resource, inspect whether it is coupled to the current project:

- Strong `package` indicators:
  - references to current project paths, files, directories, scripts, commands, package names, modules, APIs, schemas, environment variables, deployment targets, or documentation
  - references to current project business domain, product names, brand names, repository-specific workflows, internal conventions, or project-maintained templates
  - explicit project documentation saying the resource is owned and maintained by the project
- Strong `dependency` indicators:
  - no references to current project paths or files after inspection
  - no project-specific commands, modules, domain terms, or maintenance context
  - third-party `author`, `version`, `license`, upstream URL, registry, vendor namespace, or generic examples
  - generic content that is reusable across projects
  - cross-references only among a group of generic resources, which may indicate a vendored external resource set

If a resource has no project-specific references and has external-looking metadata, classify it as a probable external `dependency` and report the evidence. If project-specific references conflict with external metadata, ask the user to confirm ownership before migration.

## CRUD Workflow

Use this single skill for the full AI resource lifecycle:

### Create / Install

Use this workflow when adding a new AI resource.

1. Determine whether the resource is a project-local `package` or an external `dependency`.
2. Use explicit evidence or user confirmation. Do not guess ownership, and do not treat Git tracking as ownership evidence by itself.
3. Inspect project-specific path, file, command, module, documentation, product, and domain references before classifying.
4. If there are no project-specific references and there are external indicators, classify the resource as a probable external `dependency` and report the evidence.
5. If ownership is unclear or evidence conflicts, ask the user through interactive Q&A before installing.
6. Store project-local resources under `.ctxpm/packages/<plural-type>/`.
7. Store external resources under `.ctxpm/dependencies/<plural-type>/`.
8. For external GitHub or URL sources, record `source` and hash-based `version`.
9. By default, create compatibility symlinks for the resource in every confirmed agent's recognizable discovery directories for that resource type.
10. Update `.gitignore` and `ctxpm.yaml`.

### Read / Query / Check

Use this workflow when listing, auditing, or explaining AI resources.

1. Read `ctxpm.yaml` first.
2. List `packages`, `dependencies`, `entrypoints`, and compatibility paths separately.
3. Report each resource's `name`, `type`, semantic role (`package` or `dependency`), canonical path, source, version, and compatibility paths when present.
4. Check whether canonical paths exist.
5. Check whether compatibility symlinks exist and point to the canonical `.ctxpm` locations.
6. Check whether external dependencies have enough `source` and `version` metadata for update detection.
7. Report unresolved or inconsistent resources; do not silently fix them unless the user asked for modification.

### Update

Use this workflow when updating existing AI resources.

1. Read `ctxpm.yaml`.
2. For project-local `package` resources, update only when the user asks to modify project-maintained content.
3. For external `dependency` resources, treat `version` as the installed baseline.
4. For GitHub dependencies, compare the installed commit SHA in `version` with the latest commit SHA from the configured source.
5. For URL dependencies, compare the installed `sha256:<hex>` version with the SHA-256 hash of the current entry file content.
6. If unchanged, leave the local resource as-is.
7. If changed, update the canonical `.ctxpm/dependencies/...` copy and set `version` to the new hash.
8. Preserve compatibility symlinks.
9. Do not update dependencies whose source, entry file, or update target cannot be determined.
10. Update `ctxpm.yaml` whenever a resource is updated.

### Delete / Remove

Use this workflow when removing an AI resource.

1. Read `ctxpm.yaml` and locate the resource entry.
2. Confirm whether the user wants to remove only the compatibility path, only the canonical resource, or the entire managed resource.
3. For project-local `package` resources, do not delete canonical content without explicit user confirmation.
4. For external `dependency` resources, remove the canonical `.ctxpm/dependencies/...` copy only when the user confirms the dependency should be removed.
5. Remove compatibility symlinks that point to the removed resource.
6. Remove or update the corresponding `ctxpm.yaml` entry.
7. Update `.gitignore` if obsolete compatibility paths should no longer be ignored.
8. Report any paths that could not be removed.

## `SKILL.md` Content

Use this content for `.ctxpm/dependencies/skills/ctxpm/SKILL.md`:

````md
# ctxpm

Use this skill whenever the user asks to create, add, install, list, inspect, check, update, delete, remove, or reorganize AI resources in this project.

## Rules

1. Do not install AI resources into agent default locations directly.
2. First determine whether each resource is a project-local `package` or an external `dependency`.
3. Classification must be based on explicit evidence or user confirmation. Do not guess, and do not treat repository presence or Git tracking as ownership evidence by itself.
4. Inspect whether the resource references current project paths, files, commands, modules, docs, product names, or domain concepts.
5. If a resource has no project-specific references and has external indicators such as third-party author/version/license/source metadata, classify it as a probable external `dependency` and report that evidence.
6. If ownership is unclear or evidence conflicts, ask the user through interactive Q&A before migrating, installing, updating, or deleting.
7. Store canonical project-local resources under `.ctxpm/packages/<plural-type>/`.
8. Store canonical external resources under `.ctxpm/dependencies/<plural-type>/`.
9. By default, create compatibility symlinks in every confirmed agent's recognizable discovery directories for the resource type. If one of those discovery paths already exists in the workspace, replace that path with a compatibility symlink pointing to the canonical `.ctxpm` location instead of keeping duplicate content.
10. Do not create reverse symlinks from `.ctxpm` back to old/default locations.
11. Add compatibility ignore rules and `.ctxpm/dependencies/` to `.gitignore`; prefer a safe directory-level rule such as `.agents/skills/` or `.agents/` over many per-resource rules when that directory contains only compatibility facades.
12. If an old/default path was tracked by Git, remove it from the Git index with `git rm --cached <path>` after confirming the canonical `.ctxpm` copy exists.
13. Update `ctxpm.yaml` after every resource change.
14. For external resources installed from GitHub, record the exact source commit SHA as the dependency `version` in `ctxpm.yaml`.
15. For external resources installed from a direct URL, compute the SHA-256 hash of the resource entry file and record `version: sha256:<hex>` in `ctxpm.yaml`.
16. When checking resources, report canonical paths, compatibility paths, source metadata, version metadata, and unresolved inconsistencies.
17. When updating external dependencies, compare the recorded `version` with the current upstream hash before changing files.
18. When deleting resources, confirm the intended removal scope before deleting canonical content.
19. When maintaining a managed root entrypoint block, keep its body aligned with the canonical Bear.CTXPM `ctxpm` block template. Only the opening `agent=<entrypoint-key>` marker should vary between agent entrypoints.

## ctxpm.yaml Format

Before modifying `ctxpm.yaml`, read the sibling `ctxpm-yaml.md` in this skill directory. If that companion file is missing, use this compact schema and report that the companion YAML format document should be restored:

```yaml
version: 1

project:
  name: your-project-name

agents:
  - codex

dependencies:
  - name: external-resource
    type: skill
    path: .ctxpm/dependencies/skills/external-resource
    source:
      type: github
      url: https://github.com/example/example-ai-resources
      path: skills/external-resource
    version: 0123456789abcdef0123456789abcdef01234567
    compatibility:
      - .agents/skills/external-resource

packages:
  - name: project-resource
    type: rule
    path: .ctxpm/packages/rules/project-resource

entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
```

Rules:

- `version` must be `1`.
- Resource `type` must be one of `skill`, `rule`, `spec`, `prompt`, or `mcp`.
- `dependencies` are external resources under `.ctxpm/dependencies/`.
- `packages` are project-local resources under `.ctxpm/packages/`.
- GitHub dependencies use the installed commit SHA as `version`; do not duplicate it as `source.commit`.
- URL dependencies use `version: sha256:<hex>` from the entry file content.
- `compatibility` lists old/default discovery paths that point to canonical `.ctxpm` paths.
- `.gitignore` may consolidate many compatibility paths into one safe directory-level ignore rule, but `ctxpm.yaml` must still record each exact compatibility path.
- Preserve unknown fields and unrelated entries unless they directly conflict with the requested change.

## Resource Type Directories

- `skill` -> `skills/`
- `rule` -> `rules/`
- `spec` -> `specs/`
- `prompt` -> `prompts/`
- `mcp` -> `mcp/`

## Report

After changes, report:

- resources added, inspected, updated, removed, or left unresolved
- whether each resource is `package` or `dependency`
- evidence or user confirmation used for each ownership decision
- canonical `.ctxpm` locations
- compatibility symlinks created, preserved, or removed
- source and hash-based version recorded for each external dependency
- external dependencies whose version could not be resolved
- `.gitignore` and `ctxpm.yaml` updates
````

## Verification Checklist

After installation, verify:

1. `.ctxpm/dependencies/skills/ctxpm/SKILL.md` exists.
2. `.ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md` exists as a sibling companion document.
3. Every confirmed agent's default skill discovery directory contains a compatibility symlink for `ctxpm`, such as `.agents/skills/ctxpm`.
4. Each compatibility symlink points to `.ctxpm/dependencies/skills/ctxpm`.
5. `ctxpm.yaml` records the dependency with both canonical `path` and every `compatibility` path.
6. The generated `SKILL.md` tells AI agents to read the sibling `ctxpm-yaml.md` before modifying `ctxpm.yaml`.
7. The managed root entrypoint block uses the canonical `ctxpm` template, matches the current entrypoint key in its `agent=...` marker, and instructs future AI agents to use `ctxpm` or the same `ctxpm` workflow before creating, reading, updating, or deleting AI resources.
8. External dependencies installed from GitHub or direct URLs include hash-based `version` values in `ctxpm.yaml`.
