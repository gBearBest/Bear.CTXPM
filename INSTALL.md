# Bear.CTXPM Installation Instructions v0.1

This document is an installation guide for AI agents to read and execute directly.  
Users can give this file to any AI agent with project file read/write capability and ask it to connect the current project to the `ctxpm` protocol according to the ideas of Bear.CTXPM v0.1.

You are not only installing a command-line tool.  
You are organizing the current project into an AI resource management project that conforms to the Bear.CTXPM protocol, and you should install or prepare the companion `ctxpm` CLI when it is available so future AI resource operations can use the canonical CLI workflow.

---

## 1. Your Role

You are now the Bear.CTXPM installation executor.  
Your task is to organize the current project into a project that can sustainably manage internal and external AI resources, and to ensure that any future AI agent can understand these resources through the project's documentation and directory structure.

Your execution goals:

1. Identify which agent the user is currently using.
2. Create or update the root Markdown entrypoint file for that agent.
3. Create and maintain `ctxpm.yaml`.
4. Identify and migrate existing AI resources in the project.
5. Move external AI resources into `.ctxpm/dependencies/`.
6. Move project-local AI resources into `.ctxpm/packages/`.
7. Update `.gitignore` to ignore external AI resource directories.
8. Record stable version hashes for external AI resources when their source is known.
9. Install or prepare the companion `ctxpm` CLI when it is available, place it under the bundled `ctxpm` skill directory at `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`, and verify that it can run.

---

## 2. Ask the User Which Agent They Use First

Before performing any migration, ask the user which agent they primarily use.

The goal of this question is not open-ended conversation, but to determine which root entrypoint file should be written.

Prioritize the following profiles:

- `codex`
- `claude-code`
- `antigravity`
- `generic`

If the user does not clearly specify one:

1. First check whether the project root already contains any of these files:
   - `AGENTS.md`
   - `CLAUDE.md`
   - `ANTIGRAVITY.md`
2. If one of these files exists, prefer the corresponding profile.
3. If none of the known entrypoint files exists, ask the user which agent they primarily use.
4. If the user is still unsure, default to `generic` and write the entrypoint to `AGENTS.md`.

The mapping between agents and default entrypoint files is:

| Agent | Entrypoint |
| --- | --- |
| `codex` | `AGENTS.md` |
| `claude-code` | `CLAUDE.md` |
| `antigravity` | `ANTIGRAVITY.md` |
| `generic` | `AGENTS.md` |

---

## 3. Core Principles You Must Follow

### 3.1 Bear.CTXPM Is a Protocol First, with a Companion CLI

Do not confuse the Bear.CTXPM protocol with the companion CLI implementation.  
The project adopts `ctxpm` through documentation, directory structure, and managed entrypoint blocks.

However, when the official companion `ctxpm` CLI is available, install or prepare it early in the setup flow and prefer it for managed AI resource operations such as:

- `ctxpm install`
- `ctxpm add`
- `ctxpm list`
- `ctxpm check-updates`
- `ctxpm update`
- `ctxpm remove`
- `ctxpm validate`

If the CLI cannot be installed in the current environment, continue the protocol setup manually and report that the CLI setup could not be completed.

### 3.2 Use Only Two Semantics

You may only use the following two resource semantics:

- `dependency`
- `package`

Do not introduce alternative semantics such as `managed`, `vendored`, `bundle-cache`, or similar terms.

### 3.3 Separate Internal and External Resources

- `package`: an AI resource maintained inside the project and committed to version control.
- `dependency`: an external AI resource that is not committed to version control.

### 3.4 Prefer Compatibility with Existing Formats

Do not force users to convert existing resources into a new package format.  
Try to identify and connect existing resources such as:

- skill directories
- rule files or directories
- spec directories
- prompt template directories
- MCP configuration

### 3.5 Entrypoint Documents Are the First Entry

Any future AI agent should first understand the project's AI resource layout through root entrypoint documents.  
Therefore, the managed `ctxpm` block in an entrypoint document must be stable, clear, and repeatably maintainable.

### 3.6 Newly Added Resources Must Continue to Be Managed

Bear.CTXPM is not a one-time migration. It is a resource management protocol the project continues to follow.

This means:

- Resources identified and migrated during the first installation need to be managed according to `ctxpm` rules.
- Any AI resource package added later must also continue to follow `ctxpm` rules.
- When adding resources, AI should not scatter them into arbitrary directories without registration.
- After adding resources, AI should update `ctxpm.yaml`, entrypoint documents, and the corresponding resource directories.

If a user or AI later wants to introduce a new skill, rule, spec, prompt, or MCP resource, first decide whether it is a `package` or a `dependency`, then place it under `.ctxpm/packages/` or `.ctxpm/dependencies/`.

### 3.7 External Dependency Versions Use Source Hashes

When installing or updating an external `dependency`, record a stable hash-based version in `ctxpm.yaml` whenever the source is known.

- If the resource is installed from a GitHub repository, resolve the exact commit SHA that provided the installed resource and record that full commit SHA as the dependency `version`.
- Do not also duplicate that same GitHub commit SHA under `source.commit`; `version` is the authoritative installed revision.
- If the resource is installed from a direct URL, compute the SHA-256 hash of the AI resource entry file content after download and record it as the dependency `version` using the format `sha256:<hex>`.
- The entry file is the file the AI agent should read first for that resource, such as `SKILL.md`, a rule file, a prompt file, a spec entry file, or an MCP configuration file.
- If a URL-installed resource has no single clear entry file, ask the user to choose the entry file before computing the hash.
- Do not use branch names, tags, `latest`, filenames, timestamps, or vague release labels as the version value for external AI resources.
- If the source or entry-file hash cannot be confirmed, mark the version as unresolved in the installation report instead of inventing a value.

This hash-based version is the baseline for future external resource update detection and update decisions.

---

## 4. Target Directory Structure

Organize the project into the following minimal structure:

```text
project/
  ctxpm.yaml
  .ctxpm/
    dependencies/
      skills/
      rules/
      specs/
      prompts/
      mcp/
    packages/
      skills/
      rules/
      specs/
      prompts/
      mcp/
  AGENTS.md / CLAUDE.md / ANTIGRAVITY.md / other root entrypoint files
```

Do not create the following as required artifacts for v0.1:

- `ctxpm.lock`
- `.ctxpm/cache/`
- `.ctxpm/state/` as a required installation artifact, although companion `ctxpm` CLI commands may create it later as optional runtime state

---

## 5. Installation Flow

Follow these steps in order. Do not skip steps.

### 5.1 Confirm the Agent

1. Check for existing root entrypoint files.
2. Ask the user which agent they currently use if necessary.
3. Select the corresponding entrypoint filename.

### 5.2 Install or Prepare the Companion `ctxpm` CLI

Before migrating resources, try to install or prepare the companion `ctxpm` CLI.

Recommended order:

1. First define the canonical project-local CLI target as `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`.
2. Check whether that project-local CLI path already exists and is runnable.
3. If it is not already present, create the parent directory `.ctxpm/dependencies/skills/ctxpm/cli/`.
4. If a global `ctxpm` command is already available in the environment, it may be used as the source binary, but the project installation should still place a copy or equivalent local executable at `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`.
5. If the local repository contains the CLI source under `cli/`, prefer building from source and placing the resulting binary at `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`.
6. If source build is not practical and the Bear.CTXPM repository contains `cli/install.sh`, use that installer in project mode so the CLI lands directly in the canonical project-local path.
   - During the current pre-release phase, prefer the rolling `main` channel unless the user explicitly asked for a stable tagged version.
   - Minimal project-local install: `sh cli/install.sh --scope project`
   - Optional explicit form: `sh cli/install.sh --scope project --project-root . --version main`
   - On Windows-like shell environments, project mode should still preserve the canonical launcher path `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`, while also placing sibling helper launchers such as `ctxpm.exe` and `ctxpm.cmd` in the same directory when needed.
7. The same installer may also be used independently for a global shell-wide install when the user wants a global `ctxpm` command.
   - Minimal global install: `sh cli/install.sh --scope global`
   - Optional explicit form: `sh cli/install.sh --scope global --version main`
8. After installation, copy, or build, verify that `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm --help` succeeds.
9. If CLI setup fails, do not stop the protocol installation. Continue with the manual workflow, but report clearly that the companion CLI could not be prepared.

### 5.3 Scan AI Resources in the Project

Scan all possible AI resources in the project, including but not limited to:

- Existing `AGENTS.md`, `CLAUDE.md`, `ANTIGRAVITY.md`
- `skills/`
- `rules/`
- `specs/`
- `prompts/`
- `mcp/`
- `ai/`
- Documents in `docs/` related to AI usage rules
- Any other directories that clearly contain AI behavior constraints, prompts, specifications, or tool configuration

For each resource, decide:

1. Whether it is a `dependency` or a `package`.
2. What its `type` is.
3. Whether it should be migrated under `.ctxpm/`.
4. Whether it only needs to be referenced instead of copied.

Before classifying existing discovered resources, do not silently assume they are all `package`. First, summarize the detected resources and confirm ownership with the user through interactive dialogue.

Recommended interaction flow:

1. Present candidate resources in small batches, grouped by directory or type.
2. For each resource or group, ask the user whether it is a project-local `package` or an external `dependency`.
3. If one directory contains a mix of project-local and external resources, split the resources before migration instead of forcing a single classification for the whole directory.
4. Record which classifications were explicitly confirmed by the user.
5. If a resource cannot be automatically identified with enough confidence, do not skip it. You must ask the user through interactive Q&A whether it should be treated as a project-local `package` or an external `dependency`.
6. For each resource, report the evidence used for classification, including project-specific references, external metadata, source hints, and unresolved conflicts.

### 5.4 Determine `package` vs `dependency`

Use these rules as explicit decision criteria. Do not classify ownership based on vague intuition or guesswork.

A classification is valid only when it is supported by at least one clear source of evidence:

- The user explicitly confirms that the resource is project-local or external.
- Existing project documents, manifests, or configuration explicitly declare the resource ownership.
- The resource has a clear upstream source, repository, registry, vendor origin, or import relationship showing that it is external.
- The resource is clearly authored and maintained inside the current repository for the current project, and this is evident from project context rather than guesswork.

If none of the above evidence exists, the ownership is unresolved and must be escalated to the user through interactive Q&A.

Do not use "the files are already in this repository" or "the files are tracked by Git" as sufficient evidence that a resource is a project-local `package`. A repository may contain vendored or previously copied external AI resources.

Before deciding ownership, inspect whether the resource is coupled to the current project:

- References to current project paths, files, directories, scripts, commands, package names, modules, APIs, schemas, environment variables, deployment targets, or documentation are strong `package` indicators.
- References to current project business domain, product names, brand names, repository-specific workflows, internal conventions, or project-maintained templates are strong `package` indicators.
- If the resource contains no references to current project paths or files, no project-specific commands, and no project-specific domain or maintenance context, treat that absence as a strong `dependency` indicator rather than assuming it is a `package`.
- External-looking metadata such as third-party `author`, `version`, `license`, upstream URL, registry name, vendor namespace, or generic cross-project examples are strong `dependency` indicators.
- Cross-references among a group of generic resources do not by themselves make them project-local. They may indicate a vendored external resource set.
- If a resource has both project-specific references and external metadata, report both sides and ask the user to confirm ownership before migration.

Classify as `package` when the resource:

- Is tightly coupled to the current project.
- Needs to be reviewed and evolved with the project.
- Is directly maintained by the current project team.
- Is this project's own rules, skills, specs, prompts, or MCP configuration.
- References current project files, paths, modules, commands, docs, product names, or domain concepts in a way that would not make sense outside this repository.

Classify as `dependency` when the resource:

- Comes from an external shared repository or team-wide shared resource.
- Is more like a reference than something owned by this project.
- Should not be committed to project version control by default.
- May be upgraded or replaced independently in the future.
- Is generic and reusable across projects.
- Has external author/version/license/source metadata.
- Has no references to current project paths, files, modules, commands, docs, product names, or domain concepts after inspection.

If unsure:

1. Stop before migration and ask the user whether the resource is a long-term project-local asset or an external shared asset.
2. For existing resources that are already present in the project, prefer explicit user confirmation even if you have a strong guess.
3. If the user does not answer but the resource has no project-specific references and has strong external indicators, classify it as a probable external `dependency`, report the exact evidence, and avoid treating it as a project-local `package`.
4. If the evidence is conflicting or insufficient, mark the resource as unresolved, do not migrate it, and report that user input is still required.
5. Never skip an existing resource merely because you cannot classify it automatically. Unclear ownership requires user interaction or an explicit unresolved report, not silent omission.

### 5.5 Create the `.ctxpm/` Directories

If they do not already exist, create:

- `.ctxpm/dependencies/skills/`
- `.ctxpm/dependencies/rules/`
- `.ctxpm/dependencies/specs/`
- `.ctxpm/dependencies/prompts/`
- `.ctxpm/dependencies/mcp/`
- `.ctxpm/packages/skills/`
- `.ctxpm/packages/rules/`
- `.ctxpm/packages/specs/`
- `.ctxpm/packages/prompts/`
- `.ctxpm/packages/mcp/`

### 5.6 Migrate Existing Resources

Organize the resources that have been identified.

Only migrate a resource after its ownership has been explicitly confirmed by the user or established through the explicit evidence criteria above. If ownership is unresolved, leave the resource in place and report it as blocked pending user confirmation.

#### Migrate as `package`

When a resource is a project-local asset:

1. Move the real resource content to `.ctxpm/packages/<plural-type>/...`.
2. Keep the name as clear and stable as possible.
3. Do not satisfy migration by creating a symlink inside `.ctxpm` that points back to the original resource path. `.ctxpm/packages/` must become the canonical storage location.
4. By default, create compatibility symlinks for the migrated resource in every confirmed agent's recognizable discovery directories for that resource type. If the original path is one of those discovery paths, replace the original path with a compatibility symlink pointing to the migrated resource under `.ctxpm/packages/`.
5. Add an appropriate compatibility ignore rule to `.gitignore` using the consolidation strategy in section 5.10, so the compatibility symlink or compatibility facade is not treated as the canonical tracked resource.
6. If the original resource path was tracked by Git, try to remove it from version control with `git rm --cached` after the canonical content has been moved under `.ctxpm/packages/`.
7. Record the canonical managed location and any compatibility symlink in `ctxpm.yaml`.
8. Avoid breaking the existing semantics of user content.

#### Migrate as `dependency`

When a resource is an external asset:

1. Move the real resource content to `.ctxpm/dependencies/<plural-type>/...`.
2. Do not satisfy migration by creating a symlink inside `.ctxpm` that points back to the original resource path. `.ctxpm/dependencies/` must become the canonical local workspace for external resources.
3. By default, create compatibility symlinks for the migrated resource in every confirmed agent's recognizable discovery directories for that resource type. If the original path is one of those discovery paths, replace the original path with a compatibility symlink pointing to the migrated resource under `.ctxpm/dependencies/`.
4. Add `.ctxpm/dependencies/` and an appropriate compatibility ignore rule to `.gitignore` using the consolidation strategy in section 5.10.
5. If the original resource path was tracked by Git, try to remove it from version control with `git rm --cached` after the canonical content has been moved under `.ctxpm/dependencies/`.
6. If there is only a local copy and the remote source cannot be confirmed, record the current migrated location as the temporary local source.
7. If the remote source is known, record `source` in `ctxpm.yaml`.
8. If the remote source is a GitHub repository, record the exact source commit SHA as the dependency `version`.
9. If the remote source is a direct URL, compute the SHA-256 hash of the AI resource entry file and record `version: sha256:<hex>`.
10. External resources should not be committed to version control by default.

#### Migration Principles

- Prefer preserving the original resource format.
- Do not rewrite resource content; only organize locations and index relationships.
- Do not lose context for the sake of tidiness.
- When a resource is unclear, pause automatic migration and report it first.
- The canonical resource content must live under `.ctxpm/packages/` or `.ctxpm/dependencies/` after migration.
- Compatibility symlinks are allowed at original locations and at agent-recognizable discovery locations, always pointing forward to the migrated `.ctxpm` locations. Do not create reverse symlinks from `.ctxpm` back to the old locations.
- Before replacing an original path with a symlink, check for path conflicts and avoid overwriting user content.

### 5.7 Generate or Update `ctxpm.yaml`

Create or update `ctxpm.yaml` with at least:

- `version`
- `project.name`
- `agents`
- `dependencies`
- `packages`
- `entrypoints`

Follow these rules:

- `version` is fixed at `1`.
- `project.name` uses the project name or root directory name.
- `agents` contains the currently confirmed agents.
- `dependencies` records external resources.
- `packages` records project-local resources.
- `entrypoints` records the entrypoint file for the current agent with `mode: managed`.
- External dependencies installed from GitHub or direct URLs record a hash-based `version`.

For the complete field-level format, follow the Bear.CTXPM source specification at [`docs/ctxpm-yaml.md`](docs/ctxpm-yaml.md) when it is available. When changing `ctxpm.yaml`, preserve unknown fields and unrelated entries unless they directly conflict with the requested change.

Install the complete format document as the `ctxpm` skill companion file at `.ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md`.

Reference template for `ctxpm.yaml`:

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

Complete `dependencies` and `packages` based on the actual scan results.

For external dependencies with a known GitHub or URL source, include `source` and `version`.
For GitHub dependencies, `source` locates the upstream resource and `version` records the installed commit SHA; do not duplicate the same commit SHA as `source.commit`.

GitHub dependency example:

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

### 5.8 Write the Root Markdown Entrypoint File

Write or update the managed `ctxpm` block in the entrypoint file for the selected agent.

The managed block must use this canonical template:

```md
<!-- ctxpm:begin agent=<agent-id> -->
This project uses `ctxpm` to manage AI resources.

Read AI resources in this order:
1. Read `ctxpm.yaml`.
2. Read `.ctxpm/packages/` before `.ctxpm/dependencies/`.
3. Within each root, use this default lookup order:
   - `rules`
   - `skills`
   - `specs`
   - `prompts`
   - `mcp`
4. When resources conflict, project-local `packages` take precedence over external `dependencies`.

Do not install AI resources directly into agent default locations. Before creating, reading, updating, or deleting any AI resource, use the companion `ctxpm` CLI when it is available. If the CLI is unavailable, use the `ctxpm` skill if it is available. Otherwise follow the same `ctxpm` classification, migration, compatibility-symlink, `.gitignore`, source-version, update-detection, and `ctxpm.yaml` update rules used by this project.

Install new external AI resources as `dependency` resources under `.ctxpm/dependencies/`, not as project-local `package` resources, unless the user explicitly confirms they should become project-maintained assets. For GitHub or direct URL resources, record the hash-based `version` in `ctxpm.yaml`.

Install new project-local AI resources as `package` resources under `.ctxpm/packages/` and record them in `ctxpm.yaml`.

Expose the bundled `ctxpm` dependency through this agent's default skill discovery compatibility path or paths so the agent can discover and invoke it through its normal skill or command surface. Subsequent AI resources should be installed and managed through the companion `ctxpm` CLI when it is available, or through the `ctxpm` skill when the CLI is unavailable. Both paths must keep canonical content under `.ctxpm/...` and record the required compatibility paths for each declared agent and resource type.
<!-- ctxpm:end -->
```

Template rules:

1. Replace only `<agent-id>` with the `entrypoints` key for the current file, such as `codex` for `AGENTS.md` or `claude-code` for `CLAUDE.md`.
2. Keep the body text identical across managed entrypoint files unless a future Bear.CTXPM specification revision changes the canonical template.
3. Do not add agent-specific AI resource management rules inside the managed block. Put any extra agent-specific guidance outside the managed block.
4. If any declared agent uses multiple compatibility paths for a resource type, keep the canonical body text unchanged and record the exact paths in `ctxpm.yaml`.

If the entrypoint file does not exist, create it.  
If the entrypoint file exists but has no managed block, insert one.  
If the managed block already exists, update only the managed block and do not overwrite user content outside the block.

### 5.9 Install the Bundled External `ctxpm` Dependency

Install an external helper skill dependency so future AI agents still have an explicit in-project workflow for creating, reading, updating, deleting, or reorganizing AI resources after the initial installation, even when the companion CLI is unavailable.

Follow the dedicated [`ctxpm` skill specification](docs/ctxpm-skill.md).

At minimum, this step must:

1. Create or update `.ctxpm/dependencies/skills/ctxpm/SKILL.md`.
2. Copy or write the complete `ctxpm.yaml` format document into the skill directory as `.ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md`, using the Bear.CTXPM `docs/ctxpm-yaml.md` source specification when it is available.
3. Create or update the companion CLI directory at `.ctxpm/dependencies/skills/ctxpm/cli/`.
4. Install or place the project-local companion CLI at `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm`.
   - If a global `ctxpm` is already available, it may be copied from that global installation.
   - If `cli/install.sh` is used, prefer `sh cli/install.sh --scope project` so the canonical project-local launcher is created in place.
   - On Windows-like shell environments, the project-local directory may also include sibling helper launchers such as `ctxpm.exe` and `ctxpm.cmd`, but `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm` remains the canonical launcher path for the protocol-managed installation.
5. Immediately create compatibility symlinks in every confirmed agent's default skill discovery directories, such as `.agents/skills/ctxpm`, so each agent can discover the skill from its normal skill or slash-command UI.
6. Record `ctxpm` in `ctxpm.yaml` as an external `dependency` of type `skill`.
7. Record both the canonical `.ctxpm/dependencies/...` path and every compatibility symlink path in `ctxpm.yaml`.
8. Do not omit the `compatibility` field for `ctxpm`.
9. Record a compatibility path in `ctxpm.yaml` for every confirmed agent's supported skill discovery directory.
10. Ensure the generated `SKILL.md` contains a compact `ctxpm.yaml` format reference and points to the sibling `ctxpm-yaml.md` companion document.

### 5.10 Adjust `.gitignore`

Check `.gitignore`.

If it does not include the following rules, append them:

```gitignore
.ctxpm/dependencies/
.ctxpm/state/
```

Do not ignore `.ctxpm/packages/` by default because it is a project-local asset.

For original AI resource paths that were replaced by compatibility symlinks or compatibility facades, choose the smallest safe `.gitignore` rule that covers the compatibility surface without hiding real project-owned files.

Use this consolidation strategy:

1. Group compatibility paths by their parent directories.
2. If an entire agent compatibility directory contains only compatibility symlinks or generated compatibility facades, ignore that directory instead of listing every resource path.
   - Example: use `.agents/skills/` instead of many `.agents/skills/<name>` entries.
3. If the entire agent directory is only used as a compatibility surface, ignore the broader directory.
   - Example: use `.agents/` only when `.agents/` contains no project-owned files that should remain tracked.
4. If a parent directory contains both compatibility facades and real project-owned files that must remain tracked, do not ignore the whole directory. Use narrower child-directory rules or individual path rules only for the compatibility facades.
5. Avoid adding many per-resource `.gitignore` entries under the same parent directory when one safe directory-level rule is enough.
6. Keep `ctxpm.yaml` precise even when `.gitignore` is consolidated: record each resource's exact `compatibility` path in `ctxpm.yaml`.
7. If a compatibility path or compatibility directory was tracked by Git, try to remove it from the index with `git rm --cached <path>` after confirming the migrated canonical copy exists under `.ctxpm/`.
8. Do not remove the canonical `.ctxpm/packages/` copy from Git when it is a project-local `package`.
9. Keep external `dependency` content out of Git by ignoring `.ctxpm/dependencies/`.

### 5.11 Report Results

After installation, report:

1. The currently identified agent.
2. Which entrypoint file was written.
3. Which `package` resources were created.
4. Which `dependency` resources were created.
5. Whether existing resources were migrated.
6. Whether `.gitignore` was updated.
7. Which ownership decisions were explicitly confirmed by the user.
8. Which ownership decisions were established through explicit project evidence.
9. Which resources remain unresolved and require user confirmation before migration.
10. Whether the `ctxpm` dependency was created or updated.
11. Whether compatibility symlinks for `ctxpm` were created in every confirmed agent's default skill discovery directories.
12. Whether the managed entrypoint block now instructs future AI agents to use `ctxpm` rules for AI resource CRUD operations.
13. Which external dependencies have hash-based versions recorded, and which still require source or entry-file confirmation.
14. Whether `.ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md` was created or updated as the `ctxpm` skill companion document.
15. Whether `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm` was installed or updated as the project-local companion CLI path.

---

## 6. Resource Type Mapping Rules

Use the following directory mapping:

| Type | Target Directory |
| --- | --- |
| `skill` | `.ctxpm/packages/skills/` or `.ctxpm/dependencies/skills/` |
| `rule` | `.ctxpm/packages/rules/` or `.ctxpm/dependencies/rules/` |
| `spec` | `.ctxpm/packages/specs/` or `.ctxpm/dependencies/specs/` |
| `prompt` | `.ctxpm/packages/prompts/` or `.ctxpm/dependencies/prompts/` |
| `mcp` | `.ctxpm/packages/mcp/` or `.ctxpm/dependencies/mcp/` |

When placing a resource, always determine:

1. Whether it is a `package` or a `dependency`.
2. What its `type` is.
3. Then decide the final directory.

---

## 7. Conflict Handling Rules You Must Follow

### 7.1 Path Conflicts

If two resources would land at the same path:

- Do not silently overwrite.
- Stop automatic migration.
- Report the conflict and suggest a name.

### 7.2 Unclear Type

If a resource type cannot be determined clearly:

- Report candidate types.
- Provide the reasoning.
- Do not skip the resource.
- Ask the user through interactive Q&A whether the resource should be treated as a project-local `package` or an external `dependency`, and report any candidate `type` values if they are still unclear.
- Do not assume ownership or migrate the resource until the user answers or clear evidence is found.

### 7.3 Damaged Blocks

If an entrypoint document already contains a damaged `ctxpm` block:

- Do not overwrite the whole file directly.
- First report that the block is damaged.
- Then repair or rebuild it according to the user's intent.

### 7.4 Insufficient Capability

If you lack any of these capabilities:

- Writing files
- Searching directories
- Reading project content
- Fetching remote dependencies from the network

At minimum, output a clear installation recommendation instead of pretending that installation has been completed.

---

## 8. Expected State After Installation

After completion, the project should at least satisfy:

1. `ctxpm.yaml` exists.
2. `.ctxpm/packages/` exists.
3. `.ctxpm/dependencies/` exists.
4. The root entrypoint file for the current agent exists.
5. The entrypoint file contains a managed `ctxpm` block.
6. Existing AI resources have been organized as `package` or `dependency` as much as possible.
7. `.gitignore` ignores both `.ctxpm/dependencies/` and `.ctxpm/state/`.
8. `.ctxpm/dependencies/skills/ctxpm/SKILL.md` exists and is recorded in `ctxpm.yaml`.
9. `.ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md` exists as a companion document for the `ctxpm` skill.
10. `.ctxpm/dependencies/skills/ctxpm/cli/ctxpm` exists as the canonical local CLI path for the bundled `ctxpm` skill when the companion CLI was prepared.
11. Compatibility symlinks for managed resources exist in each confirmed agent's recognizable discovery directories for the corresponding resource types.
12. The `ctxpm` dependency entry in `ctxpm.yaml` records both the canonical `.ctxpm/dependencies/...` path and every compatibility symlink path.
13. The managed entrypoint block uses the canonical `ctxpm` template and instructs future AI agents to use `ctxpm` or the same `ctxpm` workflow before creating, reading, updating, or deleting AI resources.
14. Future AI resources continue to be managed using the same `ctxpm` rules instead of agent default install locations.
15. External dependencies installed from GitHub or direct URLs record hash-based `version` values in `ctxpm.yaml` for future update detection.
16. The complete YAML format lives in `.ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md`.

---

## 9. Execution Notes for AI

If you are an AI agent directly given this file by a user, work as follows:

1. Ask for or identify the agent first.
2. Then scan the project.
3. Then propose a migration plan.
4. Execute the migration when write permission is available.
5. Finally report the modification results.

Do not start by changing files immediately.  
Identify, classify, migrate, and then write entrypoint documents.

Your goal is not to "generate a pile of new formats", but to make this project from now on have a unified, clear AI resource management structure that other AI agents can continue to take over.
