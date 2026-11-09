package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func writeGroupedMigrationFixture(t *testing.T, packages []manifest.Resource) string {
	t.Helper()
	root := t.TempDir()
	writeManifestForTest(t, root, &manifest.Manifest{Version: "2.0", Project: manifest.Project{Name: "migration"}, Agents: []string{"generic"}, Packages: packages})
	return root
}

func writeMigrationSkill(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, "skills", name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGroupedMigrationUsesDerivedDestination(t *testing.T) {
	for _, command := range []string{"migrate", "init"} {
		t.Run(command, func(t *testing.T) {
			root := writeGroupedMigrationFixture(t, nil)
			original := writeMigrationSkill(t, root, "MySkill")
			app := New(root)
			before := readFileForTest(t, filepath.Join(root, "ctxpm.yaml"))
			if command == "migrate" {
				preview, err := app.Migrate(MigrateOptions{All: true, DryRun: true})
				if err != nil || len(preview.Applied) != 1 || preview.Applied[0].CanonicalPath != ".ctxpm/packages/skills/myskill" {
					t.Fatal(preview, err)
				}
			} else if _, err := app.Init(InitOptions{Agent: "generic", DryRun: true}); err != nil {
				t.Fatal(err)
			}
			if readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != before || readFileForTest(t, filepath.Join(original, "SKILL.md")) != "# MySkill" {
				t.Fatal("preview modified source or manifest")
			}
			if command == "migrate" {
				if _, err := app.Migrate(MigrateOptions{All: true}); err != nil {
					t.Fatal(err)
				}
			} else if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
				t.Fatal(err)
			}
			m, _, err := manifest.Load(root)
			if err != nil || len(m.Packages) != 1 || m.Packages[0].Name != "myskill" || m.Packages[0].Path != ".ctxpm/packages/skills/myskill" {
				t.Fatal(m, err)
			}
			if readFileForTest(t, filepath.Join(root, m.Packages[0].Path, "SKILL.md")) != "# MySkill" {
				t.Fatal("migrated content changed")
			}
		})
	}
}

func TestGroupedMigrationPreflightDoesNotMoveEarlierResources(t *testing.T) {
	for _, command := range []string{"migrate", "init"} {
		for _, dry := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dry=%v", command, dry), func(t *testing.T) {
				root := writeGroupedMigrationFixture(t, []manifest.Resource{{Name: "policy", Type: "rule", Layout: "file", Entry: "notice.md", Path: ".ctxpm/packages/rules/notice.md"}})
				first := writeMigrationSkill(t, filepath.Join(root, "ai"), "FirstSkill")
				if err := os.MkdirAll(filepath.Join(root, "rules"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "rules/notice.md"), []byte("# notice"), 0644); err != nil {
					t.Fatal(err)
				}
				before := readFileForTest(t, filepath.Join(root, "ctxpm.yaml"))
				app := New(root)
				var err error
				if command == "migrate" {
					_, err = app.Migrate(MigrateOptions{All: true, DryRun: dry})
				} else {
					_, err = app.Init(InitOptions{Agent: "generic", DryRun: dry})
				}
				if err == nil || !strings.Contains(err.Error(), "same local target") {
					t.Fatalf("expected collision before moving resources: %v", err)
				}
				if readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != before || readFileForTest(t, filepath.Join(first, "SKILL.md")) != "# FirstSkill" || readFileForTest(t, filepath.Join(root, "rules/notice.md")) != "# notice" {
					t.Fatal("failed preflight changed source or manifest")
				}
				if _, err := os.Lstat(filepath.Join(root, ".ctxpm/packages/skills/firstskill")); !os.IsNotExist(err) {
					t.Fatal("earlier resource was moved before preflight completed")
				}
			})
		}
	}
}

func TestGroupedGitLifecycleKeepsIndependentVersions(t *testing.T) {
	ctx := context.Background()
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	write := func(p, value string) {
		t.Helper()
		p = filepath.Join(upstream, p)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) string {
		t.Helper()
		output, err := runGit(ctx, append([]string{"-C", upstream}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(output)
	}
	write("skills/first/SKILL.md", "first one")
	write("skills/second/SKILL.md", "second one")
	write("skills/third/SKILL.md", "third one")
	git("add", ".")
	git("commit", "-m", "initial")
	v1 := git("rev-parse", "HEAD")
	root := t.TempDir()
	raw := fmt.Sprintf(`version: "2.0"
project: {name: lifecycle}
agents: [generic]
dependencies:
  - source:
      type: git
      url: %q
    type: skill
    path: skills/{name}
    resources:
      - name: first
        version: %s # keep pin comment
      - name: second
        version: %s
packages: []
`, upstream, v1, v1)
	if err := os.WriteFile(filepath.Join(root, "ctxpm.yaml"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	app := New(root)
	if _, err := app.Install(ctx, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	before := readFileForTest(t, filepath.Join(root, "ctxpm.yaml"))
	state := loadInstallState(root)
	again, err := app.Install(ctx, InstallOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Actions) != 2 || again.Actions[0].Status != "cached" || readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != before {
		t.Fatal("repeated install changed config or fetched resources", again)
	}
	write("skills/first/SKILL.md", "first two")
	write("skills/second/SKILL.md", "second two")
	git("add", ".")
	git("commit", "-m", "update")
	v2 := git("rev-parse", "HEAD")
	if _, err := app.Update(ctx, UpdateOptions{Names: []string{"first"}, Refresh: true}); err != nil {
		t.Fatal(err)
	}
	m, _, err := manifest.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Version != v2 || m.Dependencies[1].Version != v1 {
		t.Fatal("group updated together", m.Dependencies)
	}
	if readFileForTest(t, filepath.Join(root, m.Dependencies[1].Path, "SKILL.md")) != "second one" {
		t.Fatal("unselected content changed")
	}
	if !strings.Contains(readFileForTest(t, filepath.Join(root, "ctxpm.yaml")), "# keep pin comment") {
		t.Fatal("version comment lost")
	}
	if loadInstallState(root).Dependencies["second"] != state.Dependencies["second"] {
		t.Fatal("unselected installation state changed")
	}
	if _, err := app.Add(ctx, AddOptions{SourceURL: upstream, Type: "skill", SourceType: "git", SourcePath: "skills/third"}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(readFileForTest(t, filepath.Join(root, "ctxpm.yaml")), "url:") != 1 {
		t.Fatal("source was duplicated")
	}
	// Canonical package discovery takes the full Save path, not just version edits.
	p := filepath.Join(root, ".ctxpm/packages/rules/local.md")
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("local"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Install(ctx, InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	result, err := app.Validate()
	if err != nil || !result.OK {
		t.Fatal(result, err)
	}
	if _, err := app.Remove(RemoveOptions{Name: "third", DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
	m, _, err = manifest.Load(root)
	if err != nil || len(m.Dependencies) != 2 || len(m.Packages) != 1 || m.Version != "2.0" {
		t.Fatal(m, err)
	}
}
func TestManifestMigrationIsOfflineAndPreservesInstallState(t *testing.T) {
	root := t.TempDir()
	r := manifest.Resource{Name: "reviewer", Type: "skill", Layout: "dir", Entry: "SKILL.md", Path: ".ctxpm/dependencies/skills/reviewer", Source: &manifest.Source{Type: "git", URL: "https://unreachable.invalid/repo.git", Path: "skills/reviewer", Entry: "SKILL.md"}, Version: strings.Repeat("a", 40)}
	m := &manifest.Manifest{Version: "1.0", Project: manifest.Project{Name: "migration"}, Agents: []string{"generic"}, Dependencies: []manifest.Resource{r}, Packages: []manifest.Resource{}}
	if _, err := manifest.Save(root, m); err != nil {
		t.Fatal(err)
	}
	app := New(root)
	before := readFileForTest(t, filepath.Join(root, "ctxpm.yaml"))
	preview, err := app.MigrateManifest(context.Background(), ManifestMigrationOptions{To: "2.0", DryRun: true})
	if err != nil || preview.Status != "dry_run" {
		t.Fatal(preview, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ctxpm")); !os.IsNotExist(err) {
		t.Fatal("dry run wrote project state")
	}
	if readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != before {
		t.Fatal("dry run modified config")
	}
	result, err := app.MigrateManifest(context.Background(), ManifestMigrationOptions{To: "2.0"})
	if err != nil || result.Status != "migrated" {
		t.Fatal(result, err)
	}
	next, _, err := manifest.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = manifest.EquivalentResources(m, next); err != nil {
		t.Fatal(err)
	}
	if sourceFingerprint(r) != sourceFingerprint(next.Dependencies[0]) {
		t.Fatal("source fingerprint changed")
	}
	if _, err := os.Stat(filepath.Join(root, ".ctxpm/dependencies")); !os.IsNotExist(err) {
		t.Fatal("migration installed files")
	}
	result, err = app.MigrateManifest(context.Background(), ManifestMigrationOptions{To: "2.0"})
	if err != nil || result.Status != "up_to_date" {
		t.Fatal(result, err)
	}
}
func TestUnknownSchemaWriteCommandsAndDryRunsDoNotWrite(t *testing.T) {
	for _, command := range []string{"init", "add", "install", "update", "remove"} {
		for _, dry := range []bool{false, true} {
			if command == "remove" && dry {
				continue
			}
			t.Run(fmt.Sprintf("%s/%v", command, dry), func(t *testing.T) {
				root := t.TempDir()
				raw := "version: 99.0\nproject: {name: future}\ndependencies: []\npackages: []\n"
				if err := os.WriteFile(filepath.Join(root, "ctxpm.yaml"), []byte(raw), 0644); err != nil {
					t.Fatal(err)
				}
				app := New(root)
				var err error
				switch command {
				case "init":
					_, err = app.Init(InitOptions{DryRun: dry})
				case "add":
					_, err = app.Add(context.Background(), AddOptions{DryRun: dry})
				case "install":
					_, err = app.Install(context.Background(), InstallOptions{DryRun: dry})
				case "update":
					_, err = app.Update(context.Background(), UpdateOptions{All: true, DryRun: dry})
				case "remove":
					_, err = app.Remove(RemoveOptions{Name: "x"})
				}
				if err == nil {
					t.Fatal("future schema accepted")
				}
				entries, _ := os.ReadDir(root)
				if len(entries) != 1 || readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != raw {
					t.Fatal("command wrote before schema rejection")
				}
			})
		}
	}
}
func TestGroupedURLUpdateFailurePreservesOtherResources(t *testing.T) {
	var fail atomic.Bool
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() && requests.Add(1) > 1 {
			http.Error(w, "failed", 503)
			return
		}
		if fail.Load() {
			_, _ = w.Write([]byte("new rule"))
			return
		}
		_, _ = w.Write([]byte("rule"))
	}))
	defer server.Close()
	root := t.TempDir()
	raw := fmt.Sprintf("version: '2.0'\nproject: {name: url}\ndependencies:\n  - source: {type: url, url: '%s/security.md'}\n    type: rule\n    resources:\n      - name: security\npackages: []\n", server.URL)
	if err := os.WriteFile(filepath.Join(root, "ctxpm.yaml"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	app := New(root)
	if _, err := app.Install(context.Background(), InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	before := readFileForTest(t, filepath.Join(root, "ctxpm.yaml"))
	fail.Store(true)
	if _, err := app.Update(context.Background(), UpdateOptions{Names: []string{"security"}, Refresh: true}); err == nil {
		t.Fatal("failed upstream accepted")
	}
	if readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != before {
		t.Fatal("failed update changed versions")
	}
}
func TestSchemaCapabilityGuardsOldLockedRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell executable fixture")
	}
	root := t.TempDir()
	r := bundledCtxpmResource(nil, "v9.8.7")
	if _, err := manifest.Save(root, &manifest.Manifest{Version: "1.0", Project: manifest.Project{Name: "caps"}, Dependencies: []manifest.Resource{r}, Packages: []manifest.Resource{}}); err != nil {
		t.Fatal(err)
	}
	cli := filepath.Join(root, r.Path, "cli/ctxpm")
	if err := os.MkdirAll(filepath.Dir(cli), 0755); err != nil {
		t.Fatal(err)
	}
	before := readFileForTest(t, filepath.Join(root, "ctxpm.yaml"))
	if err := os.WriteFile(cli, []byte("#!/bin/sh\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	app := New(root)
	if _, err := app.MigrateManifest(context.Background(), ManifestMigrationOptions{To: "2.0"}); err == nil {
		t.Fatal("old locked CLI accepted")
	}
	if readFileForTest(t, filepath.Join(root, "ctxpm.yaml")) != before {
		t.Fatal("failed migration changed schema")
	}
	script := "#!/bin/sh\nif [ \"$1 $2\" = 'manifest capabilities' ]; then printf '%s\\n' '{\"version\":\"v9.8.7\",\"schemas\":[\"1.0\",\"2.0\"]}'; else exit 42; fi\n"
	if err := os.WriteFile(cli, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.MigrateManifest(context.Background(), ManifestMigrationOptions{To: "2.0"}); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, "old-cli")
	if err := os.WriteFile(old, []byte("#!/bin/sh\nexit 2\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := synchronizeBundleWithUpdatedCLI(context.Background(), old, root, "v1.0.0"); err == nil {
		t.Fatal("old CLI allowed to install into new schema")
	}
}
