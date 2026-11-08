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

// normalizeSharedEntrypoints clears any deprecated Entrypoints field that may
// have been parsed from an old yaml file. Returns true if the field was non-nil.
func normalizeSharedEntrypoints(m *manifest.Manifest) bool {
	if m == nil || m.Entrypoints == nil {
		return false
	}
	m.Entrypoints = nil
	return true
}

func syncManagedEntrypoints(root string, m *manifest.Manifest, allowRepair bool) ([]string, error) {
	if m == nil {
		return nil, nil
	}
	files := []string{}
	if err := seedCanonicalEntrypoint(root, m); err != nil {
		return nil, err
	}

	// Write managed content to the source file inside .ctxpm/.
	sourceAbs := filepath.Join(root, manifest.CanonicalEntrypointSourceFile())
	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		return nil, err
	}
	if err := ensureManagedEntrypoint(sourceAbs, allowRepair); err != nil {
		return nil, err
	}
	files = append(files, sourceAbs)

	// Create the root-level AGENTS.md symlink (always present, points directly to source).
	canonicalAliasAbs := filepath.Join(root, manifest.CanonicalEntrypointFile())
	if err := ensureEntrypointAlias(canonicalAliasAbs, sourceAbs); err != nil {
		return nil, err
	}
	files = append(files, canonicalAliasAbs)

	// Create agent-specific aliases (e.g. CLAUDE.md, ANTIGRAVITY.md), also pointing to source.
	for _, agent := range entrypointAgents(m) {
		alias := manifest.EntrypointFile(agent)
		if alias == "" || alias == manifest.CanonicalEntrypointFile() {
			continue
		}
		aliasAbs := filepath.Join(root, alias)
		if err := ensureEntrypointAlias(aliasAbs, sourceAbs); err != nil {
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
	issues := []string{}

	agents := entrypointAgents(m)
	sourceAbs := filepath.Join(root, manifest.CanonicalEntrypointSourceFile())
	sourceExists := false
	if len(agents) > 0 {
		switch state, err := readManagedEntrypointState(sourceAbs); {
		case errors.Is(err, os.ErrNotExist):
			issues = append(issues, fmt.Sprintf("managed entrypoint source %q is missing; run `ctxpm install`", manifest.CanonicalEntrypointSourceFile()))
		case err != nil:
			issues = append(issues, err.Error())
		case !state.HasManagedBlock:
			sourceExists = true
			issues = append(issues, fmt.Sprintf("managed entrypoint source %q does not contain a ctxpm managed block", manifest.CanonicalEntrypointSourceFile()))
		case state.Damaged:
			sourceExists = true
			issues = append(issues, fmt.Sprintf("managed entrypoint source %q has a damaged ctxpm managed block", manifest.CanonicalEntrypointSourceFile()))
		case strings.TrimRight(state.Block, "\n") != strings.TrimRight(manifest.ManagedEntrypoint(), "\n"):
			sourceExists = true
			issues = append(issues, fmt.Sprintf("managed entrypoint source %q is out of date; run `ctxpm install`", manifest.CanonicalEntrypointSourceFile()))
		default:
			sourceExists = true
		}
	}

	// Validate all root-level entrypoint symlinks only when the source file exists.
	// When source is absent (never synced), aliases can't point anywhere yet.
	if sourceExists {
		validateAlias := func(alias string) {
			aliasAbs := filepath.Join(root, alias)
			relTarget, _ := filepath.Rel(filepath.Dir(aliasAbs), sourceAbs)
			relTarget = filepath.ToSlash(relTarget)
			current, err := os.Readlink(aliasAbs)
			if err == nil {
				if filepath.ToSlash(current) != relTarget {
					issues = append(issues, fmt.Sprintf("entrypoint alias %q points to %q instead of %q", alias, current, relTarget))
				}
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, fmt.Sprintf("entrypoint alias %q is missing", alias))
				return
			}
			if _, statErr := os.Lstat(aliasAbs); statErr == nil {
				if gitSymlinkPlaceholder(root, alias, relTarget) {
					issues = append(issues, gitSymlinkPlaceholderGuidance(alias))
					return
				}
				issues = append(issues, entrypointMergeGuidance([]string{alias}))
				return
			}
			issues = append(issues, fmt.Sprintf("entrypoint alias %q could not be inspected: %v", alias, err))
		}

		validateAlias(manifest.CanonicalEntrypointFile())
		for _, agent := range agents {
			alias := manifest.EntrypointFile(agent)
			if alias == "" || alias == manifest.CanonicalEntrypointFile() {
				continue
			}
			validateAlias(alias)
		}
	}
	// Warn when an untracked source or alias would be omitted from commits.
	paths := []string{manifest.CanonicalEntrypointSourceFile(), manifest.CanonicalEntrypointFile()}
	for _, agent := range agents {
		if file := manifest.EntrypointFile(agent); file != "" {
			paths = append(paths, file)
		}
	}
	for _, path := range dedupe(paths) {
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil && ignoredEntrypoint(root, path) {
			issues = append(issues, fmt.Sprintf("entrypoint %q is ignored by Git; inspect .gitignore and Git exclude rules so it can be committed", path))
		}
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
	agents := make([]string, 0, len(seen))
	for agent := range seen {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	return agents
}

func entrypointMergeGuidance(files []string) string {
	items := dedupe(append([]string(nil), files...))
	sort.Strings(items)
	label := "legacy root entrypoint files"
	if len(items) == 1 {
		label = "root entrypoint file"
	}
	return fmt.Sprintf("%s (%s) need manual migration; merge any unique instructions into %s, then rerun `ctxpm install` so the root entrypoint filenames become compatibility symlinks", label, strings.Join(items, ", "), manifest.CanonicalEntrypointSourceFile())
}

func seedCanonicalEntrypoint(root string, m *manifest.Manifest) error {
	sourceAbs := filepath.Join(root, manifest.CanonicalEntrypointSourceFile())
	if _, err := os.Lstat(sourceAbs); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		return err
	}

	// Collect existing entrypoint contents, including links to legacy files.
	// The target of a legacy link is copied, not moved or removed.
	candidateFiles := []string{manifest.CanonicalEntrypointFile()}
	for _, agent := range entrypointAgents(m) {
		file := manifest.EntrypointFile(agent)
		if file == "" || file == manifest.CanonicalEntrypointFile() {
			continue
		}
		candidateFiles = append(candidateFiles, file)
	}

	candidates := []string{}
	var candidateInfo []os.FileInfo
	for _, file := range dedupe(candidateFiles) {
		abs := filepath.Join(root, file)
		info, err := os.Lstat(abs)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		resolvedInfo := info
		if info.Mode()&os.ModeSymlink != 0 {
			if resolvedInfo, err = os.Stat(abs); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return err
			}
		}
		duplicate := false
		for i, existing := range candidateInfo {
			if os.SameFile(existing, resolvedInfo) {
				duplicate = true
				// Prefer moving a root file to copying through its alias.
				if info.Mode().IsRegular() {
					candidates[i] = abs
				}
				break
			}
		}
		if duplicate {
			continue
		}
		candidates = append(candidates, abs)
		candidateInfo = append(candidateInfo, resolvedInfo)
	}
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
	info, err := os.Lstat(candidates[0])
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		data, err := os.ReadFile(candidates[0])
		if err != nil {
			return err
		}
		return os.WriteFile(sourceAbs, data, 0o644)
	}
	return os.Rename(candidates[0], sourceAbs)
}

func ensureEntrypointAlias(aliasPath, canonicalPath string) error {
	if err := os.MkdirAll(filepath.Dir(aliasPath), 0o755); err != nil {
		return err
	}
	relTarget, err := filepath.Rel(filepath.Dir(aliasPath), canonicalPath)
	if err != nil {
		return err
	}
	relTarget = filepath.ToSlash(relTarget)
	if current, err := os.Readlink(aliasPath); err == nil {
		if filepath.ToSlash(current) == relTarget {
			return nil
		}
		// Never discard unique instructions reachable through a legacy link.
		if _, statErr := os.Stat(aliasPath); statErr == nil {
			same, compareErr := sameFileContents(aliasPath, canonicalPath)
			if compareErr != nil {
				return compareErr
			}
			if !same {
				same, compareErr = sameManagedEntrypointEnvelope(aliasPath, canonicalPath)
				if compareErr != nil {
					return compareErr
				}
			}
			if !same {
				same, compareErr = sameAfterManagedBlockUpdate(aliasPath, canonicalPath)
				if compareErr != nil {
					return compareErr
				}
			}
			if !same {
				return errors.New(entrypointMergeGuidance([]string{filepath.Base(aliasPath)}))
			}
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return statErr
		}
		if err := os.Remove(aliasPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		info, statErr := os.Lstat(aliasPath)
		if statErr != nil {
			return err
		}
		if gitSymlinkPlaceholder(filepath.Dir(aliasPath), filepath.Base(aliasPath), relTarget) {
			return errors.New(gitSymlinkPlaceholderGuidance(filepath.Base(aliasPath)))
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
			same, sameErr = sameAfterManagedBlockUpdate(aliasPath, canonicalPath)
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

func sameAfterManagedBlockUpdate(legacyPath, canonicalPath string) (bool, error) {
	legacy, err := os.ReadFile(legacyPath)
	if err != nil {
		return false, err
	}
	canonical, err := os.ReadFile(canonicalPath)
	if err != nil {
		return false, err
	}
	updated, err := updateManagedBlock(string(legacy), manifest.ManagedEntrypoint(), false)
	return err == nil && updated == string(canonical), nil
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
