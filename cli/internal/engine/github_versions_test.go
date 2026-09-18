package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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
