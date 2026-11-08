package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestUpdateChecksOnlySelectedDependencies(t *testing.T) {
	var selected, other atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/selected" {
			selected.Add(1)
		} else {
			other.Add(1)
		}
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()
	root := t.TempDir()
	disabled := false
	deps := make([]manifest.Resource, 0, 2)
	for _, name := range []string{"selected", "other"} {
		deps = append(deps, manifest.Resource{Name: name, Type: "rule", Layout: manifest.LayoutFile,
			Path: ".ctxpm/dependencies/rules/" + name + ".md", Entry: name + ".md",
			Source: &manifest.Source{Type: "url", URL: server.URL + "/" + name, Entry: name + ".md"}, Version: "sha256:old"})
	}
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, UpdatePolicy: manifest.UpdatePolicy{Enabled: &disabled}, Dependencies: deps})
	result, err := New(root).Update(context.Background(), UpdateOptions{Names: []string{"selected"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Applied) != 1 || result.Applied[0].Name != "selected" || selected.Load() != 1 || other.Load() != 0 {
		t.Fatalf("result=%+v selected requests=%d other requests=%d", result, selected.Load(), other.Load())
	}
	if _, err := New(root).Update(context.Background(), UpdateOptions{Names: []string{"missing"}, DryRun: true}); err == nil {
		t.Fatal("unknown dependency accepted")
	}
	if selected.Load() != 1 || other.Load() != 0 {
		t.Fatal("unknown dependency caused network requests")
	}
}

func TestCheckUpdatesUsesBoundedConcurrencyAndStableOrder(t *testing.T) {
	var active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		time.Sleep(30 * time.Millisecond)
		active.Add(-1)
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()
	root := t.TempDir()
	deps := make([]manifest.Resource, 12)
	for i := range deps {
		name := fmt.Sprintf("rule-%02d", i)
		deps[i] = manifest.Resource{Name: name, Type: "rule", Layout: manifest.LayoutFile,
			Path: ".ctxpm/dependencies/rules/" + name + ".md", Entry: name + ".md",
			Source: &manifest.Source{Type: "url", URL: server.URL + "/" + name, Entry: name + ".md"}}
	}
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: deps})
	result, err := New(root).CheckUpdates(context.Background(), CheckUpdatesOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := maximum.Load(); n < 2 || n > 4 {
		t.Fatalf("maximum concurrency = %d, want 2..4", n)
	}
	for i, dep := range result.Dependencies {
		if dep.Name != deps[i].Name {
			t.Fatalf("result order %d = %q", i, dep.Name)
		}
	}
}

func TestCheckUpdatesInvalidatesChangedManifestAndRetriesFailure(t *testing.T) {
	var requests atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("content"))
	}))
	defer server.Close()
	root := t.TempDir()
	dep := manifest.Resource{Name: "rule", Type: "rule", Layout: manifest.LayoutFile,
		Path: ".ctxpm/dependencies/rules/rule.md", Entry: "rule.md",
		Source: &manifest.Source{Type: "url", URL: server.URL, Entry: "rule.md"}, Version: "sha256:old"}
	m := &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: []manifest.Resource{dep}}
	writeManifestForTest(t, root, m)
	app := New(root)
	first, err := app.CheckUpdates(context.Background(), CheckUpdatesOptions{})
	if err != nil || first.Dependencies[0].Status != "unresolved" {
		t.Fatalf("first: %+v, %v", first, err)
	}
	fail.Store(false)
	second, err := app.CheckUpdates(context.Background(), CheckUpdatesOptions{})
	if err != nil || second.Dependencies[0].Status != "update_available" || requests.Load() != 2 {
		t.Fatalf("second: %+v, %v, requests=%d", second, err, requests.Load())
	}
	third, err := app.CheckUpdates(context.Background(), CheckUpdatesOptions{})
	if err != nil || third.Status != "not_due" || requests.Load() != 2 {
		t.Fatalf("cached: %+v, %v, requests=%d", third, err, requests.Load())
	}
	m.Dependencies[0].Version = "sha256:changed"
	writeManifestForTest(t, root, m)
	fourth, err := app.CheckUpdates(context.Background(), CheckUpdatesOptions{})
	if err != nil || fourth.Status != "checked" || fourth.Dependencies[0].CurrentVersion != "sha256:changed" || requests.Load() != 3 {
		t.Fatalf("changed: %+v, %v, requests=%d", fourth, err, requests.Load())
	}
}

func TestGitFetchScopeReusesFetchWithinOperation(t *testing.T) {
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	if err := os.WriteFile(filepath.Join(upstream, "file"), []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "first"}} {
		if _, err := runGit(context.Background(), append([]string{"-C", upstream}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	app := New(t.TempDir())
	ctx := withGitFetchScope(context.Background())
	repo, release, err := app.gitCacheRepository(ctx, upstream, "")
	if err != nil {
		t.Fatal(err)
	}
	first, err := runGit(ctx, "-C", repo, "rev-parse", "origin/HEAD")
	if err != nil {
		t.Fatal(err)
	}
	release()
	if err := os.WriteFile(filepath.Join(upstream, "file"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "second"}} {
		if _, err := runGit(context.Background(), append([]string{"-C", upstream}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := runGit(context.Background(), "-C", upstream, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	commit = strings.TrimSpace(commit)
	repo, release, err = app.gitCacheRepository(ctx, upstream, "")
	if err != nil {
		t.Fatal(err)
	}
	current, err := runGit(ctx, "-C", repo, "rev-parse", "origin/HEAD")
	if err != nil || current != first {
		t.Fatalf("scope fetched twice: %q, %v", current, err)
	}
	release()
	repo, release, err = app.gitCacheRepository(withGitFetchScope(context.Background()), upstream, "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	current, err = runGit(ctx, "-C", repo, "rev-parse", "origin/HEAD")
	if err != nil || strings.TrimSpace(current) != commit {
		t.Fatalf("new operation did not fetch: %q, %v", current, err)
	}
}

func TestUpdateReusesRecentCheckAndRefreshOverrides(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = w.Write([]byte("content")) }))
	defer server.Close()
	root := t.TempDir()
	dep := manifest.Resource{Name: "rule", Type: "rule", Layout: manifest.LayoutFile, Path: ".ctxpm/dependencies/rules/rule.md", Entry: "rule.md", Source: &manifest.Source{Type: "url", URL: server.URL, Entry: "rule.md"}, Version: "sha256:old"}
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: []manifest.Resource{dep}})
	app := New(root)
	if _, err := app.CheckUpdates(context.Background(), CheckUpdatesOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Update(context.Background(), UpdateOptions{Names: []string{"rule"}, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("cached plan caused %d requests", requests.Load())
	}
	if _, err := app.Update(context.Background(), UpdateOptions{Names: []string{"rule"}, DryRun: true, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("refresh caused %d requests", requests.Load())
	}
}

func TestNonGitHubCheckResolvesSelectedBranch(t *testing.T) {
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	path := filepath.Join(upstream, "rule.md")
	if err := os.WriteFile(path, []byte("base"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "base"}, {"checkout", "-b", "feature"}} {
		if _, err := runGit(context.Background(), append([]string{"-C", upstream}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte("branch"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-m", "branch"}} {
		if _, err := runGit(context.Background(), append([]string{"-C", upstream}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	expected, err := runGit(context.Background(), "-C", upstream, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(context.Background(), "-C", upstream, "checkout", "-"); err != nil {
		t.Fatal(err)
	}
	dep := manifest.Resource{Name: "rule", Type: "rule", Layout: manifest.LayoutFile, Path: ".ctxpm/dependencies/rules/rule.md", Entry: "rule.md", Source: &manifest.Source{Type: "git", URL: upstream, Path: "rule.md", Ref: "feature", Entry: "rule.md"}}
	got, err := New(t.TempDir()).resolveLatestVersion(context.Background(), dep)
	if err != nil || got != strings.TrimSpace(expected) {
		t.Fatalf("branch version=%q, expected=%q, err=%v", got, expected, err)
	}
}

func TestNestedResourceUpdateProtectsParentAndAllowsChild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("new")) }))
	defer server.Close()
	root := t.TempDir()
	parent := manifest.Resource{Name: "parent", Type: "prompt", Layout: manifest.LayoutDir, Path: ".ctxpm/dependencies/prompts/parent", Entry: "README.md", Source: &manifest.Source{Type: "git", URL: "https://github.com/example/repo", Path: ".", Entry: "README.md"}, Version: "old"}
	child := manifest.Resource{Name: "child", Type: "rule", Layout: manifest.LayoutFile, Path: parent.Path + "/child.md", Entry: "child.md", Source: &manifest.Source{Type: "url", URL: server.URL, Entry: "child.md"}, Version: "sha256:old"}
	m := &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: []manifest.Resource{parent, child}}
	writeManifestForTest(t, root, m)
	if err := os.MkdirAll(filepath.Join(root, parent.Path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, parent.Path, "README.md"), []byte("parent"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, child.Path), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	overlaps, err := resourcePathOverlaps(root, m)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkParentReplacement(root, overlaps, "parent"); err == nil {
		t.Fatal("parent replacement permitted")
	}
	if _, err := New(root).Update(context.Background(), UpdateOptions{Names: []string{"child"}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, child.Path))
	if err != nil || string(got) != "new" {
		t.Fatalf("child=%q err=%v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(root, parent.Path, "README.md"))
	if err != nil || string(got) != "parent" {
		t.Fatalf("parent=%q err=%v", got, err)
	}
}

func TestUpdateRecordsCompletedDependencyBeforeLaterFailure(t *testing.T) {
	var second atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/second" && second.Add(1) > 1 {
			http.Error(w, "failure", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("new"))
	}))
	defer server.Close()
	root := t.TempDir()
	deps := make([]manifest.Resource, 0, 3)
	for _, name := range []string{"first", "second", "third"} {
		deps = append(deps, manifest.Resource{Name: name, Type: "rule", Layout: manifest.LayoutFile, Path: ".ctxpm/dependencies/rules/" + name + ".md", Entry: name + ".md", Source: &manifest.Source{Type: "url", URL: server.URL + "/" + name, Entry: name + ".md"}, Version: "sha256:old"})
	}
	writeManifestForTest(t, root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: deps})
	result, err := New(root).Update(context.Background(), UpdateOptions{All: true})
	if err == nil {
		t.Fatal("expected second installation failure")
	}
	if result == nil || result.Status != "partial" || len(result.Applied) != 1 || result.Applied[0].Name != "first" || len(result.Skipped) != 2 || result.Skipped[0].Name != "second" || result.Skipped[0].Status != "failed" || result.Skipped[1].Name != "third" || result.Skipped[1].Status != "not_run" {
		t.Fatalf("partial result = %+v", result)
	}
	m, _, err := manifest.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Version == "sha256:old" || m.Dependencies[1].Version != "sha256:old" {
		t.Fatalf("versions after failure: %+v", m.Dependencies)
	}
}
