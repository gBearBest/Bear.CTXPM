package engine

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestEntrypointMigrationAfterLockedRelease(t *testing.T) {
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{{Name: "ctxpm", Type: "skill", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/skills/ctxpm", Entry: "SKILL.md", Source: &manifest.Source{Type: "git", URL: "https://invalid.example/ctxpm.git", Path: "resources/skills/ctxpm", Entry: "SKILL.md"}, Version: "v1.2.3"}},
	})
	ignore := filepath.Join(root, ".gitignore")
	if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignore, []byte("# staged\n"), 0644); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", ".gitignore")
	before := gitEntrypointTest(t, root, "diff", "--cached", "--binary")
	prev := ctxpmReleaseInstallHook
	defer func() { ctxpmReleaseInstallHook = prev }()
	ctxpmReleaseInstallHook = func(a *App, ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error) {
		_, unlock, err := acquireProjectWrite(ctx, a.Root)
		if err != nil {
			return nil, err
		}
		defer unlock()
		if err := os.WriteFile(ignore, []byte("# staged\n/AGENTS.md\n"), 0644); err != nil {
			return nil, err
		}
		return &CtxpmReleaseUpdateResult{Status: "updated"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := New(root).Install(ctx, InstallOptions{Only: "ctxpm", CurrentVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if got := readFileForTest(t, ignore); strings.Contains(got, "/AGENTS.md\n") {
		t.Fatalf("old ignore remains: %q", got)
	}
	if after := gitEntrypointTest(t, root, "diff", "--cached", "--binary"); before != after {
		t.Fatalf("Git index changed: before %q after %q", before, after)
	}
	found := false
	for _, action := range result.Actions {
		if action.Kind == "entrypoint" && action.Status == "migrated" {
			found = true
		}
	}
	if !found {
		t.Fatalf("migration not reported: %+v", result.Actions)
	}
}

// Git for Windows controls how a native symlink target is stored in the index.
// Verify the blob on a native runner: Go's Windows os.Symlink itself converts
// forward slashes to backslashes in the working-tree reparse point.
func TestWindowsGitStoresPortableEntrypointTarget(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires native Git for Windows")
	}
	requireEntrypointSymlinks(t)
	root := t.TempDir()
	initGitRepoForTest(t, root)
	gitEntrypointTest(t, root, "config", "core.symlinks", "true")
	if _, err := New(root).Init(InitOptions{Agent: "generic", CurrentVersion: "v1.2.3"}); err != nil {
		t.Fatal(err)
	}
	gitEntrypointTest(t, root, "add", "AGENTS.md")
	if got := gitEntrypointTest(t, root, "show", ":AGENTS.md"); got != ".ctxpm/AGENTS.md" {
		t.Fatalf("Git stored nonportable symlink target %q", got)
	}
}
