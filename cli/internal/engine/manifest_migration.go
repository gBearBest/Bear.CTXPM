package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

// Capability probing avoids guessing which future release first supports v2.
// The probe is read-only and is also used before invoking a downloaded CLI.
type ManifestCapabilities struct {
	Version string   `json:"version"`
	Schemas []string `json:"schemas"`
}

func probeManifestSupport(ctx context.Context, executable, schema string) (*ManifestCapabilities, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, executable, "manifest", "capabilities", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("CLI %s does not advertise schema %s support; upgrade ctxpm before converting the manifest", executable, schema)
	}
	var capabilities ManifestCapabilities
	if err = json.Unmarshal(output, &capabilities); err != nil {
		return nil, fmt.Errorf("invalid CLI manifest capabilities: %w", err)
	}
	for _, supported := range capabilities.Schemas {
		if supported == schema {
			return &capabilities, nil
		}
	}
	return nil, fmt.Errorf("CLI %s does not support schema %s", executable, schema)
}
func requireProjectSchemaSupport(ctx context.Context, executable, root string) error {
	m, _, err := manifest.Load(root)
	if err != nil {
		return err
	}
	if m.Version != manifest.GroupedManifestVersion {
		return nil
	}
	_, err = probeManifestSupport(ctx, executable, m.Version)
	return err
}

type ManifestMigrationOptions struct {
	To     string
	DryRun bool
}
type ManifestMigrationResult struct {
	Status       string `json:"status"`
	From         string `json:"from"`
	To           string `json:"to"`
	ManifestPath string `json:"manifest_path"`
	Preview      string `json:"preview,omitempty"`
}

func (r ManifestMigrationResult) Text() string {
	text := fmt.Sprintf("Manifest migration: %s (%s -> %s)\n", r.Status, r.From, r.To)
	if r.Preview != "" {
		text += "\n" + r.Preview
	}
	return text
}
func (a *App) MigrateManifest(ctx context.Context, opts ManifestMigrationOptions) (*ManifestMigrationResult, error) {
	if opts.To != manifest.GroupedManifestVersion {
		return nil, fmt.Errorf("unsupported migration target %q; use 2.0", opts.To)
	}
	m, manifestPath, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	result := &ManifestMigrationResult{From: m.Version, To: opts.To, ManifestPath: manifestPath}
	if m.Version == opts.To {
		result.Status = "up_to_date"
		return result, nil
	}
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	converted, err := manifest.ConvertGrouped(m)
	if err != nil {
		return nil, err
	}
	parsed, err := manifest.Parse(converted)
	if err != nil {
		return nil, err
	}
	if err = manifest.EquivalentResources(m, parsed); err != nil {
		return nil, err
	}
	// An old locked release would otherwise replace this CLI during install and
	// rewrite the new document. Verify the installed release without networking.
	if dep, ok := findDependency(m.Dependencies, "ctxpm"); ok {
		if !isCtxpmReleaseVersion(dep.Version) {
			return nil, errors.New("ctxpm must be locked to a schema-capable Release before manifest migration; upgrade it in schema 1.0 first")
		}
		cliPath := filepath.Join(a.Root, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
		if runtime.GOOS == "windows" {
			if _, err := os.Stat(cliPath + ".exe"); err == nil {
				cliPath += ".exe"
			}
		}
		capabilities, err := probeManifestSupport(ctx, cliPath, opts.To)
		if err != nil {
			return nil, err
		}
		if canonicalCtxpmReleaseVersion(capabilities.Version) != canonicalCtxpmReleaseVersion(dep.Version) {
			return nil, fmt.Errorf("installed ctxpm CLI version %q does not match locked release %q", capabilities.Version, dep.Version)
		}
	}
	result.Preview = string(converted)
	if opts.DryRun {
		result.Status = "dry_run"
		return result, nil
	}
	_, unlock, err := acquireProjectWrite(ctx, a.Root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(current, before) {
		return nil, errors.New("manifest changed during migration preflight; rerun migration")
	}
	snapshot, err := snapshotTransactionTarget(manifestPath)
	if err != nil {
		return nil, err
	}
	stage, payload, err := newTransactionStage(manifestPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if _, err := os.Stat(projectTransactionPath(a.Root)); os.IsNotExist(err) {
			_ = os.RemoveAll(stage)
		}
	}()
	info, err := os.Stat(manifestPath)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(payload, converted, info.Mode().Perm()); err != nil {
		return nil, err
	}
	if err = commitProjectTransaction(a.Root, []transactionTarget{{Destination: manifestPath, Stage: stage, Expected: &snapshot}}); err != nil {
		return nil, err
	}
	result.Status = "migrated"
	result.Preview = ""
	return result, nil
}
