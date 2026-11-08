package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestResourceTreesPrepareNestedLockedOverlays(t *testing.T) {
	ctx := context.Background()
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	write := func(name, value string) {
		t.Helper()
		p := filepath.Join(upstream, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		v, err := runGit(ctx, append([]string{"-C", upstream}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(v)
	}
	write("README.md", "parent one")
	write("child/SKILL.md", "child one")
	write("child/nested/SKILL.md", "nested one")
	git("add", ".")
	git("commit", "-m", "first")
	first := git("rev-parse", "HEAD")
	write("README.md", "parent two")
	write("child/SKILL.md", "child two")
	write("child/nested/SKILL.md", "nested two")
	git("add", ".")
	git("commit", "-m", "second")
	second := git("rev-parse", "HEAD")
	root := t.TempDir()
	mk := func(name, target, src, entry string) manifest.Resource {
		return manifest.Resource{Name: name, Type: "skill", Layout: manifest.LayoutDir, Path: target, Entry: entry, Source: &manifest.Source{Type: "git", URL: upstream, Path: src, Entry: entry}, Version: first}
	}
	parent := mk("parent", ".ctxpm/dependencies/skills/parent", ".", "README.md")
	child := mk("child", parent.Path+"/child", "child", "SKILL.md")
	nested := mk("nested", child.Path+"/nested", "child/nested", "SKILL.md")
	m := &manifest.Manifest{Dependencies: []manifest.Resource{nested, child, parent}}
	groups, err := buildResourceTrees(root, m, map[string]bool{"parent": true, "nested": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Members) != 3 || groups[0].Members[0].Name != "parent" {
		t.Fatalf("groups=%+v", groups)
	}
	prepared, err := New(root).prepareResourceTree(withGitFetchScope(ctx), groups[0], map[string]string{"parent": second, "nested": second})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	for path, want := range map[string]string{"README.md": "parent two", "child/SKILL.md": "child one", "child/nested/SKILL.md": "nested two"} {
		got, err := os.ReadFile(filepath.Join(prepared.LocalPath, path))
		if err != nil || string(got) != want {
			t.Fatalf("%s=%q error=%v", path, got, err)
		}
	}
	childGroups, err := buildResourceTrees(root, m, map[string]bool{"child": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(childGroups) != 1 || childGroups[0].Root.Name != "child" || len(childGroups[0].Members) != 2 {
		t.Fatalf("child groups=%+v", childGroups)
	}
}

func TestResourceTreesRejectConflictingOrUnmanagedTargets(t *testing.T) {
	root := t.TempDir()
	dep := manifest.Resource{Name: "parent", Type: "skill", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/skills/parent", Entry: "SKILL.md", Source: &manifest.Source{Type: "git", URL: "x", Path: ".", Entry: "SKILL.md"}, Version: "old"}
	child := dep
	child.Name = "child"
	child.Path = dep.Path + "/child"
	for _, tc := range []struct {
		name string
		m    *manifest.Manifest
	}{
		{"same path", &manifest.Manifest{Dependencies: []manifest.Resource{dep, func() manifest.Resource { x := child; x.Path = dep.Path; return x }()}}},
		{"unsourced child", &manifest.Manifest{Dependencies: []manifest.Resource{dep, func() manifest.Resource { x := child; x.Source = nil; return x }()}}},
		{"package child", &manifest.Manifest{Dependencies: []manifest.Resource{dep}, Packages: []manifest.Resource{child}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildResourceTrees(root, tc.m, map[string]bool{"parent": true}); err == nil {
				t.Fatal("expected conflict")
			}
		})
	}
}

func TestSafeOverlayRejectsSymlinkAncestors(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip(err)
	}
	if err := safeOverlayAncestors(root, filepath.Join("link", "item")); err == nil {
		t.Fatal("accepted symlink ancestor")
	}
}

func TestUpdateParentRebuildsLockedChildAndTracksIntegrity(t *testing.T) {
	ctx := context.Background()
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	write := func(name, value string) {
		t.Helper()
		p := filepath.Join(upstream, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		v, err := runGit(ctx, append([]string{"-C", upstream}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(v)
	}
	write("README.md", "parent one")
	write("child/SKILL.md", "child one")
	git("add", ".")
	git("commit", "-m", "first")
	first := git("rev-parse", "HEAD")
	root := t.TempDir()
	parent := manifest.Resource{Name: "parent", Type: "skill", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/skills/parent", Entry: "README.md", Version: first, Source: &manifest.Source{Type: "git", URL: upstream, Path: ".", Entry: "README.md"}}
	child := manifest.Resource{Name: "child", Type: "skill", Layout: manifest.LayoutDir, Path: parent.Path + "/child", Entry: "SKILL.md", Version: first, Source: &manifest.Source{Type: "git", URL: upstream, Path: "child", Entry: "SKILL.md"}}
	m := &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: []manifest.Resource{parent, child}}
	writeManifestForTest(t, root, m)
	app := New(root)
	if _, err := app.Install(ctx, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	write("README.md", "parent two")
	write("child/SKILL.md", "child two")
	git("add", ".")
	git("commit", "-m", "second")
	second := git("rev-parse", "HEAD")
	if _, err := app.Update(ctx, UpdateOptions{Names: []string{"parent"}, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	updated, _, err := manifest.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	updatedParent, _ := findDependency(updated.Dependencies, "parent")
	updatedChild, _ := findDependency(updated.Dependencies, "child")
	if updatedParent.Version != second || updatedChild.Version != first {
		t.Fatalf("versions parent=%s child=%s want %s/%s", updatedParent.Version, updatedChild.Version, second, first)
	}
	for path, want := range map[string]string{"README.md": "parent two", "child/SKILL.md": "child one"} {
		b, err := os.ReadFile(filepath.Join(root, parent.Path, path))
		if err != nil || string(b) != want {
			t.Fatalf("%s=%q err=%v", path, b, err)
		}
	}
	state := loadInstallState(root)
	for _, dep := range updated.Dependencies {
		if !overlayDependencyIsCurrent(root, dep, updated.Dependencies, state.Dependencies[dep.Name]) {
			t.Fatalf("%s not current", dep.Name)
		}
	}
	if _, err := app.Install(ctx, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	write("README.md", "parent three")
	write("child/SKILL.md", "child three")
	git("add", ".")
	git("commit", "-m", "third")
	third := git("rev-parse", "HEAD")
	if _, err := app.Update(ctx, UpdateOptions{All: true, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	updated, _, err = manifest.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	updatedParent, _ = findDependency(updated.Dependencies, "parent")
	updatedChild, _ = findDependency(updated.Dependencies, "child")
	if updatedParent.Version != third || updatedChild.Version != third {
		t.Fatalf("batch versions: parent=%s child=%s", updatedParent.Version, updatedChild.Version)
	}
	data, err := os.ReadFile(filepath.Join(root, child.Path, "SKILL.md"))
	if err != nil || string(data) != "child three" {
		t.Fatalf("batch child=%q error=%v", data, err)
	}
}

func TestInstallFilteredParentPreservesUnselectedChild(t *testing.T) {
	ctx := context.Background()
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	for name, content := range map[string]string{"README.md": "parent", "child/SKILL.md": "upstream child"} {
		path := filepath.Join(upstream, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "base"}} {
		if _, err := runGit(ctx, append([]string{"-C", upstream}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	version, err := runGit(ctx, "-C", upstream, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	version = strings.TrimSpace(version)
	for _, tc := range []struct {
		name   string
		opts   InstallOptions
		exists bool
	}{{"only", InstallOptions{Only: "parent", Force: true}, true}, {"type", InstallOptions{Type: "prompt", Force: true}, true}, {"missing", InstallOptions{Only: "parent", Force: true}, false}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			parent := manifest.Resource{Name: "parent", Type: "prompt", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/prompts/parent", Entry: "README.md", Version: version, Source: &manifest.Source{Type: "git", URL: upstream, Path: ".", Entry: "README.md"}}
			child := manifest.Resource{Name: "child", Type: "skill", Layout: manifest.LayoutDir, Path: parent.Path + "/child", Entry: "SKILL.md", Version: "unavailable", Source: &manifest.Source{Type: "git", URL: filepath.Join(root, "missing-upstream"), Path: ".", Entry: "SKILL.md"}}
			writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: []manifest.Resource{parent, child}})
			if tc.exists {
				p := filepath.Join(root, child.Path, "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(p, []byte("local edit"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			result, err := New(root).Install(ctx, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Actions) != 1 || result.Actions[0].Name != "parent" {
				t.Fatalf("actions=%+v", result.Actions)
			}
			b, err := os.ReadFile(filepath.Join(root, child.Path, "SKILL.md"))
			if tc.exists {
				if err != nil || string(b) != "local edit" {
					t.Fatalf("child=%q err=%v", b, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("unselected missing child was installed: %q %v", b, err)
			}
			if _, ok := loadInstallState(root).Dependencies["child"]; ok {
				t.Fatal("unselected child state was rewritten")
			}
		})
	}
}
