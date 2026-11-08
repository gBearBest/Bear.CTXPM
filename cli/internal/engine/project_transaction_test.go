package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTransactionTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}
func transactionTestEntry(t *testing.T, root, name, old, next string) transactionEntry {
	t.Helper()
	dest := filepath.Join(root, name)
	if old != "" {
		writeTransactionTestFile(t, dest, old)
	}
	stage, payload, err := newTransactionStage(dest)
	if err != nil {
		t.Fatal(err)
	}
	writeTransactionTestFile(t, payload, next)
	prepared, err := transactionFingerprint(payload)
	if err != nil {
		t.Fatal(err)
	}
	e := transactionEntry{Destination: dest, Stage: stage, Prepared: prepared, Existed: old != ""}
	if e.Existed {
		e.Original, err = transactionFingerprint(dest)
		if err != nil {
			t.Fatal(err)
		}
	}
	return e
}
func TestProjectTransactionCommitsMultipleTargets(t *testing.T) {
	root := t.TempDir()
	ctx, unlock, err := acquireProjectWrite(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, nested, err := acquireProjectWrite(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	nested()
	a := transactionTestEntry(t, root, "tree/file", "old", "new")
	b := transactionTestEntry(t, root, "ctxpm.yaml", "old manifest", "new manifest")
	if err := commitProjectTransaction(root, []transactionTarget{{Destination: a.Destination, Stage: a.Stage}, {Destination: b.Destination, Stage: b.Stage}}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{a.Destination: "new", b.Destination: "new manifest"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q %v", path, got, err)
		}
	}
	if _, err := os.Stat(projectTransactionPath(root)); !os.IsNotExist(err) {
		t.Fatalf("journal left: %v", err)
	}
}
func TestProjectTransactionRecoversInterruptedCommit(t *testing.T) {
	for _, step := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+step)), func(t *testing.T) {
			root := t.TempDir()
			a := transactionTestEntry(t, root, "resource", "old", "new")
			b := transactionTestEntry(t, root, "ctxpm.yaml", "", "manifest")
			j := projectTransaction{Version: 1, Entries: []transactionEntry{a, b}}
			if err := saveProjectTransaction(root, j); err != nil {
				t.Fatal(err)
			}
			if step >= 1 {
				if err := os.Rename(a.Destination, filepath.Join(a.Stage, "backup")); err != nil {
					t.Fatal(err)
				}
			}
			if step >= 2 {
				if err := os.Rename(filepath.Join(a.Stage, "payload"), a.Destination); err != nil {
					t.Fatal(err)
				}
			}
			if step >= 3 {
				if err := os.Rename(filepath.Join(b.Stage, "payload"), b.Destination); err != nil {
					t.Fatal(err)
				}
			}
			if err := recoverProjectTransaction(root); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(a.Destination)
			if err != nil || string(got) != "old" {
				t.Fatalf("resource=%q %v", got, err)
			}
			if _, err = os.Stat(b.Destination); !os.IsNotExist(err) {
				t.Fatalf("unexpected manifest: %v", err)
			}
		})
	}
}
func TestProjectTransactionRecoveryProtectsExternalChanges(t *testing.T) {
	root := t.TempDir()
	e := transactionTestEntry(t, root, "resource", "old", "new")
	if err := saveProjectTransaction(root, projectTransaction{Version: 1, Entries: []transactionEntry{e}}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(e.Destination, filepath.Join(e.Stage, "backup")); err != nil {
		t.Fatal(err)
	}
	writeTransactionTestFile(t, e.Destination, "user edit")
	if err := recoverProjectTransaction(root); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("recovery error=%v", err)
	}
	got, err := os.ReadFile(e.Destination)
	if err != nil || string(got) != "user edit" {
		t.Fatalf("user content=%q %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(e.Stage, "backup"))
	if err != nil || string(got) != "old" {
		t.Fatalf("backup=%q %v", got, err)
	}
}
func TestProjectTransactionCommittedRecoveryKeepsNewContent(t *testing.T) {
	root := t.TempDir()
	e := transactionTestEntry(t, root, "resource", "old", "new")
	if err := os.Rename(e.Destination, filepath.Join(e.Stage, "backup")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(e.Stage, "payload"), e.Destination); err != nil {
		t.Fatal(err)
	}
	if err := saveProjectTransaction(root, projectTransaction{Version: 1, Committed: true, Entries: []transactionEntry{e}}); err != nil {
		t.Fatal(err)
	}
	if err := recoverProjectTransaction(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(e.Destination)
	if err != nil || string(got) != "new" {
		t.Fatalf("resource=%q %v", got, err)
	}
}

func TestProjectTransactionRejectsChangesDuringPreparation(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "ctxpm.yaml")
	writeTransactionTestFile(t, dest, "old")
	snapshot, err := snapshotTransactionTarget(dest)
	if err != nil {
		t.Fatal(err)
	}
	stage, payload, err := newTransactionStage(dest)
	if err != nil {
		t.Fatal(err)
	}
	writeTransactionTestFile(t, payload, "new")
	writeTransactionTestFile(t, dest, "user edit")
	err = commitProjectTransaction(root, []transactionTarget{{Destination: dest, Stage: stage, Expected: &snapshot}})
	if err == nil || !strings.Contains(err.Error(), "during preparation") {
		t.Fatalf("commit error=%v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "user edit" {
		t.Fatalf("content=%q %v", got, err)
	}
	if _, err = os.Stat(projectTransactionPath(root)); !os.IsNotExist(err) {
		t.Fatalf("unexpected journal: %v", err)
	}
}
