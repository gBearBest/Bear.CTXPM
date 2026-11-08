package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitSymlinkPlaceholder reports a Git-tracked symlink checked out as a plain
// file when core.symlinks is disabled. Require both the index mode and exact
// contents, so an ordinary user-owned file is never treated as a placeholder.
func gitSymlinkPlaceholder(root, alias, target string) bool {
	ctx := context.Background()
	config, err := runGit(ctx, "-C", root, "config", "--bool", "core.symlinks")
	if err != nil || strings.TrimSpace(config) != "false" {
		return false
	}
	index, err := runGit(ctx, "-C", root, "ls-files", "--stage", "--", alias)
	if err != nil || !strings.HasPrefix(index, "120000 ") || strings.Contains(strings.TrimSpace(index), "\n") {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, alias))
	return err == nil && string(data) == filepath.ToSlash(target)
}

func gitSymlinkPlaceholderGuidance(alias string) string {
	return fmt.Sprintf("entrypoint %q is a Git symlink checked out as a plain file because core.symlinks is false; enable symlink support and permissions, set core.symlinks=true, then restore the entrypoint from Git (preserve any local changes first)", alias)
}

// ignoredEntrypoint reports an untracked entrypoint Git would exclude, including
// patterns from parent directories and user-level excludes. Tracked paths need
// no warning, even if an ignore rule would match them.
func ignoredEntrypoint(root, path string) bool {
	output, err := runGit(context.Background(), "-C", root, "check-ignore", "--", path)
	return err == nil && strings.TrimSpace(output) != ""
}
