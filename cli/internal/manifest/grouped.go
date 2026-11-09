package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

// Grouped declarations are a disk format. Resource remains the fully resolved
// execution model: Path is local and Source.Path is upstream.
type dependencyGroup struct {
	Source    Source                `yaml:"source"`
	Type      string                `yaml:"type,omitempty"`
	Path      *string               `yaml:"path,omitempty"`
	Resources []resourceDeclaration `yaml:"resources"`
}
type resourceDeclaration struct {
	Name    string  `yaml:"name"`
	Type    string  `yaml:"type,omitempty"`
	Path    *string `yaml:"path,omitempty"`
	Layout  string  `yaml:"layout,omitempty"`
	Entry   string  `yaml:"entry,omitempty"`
	Version string  `yaml:"version,omitempty"`
}
type groupedDocument struct {
	Version      string                `yaml:"version"`
	Project      Project               `yaml:"project"`
	Agents       []string              `yaml:"agents,omitempty"`
	UpdatePolicy UpdatePolicy          `yaml:"update_policy,omitempty"`
	Dependencies []dependencyGroup     `yaml:"dependencies"`
	Packages     []resourceDeclaration `yaml:"packages"`
}

func Parse(data []byte) (*Manifest, error) {
	var header struct {
		Version string `yaml:"version"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	if header.Version == "" {
		header.Version = CurrentManifestVersion
	}
	m := &Manifest{}
	switch header.Version {
	case CurrentManifestVersion:
		if err := yaml.Unmarshal(data, m); err != nil {
			return nil, err
		}
	case GroupedManifestVersion:
		var d groupedDocument
		if err := yaml.Unmarshal(data, &d); err != nil {
			return nil, err
		}
		// Reject old or misplaced fields rather than silently changing their meaning.
		var doc yaml.Node
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return nil, err
		}
		if err := checkGroupedShape(doc.Content[0]); err != nil {
			return nil, err
		}
		m.Version, m.Project, m.Agents, m.UpdatePolicy = d.Version, d.Project, d.Agents, d.UpdatePolicy
		m.Dependencies, m.Packages = []Resource{}, []Resource{}
		for i, g := range d.Dependencies {
			if len(g.Resources) == 0 {
				return nil, fmt.Errorf("dependency group %d has no resources", i+1)
			}
			if g.Source.Path != "" {
				return nil, errors.New("use group path, not source.path, in schema 2.0")
			}
			if g.Source.NormalizedType() == "url" && len(g.Resources) != 1 {
				return nil, errors.New("URL groups require exactly one resource")
			}
			for _, declaration := range g.Resources {
				r, err := expandDeclaration(g, declaration)
				if err != nil {
					return nil, fmt.Errorf("dependency %q: %w", declaration.Name, err)
				}
				m.Dependencies = append(m.Dependencies, r)
			}
		}
		for _, declaration := range d.Packages {
			if declaration.Path != nil {
				return nil, errors.New("package path is derived in schema 2.0; use entry for a file")
			}
			r := Resource{Name: declaration.Name, Type: declaration.Type, Layout: declaration.Layout, Entry: declaration.Entry, Version: declaration.Version}
			if r.Layout == "" {
				r.Layout = LayoutDir
			}
			if r.Entry == "" {
				r.Entry = r.EffectiveEntry()
			}
			var err error
			r.Path, err = DerivedResourcePath(r, "package")
			if err != nil {
				return nil, err
			}
			m.Packages = append(m.Packages, r)
		}
	default:
		return nil, fmt.Errorf("unsupported ctxpm.yaml version %s", header.Version)
	}
	m.detectLegacyFields()
	if err := m.Validate(); err != nil {
		return nil, err
	}
	m.original = append([]byte(nil), data...)
	return m, nil
}

func checkGroupedShape(root *yaml.Node) error {
	if root.Kind != yaml.MappingNode {
		return errors.New("ctxpm manifest root must be a mapping")
	}
	for _, section := range []string{"dependencies", "packages"} {
		seq := mappingValue(root, section)
		if seq == nil || seq.Kind != yaml.SequenceNode {
			return fmt.Errorf("%s must be a sequence", section)
		}
		for _, item := range seq.Content {
			if item.Kind != yaml.MappingNode {
				return fmt.Errorf("%s entries must be mappings", section)
			}
			if section == "dependencies" {
				for _, key := range []string{"layout", "entry", "version"} {
					if mappingValue(item, key) != nil {
						return fmt.Errorf("group %s is not supported; declare it on each resource", key)
					}
				}
				if mappingValue(item, "name") != nil || mappingValue(item, "source") == nil {
					return errors.New("schema 2.0 dependencies must be source groups")
				}
				source := mappingValue(item, "source")
				if source.Kind != yaml.MappingNode {
					return errors.New("group source must be a mapping")
				}
				if mappingValue(source, "path") != nil {
					return errors.New("use group path, not source.path, in schema 2.0")
				}
				children := mappingValue(item, "resources")
				if children == nil || children.Kind != yaml.SequenceNode {
					return errors.New("group resources must be a sequence")
				}
				for _, child := range children.Content {
					if child.Kind != yaml.MappingNode {
						return errors.New("resources must be mappings")
					}
					if mappingValue(child, "source") != nil || mappingValue(child, "resources") != nil {
						return errors.New("resource source overrides are not supported; use a separate group")
					}
					if err := checkExplicitFields(child); err != nil {
						return err
					}
				}
			} else if mappingValue(item, "source") != nil || mappingValue(item, "resources") != nil {
				return errors.New("packages are local resource declarations without source or resources")
			}
			if err := checkExplicitFields(item); err != nil {
				return err
			}
		}
	}
	return nil
}
func checkExplicitFields(n *yaml.Node) error {
	for _, key := range []string{"path", "type", "layout", "entry"} {
		if v := mappingValue(n, key); v != nil && (v.Kind != yaml.ScalarNode || v.Tag == "!!null" || strings.TrimSpace(v.Value) == "") {
			return fmt.Errorf("explicit %s must not be empty", key)
		}
	}
	if mappingValue(n, "target_path") != nil {
		return errors.New("target_path is not supported; local paths are derived")
	}
	return nil
}
func expandDeclaration(g dependencyGroup, d resourceDeclaration) (Resource, error) {
	r := Resource{Name: d.Name, Type: d.Type, Layout: d.Layout, Entry: d.Entry, Version: d.Version}
	if r.Type == "" {
		r.Type = g.Type
	}
	s := g.Source
	s.Files = append([]string(nil), s.Files...)
	r.Source = &s
	if s.NormalizedType() == "url" {
		if g.Path != nil || d.Path != nil {
			return r, errors.New("URL locations are defined by source.url/files; path is only supported for git/archive")
		}
	} else {
		p := "{name}"
		if g.Path != nil {
			p = *g.Path
		}
		if d.Path != nil {
			p = *d.Path
		} else {
			p = strings.ReplaceAll(p, "{name}", d.Name)
		}
		if strings.ContainsAny(p, "{}") {
			return r, fmt.Errorf("unsupported path placeholder in %q", p)
		}
		if err := validateUpstreamPath(p); err != nil {
			return r, err
		}
		s.Path = p
	}
	// Match the existing source-entry fallback without rewriting explicit metadata.
	if r.Entry == "" && s.Entry != "" {
		r.Entry = s.Entry
	}
	if r.Layout == "" {
		if s.NormalizedType() == "url" {
			if len(s.Files) > 0 {
				r.Layout = LayoutDir
			} else {
				r.Layout = LayoutFile
			}
		} else {
			r.Layout = InferLayout(s.Path)
		}
	}
	if r.Entry == "" {
		if r.Layout == LayoutFile {
			if s.NormalizedType() == "url" {
				u, err := url.Parse(s.URL)
				if err != nil {
					return r, err
				}
				r.Entry = path.Base(u.Path)
			} else {
				r.Entry = path.Base(s.Path)
			}
		} else {
			r.Entry = r.EffectiveEntry()
		}
	}
	if s.Entry == "" {
		s.Entry = r.Entry
	}
	var err error
	r.Path, err = DerivedResourcePath(r, "dependency")
	return r, err
}

func InferLayout(sourcePath string) string {
	if path.Ext(sourcePath) != "" {
		return LayoutFile
	}
	return LayoutDir
}
func validateUpstreamPath(p string) error {
	if strings.TrimSpace(p) == "" || strings.Contains(p, "\\") || path.IsAbs(p) || strings.Contains(p, ":") {
		return fmt.Errorf("invalid upstream path %q", p)
	}
	return validateRelativePath(p, "upstream path")
}
func validateLeaf(name string) error {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:{}") {
		return fmt.Errorf("invalid resource name or filename %q", name)
	}
	return nil
}

// DerivedResourcePath is shared by declarations and source discovery. It does
// not inspect the filesystem or change source metadata.
func DerivedResourcePath(r Resource, kind string) (string, error) {
	if err := validateLeaf(r.Name); err != nil {
		return "", err
	}
	leaf := r.Name
	if r.EffectiveLayout() == LayoutFile {
		if kind == "package" {
			leaf = r.EffectiveEntry()
		} else if r.Source != nil {
			if r.Source.NormalizedType() == "url" {
				u, err := url.Parse(r.Source.URL)
				if err != nil {
					return "", err
				}
				leaf = path.Base(u.Path)
			} else {
				leaf = path.Base(r.Source.Path)
			}
		} else {
			return "", fmt.Errorf("file dependency %q needs a source", r.Name)
		}
	}
	if err := validateLeaf(leaf); err != nil {
		return "", err
	}
	dir := "dependencies"
	if kind == "package" {
		dir = "packages"
	}
	return path.Join(".ctxpm", dir, TypeDir(r.Type), leaf), nil
}
func CheckDerivedPath(r Resource, kind string) error {
	expected, err := DerivedResourcePath(r, kind)
	if err != nil {
		return err
	}
	if filepath.ToSlash(r.Path) != expected {
		return fmt.Errorf("resource %q local path %q cannot be represented in schema 2.0 (derived path %q); keep schema 1.0 or organize the resource first", r.Name, r.Path, expected)
	}
	return nil
}
func validateTargets(m *Manifest) error {
	names, targets, links := map[string]string{}, map[string]string{}, map[string]string{}
	for _, set := range []struct {
		kind      string
		resources []Resource
	}{{"dependency", m.Dependencies}, {"package", m.Packages}} {
		for _, r := range set.resources {
			if err := CheckDerivedPath(r, set.kind); err != nil {
				return err
			}
			if previous, ok := names[r.Name]; ok {
				return fmt.Errorf("duplicate resource name %q (%s and %s)", r.Name, previous, set.kind)
			}
			names[r.Name] = set.kind
			target := strings.ToLower(filepath.ToSlash(filepath.Clean(r.Path)))
			if previous, ok := targets[target]; ok {
				return fmt.Errorf("resources %q and %q have the same local target", previous, r.Name)
			}
			targets[target] = r.Name
			// Detect file/dir ancestor conflicts as well as identical targets.
			for other, name := range targets {
				if other != target && (strings.HasPrefix(other, target+"/") || strings.HasPrefix(target, other+"/")) {
					return fmt.Errorf("resources %q and %q have overlapping targets", name, r.Name)
				}
			}
			for _, link := range DerivedCompatibilityPaths(m.Agents, r) {
				key := strings.ToLower(link)
				if previous, ok := links[key]; ok {
					return fmt.Errorf("resources %q and %q expose the same compatibility link %s", previous, r.Name, link)
				}
				links[key] = r.Name
			}
		}
	}
	return nil
}
func scalarValue(n *yaml.Node, key string) string {
	if v := mappingValue(n, key); v != nil {
		return v.Value
	}
	return ""
}
func strptr(s string) *string { return &s }

// Marshal preserves the original schema and group declarations. Full execution
// resources are reconciled with their original nodes, never serialized as v2.
func Marshal(m *Manifest) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if m.Version != GroupedManifestVersion {
		return marshalManifest(m)
	}
	return marshalGrouped(m)
}

func nodeOf(v any) *yaml.Node {
	var n yaml.Node
	if err := n.Encode(v); err != nil {
		panic(err)
	}
	return &n
}
func putNode(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			old := n.Content[i+1]
			value.HeadComment, value.LineComment, value.FootComment = old.HeadComment, old.LineComment, old.FootComment
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content, nodeOf(key), value)
}
func dropNode(n *yaml.Node, key string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}
func sameNode(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, _ := marshalYAML(a)
	y, _ := marshalYAML(b)
	return bytes.Equal(x, y)
}
func sameResource(a, b Resource) bool { return reflect.DeepEqual(a, b) }
