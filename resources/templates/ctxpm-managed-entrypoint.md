<!-- ctxpm:begin -->
This project uses `ctxpm` to manage AI resources.
For detailed create/install/detect/migrate/update/remove/validate work, first use the bundled `ctxpm` skill installed by initialization at `.ctxpm/dependencies/skills/ctxpm/SKILL.md`.
The following AI resource types must still be managed through `ctxpm` so they are not skipped: `rules`, `skills`, `specs`, `prompts`, `mcp`, `memories`.

Agent profiles, shared root entrypoint mappings, and any compatibility entrypoint aliases are declared in `ctxpm.yaml`. Treat this file as the canonical shared entrypoint content when the project maps multiple agents onto the same root entrypoint.

During normal AI work, run `ctxpm detect` more often than `ctxpm check-updates`. If it finds AI resources installed outside ctxpm-managed roots, ask the user whether to migrate them, then run `ctxpm migrate` and `ctxpm validate` to verify the result.

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

Do not install AI resources directly into agent default locations. Before creating, reading, updating, or deleting any AI resource, use the companion `ctxpm` CLI when it is available; otherwise use the bundled `ctxpm` skill workflow above.

Install new external AI resources as `dependency` resources under `.ctxpm/dependencies/`, not as project-local `package` resources, unless the user explicitly confirms they should become project-maintained assets. For GitHub or direct URL resources, record the hash-based `version` in `ctxpm.yaml`.

Install new project-local AI resources as `package` resources under `.ctxpm/packages/` and record them in `ctxpm.yaml`.

Expose the bundled `ctxpm` dependency through this agent's default skill discovery compatibility path or paths so the agent can discover and invoke it through its normal skill or command surface. Subsequent AI resources should stay under `.ctxpm/...` and keep the required compatibility paths for each declared agent and resource type.
<!-- ctxpm:end -->
