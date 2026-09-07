<!-- ctxpm:begin -->
This project uses `ctxpm` to manage AI resources. For detailed lifecycle work, first use the bundled `ctxpm` skill at `.ctxpm/dependencies/skills/ctxpm/SKILL.md`. Manage `rules`, `skills`, `specs`, `prompts`, `mcp`, and `memories` through `ctxpm`.

`ctxpm.yaml` is the canonical source for agent profiles, entrypoint mappings, compatibility paths, and `update_policy`. During normal AI work, use the bundled `ctxpm` CLI for routine read-only checks: run `ctxpm detect` on a shorter cadence to find unmanaged resources, and at a session boundary run `ctxpm check-updates` when `update_policy` says checks are enabled and due. Ask for user confirmation only after an update or migration candidate is found, before running `ctxpm update` or `ctxpm migrate`; do not edit managed resources by hand when the CLI can perform the operation.

Keep routine `ctxpm` checks silent. If there are no dependency updates, migration candidates, or other actionable `ctxpm` issues, do not mention `ctxpm` in progress updates or the final response; immediately continue with the user's current request. Surface `ctxpm` only when a finding requires user confirmation or action, or when it blocks or materially affects the request.

Read resources in this order: `ctxpm.yaml`, then relevant `.ctxpm/packages/` resources, then `.ctxpm/dependencies/`. Within each root, use this priority: `rules`, `skills`, `specs`, `prompts`, `mcp`. Read `memories` only when task context requires them. On conflicts, `packages` override `dependencies`, and `rules` override `memories`.

Do not install AI resources into agent default locations directly. External resources belong under `.ctxpm/dependencies/`, project-maintained resources under `.ctxpm/packages/`, and resources should be recorded in `ctxpm.yaml` with required compatibility paths. Record the source-appropriate `version` for dependencies.
<!-- ctxpm:end -->
