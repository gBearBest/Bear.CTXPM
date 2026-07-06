package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	ErrNotFound = errors.New("ctxpm manifest not found")
)

var validTypes = map[string]bool{
	"skill":  true,
	"rule":   true,
	"spec":   true,
	"prompt": true,
	"mcp":    true,
}

type Manifest struct {
	Version      int                   `yaml:"version"`
	Project      Project               `yaml:"project"`
	Agents       []string              `yaml:"agents,omitempty"`
	UpdatePolicy UpdatePolicy          `yaml:"update_policy,omitempty"`
	Dependencies []Resource            `yaml:"dependencies"`
	Packages     []Resource            `yaml:"packages"`
	Entrypoints  map[string]Entrypoint `yaml:"entrypoints,omitempty"`
}

type Project struct {
	Name string `yaml:"name"`
}

type UpdatePolicy struct {
	Enabled     *bool  `yaml:"enabled,omitempty"`
	Interval    string `yaml:"interval,omitempty"`
	IncludeSelf *bool  `yaml:"include_self,omitempty"`
}

type Entrypoint struct {
	File string `yaml:"file"`
	Mode string `yaml:"mode"`
}

type Resource struct {
	Name          string   `yaml:"name"`
	Type          string   `yaml:"type"`
	Path          string   `yaml:"path"`
	Source        *Source  `yaml:"source,omitempty"`
	Version       string   `yaml:"version,omitempty"`
	Compatibility []string `yaml:"compatibility,omitempty"`
}

type Source struct {
	Type  string `yaml:"type"`
	URL   string `yaml:"url"`
	Path  string `yaml:"path,omitempty"`
	Ref   string `yaml:"ref,omitempty"`
	Entry string `yaml:"entry,omitempty"`
}

func DefaultPolicy() UpdatePolicy {
	enabled := true
	includeSelf := true
	return UpdatePolicy{
		Enabled:     &enabled,
		Interval:    "1d",
		IncludeSelf: &includeSelf,
	}
}

func Load(root string) (*Manifest, string, error) {
	manifestPath := filepath.Join(root, "ctxpm.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, manifestPath, ErrNotFound
		}
		return nil, manifestPath, err
	}
	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, manifestPath, err
	}
	if err := m.Validate(); err != nil {
		return nil, manifestPath, err
	}
	return &m, manifestPath, nil
}

func Save(root string, m *Manifest) (string, error) {
	manifestPath := filepath.Join(root, "ctxpm.yaml")
	data, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}
	return manifestPath, os.WriteFile(manifestPath, data, 0o644)
}

func (m *Manifest) Validate() error {
	if m.Version == 0 {
		m.Version = 1
	}
	if m.Version != 1 {
		return fmt.Errorf("unsupported ctxpm.yaml version %d", m.Version)
	}
	if strings.TrimSpace(m.Project.Name) == "" {
		return errors.New("project.name is required")
	}
	for _, agent := range m.Agents {
		if strings.TrimSpace(agent) == "" {
			return errors.New("agents entries must not be empty")
		}
	}
	for _, dep := range m.Dependencies {
		if err := dep.Validate("dependency"); err != nil {
			return err
		}
		if dep.Source == nil {
			return fmt.Errorf("dependency %q requires source", dep.Name)
		}
	}
	for _, pkg := range m.Packages {
		if err := pkg.Validate("package"); err != nil {
			return err
		}
	}
	return nil
}

func (r Resource) Validate(kind string) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("%s name is required", kind)
	}
	if !validTypes[r.Type] {
		return fmt.Errorf("%s %q has unsupported type %q", kind, r.Name, r.Type)
	}
	if strings.TrimSpace(r.Path) == "" {
		return fmt.Errorf("%s %q is missing path", kind, r.Name)
	}
	prefix := ".ctxpm/packages/"
	if kind == "dependency" {
		prefix = ".ctxpm/dependencies/"
	}
	if !strings.HasPrefix(filepath.ToSlash(r.Path), prefix) {
		return fmt.Errorf("%s %q path %q must stay under %s", kind, r.Name, r.Path, prefix)
	}
	if r.Source != nil {
		switch r.Source.NormalizedType() {
		case "git":
			if strings.TrimSpace(r.Source.URL) == "" {
				return fmt.Errorf("%s %q git source is missing url", kind, r.Name)
			}
			if strings.TrimSpace(r.Source.Path) == "" {
				return fmt.Errorf("%s %q git source is missing path", kind, r.Name)
			}
		case "url":
			if strings.TrimSpace(r.Source.URL) == "" {
				return fmt.Errorf("%s %q url source is missing url", kind, r.Name)
			}
			if strings.TrimSpace(r.Source.Entry) == "" {
				return fmt.Errorf("%s %q url source is missing entry", kind, r.Name)
			}
		default:
			return fmt.Errorf("%s %q has unsupported source.type %q", kind, r.Name, r.Source.Type)
		}
	}
	return nil
}

func (s Source) NormalizedType() string {
	switch s.Type {
	case "git", "github":
		return "git"
	case "url":
		return "url"
	default:
		return s.Type
	}
}

func TypeDir(resourceType string) string {
	switch resourceType {
	case "skill":
		return "skills"
	case "rule":
		return "rules"
	case "spec":
		return "specs"
	case "prompt":
		return "prompts"
	case "mcp":
		return "mcp"
	default:
		return resourceType + "s"
	}
}

func EntrypointFile(agent string) string {
	switch agent {
	case "claude-code":
		return "CLAUDE.md"
	case "antigravity":
		return "ANTIGRAVITY.md"
	default:
		return "AGENTS.md"
	}
}

func ManagedEntrypoint(agent string) string {
	file := EntrypointFile(agent)
	return fmt.Sprintf("# %s\n\n<!-- ctxpm:begin agent=%s -->\nThis project uses Bear.CTXPM-managed AI resources.\n\n1. Read `ctxpm.yaml` first.\n2. Prefer `.ctxpm/packages/` before `.ctxpm/dependencies/`.\n3. Treat this block as managed by ctxpm.\n<!-- ctxpm:end -->\n", strings.TrimSuffix(file, filepath.Ext(file)), agent)
}

func (m *Manifest) HasResource(name string) bool {
	for _, dep := range m.Dependencies {
		if dep.Name == name {
			return true
		}
	}
	for _, pkg := range m.Packages {
		if pkg.Name == name {
			return true
		}
	}
	return false
}
