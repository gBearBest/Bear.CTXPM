package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeVersionTag(t *testing.T) {
	tests := map[string]string{
		"v0.1.13":                "0.1.13",
		"0.1.13+abc123":          "0.1.13",
		" v0.1.13+abc123-dirty ": "0.1.13",
		"v0.1.13-rc.1":           "0.1.13-rc.1",
	}
	for input, want := range tests {
		if got := normalizeVersionTag(input); got != want {
			t.Errorf("normalizeVersionTag(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExtractChecksumEntry(t *testing.T) {
	checksums := "aaa  ctxpm_0.1.13_linux_amd64.tar.gz\n" +
		"bbb *ctxpm_0.1.13_darwin_arm64.tar.gz\n"
	if got := extractChecksumEntry(checksums, "ctxpm_0.1.13_linux_amd64.tar.gz"); got != "aaa" {
		t.Fatalf("got %q, want aaa", got)
	}
	if got := extractChecksumEntry(checksums, "ctxpm_0.1.13_darwin_arm64.tar.gz"); got != "bbb" {
		t.Fatalf("got %q, want bbb", got)
	}
	if got := extractChecksumEntry(checksums, "missing"); got != "" {
		t.Fatalf("got %q for missing checksum, want empty", got)
	}
}

func TestCurrentExecutablePathResolvesProjectLocalSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".local", "bin", "ctxpm")
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	resolved, err := resolveExecutablePath(link)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != want {
		t.Fatalf("resolved path = %q, want project-local target %q", resolved, want)
	}
}
