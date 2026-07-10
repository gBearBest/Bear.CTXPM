package source

import (
	"context"
	"testing"
)

func TestDetectGitHubTreeURL(t *testing.T) {
	result, err := Detect(context.Background(), DetectionInput{
		RawURL:       "https://github.com/example/resources/tree/main/skills/reviewer",
		ResourceType: "skill",
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Resource.Source.Type != "git" {
		t.Fatalf("source type = %q, want git", result.Resource.Source.Type)
	}
	if result.Resource.Source.Path != "skills/reviewer" {
		t.Fatalf("source path = %q", result.Resource.Source.Path)
	}
	if result.Name != "reviewer" {
		t.Fatalf("name = %q", result.Name)
	}
	if result.Canonical != ".ctxpm/dependencies/skills/reviewer" {
		t.Fatalf("canonical = %q", result.Canonical)
	}
}

func TestDetectGitLabTreeURL(t *testing.T) {
	result, err := Detect(context.Background(), DetectionInput{
		RawURL:       "https://gitlab.company.com/team/resources/-/tree/main/rules/security",
		ResourceType: "rule",
		Layout:       "dir",
		Entry:        "policy.md",
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Resource.Source.Path != "rules/security" {
		t.Fatalf("source path = %q", result.Resource.Source.Path)
	}
	if result.Name != "security" {
		t.Fatalf("name = %q", result.Name)
	}
	if result.Resource.Source.Entry != "policy.md" {
		t.Fatalf("source entry = %q", result.Resource.Source.Entry)
	}
}

func TestDetectDirectURLInfersNameAndEntry(t *testing.T) {
	result, err := Detect(context.Background(), DetectionInput{
		RawURL:       "https://example.com/prompts/release-note.md",
		ResourceType: "prompt",
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Resource.Source.Type != "url" {
		t.Fatalf("source type = %q, want url", result.Resource.Source.Type)
	}
	if result.Name != "release-note" {
		t.Fatalf("name = %q", result.Name)
	}
	if result.Resource.Source.Entry != "release-note.md" {
		t.Fatalf("entry = %q", result.Resource.Source.Entry)
	}
	if result.Canonical != ".ctxpm/dependencies/prompts/release-note.md" {
		t.Fatalf("canonical = %q", result.Canonical)
	}
}

func TestDetectMultiFileURL(t *testing.T) {
	result, err := Detect(context.Background(), DetectionInput{
		RawURL:       "https://example.com/skills/reviewer/",
		ResourceType: "skill",
		Layout:       "dir",
		Entry:        "SKILL.md",
		Files:        []string{"SKILL.md", "rules/review.md"},
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Resource.Source.Type != "url" {
		t.Fatalf("source type = %q", result.Resource.Source.Type)
	}
	if result.Resource.Layout != "dir" {
		t.Fatalf("layout = %q", result.Resource.Layout)
	}
	if result.Resource.Source.URL != "https://example.com/skills/reviewer/" {
		t.Fatalf("source url = %q", result.Resource.Source.URL)
	}
	if len(result.Resource.Source.Files) != 2 {
		t.Fatalf("source files = %v", result.Resource.Source.Files)
	}
	if result.Canonical != ".ctxpm/dependencies/skills/reviewer" {
		t.Fatalf("canonical = %q", result.Canonical)
	}
}

func TestDetectArchiveURL(t *testing.T) {
	result, err := Detect(context.Background(), DetectionInput{
		RawURL:       "https://example.com/reviewer-skill.zip",
		ResourceType: "skill",
		SourceType:   "archive",
		SourcePath:   "skills/reviewer",
		Entry:        "SKILL.md",
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Resource.Source.Type != "archive" {
		t.Fatalf("source type = %q", result.Resource.Source.Type)
	}
	if result.Resource.Source.Path != "skills/reviewer" {
		t.Fatalf("source path = %q", result.Resource.Source.Path)
	}
	if result.Resource.Entry != "SKILL.md" {
		t.Fatalf("entry = %q", result.Resource.Entry)
	}
}

func TestDetectMemoryMultiFileURLDefaultsToMemoryEntry(t *testing.T) {
	result, err := Detect(context.Background(), DetectionInput{
		RawURL:       "https://example.com/memories/project-history/",
		ResourceType: "memory",
		Files:        []string{"MEMORY.md", "entries/decisions.md"},
	})
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if result.Resource.Entry != "MEMORY.md" {
		t.Fatalf("entry = %q", result.Resource.Entry)
	}
	if result.Canonical != ".ctxpm/dependencies/memories/project-history" {
		t.Fatalf("canonical = %q", result.Canonical)
	}
}
