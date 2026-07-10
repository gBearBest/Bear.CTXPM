<!-- ctxpm:begin agent=generic -->
This project uses `ctxpm` to manage AI resources.

Read AI resources in this order:
1. Read `ctxpm.yaml`.
2. Read relevant resources under `.ctxpm/packages/` before `.ctxpm/dependencies/`.
3. Within each root, use this default lookup order for fixed-priority resource types:
   - `rules`
   - `skills`
   - `specs`
   - `prompts`
   - `mcp`
4. `memories` do not use a fixed global read order. When a task depends on historical decisions, project background, terminology, prior migrations, known pitfalls, or other context, inspect relevant `memories` on demand.
5. When resources conflict, project-local `packages` take precedence over external `dependencies`, and `rules` take precedence over `memories`.

Do not install AI resources directly into agent default locations. Before creating, reading, updating, or deleting any AI resource, use the companion `ctxpm` CLI when it is available. If the CLI is unavailable, use the `ctxpm` skill if it is available. Otherwise follow the same `ctxpm` classification, migration, compatibility-symlink, `.gitignore`, source-version, update-detection, and `ctxpm.yaml` update rules used by this project.

Install new external AI resources as `dependency` resources under `.ctxpm/dependencies/`, not as project-local `package` resources, unless the user explicitly confirms they should become project-maintained assets. For GitHub or direct URL resources, record the hash-based `version` in `ctxpm.yaml`.

Install new project-local AI resources as `package` resources under `.ctxpm/packages/` and record them in `ctxpm.yaml`.

Expose the bundled `ctxpm` dependency through this agent's default skill discovery compatibility path or paths so the agent can discover and invoke it through its normal skill or command surface. Subsequent AI resources should be installed and managed through the companion `ctxpm` CLI when it is available, or through the `ctxpm` skill when the CLI is unavailable. Both paths must keep canonical content under `.ctxpm/...` and record the required compatibility paths for each declared agent and resource type.
<!-- ctxpm:end -->
