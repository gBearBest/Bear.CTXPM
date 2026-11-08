package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

// prepareUpdateMetadata builds new metadata without changing project files.
// Version edits use the manifest's text-preserving editor.
func prepareUpdateMetadata(root string, versions map[string]string, state installState) (string, string, func(), error) {
	temp, err := os.MkdirTemp("", "ctxpm-metadata-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(temp) }
	original, err := os.ReadFile(filepath.Join(root, "ctxpm.yaml"))
	if err != nil {
		cleanup()
		return "", "", nil, err
	}
	manifestPath := filepath.Join(temp, "ctxpm.yaml")
	info, err := os.Stat(filepath.Join(root, "ctxpm.yaml"))
	if err != nil {
		cleanup()
		return "", "", nil, err
	}
	if err := os.WriteFile(manifestPath, original, info.Mode().Perm()); err != nil {
		cleanup()
		return "", "", nil, err
	}
	if _, err := manifest.UpdateResourceVersions(temp, versions); err != nil {
		cleanup()
		return "", "", nil, err
	}
	state.SchemaVersion = installStateSchemaVersion
	if state.Dependencies == nil {
		state.Dependencies = map[string]installRecord{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		cleanup()
		return "", "", nil, err
	}
	statePath := filepath.Join(temp, "installed.json")
	if err := os.WriteFile(statePath, append(data, '\n'), 0644); err != nil {
		cleanup()
		return "", "", nil, err
	}
	return manifestPath, statePath, cleanup, nil
}

func copyPreparedPath(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(source, destination)
	}
	return copyFile(source, destination, info.Mode())
}

// commitPreparedResourceTree commits a composed tree with its manifest and
// installation records as one recoverable transaction.
func (a *App) commitPreparedResourceTree(ctx context.Context, m *manifest.Manifest, tree resourceTree, versions map[string]string, state *installState) error {
	destination := filepath.Join(a.Root, filepath.FromSlash(tree.Root.Path))
	destinations := []string{destination, filepath.Join(a.Root, "ctxpm.yaml"), installStatePath(a.Root)}
	snapshots := make([]transactionSnapshot, len(destinations))
	for i, path := range destinations {
		var err error
		snapshots[i], err = snapshotTransactionTarget(path)
		if err != nil {
			return err
		}
	}
	before, beforeErr := transactionFingerprint(destination)
	if beforeErr != nil && !errors.Is(beforeErr, os.ErrNotExist) {
		return beforeErr
	}
	manifestBefore, err := os.ReadFile(filepath.Join(a.Root, "ctxpm.yaml"))
	if err != nil {
		return err
	}
	prepared, err := a.prepareResourceTree(ctx, tree, versions)
	if err != nil {
		return err
	}
	defer prepared.Close()
	all := append(append([]manifest.Resource(nil), m.Dependencies...), m.Packages...)
	nextState := installState{SchemaVersion: installStateSchemaVersion, Dependencies: map[string]installRecord{}}
	for k, v := range state.Dependencies {
		nextState.Dependencies[k] = v
	}
	rootTarget := tree.targets[tree.Root.Name]
	for _, member := range tree.Members {
		if tree.preserve[member.Name] {
			continue
		}
		member.Version = prepared.Versions[member.Name]
		rel, err := filepath.Rel(rootTarget, tree.targets[member.Name])
		if err != nil {
			return err
		}
		local := prepared.LocalPath
		if rel != "." {
			local = filepath.Join(local, rel)
		}
		record, err := overlayInstallRecordAt(local, member, all)
		if err != nil {
			return err
		}
		nextState.Dependencies[member.Name] = record
	}
	manifestStageSource, stateStageSource, cleanup, err := prepareUpdateMetadata(a.Root, versions, nextState)
	if err != nil {
		return err
	}
	defer cleanup()
	currentManifest, err := os.ReadFile(filepath.Join(a.Root, "ctxpm.yaml"))
	if err != nil {
		return err
	}
	if !bytes.Equal(manifestBefore, currentManifest) {
		return errors.New("manifest changed while preparing update")
	}
	now, nowErr := transactionFingerprint(destination)
	if (beforeErr == nil && (nowErr != nil || now != before)) || (errors.Is(beforeErr, os.ErrNotExist) && !errors.Is(nowErr, os.ErrNotExist)) {
		return fmt.Errorf("resource %q changed while preparing update", tree.Root.Name)
	}
	targets := []struct{ dest, source string }{{destination, prepared.LocalPath}, {filepath.Join(a.Root, "ctxpm.yaml"), manifestStageSource}, {installStatePath(a.Root), stateStageSource}}
	staged := make([]transactionTarget, 0, len(targets))
	defer func() {
		if _, err := os.Stat(projectTransactionPath(a.Root)); errors.Is(err, os.ErrNotExist) {
			for _, entry := range staged {
				_ = os.RemoveAll(entry.Stage)
			}
		}
	}()
	for i, t := range targets {
		stage, payload, err := newTransactionStage(t.dest)
		if err != nil {
			return err
		}
		staged = append(staged, transactionTarget{Destination: t.dest, Stage: stage, Expected: &snapshots[i]})
		if err := copyPreparedPath(t.source, payload); err != nil {
			return err
		}
	}
	if err := commitProjectTransaction(a.Root, staged); err != nil {
		return err
	}
	*state = nextState
	for i := range m.Dependencies {
		if v, ok := versions[m.Dependencies[i].Name]; ok {
			m.Dependencies[i].Version = v
		}
	}
	return nil
}
