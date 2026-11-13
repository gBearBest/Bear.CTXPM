package engine

import (
	"context"
	"errors"
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

func TestResolveGitHubPathVersionUsesPathAndRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/github/awesome-copilot/commits" {
			t.Fatalf("request path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != "skills/git-commit" {
			t.Fatalf("path query = %q", got)
		}
		if got := r.URL.Query().Get("sha"); got != "main" {
			t.Fatalf("sha query = %q", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "1" {
			t.Fatalf("per_page query = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("Accept = %q", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
			t.Fatalf("X-GitHub-Api-Version = %q", got)
		}
		_, _ = w.Write([]byte(`[{"sha":"E24BE77E6F203409CF99AB7D5A67E1540CB386D3"}]`))
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)

	version, handled, err := resolveGitHubPathVersion(context.Background(), githubSkillResource())
	if err != nil {
		t.Fatalf("resolveGitHubPathVersion() error = %v", err)
	}
	if !handled {
		t.Fatal("resolveGitHubPathVersion() did not recognize github.com")
	}
	if want := "e24be77e6f203409cf99ab7d5a67e1540cb386d3"; version != want {
		t.Fatalf("version = %q, want %q", version, want)
	}
}

func TestResolveGitHubPathVersionUsesOptionalToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("sha"); got != "" {
			t.Fatalf("sha query = %q, want empty default branch selector", got)
		}
		_, _ = w.Write([]byte(`[{"sha":"e24be77e6f203409cf99ab7d5a67e1540cb386d3"}]`))
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)

	resource := githubSkillResource()
	resource.Source.Ref = ""
	if _, handled, err := resolveGitHubPathVersion(context.Background(), resource); err != nil || !handled {
		t.Fatalf("resolveGitHubPathVersion() handled=%t error=%v", handled, err)
	}
}

func TestResolveGitHubPathVersionDoesNotFallBackAfterAPIFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)

	if _, handled, err := resolveGitHubPathVersion(context.Background(), githubSkillResource()); !handled || err == nil {
		t.Fatalf("resolveGitHubPathVersion() handled=%t error=%v, want handled API error", handled, err)
	}
}

func TestResolveLatestVersionUsesGitHubAPIForUpToDateDependency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/github/awesome-copilot/commits" {
			t.Fatalf("unexpected request path %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"sha":"e24be77e6f203409cf99ab7d5a67e1540cb386d3"}]`))
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)

	root := t.TempDir()
	resource := githubSkillResource()
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{resource},
		Packages:     []manifest.Resource{},
	})

	result, err := New(root).CheckUpdates(context.Background(), CheckUpdatesOptions{Force: true})
	if err != nil {
		t.Fatalf("CheckUpdates() error = %v", err)
	}
	if len(result.Dependencies) != 1 {
		t.Fatalf("dependencies = %+v", result.Dependencies)
	}
	dependency := result.Dependencies[0]
	if dependency.Status != "up_to_date" || dependency.LatestVersion != resource.Version {
		t.Fatalf("dependency = %+v", dependency)
	}
}

func TestGitHubRepositoryRecognizesCloneURLs(t *testing.T) {
	for _, rawURL := range []string{
		"https://github.com/github/awesome-copilot.git",
		"ssh://git@github.com/github/awesome-copilot.git",
		"git@github.com:github/awesome-copilot.git",
	} {
		owner, repo, ok := githubRepository(rawURL)
		if !ok || owner != "github" || repo != "awesome-copilot" {
			t.Errorf("githubRepository(%q) = %q, %q, %t", rawURL, owner, repo, ok)
		}
	}
	if _, _, ok := githubRepository("https://github.example.com/github/awesome-copilot.git"); ok {
		t.Error("githubRepository() recognized a GitHub Enterprise URL")
	}
}

func githubSkillResource() manifest.Resource {
	return manifest.Resource{
		Name:    "git-commit",
		Type:    "skill",
		Layout:  manifest.LayoutDir,
		Path:    ".ctxpm/dependencies/skills/git-commit",
		Entry:   "SKILL.md",
		Version: "e24be77e6f203409cf99ab7d5a67e1540cb386d3",
		Source: &manifest.Source{
			Type:  "git",
			URL:   "https://github.com/github/awesome-copilot.git",
			Ref:   "main",
			Path:  "skills/git-commit",
			Entry: "SKILL.md",
		},
	}
}

func setGitHubAPIBaseURL(t *testing.T, baseURL string) {
	t.Helper()
	previous := githubAPIBaseURL
	githubAPIBaseURL = baseURL
	t.Cleanup(func() { githubAPIBaseURL = previous })
}

func TestGitHubVersionUsesGhCredentialsAfterAPIRejection(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "incorrect-token")
	previous := githubCLIToken
	githubCLIToken = func(context.Context) (string, error) { return "gh-token", nil }
	t.Cleanup(func() { githubCLIToken = previous })
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.Header.Get("Authorization") {
		case "Bearer incorrect-token":
			http.Error(w, "denied", http.StatusNotFound)
		case "Bearer gh-token":
			_, _ = w.Write([]byte(`[{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]`))
		default:
			t.Errorf("unexpected authorization")
			http.Error(w, "denied", 403)
		}
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)
	version, handled, err := New(t.TempDir()).resolveGitHubVersion(context.Background(), githubSkillResource())
	if err != nil || !handled || version != strings.Repeat("a", 40) || calls != 2 {
		t.Fatalf("version=%q handled=%v calls=%d err=%v", version, handled, calls, err)
	}
}

func TestGitHubVersionFallsBackToGitPathHistory(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	previous := githubCLIToken
	githubCLIToken = func(context.Context) (string, error) { return "", errors.New("not logged in") }
	t.Cleanup(func() { githubCLIToken = previous })
	var slow atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			<-r.Context().Done()
			return
		}
		http.Error(w, "denied", http.StatusNotFound)
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	path := filepath.Join(upstream, "skills", "git-commit")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("first"), 0644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		result, err := runGit(context.Background(), append([]string{"-C", upstream}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(result)
	}
	run("add", ".")
	run("commit", "-m", "resource")
	expected := run("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(upstream, "other"), []byte("later"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "unrelated")
	run("branch", "feature")
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte("[url \""+filepath.ToSlash(upstream)+"\"]\n insteadOf = https://github.com/github/awesome-copilot.git\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	resource := githubSkillResource()
	resource.Source.Ref = "feature"
	app := New(t.TempDir())
	got, err := app.resolveLatestVersion(context.Background(), resource)
	if err != nil || got != expected {
		t.Fatalf("version=%q want=%q err=%v", got, expected, err)
	}
	resolved, err := app.resolveResource(withGitFetchScope(context.Background()), resource, resolveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Version != expected {
		t.Fatalf("resolved version=%q want=%q", resolved.Version, expected)
	}
	content, err := os.ReadFile(filepath.Join(resolved.LocalPath, "SKILL.md"))
	resolved.Close()
	if err != nil || string(content) != "first" {
		t.Fatalf("resolved content=%q err=%v", content, err)
	}
	// Reuse the same cache after the upstream changes its default branch.
	resource.Source.Ref = ""
	if got, err := app.resolveLatestVersion(context.Background(), resource); err != nil || got != expected {
		t.Fatalf("initial default version=%q err=%v", got, err)
	}
	run("checkout", "feature")
	if err := os.WriteFile(filepath.Join(path, "SKILL.md"), []byte("feature"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "feature resource")
	featureVersion := run("rev-parse", "HEAD")
	if got, err := app.resolveLatestVersion(context.Background(), resource); err != nil || got != featureVersion {
		t.Fatalf("changed default version=%q want=%q err=%v", got, featureVersion, err)
	}
	// A stalled API must leave the parent context usable for Git fallback.
	oldTimeout := githubAPILookupTimeout
	githubAPILookupTimeout = 50 * time.Millisecond
	t.Cleanup(func() { githubAPILookupTimeout = oldTimeout })
	slow.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if got, err := app.resolveLatestVersion(ctx, resource); err != nil || got != featureVersion {
		t.Fatalf("timeout fallback version=%q want=%q err=%v", got, featureVersion, err)
	}
	slow.Store(false)
	resource.Source.Path = "missing"
	if _, err := app.resolveLatestVersion(context.Background(), resource); err == nil {
		t.Fatal("missing path should not resolve")
	}
}

func TestGitHubVersionUsesGhTokenEnvironment(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-env-token")
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer gh-env-token" {
			http.Error(w, "denied", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`[{"sha":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]`))
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)
	got, handled, err := New(t.TempDir()).resolveGitHubVersion(context.Background(), githubSkillResource())
	if err != nil || !handled || got != strings.Repeat("b", 40) || calls != 2 {
		t.Fatalf("version=%q handled=%v calls=%d err=%v", got, handled, calls, err)
	}
}
