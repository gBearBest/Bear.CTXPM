package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestDetectFindsMemoryMigrationCandidate(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, "memories", "project-history")
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte("# Project History\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
	})

	result, err := New(root).Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Status != "migration_candidates_found" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("candidates = %+v", result.Candidates)
	}
	candidate := result.Candidates[0]
	if candidate.Type != "memory" {
		t.Fatalf("candidate type = %q", candidate.Type)
	}
	if candidate.OriginalPath != "memories/project-history" {
		t.Fatalf("original path = %q", candidate.OriginalPath)
	}
	if candidate.CanonicalPath != ".ctxpm/packages/memories/project-history" {
		t.Fatalf("canonical path = %q", candidate.CanonicalPath)
	}
}

func TestValidateReportsBrokenMemoryIndexReference(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, ".ctxpm", "packages", "memories", "project-history")
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte("# Project History\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "index.json"), []byte(`{"entries":[{"path":"entries/missing.md"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:   "project-history",
				Type:   "memory",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/packages/memories/project-history",
				Entry:  "MEMORY.md",
			},
		},
	})

	result, err := New(root).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if result.OK {
		t.Fatalf("Validate() OK = true, want false")
	}
	if !containsIssue(result.Issues, `index reference "entries/missing.md" is missing`) {
		t.Fatalf("issues = %v", result.Issues)
	}
}

func TestMemorySearchIsConsistentWithAndWithoutIndex(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, ".ctxpm", "packages", "memories", "project-history")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "entries"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte("# Project History\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	entryPath := filepath.Join(memoryRoot, "entries", "billing-day.md")
	entryContent := "# Billing Day\n\nThe project uses billing-day terminology.\n"
	if err := os.WriteFile(entryPath, []byte(entryContent), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "index.json"), []byte(`{"entries":[{"path":"entries/billing-day.md"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:   "project-history",
				Type:   "memory",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/packages/memories/project-history",
				Entry:  "MEMORY.md",
			},
		},
	})

	app := New(root)
	withIndex, err := app.MemorySearch(MemorySearchOptions{Query: "billing"})
	if err != nil {
		t.Fatalf("MemorySearch() error = %v", err)
	}
	if err := os.Remove(filepath.Join(memoryRoot, "index.json")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	withoutIndex, err := app.MemorySearch(MemorySearchOptions{Query: "billing"})
	if err != nil {
		t.Fatalf("MemorySearch() error = %v", err)
	}
	if len(withIndex.Matches) != len(withoutIndex.Matches) {
		t.Fatalf("matches length differs: with=%d without=%d", len(withIndex.Matches), len(withoutIndex.Matches))
	}
	if len(withIndex.Matches) == 0 || withIndex.Matches[0].Path != withoutIndex.Matches[0].Path {
		t.Fatalf("matches differ: with=%+v without=%+v", withIndex.Matches, withoutIndex.Matches)
	}
}

func TestMemorySuggestDoesNotWriteFiles(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, ".ctxpm", "packages", "memories", "project-history")
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	initial := "# Project History\n"
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte(initial), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:   "project-history",
				Type:   "memory",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/packages/memories/project-history",
				Entry:  "MEMORY.md",
			},
		},
	})

	result, err := New(root).MemorySuggest(MemorySuggestOptions{
		Topic:   "Billing Day",
		Summary: "Billing day uses a stable domain term.",
	})
	if err != nil {
		t.Fatalf("MemorySuggest() error = %v", err)
	}
	if result.Status != "suggested" {
		t.Fatalf("status = %q", result.Status)
	}
	if _, err := os.Stat(filepath.Join(memoryRoot, "entries")); !os.IsNotExist(err) {
		t.Fatalf("entries directory should not be created during suggest")
	}
	if got := readFileForTest(t, filepath.Join(memoryRoot, "MEMORY.md")); got != initial {
		t.Fatalf("MEMORY.md changed unexpectedly: %q", got)
	}
}

func TestMemoryCaptureWriteCreatesEntryAndUpdatesIndex(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, ".ctxpm", "packages", "memories", "project-history")
	if err := os.MkdirAll(memoryRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte("# Project History\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:   "project-history",
				Type:   "memory",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/packages/memories/project-history",
				Entry:  "MEMORY.md",
			},
		},
	})

	result, err := New(root).MemoryCapture(MemoryCaptureOptions{
		Topic:   "Billing Day",
		Summary: "Billing day uses a stable domain term.",
		Write:   true,
	})
	if err != nil {
		t.Fatalf("MemoryCapture() error = %v", err)
	}
	if !result.Wrote || result.Status != "written" {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(memoryRoot, filepath.FromSlash(result.EntryPath))); err != nil {
		t.Fatalf("entry file missing: %v", err)
	}
	indexContent := readFileForTest(t, filepath.Join(memoryRoot, "MEMORY.md"))
	if !strings.Contains(indexContent, result.EntryPath) {
		t.Fatalf("MEMORY.md missing entry link:\n%s", indexContent)
	}
}

func TestMemoryPruneReportsEmptyAndDuplicateFiles(t *testing.T) {
	root := t.TempDir()
	memoryRoot := filepath.Join(root, ".ctxpm", "packages", "memories", "project-history")
	if err := os.MkdirAll(filepath.Join(memoryRoot, "entries"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "MEMORY.md"), []byte("# Project History\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "entries", "empty.md"), []byte("\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	dupContent := []byte("# Entry\n\nDuplicate\n")
	if err := os.WriteFile(filepath.Join(memoryRoot, "entries", "a.md"), dupContent, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(memoryRoot, "entries", "b.md"), dupContent, 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	writeManifestForTest(t, root, &manifest.Manifest{
		Version: manifest.CurrentManifestVersion,
		Project: manifest.Project{Name: "sample"},
		Agents:  []string{"generic"},
		Packages: []manifest.Resource{
			{
				Name:   "project-history",
				Type:   "memory",
				Layout: manifest.LayoutDir,
				Path:   ".ctxpm/packages/memories/project-history",
				Entry:  "MEMORY.md",
			},
		},
	})

	result, err := New(root).MemoryPrune(MemoryPruneOptions{})
	if err != nil {
		t.Fatalf("MemoryPrune() error = %v", err)
	}
	if result.Status != "candidates_found" {
		t.Fatalf("status = %q", result.Status)
	}
	if len(result.Candidates) < 2 {
		t.Fatalf("candidates = %+v", result.Candidates)
	}
}
