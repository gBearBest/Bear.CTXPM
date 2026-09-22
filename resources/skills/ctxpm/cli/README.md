# ctxpm CLI Reference

This file documents every `ctxpm` command and flag for AI agents that cannot run the binary directly.

The CLI binary lives alongside this file at `cli/ctxpm` relative to the skill root.

## Global notes

- Commands that change managed resources accept `--dry-run` to report planned changes without writing files.
- All commands accept `--json` to emit structured output suitable for parsing.
- Commands that fetch remote content require network access: `add`, ordinary dependency installation, `check-updates`, and `update`.
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
| `--agent` | Primary agent profile (e.g. `codex`, `claude-code`, `gemini-cli`, `cursor`, `kiro`, `opencode`, `grok`). Defaults to auto-detected agent or `generic`. |
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

Install all dependencies declared in `ctxpm.yaml`, then repair compatibility symlinks. Installation uses each dependency's recorded `version` as a lock: it does not check for or select newer upstream content. For the registered `ctxpm` dependency, install the exact Release tag recorded in the manifest and restore its CLI, complete embedded skill snapshot, and entrypoint template.

```
ctxpm install [--type <type>] [--only <name>] [--force] [--dry-run] [--json]
```

| Flag | Description |
|---|---|
| `--type` | Only install resources of this type. |
| `--only` | Only install or repair a single resource by name. |
| `--force` | Reinstall dependencies even when their recorded version and local content are current. |
| `--dry-run` | Report the work without writing files. |
| `--json` | Emit JSON output. |

`install` maintains ignored local state in `.ctxpm/state/installed.json`. An unchanged dependency is not fetched again; missing, changed, or newly locked dependencies are restored individually. Git sources reuse ignored object caches under `.ctxpm/cache/git/`, while URL, archive, and Release dependencies only redownload when that dependency's locked version changes. Requires network access only for resources that need restoring and when the locked ctxpm Release differs from the active CLI. Installing a project-locked ctxpm Release updates the project-local CLI without replacing a separate global CLI.

---

## doctor

`ctxpm init` and `ctxpm install` enable Git hooks by default when run in a Git working tree. The hooks run `ctxpm install` after a branch checkout, merge-based pull, rebase, or rebase-based pull. They restore the versions locked in the checked-out `ctxpm.yaml`; they never run `ctxpm update`. `ctxpm doctor` reports the managed entrypoint and Git hook state. `ctxpm install` repairs or regenerates the hooks when needed.

```
ctxpm doctor [--json]
```

Git deliberately does not run repository hooks during clone, so a fresh clone becomes configured when its first `ctxpm init` or `ctxpm install` runs. ctxpm refuses to replace a different configured hook path or an existing non-ctxpm hook file. The hooks remain non-blocking: an unavailable CLI or failed dependency restore prints a diagnostic but does not fail the Git operation.

The command also checks the managed root entrypoint topology: `.ctxpm/AGENTS.md` is the single source of truth and root-level entrypoint filenames (`AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, etc.) are symlinks. Safe, read-only. Run to diagnose drift before `ctxpm install`.

---

## detect

Scan the project for AI resource files (skills, rules, prompts, memories, MCP configs) that exist outside of `.ctxpm/` and are not registered in `ctxpm.yaml`.

```
ctxpm detect [--json]
```

Safe, read-only. Run on a shorter cadence than `check-updates`. After reviewing the output, use `migrate` (with user confirmation) to bring detected resources under ctxpm management. In a conversational workflow, do not narrate a no-candidate result; continue the user's current request.

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

Check whether dependencies have upstream updates. The single `ctxpm` result represents its complete release unit and includes component status for the CLI, bundled skill, and managed entrypoint.

```
ctxpm check-updates [--force] [--json]
```

| Flag | Description |
|---|---|
| `--force` | Ignore the configured check interval and query upstream now. |
| `--json` | Emit JSON output. |

Requires network access. Safe, read-only — does not modify any files. Run before `update`. In a conversational workflow, this is the discovery step before asking the user to confirm an available update. Do not mention a no-update result unless the user explicitly asks about `ctxpm` status.

---

## update

Apply upstream updates for dependencies. Updating the special `ctxpm` dependency upgrades the CLI, bundled skill, and managed entrypoint template together.

```
ctxpm update [<name>...] [--all] [--dry-run] [--json]
```

| Flag | Description |
|---|---|
| positional `<name>` | One or more dependency names to update. |
| `--all` | Update every dependency with an available update. |
| `--dry-run` | Resolve and report without changing files. |
| `--json` | Emit JSON output. |

Requires network access. Run `check-updates` first. In a conversational workflow, call this only after the user confirms the proposed update. Use `ctxpm update ctxpm` to upgrade the complete ctxpm release unit explicitly; `ctxpm update --all` includes it when an update is available.

---

## remove

Remove a dependency or package from `ctxpm.yaml` and optionally delete its files.

```
ctxpm remove <name> [--delete-files | --keep-files] [--json]
```

| Flag | Description |
|---|---|
| `--delete-files` | Delete canonical `.ctxpm` resource files and any compatibility symlinks from disk. |
| `--keep-files` | Leave canonical `.ctxpm` resource files on disk (default behavior when neither flag is set). |
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
