package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

type resourcePathOverlap struct {
	Parent     string
	Child      string
	ParentPath string
	ChildPath  string
	SamePath   bool
}

// Refuse a parent replacement when a separately managed child already exists.
// This prevents silently destroying a child pinned to a different version.
func checkParentReplacement(root string, overlaps []resourcePathOverlap, name string) error {
	for _, overlap := range overlaps {
		if overlap.Parent != name {
			continue
		}
		if _, err := os.Lstat(overlap.ChildPath); err == nil {
			return fmt.Errorf("cannot replace resource %q: destination overlaps managed resource %q at %s; move the resources to separate destinations before updating", name, overlap.Child, overlap.ChildPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// resourcePathOverlaps reports overlapping declared targets without imposing
// overlay policy on callers. Paths are resolved through existing symlinks.
func resourcePathOverlaps(root string, m *manifest.Manifest) ([]resourcePathOverlap, error) {
	type target struct{ name, path string }
	var targets []target
	for _, resource := range append(append([]manifest.Resource(nil), m.Dependencies...), m.Packages...) {
		path, err := canonicalResourceTarget(root, resource.Path)
		if err != nil {
			return nil, fmt.Errorf("resource %q destination: %w", resource.Name, err)
		}
		targets = append(targets, target{resource.Name, path})
	}
	var overlaps []resourcePathOverlap
	for i, a := range targets {
		for _, b := range targets[i+1:] {
			if pathContains(a.path, b.path) {
				overlaps = append(overlaps, resourcePathOverlap{a.name, b.name, a.path, b.path, pathContains(b.path, a.path)})
			} else if pathContains(b.path, a.path) {
				overlaps = append(overlaps, resourcePathOverlap{b.name, a.name, b.path, a.path, false})
			}
		}
	}
	return overlaps, nil
}

// Resolve existing symlink ancestors so two differently named targets cannot
// silently alias one another. Preserve the missing suffix for new installs.
func canonicalResourceTarget(root, resourcePath string) (string, error) {
	path, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(resourcePath)))
	if err != nil {
		return "", err
	}
	path = filepath.Clean(path)
	ancestor := path
	for {
		_, err := os.Lstat(ancestor)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("cannot resolve destination %q", path)
		}
		ancestor = parent
	}
	resolved, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	suffix, err := filepath.Rel(ancestor, path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(resolved, suffix)), nil
}

func pathContains(parent, child string) bool {
	// Windows paths are case-insensitive by default. Lexical checks on macOS
	// deliberately do not assume a case-insensitive volume.
	if runtime.GOOS == "windows" {
		parent, child = strings.ToLower(parent), strings.ToLower(child)
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// replacePathSafely stages the complete replacement beside its destination,
// then preserves the previous tree until the new tree has been renamed into
// place. If the second rename fails it attempts to restore the previous tree.
func replacePathSafely(sourcePath, destination string) error {
	parent := filepath.Dir(destination)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".ctxpm-replace-*")
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(stage)
		}
	}()
	prepared := filepath.Join(stage, "new")
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		err = copyDir(sourcePath, prepared)
	} else {
		err = copyFile(sourcePath, prepared, info.Mode())
	}
	if err != nil {
		return fmt.Errorf("prepare replacement for %s: %w", destination, err)
	}
	backup := filepath.Join(stage, "previous")
	existed := false
	if _, err := os.Lstat(destination); err == nil {
		if err := os.Rename(destination, backup); err != nil {
			return fmt.Errorf("preserve %s: %w", destination, err)
		}
		existed = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(prepared, destination); err != nil {
		if existed {
			if restoreErr := os.Rename(backup, destination); restoreErr != nil {
				cleanup = false
				return fmt.Errorf("replace %s: %v; restore failed: %v; previous content retained at %s", destination, err, restoreErr, backup)
			}
		}
		return fmt.Errorf("replace %s: %w", destination, err)
	}
	return nil
}
