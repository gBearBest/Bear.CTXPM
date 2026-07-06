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
