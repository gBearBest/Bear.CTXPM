package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestCanonicalCtxpmReleaseVersion(t *testing.T) {
	tests := map[string]string{
		"v0.1.14":           "v0.1.14",
		"0.1.14+abc123":     "v0.1.14",
		" v1.2.3-rc.1+sha ": "v1.2.3-rc.1",
		"0123456789abcdef":  "0123456789abcdef",
		"latest":            "latest",
		"devel":             "devel",
	}
	for input, want := range tests {
		if got := canonicalCtxpmReleaseVersion(input); got != want {
			t.Errorf("canonicalCtxpmReleaseVersion(%q) = %q, want %q", input, got, want)
		}
	}
	for _, valid := range []string{"v0.1.14", "1.2.3", "v1.2.3-rc.1"} {
		if !isCtxpmReleaseVersion(valid) {
			t.Errorf("isCtxpmReleaseVersion(%q) = false", valid)
		}
	}
	for _, invalid := range []string{"", "latest", "devel", "abc123", "v1.2"} {
		if isCtxpmReleaseVersion(invalid) {
			t.Errorf("isCtxpmReleaseVersion(%q) = true", invalid)
		}
	}
}

func TestExtractChecksumEntry(t *testing.T) {
	checksums := "aaa  ctxpm_0.1.13_linux_amd64.tar.gz\n" +
		"bbb *ctxpm_0.1.13_darwin_arm64.tar.gz\n"
	if got := extractChecksumEntry(checksums, "ctxpm_0.1.13_linux_amd64.tar.gz"); got != "aaa" {
		t.Fatalf("got %q, want aaa", got)
	}
	if got := extractChecksumEntry(checksums, "ctxpm_0.1.13_darwin_arm64.tar.gz"); got != "bbb" {
		t.Fatalf("got %q, want bbb", got)
	}
	if got := extractChecksumEntry(checksums, "missing"); got != "" {
		t.Fatalf("got %q for missing checksum, want empty", got)
	}
}

func TestCurrentExecutablePathResolvesProjectLocalSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".local", "bin", "ctxpm")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveExecutablePath(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("resolved path = %q, want project-local target %q", resolved, want)
	}
}

func TestReplaceCurrentBinaryCanBeRolledBack(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "ctxpm")
	next := filepath.Join(dir, "ctxpm-next")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(next, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}

	backup, err := replaceCurrentBinary(current, next)
	if err != nil {
		t.Fatalf("replaceCurrentBinary() error = %v", err)
	}
	if got, err := os.ReadFile(current); err != nil || string(got) != "new" {
		t.Fatalf("current binary after replace = %q, err=%v", got, err)
	}
	if err := restorePreviousBinary(current, backup); err != nil {
		t.Fatalf("restorePreviousBinary() error = %v", err)
	}
	if got, err := os.ReadFile(current); err != nil || string(got) != "old" {
		t.Fatalf("current binary after rollback = %q, err=%v", got, err)
	}
}

func TestReplaceCurrentBinaryFromRunningProcess(t *testing.T) {
	if target := os.Getenv("CTXPM_TEST_RUNNING_BINARY"); target != "" {
		if _, err := replaceCurrentBinary(target, os.Getenv("CTXPM_TEST_NEXT_BINARY")); err != nil {
			t.Fatalf("replaceCurrentBinary() from running process: %v", err)
		}
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses the external self-update helper")
	}

	dir := t.TempDir()
	running := filepath.Join(dir, "running-ctxpm-test")
	next := filepath.Join(dir, "next-ctxpm-test")
	if err := copyFile(os.Args[0], running, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(os.Args[0], next, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(running, "-test.run=^TestReplaceCurrentBinaryFromRunningProcess$")
	cmd.Env = append(os.Environ(), "CTXPM_TEST_RUNNING_BINARY="+running, "CTXPM_TEST_NEXT_BINARY="+next)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running binary self-replacement failed: %v\n%s", err, output)
	}
	if _, err := os.Stat(running + ".backup"); err != nil {
		t.Fatalf("backup after self-replacement: %v", err)
	}
}

func TestVerifyProjectLocalCLIRequiresMatchingBinary(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "active-ctxpm")
	projectCLI := filepath.Join(root, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
	if err := os.WriteFile(executable, []byte("same binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectCLI), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectCLI, []byte("same binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyProjectLocalCLI(executable, root); err != nil {
		t.Fatalf("verifyProjectLocalCLI() error = %v", err)
	}
	if err := os.WriteFile(projectCLI, []byte("different binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := verifyProjectLocalCLI(executable, root); err == nil {
		t.Fatal("verifyProjectLocalCLI() error = nil, want mismatch")
	}
}

func TestVerifyInstalledCtxpmReleaseRequiresManifestVersion(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "active-ctxpm")
	projectCLI := filepath.Join(root, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
	if err := os.WriteFile(executable, []byte("same binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(projectCLI), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectCLI, []byte("same binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Dependencies: []manifest.Resource{{
			Name: "ctxpm", Type: "skill", Layout: manifest.LayoutDir,
			Path: ".ctxpm/dependencies/skills/ctxpm", Entry: "SKILL.md", Version: "v1.2.3",
		}},
		Packages: []manifest.Resource{},
	})
	if err := verifyInstalledCtxpmRelease(executable, root, "v1.2.3"); err != nil {
		t.Fatalf("verifyInstalledCtxpmRelease() error = %v", err)
	}
	if err := verifyInstalledCtxpmRelease(executable, root, "v1.2.4"); err == nil {
		t.Fatal("verifyInstalledCtxpmRelease() error = nil, want version mismatch")
	}
}

func TestBundledSkillStatusDetectsUnexpectedDirectory(t *testing.T) {
	root := t.TempDir()
	resourceRoot := filepath.Join(root, ".ctxpm", "dependencies", "skills", "ctxpm")
	for relative, file := range bundledCtxpmSkillFiles {
		target := filepath.Join(resourceRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(file.Content), file.Mode); err != nil {
			t.Fatal(err)
		}
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Dependencies: []manifest.Resource{{
			Name: "ctxpm", Type: "skill", Layout: manifest.LayoutDir,
			Path: ".ctxpm/dependencies/skills/ctxpm", Entry: "SKILL.md", Version: "v1.2.3",
		}},
		Packages: []manifest.Resource{},
	})
	if err := os.MkdirAll(filepath.Join(resourceRoot, "obsolete", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := New(root).bundledSkillStatus("v1.2.3"); got != "update_available" {
		t.Fatalf("bundledSkillStatus() = %q, want update_available", got)
	}
}

func setLatestReleaseURL(t *testing.T, rawURL string) {
	t.Helper()
	previous := ctxpmLatestReleaseURL
	ctxpmLatestReleaseURL = rawURL
	t.Cleanup(func() { ctxpmLatestReleaseURL = previous })
}

func TestFetchLatestReleaseTagUsesGitHubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "release-token")
	t.Setenv("GH_TOKEN", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer release-token" {
			t.Fatalf("Authorization = %q, want Bearer release-token", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != githubAPIVersion {
			t.Fatalf("X-GitHub-Api-Version = %q, want %q", got, githubAPIVersion)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.22"}`))
	}))
	defer server.Close()
	setLatestReleaseURL(t, server.URL)

	tag, err := fetchLatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("fetchLatestReleaseTag() error = %v", err)
	}
	if tag != "v0.1.22" {
		t.Fatalf("tag = %q, want v0.1.22", tag)
	}
}

func TestFetchLatestReleaseTagUsesGhTokenDirectly(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "gh-env-token")

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if got := r.Header.Get("Authorization"); got != "Bearer gh-env-token" {
			t.Fatalf("Authorization = %q, want Bearer gh-env-token", got)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.1.22"}`))
	}))
	defer server.Close()
	setLatestReleaseURL(t, server.URL)

	tag, err := fetchLatestReleaseTag(context.Background())
	if err != nil {
		t.Fatalf("fetchLatestReleaseTag() error = %v", err)
	}
	if tag != "v0.1.22" || calls != 1 {
		t.Fatalf("tag=%q calls=%d, want v0.1.22 and 1 call", tag, calls)
	}
}

func TestFetchLatestReleaseTagFallsBackToGhTokenAndCLIToken(t *testing.T) {
	t.Run("falls back from GITHUB_TOKEN to GH_TOKEN on 401", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "bad-token")
		t.Setenv("GH_TOKEN", "good-gh-token")
		previous := githubCLIToken
		githubCLIToken = func(context.Context) (string, error) {
			t.Fatal("githubCLIToken should not be called when GH_TOKEN succeeds")
			return "", nil
		}
		t.Cleanup(func() { githubCLIToken = previous })

		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			switch r.Header.Get("Authorization") {
			case "Bearer bad-token":
				http.Error(w, "unauthorized", http.StatusUnauthorized)
			case "Bearer good-gh-token":
				_, _ = w.Write([]byte(`{"tag_name":"v0.1.22"}`))
			default:
				t.Errorf("unexpected Authorization header %q", r.Header.Get("Authorization"))
				http.Error(w, "forbidden", http.StatusForbidden)
			}
		}))
		defer server.Close()
		setLatestReleaseURL(t, server.URL)

		tag, err := fetchLatestReleaseTag(context.Background())
		if err != nil || tag != "v0.1.22" || calls != 2 {
			t.Fatalf("tag=%q calls=%d err=%v", tag, calls, err)
		}
	})

	t.Run("falls back to gh CLI token on 403 rate limit", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "")
		previous := githubCLIToken
		githubCLIToken = func(context.Context) (string, error) { return "cli-token", nil }
		t.Cleanup(func() { githubCLIToken = previous })

		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			switch r.Header.Get("Authorization") {
			case "":
				http.Error(w, "rate limit exceeded", http.StatusForbidden)
			case "Bearer cli-token":
				_, _ = w.Write([]byte(`{"tag_name":"v0.1.22"}`))
			default:
				t.Errorf("unexpected Authorization header %q", r.Header.Get("Authorization"))
				http.Error(w, "forbidden", http.StatusForbidden)
			}
		}))
		defer server.Close()
		setLatestReleaseURL(t, server.URL)

		tag, err := fetchLatestReleaseTag(context.Background())
		if err != nil || tag != "v0.1.22" || calls != 2 {
			t.Fatalf("tag=%q calls=%d err=%v", tag, calls, err)
		}
	})

	t.Run("does not fall back on 500", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")
		t.Setenv("GH_TOKEN", "")
		previous := githubCLIToken
		githubCLIToken = func(context.Context) (string, error) {
			t.Fatal("githubCLIToken should not be called on 500")
			return "", nil
		}
		t.Cleanup(func() { githubCLIToken = previous })

		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Error(w, "server error", http.StatusInternalServerError)
		}))
		defer server.Close()
		setLatestReleaseURL(t, server.URL)

		if _, err := fetchLatestReleaseTag(context.Background()); err == nil || calls != 1 {
			t.Fatalf("calls=%d err=%v, want 1 call and error", calls, err)
		}
	})
}
