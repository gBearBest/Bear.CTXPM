package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestResolveGitResourceFromPartialCloneCache(t *testing.T) {
	ctx := context.Background()
	upstream := t.TempDir()
	initGitRepoForTest(t, upstream)
	skillDir := filepath.Join(upstream, "skills", "reviewer")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"SKILL.md": "# Reviewer\n",
		"guide.md": "Supporting instructions\n",
	} {
		if err := os.WriteFile(filepath.Join(skillDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(upstream, "README.md"), []byte("Unrelated root file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(upstream, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, "rules", "reviewer.md"), []byte("Review changes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(upstream, ".gitattributes"), []byte("skills/reviewer/guide.md export-ignore\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"-C", upstream, "add", "."}, {"-C", upstream, "commit", "-m", "add skill"}} {
		if _, err := runGit(ctx, args...); err != nil {
			t.Fatal(err)
		}
	}
	commit, err := runGit(ctx, "-C", upstream, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	archive, err := runGit(ctx, "-C", upstream, "archive", "--format=tar", strings.TrimSpace(commit), "--", "skills/reviewer")
	if err != nil || strings.Contains(archive, "guide.md") {
		t.Fatalf("export-ignore did not omit guide.md from archive: %v", err)
	}
	bare := filepath.Join(t.TempDir(), "upstream.git")
	if _, err := runGit(ctx, "clone", "--bare", "--quiet", upstream, bare); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(ctx, "-C", bare, "config", "uploadpack.allowFilter", "true"); err != nil {
		t.Fatal(err)
	}
	resource := manifest.Resource{
		Name: "reviewer", Type: "skill", Layout: manifest.LayoutDir,
		Path: ".ctxpm/dependencies/skills/reviewer", Entry: "SKILL.md",
		Source: &manifest.Source{Type: "git", URL: "file://" + bare, Path: "skills/reviewer", Entry: "SKILL.md"},
	}
	app := New(t.TempDir())
	resolved, err := app.resolveGitResource(ctx, resource, resolveOptions{VersionOverride: strings.TrimSpace(commit)})
	if err != nil {
		t.Fatal(err)
	}
	defer resolved.Close()
	for name, want := range map[string]string{
		"SKILL.md": "# Reviewer\n",
		"guide.md": "Supporting instructions\n",
	} {
		got, err := os.ReadFile(filepath.Join(resolved.LocalPath, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	rule := manifest.Resource{
		Name: "reviewer-rule", Type: "rule", Layout: manifest.LayoutFile,
		Path: ".ctxpm/dependencies/rules/reviewer.md", Entry: "reviewer.md",
		Source: &manifest.Source{Type: "git", URL: "file://" + bare, Path: "rules/reviewer.md", Entry: "reviewer.md"},
	}
	resolvedRule, err := app.resolveGitResource(ctx, rule, resolveOptions{VersionOverride: strings.TrimSpace(commit)})
	if err != nil {
		t.Fatal(err)
	}
	defer resolvedRule.Close()
	got, err := os.ReadFile(resolvedRule.LocalPath)
	if err != nil || string(got) != "Review changes\n" {
		t.Fatalf("rule = %q, %v", got, err)
	}
}
