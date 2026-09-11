<!-- ctxpm:begin -->
This project uses `ctxpm` to manage AI resources: `rules`, `skills`, `specs`, `prompts`, `mcp`, and `memories`.

Read `ctxpm.yaml` first, then task-relevant project resources under `.ctxpm/packages/` before external resources under `.ctxpm/dependencies/`.

At session start, run `ctxpm detect --agent <current-agent>` to verify this agent is enrolled; if the status is `agent_not_enrolled`, ask the user to confirm before running the suggested `ctxpm init --agent <agent>`. At a session boundary, run `ctxpm check-updates` when `update_policy` says the check is enabled and due. Keep non-actionable results silent.

When you need to **install**, **update**, **migrate**, **validate**, or **remove** AI resources (rules, skills, specs, prompts, mcp, memories), or when detect/check-updates finds actionable candidates, read the bundled skill at `.ctxpm/dependencies/skills/ctxpm/SKILL.md` and follow its workflow. Get user confirmation before `ctxpm update` or `ctxpm migrate`.

<!-- ctxpm:end -->
