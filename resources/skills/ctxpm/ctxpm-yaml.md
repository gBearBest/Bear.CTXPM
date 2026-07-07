# ctxpm.yaml Quick Reference

Use `version: 2`.

## Core Fields

```yaml
version: 2

project:
  name: your-project-name

agents:
  - codex

dependencies: []
packages: []

entrypoints:
  codex:
    file: AGENTS.md
    mode: managed
```

## Resource Shape

```yaml
- name: reviewer
  type: skill
  layout: dir
  path: .ctxpm/dependencies/skills/reviewer
  entry: SKILL.md
  source:
    type: git
    url: https://github.com/example/ai-resources
    path: skills/reviewer
    entry: SKILL.md
  version: 0123456789abcdef0123456789abcdef01234567
  compatibility:
    - .agents/skills/reviewer
```

Rules:

- `path` is the canonical local resource root.
- `entry` is the file an AI should read first.
- `layout` is `file` or `dir`.
- Dependencies may include `source` and `version`.
- Packages normally omit `source` and `version`.

## Source Types

### Git

- `source.type: git`
- `source.path` is the upstream resource root
- `source.entry` is the upstream entry file
- version = commit SHA

### URL, single file

```yaml
source:
  type: url
  url: https://example.com/rules/release.md
  entry: release.md
```

- use `layout: file`
- version = `sha256:<hex>`

### URL, multi file

```yaml
source:
  type: url
  url: https://example.com/reviewer/
  files:
    - SKILL.md
    - rules/review.md
  entry: SKILL.md
```

- use `layout: dir`
- `files` are relative paths
- version = `sha256tree:<hex>`

### Archive

```yaml
source:
  type: archive
  url: https://example.com/reviewer.zip
  path: skills/reviewer
  entry: SKILL.md
```

- `source.path` is the resource root inside the extracted archive
- version uses `sha256:<hex>` for file roots or `sha256tree:<hex>` for dir roots
