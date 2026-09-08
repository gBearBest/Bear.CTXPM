package engine

//go:generate go run generate_bundled_docs.go

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

const bundledCLIInstallScriptURL = "https://raw.githubusercontent.com/gBearBest/Bear.CTXPM/latest/cli/install.sh"

type managedEntrypointState struct {
	HasManagedBlock bool
	Damaged         bool
	Block           string
}

func ensureManagedEntrypoint(path string, allowRepair bool) error {
	content := manifest.ManagedEntrypoint()
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(content), 0o644)
		}
		return err
	}

	updated, err := updateManagedBlock(string(existing), content, allowRepair)
	if err != nil {
		return fmt.Errorf("entrypoint %s: %w", path, err)
	}
	if updated == string(existing) {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func updateManagedBlock(existing, block string, allowRepair bool) (string, error) {
	start, endIndex, damaged := locateManagedBlock(existing)
	if start == -1 && endIndex == -1 && !damaged {
		return appendManagedBlock(existing, block), nil
	}
	if start >= 0 && endIndex > start {
		return joinManagedBlock(existing[:start], existing[endIndex:], block), nil
	}
	if !allowRepair {
		return "", fmt.Errorf("managed ctxpm block is damaged; rerun with --force to rebuild it")
	}
	return rebuildManagedBlock(existing, block), nil
}

func rebuildManagedBlock(existing, block string) string {
	start, endIndex, _ := locateManagedBlock(existing)
	switch {
	case start >= 0 && endIndex > start:
		return joinManagedBlock(existing[:start], existing[endIndex:], block)
	case start >= 0:
		return joinManagedBlock(existing[:start], "", block)
	case endIndex > 0:
		return joinManagedBlock("", existing[endIndex:], block)
	default:
		return appendManagedBlock(existing, block)
	}
}

func readManagedEntrypointState(path string) (managedEntrypointState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return managedEntrypointState{}, err
	}
	start, endIndex, damaged := locateManagedBlock(string(data))
	state := managedEntrypointState{
		HasManagedBlock: start >= 0 || endIndex >= 0,
		Damaged:         damaged,
	}
	if start >= 0 && endIndex > start {
		state.Block = string(data[start:endIndex])
	}
	return state, nil
}

func locateManagedBlock(content string) (int, int, bool) {
	beginIndex := strings.Index(content, "<!-- ctxpm:begin")
	endIndex := strings.Index(content, "<!-- ctxpm:end -->")
	if beginIndex == -1 && endIndex == -1 {
		return -1, -1, false
	}
	if beginIndex >= 0 {
		lineEnd := strings.Index(content[beginIndex:], "\n")
		if lineEnd == -1 {
			return beginIndex, -1, true
		}
		beginIndex += 0
	}
	if beginIndex >= 0 && endIndex > beginIndex {
		endIndex += len("<!-- ctxpm:end -->")
		return beginIndex, endIndex, false
	}
	return beginIndex, endIndex, true
}

func appendManagedBlock(existing, block string) string {
	trimmed := strings.TrimRight(existing, "\n")
	if trimmed == "" {
		return block
	}
	return trimmed + "\n\n" + block + "\n"
}

func joinManagedBlock(prefix, suffix, block string) string {
	prefix = strings.TrimRight(prefix, "\n")
	suffix = strings.TrimLeft(suffix, "\n")
	switch {
	case prefix == "" && suffix == "":
		return block
	case prefix == "":
		return block + "\n\n" + suffix
	case suffix == "":
		return prefix + "\n\n" + block + "\n"
	default:
		return prefix + "\n\n" + block + "\n\n" + suffix
	}
}

type bundledCtxpmResult struct {
	Resource       manifest.Resource
	Files          []string
	Status         string
	YAMLStatus     string
	LocalCLIStatus string
	Warnings       []string
}

type bundledCtxpmFile struct {
	Content string
	Mode    os.FileMode
}

func ensureBundledCtxpm(root string, agents []string, releaseVersion string) (*bundledCtxpmResult, error) {
	resource := bundledCtxpmResource(agents, releaseVersion)
	created := []string{}
	result := &bundledCtxpmResult{
		Resource:   resource,
		Status:     "updated",
		YAMLStatus: "updated",
	}

	resourceRoot := filepath.Join(root, filepath.FromSlash(resource.Path))
	if err := os.MkdirAll(resourceRoot, 0o755); err != nil {
		return nil, err
	}
	if err := cleanBundledCtxpmSkill(resourceRoot); err != nil {
		return nil, err
	}
	created = append(created, resourceRoot)

	for _, relative := range bundledCtxpmSkillPaths() {
		file := bundledCtxpmSkillFiles[relative]
		target := filepath.Join(resourceRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(file.Content), file.Mode); err != nil {
			return nil, err
		}
		created = append(created, target)
	}

	cliPath := filepath.Join(resourceRoot, "cli", "ctxpm")
	if err := os.MkdirAll(filepath.Dir(cliPath), 0o755); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not prepare local CLI directory: %v", err))
	} else {
		created = append(created, filepath.Dir(cliPath))
		status, warnings := prepareBundledCLI(context.Background(), cliPath, root)
		result.LocalCLIStatus = status
		result.Warnings = append(result.Warnings, warnings...)
		if _, err := os.Stat(cliPath); err == nil {
			created = append(created, cliPath)
		}
	}

	if err := ensureCompatibility(root, agents, resource); err != nil {
		return nil, err
	}
	for _, compat := range manifest.DerivedCompatibilityPaths(agents, resource) {
		created = append(created, filepath.Join(root, filepath.FromSlash(compat)))
	}
	result.Files = dedupe(created)
	if result.LocalCLIStatus == "" {
		result.LocalCLIStatus = "unavailable"
	}
	return result, nil
}

func bundledCtxpmResource(agents []string, releaseVersion string) manifest.Resource {
	return manifest.Resource{
		Name:   "ctxpm",
		Type:   "skill",
		Layout: manifest.LayoutDir,
		Path:   ".ctxpm/dependencies/skills/ctxpm",
		Entry:  "SKILL.md",
		Source: &manifest.Source{
			Type:  "git",
			URL:   "https://github.com/gBearBest/Bear.CTXPM",
			Path:  "resources/skills/ctxpm",
			Ref:   "latest",
			Entry: "SKILL.md",
		},
		Version: canonicalCtxpmReleaseVersion(releaseVersion),
	}
}

func bundledCtxpmSkillPaths() []string {
	paths := make([]string, 0, len(bundledCtxpmSkillFiles))
	for relative := range bundledCtxpmSkillFiles {
		paths = append(paths, relative)
	}
	sort.Strings(paths)
	return paths
}

func bundledCtxpmSkillDirectories() map[string]bool {
	directories := map[string]bool{}
	for relative := range bundledCtxpmSkillFiles {
		for directory := filepath.ToSlash(filepath.Dir(filepath.FromSlash(relative))); directory != "."; directory = filepath.ToSlash(filepath.Dir(filepath.FromSlash(directory))) {
			directories[directory] = true
		}
	}
	return directories
}

func cleanBundledCtxpmSkill(root string) error {
	directories := []string{}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if info.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if relative == "cli/ctxpm" || relative == "cli/ctxpm.exe" {
			return nil
		}
		return os.Remove(path)
	}); err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		relative, err := filepath.Rel(root, directories[i])
		if err != nil {
			return err
		}
		if filepath.ToSlash(relative) == "cli" {
			continue
		}
		if err := os.Remove(directories[i]); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func prepareBundledCLI(ctx context.Context, targetPath, projectRoot string) (string, []string) {
	warnings := []string{}
	sources := []string{}
	if executable, err := os.Executable(); err == nil {
		sources = append(sources, executable)
	}
	if lookup, err := exec.LookPath("ctxpm"); err == nil {
		sources = append(sources, lookup)
	}
	for _, source := range dedupe(sources) {
		if samePath(source, targetPath) {
			if err := verifyBundledCLI(targetPath); err == nil {
				return "verified-current", warnings
			}
			warnings = append(warnings, fmt.Sprintf("current project-local CLI failed verification: %s", targetPath))
			continue
		}
		info, err := os.Stat(source)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("could not inspect CLI source %s: %v", source, err))
			continue
		}
		if err := copyFile(source, targetPath, info.Mode()); err != nil {
			warnings = append(warnings, fmt.Sprintf("could not copy CLI from %s: %v", source, err))
			continue
		}
		if err := verifyBundledCLI(targetPath); err != nil {
			warnings = append(warnings, fmt.Sprintf("copied CLI from %s but verification failed: %v", source, err))
			continue
		}
		return "synchronized-and-verified", warnings
	}
	if err := verifyBundledCLI(targetPath); err == nil {
		return "verified-existing", warnings
	}
	if err := installBundledCLIRemotely(ctx, projectRoot); err == nil {
		if err := verifyBundledCLI(targetPath); err == nil {
			return "remote-installed-and-verified", warnings
		}
		warnings = append(warnings, fmt.Sprintf("remote installer completed but verification failed for %s", targetPath))
	} else {
		warnings = append(warnings, err.Error())
	}
	if err := verifyBundledCLI(targetPath); err == nil {
		return "verified-existing", warnings
	}
	warnings = append(warnings, "companion CLI could not be prepared automatically; ctxpm skill remains available for manual workflow")
	return "unavailable", warnings
}

func installBundledCLIRemotely(ctx context.Context, projectRoot string) error {
	remoteCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(remoteCtx, "sh", "-c", `curl -fsSL `+bundledCLIInstallScriptURL+` | sh -s -- --scope project --project-root "$1"`, "ctxpm-install", projectRoot)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("remote installer failed: %w: %s", err, trimmed)
		}
		return fmt.Errorf("remote installer failed: %w", err)
	}
	return nil
}

func samePath(left, right string) bool {
	if left == right {
		return true
	}
	absLeft, errLeft := filepath.Abs(left)
	absRight, errRight := filepath.Abs(right)
	if errLeft == nil && errRight == nil && absLeft == absRight {
		return true
	}
	return false
}

func verifyBundledCLI(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--help")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
