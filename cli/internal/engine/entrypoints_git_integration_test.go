package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func gitEntrypointTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	args = append([]string{"-C", root}, args...)
	out, err := runGit(context.Background(), args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func requireEntrypointSymlinks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Skipf("filesystem cannot create symlinks: %v", err)
	}
}

func TestEntrypointTrackedThroughGitClone(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	gitEntrypointTest(t, root, "config", "core.excludesFile", os.DevNull)
	if _, err := New(root).Init(InitOptions{Agent: "claude-code", CurrentVersion: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", "ctxpm.yaml", ".gitignore", ".ctxpm/AGENTS.md", "AGENTS.md", "CLAUDE.md")
	for _, alias := range []string{"AGENTS.md", "CLAUDE.md"} {
		index := gitEntrypointTest(t, root, "ls-files", "--stage", "--", alias)
		if !strings.HasPrefix(index, "120000 ") {
			t.Fatalf("%s index mode: %q", alias, index)
		}
	}
	gitEntrypointTest(t, root, "commit", "-m", "initial")
	clone := filepath.Join(t.TempDir(), "clone")
	gitEntrypointTest(t, root, "-c", "core.hooksPath="+os.DevNull, "clone", "--no-local", root, clone)
	for _, alias := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(clone, alias)
		target, err := os.Readlink(path)
		if err != nil || filepath.ToSlash(target) != ".ctxpm/AGENTS.md" {
			t.Fatalf("%s target = %q, error = %v", alias, target, err)
		}
		if content := readFileForTest(t, path); content != manifest.ManagedEntrypoint() {
			t.Fatalf("%s content does not match canonical entrypoint", alias)
		}
	}
	if _, err := os.Stat(filepath.Join(clone, ".ctxpm/dependencies")); !os.IsNotExist(err) {
		t.Fatalf("dependencies unexpectedly present: %v", err)
	}
}

func TestEntrypointMigrationPreservesGitIndex(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	gitEntrypointTest(t, root, "config", "core.excludesFile", os.DevNull)
	ignore := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignore, []byte("/AGENTS.md\n/CLAUDE.md\n# user rule\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte("# Existing instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, alias)); err != nil {
			t.Fatal(err)
		}
	}
	gitEntrypointTest(t, root, "add", "-f", ".gitignore", "AGENTS.md")
	gitEntrypointTest(t, root, "commit", "-m", "old project")
	if err := os.WriteFile(ignore, []byte("/AGENTS.md\n/CLAUDE.md\n# staged rule\n*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", ".gitignore")
	before := gitEntrypointTest(t, root, "diff", "--cached", "--binary")
	if _, err := New(root).Init(InitOptions{Agent: "claude-code", CurrentVersion: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	if after := gitEntrypointTest(t, root, "diff", "--cached", "--binary"); before != after {
		t.Fatalf("init changed Git index\nbefore:\n%s\nafter:\n%s", before, after)
	}
	got := readFileForTest(t, ignore)
	for _, legacy := range []string{"/AGENTS.md\n", "/CLAUDE.md\n"} {
		if strings.Contains(got, legacy) {
			t.Errorf("legacy ignore %q remains: %s", legacy, got)
		}
	}
	if !strings.Contains(got, "# staged rule\n*.log\n") {
		t.Errorf("unrelated rules missing: %s", got)
	}
	for _, alias := range []string{"AGENTS.md", "CLAUDE.md"} {
		target, err := os.Readlink(filepath.Join(root, alias))
		if err != nil || filepath.ToSlash(target) != ".ctxpm/AGENTS.md" {
			t.Fatalf("%s target = %q, error = %v", alias, target, err)
		}
	}
	if !strings.Contains(readFileForTest(t, filepath.Join(root, "AGENTS.md")), "# Existing instructions") {
		t.Fatal("original instructions lost")
	}
	gitEntrypointTest(t, root, "add", "AGENTS.md", "CLAUDE.md", ".ctxpm/AGENTS.md", ".gitignore")
	for _, alias := range []string{"AGENTS.md", "CLAUDE.md"} {
		if index := gitEntrypointTest(t, root, "ls-files", "--stage", "--", alias); !strings.HasPrefix(index, "120000 ") {
			t.Fatalf("%s index mode: %q", alias, index)
		}
	}
}

func TestInstallMigratesLegacyEntrypointIgnores(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Agents: []string{"claude-code"}})
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"AGENTS.md", "CLAUDE.md"} {
		if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, alias)); err != nil {
			t.Fatal(err)
		}
	}
	ignore := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(ignore, []byte("/AGENTS.md\n/CLAUDE.md\n*.tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", "ctxpm.yaml", ".gitignore")
	before := gitEntrypointTest(t, root, "diff", "--cached", "--binary")
	if _, err := New(root).Install(context.Background(), InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	if after := gitEntrypointTest(t, root, "diff", "--cached", "--binary"); before != after {
		t.Fatalf("install changed Git index")
	}
	got := readFileForTest(t, ignore)
	if strings.Contains(got, "/AGENTS.md\n") || strings.Contains(got, "/CLAUDE.md\n") || !strings.Contains(got, "*.tmp\n") {
		t.Fatalf("unexpected ignore content: %s", got)
	}
	gitEntrypointTest(t, root, "add", "AGENTS.md", "CLAUDE.md", ".ctxpm/AGENTS.md")
}

func TestDoctorAndInstallReportGitSymlinkPlaceholder(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Agents: []string{"generic"}})
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "AGENTS.md")
	if err := os.Symlink(".ctxpm/AGENTS.md", alias); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", "ctxpm.yaml", ".ctxpm/AGENTS.md", "AGENTS.md")
	gitEntrypointTest(t, root, "commit", "-m", "entrypoint")
	gitEntrypointTest(t, root, "config", "core.symlinks", "false")
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "checkout", "--", "AGENTS.md")
	before := readFileForTest(t, alias)
	result, err := New(root).EntrypointDoctor()
	if err != nil {
		t.Fatal(err)
	}
	if result.OK || !containsIssue(result.Issues, "core.symlinks") {
		t.Fatalf("doctor result = %+v", result)
	}
	_, err = New(root).Install(context.Background(), InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "core.symlinks") {
		t.Fatalf("install error = %v", err)
	}
	if got := readFileForTest(t, alias); got != before {
		t.Fatalf("install changed placeholder: %q", got)
	}
	if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("placeholder was replaced: %v %v", info, err)
	}
}

func TestInstallDoesNotReplaceOrdinaryEntrypointWithPlaceholderContent(t *testing.T) {
	root := t.TempDir()
	initGitRepoForTest(t, root)
	gitEntrypointTest(t, root, "config", "core.symlinks", "false")
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Agents: []string{"generic"}})
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(alias, []byte(".ctxpm/AGENTS.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", "AGENTS.md", "ctxpm.yaml", ".ctxpm/AGENTS.md")
	_, err := New(root).Install(context.Background(), InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "manual migration") {
		t.Fatalf("install error = %v", err)
	}
	if got := readFileForTest(t, alias); got != ".ctxpm/AGENTS.md" {
		t.Fatalf("user entrypoint changed: %q", got)
	}
	if info, err := os.Lstat(alias); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("user entrypoint was replaced: %v %v", info, err)
	}
}
