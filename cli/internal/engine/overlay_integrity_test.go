package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestOverlayIntegrityTracksResourceOwnershipAndDetectsDamage(t *testing.T) {
	root := t.TempDir()
	parent := manifest.Resource{Name: "parent", Type: "prompt", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/prompts/parent", Entry: "README.md", Source: &manifest.Source{Type: "git", URL: "repo", Path: ".", Entry: "README.md"}, Version: "parent-version"}
	child := manifest.Resource{Name: "child", Type: "skill", Layout: manifest.LayoutDir, Path: parent.Path + "/child", Entry: "SKILL.md", Source: &manifest.Source{Type: "git", URL: "repo", Path: "child", Entry: "SKILL.md"}, Version: "child-version"}
	grand := manifest.Resource{Name: "grand", Type: "rule", Layout: manifest.LayoutFile, Path: child.Path + "/rule.md", Entry: "rule.md", Source: &manifest.Source{Type: "url", URL: "url", Entry: "rule.md"}, Version: "grand-version"}
	all := []manifest.Resource{parent, child, grand}
	for _, file := range []struct{ path, content string }{{parent.Path + "/README.md", "parent"}, {child.Path + "/SKILL.md", "child"}, {grand.Path, "grand"}} {
		path := filepath.Join(root, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(file.content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	records := map[string]installRecord{}
	for _, resource := range all {
		record, err := overlayInstallRecordAt(filepath.Join(root, resource.Path), resource, all)
		if err != nil {
			t.Fatal(err)
		}
		records[resource.Name] = record
	}
	if !strings.HasPrefix(records["parent"].ContentFingerprint, "sha256overlay1:") {
		t.Fatal("parent did not use ownership fingerprint")
	}
	if err := os.WriteFile(filepath.Join(root, grand.Path), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if !overlayDependencyIsCurrent(root, parent, all, records["parent"]) || !overlayDependencyIsCurrent(root, child, all, records["child"]) || overlayDependencyIsCurrent(root, grand, all, records["grand"]) {
		t.Fatal("grandchild modification incorrectly attributed")
	}
	if err := os.WriteFile(filepath.Join(root, child.Path, "SKILL.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if !overlayDependencyIsCurrent(root, parent, all, records["parent"]) || overlayDependencyIsCurrent(root, child, all, records["child"]) {
		t.Fatal("child modification incorrectly attributed")
	}
	if err := os.WriteFile(filepath.Join(root, parent.Path, "README.md"), []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	if overlayDependencyIsCurrent(root, parent, all, records["parent"]) {
		t.Fatal("parent modification missed")
	}
}

func TestOverlayIntegrityInvalidatesLegacyAndChangedGraph(t *testing.T) {
	root := t.TempDir()
	parent := manifest.Resource{Name: "p", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/prompts/p", Entry: "README.md", Source: &manifest.Source{Type: "git", URL: "repo", Path: "."}, Version: "v"}
	child := manifest.Resource{Name: "c", Layout: manifest.LayoutFile, Path: parent.Path + "/child", Entry: "child"}
	dir := filepath.Join(root, parent.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"README.md", "child"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	legacy, err := resourceContentFingerprint(root, parent)
	if err != nil {
		t.Fatal(err)
	}
	rec := installRecord{Version: parent.Version, SourceFingerprint: sourceFingerprint(parent), ContentFingerprint: legacy}
	if overlayDependencyIsCurrent(root, parent, []manifest.Resource{parent, child}, rec) {
		t.Fatal("trusted old full-tree fingerprint")
	}
	record, err := overlayInstallRecordAt(dir, parent, []manifest.Resource{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	if overlayDependencyIsCurrent(root, parent, []manifest.Resource{parent}, record) {
		t.Fatal("changed ownership graph did not invalidate")
	}
	equal := child
	equal.Path = parent.Path
	if _, err := overlayContentFingerprint(dir, parent, []manifest.Resource{parent, equal}); err == nil {
		t.Fatal("same path accepted")
	}
}
