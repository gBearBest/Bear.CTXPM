package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

// managedEntrypointRules lists exact root rules for the configured aliases.
// Rules are removed only when an alias points to the canonical source.
func managedEntrypointRules(m *manifest.Manifest) map[string]bool {
	rules := map[string]bool{}
	if m == nil {
		return rules
	}
	aliases := []string{manifest.CanonicalEntrypointFile()}
	for _, agent := range entrypointAgents(m) {
		if name := manifest.EntrypointFile(agent); name != "" {
			aliases = append(aliases, name)
		}
	}
	for _, alias := range dedupe(aliases) {
		rules["/"+alias] = true
	}
	return rules
}

// migrateEntrypointGitignore removes only eligible exact root-anchored rules.
func migrateEntrypointGitignore(root string, rules map[string]bool) (bool, error) {
	if len(rules) == 0 {
		return false, nil
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	source := filepath.Clean(filepath.Join(rootAbs, manifest.CanonicalEntrypointSourceFile()))
	for rule := range rules {
		aliasPath := filepath.Join(rootAbs, strings.TrimPrefix(rule, "/"))
		target, err := os.Readlink(aliasPath)
		if err != nil {
			delete(rules, rule)
			continue
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(aliasPath), target)
		}
		if filepath.Clean(target) != source {
			delete(rules, rule)
		}
	}
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var out strings.Builder
	changed := false
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if rules[strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")] {
			changed = true
			continue
		}
		out.WriteString(line)
	}
	if !changed {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// entrypointCommitGuidance identifies both the expected project files and any
// ignore rules that still prevent an untracked entrypoint from being added.
func entrypointCommitGuidance(root string, m *manifest.Manifest) []string {
	paths := []string{manifest.CanonicalEntrypointSourceFile(), manifest.CanonicalEntrypointFile()}
	for _, agent := range entrypointAgents(m) {
		if name := manifest.EntrypointFile(agent); name != "" {
			paths = append(paths, name)
		}
	}
	paths = dedupe(paths)
	warnings := []string{"Commit the managed entrypoint and root aliases: " + strings.Join(paths, ", ")}
	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(root, path)); err == nil && ignoredEntrypoint(root, path) {
			warnings = append(warnings, "Git ignores "+path+"; inspect .gitignore and Git exclude rules before committing")
		}
	}
	return warnings
}
