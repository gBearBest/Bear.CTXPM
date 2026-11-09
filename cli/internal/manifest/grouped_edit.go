package manifest

import (
	"fmt"
	"path"
	"reflect"
	"strings"

	"gopkg.in/yaml.v3"
)

func sharedSource(s Source) Source { s.Path = ""; return s }
func sourceEqual(a, b Source) bool {
	a, b = sharedSource(a), sharedSource(b)
	if len(a.Files) == 0 {
		a.Files = nil
	}
	if len(b.Files) == 0 {
		b.Files = nil
	}
	return reflect.DeepEqual(a, b)
}
func sourceMatchesGroup(g dependencyGroup, r Resource) bool {
	if r.Source == nil {
		return false
	}
	s := g.Source
	if s.Entry == "" {
		s.Entry = r.EffectiveEntry()
	}
	return sourceEqual(s, *r.Source)
}
func decodeGroup(n *yaml.Node) (dependencyGroup, error) {
	var g dependencyGroup
	err := n.Decode(&g)
	return g, err
}
func suggestedTemplate(r Resource) string {
	p := r.Source.Path
	base := path.Base(p)
	ext := path.Ext(base)
	if base == r.Name {
		return strings.TrimSuffix(p, base) + "{name}"
	}
	if strings.TrimSuffix(base, ext) == r.Name {
		return strings.TrimSuffix(p, base) + "{name}" + ext
	}
	return p
}
func groupFor(r Resource) (dependencyGroup, error) {
	if r.Source == nil {
		return dependencyGroup{}, fmt.Errorf("dependency %q has no source; keep schema 1.0 until its source is known", r.Name)
	}
	g := dependencyGroup{Source: sharedSource(*r.Source), Type: r.Type}
	// source.entry is redundant when the normal entry rule reproduces it.
	defaultEntry := ""
	if r.EffectiveLayout() == LayoutFile {
		defaultEntry = path.Base(r.Path)
	} else if r.Type == "skill" {
		defaultEntry = "SKILL.md"
	} else if r.Type == "memory" {
		defaultEntry = "MEMORY.md"
	}
	if g.Source.Entry == defaultEntry {
		g.Source.Entry = ""
	}
	if r.Source.NormalizedType() != "url" {
		g.Path = strptr(suggestedTemplate(r))
	}
	return g, nil
}
func declarationFor(r Resource, g dependencyGroup) resourceDeclaration {
	d := resourceDeclaration{Name: r.Name, Version: r.Version}
	if r.Type != g.Type {
		d.Type = r.Type
	}
	if r.Source != nil && r.Source.NormalizedType() != "url" {
		template := "{name}"
		if g.Path != nil {
			template = *g.Path
		}
		if strings.ReplaceAll(template, "{name}", r.Name) != r.Source.Path {
			d.Path = strptr(r.Source.Path)
		}
	}
	inferred := LayoutDir
	if r.Source != nil {
		if r.Source.NormalizedType() == "url" {
			if len(r.Source.Files) == 0 {
				inferred = LayoutFile
			}
		} else {
			inferred = InferLayout(r.Source.Path)
		}
	}
	if r.EffectiveLayout() != inferred {
		d.Layout = r.EffectiveLayout()
	}
	defaultEntry := ""
	if r.Source != nil {
		defaultEntry = r.Source.Entry
	}
	if defaultEntry == "" {
		if r.EffectiveLayout() == LayoutFile {
			defaultEntry = path.Base(r.Path)
		} else if r.Type == "skill" {
			defaultEntry = "SKILL.md"
		} else if r.Type == "memory" {
			defaultEntry = "MEMORY.md"
		}
	}
	if r.EffectiveEntry() != defaultEntry {
		d.Entry = r.EffectiveEntry()
	}
	return d
}
func packageDeclaration(r Resource) resourceDeclaration {
	d := resourceDeclaration{Name: r.Name, Type: r.Type, Version: r.Version}
	if r.EffectiveLayout() == LayoutFile {
		d.Layout = LayoutFile
		d.Entry = r.EffectiveEntry()
	} else {
		defaultEntry := ""
		if r.Type == "skill" {
			defaultEntry = "SKILL.md"
		}
		if r.Type == "memory" {
			defaultEntry = "MEMORY.md"
		}
		if r.EffectiveEntry() != defaultEntry {
			d.Entry = r.EffectiveEntry()
		}
	}
	return d
}
func updateDeclaration(n *yaml.Node, d resourceDeclaration) {
	desired := nodeOf(d)
	for _, key := range []string{"name", "type", "path", "layout", "entry", "version"} {
		v := mappingValue(desired, key)
		if v == nil {
			dropNode(n, key)
		} else if !sameNode(mappingValue(n, key), v) {
			putNode(n, key, v)
		}
	}
}
func marshalGrouped(m *Manifest) ([]byte, error) {
	var doc yaml.Node
	var original *Manifest
	if len(m.original) > 0 {
		var err error
		original, err = Parse(m.original)
		if err != nil {
			return nil, err
		}
		if original.Version != GroupedManifestVersion {
			return nil, fmt.Errorf("explicit format conversion is required")
		}
		if err := yaml.Unmarshal(m.original, &doc); err != nil {
			return nil, err
		}
	} else {
		doc = *nodeOf(groupedDocument{Version: GroupedManifestVersion, Project: m.Project, Agents: m.Agents, UpdatePolicy: m.UpdatePolicy, Dependencies: []dependencyGroup{}, Packages: []resourceDeclaration{}})
	}
	root := &doc
	if root.Kind == yaml.DocumentNode {
		root = root.Content[0]
	}
	for _, pair := range []struct {
		key   string
		value any
	}{{"version", m.Version}, {"project", m.Project}, {"agents", m.Agents}, {"update_policy", m.UpdatePolicy}} {
		desired := nodeOf(pair.value)
		old := mappingValue(root, pair.key)
		if old == nil && ((pair.key == "agents" && len(m.Agents) == 0) || (pair.key == "update_policy" && m.UpdatePolicy.Enabled == nil && m.UpdatePolicy.Interval == "" && m.UpdatePolicy.IncludeSelf == nil)) {
			continue
		}
		if old != nil {
			var a, b any
			if old.Decode(&a) == nil && desired.Decode(&b) == nil && reflect.DeepEqual(a, b) {
				continue
			}
		}
		if old != nil && old.Kind == yaml.MappingNode && desired.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(desired.Content); i += 2 {
				if !sameNodeValue(mappingValue(old, desired.Content[i].Value), desired.Content[i+1]) {
					putNode(old, desired.Content[i].Value, desired.Content[i+1])
				}
			}
		} else if !sameNode(old, desired) {
			putNode(root, pair.key, desired)
		}
	}
	deps := mappingValue(root, "dependencies")
	pkgs := mappingValue(root, "packages")
	oldResources := map[string]Resource{}
	if original != nil {
		for _, r := range append(append([]Resource(nil), original.Dependencies...), original.Packages...) {
			oldResources[r.Name] = r
		}
	}
	desired := map[string]Resource{}
	for _, r := range m.Dependencies {
		desired[r.Name] = r
	}
	present := map[string]bool{}
	var groups []*yaml.Node
	for _, group := range deps.Content {
		g, err := decodeGroup(group)
		if err != nil {
			return nil, err
		}
		resources := mappingValue(group, "resources")
		var kept []*yaml.Node
		for _, item := range resources.Content {
			name := scalarValue(item, "name")
			r, ok := desired[name]
			if !ok {
				continue
			}
			if r.Source == nil {
				return nil, fmt.Errorf("dependency %q has no source", name)
			}
			if !sourceMatchesGroup(g, r) {
				continue
			}
			if !sameResource(oldResources[name], r) {
				updateDeclaration(item, declarationFor(r, g))
			}
			kept = append(kept, item)
			present[name] = true
		}
		resources.Content = kept
		if len(kept) > 0 {
			groups = append(groups, group)
		}
	}
	deps.Content = groups
	for _, r := range m.Dependencies {
		if present[r.Name] {
			continue
		}
		if err := CheckDerivedPath(r, "dependency"); err != nil {
			return nil, err
		}
		if r.Source == nil {
			return nil, fmt.Errorf("dependency %q has no source", r.Name)
		}
		var destination *yaml.Node
		var selected dependencyGroup
		for _, n := range deps.Content {
			g, err := decodeGroup(n)
			if err != nil {
				return nil, err
			}
			if g.Source.NormalizedType() != "url" && sourceMatchesGroup(g, r) {
				destination = n
				selected = g
				break
			}
		}
		if destination == nil {
			g, err := groupFor(r)
			if err != nil {
				return nil, err
			}
			selected = g
			g.Resources = []resourceDeclaration{}
			destination = nodeOf(g)
			deps.Content = append(deps.Content, destination)
		}
		resources := mappingValue(destination, "resources")
		resources.Style = 0
		resources.Content = append(resources.Content, nodeOf(declarationFor(r, selected)))
	}
	desired = map[string]Resource{}
	for _, r := range m.Packages {
		desired[r.Name] = r
	}
	present = map[string]bool{}
	var packages []*yaml.Node
	for _, item := range pkgs.Content {
		name := scalarValue(item, "name")
		r, ok := desired[name]
		if !ok {
			continue
		}
		if !sameResource(oldResources[name], r) {
			updateDeclaration(item, packageDeclaration(r))
		}
		packages = append(packages, item)
		present[name] = true
	}
	pkgs.Content = packages
	for _, r := range m.Packages {
		if !present[r.Name] {
			pkgs.Content = append(pkgs.Content, nodeOf(packageDeclaration(r)))
		}
	}
	var data []byte
	var err error
	if len(m.original) == 0 {
		data, err = marshalYAML(root)
	} else {
		data, err = renderDocumentChanges(m.original, root)
	}
	if err != nil {
		return nil, err
	}
	parsed, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := EquivalentResources(m, parsed); err != nil {
		return nil, fmt.Errorf("cannot save grouped manifest: %w", err)
	}
	return data, nil
}

// EquivalentResources compares execution semantics; omitted defaults have the same meaning.
func EquivalentResources(a, b *Manifest) error {
	normalize := func(r Resource) Resource {
		r.Layout = r.EffectiveLayout()
		r.Entry = r.EffectiveEntry()
		r.Compatibility = nil
		if r.Source != nil {
			s := *r.Source
			if len(s.Files) == 0 {
				s.Files = nil
			}
			r.Source = &s
		}
		return r
	}
	for _, set := range []struct {
		kind string
		a, b []Resource
	}{{"dependency", a.Dependencies, b.Dependencies}, {"package", a.Packages, b.Packages}} {
		if len(set.a) != len(set.b) {
			return fmt.Errorf("%s resource count changed", set.kind)
		}
		byName := map[string]Resource{}
		for _, r := range set.b {
			byName[r.Name] = normalize(r)
		}
		for _, r := range set.a {
			if !reflect.DeepEqual(normalize(r), byName[r.Name]) {
				return fmt.Errorf("%s %q would change its source, path, layout, entry or version", set.kind, r.Name)
			}
		}
	}
	return nil
}

type lineChange struct {
	start, end int
	lines      []string
}

func nodeLines(n *yaml.Node, sequence bool, indent int) ([]string, error) {
	var v any = n
	if sequence {
		v = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{n}}
	}
	data, err := marshalYAML(v)
	if err != nil {
		return nil, err
	}
	lines := newManifestText(data).lines
	for i := range lines {
		lines[i] = strings.Repeat(" ", indent) + lines[i]
	}
	return lines, nil
}

// Preserve untouched top-level fields and untouched groups byte for byte.
func renderDocumentChanges(raw []byte, next *yaml.Node) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	old := doc.Content[0]
	text := newManifestText(raw)
	var changes []lineChange
	for i := 0; i+1 < len(old.Content); i += 2 {
		key, prev := old.Content[i], old.Content[i+1]
		n := mappingValue(next, key.Value)
		if sameNode(prev, n) {
			continue
		}
		start := key.Line - 1
		end := len(text.lines)
		if i+2 < len(old.Content) {
			end = old.Content[i+2].Line - 1
		}
		suffix := end
		for suffix > start+1 && isBlankOrCommentLine(text.lines[suffix-1]) {
			suffix--
		}
		if (key.Value == "dependencies" || key.Value == "packages") && prev.Kind == yaml.SequenceNode && n != nil && n.Kind == yaml.SequenceNode && (len(prev.Content) == 0 || (prev.Style != yaml.FlowStyle && prev.Content[0].Line > key.Line)) {
			lines, err := renderSequence(text.lines, start, suffix, prev, n)
			if err != nil {
				return nil, err
			}
			changes = append(changes, lineChange{start, suffix, lines})
		} else {
			renderKey := *key
			renderKey.HeadComment = "" // Preceding source lines remain outside this replacement.
			field := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{&renderKey, n}}
			lines, err := nodeLines(field, false, 0)
			if err != nil {
				return nil, err
			}
			changes = append(changes, lineChange{start, suffix, lines})
		}
	}
	for i := 0; i+1 < len(next.Content); i += 2 {
		if mappingValue(old, next.Content[i].Value) == nil {
			lines, err := nodeLines(&yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: next.Content[i : i+2]}, false, 0)
			if err != nil {
				return nil, err
			}
			changes = append(changes, lineChange{len(text.lines), len(text.lines), lines})
		}
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		text.lines = replaceLineRange(text.lines, c.start, c.end, c.lines)
	}
	return []byte(text.String()), nil
}
func renderSequence(lines []string, start, end int, old, next *yaml.Node) ([]string, error) {
	header := lines[start]
	if len(next.Content) == 0 {
		v, err := setSectionLineValue(header, "[]")
		return []string{v}, err
	}
	header, err := setSectionLineValue(header, "")
	if err != nil {
		return nil, err
	}
	out := []string{header}
	oldByLine := map[int]*yaml.Node{}
	starts := map[int]int{}
	ends := map[int]int{}
	for i, n := range old.Content {
		s := n.Line - 1
		for s > start+1 && isCommentLine(lines[s-1]) {
			s--
		}
		e := end
		if i+1 < len(old.Content) {
			e = old.Content[i+1].Line - 1
			for e > s+1 && isCommentLine(lines[e-1]) {
				e--
			}
		}
		oldByLine[n.Line] = n
		starts[n.Line] = s
		ends[n.Line] = e
	}
	if len(old.Content) > 0 {
		out = append(out, lines[start+1:starts[old.Content[0].Line]]...)
	}
	for _, n := range next.Content {
		prev := oldByLine[n.Line]
		if prev != nil && sameNode(prev, n) {
			out = append(out, lines[starts[n.Line]:ends[n.Line]]...)
			continue
		}
		rendered, err := nodeLines(n, true, 2)
		if err != nil {
			return nil, err
		}
		out = append(out, rendered...)
	}
	return out, nil
}

// ConvertGrouped is pure: no downloads, file moves or version resolution.
func ConvertGrouped(m *Manifest) ([]byte, error) {
	if m.Version == GroupedManifestVersion {
		return append([]byte(nil), m.original...), nil
	}
	converted := *m
	converted.Version = GroupedManifestVersion
	converted.original = nil
	if err := converted.Validate(); err != nil {
		return nil, err
	}
	data, err := Marshal(&converted)
	if err != nil {
		return nil, err
	}
	parsed, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err = EquivalentResources(m, parsed); err != nil {
		return nil, err
	}
	if len(m.original) > 0 {
		return mergeConvertedDocument(m.original, data)
	}
	return data, nil
}
func mergeConvertedDocument(raw, converted []byte) ([]byte, error) {
	var a, b yaml.Node
	if err := yaml.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(converted, &b); err != nil {
		return nil, err
	}
	old, next := a.Content[0], b.Content[0]
	for _, section := range []string{"dependencies", "packages"} {
		originals := map[string]*yaml.Node{}
		if previous := mappingValue(old, section); previous != nil {
			for _, item := range previous.Content {
				originals[scalarValue(item, "name")] = item
			}
		}
		var items []*yaml.Node
		if section == "dependencies" {
			for _, group := range mappingValue(next, section).Content {
				items = append(items, mappingValue(group, "resources").Content...)
			}
		} else {
			items = mappingValue(next, section).Content
		}
		for _, item := range items {
			prev := originals[scalarValue(item, "name")]
			if prev == nil {
				continue
			}
			item.HeadComment, item.FootComment = prev.HeadComment, prev.FootComment
			known := map[string]bool{"name": true, "type": true, "path": true, "source": true, "entry": true, "layout": true, "version": true, "compatibility": true}
			for i := 0; i+1 < len(prev.Content); i += 2 {
				key, value := prev.Content[i], prev.Content[i+1]
				if !known[key.Value] {
					item.Content = append(item.Content, key, value)
				} else if dest := mappingValue(item, key.Value); dest != nil {
					dest.HeadComment, dest.LineComment, dest.FootComment = value.HeadComment, value.LineComment, value.FootComment
				}
			}
			if src := mappingValue(prev, "source"); src != nil {
				for i := 0; i+1 < len(src.Content); i += 2 {
					switch src.Content[i].Value {
					case "type", "url", "path", "ref", "entry", "files":
					default:
						return nil, fmt.Errorf("resource %q has unknown source field %q; migrate it explicitly first", scalarValue(item, "name"), src.Content[i].Value)
					}
				}
			}
		}
	}
	putNode(old, "version", nodeOf(GroupedManifestVersion))
	putNode(old, "dependencies", mappingValue(next, "dependencies"))
	putNode(old, "packages", mappingValue(next, "packages"))
	return renderDocumentChanges(raw, old)
}

func sameNodeValue(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	var x, y any
	return a.Decode(&x) == nil && b.Decode(&y) == nil && reflect.DeepEqual(x, y)
}

// A missing version in a flow mapping and block scalars cannot be changed by
// the line editor. Re-render only the affected groups in those cases.
func groupedVersionFallback(raw []byte, root *yaml.Node, versions map[string]string) ([]byte, bool, error) {
	var selected []*yaml.Node
	needsRender := false
	visit := func(item *yaml.Node) {
		next := versions[scalarValue(item, "name")]
		previous := mappingValue(item, "version")
		if strings.TrimSpace(next) == "" || (previous != nil && previous.Value == next) {
			return
		}
		selected = append(selected, item)
		if previous == nil {
			needsRender = needsRender || item.Style&yaml.FlowStyle != 0
		} else if previous.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.Contains(previous.Value, "\n") {
			needsRender = true
		}
	}
	for _, group := range mappingValue(root, "dependencies").Content {
		for _, item := range mappingValue(group, "resources").Content {
			visit(item)
		}
	}
	for _, item := range mappingValue(root, "packages").Content {
		visit(item)
	}
	if !needsRender {
		return nil, false, nil
	}
	for _, item := range selected {
		putNode(item, "version", nodeOf(versions[scalarValue(item, "name")]))
	}
	data, err := renderDocumentChanges(raw, root)
	if err == nil {
		_, err = Parse(data)
	}
	return data, true, err
}
