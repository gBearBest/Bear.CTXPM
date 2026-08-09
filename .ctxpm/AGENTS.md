<!-- ctxpm:begin -->
This project uses `ctxpm` to manage AI resources. For detailed lifecycle work, first use the bundled `ctxpm` skill at `.ctxpm/dependencies/skills/ctxpm/SKILL.md`. Manage `rules`, `skills`, `specs`, `prompts`, `mcp`, and `memories` through `ctxpm`.

`ctxpm.yaml` is the canonical source for agent profiles, entrypoint mappings, and compatibility paths. During normal AI work, prefer `ctxpm detect` over `ctxpm check-updates`; if unmanaged resources are found outside `.ctxpm`, ask before migrating, then run `ctxpm migrate` and `ctxpm validate`.

Read resources in this order: `ctxpm.yaml`, then relevant `.ctxpm/packages/` resources, then `.ctxpm/dependencies/`. Within each root, use this priority: `rules`, `skills`, `specs`, `prompts`, `mcp`. Read `memories` only when task context requires them. On conflicts, `packages` override `dependencies`, and `rules` override `memories`.

Do not install AI resources into agent default locations directly. External resources belong under `.ctxpm/dependencies/`, project-maintained resources under `.ctxpm/packages/`, and resources should be recorded in `ctxpm.yaml` with required compatibility paths. Record the source-appropriate `version` for dependencies.
<!-- ctxpm:end -->
