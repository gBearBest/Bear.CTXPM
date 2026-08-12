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
		Version: manifest.CurrentManifestVersion,
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

func TestInstallRepairsDependencyCompatibilityWithoutSource(t *testing.T) {
	root := t.TempDir()
	dependencyPath := ".ctxpm/dependencies/rules/shared-rule.md"
	compatPath := ".agents/rules/shared-rule.md"
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm/dependencies/rules"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(dependencyPath)), []byte("shared\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:          "shared-rule",
				Type:          "rule",
				Layout:        manifest.LayoutFile,
				Path:          dependencyPath,
				Entry:         "shared-rule.md",
				Compatibility: []string{compatPath},
			},
		},
	})

	app := New(root)
	result, err := app.Install(context.Background(), InstallOptions{Only: "shared-rule"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("Install() actions = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].Kind != "dependency" || result.Actions[0].Status != "linked" {
		t.Fatalf("Install() action = %+v", result.Actions[0])
	}

	linkPath := filepath.Join(root, filepath.FromSlash(compatPath))
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/dependencies/rules/shared-rule.md" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestInstallUpdatesGitignoreForCompatibilityDirectories(t *testing.T) {
	root := t.TempDir()
	packagePath := ".ctxpm/packages/rules/project-review.md"
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm/packages/rules"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(packagePath)), []byte("project review\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:          "project-review",
				Type:          "rule",
				Layout:        manifest.LayoutFile,
				Path:          packagePath,
				Entry:         "project-review.md",
				Compatibility: []string{".agents/rules/project-review.md"},
			},
		},
	})

	app := New(root)
	if _, err := app.Install(context.Background(), InstallOptions{Only: "project-review"}); err != nil {
		t.Fatalf("Install() error = %v", err)
	}

	gitignore := readFileForTest(t, filepath.Join(root, ".gitignore"))
	for _, rule := range []string{".ctxpm/dependencies/", ".ctxpm/state/", ".agents/rules/"} {
		if !strings.Contains(gitignore, rule+"\n") {
			t.Fatalf(".gitignore missing %q:\n%s", rule, gitignore)
		}
	}
}

func TestInstallIndexesCanonicalPackageFromCtxpmDirectory(t *testing.T) {
	root := t.TempDir()
	packageRoot := filepath.Join(root, ".ctxpm", "packages", "skills", "ctxpm-release")
	if err := os.MkdirAll(packageRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(packageRoot, "SKILL.md"), []byte("# release\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
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

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	found := false
	for _, pkg := range loaded.Packages {
		if pkg.Name != "ctxpm-release" {
			continue
		}
		found = true
		if pkg.Path != ".ctxpm/packages/skills/ctxpm-release" {
			t.Fatalf("package path = %q", pkg.Path)
		}
		break
	}
	if !found {
		t.Fatalf("packages = %+v", loaded.Packages)
	}
	target, err := os.Readlink(filepath.Join(root, ".agents", "skills", "ctxpm-release"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/packages/skills/ctxpm-release" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestInstallIndexesCanonicalDependencyFromCtxpmDirectory(t *testing.T) {
	root := t.TempDir()
	dependencyRoot := filepath.Join(root, ".ctxpm", "dependencies", "rules")
	if err := os.MkdirAll(dependencyRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependencyRoot, "shared-rule.md"), []byte("shared rule\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
	})

	app := New(root)
	result, err := app.Install(context.Background(), InstallOptions{Only: "shared-rule"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(result.Actions) != 1 {
		t.Fatalf("Install() actions = %d, want 1", len(result.Actions))
	}
	if result.Actions[0].Kind != "dependency" || result.Actions[0].Status != "linked" {
		t.Fatalf("Install() action = %+v", result.Actions[0])
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	found := false
	for _, dep := range loaded.Dependencies {
		if dep.Name != "shared-rule" {
			continue
		}
		found = true
		if dep.Path != ".ctxpm/dependencies/rules/shared-rule.md" {
			t.Fatalf("dependency path = %q", dep.Path)
		}
		break
	}
	if !found {
		t.Fatalf("dependencies = %+v", loaded.Dependencies)
	}
	target, err := os.Readlink(filepath.Join(root, ".agents", "rules", "shared-rule.md"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/dependencies/rules/shared-rule.md" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestInitCreatesV2Manifest(t *testing.T) {
	root := t.TempDir()
	app := New(root)
	result, err := app.Init(InitOptions{Agent: "generic"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.Agent != "generic" || result.EntrypointFile != "AGENTS.md" {
		t.Fatalf("Init() result = %+v", result)
	}
	if result.LocalCLIStatus == "" {
		t.Fatalf("LocalCLIStatus = empty")
	}
	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	if loaded.Version != manifest.CurrentManifestVersion {
		t.Fatalf("manifest version = %s", loaded.Version)
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
	if got := readFileForTest(t, filepath.Join(root, "AGENTS.md")); got != manifest.ManagedEntrypoint() {
		t.Fatalf("AGENTS.md mismatch\n--- got ---\n%s\n--- want ---\n%s", got, manifest.ManagedEntrypoint())
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

func TestInitUpdatesGitignoreForExistingCompatibilityPaths(t *testing.T) {
	root := t.TempDir()
	packagePath := ".ctxpm/packages/rules/project-review.md"
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm/packages/rules"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(packagePath)), []byte("project review\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:          "project-review",
				Type:          "rule",
				Layout:        manifest.LayoutFile,
				Path:          packagePath,
				Entry:         "project-review.md",
				Compatibility: []string{".agents/rules/project-review.md"},
			},
		},
	})

	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	gitignore := readFileForTest(t, filepath.Join(root, ".gitignore"))
	for _, rule := range []string{".ctxpm/dependencies/", ".ctxpm/state/", ".agents/rules/"} {
		if !strings.Contains(gitignore, rule+"\n") {
			t.Fatalf(".gitignore missing %q:\n%s", rule, gitignore)
		}
	}
}

func TestInitDefaultsToGenericWhenAgentOmitted(t *testing.T) {
	root := t.TempDir()
	app := New(root)
	result, err := app.Init(InitOptions{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.Agent != "generic" || result.EntrypointFile != "AGENTS.md" {
		t.Fatalf("Init() result = %+v", result)
	}
}

func TestInitDetectsExistingClaudeEntrypointWhenAgentOmitted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app := New(root)
	result, err := app.Init(InitOptions{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.Agent != "claude-code" || result.EntrypointFile != "AGENTS.md" {
		t.Fatalf("Init() result = %+v", result)
	}
	if got := readFileForTest(t, filepath.Join(root, "AGENTS.md")); got != "hello\n\n"+manifest.ManagedEntrypoint()+"\n" {
		t.Fatalf("AGENTS.md mismatch\n--- got ---\n%s", got)
	}
	if target, err := os.Readlink(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("Readlink() error = %v", err)
	} else if target != ".ctxpm/AGENTS.md" {
		t.Fatalf("CLAUDE.md symlink target = %q", target)
	}
}

func TestInitGuidesMergeWhenMultipleLegacyEntrypointsExist(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"CLAUDE.md", "ANTIGRAVITY.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	app := New(root)
	result, err := app.Init(InitOptions{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !containsIssue(result.Warnings, "merge any unique instructions into .ctxpm/AGENTS.md") {
		t.Fatalf("Init() warnings = %v", result.Warnings)
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
	want := "Intro\n\n" + manifest.ManagedEntrypoint() + "\n\nFooter\n"
	if got != want {
		t.Fatalf("entrypoint mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestInitReportsDamagedManagedBlockWithoutForce(t *testing.T) {
	root := t.TempDir()
	// Write damaged block at root; seedCanonicalEntrypoint will move it to .ctxpm/AGENTS.md.
	setupPath := filepath.Join(root, "AGENTS.md")
	original := "Intro\n\n<!-- ctxpm:begin agent=generic -->\noutdated\n"
	if err := os.WriteFile(setupPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	_, err := app.Init(InitOptions{Agent: "generic"})
	if err == nil {
		t.Fatal("Init() error = nil, want damaged block error")
	}
	if !strings.Contains(err.Error(), "managed ctxpm block is damaged") {
		t.Fatalf("Init() error = %v", err)
	}
	// File was moved to .ctxpm/AGENTS.md by seedCanonicalEntrypoint; content must be unchanged.
	movedPath := filepath.Join(root, ".ctxpm", "AGENTS.md")
	if got := readFileForTest(t, movedPath); got != original {
		t.Fatalf("damaged entrypoint should remain unchanged\n--- got ---\n%s\n--- want ---\n%s", got, original)
	}
}

func TestInitForceRepairsDamagedManagedBlock(t *testing.T) {
	root := t.TempDir()
	entrypoint := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(entrypoint, []byte("Intro\n\n<!-- ctxpm:begin agent=generic -->\noutdated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic", Force: true}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	got := readFileForTest(t, entrypoint)
	want := "Intro\n\n" + manifest.ManagedEntrypoint() + "\n"
	if got != want {
		t.Fatalf("entrypoint mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestInitRepairsExistingManifestWithoutForce(t *testing.T) {
	root := t.TempDir()
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"generic": {File: "AGENTS.md", Mode: "managed"},
		},
	})

	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	if loaded.Version != manifest.CurrentManifestVersion {
		t.Fatalf("manifest version = %s", loaded.Version)
	}
	if loaded.Project.Name != "sample" {
		t.Fatalf("project.name = %q", loaded.Project.Name)
	}
	if len(loaded.Dependencies) != 1 || loaded.Dependencies[0].Name != "ctxpm" {
		t.Fatalf("dependencies = %+v", loaded.Dependencies)
	}
	for _, relative := range []string{
		"AGENTS.md",
		".ctxpm/dependencies/skills/ctxpm/SKILL.md",
		".ctxpm/dependencies/skills/ctxpm/ctxpm-yaml.md",
		".ctxpm/dependencies/skills/ctxpm/cli/ctxpm",
		".agents/skills/ctxpm",
	} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("expected %s to exist: %v", relative, err)
		}
	}
}

func TestInitForceSyncsAgentsAndCompatibility(t *testing.T) {
	root := t.TempDir()
	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := app.Init(InitOptions{Agent: "claude-code", Force: true}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	if !hasString(loaded.Agents, "generic") || !hasString(loaded.Agents, "claude-code") {
		t.Fatalf("agents = %v", loaded.Agents)
	}
	for _, relative := range []string{".agents/skills/ctxpm", ".claude/skills/ctxpm", "AGENTS.md", "CLAUDE.md"} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("expected %s to exist: %v", relative, err)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("Readlink() error = %v", err)
	} else if target != ".ctxpm/AGENTS.md" {
		t.Fatalf("CLAUDE.md symlink target = %q", target)
	}
}

func TestInitDetectsGeminiEntrypointWhenAgentOmitted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "GEMINI.md"), []byte("gemini config\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	app := New(root)
	result, err := app.Init(InitOptions{})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if result.Agent != "gemini-cli" || result.EntrypointFile != "AGENTS.md" {
		t.Fatalf("Init() result agent=%q entrypoint=%q", result.Agent, result.EntrypointFile)
	}
	if target, err := os.Readlink(filepath.Join(root, "GEMINI.md")); err != nil {
		t.Fatalf("Readlink(GEMINI.md) error = %v", err)
	} else if target != ".ctxpm/AGENTS.md" {
		t.Fatalf("GEMINI.md symlink target = %q, want .ctxpm/AGENTS.md", target)
	}
}

func TestInitCreatesCompatibilitySymlinksForNewAgents(t *testing.T) {
	tests := []struct {
		agent      string
		entrypoint string
		compatDir  string
	}{
		{agent: "gemini-cli", entrypoint: "GEMINI.md", compatDir: ".gemini"},
		{agent: "cursor", entrypoint: "AGENTS.md", compatDir: ".cursor"},
		{agent: "windsurf", entrypoint: "AGENTS.md", compatDir: ".windsurf"},
		{agent: "kiro", entrypoint: "AGENTS.md", compatDir: ".kiro"},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			root := t.TempDir()
			app := New(root)
			if _, err := app.Init(InitOptions{Agent: tt.agent}); err != nil {
				t.Fatalf("Init() error = %v", err)
			}
			loaded, _, err := manifest.Load(root)
			if err != nil {
				t.Fatalf("manifest.Load() error = %v", err)
			}
			if !hasString(loaded.Agents, tt.agent) {
				t.Fatalf("agents = %v, want to contain %q", loaded.Agents, tt.agent)
			}
			compatPath := filepath.Join(root, tt.compatDir, "skills", "ctxpm")
			if _, err := os.Lstat(compatPath); err != nil {
				t.Fatalf("expected %s/skills/ctxpm to exist: %v", tt.compatDir, err)
			}
			target, err := os.Readlink(compatPath)
			if err != nil {
				t.Fatalf("Readlink(%s/skills/ctxpm) error = %v", tt.compatDir, err)
			}
			if target != "../../.ctxpm/dependencies/skills/ctxpm" {
				t.Fatalf("compat symlink target = %q, want ../../.ctxpm/dependencies/skills/ctxpm", target)
			}
			if _, err := os.Lstat(filepath.Join(root, tt.entrypoint)); err != nil {
				t.Fatalf("expected %s to exist: %v", tt.entrypoint, err)
			}
		})
	}
}

func TestInitForceSyncsWithGeminiAndCursor(t *testing.T) {
	root := t.TempDir()
	app := New(root)
	if _, err := app.Init(InitOptions{Agent: "generic"}); err != nil {
		t.Fatalf("Init(generic) error = %v", err)
	}
	if _, err := app.Init(InitOptions{Agent: "gemini-cli", Force: true}); err != nil {
		t.Fatalf("Init(gemini-cli) error = %v", err)
	}
	if _, err := app.Init(InitOptions{Agent: "cursor", Force: true}); err != nil {
		t.Fatalf("Init(cursor) error = %v", err)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	for _, agent := range []string{"generic", "gemini-cli", "cursor"} {
		if !hasString(loaded.Agents, agent) {
			t.Fatalf("agents = %v, want to contain %q", loaded.Agents, agent)
		}
	}
	for _, relative := range []string{
		".agents/skills/ctxpm",
		".gemini/skills/ctxpm",
		".cursor/skills/ctxpm",
		"AGENTS.md",
		"GEMINI.md",
	} {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("expected %s to exist: %v", relative, err)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "GEMINI.md")); err != nil {
		t.Fatalf("Readlink(GEMINI.md) error = %v", err)
	} else if target != ".ctxpm/AGENTS.md" {
		t.Fatalf("GEMINI.md symlink target = %q, want .ctxpm/AGENTS.md", target)
	}
}

func TestInitMigratesExistingSkillDirectoryIntoPackages(t *testing.T) {
	root := t.TempDir()
	skillRoot := filepath.Join(root, "skills", "reviewer")
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "SKILL.md"), []byte("Use README.md in this project.\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	result, err := app.Init(InitOptions{Agent: "generic"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !hasString(result.PackagesCreated, "reviewer") {
		t.Fatalf("PackagesCreated = %v", result.PackagesCreated)
	}
	if !hasString(result.MigratedResources, "skills/reviewer") {
		t.Fatalf("MigratedResources = %v", result.MigratedResources)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	var pkg manifest.Resource
	found := false
	for _, candidate := range loaded.Packages {
		if candidate.Name == "reviewer" {
			pkg = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("packages = %+v", loaded.Packages)
	}
	if pkg.Path != ".ctxpm/packages/skills/reviewer" {
		t.Fatalf("package path = %q", pkg.Path)
	}
	// Verify compatibility symlinks exist on disk (derived: .agents/skills/reviewer,
	// and the original migration path: skills/reviewer).
	for _, compatPath := range []string{".agents/skills/reviewer", "skills/reviewer"} {
		if _, err := os.Lstat(filepath.Join(root, compatPath)); err != nil {
			t.Fatalf("compatibility symlink %q missing: %v", compatPath, err)
		}
	}
	target, err := os.Readlink(filepath.Join(root, "skills", "reviewer"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../.ctxpm/packages/skills/reviewer" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestInitRegistersExistingCanonicalPackageFromCtxpmDirectory(t *testing.T) {
	root := t.TempDir()
	packagePath := filepath.Join(root, ".ctxpm", "packages", "skills", "ctxpm-release")
	if err := os.MkdirAll(packagePath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(packagePath, "SKILL.md"), []byte("# release\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	result, err := app.Init(InitOptions{Agent: "generic"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !hasString(result.PackagesCreated, "ctxpm-release") {
		t.Fatalf("PackagesCreated = %v", result.PackagesCreated)
	}
	if hasString(result.MigratedResources, ".ctxpm/packages/skills/ctxpm-release") {
		t.Fatalf("MigratedResources should not include canonical ctxpm path: %v", result.MigratedResources)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	found := false
	for _, pkg := range loaded.Packages {
		if pkg.Name != "ctxpm-release" {
			continue
		}
		found = true
		if pkg.Path != ".ctxpm/packages/skills/ctxpm-release" {
			t.Fatalf("package path = %q", pkg.Path)
		}
		break
	}
	if !found {
		t.Fatalf("packages = %+v", loaded.Packages)
	}
	target, err := os.Readlink(filepath.Join(root, ".agents", "skills", "ctxpm-release"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/packages/skills/ctxpm-release" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestInitRegistersExistingCanonicalDependencyFromCtxpmDirectory(t *testing.T) {
	root := t.TempDir()
	dependencyPath := filepath.Join(root, ".ctxpm", "dependencies", "rules")
	if err := os.MkdirAll(dependencyPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dependencyPath, "shared-rule.md"), []byte("follow shared rule\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	result, err := app.Init(InitOptions{Agent: "generic"})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !hasString(result.DependenciesCreated, "shared-rule") {
		t.Fatalf("DependenciesCreated = %v", result.DependenciesCreated)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	found := false
	for _, dep := range loaded.Dependencies {
		if dep.Name != "shared-rule" {
			continue
		}
		found = true
		if dep.Path != ".ctxpm/dependencies/rules/shared-rule.md" {
			t.Fatalf("dependency path = %q", dep.Path)
		}
		break
	}
	if !found {
		t.Fatalf("dependencies = %+v", loaded.Dependencies)
	}
	target, err := os.Readlink(filepath.Join(root, ".agents", "rules", "shared-rule.md"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/dependencies/rules/shared-rule.md" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestValidateReportsMissingPackageCompatibility(t *testing.T) {
	root := t.TempDir()
	packagePath := ".ctxpm/packages/skills/ctxpm-release"
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(packagePath)), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
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

func TestValidateReportsMissingEntrypointAlias(t *testing.T) {
	root := t.TempDir()
	// Source lives in .ctxpm/; root AGENTS.md is a symlink. CLAUDE.md is absent.
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
		t.Fatalf("WriteFile(.ctxpm/AGENTS.md) error = %v", err)
	}
	if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("Symlink(AGENTS.md) error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"claude-code"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"claude-code": {File: "AGENTS.md", Mode: "managed"},
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
	if !containsIssue(result.Issues, `entrypoint alias "CLAUDE.md" is missing`) {
		t.Fatalf("Validate() issues = %v", result.Issues)
	}
}

func TestEntrypointSyncCreatesSharedAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"claude-code"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"claude-code": {File: "CLAUDE.md", Mode: "managed"},
		},
	})

	app := New(root)
	result, err := app.EntrypointSync()
	if err != nil {
		t.Fatalf("EntrypointSync() error = %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("EntrypointSync() status = %q", result.Status)
	}
	if got := readFileForTest(t, filepath.Join(root, "AGENTS.md")); got != "hello\n\n"+manifest.ManagedEntrypoint()+"\n" {
		t.Fatalf("AGENTS.md mismatch\n--- got ---\n%s", got)
	}
	if target, err := os.Readlink(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("Readlink() error = %v", err)
	} else if target != ".ctxpm/AGENTS.md" {
		t.Fatalf("CLAUDE.md symlink target = %q", target)
	}
}

func TestEntrypointDoctorGuidesMergeForRealAliasFile(t *testing.T) {
	root := t.TempDir()
	// Source in .ctxpm/, root AGENTS.md is a symlink. CLAUDE.md exists as a real file (needs migration).
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ctxpm", "AGENTS.md"), []byte(manifest.ManagedEntrypoint()), 0o644); err != nil {
		t.Fatalf("WriteFile(.ctxpm/AGENTS.md) error = %v", err)
	}
	if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("Symlink(AGENTS.md) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("custom\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(CLAUDE.md) error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"claude-code"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"claude-code": {File: "AGENTS.md", Mode: "managed"},
		},
	})

	app := New(root)
	result, err := app.EntrypointDoctor()
	if err != nil {
		t.Fatalf("EntrypointDoctor() error = %v", err)
	}
	if result.OK {
		t.Fatalf("EntrypointDoctor() OK = true, want false")
	}
	if !containsIssue(result.Issues, "merge any unique instructions into .ctxpm/AGENTS.md") {
		t.Fatalf("EntrypointDoctor() issues = %v", result.Issues)
	}
}

func TestDetectFindsUnmanagedCompatibilityResource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills", "reviewer"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agents", "skills", "reviewer", "SKILL.md"), []byte("review carefully\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
	})

	app := New(root)
	result, err := app.Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Status != "migration_candidates_found" {
		t.Fatalf("Detect() status = %q", result.Status)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("Detect() candidates = %+v", result.Candidates)
	}
	candidate := result.Candidates[0]
	if candidate.OriginalPath != ".agents/skills/reviewer" {
		t.Fatalf("candidate original = %q", candidate.OriginalPath)
	}
	if candidate.CanonicalPath != ".ctxpm/packages/skills/reviewer" {
		t.Fatalf("candidate canonical = %q", candidate.CanonicalPath)
	}
}

func TestMigrateMovesCompatibilityResourceAndValidates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills", "reviewer"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".agents", "skills", "reviewer", "SKILL.md"), []byte("review carefully\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version:      manifest.CurrentManifestVersion,
		Project:      manifest.Project{Name: "sample"},
		Agents:       []string{"generic"},
		Dependencies: []manifest.Resource{},
		Packages:     []manifest.Resource{},
	})

	app := New(root)
	result, err := app.Migrate(MigrateOptions{Paths: []string{".agents/skills/reviewer"}})
	if err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("Migrate() status = %q", result.Status)
	}
	if !result.Validation.OK {
		t.Fatalf("Migrate() validation = %+v", result.Validation)
	}

	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	found := false
	for _, pkg := range loaded.Packages {
		if pkg.Name != "reviewer" {
			continue
		}
		found = true
		if pkg.Path != ".ctxpm/packages/skills/reviewer" {
			t.Fatalf("package path = %q", pkg.Path)
		}
		// Verify the derived compatibility symlink exists on disk.
		if _, err := os.Lstat(filepath.Join(root, ".agents", "skills", "reviewer")); err != nil {
			t.Fatalf("derived compatibility symlink missing: %v", err)
		}
		break
	}
	if !found {
		t.Fatalf("packages = %+v", loaded.Packages)
	}
	target, err := os.Readlink(filepath.Join(root, ".agents", "skills", "reviewer"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "../../.ctxpm/packages/skills/reviewer" {
		t.Fatalf("compat symlink = %q", target)
	}
	if got := readFileForTest(t, filepath.Join(root, ".ctxpm/packages/skills/reviewer", "SKILL.md")); got != "review carefully\n" {
		t.Fatalf("migrated content mismatch: %q", got)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
		Version: manifest.CurrentManifestVersion,
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
		Version: manifest.CurrentManifestVersion,
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

func TestInstallPreparesBundledCtxpmLocalCLI(t *testing.T) {
	root := t.TempDir()
	ctxpmRoot := filepath.Join(root, ".ctxpm", "dependencies", "skills", "ctxpm")
	if err := os.MkdirAll(ctxpmRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctxpmRoot, "SKILL.md"), []byte("# ctxpm\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctxpmRoot, "ctxpm-yaml.md"), []byte("version: 1.0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:          "ctxpm",
				Type:          "skill",
				Layout:        manifest.LayoutDir,
				Path:          ".ctxpm/dependencies/skills/ctxpm",
				Entry:         "SKILL.md",
				Compatibility: []string{".agents/skills/ctxpm"},
			},
		},
		Packages: []manifest.Resource{},
	})

	app := New(root)
	result, err := app.Install(context.Background(), InstallOptions{Only: "ctxpm"})
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if len(result.Actions) != 2 {
		t.Fatalf("Install() actions = %d, want 2", len(result.Actions))
	}
	if result.Actions[1].Kind != "tool" || result.Actions[1].Name != "ctxpm local cli" {
		t.Fatalf("Install() cli action = %+v", result.Actions[1])
	}
	cliPath := filepath.Join(ctxpmRoot, "cli", "ctxpm")
	info, err := os.Stat(cliPath)
	if err != nil {
		t.Fatalf("missing local CLI: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("local CLI is not executable: %v", info.Mode())
	}
}

func TestUpdateRefreshesManagedEntrypointBlocks(t *testing.T) {
	root := t.TempDir()
	server := newSingleFileUpdateServer(t, "/reviewer.md", "# reviewer\n")
	defer server.Close()

	entrypoint := filepath.Join(root, "AGENTS.md")
	original := "Intro\n\n<!-- ctxpm:begin agent=generic -->\nold instructions\n<!-- ctxpm:end -->\n\nFooter\n"
	if err := os.WriteFile(entrypoint, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:   "reviewer",
				Type:   "rule",
				Layout: manifest.LayoutFile,
				Path:   ".ctxpm/dependencies/rules/reviewer.md",
				Entry:  "reviewer.md",
				Source: &manifest.Source{
					Type:  "url",
					URL:   server.URL + "/reviewer.md",
					Entry: "reviewer.md",
				},
				Version:       "sha256:stale",
				Compatibility: []string{".agents/rules/reviewer.md"},
			},
		},
		Packages: []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"generic": {File: "AGENTS.md", Mode: "managed"},
		},
	})

	app := New(root)
	result, err := app.Update(context.Background(), UpdateOptions{Names: []string{"reviewer"}})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Status != "applied" {
		t.Fatalf("Update() status = %q", result.Status)
	}

	got := readFileForTest(t, entrypoint)
	want := "Intro\n\n" + manifest.ManagedEntrypoint() + "\n\nFooter\n"
	if got != want {
		t.Fatalf("entrypoint mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	loaded, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load() error = %v", err)
	}
	if loaded.Dependencies[0].Version == "sha256:stale" {
		t.Fatalf("dependency version was not updated")
	}
}

func TestUpdateRefreshesAllManagedEntrypoints(t *testing.T) {
	root := t.TempDir()
	server := newSingleFileUpdateServer(t, "/reviewer.md", "# reviewer\n")
	defer server.Close()

	// New-world setup: source file lives in .ctxpm/, root AGENTS.md is a symlink.
	sourceDir := filepath.Join(root, ".ctxpm")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	sourcePath := filepath.Join(sourceDir, "AGENTS.md")
	sourceBody := "Intro\n\n<!-- ctxpm:begin -->\nshared instructions\n<!-- ctxpm:end -->\n\nFooter\n"
	if err := os.WriteFile(sourcePath, []byte(sourceBody), 0o644); err != nil {
		t.Fatalf("WriteFile(.ctxpm/AGENTS.md) error = %v", err)
	}
	// AGENTS.md and CLAUDE.md at root are symlinks pointing to the source.
	if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatalf("Symlink(AGENTS.md) error = %v", err)
	}
	if err := os.Symlink(".ctxpm/AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("Symlink(CLAUDE.md) error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic", "claude-code"},
		Dependencies: []manifest.Resource{
			{
				Name:   "reviewer",
				Type:   "rule",
				Layout: manifest.LayoutFile,
				Path:   ".ctxpm/dependencies/rules/reviewer.md",
				Entry:  "reviewer.md",
				Source: &manifest.Source{
					Type:  "url",
					URL:   server.URL + "/reviewer.md",
					Entry: "reviewer.md",
				},
				Version:       "sha256:stale",
				Compatibility: []string{".agents/rules/reviewer.md"},
			},
		},
		Packages: []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"generic":     {File: "AGENTS.md", Mode: "managed"},
			"claude-code": {File: "AGENTS.md", Mode: "managed"},
		},
	})

	app := New(root)
	if _, err := app.Update(context.Background(), UpdateOptions{Names: []string{"reviewer"}}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got := readFileForTest(t, filepath.Join(root, ".ctxpm", "AGENTS.md"))
	want := "Intro\n\n" + manifest.ManagedEntrypoint() + "\n\nFooter\n"
	if got != want {
		t.Fatalf(".ctxpm/AGENTS.md mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	// Root AGENTS.md and CLAUDE.md remain valid symlinks pointing to the source.
	if target, err := os.Readlink(filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatalf("Readlink() error = %v", err)
	} else if target != ".ctxpm/AGENTS.md" {
		t.Fatalf("CLAUDE.md symlink target = %q", target)
	}
}

func TestUpdateRunsInstallAfterManifestRefresh(t *testing.T) {
	root := t.TempDir()
	server := newSingleFileUpdateServer(t, "/reviewer.md", "# reviewer\n")
	defer server.Close()

	packagePath := ".ctxpm/packages/rules/project-review.md"
	if err := os.MkdirAll(filepath.Join(root, ".ctxpm/packages/rules"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(packagePath)), []byte("project review\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	app := New(root)
	latest, err := app.resolveLatestVersion(context.Background(), manifest.Resource{
		Name:   "reviewer",
		Type:   "rule",
		Layout: manifest.LayoutFile,
		Path:   ".ctxpm/dependencies/rules/reviewer.md",
		Entry:  "reviewer.md",
		Source: &manifest.Source{
			Type:  "url",
			URL:   server.URL + "/reviewer.md",
			Entry: "reviewer.md",
		},
	})
	if err != nil {
		t.Fatalf("resolveLatestVersion() error = %v", err)
	}

	entrypoint := filepath.Join(root, "AGENTS.md")
	original := "Intro\n\n<!-- ctxpm:begin agent=generic -->\nold instructions\n<!-- ctxpm:end -->\n\nFooter\n"
	if err := os.WriteFile(entrypoint, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:   "reviewer",
				Type:   "rule",
				Layout: manifest.LayoutFile,
				Path:   ".ctxpm/dependencies/rules/reviewer.md",
				Entry:  "reviewer.md",
				Source: &manifest.Source{
					Type:  "url",
					URL:   server.URL + "/reviewer.md",
					Entry: "reviewer.md",
				},
				Version:       latest,
				Compatibility: []string{".agents/rules/reviewer.md"},
			},
		},
		Packages: []manifest.Resource{
			{
				Name:          "project-review",
				Type:          "rule",
				Layout:        manifest.LayoutFile,
				Path:          packagePath,
				Entry:         "project-review.md",
				Compatibility: []string{".agents/rules/project-review.md"},
			},
		},
		Entrypoints: map[string]manifest.Entrypoint{
			"generic": {File: "AGENTS.md", Mode: "managed"},
		},
	})

	if _, err := app.Update(context.Background(), UpdateOptions{Names: []string{"reviewer"}}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if got := readFileForTest(t, entrypoint); got != "Intro\n\n"+manifest.ManagedEntrypoint()+"\n\nFooter\n" {
		t.Fatalf("entrypoint mismatch\n--- got ---\n%s", got)
	}
	if target, err := os.Readlink(filepath.Join(root, ".agents", "rules", "project-review.md")); err != nil {
		t.Fatalf("Readlink() error = %v", err)
	} else if target != "../../.ctxpm/packages/rules/project-review.md" {
		t.Fatalf("compat symlink = %q", target)
	}
}

func TestUpdateReportsDamagedManagedBlock(t *testing.T) {
	root := t.TempDir()
	server := newSingleFileUpdateServer(t, "/reviewer.md", "# reviewer\n")
	defer server.Close()

	// Write damaged block at root; seedCanonicalEntrypoint will move it to .ctxpm/AGENTS.md.
	setupPath := filepath.Join(root, "AGENTS.md")
	original := "Intro\n\n<!-- ctxpm:begin agent=generic -->\nold instructions\n"
	if err := os.WriteFile(setupPath, []byte(original), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Dependencies: []manifest.Resource{
			{
				Name:   "reviewer",
				Type:   "rule",
				Layout: manifest.LayoutFile,
				Path:   ".ctxpm/dependencies/rules/reviewer.md",
				Entry:  "reviewer.md",
				Source: &manifest.Source{
					Type:  "url",
					URL:   server.URL + "/reviewer.md",
					Entry: "reviewer.md",
				},
				Version:       "sha256:stale",
				Compatibility: []string{".agents/rules/reviewer.md"},
			},
		},
		Packages: []manifest.Resource{},
		Entrypoints: map[string]manifest.Entrypoint{
			"generic": {File: "AGENTS.md", Mode: "managed"},
		},
	})

	app := New(root)
	_, err := app.Update(context.Background(), UpdateOptions{Names: []string{"reviewer"}})
	if err == nil {
		t.Fatal("Update() error = nil, want damaged block error")
	}
	if !strings.Contains(err.Error(), "managed ctxpm block is damaged") {
		t.Fatalf("Update() error = %v", err)
	}
	// File was moved to .ctxpm/AGENTS.md by seedCanonicalEntrypoint; content must be unchanged.
	movedPath := filepath.Join(root, ".ctxpm", "AGENTS.md")
	if got := readFileForTest(t, movedPath); got != original {
		t.Fatalf("damaged entrypoint should remain unchanged\n--- got ---\n%s\n--- want ---\n%s", got, original)
	}
}

func newSingleFileUpdateServer(t *testing.T, path, content string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(content))
	}))
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

func initGitRepoForTest(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", root},
		{"-C", root, "config", "user.email", "test@example.com"},
		{"-C", root, "config", "user.name", "Test"},
	} {
		out, err := runGit(context.Background(), args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func TestStageEntrypointMigration_NotGitRepo(t *testing.T) {
	root := t.TempDir()
	staged := stageEntrypointMigration(root)
	if len(staged) != 0 {
		t.Errorf("expected no staged ops in non-git dir, got %v", staged)
	}
}

func TestStageEntrypointMigration_StagesAfterMigration(t *testing.T) {
	root := t.TempDir()
	initGitRepoForTest(t, root)

	// Create AGENTS.md as a real file and commit it (simulates a pre-migration project).
	agentsPath := filepath.Join(root, manifest.CanonicalEntrypointFile())
	if err := os.WriteFile(agentsPath, []byte("# agents\n"), 0o644); err != nil {
		t.Fatalf("WriteFile AGENTS.md: %v", err)
	}
	if _, err := runGit(context.Background(), "-C", root, "add", manifest.CanonicalEntrypointFile()); err != nil {
		t.Fatalf("git add AGENTS.md: %v", err)
	}
	if _, err := runGit(context.Background(), "-C", root, "commit", "-m", "initial"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// Simulate migration: rename to .ctxpm/AGENTS.md and create a symlink.
	sourceRel := manifest.CanonicalEntrypointSourceFile()
	sourceAbs := filepath.Join(root, sourceRel)
	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.Rename(agentsPath, sourceAbs); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	relTarget, _ := filepath.Rel(root, sourceAbs)
	if err := os.Symlink(relTarget, agentsPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	staged := stageEntrypointMigration(root)

	wantOps := []string{
		"git rm --cached " + manifest.CanonicalEntrypointFile(),
		"git add " + manifest.CanonicalEntrypointSourceFile(),
	}
	for _, want := range wantOps {
		found := false
		for _, op := range staged {
			if op == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected staged op %q, got %v", want, staged)
		}
	}

	// Running again should be idempotent (nothing left to stage).
	staged2 := stageEntrypointMigration(root)
	for _, op := range staged2 {
		if op == "git rm --cached "+manifest.CanonicalEntrypointFile() ||
			op == "git add "+manifest.CanonicalEntrypointSourceFile() {
			t.Errorf("second call should be idempotent, but staged: %q", op)
		}
	}
}

func TestStageEntrypointMigration_AlreadyMigrated(t *testing.T) {
	root := t.TempDir()
	initGitRepoForTest(t, root)

	// Set up already-migrated state: .ctxpm/AGENTS.md committed, AGENTS.md as symlink in index.
	sourceRel := manifest.CanonicalEntrypointSourceFile()
	sourceAbs := filepath.Join(root, sourceRel)
	if err := os.MkdirAll(filepath.Dir(sourceAbs), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(sourceAbs, []byte("# agents\n"), 0o644); err != nil {
		t.Fatalf("WriteFile source: %v", err)
	}
	agentsPath := filepath.Join(root, manifest.CanonicalEntrypointFile())
	relTarget, _ := filepath.Rel(root, sourceAbs)
	if err := os.Symlink(relTarget, agentsPath); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if _, err := runGit(context.Background(), "-C", root, "add", sourceRel, manifest.CanonicalEntrypointFile()); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if _, err := runGit(context.Background(), "-C", root, "commit", "-m", "migrated"); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	staged := stageEntrypointMigration(root)
	for _, op := range staged {
		if op == "git rm --cached "+manifest.CanonicalEntrypointFile() ||
			op == "git add "+manifest.CanonicalEntrypointSourceFile() {
			t.Errorf("already-migrated repo should not stage entrypoint ops, got: %q", op)
		}
	}
}
