package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

type EntrypointSyncResult struct {
	Status    string   `json:"status"`
	Files     []string `json:"files,omitempty"`
	GitStaged []string `json:"git_staged,omitempty"`
}

func (r EntrypointSyncResult) Text() string {
	lines := []string{"Entrypoint sync status: " + r.Status}
	for _, item := range r.Files {
		lines = append(lines, "- "+item)
	}
	for _, item := range r.GitStaged {
		lines = append(lines, "git staged: "+item)
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
	gitStaged := stageEntrypointMigration(a.Root)
	return &EntrypointSyncResult{Status: "applied", Files: dedupe(files), GitStaged: gitStaged}, nil
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

// normalizeSharedEntrypoints ensures any explicitly declared entrypoints use
// the canonical file and managed mode. It does NOT back-fill entries for agents
// that are absent from m.Entrypoints — omitting entrypoints is the canonical
// form and means "all declared agents use AGENTS.md + managed".
//
// Returns true if a change was made to m.Entrypoints.
func normalizeSharedEntrypoints(m *manifest.Manifest) bool {
	if m == nil {
		return false
	}
	if m.Entrypoints == nil {
		return false
	}
	delete(m.Entrypoints, "")

	changed := false
	want := manifest.Entrypoint{File: manifest.CanonicalEntrypointFile(), Mode: "managed"}
	for agent, current := range m.Entrypoints {
		if strings.TrimSpace(agent) == "" {
			continue
		}
		if current.File != want.File || current.EffectiveMode() != want.Mode {
			m.Entrypoints[agent] = want
			changed = true
		}
	}
	// If normalizing left the map empty, nil it so omitempty drops it from yaml.
	if len(m.Entrypoints) == 0 {
		m.Entrypoints = nil
		if changed {
			// It was non-nil before, so clearing it is a change.
			return true
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
	// Validate any explicitly declared entrypoints.
	if len(m.Entrypoints) > 0 {
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
	}

	agents := entrypointAgents(m)
	sourceAbs := filepath.Join(root, manifest.CanonicalEntrypointSourceFile())
	sourceExists := false
	if len(agents) > 0 {
		switch state, err := readManagedEntrypointState(sourceAbs); {
		case errors.Is(err, os.ErrNotExist):
			if len(m.Entrypoints) > 0 {
				issues = append(issues, fmt.Sprintf("managed entrypoint %q is missing", manifest.CanonicalEntrypointFile()))
			}
		case err != nil:
			issues = append(issues, err.Error())
		case !state.HasManagedBlock:
			sourceExists = true
			issues = append(issues, fmt.Sprintf("managed entrypoint %q does not contain a ctxpm managed block", manifest.CanonicalEntrypointFile()))
		case state.Damaged:
			sourceExists = true
			issues = append(issues, fmt.Sprintf("managed entrypoint %q has a damaged ctxpm managed block", manifest.CanonicalEntrypointFile()))
		case strings.TrimRight(state.Block, "\n") != strings.TrimRight(manifest.ManagedEntrypoint(), "\n"):
			sourceExists = true
			issues = append(issues, fmt.Sprintf("managed entrypoint %q is out of date; run `ctxpm entrypoint sync`", manifest.CanonicalEntrypointFile()))
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
			current, err := os.Readlink(aliasAbs)
			if err == nil {
				if current != relTarget {
					issues = append(issues, fmt.Sprintf("entrypoint alias %q points to %q instead of %q", alias, current, relTarget))
				}
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, fmt.Sprintf("entrypoint alias %q is missing", alias))
				return
			}
			if _, statErr := os.Lstat(aliasAbs); statErr == nil {
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
	// Always gitignore the root AGENTS.md symlink; the source lives in .ctxpm/AGENTS.md.
	// Use a leading "/" so the rule matches only the project root, not .ctxpm/AGENTS.md.
	rules := []string{"/" + manifest.CanonicalEntrypointFile()}
	for _, agent := range entrypointAgents(m) {
		file := manifest.EntrypointFile(agent)
		if file == "" || file == manifest.CanonicalEntrypointFile() {
			continue
		}
		rules = append(rules, "/"+file)
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
	sourceAbs := filepath.Join(root, manifest.CanonicalEntrypointSourceFile())
	if _, err := os.Lstat(sourceAbs); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		return err
	}

	// Collect all real (non-symlink) root-level entrypoint files as migration candidates,
	// including AGENTS.md itself (which was the canonical file before this change).
	candidateFiles := []string{manifest.CanonicalEntrypointFile()}
	for _, agent := range entrypointAgents(m) {
		file := manifest.EntrypointFile(agent)
		if file == "" || file == manifest.CanonicalEntrypointFile() {
			continue
		}
		candidateFiles = append(candidateFiles, file)
	}

	candidates := []string{}
	for _, file := range dedupe(candidateFiles) {
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

// stageEntrypointMigration detects when AGENTS.md was just migrated from a
// tracked root file to a symlink pointing at .ctxpm/AGENTS.md and automatically
// applies the corresponding git index operations so the user does not need to
// run git commands manually. This is best-effort: if git is unavailable or the
// root is not a git repo the function is a no-op.
//
// Operations performed:
//   - git rm --cached AGENTS.md  (when index has it as a regular file but disk has a symlink)
//   - git add .ctxpm/AGENTS.md   (when the source file is not yet in the index)
//   - git add .gitignore          (when .gitignore has unstaged modifications)
func stageEntrypointMigration(root string) []string {
	ctx := context.Background()
	if _, err := runGit(ctx, "-C", root, "rev-parse", "--git-dir"); err != nil {
		return nil
	}

	var staged []string
	canonical := manifest.CanonicalEntrypointFile()
	source := manifest.CanonicalEntrypointSourceFile()

	// If AGENTS.md is tracked in the git index as a regular file (mode 100644)
	// but is now a symlink on disk, remove it from the index.
	lsOut, err := runGit(ctx, "-C", root, "ls-files", "--stage", "--", canonical)
	if err == nil && strings.HasPrefix(lsOut, "100644") {
		info, statErr := os.Lstat(filepath.Join(root, canonical))
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			if _, rmErr := runGit(ctx, "-C", root, "rm", "--cached", "--", canonical); rmErr == nil {
				staged = append(staged, "git rm --cached "+canonical)
			}
		}
	}

	// If .ctxpm/AGENTS.md exists on disk but is not yet in the git index, stage it.
	lsOut, err = runGit(ctx, "-C", root, "ls-files", "--", source)
	if err == nil && strings.TrimSpace(lsOut) == "" {
		if _, statErr := os.Lstat(filepath.Join(root, source)); statErr == nil {
			if _, addErr := runGit(ctx, "-C", root, "add", "--", source); addErr == nil {
				staged = append(staged, "git add "+source)
			}
		}
	}

	// Stage .gitignore if it has unstaged changes.
	diffOut, err := runGit(ctx, "-C", root, "diff", "--name-only", "--", ".gitignore")
	if err == nil && strings.TrimSpace(diffOut) != "" {
		if _, addErr := runGit(ctx, "-C", root, "add", "--", ".gitignore"); addErr == nil {
			staged = append(staged, "git add .gitignore")
		}
	}

	return staged
}
