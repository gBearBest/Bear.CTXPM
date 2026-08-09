package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCurrentVersionMultiFileURLResource(t *testing.T) {
	resource := Resource{
		Name:   "reviewer",
		Type:   "skill",
		Layout: LayoutDir,
		Path:   ".ctxpm/dependencies/skills/reviewer",
		Entry:  "SKILL.md",
		Source: &Source{
			Type:  "url",
			URL:   "https://example.com/reviewer/",
			Entry: "SKILL.md",
			Files: []string{"SKILL.md", "rules/review.md"},
		},
	}
	if err := resource.validate("dependency"); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestValidateMemoryDirectoryDefaultsToMemoryEntry(t *testing.T) {
	resource := Resource{
		Name: "project-memory",
		Type: "memory",
		Path: ".ctxpm/packages/memories/project-memory",
	}
	if err := resource.validate("package"); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
	if got := resource.EffectiveEntry(); got != "MEMORY.md" {
		t.Fatalf("EffectiveEntry() = %q, want MEMORY.md", got)
	}
}

func TestTypeDirReturnsMemoriesForMemoryResources(t *testing.T) {
	if got := TypeDir("memory"); got != "memories" {
		t.Fatalf("TypeDir(memory) = %q, want memories", got)
	}
}

func TestValidateCurrentVersionRejectsSingleFileURLDirectoryLayout(t *testing.T) {
	resource := Resource{
		Name:   "reviewer",
		Type:   "skill",
		Layout: LayoutDir,
		Path:   ".ctxpm/dependencies/skills/reviewer",
		Entry:  "SKILL.md",
		Source: &Source{
			Type:  "url",
			URL:   "https://example.com/reviewer.md",
			Entry: "SKILL.md",
		},
	}
	if err := resource.validate("dependency"); err == nil {
		t.Fatalf("validate() error = nil, want rejection")
	}
}

func TestSaveUsesTwoSpaceIndent(t *testing.T) {
	root := t.TempDir()
	m := &Manifest{
		Version: CurrentManifestVersion,
		Project: Project{Name: "sample"},
		Agents:  []string{"generic"},
		UpdatePolicy: UpdatePolicy{
			Interval: "1d",
		},
		Dependencies: []Resource{},
		Packages:     []Resource{},
		// Entrypoints is deprecated and should be stripped on save.
		Entrypoints: map[string]Entrypoint{
			"generic": {
				File: "AGENTS.md",
				Mode: "managed",
			},
		},
	}

	if _, err := Save(root, m); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got := readManifest(t, root)
	want := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"agents:\n" +
		"  - generic\n" +
		"update_policy:\n" +
		"  interval: 1d\n" +
		"dependencies: []\n" +
		"packages: []\n"
	if got != want {
		t.Fatalf("Save() wrote unexpected YAML\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSyncVersionRewritesTopLevelVersion(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 2\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages: []\n"
	writeManifest(t, root, original)

	changed, err := SyncVersion(root)
	if err != nil {
		t.Fatalf("SyncVersion() error = %v", err)
	}
	if !changed {
		t.Fatalf("SyncVersion() changed = false, want true")
	}

	got := readManifest(t, root)
	want := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages: []\n"
	if got != want {
		t.Fatalf("SyncVersion() wrote unexpected YAML\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestUpdateResourceVersionsNoChangeLeavesManifestUntouched(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"\n" +
		"project:\n" +
		"  name: sample\n" +
		"\n" +
		"dependencies:\n" +
		"  - name: ctxpm\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/ctxpm\n" +
		"    source:\n" +
		"      type: git\n" +
		"      url: https://example.com/repo.git\n" +
		"      path: resources/skills/ctxpm\n" +
		"    version: abc123 # pinned\n" +
		"\n" +
		"packages: []\n"
	writeManifest(t, root, original)

	changed, err := UpdateResourceVersions(root, map[string]string{"ctxpm": "abc123"})
	if err != nil {
		t.Fatalf("UpdateResourceVersions() error = %v", err)
	}
	if changed {
		t.Fatalf("UpdateResourceVersions() changed = true, want false")
	}

	got := readManifest(t, root)
	if got != original {
		t.Fatalf("manifest changed unexpectedly\n--- got ---\n%s\n--- want ---\n%s", got, original)
	}
}

func TestUpdateResourceVersionsReplacesOnlyVersionValue(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"\n" +
		"# project comment\n" +
		"project:\n" +
		"  name: sample\n" +
		"\n" +
		"dependencies:\n" +
		"  - name: ctxpm\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/ctxpm\n" +
		"    source:\n" +
		"      type: git\n" +
		"      url: https://example.com/repo.git\n" +
		"      path: resources/skills/ctxpm\n" +
		"    version: abc123 # pinned\n" +
		"    compatibility:\n" +
		"      - .agents/skills/ctxpm\n" +
		"\n" +
		"packages: []\n"
	expected := "" +
		"version: 1.0\n" +
		"\n" +
		"# project comment\n" +
		"project:\n" +
		"  name: sample\n" +
		"\n" +
		"dependencies:\n" +
		"  - name: ctxpm\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/ctxpm\n" +
		"    source:\n" +
		"      type: git\n" +
		"      url: https://example.com/repo.git\n" +
		"      path: resources/skills/ctxpm\n" +
		"    version: def456 # pinned\n" +
		"    compatibility:\n" +
		"      - .agents/skills/ctxpm\n" +
		"\n" +
		"packages: []\n"
	writeManifest(t, root, original)

	changed, err := UpdateResourceVersions(root, map[string]string{"ctxpm": "def456"})
	if err != nil {
		t.Fatalf("UpdateResourceVersions() error = %v", err)
	}
	if !changed {
		t.Fatalf("UpdateResourceVersions() changed = false, want true")
	}

	got := readManifest(t, root)
	if got != expected {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
}

func TestUpdateResourceVersionsInsertsMissingVersionBeforeCompatibility(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: ctxpm\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/ctxpm\n" +
		"    source:\n" +
		"      type: git\n" +
		"      url: https://example.com/repo.git\n" +
		"      path: resources/skills/ctxpm\n" +
		"    compatibility:\n" +
		"      - .agents/skills/ctxpm\n" +
		"packages: []\n"
	expected := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: ctxpm\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/ctxpm\n" +
		"    source:\n" +
		"      type: git\n" +
		"      url: https://example.com/repo.git\n" +
		"      path: resources/skills/ctxpm\n" +
		"    version: def456\n" +
		"    compatibility:\n" +
		"      - .agents/skills/ctxpm\n" +
		"packages: []\n"
	writeManifest(t, root, original)

	changed, err := UpdateResourceVersions(root, map[string]string{"ctxpm": "def456"})
	if err != nil {
		t.Fatalf("UpdateResourceVersions() error = %v", err)
	}
	if !changed {
		t.Fatalf("UpdateResourceVersions() changed = false, want true")
	}

	got := readManifest(t, root)
	if got != expected {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
}

func TestAddDependencyReordersOnlyDependencySection(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: zebra\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/zebra\n" +
		"    version: zzz\n" +
		"  - name: beta\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/beta\n" +
		"    version: bbb\n" +
		"\n" +
		"packages:\n" +
		"  # keep package section untouched\n" +
		"  - name: local\n" +
		"    type: skill\n" +
		"    path: .ctxpm/packages/skills/local\n"
	expected := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: beta\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/beta\n" +
		"    version: bbb\n" +
		"  - name: gamma\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/gamma\n" +
		"    source:\n" +
		"      type: git\n" +
		"      url: https://example.com/repo.git\n" +
		"      path: skills/gamma\n" +
		"    version: ggg\n" +
		"  - name: zebra\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/zebra\n" +
		"    version: zzz\n" +
		"\n" +
		"packages:\n" +
		"  # keep package section untouched\n" +
		"  - name: local\n" +
		"    type: skill\n" +
		"    path: .ctxpm/packages/skills/local\n"
	writeManifest(t, root, original)

	changed, err := AddDependency(root, Resource{
		Name:    "gamma",
		Type:    "skill",
		Path:    ".ctxpm/dependencies/skills/gamma",
		Version: "ggg",
		Source: &Source{
			Type: "git",
			URL:  "https://example.com/repo.git",
			Path: "skills/gamma",
		},
	})
	if err != nil {
		t.Fatalf("AddDependency() error = %v", err)
	}
	if !changed {
		t.Fatalf("AddDependency() changed = false, want true")
	}
	got := readManifest(t, root)
	if got != expected {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
}

func TestAddDependencyExpandsInlineEmptySection(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages: []\n"
	expected := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: ctxpm\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/ctxpm\n" +
		"    version: abc123\n" +
		"packages: []\n"
	writeManifest(t, root, original)

	changed, err := AddDependency(root, Resource{
		Name:    "ctxpm",
		Type:    "skill",
		Path:    ".ctxpm/dependencies/skills/ctxpm",
		Version: "abc123",
	})
	if err != nil {
		t.Fatalf("AddDependency() error = %v", err)
	}
	if !changed {
		t.Fatalf("AddDependency() changed = false, want true")
	}

	got := readManifest(t, root)
	if got != expected {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
}

func TestRemoveDependencyDeletesOnlyTargetBlock(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: alpha\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/alpha\n" +
		"    version: aaa\n" +
		"  - name: beta\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/beta\n" +
		"    version: bbb\n" +
		"\n" +
		"packages:\n" +
		"  - name: local\n" +
		"    type: skill\n" +
		"    path: .ctxpm/packages/skills/local\n"
	expected := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies:\n" +
		"  - name: beta\n" +
		"    type: skill\n" +
		"    path: .ctxpm/dependencies/skills/beta\n" +
		"    version: bbb\n" +
		"\n" +
		"packages:\n" +
		"  - name: local\n" +
		"    type: skill\n" +
		"    path: .ctxpm/packages/skills/local\n"
	writeManifest(t, root, original)

	changed, err := RemoveDependency(root, "alpha")
	if err != nil {
		t.Fatalf("RemoveDependency() error = %v", err)
	}
	if !changed {
		t.Fatalf("RemoveDependency() changed = false, want true")
	}
	got := readManifest(t, root)
	if got != expected {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
}

func TestRemovePackageCollapsesEmptySection(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages:\n" +
		"  - name: local\n" +
		"    type: skill\n" +
		"    path: .ctxpm/packages/skills/local\n"
	expected := "" +
		"version: 1.0\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages: []\n"
	writeManifest(t, root, original)

	changed, err := RemovePackage(root, "local")
	if err != nil {
		t.Fatalf("RemovePackage() error = %v", err)
	}
	if !changed {
		t.Fatalf("RemovePackage() changed = false, want true")
	}

	got := readManifest(t, root)
	if got != expected {
		t.Fatalf("manifest mismatch\n--- got ---\n%s\n--- want ---\n%s", got, expected)
	}
}

func TestAgentCompatibilityPrefix(t *testing.T) {
	tests := []struct {
		agent string
		want  string
	}{
		{agent: "generic", want: ".agents"},
		{agent: "codex", want: ".agents"},
		{agent: "claude-code", want: ".claude"},
		{agent: "antigravity", want: ".antigravity"},
		{agent: "gemini-cli", want: ".gemini"},
		{agent: "cursor", want: ".cursor"},
		{agent: "windsurf", want: ".windsurf"},
		{agent: "kiro", want: ".kiro"},
		{agent: "unknown", want: ""},
		{agent: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			got := agentCompatibilityPrefix(tt.agent)
			if got != tt.want {
				t.Errorf("agentCompatibilityPrefix(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}

func TestEntrypointFile(t *testing.T) {
	tests := []struct {
		agent string
		want  string
	}{
		{agent: "claude-code", want: "CLAUDE.md"},
		{agent: "antigravity", want: "ANTIGRAVITY.md"},
		{agent: "gemini-cli", want: "GEMINI.md"},
		{agent: "generic", want: "AGENTS.md"},
		{agent: "codex", want: "AGENTS.md"},
		{agent: "cursor", want: "AGENTS.md"},
		{agent: "windsurf", want: "AGENTS.md"},
		{agent: "kiro", want: "AGENTS.md"},
		{agent: "unknown", want: "AGENTS.md"},
		{agent: "", want: "AGENTS.md"},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			got := EntrypointFile(tt.agent)
			if got != tt.want {
				t.Errorf("EntrypointFile(%q) = %q, want %q", tt.agent, got, tt.want)
			}
		})
	}
}

func TestDerivedCompatibilityPathsNewAgents(t *testing.T) {
	tests := []struct {
		name     string
		agents   []string
		resource Resource
		want     []string
	}{
		{
			name:   "gemini-cli skill",
			agents: []string{"gemini-cli"},
			resource: Resource{
				Type: "skill",
				Path: ".ctxpm/packages/skills/my-skill",
			},
			want: []string{".gemini/skills/my-skill"},
		},
		{
			name:   "cursor rule",
			agents: []string{"cursor"},
			resource: Resource{
				Type: "rule",
				Path: ".ctxpm/packages/rules/my-rule.md",
			},
			want: []string{".cursor/rules/my-rule.md"},
		},
		{
			name:   "windsurf spec",
			agents: []string{"windsurf"},
			resource: Resource{
				Type: "spec",
				Path: ".ctxpm/packages/specs/my-spec",
			},
			want: []string{".windsurf/specs/my-spec"},
		},
		{
			name:   "kiro prompt",
			agents: []string{"kiro"},
			resource: Resource{
				Type: "prompt",
				Path: ".ctxpm/packages/prompts/my-prompt.md",
			},
			want: []string{".kiro/prompts/my-prompt.md"},
		},
		{
			name:   "multi-agent with new agents",
			agents: []string{"generic", "claude-code", "gemini-cli", "cursor"},
			resource: Resource{
				Type: "skill",
				Path: ".ctxpm/packages/skills/shared-skill",
			},
			want: []string{
				".agents/skills/shared-skill",
				".claude/skills/shared-skill",
				".gemini/skills/shared-skill",
				".cursor/skills/shared-skill",
			},
		},
		{
			name:   "all new agents memory",
			agents: []string{"gemini-cli", "cursor", "windsurf", "kiro"},
			resource: Resource{
				Type: "memory",
				Path: ".ctxpm/packages/memories/project-memory",
			},
			want: []string{
				".gemini/memories/project-memory",
				".cursor/memories/project-memory",
				".windsurf/memories/project-memory",
				".kiro/memories/project-memory",
			},
		},
		{
			name:   "unknown agent yields no paths",
			agents: []string{"unknown-agent"},
			resource: Resource{
				Type: "skill",
				Path: ".ctxpm/packages/skills/my-skill",
			},
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DerivedCompatibilityPaths(tt.agents, tt.resource)
			if len(got) != len(tt.want) {
				t.Fatalf("DerivedCompatibilityPaths() len=%d, want %d\ngot:  %v\nwant: %v", len(got), len(tt.want), got, tt.want)
			}
			for i, p := range got {
				if p != tt.want[i] {
					t.Errorf("path[%d] = %q, want %q", i, p, tt.want[i])
				}
			}
		})
	}
}

func writeManifest(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "ctxpm.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

func readManifest(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "ctxpm.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	return string(data)
}
