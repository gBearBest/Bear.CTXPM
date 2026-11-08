package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

// overlayExcludedPaths returns every managed descendant relative to resource.
// The callers must reject aliased or equal destinations before using this API.
func overlayExcludedPaths(resource manifest.Resource, all []manifest.Resource) ([]string, error) {
	parent := filepath.Clean(filepath.FromSlash(resource.Path))
	var excluded []string
	for _, child := range all {
		if child.Name == resource.Name && child.Path == resource.Path {
			continue
		}
		path := filepath.Clean(filepath.FromSlash(child.Path))
		rel, err := filepath.Rel(parent, path)
		if err != nil {
			return nil, err
		}
		if rel == "." {
			return nil, fmt.Errorf("resources %q and %q share destination %q", resource.Name, child.Name, resource.Path)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		if resource.EffectiveLayout() != manifest.LayoutDir {
			return nil, fmt.Errorf("file resource %q cannot contain resource %q", resource.Name, child.Name)
		}
		excluded = append(excluded, filepath.ToSlash(rel))
	}
	sort.Strings(excluded)
	return excluded, nil
}

// overlayContentFingerprint records the resource-owned files of a composed tree.
// Paths and layout are part of the fingerprint, so changing the ownership graph
// invalidates prior records even when the visible bytes are identical.
func overlayContentFingerprint(localPath string, resource manifest.Resource, all []manifest.Resource) (string, error) {
	if err := validateResolvedResource(localPath, resource); err != nil {
		return "", err
	}
	excluded, err := overlayExcludedPaths(resource, all)
	if err != nil {
		return "", err
	}
	if len(excluded) == 0 {
		if resource.EffectiveLayout() == manifest.LayoutFile {
			return hashFileVersion(localPath)
		}
		return hashTreeVersion(localPath)
	}
	entries := make([]string, 0)
	err = filepath.Walk(localPath, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(localPath, current)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, child := range excluded {
			if rel == child || strings.HasPrefix(rel, child+"/") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if !info.IsDir() {
			entries = append(entries, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	h := sha256.New()
	metadata, err := json.Marshal(struct {
		Layout   string   `json:"layout"`
		Entry    string   `json:"entry"`
		Excluded []string `json:"excluded"`
	}{resource.EffectiveLayout(), resource.EffectiveEntry(), excluded})
	if err != nil {
		return "", err
	}
	_, _ = h.Write(metadata)
	_, _ = h.Write([]byte{0})
	for _, rel := range entries {
		sum, err := hashFileVersion(filepath.Join(localPath, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		_, _ = h.Write([]byte(rel))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(sum))
		_, _ = h.Write([]byte{'\n'})
	}
	return "sha256overlay1:" + hex.EncodeToString(h.Sum(nil)), nil
}

// overlayInstallRecordAt is usable for a prepared tree or the installed tree.
// A non-overlay legacy record remains compatible; an old full-tree parent hash
// cannot match the overlay hash and will be rebuilt rather than trusted.
func overlayInstallRecordAt(localPath string, resource manifest.Resource, all []manifest.Resource) (installRecord, error) {
	fingerprint, err := overlayContentFingerprint(localPath, resource, all)
	if err != nil {
		return installRecord{}, err
	}
	return installRecord{Version: resource.Version, SourceFingerprint: sourceFingerprint(resource), ContentFingerprint: fingerprint}, nil
}

func overlayDependencyIsCurrent(root string, resource manifest.Resource, all []manifest.Resource, record installRecord) bool {
	if resource.Source == nil || strings.TrimSpace(resource.Version) == "" || record.Version != resource.Version || record.SourceFingerprint != sourceFingerprint(resource) {
		return false
	}
	fingerprint, err := overlayContentFingerprint(filepath.Join(root, filepath.FromSlash(resource.Path)), resource, all)
	return err == nil && fingerprint == record.ContentFingerprint
}
