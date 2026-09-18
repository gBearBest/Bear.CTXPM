//go:build windows

package engine

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const selfUpdateHelperPrefix = "ctxpm-self-update-"

// replaceCurrentBinary is intentionally unavailable on Windows. The executable
// is locked while running, so UpdateCtxpmRelease schedules a helper instead.
func replaceCurrentBinary(oldPath, newPath string) (string, error) {
	return "", fmt.Errorf("cannot replace a running Windows executable directly")
}

func restorePreviousBinary(executable, backupPath string) error {
	return fmt.Errorf("cannot restore a running Windows executable directly")
}

func scheduleWindowsSelfUpdate(executable, tmpBinary, projectRoot, version string) error {
	info, err := os.Stat(executable)
	if err != nil {
		return err
	}
	staged := executable + ".new"
	if err := os.Remove(staged); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := copyFile(tmpBinary, staged, info.Mode()); err != nil {
		return fmt.Errorf("stage replacement: %w", err)
	}
	helper, err := os.CreateTemp("", selfUpdateHelperPrefix+"*.exe")
	if err != nil {
		_ = os.Remove(staged)
		return err
	}
	helperPath := helper.Name()
	if err := helper.Close(); err != nil {
		_ = os.Remove(helperPath)
		_ = os.Remove(staged)
		return err
	}
	if err := copyFile(tmpBinary, helperPath, info.Mode()); err != nil {
		_ = os.Remove(helperPath)
		_ = os.Remove(staged)
		return fmt.Errorf("create update helper: %w", err)
	}
	if err := os.Remove(tmpBinary); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(helperPath)
		_ = os.Remove(staged)
		return err
	}
	cmd := exec.Command(helperPath, "__self-update-helper", "--target", executable, "--staged", staged, "--root", projectRoot, "--version", version)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = os.Remove(helperPath)
		_ = os.Remove(staged)
		return err
	}
	return nil
}

func runSelfUpdateHelper(args []string) error {
	fs := flag.NewFlagSet("__self-update-helper", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	target := fs.String("target", "", "")
	staged := fs.String("staged", "", "")
	root := fs.String("root", "", "")
	version := fs.String("version", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *target == "" || *staged == "" || *root == "" || !isCtxpmReleaseVersion(*version) {
		return fmt.Errorf("invalid self-update helper arguments")
	}
	backup := *target + ".backup"
	deadline := time.Now().Add(30 * time.Second)
	for {
		_ = os.Remove(backup)
		err := os.Rename(*target, backup)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for ctxpm to exit: %w", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := os.Rename(*staged, *target); err != nil {
		_ = os.Rename(backup, *target)
		return fmt.Errorf("activate replacement binary: %w", err)
	}
	helper, err := currentExecutablePath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := synchronizeBundleWithUpdatedCLI(ctx, helper, *root, *version); err != nil {
		_ = os.Remove(*target)
		_ = os.Rename(backup, *target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

func CleanupStaleSelfUpdateHelpers() {
	current, _ := currentExecutablePath()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), selfUpdateHelperPrefix+"*.exe"))
	if err != nil {
		return
	}
	for _, path := range matches {
		if strings.EqualFold(path, current) {
			continue
		}
		_ = os.Remove(path)
	}
}

// RunSelfUpdateHelper is an internal command entrypoint used after the parent
// process releases its executable lock.
func RunSelfUpdateHelper(args []string) error {
	return runSelfUpdateHelper(args)
}
