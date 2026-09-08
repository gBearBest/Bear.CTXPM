<!-- ctxpm:begin -->
This project uses `ctxpm` to manage `rules`, `skills`, `specs`, `prompts`, `mcp`, and `memories`.

Read `ctxpm.yaml` first, then task-relevant project resources under `.ctxpm/packages/` before external resources under `.ctxpm/dependencies/`.

During normal AI work, run `ctxpm detect` periodically and, at a session boundary, run `ctxpm check-updates` when `update_policy` says the check is enabled and due. Keep non-actionable results silent.

When you need to **install**, **update**, **migrate**, **validate**, or **remove** AI resources (rules, skills, specs, prompts, mcp, memories), or when detect/check-updates finds actionable candidates, read the bundled skill at `.ctxpm/dependencies/skills/ctxpm/SKILL.md` and follow its workflow. Get user confirmation before `ctxpm update` or `ctxpm migrate`.
<!-- ctxpm:end -->
