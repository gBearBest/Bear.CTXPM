package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateV2MultiFileURLResource(t *testing.T) {
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
	if err := resource.validate(ManifestVersion2, "dependency"); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestValidateV2RejectsSingleFileURLDirectoryLayout(t *testing.T) {
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
	if err := resource.validate(ManifestVersion2, "dependency"); err == nil {
		t.Fatalf("validate() error = nil, want rejection")
	}
}

func TestSaveUsesTwoSpaceIndent(t *testing.T) {
	root := t.TempDir()
	m := &Manifest{
		Version: 1,
		Project: Project{Name: "sample"},
		Agents:  []string{"generic"},
		UpdatePolicy: UpdatePolicy{
			Interval: "1d",
		},
		Dependencies: []Resource{},
		Packages:     []Resource{},
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
		"version: 1\n" +
		"project:\n" +
		"  name: sample\n" +
		"agents:\n" +
		"  - generic\n" +
		"update_policy:\n" +
		"  interval: 1d\n" +
		"dependencies: []\n" +
		"packages: []\n" +
		"entrypoints:\n" +
		"  generic:\n" +
		"    file: AGENTS.md\n" +
		"    mode: managed\n"
	if got != want {
		t.Fatalf("Save() wrote unexpected YAML\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestUpdateResourceVersionsNoChangeLeavesManifestUntouched(t *testing.T) {
	root := t.TempDir()
	original := "" +
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages: []\n"
	expected := "" +
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
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
		"version: 1\n" +
		"project:\n" +
		"  name: sample\n" +
		"dependencies: []\n" +
		"packages:\n" +
		"  - name: local\n" +
		"    type: skill\n" +
		"    path: .ctxpm/packages/skills/local\n"
	expected := "" +
		"version: 1\n" +
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
