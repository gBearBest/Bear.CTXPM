# ctxpm source vs managed-resource boundary

This repository contains two different kinds of ctxpm-related content, and they must not be mixed together during analysis or edits.

Default rule: for normal AI work in this repository, restrict both reads and writes to the Bear.CTXPM product source and product documentation scope unless the user explicitly asks for repository-local AI resource management.

## 1. Treat these as the Bear.CTXPM product source of truth

Use these paths when the user asks about ctxpm features, behavior, releases, installation, schema, public docs, tests, or implementation changes:

- `cli/`
- `docs/`
- `resources/`
- `README.md`
- `INSTALL.md`
- `.github/workflows/`

For bundled-skill source changes, the canonical source is `resources/skills/ctxpm/`, not the installed copy under `.ctxpm/`.

## 2. Treat these as this repository's own managed AI-resource layer

Use these paths only when the user is explicitly asking about agent behavior, project AI-resource management, ctxpm packaging/installation state, or repository-local customizations:

- `ctxpm.yaml`
- `AGENTS.md`
- `.ctxpm/`
- `.agents/`

Important distinctions:

- `ctxpm.yaml` describes how this repository consumes and manages AI resources for itself.
- `.ctxpm/packages/` contains project-local AI resources for this repository.
- `.ctxpm/dependencies/` contains installed dependency copies, including a local installed copy of the bundled `ctxpm` skill.
- `.ctxpm/dependencies/skills/ctxpm/` is a managed install artifact, not the canonical source to edit for product changes.

## 3. Read/write boundary rules

For normal product work:

- Read only from the product/source scope unless extra repository-management context is explicitly required.
- Write only to the product/source scope.
- Do not use `ctxpm.yaml`, `.ctxpm/**`, or `.agents/**` as default investigation targets.
- Do not expand the working set into managed-resource paths just because similar filenames or duplicated content exist there.

For repository-local AI-resource management work:

- Limit reads and writes to the specific managed-resource paths needed by the request.
- Keep those changes isolated from product-source edits unless the user explicitly asks for both.

## 4. Scope-selection rules

Before changing files, first classify the request into one of these scopes:

- **Product/source scope**: work in `cli/`, `docs/`, `resources/`, tests, and public documentation.
- **Managed-resource scope**: work in `ctxpm.yaml`, `AGENTS.md`, `.ctxpm/`, `.agents/`, and other repository-local AI customizations.

Do not cross from one scope into the other unless the user explicitly asks for both, or the task is specifically about synchronizing the managed copy with the product source.

## 5. Evidence and edit rules

- Do not use `.ctxpm/**` or `ctxpm.yaml` as the primary evidence for how the Bear.CTXPM product works.
- Prefer `resources/skills/ctxpm/` over `.ctxpm/dependencies/skills/ctxpm/` when reasoning about bundled-skill source content.
- Never edit `.ctxpm/dependencies/**` to implement product changes.
- Treat `.ctxpm/dependencies/**` as installed managed copies only, not writable source files.
- If a user explicitly asks to refresh or repair an installed managed copy, perform that as repository-management work rather than product-source editing.
- When both source and managed copies exist for the same skill or document, treat `resources/...` as the editable source and `.ctxpm/dependencies/...` as the consumed snapshot.
