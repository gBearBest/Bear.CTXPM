package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

// resourceTree is a selected root and every separately managed dependency
// beneath it. Members are sorted shallowest first for deterministic overlays.
type resourceTree struct {
	Root     manifest.Resource
	Members  []manifest.Resource
	targets  map[string]string
	preserve map[string]bool
}

// buildResourceTrees groups only selected roots and their descendants. Selecting
// a child does not force replacing or downloading its ancestors.
func buildResourceTrees(root string, m *manifest.Manifest, selected map[string]bool, preserved ...map[string]bool) ([]resourceTree, error) {
	var preserve map[string]bool
	if len(preserved) > 0 {
		preserve = preserved[0]
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	realRoot, err = filepath.Abs(realRoot)
	if err != nil {
		return nil, err
	}
	type node struct {
		resource  manifest.Resource
		target    string
		selected  bool
		isPackage bool
	}
	nodes := make([]node, 0, len(m.Dependencies)+len(m.Packages))
	seen := make(map[string]bool)
	for _, entry := range []struct {
		resources []manifest.Resource
		isPackage bool
	}{{m.Dependencies, false}, {m.Packages, true}} {
		for _, res := range entry.resources {
			if seen[res.Name] {
				return nil, fmt.Errorf("duplicate managed resource name %q", res.Name)
			}
			seen[res.Name] = true
			target, err := canonicalResourceTarget(root, res.Path)
			if err != nil {
				return nil, fmt.Errorf("resource %q: %w", res.Name, err)
			}
			lexical := filepath.Clean(filepath.Join(realRoot, filepath.FromSlash(res.Path)))
			if target != lexical {
				return nil, fmt.Errorf("resource %q destination traverses a symlink; move it to a direct managed path", res.Name)
			}
			nodes = append(nodes, node{res, target, selected[res.Name] && !entry.isPackage, entry.isPackage})
		}
	}
	for name := range selected {
		if !seen[name] {
			return nil, fmt.Errorf("dependency %q was not found", name)
		}
	}
	for i := range nodes {
		for j := i + 1; j < len(nodes); j++ {
			a, b := nodes[i], nodes[j]
			if pathContains(a.target, b.target) && pathContains(b.target, a.target) {
				return nil, fmt.Errorf("resources %q and %q have the same destination", a.resource.Name, b.resource.Name)
			}
		}
	}
	var roots []node
	for _, n := range nodes {
		if !n.selected {
			continue
		}
		nested := false
		for _, other := range nodes {
			if other.selected && other.resource.Name != n.resource.Name && pathContains(other.target, n.target) {
				nested = true
				break
			}
		}
		if !nested {
			roots = append(roots, n)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].target < roots[j].target })
	trees := make([]resourceTree, 0, len(roots))
	for _, top := range roots {
		group := resourceTree{Root: top.resource, targets: map[string]string{}, preserve: preserve}
		for _, n := range nodes {
			if !pathContains(top.target, n.target) {
				continue
			}
			if n.isPackage {
				return nil, fmt.Errorf("dependency %q contains project package %q; move the package to a separate destination", top.resource.Name, n.resource.Name)
			}
			if n.resource.Name == "ctxpm" {
				return nil, fmt.Errorf("ctxpm release cannot be combined with overlapping resources")
			}
			if n.resource.Source == nil && !preserve[n.resource.Name] {
				return nil, fmt.Errorf("resource %q has no source and cannot be assembled as an overlay", n.resource.Name)
			}
			if n.resource.Name != top.resource.Name && top.resource.EffectiveLayout() != manifest.LayoutDir {
				return nil, fmt.Errorf("file resource %q contains another resource", top.resource.Name)
			}
			group.Members = append(group.Members, n.resource)
			group.targets[n.resource.Name] = n.target
		}
		sort.Slice(group.Members, func(i, j int) bool {
			a, b := group.targets[group.Members[i].Name], group.targets[group.Members[j].Name]
			if len(a) == len(b) {
				return a < b
			}
			return len(a) < len(b)
		})
		// Every intermediate file resource must be excluded as a parent.
		for i, parent := range group.Members {
			if parent.EffectiveLayout() != manifest.LayoutDir {
				for _, child := range group.Members[i+1:] {
					if pathContains(group.targets[parent.Name], group.targets[child.Name]) {
						return nil, fmt.Errorf("file resource %q contains resource %q", parent.Name, child.Name)
					}
				}
			}
		}
		trees = append(trees, group)
	}
	return trees, nil
}

type preparedResourceTree struct {
	LocalPath string
	Versions  map[string]string
	cleanup   func()
}

func (p *preparedResourceTree) Close() {
	if p.cleanup != nil {
		p.cleanup()
	}
}

// prepareResourceTree creates a complete isolated tree. versions overrides the
// recorded version for selected updates; every other member stays locked.
func (a *App) prepareResourceTree(ctx context.Context, tree resourceTree, versions map[string]string) (*preparedResourceTree, error) {
	temp, err := os.MkdirTemp("", "ctxpm-overlay-*")
	if err != nil {
		return nil, err
	}
	result := &preparedResourceTree{LocalPath: filepath.Join(temp, "tree"), Versions: map[string]string{}, cleanup: func() { _ = os.RemoveAll(temp) }}
	success := false
	defer func() {
		if !success {
			result.Close()
		}
	}()
	rootPath := tree.targets[tree.Root.Name]
	for _, member := range tree.Members {
		if tree.preserve[member.Name] {
			rel, err := filepath.Rel(rootPath, tree.targets[member.Name])
			if err != nil {
				return nil, err
			}
			if err := safeOverlayAncestors(result.LocalPath, rel); err != nil {
				return nil, err
			}
			dst := filepath.Join(result.LocalPath, rel)
			if err := os.RemoveAll(dst); err != nil {
				return nil, err
			}
			source := tree.targets[member.Name]
			if _, err := os.Lstat(source); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			if err := copyPreservedTree(source, dst); err != nil {
				return nil, fmt.Errorf("preserve %q: %w", member.Name, err)
			}
			continue
		}
		expected := strings.TrimSpace(member.Version)
		if v, ok := versions[member.Name]; ok {
			expected = strings.TrimSpace(v)
		}
		if expected == "" {
			return nil, fmt.Errorf("resource %q has no locked version; resolve it before assembling", member.Name)
		}
		resolved, err := a.resolveResource(ctx, member, resolveOptions{VersionOverride: expected})
		if err != nil {
			return nil, fmt.Errorf("prepare %q: %w", member.Name, err)
		}
		if resolved.Version != expected {
			resolved.Close()
			return nil, fmt.Errorf("resource %q resolved version %s, expected %s", member.Name, resolved.Version, expected)
		}
		rel, err := filepath.Rel(rootPath, tree.targets[member.Name])
		if err != nil {
			resolved.Close()
			return nil, err
		}
		dst := result.LocalPath
		if rel != "." {
			if err := safeOverlayAncestors(result.LocalPath, rel); err != nil {
				resolved.Close()
				return nil, fmt.Errorf("resource %q: %w", member.Name, err)
			}
			dst = filepath.Join(result.LocalPath, rel)
		}
		if err := os.RemoveAll(dst); err != nil {
			resolved.Close()
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			resolved.Close()
			return nil, err
		}
		info, err := os.Stat(resolved.LocalPath)
		if err == nil {
			if info.IsDir() {
				err = copyDir(resolved.LocalPath, dst)
			} else {
				err = copyFile(resolved.LocalPath, dst, info.Mode())
			}
		}
		resolved.Close()
		if err != nil {
			return nil, fmt.Errorf("prepare %q: %w", member.Name, err)
		}
		result.Versions[member.Name] = expected
	}
	for _, member := range tree.Members {
		if tree.preserve[member.Name] {
			continue
		}
		rel, _ := filepath.Rel(rootPath, tree.targets[member.Name])
		p := result.LocalPath
		if rel != "." {
			p = filepath.Join(p, rel)
		}
		if err := validateResolvedResource(p, member); err != nil {
			return nil, err
		}
	}
	success = true
	return result, nil
}

func copyPreservedTree(source, destination string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type at %s", path)
		}
		return copyFile(path, target, info.Mode())
	})
}

// Reject symlinks in every ancestor, including the final destination, so a
// source tree cannot redirect removal or copying outside the staging tree.
func safeOverlayAncestors(root, rel string) error {
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("overlay escapes staging tree")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid overlay path")
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("overlay path traverses symlink %s", current)
		}
		if current != filepath.Join(root, rel) && !info.IsDir() {
			return fmt.Errorf("overlay ancestor %s is not a directory", current)
		}
	}
	return nil
}
