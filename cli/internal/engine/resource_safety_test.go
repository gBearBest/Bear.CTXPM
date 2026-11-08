package engine

import (
	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResourcePathOverlapsFindsNestedEqualAndAliases(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, ".ctxpm", "dependencies", "skills", "parent")
	if err := os.MkdirAll(base, 0755); err != nil {
		t.Fatal(err)
	}
	deps := []manifest.Resource{{Name: "parent", Path: ".ctxpm/dependencies/skills/parent"}, {Name: "child", Path: ".ctxpm/dependencies/skills/parent/child"}, {Name: "other", Path: ".ctxpm/dependencies/skills/parent-other"}, {Name: "same", Path: ".ctxpm/dependencies/skills/parent/child"}}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(base, filepath.Join(root, ".ctxpm", "dependencies", "skills", "alias")); err != nil {
			t.Fatal(err)
		}
		deps = append(deps, manifest.Resource{Name: "alias-child", Path: ".ctxpm/dependencies/skills/alias/child"})
	}
	got, err := resourcePathOverlaps(root, &manifest.Manifest{Dependencies: deps})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]resourcePathOverlap{}
	for _, overlap := range got {
		seen[overlap.Parent+":"+overlap.Child] = overlap
	}
	if _, ok := seen["parent:child"]; !ok {
		t.Fatalf("missing nested overlap: %+v", got)
	}
	if !seen["child:same"].SamePath {
		t.Fatalf("missing identical path: %+v", got)
	}
	for key := range seen {
		if strings.Contains(key, "other") {
			t.Fatalf("prefix is not overlap: %q", key)
		}
	}
	if runtime.GOOS != "windows" && !seen["child:alias-child"].SamePath {
		t.Fatalf("missing alias: %+v", got)
	}
}

func TestReplacePathSafelyPreservesExistingOnPreparationFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges")
	}
	root := t.TempDir()
	dst := filepath.Join(root, "installed")
	src := filepath.Join(root, "source")
	if err := os.MkdirAll(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "old"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(src, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := replacePathSafely(src, dst); err == nil {
		t.Fatal("expected preparation failure")
	}
	got, err := os.ReadFile(filepath.Join(dst, "old"))
	if err != nil || string(got) != "old" {
		t.Fatalf("old content damaged: %q, %v", got, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".ctxpm-replace-") {
			t.Fatalf("temporary directory retained: %s", entry.Name())
		}
	}
}

func TestReplacePathSafelyReplacesFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "source")
	dst := filepath.Join(root, "destination")
	if err := os.WriteFile(src, []byte("new"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "old"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := replacePathSafely(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "new" {
		t.Fatalf("content %q: %v", got, err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "new"), []byte("directory"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := replacePathSafely(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(dst, "new"))
	if err != nil || string(got) != "directory" {
		t.Fatalf("content %q: %v", got, err)
	}
}
