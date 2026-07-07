package engine

//go:generate go run generate_bundled_docs.go

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func ensureManagedEntrypoint(path, agent string, allowRepair bool) error {
	content := manifest.ManagedEntrypoint(agent)
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
	return os.WriteFile(path, []byte(updated), 0o644)
}

func updateManagedBlock(existing, block string, allowRepair bool) (string, error) {
	const begin = "<!-- ctxpm:begin agent="
	const end = "<!-- ctxpm:end -->"

	start := strings.Index(existing, begin)
	endIndex := strings.Index(existing, end)
	if start == -1 && endIndex == -1 {
		return appendManagedBlock(existing, block), nil
	}
	if start >= 0 && endIndex > start {
		rest := existing[start:]
		relativeEndIndex := strings.Index(rest, end)
		if relativeEndIndex >= 0 {
			relativeEndIndex += start + len(end)
			return joinManagedBlock(existing[:start], existing[relativeEndIndex:], block), nil
		}
	}
	if !allowRepair {
		return "", fmt.Errorf("managed ctxpm block is damaged; rerun with --force to rebuild it")
	}
	return rebuildManagedBlock(existing, block), nil
}

func rebuildManagedBlock(existing, block string) string {
	const begin = "<!-- ctxpm:begin agent="
	const end = "<!-- ctxpm:end -->"

	start := strings.Index(existing, begin)
	endIndex := strings.LastIndex(existing, end)
	switch {
	case start >= 0 && endIndex > start:
		return joinManagedBlock(existing[:start], existing[endIndex+len(end):], block)
	case start >= 0:
		return joinManagedBlock(existing[:start], "", block)
	case endIndex >= 0:
		return joinManagedBlock("", existing[endIndex+len(end):], block)
	default:
		return appendManagedBlock(existing, block)
	}
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

func ensureBundledCtxpm(root string, agents []string) (*bundledCtxpmResult, error) {
	resource := bundledCtxpmResource(agents)
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
	created = append(created, resourceRoot)

	skillPath := filepath.Join(resourceRoot, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(bundledCtxpmSkillContent), 0o644); err != nil {
		return nil, err
	}
	created = append(created, skillPath)

	yamlPath := filepath.Join(resourceRoot, "ctxpm-yaml.md")
	if err := os.WriteFile(yamlPath, []byte(bundledCtxpmYAMLContent), 0o644); err != nil {
		return nil, err
	}
	created = append(created, yamlPath)
	cliPath := filepath.Join(resourceRoot, "cli", "ctxpm")
	if err := os.MkdirAll(filepath.Dir(cliPath), 0o755); err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("could not prepare local CLI directory: %v", err))
	} else {
		created = append(created, filepath.Dir(cliPath))
		status, warnings := prepareBundledCLI(cliPath)
		result.LocalCLIStatus = status
		result.Warnings = append(result.Warnings, warnings...)
		if _, err := os.Stat(cliPath); err == nil {
			created = append(created, cliPath)
		}
	}

	if err := ensureCompatibility(root, resource); err != nil {
		return nil, err
	}
	for _, compat := range resource.Compatibility {
		created = append(created, filepath.Join(root, filepath.FromSlash(compat)))
	}
	result.Files = dedupe(created)
	if result.LocalCLIStatus == "" {
		result.LocalCLIStatus = "unavailable"
	}
	return result, nil
}

func bundledCtxpmResource(agents []string) manifest.Resource {
	resource := manifest.Resource{
		Name:   "ctxpm",
		Type:   "skill",
		Layout: manifest.LayoutDir,
		Path:   ".ctxpm/dependencies/skills/ctxpm",
		Entry:  "SKILL.md",
		Source: &manifest.Source{
			Type:  "git",
			URL:   "https://github.com/gBearBest/Bear.CTXPM",
			Path:  "resources/skills/ctxpm",
			Entry: "SKILL.md",
		},
		Version: currentBuildRevision(),
	}
	resource.Compatibility = compatibilityPaths(agents, resource)
	return resource
}

func currentBuildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

func prepareBundledCLI(targetPath string) (string, []string) {
	warnings := []string{}
	if err := verifyBundledCLI(targetPath); err == nil {
		return "verified-existing", nil
	}

	sources := []string{}
	if executable, err := os.Executable(); err == nil {
		sources = append(sources, executable)
	}
	if lookup, err := exec.LookPath("ctxpm"); err == nil {
		sources = append(sources, lookup)
	}
	for _, source := range dedupe(sources) {
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
		return "installed-and-verified", warnings
	}
	if err := verifyBundledCLI(targetPath); err == nil {
		return "verified-existing", warnings
	}
	warnings = append(warnings, "companion CLI could not be prepared automatically; ctxpm skill remains available for manual workflow")
	return "unavailable", warnings
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
