package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const manifestIndent = 2

const (
	ManifestVersion1 = 1
	ManifestVersion2 = 2

	LayoutFile = "file"
	LayoutDir  = "dir"

	VersionPrefixSHA256     = "sha256:"
	VersionPrefixSHA256Tree = "sha256tree:"
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
	Layout        string   `yaml:"layout,omitempty"`
	Path          string   `yaml:"path"`
	Entry         string   `yaml:"entry,omitempty"`
	Source        *Source  `yaml:"source,omitempty"`
	Version       string   `yaml:"version,omitempty"`
	Compatibility []string `yaml:"compatibility,omitempty"`
}

type Source struct {
	Type  string   `yaml:"type"`
	URL   string   `yaml:"url"`
	Path  string   `yaml:"path,omitempty"`
	Ref   string   `yaml:"ref,omitempty"`
	Entry string   `yaml:"entry,omitempty"`
	Files []string `yaml:"files,omitempty"`
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
	data, err := marshalManifest(m)
	if err != nil {
		return "", err
	}
	return manifestPath, os.WriteFile(manifestPath, data, 0o644)
}

func marshalManifest(m *Manifest) ([]byte, error) {
	return marshalYAML(m)
}

func UpdateResourceVersions(root string, versions map[string]string) (bool, error) {
	if len(versions) == 0 {
		return false, nil
	}

	manifestPath := filepath.Join(root, "ctxpm.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, ErrNotFound
		}
		return false, err
	}

	text := newManifestText(data)
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, err
	}
	if len(doc.Content) == 0 {
		return false, errors.New("ctxpm manifest is empty")
	}

	edits, err := versionEdits(doc.Content[0], text.lines, versions)
	if err != nil {
		return false, err
	}
	if len(edits) == 0 {
		return false, nil
	}

	sort.SliceStable(edits, func(i, j int) bool {
		if edits[i].lineIndex == edits[j].lineIndex {
			return edits[i].kind > edits[j].kind
		}
		return edits[i].lineIndex > edits[j].lineIndex
	})
	for _, edit := range edits {
		switch edit.kind {
		case manifestEditReplace:
			updatedLine, err := replaceScalarOnLine(text.lines[edit.lineIndex], edit.column, edit.value)
			if err != nil {
				return false, err
			}
			text.lines[edit.lineIndex] = updatedLine
		case manifestEditInsertBefore:
			text.lines = insertLine(text.lines, edit.lineIndex, edit.value)
		case manifestEditInsertAfter:
			text.lines = insertLine(text.lines, edit.lineIndex+1, edit.value)
		default:
			return false, fmt.Errorf("unsupported manifest edit kind %q", edit.kind)
		}
	}

	return true, os.WriteFile(manifestPath, []byte(text.String()), 0o644)
}

func AddDependency(root string, resource Resource) (bool, error) {
	return addResource(root, "dependencies", resource, true)
}

func RemoveDependency(root, name string) (bool, error) {
	return removeResource(root, "dependencies", name)
}

func RemovePackage(root, name string) (bool, error) {
	return removeResource(root, "packages", name)
}

func (m *Manifest) Validate() error {
	if m.Version == 0 {
		m.Version = ManifestVersion2
	}
	if m.Version != ManifestVersion1 && m.Version != ManifestVersion2 {
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
		if err := dep.validate(m.Version, "dependency"); err != nil {
			return err
		}
	}
	for _, pkg := range m.Packages {
		if err := pkg.validate(m.Version, "package"); err != nil {
			return err
		}
	}
	return nil
}

func (r Resource) Validate(kind string) error {
	return r.validate(ManifestVersion1, kind)
}

func (r Resource) validate(version int, kind string) error {
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
	if version >= ManifestVersion2 {
		if r.Layout != LayoutFile && r.Layout != LayoutDir {
			return fmt.Errorf("%s %q has unsupported layout %q", kind, r.Name, r.Layout)
		}
		if err := validateRelativePath(r.Entry, fmt.Sprintf("%s %q entry", kind, r.Name)); err != nil {
			return err
		}
		switch r.Layout {
		case LayoutFile:
			if filepath.Base(filepath.FromSlash(r.Path)) != filepath.Base(filepath.FromSlash(r.Entry)) {
				return fmt.Errorf("%s %q entry %q must match file path %q", kind, r.Name, r.Entry, r.Path)
			}
		case LayoutDir:
			if pathIsFileLike(r.Entry) == false && strings.TrimSpace(r.Entry) == "" {
				return fmt.Errorf("%s %q directory resources require entry", kind, r.Name)
			}
		}
	}
	if r.Source != nil {
		if err := r.Source.validate(version, kind, r); err != nil {
			return err
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
	case "archive":
		return "archive"
	default:
		return s.Type
	}
}

func (r Resource) EffectiveLayout() string {
	if strings.TrimSpace(r.Layout) != "" {
		return r.Layout
	}
	if r.Source != nil {
		if len(r.Source.Files) > 0 {
			return LayoutDir
		}
		if strings.TrimSpace(r.Source.Path) != "" && !pathIsFileLike(r.Source.Path) {
			return LayoutDir
		}
		if strings.TrimSpace(r.Source.Path) != "" && pathIsFileLike(r.Source.Path) {
			return LayoutFile
		}
	}
	if pathIsFileLike(r.Path) {
		return LayoutFile
	}
	return LayoutDir
}

func (r Resource) EffectiveEntry() string {
	if strings.TrimSpace(r.Entry) != "" {
		return filepath.ToSlash(strings.TrimSpace(r.Entry))
	}
	switch r.EffectiveLayout() {
	case LayoutFile:
		return filepath.Base(filepath.ToSlash(r.Path))
	case LayoutDir:
		if r.Type == "skill" {
			return "SKILL.md"
		}
	}
	return ""
}

func (r Resource) EntryPath() string {
	entry := r.EffectiveEntry()
	switch r.EffectiveLayout() {
	case LayoutDir:
		return filepath.ToSlash(filepath.Join(r.Path, entry))
	case LayoutFile:
		return filepath.ToSlash(r.Path)
	default:
		return filepath.ToSlash(r.Path)
	}
}

func (s Source) validate(version int, kind string, resource Resource) error {
	sourceType := s.NormalizedType()
	switch sourceType {
	case "git":
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("%s %q git source is missing url", kind, resource.Name)
		}
		if strings.TrimSpace(s.Path) == "" {
			return fmt.Errorf("%s %q git source is missing path", kind, resource.Name)
		}
		if version >= ManifestVersion2 {
			if err := validateRelativePath(s.Path, fmt.Sprintf("%s %q git source.path", kind, resource.Name)); err != nil {
				return err
			}
			if err := validateRelativePath(s.Entry, fmt.Sprintf("%s %q git source.entry", kind, resource.Name)); err != nil {
				return err
			}
		}
	case "url":
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("%s %q url source is missing url", kind, resource.Name)
		}
		if version == ManifestVersion1 && strings.TrimSpace(s.Entry) == "" {
			return fmt.Errorf("%s %q url source is missing entry", kind, resource.Name)
		}
		if version >= ManifestVersion2 {
			if err := validateRelativePath(s.Entry, fmt.Sprintf("%s %q url source.entry", kind, resource.Name)); err != nil {
				return err
			}
			if len(s.Files) > 0 {
				if resource.EffectiveLayout() != LayoutDir {
					return fmt.Errorf("%s %q url source.files requires layout dir", kind, resource.Name)
				}
				seen := map[string]bool{}
				for _, file := range s.Files {
					if err := validateRelativePath(file, fmt.Sprintf("%s %q url source.files entry", kind, resource.Name)); err != nil {
						return err
					}
					if seen[file] {
						return fmt.Errorf("%s %q url source.files contains duplicate %q", kind, resource.Name, file)
					}
					seen[file] = true
				}
				if !seen[s.Entry] {
					return fmt.Errorf("%s %q url source.entry %q must be listed in source.files", kind, resource.Name, s.Entry)
				}
			} else if resource.EffectiveLayout() != LayoutFile {
				return fmt.Errorf("%s %q single-file url source requires layout file", kind, resource.Name)
			}
		}
	case "archive":
		if version < ManifestVersion2 {
			return fmt.Errorf("%s %q uses unsupported source.type %q", kind, resource.Name, s.Type)
		}
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("%s %q archive source is missing url", kind, resource.Name)
		}
		if strings.TrimSpace(s.Path) == "" {
			return fmt.Errorf("%s %q archive source is missing path", kind, resource.Name)
		}
		if err := validateRelativePath(s.Path, fmt.Sprintf("%s %q archive source.path", kind, resource.Name)); err != nil {
			return err
		}
		if err := validateRelativePath(s.Entry, fmt.Sprintf("%s %q archive source.entry", kind, resource.Name)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s %q has unsupported source.type %q", kind, resource.Name, s.Type)
	}
	return nil
}

func validateRelativePath(value, label string) error {
	trimmed := filepath.ToSlash(strings.TrimSpace(value))
	if trimmed == "" {
		return fmt.Errorf("%s is required", label)
	}
	if strings.HasPrefix(trimmed, "/") {
		return fmt.Errorf("%s %q must be relative", label, value)
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == ".." {
			return fmt.Errorf("%s %q must not contain '..'", label, value)
		}
	}
	return nil
}

func pathIsFileLike(value string) bool {
	base := filepath.Base(filepath.ToSlash(strings.TrimSpace(value)))
	return strings.Contains(base, ".")
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
	return strings.Replace(managedEntrypointTemplate, "<agent-id>", agent, 1)
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

type manifestText struct {
	lines           []string
	newline         string
	trailingNewline bool
}

func newManifestText(data []byte) manifestText {
	raw := string(data)
	newline := "\n"
	if strings.Contains(raw, "\r\n") {
		newline = "\r\n"
		raw = strings.ReplaceAll(raw, "\r\n", "\n")
	}
	trailingNewline := strings.HasSuffix(raw, "\n")
	lines := strings.Split(raw, "\n")
	if trailingNewline {
		lines = lines[:len(lines)-1]
	}
	return manifestText{
		lines:           lines,
		newline:         newline,
		trailingNewline: trailingNewline,
	}
}

func (m manifestText) String() string {
	if len(m.lines) == 0 {
		if m.trailingNewline {
			return m.newline
		}
		return ""
	}
	joined := strings.Join(m.lines, m.newline)
	if m.trailingNewline {
		return joined + m.newline
	}
	return joined
}

const (
	manifestEditReplace      = "replace"
	manifestEditInsertBefore = "insert_before"
	manifestEditInsertAfter  = "insert_after"
)

type resourceVersionEdit struct {
	kind      string
	lineIndex int
	column    int
	value     string
}

type resourceBlock struct {
	name  string
	lines []string
}

type resourceSection struct {
	key         string
	keyLine     int
	endLine     int
	inlineEmpty bool
	itemIndent  string
	prefix      []string
	suffix      []string
	items       []resourceBlock
}

func versionEdits(root *yaml.Node, lines []string, versions map[string]string) ([]resourceVersionEdit, error) {
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("ctxpm manifest root must be a mapping")
	}

	edits := []resourceVersionEdit{}
	targets := []string{"dependencies", "packages"}
	for _, target := range targets {
		sequence := mappingValue(root, target)
		if sequence == nil || sequence.Kind != yaml.SequenceNode {
			continue
		}
		for _, item := range sequence.Content {
			if item.Kind != yaml.MappingNode {
				continue
			}
			nameValue := mappingValue(item, "name")
			if nameValue == nil {
				continue
			}
			nextVersion, ok := versions[nameValue.Value]
			if !ok || strings.TrimSpace(nextVersion) == "" {
				continue
			}

			versionValue := mappingValue(item, "version")
			if versionValue != nil {
				if versionValue.Value == nextVersion {
					continue
				}
				edits = append(edits, resourceVersionEdit{
					kind:      manifestEditReplace,
					lineIndex: versionValue.Line - 1,
					column:    versionValue.Column,
					value:     nextVersion,
				})
				continue
			}

			insertLineIndex, insertKind, err := versionInsertionPoint(item)
			if err != nil {
				return nil, err
			}
			indent, err := versionFieldIndent(item, lines)
			if err != nil {
				return nil, err
			}
			edits = append(edits, resourceVersionEdit{
				kind:      insertKind,
				lineIndex: insertLineIndex,
				value:     indent + "version: " + nextVersion,
			})
		}
	}

	return edits, nil
}

func addResource(root, section string, resource Resource, sortByName bool) (bool, error) {
	manifestPath, text, rootNode, err := loadManifestDocument(root)
	if err != nil {
		return false, err
	}
	resourceSection, err := parseResourceSection(rootNode, text.lines, section)
	if err != nil {
		return false, err
	}
	blockLines, err := marshalResourceBlock(resource, resourceSection.itemIndent)
	if err != nil {
		return false, err
	}
	blocks := make([]resourceBlock, 0, len(resourceSection.items)+1)
	blocks = append(blocks, cloneBlocks(resourceSection.items)...)
	blocks = append(blocks, resourceBlock{name: resource.Name, lines: blockLines})
	if sortByName {
		sort.SliceStable(blocks, func(i, j int) bool {
			return blocks[i].name < blocks[j].name
		})
	}
	updatedLines, err := rewriteResourceSection(text.lines, *resourceSection, blocks)
	if err != nil {
		return false, err
	}
	if equalLines(text.lines, updatedLines) {
		return false, nil
	}
	text.lines = updatedLines
	return true, os.WriteFile(manifestPath, []byte(text.String()), 0o644)
}

func removeResource(root, section, name string) (bool, error) {
	manifestPath, text, rootNode, err := loadManifestDocument(root)
	if err != nil {
		return false, err
	}
	resourceSection, err := parseResourceSection(rootNode, text.lines, section)
	if err != nil {
		return false, err
	}
	filtered := make([]resourceBlock, 0, len(resourceSection.items))
	removed := false
	for _, block := range resourceSection.items {
		if block.name == name {
			removed = true
			continue
		}
		filtered = append(filtered, cloneBlock(block))
	}
	if !removed {
		return false, nil
	}
	updatedLines, err := rewriteResourceSection(text.lines, *resourceSection, filtered)
	if err != nil {
		return false, err
	}
	if equalLines(text.lines, updatedLines) {
		return false, nil
	}
	text.lines = updatedLines
	return true, os.WriteFile(manifestPath, []byte(text.String()), 0o644)
}

func loadManifestDocument(root string) (string, manifestText, *yaml.Node, error) {
	manifestPath := filepath.Join(root, "ctxpm.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", manifestText{}, nil, ErrNotFound
		}
		return "", manifestText{}, nil, err
	}
	text := newManifestText(data)
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", manifestText{}, nil, err
	}
	if len(doc.Content) == 0 {
		return "", manifestText{}, nil, errors.New("ctxpm manifest is empty")
	}
	return manifestPath, text, doc.Content[0], nil
}

func parseResourceSection(root *yaml.Node, lines []string, section string) (*resourceSection, error) {
	if root.Kind != yaml.MappingNode {
		return nil, errors.New("ctxpm manifest root must be a mapping")
	}
	entryIndex := -1
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == section {
			entryIndex = i
			break
		}
	}
	if entryIndex < 0 {
		return nil, fmt.Errorf("%s section was not found in ctxpm.yaml", section)
	}

	keyNode := root.Content[entryIndex]
	valueNode := root.Content[entryIndex+1]
	endLine := len(lines)
	if entryIndex+2 < len(root.Content) {
		endLine = root.Content[entryIndex+2].Line - 1
	}
	resourceSection := &resourceSection{
		key:         section,
		keyLine:     keyNode.Line - 1,
		endLine:     endLine,
		inlineEmpty: sectionLineHasInlineEmpty(lines[keyNode.Line-1]),
		itemIndent:  strings.Repeat(" ", keyNode.Column-1+manifestIndent),
	}
	if valueNode.Kind != yaml.SequenceNode {
		return resourceSection, nil
	}
	if len(valueNode.Content) == 0 {
		return resourceSection, nil
	}

	rawStarts := make([]int, len(valueNode.Content))
	itemNames := make([]string, len(valueNode.Content))
	for i, item := range valueNode.Content {
		if item.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%s entries must be mappings", section)
		}
		nameValue := mappingValue(item, "name")
		if nameValue == nil || strings.TrimSpace(nameValue.Value) == "" {
			return nil, fmt.Errorf("%s entry is missing name", section)
		}
		rawStarts[i] = item.Line - 1
		itemNames[i] = nameValue.Value
	}

	if len(rawStarts) > 0 {
		resourceSection.itemIndent = strings.Repeat(" ", leadingSpaceCount(lines[rawStarts[0]]))
	}
	suffixStart := endLine
	for suffixStart > rawStarts[len(rawStarts)-1] && isBlankOrCommentLine(lines[suffixStart-1]) {
		suffixStart--
	}

	starts := make([]int, len(rawStarts))
	for i, rawStart := range rawStarts {
		start := rawStart
		commentStart := start
		lowerBound := resourceSection.keyLine + 1
		if i > 0 {
			lowerBound = rawStarts[i-1] + 1
		}
		for commentStart > lowerBound && isCommentLine(lines[commentStart-1]) {
			commentStart--
		}
		if commentStart < start {
			for commentStart > lowerBound && isBlankLine(lines[commentStart-1]) {
				commentStart--
			}
		}
		starts[i] = commentStart
	}

	resourceSection.prefix = cloneLines(lines[resourceSection.keyLine+1 : starts[0]])
	for i, name := range itemNames {
		end := suffixStart
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		resourceSection.items = append(resourceSection.items, resourceBlock{
			name:  name,
			lines: cloneLines(lines[starts[i]:end]),
		})
	}
	resourceSection.suffix = cloneLines(lines[suffixStart:endLine])
	return resourceSection, nil
}

func rewriteResourceSection(lines []string, section resourceSection, blocks []resourceBlock) ([]string, error) {
	replacement := make([]string, 0, len(lines))
	keyLine := lines[section.keyLine]
	if len(blocks) == 0 {
		updatedKeyLine, err := setSectionLineValue(keyLine, "[]")
		if err != nil {
			return nil, err
		}
		replacement = append(replacement, updatedKeyLine)
		replacement = append(replacement, cloneLines(section.suffix)...)
		return replaceLineRange(lines, section.keyLine, section.endLine, replacement), nil
	}

	if section.inlineEmpty {
		updatedKeyLine, err := setSectionLineValue(keyLine, "")
		if err != nil {
			return nil, err
		}
		keyLine = updatedKeyLine
	}
	replacement = append(replacement, keyLine)
	replacement = append(replacement, cloneLines(section.prefix)...)
	for i, block := range blocks {
		blockLines := cloneLines(block.lines)
		if i == 0 {
			blockLines = trimLeadingBlankLines(blockLines)
		}
		replacement = append(replacement, blockLines...)
	}
	replacement = append(replacement, cloneLines(section.suffix)...)
	return replaceLineRange(lines, section.keyLine, section.endLine, replacement), nil
}

func marshalResourceBlock(resource Resource, indent string) ([]string, error) {
	data, err := marshalYAML([]Resource{resource})
	if err != nil {
		return nil, err
	}
	text := newManifestText(data)
	lines := cloneLines(text.lines)
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return lines, nil
}

func marshalYAML(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(manifestIndent)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func mappingKey(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i]
		}
	}
	return nil
}

func versionInsertionPoint(resource *yaml.Node) (int, string, error) {
	if compatKey := mappingKey(resource, "compatibility"); compatKey != nil {
		return compatKey.Line - 1, manifestEditInsertBefore, nil
	}

	for _, anchor := range []string{"source", "path"} {
		if value := mappingValue(resource, anchor); value != nil {
			return maxNodeLine(value) - 1, manifestEditInsertAfter, nil
		}
	}

	if len(resource.Content) < 2 {
		return 0, "", errors.New("resource mapping is missing fields")
	}
	return maxNodeLine(resource.Content[len(resource.Content)-1]) - 1, manifestEditInsertAfter, nil
}

func versionFieldIndent(resource *yaml.Node, lines []string) (string, error) {
	nameKey := mappingKey(resource, "name")
	if nameKey == nil || nameKey.Line <= 0 || nameKey.Line > len(lines) {
		return "", errors.New("resource mapping is missing a valid name key position")
	}
	if nameKey.Column <= 1 {
		return "", errors.New("resource mapping is missing a valid name key column")
	}
	return strings.Repeat(" ", nameKey.Column-1), nil
}

func maxNodeLine(node *yaml.Node) int {
	if node == nil {
		return 0
	}
	maxLine := node.Line
	for _, child := range node.Content {
		if childLine := maxNodeLine(child); childLine > maxLine {
			maxLine = childLine
		}
	}
	return maxLine
}

func replaceScalarOnLine(line string, column int, value string) (string, error) {
	start, err := runeColumnToByteIndex(line, column)
	if err != nil {
		return "", err
	}
	if start >= len(line) {
		return "", fmt.Errorf("column %d is out of range for line %q", column, line)
	}

	segment := line[start:]
	switch segment[0] {
	case '\'':
		end, err := singleQuotedScalarEnd(segment)
		if err != nil {
			return "", err
		}
		return line[:start] + "'" + strings.ReplaceAll(value, "'", "''") + "'" + segment[end:], nil
	case '"':
		end, err := doubleQuotedScalarEnd(segment)
		if err != nil {
			return "", err
		}
		return line[:start] + strconv.Quote(value) + segment[end:], nil
	default:
		end := 0
		for end < len(segment) {
			r, size := utf8.DecodeRuneInString(segment[end:])
			if unicode.IsSpace(r) || r == '#' {
				break
			}
			end += size
		}
		return line[:start] + value + segment[end:], nil
	}
}

func runeColumnToByteIndex(line string, column int) (int, error) {
	if column <= 0 {
		return 0, fmt.Errorf("column must be positive, got %d", column)
	}
	current := 1
	for index := range line {
		if current == column {
			return index, nil
		}
		current++
	}
	if current == column {
		return len(line), nil
	}
	return 0, fmt.Errorf("column %d exceeds line width", column)
}

func singleQuotedScalarEnd(segment string) (int, error) {
	for i := 1; i < len(segment); i++ {
		if segment[i] != '\'' {
			continue
		}
		if i+1 < len(segment) && segment[i+1] == '\'' {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, errors.New("unterminated single-quoted scalar")
}

func doubleQuotedScalarEnd(segment string) (int, error) {
	escaped := false
	for i := 1; i < len(segment); i++ {
		switch segment[i] {
		case '\\':
			escaped = !escaped
		case '"':
			if !escaped {
				return i + 1, nil
			}
			escaped = false
		default:
			escaped = false
		}
	}
	return 0, errors.New("unterminated double-quoted scalar")
}

func insertLine(lines []string, index int, value string) []string {
	if index < 0 {
		index = 0
	}
	if index > len(lines) {
		index = len(lines)
	}
	lines = append(lines, "")
	copy(lines[index+1:], lines[index:])
	lines[index] = value
	return lines
}

func replaceLineRange(lines []string, start, end int, replacement []string) []string {
	if start < 0 {
		start = 0
	}
	if end < start {
		end = start
	}
	if end > len(lines) {
		end = len(lines)
	}
	result := make([]string, 0, len(lines)-(end-start)+len(replacement))
	result = append(result, lines[:start]...)
	result = append(result, replacement...)
	result = append(result, lines[end:]...)
	return result
}

func setSectionLineValue(line, value string) (string, error) {
	commentIndex := findLineCommentIndex(line)
	content := strings.TrimRight(line, " \t")
	comment := ""
	if commentIndex >= 0 {
		content = strings.TrimRight(line[:commentIndex], " \t")
		comment = line[commentIndex:]
	}
	colonIndex := strings.Index(content, ":")
	if colonIndex < 0 {
		return "", fmt.Errorf("section line %q is missing ':'", line)
	}
	prefix := strings.TrimRight(content[:colonIndex+1], " \t")
	if value != "" {
		prefix += " " + value
	}
	if comment != "" {
		prefix += " " + comment
	}
	return prefix, nil
}

func findLineCommentIndex(line string) int {
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false
	for i, r := range line {
		switch r {
		case '\\':
			if inDoubleQuote {
				escaped = !escaped
			}
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
			escaped = false
		case '"':
			if !inSingleQuote && !escaped {
				inDoubleQuote = !inDoubleQuote
			}
			escaped = false
		case '#':
			if !inSingleQuote && !inDoubleQuote {
				return i
			}
			escaped = false
		default:
			escaped = false
		}
	}
	return -1
}

func sectionLineHasInlineEmpty(line string) bool {
	commentIndex := findLineCommentIndex(line)
	if commentIndex >= 0 {
		line = line[:commentIndex]
	}
	colonIndex := strings.Index(line, ":")
	if colonIndex < 0 {
		return false
	}
	return strings.TrimSpace(line[colonIndex+1:]) == "[]"
}

func leadingSpaceCount(line string) int {
	count := 0
	for count < len(line) && line[count] == ' ' {
		count++
	}
	return count
}

func isBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

func isCommentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#")
}

func isBlankOrCommentLine(line string) bool {
	return isBlankLine(line) || isCommentLine(line)
}

func trimLeadingBlankLines(lines []string) []string {
	index := 0
	for index < len(lines) && isBlankLine(lines[index]) {
		index++
	}
	return cloneLines(lines[index:])
}

func cloneLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	cloned := make([]string, len(lines))
	copy(cloned, lines)
	return cloned
}

func cloneBlock(block resourceBlock) resourceBlock {
	return resourceBlock{
		name:  block.name,
		lines: cloneLines(block.lines),
	}
}

func cloneBlocks(blocks []resourceBlock) []resourceBlock {
	if len(blocks) == 0 {
		return nil
	}
	cloned := make([]resourceBlock, len(blocks))
	for i, block := range blocks {
		cloned[i] = cloneBlock(block)
	}
	return cloned
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
