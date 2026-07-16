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

type EntrypointSyncResult struct {
	Status string   `json:"status"`
	Files  []string `json:"files,omitempty"`
}

func (r EntrypointSyncResult) Text() string {
	lines := []string{"Entrypoint sync status: " + r.Status}
	for _, item := range r.Files {
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n") + "\n"
}

type EntrypointDoctorResult struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues,omitempty"`
}

func (r EntrypointDoctorResult) Text() string {
	if r.OK {
		return "Entrypoint doctor status: ok\n"
	}
	lines := []string{"Entrypoint doctor status: failed"}
	for _, issue := range r.Issues {
		lines = append(lines, "- "+issue)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) EntrypointSync() (*EntrypointSyncResult, error) {
	if err := ensureManifestVersion(a.Root, false); err != nil {
		return nil, err
	}
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	changed := normalizeSharedEntrypoints(m)
	if changed {
		if _, err := manifest.Save(a.Root, m); err != nil {
			return nil, err
		}
	}
	files, err := syncManagedEntrypoints(a.Root, m, false)
	if err != nil {
		return nil, err
	}
	gitignoreRules := dedupe(append([]string{".ctxpm/dependencies/", ".ctxpm/state/"}, entrypointGitignoreRules(m)...))
	if _, err := ensureGitignoreRules(filepath.Join(a.Root, ".gitignore"), gitignoreRules); err != nil {
		return nil, err
	}
	files = append(files, filepath.Join(a.Root, ".gitignore"))
	if changed {
		files = append(files, filepath.Join(a.Root, "ctxpm.yaml"))
	}
	return &EntrypointSyncResult{Status: "applied", Files: dedupe(files)}, nil
}

func (a *App) EntrypointDoctor() (*EntrypointDoctorResult, error) {
	if err := ensureManifestVersion(a.Root, false); err != nil {
		return nil, err
	}
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	issues := validateManagedEntrypoints(a.Root, m)
	return &EntrypointDoctorResult{OK: len(issues) == 0, Issues: issues}, nil
}

func normalizeSharedEntrypoints(m *manifest.Manifest) bool {
	if m == nil {
		return false
	}
	changed := false
	if m.Entrypoints == nil {
		m.Entrypoints = map[string]manifest.Entrypoint{}
	}
	delete(m.Entrypoints, "")

	agents := map[string]bool{}
	for _, agent := range m.Agents {
		if trimmed := strings.TrimSpace(agent); trimmed != "" {
			agents[trimmed] = true
		}
	}
	for agent := range m.Entrypoints {
		if trimmed := strings.TrimSpace(agent); trimmed != "" {
			agents[trimmed] = true
		}
	}

	if len(agents) == 0 {
		return changed
	}

	keys := make([]string, 0, len(agents))
	for agent := range agents {
		keys = append(keys, agent)
	}
	sort.Strings(keys)
	for _, agent := range keys {
		m.Agents = appendIfMissing(m.Agents, agent)
		want := manifest.Entrypoint{File: manifest.CanonicalEntrypointFile(), Mode: "managed"}
		if current, ok := m.Entrypoints[agent]; !ok || current.File != want.File || current.EffectiveMode() != want.Mode {
			m.Entrypoints[agent] = want
			changed = true
		}
	}
	return changed
}

func syncManagedEntrypoints(root string, m *manifest.Manifest, allowRepair bool) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	files := []string{}
	if err := seedCanonicalEntrypoint(root, m); err != nil {
		return nil, err
	}
	distinct := map[string]bool{}
	for _, entrypoint := range m.Entrypoints {
		if entrypoint.EffectiveMode() != "managed" {
			continue
		}
		distinct[strings.TrimSpace(entrypoint.File)] = true
	}
	if len(distinct) == 0 {
		distinct[manifest.CanonicalEntrypointFile()] = true
	}

	managedFiles := make([]string, 0, len(distinct))
	for file := range distinct {
		if strings.TrimSpace(file) == "" {
			continue
		}
		managedFiles = append(managedFiles, file)
	}
	sort.Strings(managedFiles)
	for _, file := range managedFiles {
		abs := filepath.Join(root, file)
		if err := ensureManagedEntrypoint(abs, allowRepair); err != nil {
			return nil, err
		}
		files = append(files, abs)
	}

	for _, agent := range entrypointAgents(m) {
		alias := manifest.EntrypointFile(agent)
		if alias == "" || alias == manifest.CanonicalEntrypointFile() {
			continue
		}
		aliasAbs := filepath.Join(root, alias)
		canonicalAbs := filepath.Join(root, manifest.CanonicalEntrypointFile())
		if err := ensureEntrypointAlias(aliasAbs, canonicalAbs); err != nil {
			return nil, err
		}
		files = append(files, aliasAbs)
	}
	return dedupe(files), nil
}

func validateManagedEntrypoints(root string, m *manifest.Manifest) []string {
	if m == nil {
		return nil
	}
	if len(m.Entrypoints) == 0 {
		return nil
	}
	issues := []string{}
	distinct := map[string]bool{}
	for agent, entrypoint := range m.Entrypoints {
		if entrypoint.EffectiveMode() != "managed" {
			issues = append(issues, fmt.Sprintf("entrypoint %q uses unsupported mode %q", agent, entrypoint.Mode))
			continue
		}
		distinct[entrypoint.File] = true
		if entrypoint.File != manifest.CanonicalEntrypointFile() {
			issues = append(issues, fmt.Sprintf("entrypoint %q should use shared canonical file %q, found %q", agent, manifest.CanonicalEntrypointFile(), entrypoint.File))
		}
	}
	if len(distinct) > 1 {
		files := make([]string, 0, len(distinct))
		for file := range distinct {
			files = append(files, file)
		}
		sort.Strings(files)
		issues = append(issues, "multiple managed entrypoint files are configured: "+strings.Join(files, ", "))
	}

	canonicalAbs := filepath.Join(root, manifest.CanonicalEntrypointFile())
	switch state, err := readManagedEntrypointState(canonicalAbs); {
	case errors.Is(err, os.ErrNotExist):
		issues = append(issues, fmt.Sprintf("managed entrypoint %q is missing", manifest.CanonicalEntrypointFile()))
	case err != nil:
		issues = append(issues, err.Error())
	case !state.HasManagedBlock:
		issues = append(issues, fmt.Sprintf("managed entrypoint %q does not contain a ctxpm managed block", manifest.CanonicalEntrypointFile()))
	case state.Damaged:
		issues = append(issues, fmt.Sprintf("managed entrypoint %q has a damaged ctxpm managed block", manifest.CanonicalEntrypointFile()))
	case strings.TrimRight(state.Block, "\n") != strings.TrimRight(manifest.ManagedEntrypoint(), "\n"):
		issues = append(issues, fmt.Sprintf("managed entrypoint %q is out of date; run `ctxpm entrypoint sync`", manifest.CanonicalEntrypointFile()))
	}

	for _, agent := range entrypointAgents(m) {
		alias := manifest.EntrypointFile(agent)
		if alias == "" || alias == manifest.CanonicalEntrypointFile() {
			continue
		}
		aliasAbs := filepath.Join(root, alias)
		relTarget, _ := filepath.Rel(filepath.Dir(aliasAbs), canonicalAbs)
		current, err := os.Readlink(aliasAbs)
		if err == nil {
			if current != relTarget {
				issues = append(issues, fmt.Sprintf("entrypoint alias %q points to %q instead of %q", alias, current, relTarget))
			}
			continue
		}
		if errors.Is(err, os.ErrNotExist) {
			issues = append(issues, fmt.Sprintf("entrypoint alias %q is missing", alias))
			continue
		}
		if _, statErr := os.Lstat(aliasAbs); statErr == nil {
			issues = append(issues, entrypointMergeGuidance([]string{alias}))
			continue
		}
		issues = append(issues, fmt.Sprintf("entrypoint alias %q could not be inspected: %v", alias, err))
	}
	return dedupe(issues)
}

func entrypointAgents(m *manifest.Manifest) []string {
	seen := map[string]bool{}
	for _, agent := range m.Agents {
		if trimmed := strings.TrimSpace(agent); trimmed != "" {
			seen[trimmed] = true
		}
	}
	for agent := range m.Entrypoints {
		if trimmed := strings.TrimSpace(agent); trimmed != "" {
			seen[trimmed] = true
		}
	}
	agents := make([]string, 0, len(seen))
	for agent := range seen {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	return agents
}

func entrypointGitignoreRules(m *manifest.Manifest) []string {
	rules := []string{}
	for _, agent := range entrypointAgents(m) {
		file := manifest.EntrypointFile(agent)
		if file == "" || file == manifest.CanonicalEntrypointFile() {
			continue
		}
		rules = append(rules, file)
	}
	return dedupe(rules)
}

func entrypointMergeGuidance(files []string) string {
	items := dedupe(append([]string(nil), files...))
	sort.Strings(items)
	label := "legacy root entrypoint files"
	if len(items) == 1 {
		label = "root entrypoint file"
	}
	return fmt.Sprintf("%s (%s) need manual migration; merge any unique instructions into %s, then rerun `ctxpm entrypoint sync` so the other root entrypoint filenames become compatibility symlinks", label, strings.Join(items, ", "), manifest.CanonicalEntrypointFile())
}

func seedCanonicalEntrypoint(root string, m *manifest.Manifest) error {
	canonical := filepath.Join(root, manifest.CanonicalEntrypointFile())
	if _, err := os.Lstat(canonical); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	candidates := []string{}
	for _, agent := range entrypointAgents(m) {
		file := manifest.EntrypointFile(agent)
		if file == "" || file == manifest.CanonicalEntrypointFile() {
			continue
		}
		abs := filepath.Join(root, file)
		info, err := os.Lstat(abs)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		candidates = append(candidates, abs)
	}
	candidates = dedupe(candidates)
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) > 1 {
		labels := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			labels = append(labels, filepath.Base(candidate))
		}
		return errors.New(entrypointMergeGuidance(labels))
	}
	return os.Rename(candidates[0], canonical)
}

func ensureEntrypointAlias(aliasPath, canonicalPath string) error {
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o755); err != nil {
		return err
	}
	relTarget, err := filepath.Rel(filepath.Dir(aliasPath), canonicalPath)
	if err != nil {
		return err
	}
	if current, err := os.Readlink(aliasPath); err == nil {
		if current == relTarget {
			return nil
		}
		if err := os.Remove(aliasPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		info, statErr := os.Lstat(aliasPath)
		if statErr != nil {
			return err
		}
		if info.IsDir() {
			return errors.New(entrypointMergeGuidance([]string{filepath.Base(aliasPath)}))
		}
		same, sameErr := sameFileContents(aliasPath, canonicalPath)
		if sameErr != nil {
			return sameErr
		}
		if !same {
			same, sameErr = sameManagedEntrypointEnvelope(aliasPath, canonicalPath)
			if sameErr != nil {
				return sameErr
			}
		}
		if !same {
			return errors.New(entrypointMergeGuidance([]string{filepath.Base(aliasPath)}))
		}
		if err := os.Remove(aliasPath); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(aliasPath); err == nil {
		return errors.New(entrypointMergeGuidance([]string{filepath.Base(aliasPath)}))
	}
	return os.Symlink(relTarget, aliasPath)
}

func sameFileContents(leftPath, rightPath string) (bool, error) {
	left, err := os.ReadFile(leftPath)
	if err != nil {
		return false, err
	}
	right, err := os.ReadFile(rightPath)
	if err != nil {
		return false, err
	}
	return string(left) == string(right), nil
}

func sameManagedEntrypointEnvelope(leftPath, rightPath string) (bool, error) {
	left, err := os.ReadFile(leftPath)
	if err != nil {
		return false, err
	}
	right, err := os.ReadFile(rightPath)
	if err != nil {
		return false, err
	}
	leftPrefix, leftSuffix, leftOK := managedEntrypointEnvelope(string(left))
	rightPrefix, rightSuffix, rightOK := managedEntrypointEnvelope(string(right))
	if !leftOK || !rightOK {
		return false, nil
	}
	leftPrefix = strings.TrimRight(leftPrefix, "\n")
	rightPrefix = strings.TrimRight(rightPrefix, "\n")
	leftSuffix = strings.TrimLeft(leftSuffix, "\n")
	rightSuffix = strings.TrimLeft(rightSuffix, "\n")
	return leftPrefix == rightPrefix && leftSuffix == rightSuffix, nil
}

func managedEntrypointEnvelope(content string) (string, string, bool) {
	start, endIndex, damaged := locateManagedBlock(content)
	if damaged || start < 0 || endIndex <= start {
		return "", "", false
	}
	return content[:start], content[endIndex:], true
}
