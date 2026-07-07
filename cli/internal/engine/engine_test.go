package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestInstallRepairsPackageCompatibility(t *testing.T) {
	root := t.TempDir()
	packagePath := ".ctxpm/packages/skills/ctxpm-release"
	compatPath := ".agents/skills/ctxpm-release"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(packagePath)), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(packagePath), "SKILL.md"), []byte("# release\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: 1,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:          "ctxpm-release",
				Type:          "skill",
				Path:          packagePath,
				Compatibility: []string{compatPath},
			},
		},
	})

	app := New(root)
	result, err := app.Install(context.Background(), InstallOptions{Only: "ctxpm-release"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("Install() actions = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].Kind != "package" || result.Actions[0].Status != "linked" {
		t.Fatalf("Install() action = %+v", result.Actions[0])
	}

	linkPath := filepath.Join(root, filepath.FromSlash(compatPath))
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/packages/skills/ctxpm-release" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestInitCreatesV2Manifest(t *testing.T) {
	root := t.TempDir()
	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	if loaded.Version != manifest.ManifestVersion2 {
		t.Fatalf("manifest version = %d", loaded.Version)
	}
	if len(loaded.Dependencies) != 1 {
		t.Fatalf("dependencies = %d, want 1", len(loaded.Dependencies))
	}
	ctxpm := loaded.Dependencies[0]
	if ctxpm.Name != "ctxpm" || ctxpm.Layout != manifest.LayoutDir || ctxpm.Entry != "SKILL.md" {
		t.Fatalf("ctxpm dependency = %+v", ctxpm)
	}
	if ctxpm.Source == nil || ctxpm.Source.Path != "resources/skills/ctxpm" || ctxpm.Source.Entry != "SKILL.md" {
		t.Fatalf("ctxpm source = %+v", ctxpm.Source)
	}
	if got := readFileForTest(t, filepath.Join(root, "AGENTS.md")); got != manifest.ManagedEntrypoint("generic") {
		t.Fatalf("AGENTS.md mismatch\n--- got ---\n%s\n--- want ---\n%s", got, manifest.ManagedEntrypoint("generic"))
	}
	if got := readFileForTest(t, filepath.Join(root, ".ctxpm/dependencies/skills/ctxpm/SKILL.md")); got != readRepoFileForTest(t, "../../../resources/skills/ctxpm/SKILL.md") {
		t.Fatalf("installed ctxpm skill mismatch with resources/skills/ctxpm/SKILL.md")
	}
	if got := readFileForTest(t, filepath.Join(root, ".ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md")); got != readRepoFileForTest(t, "../../../resources/skills/ctxpm/ctxpm-yaml.md") {
		t.Fatalf("installed ctxpm-yaml mismatch with resources/skills/ctxpm/ctxpm-yaml.md")
	}
	for _, relative := range []string{
		".ctxpm/dependencies/skills/ctxpm/SKILL.md",
		".ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md",
		".ctxpm/dependencies/skills/ctxpm/cli/ctxpm",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("expected %s to exist: %v", relative, err)
		}
	}
	target, err := os.Readlink(filepath.Join(root, ".agents/skills/ctxpm"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/dependencies/skills/ctxpm" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestBundledCtxpmAssetsStayInSyncWithResources(t *testing.T) {
	skillResource := readRepoFileForTest(t, "../../../resources/skills/ctxpm/SKILL.md")
	yamlResource := readRepoFileForTest(t, "../../../resources/skills/ctxpm/ctxpm-yaml.md")
	if bundledCtxpmSkillContent != skillResource {
		t.Fatalf("generated bundled ctxpm skill does not match resources/skills/ctxpm/SKILL.md")
	}
	if bundledCtxpmYAMLContent != yamlResource {
		t.Fatalf("generated bundled ctxpm yaml does not match resources/skills/ctxpm/ctxpm-yaml.md")
	}
}

func TestInitReplacesManagedBlockOnly(t *testing.T) {
	root := t.TempDir()
	entrypoint := filepath.Join(root, "AGENTS.md")
	original := "Intro\n\n<!-- ctxpm:begin agent=generic -->\noutdated\n<!-- ctxpm:end -->\n\nFooter\n"
	if err := os.WriteFile(entrypoint, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	got := readFileForTest(t, entrypoint)
	want := "Intro\n\n" + manifest.ManagedEntrypoint("generic") + "\n\nFooter\n"
	if got != want {
		t.Fatalf("entrypoint mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestValidateReportsMissingPackageCompatibility(t *testing.T) {
	root := t.TempDir()
	packagePath := ".ctxpm/packages/skills/ctxpm-release"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(packagePath)), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: 1,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:          "ctxpm-release",
				Type:          "skill",
				Path:          packagePath,
				Compatibility: []string{".agents/skills/ctxpm-release"},
			},
		},
	})

	app := New(root)
	result, err := app.Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if result.OK {
		t.Fatalf("Validate() OK = true, want false")
	}
	if !containsIssue(result.Issues, `compatibility path ".agents/skills/ctxpm-release" is missing`) {
		t.Fatalf("Validate() issues = %v", result.Issues)
	}
}

func TestResolveLatestVersionForMultiFileURLUsesTreeHash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reviewer/SKILL.md":
			_, _ = w.Write([]byte("# reviewer\n"))
		case "/reviewer/rules/review.md":
			_, _ = w.Write([]byte("be strict\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	app := New(t.TempDir())
	version, err := app.resolveLatestVersion(context.Background(), manifest.Resource{
		Name:   "reviewer",
		Type:   "skill",
		Layout: manifest.LayoutDir,
		Path:   ".ctxpm/dependencies/skills/reviewer",
		Entry:  "SKILL.md",
		Source: &manifest.Source{
			Type:  "url",
			URL:   server.URL + "/reviewer/",
			Entry: "SKILL.md",
			Files: []string{"SKILL.md", "rules/review.md"},
		},
	})
	if err != nil {
		t.Fatalf("resolveLatestVersion() error = %v", err)
	}
	if !strings.HasPrefix(version, manifest.VersionPrefixSHA256Tree) {
		t.Fatalf("version = %q", version)
	}
}

func TestInstallCopiesMultiFileURLDependencyAndUpdatesVersion(t *testing.T) {
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/reviewer/SKILL.md":
			_, _ = w.Write([]byte("# reviewer\n"))
		case "/reviewer/rules/review.md":
			_, _ = w.Write([]byte("be strict\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	writeManifestForTest(t, root, &manifest.Manifest{
		Version: 2,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:   "reviewer",
				Type:   "skill",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/dependencies/skills/reviewer",
				Entry:  "SKILL.md",
				Source: &manifest.Source{
					Type:  "url",
					URL:   server.URL + "/reviewer/",
					Entry: "SKILL.md",
					Files: []string{"SKILL.md", "rules/review.md"},
				},
				Compatibility: []string{".agents/skills/reviewer"},
			},
		},
		Packages: []manifest.Resource{},
	})

	app := New(root)
	result, err := app.Install(context.Background(), InstallOptions{Only: "reviewer"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("Install() actions = %d, want 1", len(result.Actions))
	}
	if !strings.HasPrefix(result.Actions[0].Version, manifest.VersionPrefixSHA256Tree) {
		t.Fatalf("installed version = %q", result.Actions[0].Version)
	}
	if _, err := os.Stat(filepath.Join(root, ".ctxpm/dependencies/skills/reviewer/SKILL.md")); err != nil {
		t.Fatalf("missing installed entry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ctxpm/dependencies/skills/reviewer/rules/review.md")); err != nil {
		t.Fatalf("missing installed companion file: %v", err)
	}
	linkTarget, err := os.Readlink(filepath.Join(root, ".agents/skills/reviewer"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if linkTarget != "../../.ctxpm/dependencies/skills/reviewer" {
		t.Fatalf("compat symlink = %q", linkTarget)
	}
	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	if !strings.HasPrefix(loaded.Dependencies[0].Version, manifest.VersionPrefixSHA256Tree) {
		t.Fatalf("manifest version = %q", loaded.Dependencies[0].Version)
	}
}

func TestInstallArchiveDependency(t *testing.T) {
	root := t.TempDir()
	archive := bytes.Buffer{}
	zipWriter := zip.NewWriter(&archive)
	writeZipFile := func(name, content string) {
		t.Helper()
		writer, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("zipWriter.Create(%q) error = %v", name, err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatalf("writer.Write(%q) error = %v", name, err)
		}
	}
	writeZipFile("bundle/skills/analyzer/SKILL.md", "# analyzer\n")
	writeZipFile("bundle/skills/analyzer/rules/check.md", "check carefully\n")
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("zipWriter.Close() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(archive.Bytes())
	}))
	defer server.Close()

	writeManifestForTest(t, root, &manifest.Manifest{
		Version: 2,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:   "analyzer",
				Type:   "skill",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/dependencies/skills/analyzer",
				Entry:  "SKILL.md",
				Source: &manifest.Source{
					Type:  "archive",
					URL:   server.URL + "/analyzer.zip",
					Path:  "skills/analyzer",
					Entry: "SKILL.md",
				},
				Compatibility: []string{".agents/skills/analyzer"},
			},
		},
		Packages: []manifest.Resource{},
	})

	app := New(root)
	result, err := app.Install(context.Background(), InstallOptions{Only: "analyzer"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("Install() actions = %d, want 1", len(result.Actions))
	}
	if !strings.HasPrefix(result.Actions[0].Version, manifest.VersionPrefixSHA256Tree) {
		t.Fatalf("installed version = %q", result.Actions[0].Version)
	}
	if _, err := os.Stat(filepath.Join(root, ".ctxpm/dependencies/skills/analyzer/SKILL.md")); err != nil {
		t.Fatalf("missing installed archive entry: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ctxpm/dependencies/skills/analyzer/rules/check.md")); err != nil {
		t.Fatalf("missing installed archive companion file: %v", err)
	}
}

func writeManifestForTest(t *testing.T, root string, m *manifest.Manifest) {
	t.Helper()
	if m.Dependencies == nil {
		m.Dependencies = []manifest.Resource{}
	}
	if m.Packages == nil {
		m.Packages = []manifest.Resource{}
	}
	if _, err := manifest.Save(root, m); err != nil {
		t.Fatalf("manifest.Save() error = %v", err)
	}
}

func containsIssue(issues []string, want string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, want) {
			return true
		}
	}
	return false
}

func readFileForTest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return string(data)
}

func readRepoFileForTest(t *testing.T, relative string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Clean(relative))
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", relative, err)
	}
	return string(data)
}
