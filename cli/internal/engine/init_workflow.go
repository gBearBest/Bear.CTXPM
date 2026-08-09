package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

type initDiscovery struct {
	agent            string
	warnings         []string
	evidence         []string
	entrypointSource string
}

type initResourcePlan struct {
	agents            []string
	packages          []manifest.Resource
	dependencies      []manifest.Resource
	migrated          []string
	files             []string
	gitignoreRules    []string
	ownershipInferred []string
	unresolved        []string
	warnings          []string
}

type discoveredResource struct {
	kind              string
	resource          manifest.Resource
	original          string
	evidence          []string
	requiresMigration bool
}

type migrationDiscoveryPlan struct {
	candidates []discoveredResource
	unresolved []string
}

func detectInitAgent(root string, requested string, m *manifest.Manifest) initDiscovery {
	agent := strings.TrimSpace(requested)
	if agent != "" {
		return initDiscovery{
			agent:            agent,
			entrypointSource: "explicit --agent",
		}
	}
	if m != nil {
		if agent := detectAgentFromManifest(m); agent != "" {
			return initDiscovery{
				agent:            agent,
				evidence:         []string{fmt.Sprintf("detected existing manifest agent %q", agent)},
				entrypointSource: "ctxpm.yaml",
			}
		}
	}

	existing := map[string]string{}
	for _, candidate := range []struct {
		agent string
		file  string
	}{
		{agent: "claude-code", file: "CLAUDE.md"},
		{agent: "antigravity", file: "ANTIGRAVITY.md"},
		{agent: "generic", file: manifest.CanonicalEntrypointSourceFile()},
		{agent: "generic", file: "AGENTS.md"},
	} {
		if _, alreadySeen := existing[candidate.agent]; alreadySeen {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, candidate.file)); err == nil {
			existing[candidate.agent] = candidate.file
		}
	}
	if len(existing) == 1 {
		for agent, file := range existing {
			return initDiscovery{
				agent:            agent,
				evidence:         []string{fmt.Sprintf("detected existing root entrypoint %s", file)},
				entrypointSource: file,
			}
		}
	}
	if len(existing) > 1 {
		files := make([]string, 0, len(existing))
		for _, file := range existing {
			files = append(files, file)
		}
		sort.Strings(files)
		return initDiscovery{
			agent:            "generic",
			warnings:         []string{fmt.Sprintf("multiple existing root entrypoints were found (%s); merge any unique instructions into %s, then rerun `ctxpm entrypoint sync` so the other root entrypoint filenames can be converted into compatibility symlinks", strings.Join(files, ", "), manifest.CanonicalEntrypointFile())},
			evidence:         []string{fmt.Sprintf("detected multiple existing root entrypoints: %s", strings.Join(files, ", "))},
			entrypointSource: "ambiguous-root-entrypoints",
		}
	}
	return initDiscovery{
		agent:            "generic",
		evidence:         []string{"no existing entrypoint was found; defaulted to generic/AGENTS.md"},
		entrypointSource: "default",
	}
}

func detectAgentFromManifest(m *manifest.Manifest) string {
	if m == nil {
		return ""
	}
	if len(m.Entrypoints) == 1 {
		for agent := range m.Entrypoints {
			return agent
		}
	}
	if len(m.Agents) == 1 {
		return m.Agents[0]
	}
	return ""
}

func scanAndMigrateResources(root string, m *manifest.Manifest, projectName string, dryRun bool) (*initResourcePlan, error) {
	plan := &initResourcePlan{agents: m.Agents}
	topLevelEntries, err := listTopLevelEntries(root)
	if err != nil {
		return nil, err
	}
	projectHints := buildProjectHints(projectName, topLevelEntries)
	candidates, err := discoverInitResources(root, projectHints)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if candidate.requiresMigration {
			// Keep the original path as an additional explicit compatibility symlink
			// so the old location continues to resolve during the migration transition.
			derived := manifest.DerivedCompatibilityPaths(m.Agents, candidate.resource)
			candidate.resource.Compatibility = dedupe(append(derived, candidate.original))
		}
		if m.HasResource(candidate.resource.Name) {
			if candidate.requiresMigration {
				plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s (%s): resource name already exists in ctxpm.yaml", candidate.original, candidate.resource.Name))
			}
			continue
		}
		if dryRun {
			plan.recordCandidate(candidate, candidate.requiresMigration)
			continue
		}
		if candidate.requiresMigration {
			if resourcePathExists(root, candidate.resource.Path) {
				plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s: canonical path %s already exists", candidate.original, candidate.resource.Path))
				continue
			}
			if err := moveResourceToCanonical(root, candidate.original, candidate.resource.Path); err != nil {
				plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s: migration failed: %v", candidate.original, err))
				continue
			}
		} else if err := ensureResourcePresence(root, candidate.resource, ""); err != nil {
			plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s: canonical resource is invalid: %v", candidate.original, err))
			continue
		}
		if err := ensureCompatibility(root, m.Agents, candidate.resource); err != nil {
			plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s: compatibility setup failed: %v", candidate.original, err))
			continue
		}
		plan.recordCandidate(candidate, candidate.requiresMigration)
		plan.files = append(plan.files, filepath.Join(root, filepath.FromSlash(candidate.resource.Path)))
		for _, compat := range candidate.resource.Compatibility {
			plan.files = append(plan.files, filepath.Join(root, filepath.FromSlash(compat)))
		}
	}
	return plan, nil
}

func collectMigrationCandidates(root string, m *manifest.Manifest, projectName string) (*migrationDiscoveryPlan, error) {
	topLevelEntries, err := listTopLevelEntries(root)
	if err != nil {
		return nil, err
	}
	projectHints := buildProjectHints(projectName, topLevelEntries)
	candidates, err := discoverInitResources(root, projectHints)
	if err != nil {
		return nil, err
	}
	compatibilityCandidates, err := discoverCompatibilityResources(root, projectHints)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, compatibilityCandidates...)
	sortDiscoveredResources(candidates)
	candidates = dedupeDiscoveredResources(candidates)

	plan := &migrationDiscoveryPlan{}
	for _, candidate := range candidates {
		if !candidate.requiresMigration {
			continue
		}
		if m.HasResource(candidate.resource.Name) {
			plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s (%s): resource name already exists in ctxpm.yaml", candidate.original, candidate.resource.Name))
			continue
		}
		if resourcePathExists(root, candidate.resource.Path) {
			plan.unresolved = append(plan.unresolved, fmt.Sprintf("%s: canonical path %s already exists", candidate.original, candidate.resource.Path))
			continue
		}
		derived := manifest.DerivedCompatibilityPaths(m.Agents, candidate.resource)
		candidate.resource.Compatibility = dedupe(append(derived, candidate.original))
		plan.candidates = append(plan.candidates, candidate)
	}
	sortDiscoveredResources(plan.candidates)
	plan.unresolved = dedupe(plan.unresolved)
	return plan, nil
}

func (p *initResourcePlan) recordCandidate(candidate discoveredResource, migrated bool) {
	switch candidate.kind {
	case "dependency":
		p.dependencies = append(p.dependencies, candidate.resource)
	default:
		p.packages = append(p.packages, candidate.resource)
	}
	if migrated {
		p.migrated = append(p.migrated, candidate.original)
	}
	p.ownershipInferred = append(p.ownershipInferred, fmt.Sprintf("%s -> %s (%s)", candidate.original, candidate.kind, strings.Join(candidate.evidence, "; ")))
	p.gitignoreRules = append(p.gitignoreRules, compatibilityIgnoreRules(p.agents, candidate.resource)...)
}

func discoverInitResources(root string, projectHints []string) ([]discoveredResource, error) {
	candidates := []discoveredResource{}
	for relDir, resourceType := range map[string]string{
		"skills":   "skill",
		"rules":    "rule",
		"specs":    "spec",
		"prompts":  "prompt",
		"mcp":      "mcp",
		"memories": "memory",
	} {
		discovered, err := discoverTypedResourceDir(root, relDir, resourceType, true, projectHints, true, false)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, discovered...)
	}
	for relDir, resourceType := range map[string]string{
		"ai/skills":   "skill",
		"ai/rules":    "rule",
		"ai/specs":    "spec",
		"ai/prompts":  "prompt",
		"ai/mcp":      "mcp",
		"ai/memories": "memory",
	} {
		discovered, err := discoverTypedResourceDir(root, relDir, resourceType, true, projectHints, true, false)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, discovered...)
	}
	discoveredDocs, err := discoverDocsResources(root, projectHints)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, discoveredDocs...)
	discoveredCanonical, err := discoverCanonicalResources(root)
	if err != nil {
		return nil, err
	}
	candidates = append(candidates, discoveredCanonical...)
	sortDiscoveredResources(candidates)
	return dedupeDiscoveredResources(candidates), nil
}

func discoverCompatibilityResources(root string, projectHints []string) ([]discoveredResource, error) {
	candidates := []discoveredResource{}
	for _, relDir := range []string{".agents", ".claude", ".antigravity"} {
		for _, resourceType := range manifest.SupportedTypes() {
			discovered, err := discoverTypedResourceDir(root, filepath.ToSlash(filepath.Join(relDir, manifest.TypeDir(resourceType))), resourceType, true, projectHints, true, true)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, discovered...)
		}
	}
	return candidates, nil
}

func discoverTypedResourceDir(root, relDir, resourceType string, packageOwned bool, projectHints []string, requiresMigration bool, allowCompatibilityRoots bool) ([]discoveredResource, error) {
	absDir := filepath.Join(root, filepath.FromSlash(relDir))
	entries, err := os.ReadDir(absDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	discovered := []discoveredResource{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		entryRel := filepath.ToSlash(filepath.Join(relDir, entry.Name()))
		entryAbs := filepath.Join(absDir, entry.Name())
		if ignoredInitPath(entryRel) && !(allowCompatibilityRoots && compatibilityDiscoveryPath(entryRel)) {
			continue
		}
		info, err := os.Lstat(entryAbs)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		layout := manifest.LayoutFile
		resourcePath := canonicalPackagePath(resourceType, entryRel)
		entryName := entry.Name()
		if info.IsDir() {
			layout = manifest.LayoutDir
			entryName, err = detectDirectoryEntry(entryAbs, resourceType)
			if err != nil {
				return nil, err
			}
			if entryName == "" {
				continue
			}
		}
		resource := manifest.Resource{
			Name:   normalizeResourceName(entry.Name()),
			Type:   resourceType,
			Layout: layout,
			Path:   resourcePath,
			Entry:  entryName,
		}
		evidence := []string{fmt.Sprintf("discovered in project AI resource directory %s", relDir)}
		evidence = append(evidence, projectEvidence(entryAbs, projectHints)...)
		kind := "package"
		if !packageOwned {
			kind = "dependency"
		}
		discovered = append(discovered, discoveredResource{
			kind:              kind,
			resource:          resource,
			original:          entryRel,
			evidence:          dedupe(evidence),
			requiresMigration: requiresMigration,
		})
	}
	return discovered, nil
}

func discoverCanonicalResources(root string) ([]discoveredResource, error) {
	candidates := []discoveredResource{}
	for _, candidate := range []struct {
		kind string
		root string
	}{
		{kind: "package", root: ".ctxpm/packages"},
		{kind: "dependency", root: ".ctxpm/dependencies"},
	} {
		for _, resourceType := range manifest.SupportedTypes() {
			relDir := filepath.ToSlash(filepath.Join(candidate.root, manifest.TypeDir(resourceType)))
			discovered, err := discoverCanonicalResourceDir(root, relDir, resourceType, candidate.kind)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, discovered...)
		}
	}
	return candidates, nil
}

func discoverCanonicalResourceDir(root, relDir, resourceType, kind string) ([]discoveredResource, error) {
	absDir := filepath.Join(root, filepath.FromSlash(relDir))
	entries, err := os.ReadDir(absDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	discovered := []discoveredResource{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		entryRel := filepath.ToSlash(filepath.Join(relDir, entry.Name()))
		entryAbs := filepath.Join(absDir, entry.Name())
		info, err := os.Lstat(entryAbs)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		layout := manifest.LayoutFile
		entryName := entry.Name()
		if info.IsDir() {
			layout = manifest.LayoutDir
			entryName, err = detectDirectoryEntry(entryAbs, resourceType)
			if err != nil {
				return nil, err
			}
			if entryName == "" {
				continue
			}
		}
		resource := manifest.Resource{
			Name:   normalizeResourceName(entry.Name()),
			Type:   resourceType,
			Layout: layout,
			Path:   entryRel,
			Entry:  entryName,
		}
		discovered = append(discovered, discoveredResource{
			kind:              kind,
			resource:          resource,
			original:          entryRel,
			evidence:          []string{fmt.Sprintf("discovered existing canonical %s resource under %s", kind, relDir)},
			requiresMigration: false,
		})
	}
	return discovered, nil
}

func discoverDocsResources(root string, projectHints []string) ([]discoveredResource, error) {
	docsDir := filepath.Join(root, "docs")
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	discovered := []discoveredResource{}
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		resourceType := docsResourceType(entry.Name())
		if resourceType == "" {
			continue
		}
		entryRel := filepath.ToSlash(filepath.Join("docs", entry.Name()))
		entryAbs := filepath.Join(docsDir, entry.Name())
		evidence := projectEvidence(entryAbs, projectHints)
		if len(evidence) == 0 {
			continue
		}
		resource := manifest.Resource{
			Name:   normalizeResourceName(entry.Name()),
			Type:   resourceType,
			Layout: manifest.LayoutFile,
			Path:   canonicalPackagePath(resourceType, entryRel),
			Entry:  entry.Name(),
		}
		discovered = append(discovered, discoveredResource{
			kind:              "package",
			resource:          resource,
			original:          entryRel,
			evidence:          append([]string{"detected project-coupled AI document under docs/"}, evidence...),
			requiresMigration: true,
		})
	}
	return discovered, nil
}

func canonicalPackagePath(resourceType, originalRel string) string {
	base := filepath.Base(filepath.FromSlash(originalRel))
	absOriginal := filepath.ToSlash(originalRel)
	if strings.Count(absOriginal, "/") > 1 {
		parent := filepath.Base(filepath.Dir(absOriginal))
		if parent != manifest.TypeDir(resourceType) && parent != "ai" {
			base = normalizeResourceName(parent) + pathSuffix(base)
		}
	}
	return filepath.ToSlash(filepath.Join(".ctxpm", "packages", manifest.TypeDir(resourceType), base))
}

func normalizeResourceName(name string) string {
	normalized := strings.TrimSuffix(name, filepath.Ext(name))
	normalized = strings.ToLower(normalized)
	normalized = strings.ReplaceAll(normalized, " ", "-")
	normalized = strings.ReplaceAll(normalized, "_", "-")
	return normalized
}

func detectDirectoryEntry(root, resourceType string) (string, error) {
	defaults := []string{}
	switch resourceType {
	case "skill":
		defaults = append(defaults, "SKILL.md")
	case "rule":
		defaults = append(defaults, "RULE.md")
	case "spec":
		defaults = append(defaults, "SPEC.md")
	case "prompt":
		defaults = append(defaults, "PROMPT.md")
	case "memory":
		defaults = append(defaults, "MEMORY.md")
	}
	defaults = append(defaults, "README.md", "index.md")
	for _, candidate := range defaults {
		if _, err := os.Stat(filepath.Join(root, candidate)); err == nil {
			return candidate, nil
		}
	}
	files := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	if len(files) == 0 {
		return "", nil
	}
	return files[0], nil
}

func buildProjectHints(projectName string, topLevel []string) []string {
	hints := []string{projectName}
	hints = append(hints, topLevel...)
	return dedupe(hints)
}

func projectEvidence(path string, hints []string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	evidence := []string{}
	for _, hint := range hints {
		hint = strings.TrimSpace(hint)
		if hint == "" || len(hint) < 3 {
			continue
		}
		if strings.Contains(text, hint) {
			evidence = append(evidence, fmt.Sprintf("references project-specific token %q", hint))
		}
	}
	return evidence
}

func listTopLevelEntries(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	values := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		values = append(values, name)
	}
	sort.Strings(values)
	return values, nil
}

func compatibilityIgnoreRules(agents []string, resource manifest.Resource) []string {
	rules := []string{}
	for _, compat := range resource.EffectiveCompatibility(agents) {
		switch {
		case strings.HasPrefix(compat, ".agents/"), strings.HasPrefix(compat, ".claude/"), strings.HasPrefix(compat, ".antigravity/"):
			rules = append(rules, filepath.ToSlash(filepath.Dir(compat))+"/")
		default:
			rules = append(rules, compat)
		}
	}
	return dedupe(rules)
}

func moveResourceToCanonical(root, fromRel, toRel string) error {
	from := filepath.Join(root, filepath.FromSlash(fromRel))
	to := filepath.Join(root, filepath.FromSlash(toRel))
	if filepath.Clean(from) == filepath.Clean(to) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(to); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := replacePath(from, to); err != nil {
		return err
	}
	return os.RemoveAll(from)
}

func ignoredInitPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, ".ctxpm/") ||
		strings.HasPrefix(rel, ".agents/") ||
		strings.HasPrefix(rel, ".claude/") ||
		strings.HasPrefix(rel, ".antigravity/")
}

func compatibilityDiscoveryPath(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, ".agents/") ||
		strings.HasPrefix(rel, ".claude/") ||
		strings.HasPrefix(rel, ".antigravity/")
}

func resourcePathExists(root, resourcePath string) bool {
	_, err := os.Lstat(filepath.Join(root, filepath.FromSlash(resourcePath)))
	return err == nil
}

func docsResourceType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "skill"):
		return "skill"
	case strings.Contains(lower, "rule"):
		return "rule"
	case strings.Contains(lower, "spec"):
		return "spec"
	case strings.Contains(lower, "prompt"):
		return "prompt"
	case strings.Contains(lower, "mcp"):
		return "mcp"
	case strings.Contains(lower, "memory"):
		return "memory"
	default:
		return ""
	}
}

func pathSuffix(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	return ext
}

func sortDiscoveredResources(items []discoveredResource) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].kind != items[j].kind {
			return items[i].kind < items[j].kind
		}
		return items[i].original < items[j].original
	})
}

func dedupeDiscoveredResources(items []discoveredResource) []discoveredResource {
	seen := map[string]bool{}
	result := []discoveredResource{}
	for _, item := range items {
		key := item.kind + ":" + item.original
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, item)
	}
	return result
}
