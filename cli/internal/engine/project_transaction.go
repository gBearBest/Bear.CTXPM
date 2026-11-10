package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Project writes share a lock across the whole command, including nested install calls.
type projectWriteKey struct{}
type projectWriteScope struct{ root string }

func acquireProjectWrite(ctx context.Context, root string) (context.Context, func(), error) {
	// Validate the schema before even creating local lock/state directories.
	if err := ensureManifestVersion(root, true); err != nil {
		return ctx, nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return ctx, nil, err
	}
	if scope, ok := ctx.Value(projectWriteKey{}).(*projectWriteScope); ok && scope.root == root {
		return ctx, func() {}, nil
	}
	unlock, err := acquireCacheLock(ctx, filepath.Join(root, ".ctxpm", "state", "project-write.lock"))
	if err != nil {
		return ctx, nil, err
	}
	if err = recoverProjectTransaction(root); err != nil {
		unlock()
		return ctx, nil, err
	}
	return context.WithValue(ctx, projectWriteKey{}, &projectWriteScope{root}), unlock, nil
}

type transactionEntry struct {
	Destination string `json:"destination"`
	Stage       string `json:"stage"`
	Original    string `json:"original,omitempty"`
	Prepared    string `json:"prepared"`
	Existed     bool   `json:"existed"`
}
type projectTransaction struct {
	Version   int                `json:"version"`
	Committed bool               `json:"committed"`
	Entries   []transactionEntry `json:"entries"`
}
type transactionSnapshot struct {
	Fingerprint string
	Exists      bool
}

// Capture a target before preparation and pass it to commitProjectTransaction.
// This detects edits made while downloading or assembling the prepared tree.
func snapshotTransactionTarget(destination string) (transactionSnapshot, error) {
	fingerprint, err := transactionFingerprint(destination)
	if errors.Is(err, os.ErrNotExist) {
		return transactionSnapshot{}, nil
	}
	if err != nil {
		return transactionSnapshot{}, err
	}
	return transactionSnapshot{Fingerprint: fingerprint, Exists: true}, nil
}

type transactionTarget struct {
	Destination, Stage string
	Expected           *transactionSnapshot
}

// Stage directories are adjacent to the destination, so renames remain on the
// same filesystem. The caller must create the returned payload (file or tree).
func newTransactionStage(destination string) (string, string, error) {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return "", "", err
	}
	parent := filepath.Dir(destination)
	if err = os.MkdirAll(parent, 0755); err != nil {
		return "", "", err
	}
	stage, err := os.MkdirTemp(parent, ".ctxpm-transaction-")
	if err != nil {
		return "", "", err
	}
	return stage, filepath.Join(stage, "payload"), nil
}
func projectTransactionPath(root string) string {
	return filepath.Join(root, ".ctxpm", "state", "transaction.json")
}

// commitProjectTransaction requires acquireProjectWrite. Each stage must be
// allocated with newTransactionStage and contain the intended payload.
func commitProjectTransaction(root string, targets []transactionTarget) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return nil
	}
	if _, err := os.Lstat(projectTransactionPath(root)); err == nil {
		return errors.New("an unfinished ctxpm transaction must be recovered before committing")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	journal := projectTransaction{Version: 1}
	for _, target := range targets {
		dest, err := filepath.Abs(target.Destination)
		if err != nil {
			return err
		}
		stage, err := filepath.Abs(target.Stage)
		if err != nil {
			return err
		}
		entry := transactionEntry{Destination: dest, Stage: stage}
		if err = validateTransactionEntry(root, entry); err != nil {
			return err
		}
		entry.Prepared, err = transactionFingerprint(filepath.Join(stage, "payload"))
		if err != nil {
			return fmt.Errorf("prepared %s: %w", dest, err)
		}
		entry.Original, err = transactionFingerprint(dest)
		if err == nil {
			entry.Existed = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if target.Expected != nil && (target.Expected.Exists != entry.Existed || (entry.Existed && target.Expected.Fingerprint != entry.Original)) {
			return fmt.Errorf("destination changed during preparation: %s", dest)
		}
		journal.Entries = append(journal.Entries, entry)
	}
	for i, a := range journal.Entries {
		for _, b := range journal.Entries[i+1:] {
			if pathContains(a.Destination, b.Destination) || pathContains(b.Destination, a.Destination) {
				return fmt.Errorf("transaction destinations overlap: %s and %s", a.Destination, b.Destination)
			}
		}
	}
	if err = saveProjectTransaction(root, journal); err != nil {
		return err
	}
	for _, entry := range journal.Entries {
		original, err := transactionFingerprint(entry.Destination)
		if entry.Existed {
			if err != nil || original != entry.Original {
				return rollbackTransactionError(root, fmt.Errorf("destination changed before commit: %s", entry.Destination))
			}
		} else if err == nil || !errors.Is(err, os.ErrNotExist) {
			return rollbackTransactionError(root, fmt.Errorf("destination changed before commit: %s", entry.Destination))
		}
		if entry.Existed {
			if err = os.Rename(entry.Destination, filepath.Join(entry.Stage, "backup")); err != nil {
				return rollbackTransactionError(root, err)
			}
			syncTransactionDir(filepath.Dir(entry.Destination))
			syncTransactionDir(entry.Stage)
		}
		if err = os.Rename(filepath.Join(entry.Stage, "payload"), entry.Destination); err != nil {
			return rollbackTransactionError(root, err)
		}
		syncTransactionDir(filepath.Dir(entry.Destination))
		syncTransactionDir(entry.Stage)
	}
	journal.Committed = true
	if err = saveProjectTransaction(root, journal); err != nil {
		return rollbackTransactionError(root, err)
	}
	return recoverProjectTransaction(root)
}
func rollbackTransactionError(root string, cause error) error {
	if err := recoverProjectTransaction(root); err != nil {
		return fmt.Errorf("transaction failed: %v; recovery required: %w", cause, err)
	}
	return cause
}

func saveProjectTransaction(root string, journal projectTransaction) error {
	path := projectTransactionPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "transaction-log-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	syncTransactionDir(filepath.Dir(path))
	return nil
}
func syncTransactionDir(path string) {
	if f, err := os.Open(path); err == nil {
		_ = f.Sync()
		_ = f.Close()
	}
}
func validateTransactionEntry(root string, e transactionEntry) error {
	if !filepath.IsAbs(e.Destination) || !filepath.IsAbs(e.Stage) || filepath.Dir(e.Stage) != filepath.Dir(e.Destination) || !strings.HasPrefix(filepath.Base(e.Stage), ".ctxpm-transaction-") {
		return errors.New("invalid transaction destination or staging path")
	}
	rel, err := filepath.Rel(root, e.Destination)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("transaction destination escapes project")
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	realParent, err := filepath.EvalSymlinks(filepath.Dir(e.Destination))
	if err != nil {
		return err
	}
	rel, err = filepath.Rel(realRoot, realParent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("transaction destination resolves outside project")
	}
	if info, err := os.Lstat(e.Stage); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("invalid transaction staging directory")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Lstat(filepath.Join(e.Stage, "backup")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func recoverProjectTransaction(root string) error {
	data, err := os.ReadFile(projectTransactionPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var journal projectTransaction
	if err = json.Unmarshal(data, &journal); err != nil || journal.Version != 1 || len(journal.Entries) == 0 {
		return errors.New("invalid ctxpm transaction journal; manual recovery required")
	}
	// Verify all affected paths before modifying any; never overwrite subsequent edits.
	for _, e := range journal.Entries {
		if err = validateTransactionEntry(root, e); err != nil {
			return fmt.Errorf("invalid transaction journal: %w", err)
		}
		current, curErr := transactionFingerprint(e.Destination)
		backup, backupErr := transactionFingerprint(filepath.Join(e.Stage, "backup"))
		if curErr != nil && !errors.Is(curErr, os.ErrNotExist) {
			return curErr
		}
		if backupErr != nil && !errors.Is(backupErr, os.ErrNotExist) {
			return backupErr
		}
		if journal.Committed {
			if curErr != nil || current != e.Prepared {
				return fmt.Errorf("transaction target %s changed after commit; backup retained at %s", e.Destination, e.Stage)
			}
			continue
		}
		if backupErr == nil && (!e.Existed || backup != e.Original) {
			return fmt.Errorf("transaction backup changed: %s", e.Stage)
		}
		if curErr == nil && current != e.Prepared && (!e.Existed || current != e.Original) {
			return fmt.Errorf("transaction target %s changed during recovery; backup retained at %s", e.Destination, e.Stage)
		}
		if e.Existed && backupErr != nil && (curErr != nil || current != e.Original) {
			return fmt.Errorf("transaction original missing for %s; backup retained at %s", e.Destination, e.Stage)
		}
	}
	if !journal.Committed {
		for i := len(journal.Entries) - 1; i >= 0; i-- {
			e := journal.Entries[i]
			backup := filepath.Join(e.Stage, "backup")
			if _, err := os.Lstat(backup); err == nil {
				if err = os.RemoveAll(e.Destination); err != nil {
					return err
				}
				if err = os.Rename(backup, e.Destination); err != nil {
					return err
				}
			} else if !e.Existed {
				if err = os.RemoveAll(e.Destination); err != nil {
					return err
				}
			}
		}
	}
	// Delete the journal first only after recovery is complete. Orphaned stages
	// are harmless; cleanup failure remains visible and can be retried.
	for _, e := range journal.Entries {
		if err = os.RemoveAll(e.Stage); err != nil {
			return err
		}
	}
	if err = os.Remove(projectTransactionPath(root)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	syncTransactionDir(filepath.Dir(projectTransactionPath(root)))
	return nil
}

// Hash path names, types, modes and contents without following symlinks.
func transactionFingerprint(path string) (string, error) {
	if _, err := os.Lstat(path); err != nil {
		return "", err
	}
	h := sha256.New()
	var paths []string
	err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	for _, p := range paths {
		info, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", filepath.ToSlash(rel), info.Mode())
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return "", err
			}
			fmt.Fprint(h, target)
		} else if info.Mode().IsRegular() {
			f, err := os.Open(p)
			if err != nil {
				return "", err
			}
			_, err = io.Copy(h, f)
			closeErr := f.Close()
			if err != nil {
				return "", err
			}
			if closeErr != nil {
				return "", closeErr
			}
		} else if !info.IsDir() {
			return "", fmt.Errorf("unsupported file type at %s", p)
		}
		fmt.Fprint(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
