# ctxpm CLI Reference

This file documents every `ctxpm` command and flag for AI agents that cannot run the binary directly.

The CLI binary lives alongside this file at `cli/ctxpm` relative to the skill root.

## Global notes

- All mutating commands accept `--dry-run` to report planned changes without writing files.
- All commands accept `--json` to emit structured output suitable for parsing.
- Commands that fetch remote content require network access: `add`, `install`, `check-updates`, `update`.
- Commands that only read local state are safe to run at any time: `list`, `validate`, `detect`, `memory search`.

---

## version

Print the CLI version string.

```
ctxpm version
ctxpm --version
```

---

## init

Initialize the current directory as a Bear.CTXPM project. Creates `ctxpm.yaml`, `.ctxpm/AGENTS.md`, root-level entrypoint symlinks, `.gitignore` rules, and installs the bundled `ctxpm` skill.

```
ctxpm init [--agent <profile>] [--project-name <name>] [--force] [--dry-run]
```

| Flag | Description |
|---|---|
| `--agent` | Primary agent profile (e.g. `codex`, `claude`, `gemini`, `cursor`, `kiro`). Defaults to auto-detected agent or `generic`. |
| `--project-name` | Override the project name written into `ctxpm.yaml`. |
| `--force` | Overwrite missing managed files when a manifest already exists. |
| `--dry-run` | Report changes without writing files. |

Run once per project. Safe to re-run with `--force` to repair a partial initialization.

---

## add

Resolve an external AI resource from a URL, install it under `.ctxpm/dependencies/`, and register it in `ctxpm.yaml`.

```
ctxpm add <source-url> --type <type> [options]
```

| Flag | Description |
|---|---|
| `--type` | **Required.** Resource type: `skill`, `rule`, `spec`, `prompt`, `memory`, `mcp`. |
| `--name` | Override the resource name derived from the URL. |
| `--layout` | Resource layout: `file` or `dir`. Inferred when omitted. |
| `--source-type` | Source type: `git`, `url`, `archive`. Inferred from URL when omitted. |
| `--source-path` | Path inside a git repository when it cannot be inferred from the URL. |
| `--target-path` | Override the canonical install path under `.ctxpm/dependencies/`. |
| `--ref` | Override git ref (branch, tag, or commit SHA). |
| `--entry` | Entry filename relative to the resource root. |
| `--file` | Relative file path for multi-file URL resources; repeat to add more files. |
| `--dry-run` | Resolve and report without writing files. |
| `--json` | Emit JSON output. |

Examples:

```
ctxpm add https://github.com/example/ai/tree/main/skills/reviewer --type skill
ctxpm add https://gitlab.company.com/team/ai.git --type rule --source-path rules/security
ctxpm add https://example.com/prompt.md --type prompt
```

---

## list

List all dependencies and packages registered in `ctxpm.yaml`.

```
ctxpm list [--type <type>] [--kind <kind>] [--json]
```

| Flag | Description |
|---|---|
| `--type` | Filter by resource type (`skill`, `rule`, etc.). |
| `--kind` | Filter by kind: `dependency` or `package`. |
| `--json` | Emit JSON output. |

Safe, read-only. Run any time to inspect managed resources.

---

## validate

Validate `ctxpm.yaml` structure and verify that all declared local paths, entry files, and compatibility symlinks exist on disk.

```
ctxpm validate [--json]
```

Safe, read-only. Run after manual edits to `ctxpm.yaml` or after filesystem changes.

---

## install

Download and install all dependencies declared in `ctxpm.yaml`, then repair compatibility symlinks. Idempotent — safe to re-run.

```
ctxpm install [--type <type>] [--only <name>] [--dry-run] [--json]
```

| Flag | Description |
|---|---|
| `--type` | Only install resources of this type. |
| `--only` | Only install or repair a single resource by name. |
| `--dry-run` | Report the work without writing files. |
| `--json` | Emit JSON output. |

Requires network access for git and URL sources.

---

## entrypoint

Manage the shared root entrypoint topology: `.ctxpm/AGENTS.md` as the single source of truth, with root-level entrypoint filenames (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, etc.) as symlinks.

### entrypoint sync

Apply the canonical entrypoint topology for all declared agents.

```
ctxpm entrypoint sync [--json]
```

Run after adding or removing agents from `ctxpm.yaml`, or to repair broken symlinks.

### entrypoint doctor

Check whether the entrypoint topology is healthy without modifying anything.

```
ctxpm entrypoint doctor [--json]
```

Safe, read-only. Run to diagnose drift before deciding to sync.

---

## detect

Scan the project for AI resource files (skills, rules, prompts, memories, MCP configs) that exist outside of `.ctxpm/` and are not registered in `ctxpm.yaml`.

```
ctxpm detect [--json]
```

Safe, read-only. Run on a shorter cadence than `check-updates`. After reviewing the output, use `migrate` (with user confirmation) to bring detected resources under ctxpm management.

---

## migrate

Move detected unmanaged AI resources into their canonical ctxpm-managed roots under `.ctxpm/`.

```
ctxpm migrate [--path <path>] [--all] [--dry-run] [--json]
```

| Flag | Description |
|---|---|
| `--path` | Original path or resource name of a specific detected candidate. Repeat to migrate multiple. |
| `--all` | Migrate every detected candidate. |
| `--dry-run` | Report the work without writing files. |
| `--json` | Emit JSON output. |

Always run `detect` first and confirm the candidate list with the user before running `migrate`.

---

## check-updates

Check whether any installed dependencies have upstream updates available.

```
ctxpm check-updates [--force] [--json]
```

| Flag | Description |
|---|---|
| `--force` | Ignore the configured check interval and query upstream now. |
| `--json` | Emit JSON output. |

Requires network access. Safe, read-only — does not modify any files. Run before `update`.

---

## update

Apply upstream updates for one or more dependencies, rewrite their versions in `ctxpm.yaml`, and reinstall.

```
ctxpm update [<name>...] [--all] [--dry-run] [--json]
```

| Flag | Description |
|---|---|
| positional `<name>` | One or more dependency names to update. |
| `--all` | Update every dependency with an available update. |
| `--dry-run` | Resolve and report without changing files. |
| `--json` | Emit JSON output. |

Requires network access. Run `check-updates` first.

---

## remove

Remove a dependency or package from `ctxpm.yaml` and optionally delete its files.

```
ctxpm remove <name> [--delete-files | --keep-files] [--json]
```

| Flag | Description |
|---|---|
| `--delete-files` | Delete canonical files and compatibility symlinks from disk. |
| `--keep-files` | Leave canonical files on disk (default behavior when neither flag is set). |
| `--json` | Emit JSON output. |

`--delete-files` and `--keep-files` are mutually exclusive. When neither is specified, files are kept.

---

## memory

Manage project memory resources: search entries, suggest capture targets, write new entries, and prune stale content.

### memory search

Search across all memory resources in the project.

```
ctxpm memory search [--query <text>] [--resource <name>] [--title <text>] [--tag <tag>] [--path <path>] [--json]
```

| Flag | Description |
|---|---|
| `--query` | Free-text search query. |
| `--resource` | Filter results to a specific memory resource name. |
| `--title` | Filter by document title. |
| `--tag` | Filter by tag. |
| `--path` | Filter by relative file path inside the resource. |

Safe, read-only.

### memory suggest

Evaluate a task summary and suggest which memory resource and topic to capture to.

```
ctxpm memory suggest [--topic <topic>] [--summary <text>] [--resource <name>] [--json]
```

| Flag | Description |
|---|---|
| `--summary` | Task summary to evaluate. |
| `--topic` | Optional memory topic hint. |
| `--resource` | Preferred writable memory resource name. |

Safe, read-only.

### memory capture

Generate and optionally persist a new memory entry.

```
ctxpm memory capture [--summary <text>] [--topic <topic>] [--resource <name>] [--title <title>] [--write] [--json]
```

| Flag | Description |
|---|---|
| `--summary` | Memory summary content to capture. |
| `--topic` | Optional memory topic. |
| `--resource` | Target writable memory resource name. |
| `--title` | Optional entry title override. |
| `--write` | Persist the generated memory entry to disk. Without this flag the entry is returned but not written. |

### memory prune

Remove duplicate, empty, or stale entries from memory resources.

```
ctxpm memory prune [--resource <name>] [--archive] [--json]
```

| Flag | Description |
|---|---|
| `--resource` | Filter to a specific memory resource name. |
| `--archive` | Archive duplicate and empty files in package memories instead of deleting them. |
