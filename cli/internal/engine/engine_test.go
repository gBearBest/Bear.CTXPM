package engine

import (
	"context"
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
