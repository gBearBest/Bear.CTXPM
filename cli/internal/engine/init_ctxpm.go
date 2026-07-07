package engine

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

const bundledCtxpmSkillContent = `# ctxpm

Read ` + "`ctxpm-yaml.md`" + ` in this directory before editing ` + "`ctxpm.yaml`" + `.

## Purpose

` + "`ctxpm`" + ` manages the AI resource lifecycle for a Bear.CTXPM project:

- install
- add
- list
- validate
- check-updates
- update
- remove

## Resource Model

Treat each managed resource as:

1. a **resource root**
2. an **entry file**
3. an optional **upstream source**

The canonical root lives under ` + "`.ctxpm/packages/`" + ` or ` + "`.ctxpm/dependencies/`" + `. Compatibility paths only expose that root to agent-specific discovery directories.

## Dependency Version Rules

- Git resources use the installed commit SHA.
- Single-file non-Git resources use ` + "`sha256:<hex>`" + `.
- Directory non-Git resources use ` + "`sha256tree:<hex>`" + `.

Always version the whole resolved resource root, not only one guessed companion file.

## Installation Rules

1. Materialize the resource root from its source.
2. Validate that the result matches ` + "`layout`" + `.
3. Validate that ` + "`entry`" + ` exists inside the root.
4. Install or replace the canonical root.
5. Repair compatibility links.
6. Only rewrite ` + "`ctxpm.yaml`" + ` when a recorded dependency version actually changed.

## Bundled ` + "`ctxpm`" + ` Registration

The bundled ` + "`ctxpm`" + ` skill should itself be modeled as a directory dependency:

` + "```yaml" + `
- name: ctxpm
  type: skill
  layout: dir
  path: .ctxpm/dependencies/skills/ctxpm
  entry: SKILL.md
  source:
    type: git
    url: https://github.com/gBearBest/Bear.CTXPM
    path: resources/skills/ctxpm
    entry: SKILL.md
` + "```" + `
`

const bundledCtxpmYAMLContent = `# ctxpm.yaml Quick Reference

Use ` + "`version: 2`" + `.

## Core Fields

` + "```yaml" + `
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
` + "```" + `

## Resource Shape

` + "```yaml" + `
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
` + "```" + `

Rules:

- ` + "`path`" + ` is the canonical local resource root.
- ` + "`entry`" + ` is the file an AI should read first.
- ` + "`layout`" + ` is ` + "`file`" + ` or ` + "`dir`" + `.
- Dependencies may include ` + "`source`" + ` and ` + "`version`" + `.
- Packages normally omit ` + "`source`" + ` and ` + "`version`" + `.

## Source Types

### Git

- ` + "`source.type: git`" + `
- ` + "`source.path`" + ` is the upstream resource root
- ` + "`source.entry`" + ` is the upstream entry file
- version = commit SHA

### URL, single file

` + "```yaml" + `
source:
  type: url
  url: https://example.com/rules/release.md
  entry: release.md
` + "```" + `

- use ` + "`layout: file`" + `
- version = ` + "`sha256:<hex>`" + `

### URL, multi file

` + "```yaml" + `
source:
  type: url
  url: https://example.com/reviewer/
  files:
    - SKILL.md
    - rules/review.md
  entry: SKILL.md
` + "```" + `

- use ` + "`layout: dir`" + `
- ` + "`files`" + ` are relative paths
- version = ` + "`sha256tree:<hex>`" + `

### Archive

` + "```yaml" + `
source:
  type: archive
  url: https://example.com/reviewer.zip
  path: skills/reviewer
  entry: SKILL.md
` + "```" + `

- ` + "`source.path`" + ` is the resource root inside the extracted archive
- version uses ` + "`sha256:<hex>`" + ` for file roots or ` + "`sha256tree:<hex>`" + ` for dir roots
`

func ensureManagedEntrypoint(path, agent string) error {
	content := manifest.ManagedEntrypoint(agent)
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(content), 0o644)
		}
		return err
	}

	updated := replaceManagedBlock(string(existing), content)
	return os.WriteFile(path, []byte(updated), 0o644)
}

func replaceManagedBlock(existing, block string) string {
	const begin = "<!-- ctxpm:begin agent="
	const end = "<!-- ctxpm:end -->"

	start := strings.Index(existing, begin)
	if start >= 0 {
		rest := existing[start:]
		endIndex := strings.Index(rest, end)
		if endIndex >= 0 {
			endIndex += start + len(end)
			prefix := strings.TrimRight(existing[:start], "\n")
			suffix := strings.TrimLeft(existing[endIndex:], "\n")
			switch {
			case prefix == "" && suffix == "":
				return block
			case prefix == "":
				return block + "\n\n" + suffix
			case suffix == "":
				return prefix + "\n\n" + block + "\n"
			default:
				return prefix + "\n\n" + block + "\n\n" + suffix
			}
		}
	}

	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return block
	}
	return trimmed + "\n\n" + block + "\n"
}

func ensureBundledCtxpm(root string, agents []string) (manifest.Resource, []string, error) {
	resource := bundledCtxpmResource(agents)
	created := []string{}

	resourceRoot := filepath.Join(root, filepath.FromSlash(resource.Path))
	if err := os.MkdirAll(resourceRoot, 0o755); err != nil {
		return manifest.Resource{}, nil, err
	}
	created = append(created, resourceRoot)

	skillPath := filepath.Join(resourceRoot, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(bundledCtxpmSkillContent), 0o644); err != nil {
		return manifest.Resource{}, nil, err
	}
	created = append(created, skillPath)

	yamlPath := filepath.Join(resourceRoot, "ctxpm-yaml.md")
	if err := os.WriteFile(yamlPath, []byte(bundledCtxpmYAMLContent), 0o644); err != nil {
		return manifest.Resource{}, nil, err
	}
	created = append(created, yamlPath)

	executable, err := os.Executable()
	if err != nil {
		return manifest.Resource{}, nil, err
	}
	info, err := os.Stat(executable)
	if err != nil {
		return manifest.Resource{}, nil, err
	}
	cliPath := filepath.Join(resourceRoot, "cli", "ctxpm")
	if err := copyFile(executable, cliPath, info.Mode()); err != nil {
		return manifest.Resource{}, nil, err
	}
	created = append(created, cliPath)

	if err := ensureCompatibility(root, resource); err != nil {
		return manifest.Resource{}, nil, err
	}
	for _, compat := range resource.Compatibility {
		created = append(created, filepath.Join(root, filepath.FromSlash(compat)))
	}

	return resource, created, nil
}

func bundledCtxpmResource(agents []string) manifest.Resource {
	resource := manifest.Resource{
		Name:   "ctxpm",
		Type:   "skill",
		Layout: manifest.LayoutDir,
		Path:   ".ctxpm/dependencies/skills/ctxpm",
		Entry:  "SKILL.md",
		Source: &manifest.Source{
			Type:  "git",
			URL:   "https://github.com/gBearBest/Bear.CTXPM",
			Path:  "resources/skills/ctxpm",
			Entry: "SKILL.md",
		},
		Version: currentBuildRevision(),
	}
	resource.Compatibility = compatibilityPaths(agents, resource)
	return resource
}

func currentBuildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}
