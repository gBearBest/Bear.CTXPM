package engine

//go:generate go run generate_bundled_docs.go

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

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
