package engine

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

type memoryResource struct {
	Kind     string
	Resource manifest.Resource
	Root     string
}

type memoryDocument struct {
	ResourceName string
	ResourceKind string
	RelativePath string
	AbsolutePath string
	Title        string
	Tags         []string
	Content      string
}

type MemorySearchOptions struct {
	Query    string
	Resource string
	Title    string
	Tag      string
	Path     string
}

type MemorySearchMatch struct {
	Resource string   `json:"resource"`
	Kind     string   `json:"kind"`
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Tags     []string `json:"tags,omitempty"`
	Snippet  string   `json:"snippet,omitempty"`
}

type MemorySearchResult struct {
	Status  string              `json:"status"`
	Matches []MemorySearchMatch `json:"matches"`
}

func (r MemorySearchResult) Text() string {
	if len(r.Matches) == 0 {
		return "Memory search status: no_matches\n"
	}
	lines := []string{"Memory search status: " + r.Status}
	for _, match := range r.Matches {
		line := fmt.Sprintf("- %s [%s] %s", match.Resource, match.Kind, match.Path)
		if match.Title != "" {
			line += " title=" + match.Title
		}
		if match.Snippet != "" {
			line += " snippet=" + strconvQuote(match.Snippet)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) MemorySearch(opts MemorySearchOptions) (*MemorySearchResult, error) {
	resources, err := a.loadMemoryResources()
	if err != nil {
		return nil, err
	}
	matches := []MemorySearchMatch{}
	for _, resource := range resources {
		if !memoryResourceFilter(resource, opts.Resource) {
			continue
		}
		docs, err := collectMemoryDocuments(resource)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			snippet, ok := matchMemoryDocument(doc, opts)
			if !ok {
				continue
			}
			matches = append(matches, MemorySearchMatch{
				Resource: doc.ResourceName,
				Kind:     doc.ResourceKind,
				Path:     doc.RelativePath,
				Title:    doc.Title,
				Tags:     append([]string(nil), doc.Tags...),
				Snippet:  snippet,
			})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Resource == matches[j].Resource {
			return matches[i].Path < matches[j].Path
		}
		return matches[i].Resource < matches[j].Resource
	})
	status := "matches_found"
	if len(matches) == 0 {
		status = "no_matches"
	}
	return &MemorySearchResult{Status: status, Matches: matches}, nil
}

type MemorySuggestOptions struct {
	Topic    string
	Summary  string
	Resource string
}

type MemorySuggestResult struct {
	Status       string `json:"status"`
	Resource     string `json:"resource,omitempty"`
	Action       string `json:"action,omitempty"`
	Title        string `json:"title,omitempty"`
	EntryPath    string `json:"entry_path,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Preview      string `json:"preview,omitempty"`
	Recommended  string `json:"recommended_resource,omitempty"`
	ResourceKind string `json:"resource_kind,omitempty"`
}

func (r MemorySuggestResult) Text() string {
	lines := []string{"Memory suggest status: " + r.Status}
	if r.Resource != "" {
		lines = append(lines, "Resource: "+r.Resource)
	}
	if r.Recommended != "" {
		lines = append(lines, "Recommended resource: "+r.Recommended)
	}
	if r.Action != "" {
		lines = append(lines, "Action: "+r.Action)
	}
	if r.Title != "" {
		lines = append(lines, "Title: "+r.Title)
	}
	if r.EntryPath != "" {
		lines = append(lines, "Entry path: "+r.EntryPath)
	}
	if r.Reason != "" {
		lines = append(lines, "Reason: "+r.Reason)
	}
	if r.Preview != "" {
		lines = append(lines, "", r.Preview)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) MemorySuggest(opts MemorySuggestOptions) (*MemorySuggestResult, error) {
	resources, err := a.loadMemoryResources()
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(strings.Join(filterEmptyStrings([]string{opts.Topic, opts.Summary}), " "))
	title := chooseMemoryTitle(opts.Topic, opts.Summary, "")
	slug := slugify(title)
	packages := filterWritableMemoryResources(resources)
	if len(packages) == 0 {
		name := suggestedMemoryResourceName(opts.Resource, opts.Topic, opts.Summary)
		return &MemorySuggestResult{
			Status:      "needs_resource",
			Action:      "create_resource",
			Title:       title,
			Reason:      "no writable project memory package exists yet",
			Recommended: name,
		}, nil
	}
	target, err := selectWritableMemoryResource(packages, opts.Resource, query)
	if err != nil {
		return nil, err
	}
	search, err := a.MemorySearch(MemorySearchOptions{
		Query:    query,
		Resource: target.Resource.Name,
	})
	if err != nil {
		return nil, err
	}
	action := "add_entry"
	reason := "no closely related memory entry was found in the selected project memory package"
	if len(search.Matches) > 0 {
		action = "review_existing"
		reason = "related memory entries already exist; review or extend them before writing a new entry"
	}
	entryPath := filepath.ToSlash(filepath.Join("entries", timestampSlug(time.Now().UTC(), slug)+".md"))
	preview := buildMemoryEntryContent(title, opts.Topic, opts.Summary)
	return &MemorySuggestResult{
		Status:       "suggested",
		Resource:     target.Resource.Name,
		ResourceKind: target.Kind,
		Action:       action,
		Title:        title,
		EntryPath:    entryPath,
		Reason:       reason,
		Preview:      preview,
	}, nil
}

type MemoryCaptureOptions struct {
	Topic    string
	Summary  string
	Resource string
	Title    string
	Write    bool
}

type MemoryCaptureResult struct {
	Status    string `json:"status"`
	Resource  string `json:"resource,omitempty"`
	Title     string `json:"title,omitempty"`
	EntryPath string `json:"entry_path,omitempty"`
	Preview   string `json:"preview,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Wrote     bool   `json:"wrote"`
}

func (r MemoryCaptureResult) Text() string {
	lines := []string{"Memory capture status: " + r.Status}
	if r.Resource != "" {
		lines = append(lines, "Resource: "+r.Resource)
	}
	if r.Title != "" {
		lines = append(lines, "Title: "+r.Title)
	}
	if r.EntryPath != "" {
		lines = append(lines, "Entry path: "+r.EntryPath)
	}
	if r.Reason != "" {
		lines = append(lines, "Reason: "+r.Reason)
	}
	if r.Preview != "" {
		lines = append(lines, "", r.Preview)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) MemoryCapture(opts MemoryCaptureOptions) (*MemoryCaptureResult, error) {
	suggestion, err := a.MemorySuggest(MemorySuggestOptions{
		Topic:    opts.Topic,
		Summary:  opts.Summary,
		Resource: opts.Resource,
	})
	if err != nil {
		return nil, err
	}
	title := chooseMemoryTitle(opts.Topic, opts.Summary, opts.Title)
	preview := buildMemoryEntryContent(title, opts.Topic, opts.Summary)
	if suggestion.Status != "suggested" {
		return &MemoryCaptureResult{
			Status:  suggestion.Status,
			Title:   title,
			Preview: preview,
			Reason:  suggestion.Reason,
		}, nil
	}
	entryPath := suggestion.EntryPath
	if !opts.Write {
		return &MemoryCaptureResult{
			Status:    "dry_run",
			Resource:  suggestion.Resource,
			Title:     title,
			EntryPath: entryPath,
			Preview:   preview,
			Reason:    "capture previews changes by default; rerun with --write to persist",
		}, nil
	}
	resources, err := a.loadMemoryResources()
	if err != nil {
		return nil, err
	}
	target, err := findMemoryResource(resources, suggestion.Resource, "package")
	if err != nil {
		return nil, err
	}
	if target.Resource.EffectiveLayout() != manifest.LayoutDir {
		return nil, fmt.Errorf("memory capture requires a directory-root memory package, but %q is a file resource", target.Resource.Name)
	}
	if strings.TrimSpace(opts.Summary) == "" {
		return nil, errors.New("memory capture requires --summary")
	}
	finalPath, err := writeMemoryEntry(*target, title, opts.Topic, opts.Summary, entryPath, preview)
	if err != nil {
		return nil, err
	}
	return &MemoryCaptureResult{
		Status:    "written",
		Resource:  target.Resource.Name,
		Title:     title,
		EntryPath: finalPath,
		Preview:   preview,
		Wrote:     true,
	}, nil
}

type MemoryPruneOptions struct {
	Resource string
	Archive  bool
}

type MemoryPruneCandidate struct {
	Resource string `json:"resource"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Reason   string `json:"reason"`
}

type MemoryPruneResult struct {
	Status     string                 `json:"status"`
	Candidates []MemoryPruneCandidate `json:"candidates"`
	Archived   []string               `json:"archived,omitempty"`
}

func (r MemoryPruneResult) Text() string {
	lines := []string{"Memory prune status: " + r.Status}
	for _, candidate := range r.Candidates {
		lines = append(lines, fmt.Sprintf("- %s [%s] %s reason=%s", candidate.Resource, candidate.Type, candidate.Path, candidate.Reason))
	}
	if len(r.Archived) > 0 {
		lines = append(lines, "Archived:")
		for _, archived := range r.Archived {
			lines = append(lines, "- "+archived)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) MemoryPrune(opts MemoryPruneOptions) (*MemoryPruneResult, error) {
	resources, err := a.loadMemoryResources()
	if err != nil {
		return nil, err
	}
	result := &MemoryPruneResult{Status: "clean"}
	for _, resource := range resources {
		if !memoryResourceFilter(resource, opts.Resource) {
			continue
		}
		candidates, err := inspectMemoryPruneCandidates(resource)
		if err != nil {
			return nil, err
		}
		result.Candidates = append(result.Candidates, candidates...)
		if opts.Archive {
			archived, err := archiveMemoryPruneCandidates(resource, candidates)
			if err != nil {
				return nil, err
			}
			result.Archived = append(result.Archived, archived...)
		}
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].Resource == result.Candidates[j].Resource {
			return result.Candidates[i].Path < result.Candidates[j].Path
		}
		return result.Candidates[i].Resource < result.Candidates[j].Resource
	})
	sort.Strings(result.Archived)
	switch {
	case len(result.Candidates) == 0:
		result.Status = "clean"
	case opts.Archive && len(result.Archived) > 0:
		result.Status = "archived"
	default:
		result.Status = "candidates_found"
	}
	return result, nil
}

func (a *App) loadMemoryResources() ([]memoryResource, error) {
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	resources := []memoryResource{}
	for _, dep := range m.Dependencies {
		if dep.Type != "memory" {
			continue
		}
		resources = append(resources, memoryResource{
			Kind:     "dependency",
			Resource: dep,
			Root:     filepath.Join(a.Root, filepath.FromSlash(dep.Path)),
		})
	}
	for _, pkg := range m.Packages {
		if pkg.Type != "memory" {
			continue
		}
		resources = append(resources, memoryResource{
			Kind:     "package",
			Resource: pkg,
			Root:     filepath.Join(a.Root, filepath.FromSlash(pkg.Path)),
		})
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Kind == resources[j].Kind {
			return resources[i].Resource.Name < resources[j].Resource.Name
		}
		return resources[i].Kind < resources[j].Kind
	})
	return resources, nil
}

func validateMemoryResource(localPath string, resource manifest.Resource) []string {
	if resource.Type != "memory" || resource.EffectiveLayout() != manifest.LayoutDir {
		return nil
	}
	issues := []string{}
	references, err := collectMemoryIndexReferences(localPath)
	if err != nil {
		return []string{fmt.Sprintf("memory resource %q index parsing failed: %v", resource.Name, err)}
	}
	for _, reference := range references {
		target := filepath.Join(localPath, filepath.FromSlash(reference))
		if _, err := os.Stat(target); err != nil {
			issues = append(issues, fmt.Sprintf("memory resource %q index reference %q is missing", resource.Name, reference))
		}
	}
	return dedupe(issues)
}

func collectMemoryDocuments(resource memoryResource) ([]memoryDocument, error) {
	if resource.Resource.EffectiveLayout() == manifest.LayoutFile {
		data, err := os.ReadFile(resource.Root)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(data) {
			return nil, nil
		}
		relativePath := filepath.Base(filepath.ToSlash(resource.Resource.Path))
		title, tags := extractMemoryMetadata(relativePath, string(data))
		return []memoryDocument{{
			ResourceName: resource.Resource.Name,
			ResourceKind: resource.Kind,
			RelativePath: relativePath,
			AbsolutePath: resource.Root,
			Title:        title,
			Tags:         tags,
			Content:      string(data),
		}}, nil
	}
	paths, err := memorySearchPaths(resource)
	if err != nil {
		return nil, err
	}
	docs := []memoryDocument{}
	for _, rel := range paths {
		if rel == "index.json" || rel == "index.jsonl" {
			continue
		}
		abs := filepath.Join(resource.Root, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		if !utf8.Valid(data) {
			continue
		}
		content := string(data)
		title, tags := extractMemoryMetadata(rel, content)
		docs = append(docs, memoryDocument{
			ResourceName: resource.Resource.Name,
			ResourceKind: resource.Kind,
			RelativePath: rel,
			AbsolutePath: abs,
			Title:        title,
			Tags:         tags,
			Content:      content,
		})
	}
	return docs, nil
}

func memorySearchPaths(resource memoryResource) ([]string, error) {
	paths, err := collectMemoryIndexReferences(resource.Root)
	if err != nil {
		return nil, err
	}
	all, err := walkMemoryFiles(resource.Root)
	if err != nil {
		return nil, err
	}
	return dedupe(append(paths, all...)), nil
}

func collectMemoryIndexReferences(root string) ([]string, error) {
	refs := []string{}
	for _, name := range []string{"index.json", "index.jsonl"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if strings.HasSuffix(name, ".jsonl") {
			scanner := bufio.NewScanner(strings.NewReader(string(data)))
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				var payload any
				if err := json.Unmarshal([]byte(line), &payload); err != nil {
					return nil, err
				}
				refs = append(refs, collectRelativeReferences(payload)...)
			}
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			continue
		}
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			return nil, err
		}
		refs = append(refs, collectRelativeReferences(payload)...)
	}
	return dedupe(refs), nil
}

func collectRelativeReferences(value any) []string {
	refs := []string{}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(strings.TrimSpace(key))
			if ref, ok := child.(string); ok && isReferenceKey(lower) {
				if normalized := normalizeRelativeReference(ref); normalized != "" {
					refs = append(refs, normalized)
				}
			}
			refs = append(refs, collectRelativeReferences(child)...)
		}
	case []any:
		for _, child := range typed {
			refs = append(refs, collectRelativeReferences(child)...)
		}
	}
	return refs
}

func isReferenceKey(key string) bool {
	switch key {
	case "path", "file", "target", "ref", "entry":
		return true
	default:
		return false
	}
}

func normalizeRelativeReference(value string) string {
	trimmed := filepath.ToSlash(strings.TrimSpace(value))
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, "://") {
		return ""
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == ".." {
			return ""
		}
	}
	return trimmed
}

func walkMemoryFiles(root string) ([]string, error) {
	paths := []string{}
	err := filepath.Walk(root, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			name := info.Name()
			if name == ".git" || name == ".ctxpm" || name == "archive" {
				if current == root {
					return nil
				}
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func extractMemoryMetadata(relativePath, content string) (string, []string) {
	title := ""
	tags := []string{}
	body := content
	if strings.HasPrefix(body, "---\n") {
		end := strings.Index(body[4:], "\n---\n")
		if end >= 0 {
			frontmatter := body[4 : end+4]
			body = body[end+9:]
			title, tags = parseFrontmatterMemoryMetadata(frontmatter)
		}
	}
	if title == "" {
		for _, line := range strings.Split(body, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				title = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
				break
			}
		}
	}
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(relativePath), filepath.Ext(relativePath))
	}
	return title, dedupe(tags)
}

func parseFrontmatterMemoryMetadata(frontmatter string) (string, []string) {
	title := ""
	tags := []string{}
	lines := strings.Split(frontmatter, "\n")
	collectingTags := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "title:") {
			title = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "title:")), `"'`)
			collectingTags = false
			continue
		}
		if strings.HasPrefix(trimmed, "tags:") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "tags:"))
			if rest == "" {
				collectingTags = true
				continue
			}
			for _, part := range strings.Split(rest, ",") {
				tag := strings.Trim(strings.TrimSpace(part), `"'[]`)
				if tag != "" {
					tags = append(tags, tag)
				}
			}
			collectingTags = false
			continue
		}
		if collectingTags && strings.HasPrefix(trimmed, "-") {
			tag := strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "-")), `"'`)
			if tag != "" {
				tags = append(tags, tag)
			}
			continue
		}
		collectingTags = false
	}
	return title, tags
}

func matchMemoryDocument(doc memoryDocument, opts MemorySearchOptions) (string, bool) {
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	titleFilter := strings.ToLower(strings.TrimSpace(opts.Title))
	tagFilter := strings.ToLower(strings.TrimSpace(opts.Tag))
	pathFilter := strings.ToLower(strings.TrimSpace(opts.Path))
	if titleFilter != "" && !strings.Contains(strings.ToLower(doc.Title), titleFilter) {
		return "", false
	}
	if tagFilter != "" && !containsTag(doc.Tags, tagFilter) {
		return "", false
	}
	if pathFilter != "" && !strings.Contains(strings.ToLower(doc.RelativePath), pathFilter) {
		return "", false
	}
	if query == "" {
		return firstMeaningfulLine(doc.Content), true
	}
	searchable := []string{
		strings.ToLower(doc.ResourceName),
		strings.ToLower(doc.Title),
		strings.ToLower(doc.RelativePath),
		strings.ToLower(strings.Join(doc.Tags, " ")),
		strings.ToLower(doc.Content),
	}
	for _, candidate := range searchable {
		if strings.Contains(candidate, query) {
			return matchingLine(doc.Content, query), true
		}
	}
	return "", false
}

func containsTag(tags []string, want string) bool {
	for _, tag := range tags {
		lower := strings.ToLower(strings.TrimSpace(tag))
		if lower == want || strings.Contains(lower, want) {
			return true
		}
	}
	return false
}

func matchingLine(content, query string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), query) {
			return trimmed
		}
	}
	return firstMeaningfulLine(content)
}

func firstMeaningfulLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func memoryResourceFilter(resource memoryResource, filter string) bool {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		return true
	}
	name := strings.ToLower(resource.Resource.Name)
	return name == filter || strings.Contains(name, filter)
}

func filterWritableMemoryResources(resources []memoryResource) []memoryResource {
	filtered := []memoryResource{}
	for _, resource := range resources {
		if resource.Kind == "package" && resource.Resource.EffectiveLayout() == manifest.LayoutDir {
			filtered = append(filtered, resource)
		}
	}
	return filtered
}

func selectWritableMemoryResource(resources []memoryResource, requested, query string) (*memoryResource, error) {
	if requested != "" {
		return findMemoryResource(resources, requested, "package")
	}
	query = strings.ToLower(strings.TrimSpace(query))
	for i := range resources {
		if query == "" {
			break
		}
		if strings.Contains(strings.ToLower(resources[i].Resource.Name), query) {
			return &resources[i], nil
		}
	}
	if len(resources) == 0 {
		return nil, errors.New("no writable project memory package exists")
	}
	return &resources[0], nil
}

func findMemoryResource(resources []memoryResource, name, kind string) (*memoryResource, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	for i := range resources {
		if kind != "" && resources[i].Kind != kind {
			continue
		}
		if strings.ToLower(resources[i].Resource.Name) == normalized {
			return &resources[i], nil
		}
	}
	if kind == "" {
		return nil, fmt.Errorf("memory resource %q was not found", name)
	}
	return nil, fmt.Errorf("%s memory resource %q was not found", kind, name)
}

func chooseMemoryTitle(topic, summary, explicit string) string {
	if strings.TrimSpace(explicit) != "" {
		return strings.TrimSpace(explicit)
	}
	if strings.TrimSpace(topic) != "" {
		return strings.TrimSpace(topic)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return "Memory Entry"
	}
	line := summary
	if index := strings.Index(line, "\n"); index >= 0 {
		line = line[:index]
	}
	runes := []rune(strings.TrimSpace(line))
	if len(runes) > 72 {
		runes = runes[:72]
	}
	return strings.TrimSpace(string(runes))
}

func suggestedMemoryResourceName(requested, topic, summary string) string {
	if strings.TrimSpace(requested) != "" {
		return slugify(requested)
	}
	if strings.TrimSpace(topic) != "" {
		return slugify(topic)
	}
	if strings.TrimSpace(summary) != "" {
		return slugify(chooseMemoryTitle("", summary, ""))
	}
	return "project-memory"
}

func buildMemoryEntryContent(title, topic, summary string) string {
	lines := []string{
		"# " + strings.TrimSpace(title),
		"",
		fmt.Sprintf("Captured at: %s", time.Now().UTC().Format(time.RFC3339)),
	}
	if strings.TrimSpace(topic) != "" {
		lines = append(lines, "Topic: "+strings.TrimSpace(topic))
	}
	lines = append(lines, "", strings.TrimSpace(summary), "")
	return strings.Join(lines, "\n")
}

func writeMemoryEntry(resource memoryResource, title, topic, summary, suggestedPath, preview string) (string, error) {
	entriesDir := filepath.Join(resource.Root, "entries")
	if err := os.MkdirAll(entriesDir, 0o755); err != nil {
		return "", err
	}
	relativePath := filepath.ToSlash(suggestedPath)
	relativePath = ensureUniqueRelativePath(resource.Root, relativePath)
	absolutePath := filepath.Join(resource.Root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(absolutePath, []byte(preview), 0o644); err != nil {
		return "", err
	}
	memoryPath := filepath.Join(resource.Root, filepath.FromSlash(resource.Resource.EffectiveEntry()))
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		return "", err
	}
	linkLine := fmt.Sprintf("- [%s](%s)", title, relativePath)
	content := string(data)
	if !strings.Contains(content, linkLine) {
		content = appendMemoryIndexLink(content, linkLine)
		if err := os.WriteFile(memoryPath, []byte(content), 0o644); err != nil {
			return "", err
		}
	}
	return relativePath, nil
}

func ensureUniqueRelativePath(root, relativePath string) string {
	candidate := relativePath
	ext := filepath.Ext(relativePath)
	base := strings.TrimSuffix(relativePath, ext)
	index := 2
	for {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(candidate))); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d%s", base, index, ext)
		index++
	}
}

func appendMemoryIndexLink(content, linkLine string) string {
	if strings.Contains(content, "\n## Entries\n") {
		return strings.TrimRight(content, "\n") + "\n" + linkLine + "\n"
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return "## Entries\n\n" + linkLine + "\n"
	}
	return trimmed + "\n\n## Entries\n\n" + linkLine + "\n"
}

func inspectMemoryPruneCandidates(resource memoryResource) ([]MemoryPruneCandidate, error) {
	if resource.Resource.EffectiveLayout() != manifest.LayoutDir {
		return nil, nil
	}
	paths, err := walkMemoryFiles(resource.Root)
	if err != nil {
		return nil, err
	}
	candidates := []MemoryPruneCandidate{}
	seenHashes := map[string]string{}
	entry := resource.Resource.EffectiveEntry()
	for _, rel := range paths {
		if rel == entry || strings.HasPrefix(rel, "archive/") {
			continue
		}
		abs := filepath.Join(resource.Root, filepath.FromSlash(rel))
		data, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(data)) == "" {
			candidates = append(candidates, MemoryPruneCandidate{
				Resource: resource.Resource.Name,
				Path:     rel,
				Type:     "empty_file",
				Reason:   "file contains no meaningful content",
			})
			continue
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		if original, ok := seenHashes[hash]; ok {
			candidates = append(candidates, MemoryPruneCandidate{
				Resource: resource.Resource.Name,
				Path:     rel,
				Type:     "duplicate_content",
				Reason:   "content duplicates " + original,
			})
			continue
		}
		seenHashes[hash] = rel
	}
	references, err := collectMemoryIndexReferences(resource.Root)
	if err != nil {
		return nil, err
	}
	for _, ref := range references {
		if _, err := os.Stat(filepath.Join(resource.Root, filepath.FromSlash(ref))); err != nil {
			candidates = append(candidates, MemoryPruneCandidate{
				Resource: resource.Resource.Name,
				Path:     ref,
				Type:     "broken_reference",
				Reason:   "index references a missing file",
			})
		}
	}
	return dedupePruneCandidates(candidates), nil
}

func archiveMemoryPruneCandidates(resource memoryResource, candidates []MemoryPruneCandidate) ([]string, error) {
	if resource.Kind != "package" || resource.Resource.EffectiveLayout() != manifest.LayoutDir {
		return nil, nil
	}
	archived := []string{}
	archiveRoot := filepath.Join(resource.Root, "archive", time.Now().UTC().Format("20060102-150405"))
	for _, candidate := range candidates {
		if candidate.Type != "empty_file" && candidate.Type != "duplicate_content" {
			continue
		}
		source := filepath.Join(resource.Root, filepath.FromSlash(candidate.Path))
		if _, err := os.Stat(source); err != nil {
			continue
		}
		target := filepath.Join(archiveRoot, filepath.FromSlash(candidate.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.Rename(source, target); err != nil {
			return nil, err
		}
		archived = append(archived, filepath.ToSlash(filepath.Join("archive", filepath.Base(archiveRoot), candidate.Path)))
	}
	return archived, nil
}

func dedupePruneCandidates(values []MemoryPruneCandidate) []MemoryPruneCandidate {
	seen := map[string]bool{}
	result := []MemoryPruneCandidate{}
	for _, value := range values {
		key := value.Resource + ":" + value.Path + ":" + value.Type
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	builder := strings.Builder{}
	lastHyphen := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastHyphen = false
			continue
		}
		if !lastHyphen {
			builder.WriteByte('-')
			lastHyphen = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "memory-entry"
	}
	return result
}

func timestampSlug(now time.Time, slug string) string {
	return now.UTC().Format("20060102-150405") + "-" + slug
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
