package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const hooksPath = ".githooks"
const hookMarker = "# ctxpm-managed-git-hook"

type HooksResult struct {
	Status  string   `json:"status"`
	Enabled bool     `json:"enabled"`
	Path    string   `json:"path"`
	Hooks   []string `json:"hooks"`
}

func (r HooksResult) Text() string {
	return fmt.Sprintf("Git hooks: %s\nPath: %s\n", r.Status, r.Path)
}

func (a *App) EnableHooks(ctx context.Context) (*HooksResult, error) {
	if err := ensureGitRepository(ctx, a.Root); err != nil {
		return nil, err
	}
	configured, err := configuredHooksPath(ctx, a.Root)
	if err != nil {
		return nil, err
	}
	if configured != "" && configured != hooksPath {
		return nil, fmt.Errorf("core.hooksPath is already set to %q; ctxpm will not replace it", configured)
	}
	dir := filepath.Join(a.Root, hooksPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for name, content := range managedHookFiles() {
		path := filepath.Join(dir, name)
		if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), hookMarker) {
			return nil, fmt.Errorf("hook %s already exists and is not managed by ctxpm", filepath.ToSlash(filepath.Join(hooksPath, name)))
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			return nil, err
		}
	}
	if _, err := runGit(ctx, "-C", a.Root, "config", "--local", "core.hooksPath", hooksPath); err != nil {
		return nil, err
	}
	return a.HooksStatus(ctx)
}

func (a *App) enableHooksByDefault(ctx context.Context) (string, error) {
	if err := ensureGitRepository(ctx, a.Root); err != nil {
		return "not_git_repository", nil
	}
	configured, err := configuredHooksPath(ctx, a.Root)
	if err != nil {
		return "", err
	}
	if configured != "" && configured != hooksPath {
		return "custom_hooks_path", nil
	}
	result, err := a.EnableHooks(ctx)
	if err != nil {
		return "", err
	}
	return result.Status, nil
}

func (a *App) HooksStatus(ctx context.Context) (*HooksResult, error) {
	if err := ensureGitRepository(ctx, a.Root); err != nil {
		return nil, err
	}
	configured, err := configuredHooksPath(ctx, a.Root)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for name, content := range managedHookFiles() {
		data, err := os.ReadFile(filepath.Join(a.Root, hooksPath, name))
		if err == nil && string(data) == content {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	enabled := configured == hooksPath && len(names) == len(managedHookFiles())
	status := "disabled"
	if enabled {
		status = "enabled"
	} else if configured != "" {
		status = "custom_hooks_path"
	}
	return &HooksResult{Status: status, Enabled: enabled, Path: configured, Hooks: names}, nil
}

func ensureGitRepository(ctx context.Context, root string) error {
	if _, err := runGit(ctx, "-C", root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return errors.New("ctxpm git hooks require a Git working tree")
	}
	return nil
}

func configuredHooksPath(ctx context.Context, root string) (string, error) {
	output, err := runGit(ctx, "-C", root, "config", "--local", "--get", "core.hooksPath")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(output), nil
}

func managedHookFiles() map[string]string {
	helper := strings.Join([]string{
		"#!/bin/sh",
		hookMarker,
		"repo_root=$(git rev-parse --show-toplevel 2>/dev/null) || exit 0",
		"[ -f \"$repo_root/ctxpm.yaml\" ] || exit 0",
		"project_cli=\"$repo_root/.ctxpm/dependencies/skills/ctxpm/cli/ctxpm\"",
		"if [ -x \"$project_cli\" ]; then",
		"  ctxpm_cli=$project_cli",
		"elif command -v ctxpm >/dev/null 2>&1; then",
		"  ctxpm_cli=$(command -v ctxpm)",
		"else",
		"  printf '%s\\n' 'ctxpm: automatic dependency install skipped because no ctxpm CLI is available' >&2",
		"  exit 0",
		"fi",
		"if ! install_output=$(\"$ctxpm_cli\" install 2>&1); then",
		"  printf '%s\\n' 'ctxpm: automatic dependency install failed; run ctxpm install manually for details' >&2",
		"  printf '%s\\n' \"$install_output\" >&2",
		"fi",
		"exit 0",
		"",
	}, "\n")
	forward := func(extra string) string {
		lines := []string{"#!/bin/sh", hookMarker}
		if extra != "" {
			lines = append(lines, extra)
		}
		lines = append(lines, "exec \"$(dirname \"$0\")/ctxpm-auto-install\"", "")
		return strings.Join(lines, "\n")
	}
	return map[string]string{
		"ctxpm-auto-install": helper,
		"post-checkout":      forward(""),
		"post-merge":         forward(""),
		"post-rewrite":       forward("[ \"$1\" = \"rebase\" ] || exit 0"),
	}
}
