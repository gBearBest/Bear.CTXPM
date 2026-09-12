//go:build !windows

package engine

import (
	"fmt"
	"os"
	"path/filepath"
)

// replaceCurrentBinary stages the next binary beside the running executable and
// atomically renames it into place. Unix permits renaming an executing file but
// rejects opening it for truncation (ETXTBSY).
func replaceCurrentBinary(oldPath, newPath string) (string, error) {
	info, err := os.Stat(oldPath)
	if err != nil {
		return "", err
	}
	backupPath := oldPath + ".backup"
	stagedPath := oldPath + ".new"
	if err := os.Remove(stagedPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to remove stale staged binary: %w", err)
	}
	if err := copyFile(newPath, stagedPath, info.Mode()); err != nil {
		return "", fmt.Errorf("failed to stage replacement binary: %w", err)
	}
	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("failed to remove previous backup: %w", err)
	}
	if err := copyFile(oldPath, backupPath, info.Mode()); err != nil {
		_ = os.Remove(stagedPath)
		return "", fmt.Errorf("failed to back up current binary: %w", err)
	}
	if err := os.Rename(stagedPath, oldPath); err != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("failed to activate replacement binary: %w", err)
	}
	return backupPath, nil
}

func restorePreviousBinary(executable, backupPath string) error {
	if err := os.Rename(backupPath, executable); err != nil {
		return err
	}
	return nil
}

func scheduleWindowsSelfUpdate(executable, tmpBinary, projectRoot, version string) error {
	return fmt.Errorf("Windows self-update is unavailable on %s", filepath.Base(executable))
}

func runSelfUpdateHelper(args []string) error {
	return fmt.Errorf("self-update helper is only supported on Windows")
}

func CleanupStaleSelfUpdateHelpers() {}

// RunSelfUpdateHelper is an internal command entrypoint used by Windows only.
func RunSelfUpdateHelper(args []string) error {
	return runSelfUpdateHelper(args)
}
