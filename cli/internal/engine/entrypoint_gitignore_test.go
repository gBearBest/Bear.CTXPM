package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestInstallMigratesIgnoreForCreatedAndRepairedAlias(t *testing.T) {
	requireEntrypointSymlinks(t)
	for _, initial := range []string{"missing", "different link"} {
		t.Run(initial, func(t *testing.T) {
			root := t.TempDir()
			initGitRepoForTest(t, root)
			writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Agents: []string{"generic"}})
			if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
				t.Fatal(err)
			}
			if initial == "different link" {
				if err := os.Symlink("other.md", filepath.Join(root, "AGENTS.md")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/AGENTS.md\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := New(root).Install(context.Background(), InstallOptions{})
			if err != nil {
				t.Fatal(err)
			}
			ignored := strings.Contains(readFileForTest(t, filepath.Join(root, ".gitignore")), "/AGENTS.md\n")
			if ignored {
				t.Fatalf("ignored = %v for %s", ignored, initial)
			}
			before := readFileForTest(t, filepath.Join(root, ".gitignore"))
			if _, err := New(root).Install(context.Background(), InstallOptions{}); err != nil {
				t.Fatal(err)
			}
			if after := readFileForTest(t, filepath.Join(root, ".gitignore")); before != after {
				t.Fatalf("repeated install changed ignore: before %q, after %q", before, after)
			}
		})
	}
}

func TestInitMigratesIgnoreForOriginalRegularEntrypoint(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# User instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(root).Init(InitOptions{Agent: "generic", CurrentVersion: "v1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(readFileForTest(t, filepath.Join(root, ".gitignore")), "/AGENTS.md\n") {
		t.Fatal("regular file's ignore rule remains after migration")
	}
	if !strings.Contains(readFileForTest(t, filepath.Join(root, ".ctxpm", "AGENTS.md")), "# User instructions") {
		t.Fatal("original instructions were lost")
	}
}

func TestInitMigratesLegacySymlinkContent(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	legacy := filepath.Join(root, "legacy.md")
	if err := os.WriteFile(legacy, []byte("# Legacy instructions\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("legacy.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root).Init(InitOptions{Agent: "generic", CurrentVersion: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	if got := readFileForTest(t, legacy); got != "# Legacy instructions\n" {
		t.Fatalf("legacy target changed: %q", got)
	}
	if got := readFileForTest(t, filepath.Join(root, "AGENTS.md")); !strings.Contains(got, "# Legacy instructions") {
		t.Fatalf("legacy instructions lost: %q", got)
	}
	if got := readFileForTest(t, filepath.Join(root, ".gitignore")); strings.Contains(got, "/AGENTS.md\n") {
		t.Fatalf("ignore rule remains: %q", got)
	}
}

func TestInitMigratesEntrypointsWithSharedSource(t *testing.T) {
	requireEntrypointSymlinks(t)
	for _, mode := range []string{"root file", "other root file", "shared external", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			initGitRepoForTest(t, root)
			agents, claude := filepath.Join(root, "AGENTS.md"), filepath.Join(root, "CLAUDE.md")
			write := func(path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("# Shared instructions\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			link := func(target, path string) {
				t.Helper()
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			}
			switch mode {
			case "root file":
				write(agents)
				link("AGENTS.md", claude)
			case "other root file":
				write(claude)
				link("CLAUDE.md", agents)
			case "shared external":
				write(filepath.Join(root, "legacy.md"))
				link("legacy.md", agents)
				link("legacy.md", claude)
			case "hardlink":
				write(agents)
				if err := os.Link(agents, claude); err != nil {
					t.Skipf("hardlinks unavailable: %v", err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/AGENTS.md\n/CLAUDE.md\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := New(root).Init(InitOptions{Agent: "claude-code", CurrentVersion: "v1.2.3"}); err != nil {
				t.Fatal(err)
			}
			for _, alias := range []string{agents, claude} {
				if target, err := os.Readlink(alias); err != nil || filepath.ToSlash(target) != ".ctxpm/AGENTS.md" {
					t.Fatalf("%s target %q: %v", alias, target, err)
				}
				if got := readFileForTest(t, alias); !strings.Contains(got, "# Shared instructions") {
					t.Fatalf("instructions lost: %q", got)
				}
			}
			if got := readFileForTest(t, filepath.Join(root, ".gitignore")); strings.Contains(got, "/AGENTS.md\n") || strings.Contains(got, "/CLAUDE.md\n") {
				t.Fatalf("old ignore rules remain: %q", got)
			}
			if mode == "shared external" && readFileForTest(t, filepath.Join(root, "legacy.md")) != "# Shared instructions\n" {
				t.Fatal("external source was modified")
			}
		})
	}
}

func TestInitPreservesDistinctEntrypointSources(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	for name, content := range map[string]string{"AGENTS.md": "agents", "CLAUDE.md": "claude"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := New(root).Init(InitOptions{Agent: "claude-code", CurrentVersion: "v1.2.3"}); err == nil || !strings.Contains(err.Error(), "manual migration") {
		t.Fatalf("expected conflict, got %v", err)
	}
	for name, content := range map[string]string{"AGENTS.md": "agents", "CLAUDE.md": "claude"} {
		if got := readFileForTest(t, filepath.Join(root, name)); got != content {
			t.Fatalf("%s changed: %q", name, got)
		}
	}
}

func TestInstallPreservesConflictingLegacySymlink(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(root, "legacy.md"), []byte("unique instructions"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("legacy.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("/AGENTS.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New(root).Install(context.Background(), InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "manual migration") {
		t.Fatalf("expected conflict, got %v", err)
	}
	if target, err := os.Readlink(filepath.Join(root, "AGENTS.md")); err != nil || target != "legacy.md" {
		t.Fatalf("legacy link changed: %q, %v", target, err)
	}
	if got := readFileForTest(t, filepath.Join(root, ".gitignore")); !strings.Contains(got, "/AGENTS.md\n") {
		t.Fatalf("ignore rule changed: %q", got)
	}
}
