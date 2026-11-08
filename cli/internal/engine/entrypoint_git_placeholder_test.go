package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestGitSymlinkPlaceholder(t *testing.T) {
	root := t.TempDir()
	initGitRepoForTest(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte("instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "AGENTS.md")
	if err := os.Symlink(".ctxpm/AGENTS.md", alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, args := range [][]string{{"add", "AGENTS.md", ".ctxpm/AGENTS.md"}, {"commit", "-m", "entrypoint"}, {"config", "core.symlinks", "false"}} {
		if out, err := runGit(context.Background(), append([]string{"-C", root}, args...)...); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(context.Background(), "-C", root, "checkout", "--", "AGENTS.md"); err != nil {
		t.Fatal(err)
	}
	if !gitSymlinkPlaceholder(root, "AGENTS.md", ".ctxpm/AGENTS.md") {
		t.Fatal("expected Git placeholder to be recognized")
	}
	if err := os.WriteFile(alias, []byte("user instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	if gitSymlinkPlaceholder(root, "AGENTS.md", ".ctxpm/AGENTS.md") {
		t.Fatal("user contents must not be treated as a placeholder")
	}
}

func TestEntrypointDoctorReportsBroaderIgnore(t *testing.T) {
	root := t.TempDir()
	initGitRepoForTest(t, root)
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Agents: []string{"generic"}})
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/*.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := New(root).EntrypointDoctor()
	if err != nil {
		t.Fatal(err)
	}
	if !containsIssue(result.Issues, `entrypoint "AGENTS.md" is ignored by Git`) {
		t.Fatalf("doctor result = %+v", result)
	}
}
