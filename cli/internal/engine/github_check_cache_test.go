package engine

import (
	"context"
	"fmt"
	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestGitHubCheckScopeCachesPathsPerResolvedHead(t *testing.T) {
	const h1 = "1111111111111111111111111111111111111111"
	const h2 = "2222222222222222222222222222222222222222"
	const p1 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const p2 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	var mu sync.Mutex
	head, headCalls, pathCalls := h1, 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Query().Get("path")
		var sha string
		if path == "" {
			headCalls++
			sha = head
			if r.URL.Query().Get("sha") != "main" {
				t.Errorf("head ref: %s", r.URL.RawQuery)
			}
		} else {
			pathCalls++
			if r.URL.Query().Get("sha") != head {
				t.Errorf("path not pinned: %s", r.URL.RawQuery)
			}
			switch path {
			case "skills/first":
				sha = p1
			case "skills/second":
				sha = p2
			default:
				t.Errorf("unexpected path %q", path)
				sha = p1
			}
		}
		_, _ = fmt.Fprintf(w, `[{"sha":%q}]`, sha)
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)
	t.Setenv("GITHUB_TOKEN", "")
	deps := []manifest.Resource{githubSkillResource(), githubSkillResource()}
	deps[0].Source = &manifest.Source{Type: "git", URL: "https://github.com/github/awesome-copilot.git", Ref: "main", Path: "skills/first"}
	deps[1].Source = &manifest.Source{Type: "git", URL: "https://github.com/github/awesome-copilot.git", Ref: "main", Path: "skills/second"}
	root := t.TempDir()
	check := func() {
		ctx, scope := withGitHubCheckScope(context.Background(), root, deps)
		var wg sync.WaitGroup
		for i := range deps {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				version, handled, err := resolveGitHubPathVersion(ctx, deps[i])
				want := []string{p1, p2}[i]
				if err != nil || !handled || version != want {
					t.Errorf("path %d: %q, %t, %v", i, version, handled, err)
				}
			}(i)
		}
		wg.Wait()
		if err := scope.save(); err != nil {
			t.Fatal(err)
		}
	}
	check()
	if headCalls != 1 || pathCalls != 2 {
		t.Fatalf("initial requests head=%d path=%d", headCalls, pathCalls)
	}
	check()
	if headCalls != 2 || pathCalls != 2 {
		t.Fatalf("cached requests head=%d path=%d", headCalls, pathCalls)
	}
	head = h2
	check()
	if headCalls != 3 || pathCalls != 4 {
		t.Fatalf("changed head requests head=%d path=%d", headCalls, pathCalls)
	}
}

func TestGitHubCheckScopeRetriesFailedPath(t *testing.T) {
	const head = "1111111111111111111111111111111111111111"
	const version = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "" {
			_, _ = fmt.Fprintf(w, `[{"sha":%q}]`, head)
			return
		}
		calls++
		if calls == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprintf(w, `[{"sha":%q}]`, version)
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)
	t.Setenv("GITHUB_TOKEN", "")
	dep := githubSkillResource()
	deps := []manifest.Resource{dep, dep}
	root := t.TempDir()
	ctx, scope := withGitHubCheckScope(context.Background(), root, deps)
	_, _, err := resolveGitHubPathVersion(ctx, dep)
	if err == nil {
		t.Fatal("expected failed path")
	}
	if err := scope.save(); err != nil {
		t.Fatal(err)
	}
	ctx, scope = withGitHubCheckScope(context.Background(), root, deps)
	got, _, err := resolveGitHubPathVersion(ctx, dep)
	if err != nil || got != version || calls != 2 {
		t.Fatalf("retry %q, %v, calls %d", got, err, calls)
	}
	if err := scope.save(); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubCheckScopeGroupsByCredentialsAndRef(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "one")
	a := githubCheckGroupKey("owner", "repo", "main")
	if a == githubCheckGroupKey("owner", "repo", "other") {
		t.Fatal("ref not in key")
	}
	t.Setenv("GITHUB_TOKEN", "two")
	if a == githubCheckGroupKey("owner", "repo", "main") {
		t.Fatal("credentials not isolated")
	}
}

func TestGitHubCheckScopeDefaultRefAndRootPath(t *testing.T) {
	const head = "1111111111111111111111111111111111111111"
	const version = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, ref := r.URL.Query().Get("path"), r.URL.Query().Get("sha")
		if path == "" {
			if ref != "" {
				t.Errorf("default ref = %q", ref)
			}
			_, _ = fmt.Fprintf(w, `[{"sha":%q}]`, head)
			return
		}
		if path != "." || ref != head {
			t.Errorf("root path query = %s", r.URL.RawQuery)
		}
		_, _ = fmt.Fprintf(w, `[{"sha":%q}]`, version)
	}))
	defer server.Close()
	setGitHubAPIBaseURL(t, server.URL)
	t.Setenv("GITHUB_TOKEN", "")
	dep := githubSkillResource()
	dep.Source.Ref = ""
	dep.Source.Path = "."
	ctx, scope := withGitHubCheckScope(context.Background(), t.TempDir(), []manifest.Resource{dep, dep})
	got, _, err := resolveGitHubPathVersion(ctx, dep)
	if err != nil || got != version {
		t.Fatalf("root path = %q, %v", got, err)
	}
	if err := scope.save(); err != nil {
		t.Fatal(err)
	}
}
