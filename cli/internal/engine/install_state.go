package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

const installStateSchemaVersion = 1

type installState struct {
	SchemaVersion int                      `json:"schema_version"`
	Dependencies  map[string]installRecord `json:"dependencies"`
}

type installRecord struct {
	Version            string `json:"version"`
	SourceFingerprint  string `json:"source_fingerprint"`
	ContentFingerprint string `json:"content_fingerprint"`
}

func installStatePath(root string) string {
	return filepath.Join(root, ".ctxpm", "state", "installed.json")
}

func loadInstallState(root string) installState {
	state := installState{SchemaVersion: installStateSchemaVersion, Dependencies: map[string]installRecord{}}
	data, err := os.ReadFile(installStatePath(root))
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil || state.SchemaVersion != installStateSchemaVersion || state.Dependencies == nil {
		return installState{SchemaVersion: installStateSchemaVersion, Dependencies: map[string]installRecord{}}
	}
	return state
}

func saveInstallState(root string, state installState) error {
	if err := os.MkdirAll(filepath.Dir(installStatePath(root)), 0o755); err != nil {
		return err
	}
	state.SchemaVersion = installStateSchemaVersion
	if state.Dependencies == nil {
		state.Dependencies = map[string]installRecord{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(installStatePath(root)), "installed-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, installStatePath(root))
}

func sourceFingerprint(resource manifest.Resource) string {
	if resource.Source == nil {
		return ""
	}
	files := append([]string(nil), resource.Source.Files...)
	// File order does not affect the resolved resource tree.
	sort.Strings(files)
	value := strings.Join([]string{
		resource.Source.NormalizedType(), resource.Source.URL, resource.Source.Path,
		resource.Source.Ref, resource.Source.Entry, strings.Join(files, "\x00"),
	}, "\x00")
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func resourceContentFingerprint(root string, resource manifest.Resource) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(resource.Path))
	if err := validateResolvedResource(path, resource); err != nil {
		return "", err
	}
	if resource.EffectiveLayout() == manifest.LayoutFile {
		return hashFileVersion(path)
	}
	return hashTreeVersion(path)
}

func dependencyIsCurrent(root string, resource manifest.Resource, record installRecord) bool {
	if resource.Source == nil || strings.TrimSpace(resource.Version) == "" {
		return false
	}
	if record.Version != resource.Version || record.SourceFingerprint != sourceFingerprint(resource) {
		return false
	}
	fingerprint, err := resourceContentFingerprint(root, resource)
	return err == nil && fingerprint == record.ContentFingerprint
}

func recordInstalledDependency(root string, state *installState, resource manifest.Resource) error {
	if resource.Source == nil {
		return nil
	}
	fingerprint, err := resourceContentFingerprint(root, resource)
	if err != nil {
		return err
	}
	if state.Dependencies == nil {
		state.Dependencies = map[string]installRecord{}
	}
	state.Dependencies[resource.Name] = installRecord{
		Version:            resource.Version,
		SourceFingerprint:  sourceFingerprint(resource),
		ContentFingerprint: fingerprint,
	}
	return nil
}
