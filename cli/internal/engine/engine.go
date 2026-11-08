package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
	"github.com/gBearBest/Bear.CTXPM/cli/internal/source"
)

const userAgent = "Bear.CTXPM ctxpm CLI"

type App struct {
	Root string
}

func New(root string) *App {
	return &App{Root: root}
}

func ensureManifestVersion(root string, allowMissing bool) error {
	_, err := manifest.SyncVersion(root)
	if err == nil {
		return nil
	}
	if allowMissing && errors.Is(err, manifest.ErrNotFound) {
		return nil
	}
	return err
}

type textResult interface {
	Text() string
}

type InitOptions struct {
	Agent          string
	ProjectName    string
	CurrentVersion string
	Force          bool
	DryRun         bool
	Progress       func(ProgressEvent)
}

type InitResult struct {
	ManifestPath               string   `json:"manifest_path"`
	Files                      []string `json:"files"`
	DryRun                     bool     `json:"dry_run"`
	Agent                      string   `json:"agent"`
	EntrypointFile             string   `json:"entrypoint_file"`
	EntrypointSourceFile       string   `json:"entrypoint_source_file"`
	PackagesCreated            []string `json:"packages_created,omitempty"`
	DependenciesCreated        []string `json:"dependencies_created,omitempty"`
	MigratedResources          []string `json:"migrated_resources,omitempty"`
	GitignoreUpdated           bool     `json:"gitignore_updated"`
	GitStaged                  []string `json:"git_staged,omitempty"`
	OwnershipConfirmed         []string `json:"ownership_confirmed,omitempty"`
	OwnershipInferred          []string `json:"ownership_inferred,omitempty"`
	UnresolvedResources        []string `json:"unresolved_resources,omitempty"`
	CtxpmDependencyStatus      string   `json:"ctxpm_dependency_status,omitempty"`
	CtxpmCompatibilityComplete bool     `json:"ctxpm_compatibility_complete"`
	ManagedEntrypointUpdated   bool     `json:"managed_entrypoint_updated"`
	CtxpmYAMLStatus            string   `json:"ctxpm_yaml_status,omitempty"`
	LocalCLIStatus             string   `json:"local_cli_status,omitempty"`
	GitHooksStatus             string   `json:"git_hooks_status,omitempty"`
	Warnings                   []string `json:"warnings,omitempty"`
	AgentDetectionEvidence     []string `json:"agent_detection_evidence,omitempty"`
	AgentDetectionSource       string   `json:"agent_detection_source,omitempty"`
}

func (r InitResult) Text() string {
	lines := []string{
		fmt.Sprintf("Initialized ctxpm project (%s)", ternary(r.DryRun, "dry-run", "applied")),
		fmt.Sprintf("Agent: %s", r.Agent),
		fmt.Sprintf("Entrypoint source: %s", r.EntrypointSourceFile),
		fmt.Sprintf("Root entrypoint alias: %s", r.EntrypointFile),
		fmt.Sprintf("Manifest: %s", r.ManifestPath),
	}
	lines = append(lines, fmt.Sprintf("ctxpm dependency: %s", fallbackString(r.CtxpmDependencyStatus, "unchanged")))
	lines = append(lines, fmt.Sprintf("Local CLI: %s", fallbackString(r.LocalCLIStatus, "not prepared")))
	lines = append(lines, fmt.Sprintf("Git hooks: %s", fallbackString(r.GitHooksStatus, "not configured")))
	lines = append(lines, fmt.Sprintf("Managed entrypoint updated: %s", ternary(r.ManagedEntrypointUpdated, "yes", "no")))
	lines = append(lines, fmt.Sprintf("ctxpm compatibility complete: %s", ternary(r.CtxpmCompatibilityComplete, "yes", "no")))
	lines = append(lines, fmt.Sprintf(".gitignore updated: %s", ternary(r.GitignoreUpdated, "yes", "no")))
	if len(r.PackagesCreated) > 0 {
		lines = append(lines, "Packages created:")
		for _, item := range r.PackagesCreated {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.DependenciesCreated) > 0 {
		lines = append(lines, "Dependencies created:")
		for _, item := range r.DependenciesCreated {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.MigratedResources) > 0 {
		lines = append(lines, "Migrated existing resources:")
		for _, item := range r.MigratedResources {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.OwnershipInferred) > 0 {
		lines = append(lines, "Ownership inferred from project evidence:")
		for _, item := range r.OwnershipInferred {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.UnresolvedResources) > 0 {
		lines = append(lines, "Unresolved resources:")
		for _, item := range r.UnresolvedResources {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.Warnings) > 0 {
		lines = append(lines, "Warnings:")
		for _, item := range r.Warnings {
			lines = append(lines, "- "+item)
		}
	}
	for _, item := range r.GitStaged {
		lines = append(lines, "git staged: "+item)
	}
	lines = append(lines, "Files touched:")
	for _, item := range r.Files {
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Init(opts InitOptions) (*InitResult, error) {
	if !opts.DryRun {
		_, unlock, err := acquireProjectWrite(context.Background(), a.Root)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	projectName := strings.TrimSpace(opts.ProjectName)
	if projectName == "" {
		projectName = filepath.Base(a.Root)
	}

	if err := ensureManifestVersion(a.Root, true); err != nil {
		return nil, err
	}
	m, manifestPath, err := manifest.Load(a.Root)
	switch {
	case err == nil:
	case errors.Is(err, manifest.ErrNotFound):
		m = nil
	default:
		return nil, err
	}
	discovery := detectInitAgent(a.Root, opts.Agent, m)
	opts.Agent = discovery.agent
	if m == nil {
		m = &manifest.Manifest{
			Version:      manifest.CurrentManifestVersion,
			Project:      manifest.Project{Name: projectName},
			Agents:       []string{opts.Agent},
			UpdatePolicy: manifest.DefaultPolicy(),
			Dependencies: []manifest.Resource{},
			Packages:     []manifest.Resource{},
		}
	}

	if m.Version != manifest.CurrentManifestVersion {
		m.Version = manifest.CurrentManifestVersion
	}
	if strings.TrimSpace(m.Project.Name) == "" || opts.Force {
		m.Project.Name = projectName
	}
	m.Agents = filterEmptyStrings(m.Agents)
	m.Agents = appendIfMissing(m.Agents, opts.Agent)
	normalizeSharedEntrypoints(m)
	if m.UpdatePolicy.Enabled == nil && strings.TrimSpace(m.UpdatePolicy.Interval) == "" && m.UpdatePolicy.IncludeSelf == nil {
		m.UpdatePolicy = manifest.DefaultPolicy()
	}

	files := []string{}
	dirs := []string{
		filepath.Join(a.Root, ".ctxpm", "dependencies", "skills"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "rules"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "specs"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "prompts"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "mcp"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "memories"),
		filepath.Join(a.Root, ".ctxpm", "packages", "skills"),
		filepath.Join(a.Root, ".ctxpm", "packages", "rules"),
		filepath.Join(a.Root, ".ctxpm", "packages", "specs"),
		filepath.Join(a.Root, ".ctxpm", "packages", "prompts"),
		filepath.Join(a.Root, ".ctxpm", "packages", "mcp"),
		filepath.Join(a.Root, ".ctxpm", "packages", "memories"),
		filepath.Join(a.Root, ".ctxpm", "state"),
	}
	for _, dir := range dirs {
		files = append(files, dir)
		if !opts.DryRun {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}

	resourcePlan, err := scanAndMigrateResources(a.Root, m, projectName, opts.DryRun)
	if err != nil {
		return nil, err
	}
	for _, pkg := range resourcePlan.packages {
		m.Packages = append(m.Packages, pkg)
	}
	for _, dep := range resourcePlan.dependencies {
		m.Dependencies = append(m.Dependencies, dep)
	}
	sortResources(m.Packages)
	sortResources(m.Dependencies)

	entrypointFile := filepath.Join(a.Root, manifest.CanonicalEntrypointSourceFile())
	files = append(files, entrypointFile)
	if !opts.DryRun {
		entrypointFiles, err := syncManagedEntrypoints(a.Root, m, opts.Force)
		if err != nil {
			return nil, err
		}
		files = append(files, entrypointFiles...)
	}

	currentCtxpmVersion := currentCtxpmManifestVersion(opts.CurrentVersion)
	ctxpmResource := bundledCtxpmResource(m.Agents, currentCtxpmVersion)
	upsertManagedDependency(&m.Dependencies, ctxpmResource)

	gitignorePath := filepath.Join(a.Root, ".gitignore")
	files = append(files, gitignorePath)
	gitignoreUpdated := false
	ctxpmStatus := "planned"
	ctxpmYAMLStatus := "planned"
	localCLIStatus := ""
	gitHooksStatus := "planned"
	if !opts.DryRun {
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "release", Name: "ctxpm"})
		bundled, err := ensureBundledCtxpm(a.Root, m.Agents, currentCtxpmVersion)
		if err != nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: "ctxpm", Status: "failed", Err: err})
			return nil, err
		}
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: "ctxpm", Status: bundled.Status, Version: currentCtxpmVersion})
		ctxpmResource = bundled.Resource
		upsertManagedDependency(&m.Dependencies, ctxpmResource)
		files = append(files, bundled.Files...)
		ctxpmStatus = bundled.Status
		ctxpmYAMLStatus = bundled.YAMLStatus
		localCLIStatus = bundled.LocalCLIStatus
		resourcePlan.warnings = append(resourcePlan.warnings, bundled.Warnings...)

		gitignoreRules := dedupe(append(
			[]string{".ctxpm/dependencies/", ".ctxpm/state/", ".ctxpm/cache/", ".githooks/"},
			compatibilityGitignoreRules(m.Agents, m.Dependencies)...,
		))
		gitignoreRules = dedupe(append(gitignoreRules, compatibilityGitignoreRules(m.Agents, m.Packages)...))
		updated, err := ensureGitignoreRules(gitignorePath, gitignoreRules)
		if err != nil {
			return nil, err
		}
		migrated, err := migrateEntrypointGitignore(a.Root, managedEntrypointRules(m))
		if err != nil {
			return nil, err
		}
		gitignoreUpdated = updated || migrated
		resourcePlan.warnings = append(resourcePlan.warnings, entrypointCommitGuidance(a.Root, m)...)
		if _, err := manifest.Save(a.Root, m); err != nil {
			return nil, err
		}
		status, err := a.enableHooksByDefault(context.Background())
		if err != nil {
			gitHooksStatus = "failed"
			resourcePlan.warnings = append(resourcePlan.warnings, fmt.Sprintf("could not enable default Git hooks: %v", err))
		} else {
			gitHooksStatus = status
		}
	} else {
		ctxpmStatus = "planned"
		ctxpmYAMLStatus = "planned"
		localCLIStatus = "planned"
	}
	files = append(files, resourcePlan.files...)
	sort.Strings(files)

	return &InitResult{
		ManifestPath:               manifestPath,
		Files:                      dedupe(files),
		DryRun:                     opts.DryRun,
		Agent:                      opts.Agent,
		EntrypointFile:             manifest.CanonicalEntrypointFile(),
		EntrypointSourceFile:       manifest.CanonicalEntrypointSourceFile(),
		PackagesCreated:            resourceNames(resourcePlan.packages),
		DependenciesCreated:        dedupe(append(resourceNames(resourcePlan.dependencies), "ctxpm")),
		MigratedResources:          resourcePlan.migrated,
		GitignoreUpdated:           gitignoreUpdated,
		OwnershipConfirmed:         nil,
		OwnershipInferred:          dedupe(append(discovery.evidence, resourcePlan.ownershipInferred...)),
		UnresolvedResources:        dedupe(resourcePlan.unresolved),
		CtxpmDependencyStatus:      ctxpmStatus,
		CtxpmCompatibilityComplete: len(manifest.DerivedCompatibilityPaths(m.Agents, ctxpmResource)) > 0,
		ManagedEntrypointUpdated:   true,
		CtxpmYAMLStatus:            ctxpmYAMLStatus,
		LocalCLIStatus:             localCLIStatus,
		GitHooksStatus:             gitHooksStatus,
		Warnings:                   dedupe(append(discovery.warnings, resourcePlan.warnings...)),
		AgentDetectionEvidence:     discovery.evidence,
		AgentDetectionSource:       discovery.entrypointSource,
	}, nil
}

type AddOptions struct {
	SourceURL  string
	Type       string
	Name       string
	Layout     string
	SourceType string
	SourcePath string
	TargetPath string
	Ref        string
	Entry      string
	Files      []string
	DryRun     bool
	Progress   func(ProgressEvent)
}

type AddResult struct {
	Status       string            `json:"status"`
	ManifestPath string            `json:"manifest_path"`
	Resource     manifest.Resource `json:"resource"`
}

func (r AddResult) Text() string {
	return fmt.Sprintf("Add status: %s\n- %s [%s] %s\n", r.Status, r.Resource.Name, r.Resource.Source.Type, r.Resource.Path)
}

func (a *App) Add(ctx context.Context, opts AddOptions) (*AddResult, error) {
	if !opts.DryRun {
		var unlock func()
		var err error
		ctx, unlock, err = acquireProjectWrite(ctx, a.Root)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	if err := ensureManifestVersion(a.Root, false); err != nil {
		return nil, err
	}
	m, manifestPath, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	detected, err := source.Detect(ctx, source.DetectionInput{
		RawURL:       opts.SourceURL,
		ResourceType: opts.Type,
		Name:         opts.Name,
		Layout:       opts.Layout,
		SourceType:   opts.SourceType,
		SourcePath:   opts.SourcePath,
		TargetPath:   opts.TargetPath,
		Ref:          opts.Ref,
		Entry:        opts.Entry,
		Files:        opts.Files,
	})
	if err != nil {
		return nil, err
	}
	if m.HasResource(detected.Name) {
		return nil, fmt.Errorf("resource %q already exists in ctxpm.yaml", detected.Name)
	}

	if !opts.DryRun {
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "dependency", Name: detected.Name})
		installed, err := a.installResource(ctx, m.Agents, &detected.Resource, "")
		if err != nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: detected.Name, Status: "failed", Err: err})
			return nil, err
		}
		if installed.Version != "" {
			detected.Resource.Version = installed.Version
		}
		if _, err := manifest.AddDependency(a.Root, detected.Resource); err != nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: detected.Name, Status: "failed", Err: err})
			return nil, err
		}
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: detected.Name, Status: "added", Version: detected.Resource.Version})
	} else {
		version, err := a.resolveLatestVersion(ctx, detected.Resource)
		if err == nil {
			detected.Resource.Version = version
		}
	}

	return &AddResult{
		Status:       ternary(opts.DryRun, "dry_run", "added"),
		ManifestPath: manifestPath,
		Resource:     detected.Resource,
	}, nil
}

type ResourceListItem struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Path       string `json:"path"`
	SourceType string `json:"source_type,omitempty"`
	Version    string `json:"version,omitempty"`
	Status     string `json:"status"`
}

type ListOptions struct {
	Type string
	Kind string
}

type ListResult struct {
	Items []ResourceListItem `json:"items"`
}

func (r ListResult) Text() string {
	lines := []string{}
	for _, item := range r.Items {
		line := fmt.Sprintf("- %s [%s/%s] path=%s status=%s", item.Name, item.Kind, item.Type, item.Path, item.Status)
		if item.SourceType != "" {
			line += " source=" + item.SourceType
		}
		if item.Version != "" {
			line += " version=" + item.Version
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "No resources found.\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) List(opts ListOptions) (*ListResult, error) {
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	items := []ResourceListItem{}
	appendResource := func(kind string, resource manifest.Resource) {
		if opts.Kind != "" && opts.Kind != kind {
			return
		}
		if opts.Type != "" && opts.Type != resource.Type {
			return
		}
		item := ResourceListItem{
			Kind:    kind,
			Name:    resource.Name,
			Type:    resource.Type,
			Path:    resource.Path,
			Version: resource.Version,
			Status:  localStatus(a.Root, m.Agents, resource),
		}
		if resource.Source != nil {
			item.SourceType = resource.Source.NormalizedType()
		}
		items = append(items, item)
	}
	for _, dep := range m.Dependencies {
		appendResource("dependency", dep)
	}
	for _, pkg := range m.Packages {
		appendResource("package", pkg)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return items[i].Name < items[j].Name
		}
		return items[i].Kind < items[j].Kind
	})
	return &ListResult{Items: items}, nil
}

type ValidateResult struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues"`
}

func (r ValidateResult) Text() string {
	if r.OK {
		return "Validation succeeded.\n"
	}
	lines := []string{"Validation failed:"}
	for _, issue := range r.Issues {
		lines = append(lines, "- "+issue)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Validate() (*ValidateResult, error) {
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	issues := []string{}
	if err := m.Validate(); err != nil {
		issues = append(issues, err.Error())
	}
	issues = append(issues, validateManagedEntrypoints(a.Root, m)...)
	for _, dep := range m.Dependencies {
		abs := filepath.Join(a.Root, filepath.FromSlash(dep.Path))
		if err := validateResolvedResource(abs, dep); err != nil {
			issues = append(issues, err.Error())
		}
		issues = append(issues, validateMemoryResource(abs, dep)...)
		for _, compat := range manifest.DerivedCompatibilityPaths(m.Agents, dep) {
			compatAbs := filepath.Join(a.Root, filepath.FromSlash(compat))
			if _, err := os.Lstat(compatAbs); err != nil {
				issues = append(issues, fmt.Sprintf("compatibility path %q is missing", compat))
			}
		}
	}
	for _, pkg := range m.Packages {
		abs := filepath.Join(a.Root, filepath.FromSlash(pkg.Path))
		if err := validateResolvedResource(abs, pkg); err != nil {
			issues = append(issues, err.Error())
		}
		issues = append(issues, validateMemoryResource(abs, pkg)...)
		for _, compat := range manifest.DerivedCompatibilityPaths(m.Agents, pkg) {
			compatAbs := filepath.Join(a.Root, filepath.FromSlash(compat))
			if _, err := os.Lstat(compatAbs); err != nil {
				issues = append(issues, fmt.Sprintf("compatibility path %q is missing", compat))
			}
		}
	}
	return &ValidateResult{OK: len(issues) == 0, Issues: issues}, nil
}

type InstallOptions struct {
	Type                string
	Only                string
	Force               bool
	DryRun              bool
	CurrentVersion      string
	BundledCtxpmRelease bool
	SkipCtxpmRelease    bool
	SkipDependencies    map[string]bool
	Progress            func(ProgressEvent)
}

type ProgressPhase string

const (
	ProgressStart ProgressPhase = "start"
	ProgressDone  ProgressPhase = "done"
)

type ProgressEvent struct {
	Phase   ProgressPhase
	Kind    string
	Name    string
	Version string
	Status  string
	Err     error
}

func emitProgress(fn func(ProgressEvent), ev ProgressEvent) {
	if fn != nil {
		fn(ev)
	}
}

type InstallAction struct {
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

type InstallResult struct {
	Status   string          `json:"status"`
	Actions  []InstallAction `json:"actions"`
	Warnings []string        `json:"warnings,omitempty"`
}

func (r InstallResult) Text() string {
	lines := []string{"Install status: " + r.Status}
	for _, item := range r.Actions {
		line := fmt.Sprintf("- %s [%s]", item.Name, item.Status)
		if item.Kind != "" {
			line += " kind=" + item.Kind
		}
		if item.Version != "" {
			line += " version=" + item.Version
		}
		lines = append(lines, line)
	}
	for _, warning := range r.Warnings {
		lines = append(lines, "- "+warning)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Install(ctx context.Context, opts InstallOptions) (*InstallResult, error) {
	ctx = withGitFetchScope(ctx)
	var unlock func()
	if !opts.DryRun {
		var err error
		ctx, unlock, err = acquireProjectWrite(ctx, a.Root)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	if err := ensureManifestVersion(a.Root, false); err != nil {
		return nil, err
	}
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	discoveredCanonical, err := discoverCanonicalResources(a.Root)
	if err != nil {
		return nil, err
	}
	manifestChanged := normalizeSharedEntrypoints(m)
	for _, candidate := range discoveredCanonical {
		if m.HasResource(candidate.resource.Name) {
			continue
		}
		switch candidate.kind {
		case "dependency":
			m.Dependencies = append(m.Dependencies, candidate.resource)
		default:
			m.Packages = append(m.Packages, candidate.resource)
		}
		manifestChanged = true
	}
	if manifestChanged {
		sortResources(m.Dependencies)
		sortResources(m.Packages)
		if !opts.DryRun {
			if _, err := manifest.Save(a.Root, m); err != nil {
				return nil, err
			}
			manifestChanged = false
		}
	}
	overlaps, err := resourcePathOverlaps(a.Root, m)
	if err != nil {
		return nil, err
	}
	actions := []InstallAction{}
	versionUpdates := map[string]string{}
	installState := loadInstallState(a.Root)
	installStateChanged := false
	installedResources := []manifest.Resource{}
	currentCtxpmVersion := currentCtxpmManifestVersion(opts.CurrentVersion)
	ctxpmReleaseTarget := ""
	completedTrees := map[string]bool{}
	allResources := append(append([]manifest.Resource(nil), m.Dependencies...), m.Packages...)
	for i := range m.Dependencies {
		dep := &m.Dependencies[i]
		if completedTrees[dep.Name] {
			continue
		}
		if opts.SkipDependencies[dep.Name] {
			continue
		}
		if dep.Name == "ctxpm" && opts.SkipCtxpmRelease {
			continue
		}
		if opts.Type != "" && dep.Type != opts.Type {
			continue
		}
		if opts.Only != "" && dep.Name != opts.Only {
			continue
		}
		if opts.DryRun {
			status := "would_install"
			kind := "dependency"
			if dep.Name == "ctxpm" && opts.BundledCtxpmRelease {
				status = "would_install_bundled"
				kind = "release"
			} else if dep.Name == "ctxpm" && isCtxpmReleaseVersion(dep.Version) {
				status = "would_install_release"
				kind = "release"
			} else if dep.Source == nil {
				status = "would_link"
			}
			actions = append(actions, InstallAction{Kind: kind, Name: dep.Name, Status: status, Version: dep.Version})
			continue
		}
		if dep.Name == "ctxpm" && opts.BundledCtxpmRelease {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "release", Name: dep.Name})
			previousVersion := dep.Version
			bundleVersion := dep.Version
			if isCtxpmReleaseVersion(currentCtxpmVersion) {
				bundleVersion = currentCtxpmVersion
			}
			bundled, err := ensureBundledCtxpm(a.Root, m.Agents, bundleVersion)
			if err != nil {
				emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: dep.Name, Status: "failed", Err: err})
				return nil, err
			}
			if bundled.Resource.Version != "" {
				dep.Version = bundled.Resource.Version
			}
			if previousVersion != dep.Version {
				versionUpdates[dep.Name] = dep.Version
			}
			actions = append(actions, InstallAction{Kind: "release", Name: dep.Name, Status: "installed_bundled", Version: dep.Version})
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: dep.Name, Status: "installed_bundled", Version: dep.Version})
			if bundled.LocalCLIStatus != "" {
				actions = append(actions, InstallAction{Kind: "tool", Name: "ctxpm local cli", Status: bundled.LocalCLIStatus})
			}
			installedResources = append(installedResources, *dep)
			continue
		}
		if dep.Name == "ctxpm" && isCtxpmReleaseVersion(dep.Version) {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "release", Name: dep.Name})
			ctxpmReleaseTarget = canonicalCtxpmReleaseVersion(dep.Version)
			installedResources = append(installedResources, *dep)
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: dep.Name, Status: "release_pending", Version: dep.Version})
			continue
		}
		if dep.Source == nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "dependency", Name: dep.Name})
			if err := ensureResourcePresence(a.Root, *dep, "dependency"); err != nil {
				emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: "failed", Err: err})
				return nil, err
			}
			if err := ensureCompatibility(a.Root, m.Agents, *dep); err != nil {
				emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: "failed", Err: err})
				return nil, err
			}
			actions = append(actions, InstallAction{Kind: "dependency", Name: dep.Name, Status: "linked", Version: dep.Version})
			installedResources = append(installedResources, *dep)
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: "linked", Version: dep.Version})
			if dep.Name == "ctxpm" {
				cliPath := filepath.Join(a.Root, ".ctxpm/dependencies/skills/ctxpm/cli/ctxpm")
				status, _ := prepareBundledCLI(ctx, cliPath, a.Root)
				actions = append(actions, InstallAction{Kind: "tool", Name: "ctxpm local cli", Status: status})
			}
			continue
		}
		if !opts.Force && overlayDependencyIsCurrent(a.Root, *dep, allResources, installState.Dependencies[dep.Name]) {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "dependency", Name: dep.Name})
			if err := ensureCompatibility(a.Root, m.Agents, *dep); err != nil {
				emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: "failed", Err: err})
				return nil, err
			}
			actions = append(actions, InstallAction{Kind: "dependency", Name: dep.Name, Status: "cached", Version: dep.Version})
			installedResources = append(installedResources, *dep)
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: "cached", Version: dep.Version})
			continue
		}
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "dependency", Name: dep.Name})
		if dep.Name != "ctxpm" && strings.TrimSpace(dep.Version) != "" {
			preserve := map[string]bool{}
			for _, member := range m.Dependencies {
				if (opts.Only != "" && member.Name != opts.Only) || (opts.Type != "" && member.Type != opts.Type) || opts.SkipDependencies[member.Name] {
					preserve[member.Name] = true
				}
			}
			trees, err := buildResourceTrees(a.Root, m, map[string]bool{dep.Name: true}, preserve)
			if err != nil {
				return nil, err
			}
			if err := a.commitPreparedResourceTree(ctx, m, trees[0], map[string]string{}, &installState); err != nil {
				return nil, fmt.Errorf("install resource tree %q: %w", dep.Name, err)
			}
			for _, member := range trees[0].Members {
				if trees[0].preserve[member.Name] {
					continue
				}
				completedTrees[member.Name] = true
				if err := ensureCompatibility(a.Root, m.Agents, member); err != nil {
					return nil, err
				}
				installedResources = append(installedResources, member)
				actions = append(actions, InstallAction{Kind: "dependency", Name: member.Name, Status: "installed", Version: member.Version})
				emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: member.Name, Status: "installed", Version: member.Version})
			}
			continue
		}
		if err := checkParentReplacement(a.Root, overlaps, dep.Name); err != nil {
			return nil, err
		}
		installed, err := a.installResource(ctx, m.Agents, dep, dep.Version)
		if err != nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: "failed", Err: err})
			return nil, err
		}
		previousVersion := dep.Version
		dep.Version = installed.Version
		if previousVersion != dep.Version {
			versionUpdates[dep.Name] = dep.Version
		}
		if err := recordInstalledDependency(a.Root, &installState, *dep); err != nil {
			return nil, err
		}
		installStateChanged = true
		actions = append(actions, InstallAction{Kind: "dependency", Name: dep.Name, Status: installed.Status, Version: dep.Version})
		installedResources = append(installedResources, *dep)
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: installed.Status, Version: dep.Version})
		if dep.Name == "ctxpm" {
			cliPath := filepath.Join(a.Root, ".ctxpm/dependencies/skills/ctxpm/cli/ctxpm")
			status, _ := prepareBundledCLI(ctx, cliPath, a.Root)
			actions = append(actions, InstallAction{Kind: "tool", Name: "ctxpm local cli", Status: status})
		}
	}
	for i := range m.Packages {
		pkg := &m.Packages[i]
		if opts.Type != "" && pkg.Type != opts.Type {
			continue
		}
		if opts.Only != "" && pkg.Name != opts.Only {
			continue
		}
		if opts.DryRun {
			actions = append(actions, InstallAction{Kind: "package", Name: pkg.Name, Status: "would_link"})
			continue
		}
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "package", Name: pkg.Name})
		if err := ensureResourcePresence(a.Root, *pkg, "package"); err != nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "package", Name: pkg.Name, Status: "failed", Err: err})
			return nil, err
		}
		if err := ensureCompatibility(a.Root, m.Agents, *pkg); err != nil {
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "package", Name: pkg.Name, Status: "failed", Err: err})
			return nil, err
		}
		actions = append(actions, InstallAction{Kind: "package", Name: pkg.Name, Status: "linked"})
		installedResources = append(installedResources, *pkg)
		emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "package", Name: pkg.Name, Status: "linked"})
	}
	if !opts.DryRun && manifestChanged {
		if _, err := manifest.Save(a.Root, m); err != nil {
			return nil, err
		}
	} else if !opts.DryRun && len(versionUpdates) > 0 {
		if _, err := manifest.UpdateResourceVersions(a.Root, versionUpdates); err != nil {
			return nil, err
		}
	}
	if !opts.DryRun && installStateChanged {
		if err := saveInstallState(a.Root, installState); err != nil {
			return nil, err
		}
	}
	if !opts.DryRun && shouldSyncEntrypointFromActiveCLI(m, currentCtxpmVersion, opts.BundledCtxpmRelease, ctxpmReleaseTarget) {
		if _, err := syncManagedEntrypoints(a.Root, m, true); err != nil {
			return nil, err
		}
	}
	if !opts.DryRun {
		gitignoreRules := dedupe(append(
			[]string{".ctxpm/dependencies/", ".ctxpm/state/", ".ctxpm/cache/", ".githooks/"},
			compatibilityGitignoreRules(m.Agents, installedResources)...,
		))
		if _, err := ensureGitignoreRules(filepath.Join(a.Root, ".gitignore"), gitignoreRules); err != nil {
			return nil, err
		}
	}
	if !opts.DryRun && ctxpmReleaseTarget != "" {
		unlock()
		ctx = context.WithValue(ctx, projectWriteKey{}, nil)
		releaseResult, err := runCtxpmReleaseInstall(a, ctx, CtxpmReleaseUpdateOptions{
			Version:        ctxpmReleaseTarget,
			CurrentVersion: currentCtxpmVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to install ctxpm release %s: %w", ctxpmReleaseTarget, err)
		}
		status := "installed_release"
		if releaseResult.Status == "up_to_date" {
			status = "installed_bundled"
		}
		actions = append(actions, InstallAction{Kind: "release", Name: "ctxpm", Status: status, Version: ctxpmReleaseTarget})
	}
	if !opts.DryRun {
		// A locked release may invoke an older CLI that recreates the old rules.
		// Reacquire the write lock before migrating after that subprocess returns.
		if ctxpmReleaseTarget != "" {
			var err error
			ctx, unlock, err = acquireProjectWrite(ctx, a.Root)
			if err != nil {
				return nil, err
			}
			defer unlock()
		}
		if migrated, err := migrateEntrypointGitignore(a.Root, managedEntrypointRules(m)); err != nil {
			return nil, err
		} else if migrated {
			actions = append(actions, InstallAction{Kind: "entrypoint", Name: ".gitignore", Status: "migrated"})
		}
		status, err := a.enableHooksByDefault(ctx)
		if err != nil {
			actions = append(actions, InstallAction{Kind: "hook", Name: "git hooks", Status: "failed"})
		} else if status != "not_git_repository" {
			actions = append(actions, InstallAction{Kind: "hook", Name: "git hooks", Status: status})
		}
	}
	var warnings []string
	if !opts.DryRun {
		warnings = entrypointCommitGuidance(a.Root, m)
	}
	return &InstallResult{Status: ternary(opts.DryRun, "dry_run", "applied"), Actions: actions, Warnings: warnings}, nil
}

func shouldSyncEntrypointFromActiveCLI(m *manifest.Manifest, currentVersion string, bundledRelease bool, pendingRelease string) bool {
	if bundledRelease {
		return true
	}
	if pendingRelease != "" {
		return false
	}
	ctxpm, ok := findDependency(m.Dependencies, "ctxpm")
	if !ok || !isCtxpmReleaseVersion(ctxpm.Version) {
		return true
	}
	return canonicalCtxpmReleaseVersion(ctxpm.Version) == canonicalCtxpmReleaseVersion(currentVersion)
}

type CheckUpdatesOptions struct {
	Names          []string
	Explicit       bool
	Force          bool
	CurrentVersion string
	Progress       func(ProgressEvent)
}

type DependencyUpdate struct {
	Name           string                       `json:"name"`
	Type           string                       `json:"type"`
	Path           string                       `json:"path"`
	SourceType     string                       `json:"source_type"`
	CurrentVersion string                       `json:"current_version,omitempty"`
	LatestVersion  string                       `json:"latest_version,omitempty"`
	Status         string                       `json:"status"`
	Reason         string                       `json:"reason,omitempty"`
	Compatibility  []string                     `json:"compatibility,omitempty"`
	Components     []CtxpmReleaseComponentCheck `json:"components,omitempty"`
}

type CheckUpdatesResult struct {
	cacheFingerprint string
	Status           string                `json:"status"`
	CheckedAt        string                `json:"checked_at,omitempty"`
	LastFullCheckAt  string                `json:"last_full_check_at,omitempty"`
	NextCheckAt      string                `json:"next_check_at,omitempty"`
	Policy           manifest.UpdatePolicy `json:"policy"`
	Dependencies     []DependencyUpdate    `json:"dependencies"`
}

func (r CheckUpdatesResult) Text() string {
	lines := []string{"Update check status: " + r.Status}
	if r.CheckedAt != "" {
		lines = append(lines, "Checked at: "+r.CheckedAt)
	}
	if r.NextCheckAt != "" {
		lines = append(lines, "Next check at: "+r.NextCheckAt)
	}
	for _, dep := range r.Dependencies {
		line := fmt.Sprintf("- %s [%s]", dep.Name, dep.Status)
		if dep.CurrentVersion != "" {
			line += " current=" + dep.CurrentVersion
		}
		if dep.LatestVersion != "" {
			line += " latest=" + dep.LatestVersion
		}
		if dep.Reason != "" {
			line += " reason=" + dep.Reason
		}
		lines = append(lines, line)
		for _, component := range dep.Components {
			lines = append(lines, fmt.Sprintf("  - %s [%s]", component.Name, component.Status))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) CheckUpdates(ctx context.Context, opts CheckUpdatesOptions) (*CheckUpdatesResult, error) {
	ctx = withGitFetchScope(ctx)
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	policy := manifest.DefaultPolicy()
	if m.UpdatePolicy.Enabled != nil {
		policy.Enabled = m.UpdatePolicy.Enabled
	}
	if m.UpdatePolicy.IncludeSelf != nil {
		policy.IncludeSelf = m.UpdatePolicy.IncludeSelf
	}
	if m.UpdatePolicy.Interval != "" {
		policy.Interval = m.UpdatePolicy.Interval
	}

	now := time.Now().UTC()
	statePath := filepath.Join(a.Root, ".ctxpm", "state", "update-checks.json")
	if !opts.Explicit && !boolValue(policy.Enabled, true) {
		return &CheckUpdatesResult{
			Status:       "disabled",
			CheckedAt:    now.Format(time.RFC3339),
			Policy:       policy,
			Dependencies: []DependencyUpdate{},
		}, nil
	}

	if !opts.Force && len(opts.Names) == 0 {
		if cached, ok := maybeReuseCheckState(statePath, now, policy.Interval, checkManifestFingerprint(m)); ok {
			cached.Policy = policy
			ok = a.refreshCachedCtxpmCheck(cached, m, opts.CurrentVersion, boolValue(policy.IncludeSelf, true))
			if ok {
				return cached, nil
			}
		}
	}

	selected := map[string]bool{}
	for _, name := range opts.Names {
		if _, ok := findDependency(m.Dependencies, name); !ok {
			return nil, fmt.Errorf("dependency %q was not found", name)
		}
		selected[name] = true
	}
	deps := make([]manifest.Resource, 0, len(m.Dependencies))
	for _, dep := range m.Dependencies {
		if len(selected) > 0 && !selected[dep.Name] {
			continue
		}
		if dep.Name == "ctxpm" && !boolValue(policy.IncludeSelf, true) && !selected[dep.Name] {
			continue
		}
		deps = append(deps, dep)
	}
	ctx, githubScope := withGitHubCheckScope(ctx, a.Root, deps)
	results := make([]DependencyUpdate, len(deps))
	jobs := make(chan int)
	var workers sync.WaitGroup
	var progressMu, gitMu sync.Mutex
	progress := func(ev ProgressEvent) {
		progressMu.Lock()
		defer progressMu.Unlock()
		emitProgress(opts.Progress, ev)
	}
	for worker := 0; worker < 4 && worker < len(deps); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				dep := deps[i]
				if dep.Name == "ctxpm" {
					results[i] = a.checkCtxpmDependency(ctx, dep, m.Agents, opts.CurrentVersion)
					continue
				}
				progress(ProgressEvent{Phase: ProgressStart, Kind: "dependency", Name: dep.Name})
				// Non-GitHub Git uses the filesystem cache. Keep these resolutions
				// serial until the cache lock is safe for concurrent operations.
				serialGit := false
				if dep.Source != nil && dep.Source.NormalizedType() == "git" {
					_, _, github := githubRepository(dep.Source.URL)
					serialGit = !github
				}
				if serialGit {
					gitMu.Lock()
				}
				checkCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
				version, err := a.resolveLatestVersion(checkCtx, dep)
				cancel()
				if serialGit {
					gitMu.Unlock()
				}
				item := DependencyUpdate{Name: dep.Name, Type: dep.Type, Path: dep.Path,
					CurrentVersion: dep.Version, Compatibility: manifest.DerivedCompatibilityPaths(m.Agents, dep)}
				if dep.Source != nil {
					item.SourceType = dep.Source.NormalizedType()
				}
				if err != nil {
					item.Status, item.Reason = "unresolved", err.Error()
				} else {
					item.LatestVersion, item.Status = version, "up_to_date"
					if dep.Version != version {
						item.Status = "update_available"
					}
				}
				results[i] = item
				progress(ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: dep.Name, Status: item.Status, Version: version, Err: err})
			}
		}()
	}
	for i := range deps {
		jobs <- i
	}
	close(jobs)
	workers.Wait()
	// The cache only speeds up future checks; a failed cache write must not
	// discard otherwise valid remote results.
	_ = githubScope.save()
	result := &CheckUpdatesResult{
		cacheFingerprint: checkManifestFingerprint(m),
		Status:           "checked",
		CheckedAt:        now.Format(time.RFC3339),
		Policy:           policy,
		Dependencies:     results,
	}
	if len(opts.Names) == 0 {
		if err := writeCheckState(statePath, result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (a *App) checkCtxpmDependency(ctx context.Context, dep manifest.Resource, agents []string, currentVersion string) DependencyUpdate {
	check := a.CheckCtxpmRelease(ctx, currentVersion)
	return ctxpmReleaseDependencyUpdate(dep, agents, check)
}

func ctxpmReleaseDependencyUpdate(dep manifest.Resource, agents []string, check *CtxpmReleaseCheck) DependencyUpdate {
	installedVersion := strings.TrimSpace(dep.Version)
	if isCtxpmReleaseVersion(installedVersion) {
		installedVersion = canonicalCtxpmReleaseVersion(installedVersion)
	}
	return DependencyUpdate{
		Name:           dep.Name,
		Type:           dep.Type,
		Path:           dep.Path,
		SourceType:     "release",
		CurrentVersion: installedVersion,
		LatestVersion:  check.LatestVersion,
		Status:         check.Status,
		Reason:         check.Reason,
		Compatibility:  manifest.DerivedCompatibilityPaths(agents, dep),
		Components:     append([]CtxpmReleaseComponentCheck(nil), check.Components...),
	}
}

func (a *App) refreshCachedCtxpmCheck(result *CheckUpdatesResult, m *manifest.Manifest, currentVersion string, include bool) bool {
	index := -1
	for i := range result.Dependencies {
		if result.Dependencies[i].Name == "ctxpm" {
			index = i
			break
		}
	}
	var dep *manifest.Resource
	for i := range m.Dependencies {
		if m.Dependencies[i].Name == "ctxpm" {
			dep = &m.Dependencies[i]
			break
		}
	}
	if !include || dep == nil {
		if index >= 0 {
			result.Dependencies = append(result.Dependencies[:index], result.Dependencies[index+1:]...)
		}
		return true
	}
	if index < 0 {
		return false
	}
	local := a.checkCtxpmReleaseLocal(currentVersion)
	cached := result.Dependencies[index]
	applyCtxpmReleaseCheck(local, cached.LatestVersion, errorFromReason(cached.Reason))
	updated := ctxpmReleaseDependencyUpdate(*dep, m.Agents, local)
	result.Dependencies[index] = updated
	return true
}

func errorFromReason(reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil
	}
	return errors.New(reason)
}

type UpdateOptions struct {
	Names          []string
	All            bool
	Refresh        bool
	CurrentVersion string
	DryRun         bool
	Progress       func(ProgressEvent)
}

type UpdateAction struct {
	Kind           string `json:"kind,omitempty"`
	Name           string `json:"name"`
	Status         string `json:"status"`
	CurrentVersion string `json:"current_version,omitempty"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type UpdateResult struct {
	Status  string         `json:"status"`
	Error   string         `json:"error,omitempty"`
	Applied []UpdateAction `json:"applied"`
	Skipped []UpdateAction `json:"skipped"`
}

func (r UpdateResult) Text() string {
	lines := []string{"Update status: " + r.Status}
	if r.Error != "" {
		lines = append(lines, "Error: "+r.Error)
	}
	for _, item := range r.Applied {
		lines = append(lines, fmt.Sprintf("- %s [%s] %s -> %s", item.Name, item.Status, item.CurrentVersion, item.LatestVersion))
	}
	for _, item := range r.Skipped {
		line := fmt.Sprintf("- %s [%s]", item.Name, item.Status)
		if item.Reason != "" {
			line += " reason=" + item.Reason
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Update(ctx context.Context, opts UpdateOptions) (out *UpdateResult, outErr error) {
	ctx = withGitFetchScope(ctx)
	var unlock func()
	if !opts.DryRun {
		var err error
		ctx, unlock, err = acquireProjectWrite(ctx, a.Root)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	if err := ensureManifestVersion(a.Root, false); err != nil {
		return nil, err
	}
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	normalizeSharedEntrypoints(m)
	if !opts.All && len(opts.Names) == 0 {
		return nil, errors.New("no dependencies were selected for update")
	}
	for _, name := range opts.Names {
		if _, ok := findDependency(m.Dependencies, name); !ok {
			return nil, fmt.Errorf("dependency %q was not found", name)
		}
	}
	var names []string
	if !opts.All {
		names = opts.Names
	}
	var check *CheckUpdatesResult
	if !opts.Refresh {
		interval := m.UpdatePolicy.Interval
		if interval == "" {
			interval = manifest.DefaultPolicy().Interval
		}
		cached, ok := maybeReuseCheckState(filepath.Join(a.Root, ".ctxpm", "state", "update-checks.json"), time.Now().UTC(), interval, checkManifestFingerprint(m))
		includeSelf := boolValue(m.UpdatePolicy.IncludeSelf, true) || containsString(names, "ctxpm")
		if ok && a.refreshCachedCtxpmCheck(cached, m, opts.CurrentVersion, includeSelf) {
			available := make(map[string]bool, len(cached.Dependencies))
			for _, item := range cached.Dependencies {
				available[item.Name] = true
			}
			for _, name := range names {
				if !available[name] {
					ok = false
				}
			}
			if ok {
				check = cached
			}
		}
	}
	if check == nil {
		check, err = a.CheckUpdates(ctx, CheckUpdatesOptions{Force: true, Explicit: true, Names: names, CurrentVersion: opts.CurrentVersion})
		if err != nil {
			return nil, err
		}
	}
	initialFingerprint := checkManifestFingerprint(m)
	byName := map[string]DependencyUpdate{}
	for _, item := range check.Dependencies {
		byName[item.Name] = item
	}
	targets := map[string]bool{}
	if opts.All {
		for _, item := range check.Dependencies {
			if item.Status == "update_available" || item.Status == "unresolved" {
				targets[item.Name] = true
			}
		}
	} else {
		for _, name := range opts.Names {
			targets[name] = true
		}
	}
	if len(targets) == 0 {
		return nil, errors.New("no dependencies were selected for update")
	}
	result := &UpdateResult{Status: ternary(opts.DryRun, "dry_run", "applied")}
	active := map[string]bool{}
	installState := loadInstallState(a.Root)
	ctxpmSelected := targets["ctxpm"]
	selected := map[string]bool{}
	versions := map[string]string{}
	for _, dep := range m.Dependencies {
		if !targets[dep.Name] || dep.Name == "ctxpm" {
			continue
		}
		item, ok := byName[dep.Name]
		if !ok {
			return nil, fmt.Errorf("dependency %q was not found", dep.Name)
		}
		switch item.Status {
		case "update_available":
			selected[dep.Name] = true
			versions[dep.Name] = item.LatestVersion
			if opts.DryRun {
				result.Applied = append(result.Applied, UpdateAction{Name: dep.Name, Status: "would_update", CurrentVersion: dep.Version, LatestVersion: item.LatestVersion})
			}
		case "up_to_date":
			result.Skipped = append(result.Skipped, UpdateAction{Name: dep.Name, Status: "up_to_date"})
		default:
			result.Skipped = append(result.Skipped, UpdateAction{Name: dep.Name, Status: item.Status, Reason: item.Reason})
		}
	}
	defer func() {
		if outErr == nil {
			return
		}
		out = result
		result.Error = outErr.Error()
		result.Status = "failed"
		if len(result.Applied) > 0 {
			result.Status = "partial"
		}
		finished := map[string]bool{}
		for _, item := range result.Applied {
			finished[item.Name] = true
		}
		for _, item := range result.Skipped {
			finished[item.Name] = true
		}
		for _, dep := range m.Dependencies {
			item, ok := byName[dep.Name]
			if !targets[dep.Name] || !ok || item.Status != "update_available" || finished[dep.Name] {
				continue
			}
			status := "not_run"
			reason := ""
			if active[dep.Name] {
				status, reason = "failed", outErr.Error()
			}
			result.Skipped = append(result.Skipped, UpdateAction{Name: dep.Name, Status: status, CurrentVersion: item.CurrentVersion, LatestVersion: item.LatestVersion, Reason: reason})
		}
	}()
	trees, err := buildResourceTrees(a.Root, m, selected)
	if err != nil {
		return nil, err
	}
	if !opts.DryRun {
		for _, tree := range trees {
			currentManifest, _, err := manifest.Load(a.Root)
			if err != nil {
				return nil, err
			}
			if checkManifestFingerprint(currentManifest) != initialFingerprint {
				return nil, errors.New("manifest changed since the update plan was created; run the update again")
			}
			treeVersions := map[string]string{}
			oldVersions := map[string]string{}
			for _, member := range tree.Members {
				if v, ok := versions[member.Name]; ok {
					active[member.Name] = true
					treeVersions[member.Name] = v
					oldVersions[member.Name] = member.Version
					emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "dependency", Name: member.Name})
				}
			}
			if err := a.commitPreparedResourceTree(ctx, m, tree, treeVersions, &installState); err != nil {
				return nil, fmt.Errorf("update resource tree %q: %w", tree.Root.Name, err)
			}
			initialFingerprint = checkManifestFingerprint(m)
			for _, member := range tree.Members {
				if v, ok := treeVersions[member.Name]; ok {
					delete(active, member.Name)
					result.Applied = append(result.Applied, UpdateAction{Name: member.Name, Status: "updated", CurrentVersion: oldVersions[member.Name], LatestVersion: v})
					emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "dependency", Name: member.Name, Status: "updated", Version: v})
				}
			}
		}
		// The install step may invoke a new ctxpm process for release syncing;
		// let that operation acquire its own lock instead of inheriting ours.
		unlock()
		ctx = context.WithValue(ctx, projectWriteKey{}, nil)
		overlaps, err := resourcePathOverlaps(a.Root, m)
		if err != nil {
			return nil, err
		}
		skipParents := map[string]bool{}
		for _, overlap := range overlaps {
			if selected[overlap.Child] && !selected[overlap.Parent] {
				skipParents[overlap.Parent] = true
			}
		}
		if _, err := a.Install(ctx, InstallOptions{CurrentVersion: opts.CurrentVersion, SkipCtxpmRelease: ctxpmSelected, SkipDependencies: skipParents}); err != nil {
			return nil, err
		}
	}

	if ctxpmSelected {
		updateInfo, ok := byName["ctxpm"]
		if !ok {
			return nil, errors.New("dependency \"ctxpm\" was not found")
		}
		switch updateInfo.Status {
		case "up_to_date":
			result.Skipped = append(result.Skipped, UpdateAction{Name: "ctxpm", Status: "up_to_date"})
		case "update_available":
			if opts.DryRun {
				result.Applied = append(result.Applied, UpdateAction{Name: "ctxpm", Kind: "release", Status: "would_update", CurrentVersion: updateInfo.CurrentVersion, LatestVersion: updateInfo.LatestVersion})
				break
			}
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressStart, Kind: "release", Name: "ctxpm"})
			active["ctxpm"] = true
			ctxpmResult, err := runCtxpmReleaseUpdate(a, ctx, CtxpmReleaseUpdateOptions{Version: updateInfo.LatestVersion, CurrentVersion: opts.CurrentVersion, Force: true})
			if err != nil {
				emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: "ctxpm", Status: "failed", Err: err})
				return nil, err
			}
			result.Applied = append(result.Applied, UpdateAction{Name: "ctxpm", Kind: "release", Status: ctxpmResult.Status, CurrentVersion: ctxpmResult.CurrentVersion, LatestVersion: ctxpmResult.LatestVersion})
			delete(active, "ctxpm")
			emitProgress(opts.Progress, ProgressEvent{Phase: ProgressDone, Kind: "release", Name: "ctxpm", Status: ctxpmResult.Status, Version: ctxpmResult.LatestVersion})
		default:
			result.Skipped = append(result.Skipped, UpdateAction{Name: "ctxpm", Status: updateInfo.Status, Reason: updateInfo.Reason})
		}
	}
	return result, nil
}

type RemoveOptions struct {
	Name        string
	DeleteFiles bool
}

type RemoveResult struct {
	Status string `json:"status"`
	Kind   string `json:"kind"`
	Name   string `json:"name"`
}

func (r RemoveResult) Text() string {
	return fmt.Sprintf("Remove status: %s\n- %s [%s]\n", r.Status, r.Name, r.Kind)
}

func (a *App) Remove(opts RemoveOptions) (*RemoveResult, error) {
	_, unlock, err := acquireProjectWrite(context.Background(), a.Root)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if err := ensureManifestVersion(a.Root, false); err != nil {
		return nil, err
	}
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	for _, dep := range m.Dependencies {
		if dep.Name != opts.Name {
			continue
		}
		if opts.DeleteFiles {
			if err := removePaths(a.Root, m.Agents, dep); err != nil {
				return nil, err
			}
		}
		if _, err := manifest.RemoveDependency(a.Root, dep.Name); err != nil {
			return nil, err
		}
		return &RemoveResult{Status: ternary(opts.DeleteFiles, "removed_and_deleted", "removed"), Kind: "dependency", Name: dep.Name}, nil
	}
	for _, pkg := range m.Packages {
		if pkg.Name != opts.Name {
			continue
		}
		if opts.DeleteFiles {
			if err := removePaths(a.Root, m.Agents, pkg); err != nil {
				return nil, err
			}
		}
		if _, err := manifest.RemovePackage(a.Root, pkg.Name); err != nil {
			return nil, err
		}
		return &RemoveResult{Status: ternary(opts.DeleteFiles, "removed_and_deleted", "removed"), Kind: "package", Name: pkg.Name}, nil
	}
	return nil, fmt.Errorf("resource %q was not found", opts.Name)
}

type MigrationCandidate struct {
	Kind              string   `json:"kind"`
	Name              string   `json:"name"`
	Type              string   `json:"type"`
	OriginalPath      string   `json:"original_path"`
	CanonicalPath     string   `json:"canonical_path"`
	Compatibility     []string `json:"compatibility,omitempty"`
	Evidence          []string `json:"evidence,omitempty"`
	RequiresMigration bool     `json:"requires_migration"`
}

type DetectOptions struct {
	Agent string
}

type AgentEnrollmentStatus struct {
	Agent           string   `json:"agent"`
	Enrolled        bool     `json:"enrolled"`
	EntrypointOK    bool     `json:"entrypoint_ok"`
	CompatibilityOK bool     `json:"compatibility_ok"`
	MissingLinks    []string `json:"missing_links,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	Recognized      bool     `json:"recognized"`
	Warning         string   `json:"warning,omitempty"`
}

type DetectResult struct {
	Status          string                 `json:"status"`
	ManifestPath    string                 `json:"manifest_path"`
	Candidates      []MigrationCandidate   `json:"candidates"`
	Unresolved      []string               `json:"unresolved_resources,omitempty"`
	Warnings        []string               `json:"warnings,omitempty"`
	AgentEnrollment *AgentEnrollmentStatus `json:"agent_enrollment,omitempty"`
}

func (r DetectResult) Text() string {
	lines := []string{"Detection status: " + r.Status}
	for _, candidate := range r.Candidates {
		line := fmt.Sprintf("- %s [%s/%s] original=%s canonical=%s", candidate.Name, candidate.Kind, candidate.Type, candidate.OriginalPath, candidate.CanonicalPath)
		lines = append(lines, line)
	}
	if len(r.Unresolved) > 0 {
		lines = append(lines, "Unresolved:")
		for _, item := range r.Unresolved {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.Warnings) > 0 {
		lines = append(lines, "Warnings:")
		for _, item := range r.Warnings {
			lines = append(lines, "- "+item)
		}
	}
	if r.AgentEnrollment != nil {
		e := r.AgentEnrollment
		if !e.Enrolled || !e.EntrypointOK || !e.CompatibilityOK {
			lines = append(lines, fmt.Sprintf("Agent %q is not fully enrolled; run: %s", e.Agent, e.Suggestion))
			for _, link := range e.MissingLinks {
				lines = append(lines, "  missing compatibility link: "+link)
			}
		} else {
			lines = append(lines, fmt.Sprintf("Agent %q: enrolled, entrypoint and compatibility links OK", e.Agent))
		}
		if e.Warning != "" {
			lines = append(lines, "  warning: "+e.Warning)
		}
	}
	if r.ManifestPath != "" {
		lines = append(lines, "Manifest: "+r.ManifestPath)
	}
	return strings.Join(lines, "\n") + "\n"
}

func checkAgentEnrollment(root, agent string, m *manifest.Manifest) *AgentEnrollmentStatus {
	canonical := manifest.NormalizeAgent(agent)
	recognized := manifest.IsKnownAgent(agent)

	enrolled := false
	for _, a := range m.Agents {
		if manifest.NormalizeAgent(a) == canonical {
			enrolled = true
			break
		}
	}

	entrypointOK := false
	entrypointFile := manifest.EntrypointFile(canonical)
	if entrypointFile != "" {
		sourceAbs := filepath.Join(root, manifest.CanonicalEntrypointSourceFile())
		aliasAbs := filepath.Join(root, entrypointFile)
		relTarget, relErr := filepath.Rel(filepath.Dir(aliasAbs), sourceAbs)
		if relErr == nil {
			if current, err := os.Readlink(aliasAbs); err == nil && current == relTarget {
				entrypointOK = true
			}
		}
	}

	var missingLinks []string
	for _, dep := range m.Dependencies {
		for _, compat := range manifest.DerivedCompatibilityPaths([]string{canonical}, dep) {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(compat))); err != nil {
				missingLinks = append(missingLinks, compat)
			}
		}
	}
	for _, pkg := range m.Packages {
		for _, compat := range manifest.DerivedCompatibilityPaths([]string{canonical}, pkg) {
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(compat))); err != nil {
				missingLinks = append(missingLinks, compat)
			}
		}
	}
	compatOK := len(missingLinks) == 0

	status := &AgentEnrollmentStatus{
		Agent:           agent,
		Enrolled:        enrolled,
		EntrypointOK:    entrypointOK,
		CompatibilityOK: compatOK,
		MissingLinks:    missingLinks,
		Recognized:      recognized,
	}
	if !recognized {
		status.Warning = fmt.Sprintf("agent profile %q is not recognized; recognized profiles: %s", agent, strings.Join(manifest.KnownAgentNames(), ", "))
	} else if canonical != agent {
		status.Warning = fmt.Sprintf("agent profile %q resolved to %q", agent, canonical)
	}
	if !enrolled || !entrypointOK || !compatOK {
		status.Suggestion = fmt.Sprintf("ctxpm init --agent %s", canonical)
	}
	return status
}

func (a *App) Detect(opts DetectOptions) (*DetectResult, error) {
	m, manifestPath, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	plan, err := collectMigrationCandidates(a.Root, m, m.Project.Name)
	if err != nil {
		return nil, err
	}
	result := &DetectResult{
		Status:       "clean",
		ManifestPath: manifestPath,
		Unresolved:   plan.unresolved,
	}
	for _, candidate := range plan.candidates {
		result.Candidates = append(result.Candidates, migrationCandidateToResult(m.Agents, candidate))
	}
	if len(result.Candidates) > 0 {
		result.Status = "migration_candidates_found"
	}
	if len(result.Unresolved) > 0 {
		if result.Status == "clean" {
			result.Status = "blocked"
		} else {
			result.Status = "partial"
		}
	}
	if agent := strings.TrimSpace(opts.Agent); agent != "" {
		enrollment := checkAgentEnrollment(a.Root, agent, m)
		result.AgentEnrollment = enrollment
		if !enrollment.Enrolled || !enrollment.EntrypointOK || !enrollment.CompatibilityOK {
			if result.Status == "clean" {
				result.Status = "agent_not_enrolled"
			}
		}
	}
	return result, nil
}

type MigrateOptions struct {
	Paths  []string
	All    bool
	DryRun bool
}

type MigrationAction struct {
	Name          string `json:"name"`
	Kind          string `json:"kind,omitempty"`
	Status        string `json:"status"`
	OriginalPath  string `json:"original_path,omitempty"`
	CanonicalPath string `json:"canonical_path,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type MigrateResult struct {
	Status       string            `json:"status"`
	ManifestPath string            `json:"manifest_path,omitempty"`
	Applied      []MigrationAction `json:"applied"`
	Skipped      []MigrationAction `json:"skipped,omitempty"`
	Unresolved   []string          `json:"unresolved_resources,omitempty"`
	Validation   ValidateResult    `json:"validation"`
	Warnings     []string          `json:"warnings,omitempty"`
	Files        []string          `json:"files,omitempty"`
}

func (r MigrateResult) Text() string {
	lines := []string{"Migration status: " + r.Status}
	if r.ManifestPath != "" {
		lines = append(lines, "Manifest: "+r.ManifestPath)
	}
	if len(r.Applied) > 0 {
		lines = append(lines, "Applied:")
		for _, item := range r.Applied {
			line := fmt.Sprintf("- %s [%s] %s", item.Name, item.Status, item.OriginalPath)
			if item.CanonicalPath != "" {
				line += " -> " + item.CanonicalPath
			}
			lines = append(lines, line)
		}
	}
	if len(r.Skipped) > 0 {
		lines = append(lines, "Skipped:")
		for _, item := range r.Skipped {
			line := fmt.Sprintf("- %s [%s]", item.Name, item.Status)
			if item.Reason != "" {
				line += " reason=" + item.Reason
			}
			lines = append(lines, line)
		}
	}
	if len(r.Unresolved) > 0 {
		lines = append(lines, "Unresolved:")
		for _, item := range r.Unresolved {
			lines = append(lines, "- "+item)
		}
	}
	if len(r.Warnings) > 0 {
		lines = append(lines, "Warnings:")
		for _, item := range r.Warnings {
			lines = append(lines, "- "+item)
		}
	}
	lines = append(lines, "Validation: "+ternary(r.Validation.OK, "ok", "failed"))
	for _, issue := range r.Validation.Issues {
		lines = append(lines, "- "+issue)
	}
	if len(r.Files) > 0 {
		lines = append(lines, "Files touched:")
		for _, item := range r.Files {
			lines = append(lines, "- "+item)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Migrate(opts MigrateOptions) (*MigrateResult, error) {
	if !opts.DryRun {
		_, unlock, err := acquireProjectWrite(context.Background(), a.Root)
		if err != nil {
			return nil, err
		}
		defer unlock()
	}
	m, manifestPath, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	plan, err := collectMigrationCandidates(a.Root, m, m.Project.Name)
	if err != nil {
		return nil, err
	}
	selected, err := selectMigrationCandidates(plan.candidates, opts.Paths, opts.All)
	if err != nil {
		return nil, err
	}
	result := &MigrateResult{
		Status:       ternary(opts.DryRun, "dry_run", "applied"),
		ManifestPath: manifestPath,
		Unresolved:   dedupe(plan.unresolved),
	}
	if len(selected) == 0 {
		return nil, errors.New("no migration candidates were selected")
	}

	migratedResources := []manifest.Resource{}
	files := []string{manifestPath}
	for _, candidate := range selected {
		action := MigrationAction{
			Name:          candidate.resource.Name,
			Kind:          candidate.kind,
			OriginalPath:  candidate.original,
			CanonicalPath: candidate.resource.Path,
		}
		if m.HasResource(candidate.resource.Name) {
			action.Status = "blocked"
			action.Reason = "resource name already exists in ctxpm.yaml"
			result.Skipped = append(result.Skipped, action)
			continue
		}
		if opts.DryRun {
			action.Status = "would_migrate"
			result.Applied = append(result.Applied, action)
			continue
		}
		if err := moveResourceToCanonical(a.Root, candidate.original, candidate.resource.Path); err != nil {
			action.Status = "blocked"
			action.Reason = err.Error()
			result.Skipped = append(result.Skipped, action)
			continue
		}
		if err := ensureCompatibility(a.Root, m.Agents, candidate.resource); err != nil {
			return nil, err
		}
		switch candidate.kind {
		case "dependency":
			m.Dependencies = append(m.Dependencies, candidate.resource)
		default:
			m.Packages = append(m.Packages, candidate.resource)
		}
		migratedResources = append(migratedResources, candidate.resource)
		files = append(files, filepath.Join(a.Root, filepath.FromSlash(candidate.resource.Path)))
		for _, compat := range manifest.DerivedCompatibilityPaths(m.Agents, candidate.resource) {
			files = append(files, filepath.Join(a.Root, filepath.FromSlash(compat)))
		}
		action.Status = "migrated"
		result.Applied = append(result.Applied, action)
	}
	if !opts.DryRun {
		sortResources(m.Dependencies)
		sortResources(m.Packages)
		if _, err := manifest.Save(a.Root, m); err != nil {
			return nil, err
		}
		gitignoreRules := dedupe(append(
			[]string{".ctxpm/dependencies/", ".ctxpm/state/"},
			compatibilityGitignoreRules(m.Agents, migratedResources)...,
		))
		if _, err := ensureGitignoreRules(filepath.Join(a.Root, ".gitignore"), gitignoreRules); err != nil {
			return nil, err
		}
	}
	validation, err := a.Validate()
	if err != nil {
		return nil, err
	}
	result.Validation = *validation
	result.Files = dedupe(files)
	if !opts.DryRun {
		switch {
		case len(result.Applied) == 0 && len(result.Skipped) > 0:
			result.Status = "blocked"
		case len(result.Skipped) > 0:
			result.Status = "partial"
		}
	}
	return result, nil
}

func migrationCandidateToResult(agents []string, candidate discoveredResource) MigrationCandidate {
	derived := manifest.DerivedCompatibilityPaths(agents, candidate.resource)
	compatPaths := append([]string(nil), derived...)
	// Include the original path if it's not already in the derived set (transitional symlink).
	inDerived := false
	for _, p := range derived {
		if p == candidate.original {
			inDerived = true
			break
		}
	}
	if !inDerived && candidate.original != "" {
		compatPaths = append(compatPaths, candidate.original)
	}
	return MigrationCandidate{
		Kind:              candidate.kind,
		Name:              candidate.resource.Name,
		Type:              candidate.resource.Type,
		OriginalPath:      candidate.original,
		CanonicalPath:     candidate.resource.Path,
		Compatibility:     compatPaths,
		Evidence:          append([]string(nil), candidate.evidence...),
		RequiresMigration: candidate.requiresMigration,
	}
}

func selectMigrationCandidates(candidates []discoveredResource, selectors []string, all bool) ([]discoveredResource, error) {
	if all {
		return append([]discoveredResource(nil), candidates...), nil
	}
	selectors = filterEmptyStrings(selectors)
	if len(selectors) == 0 {
		return nil, nil
	}
	selected := []discoveredResource{}
	used := map[int]bool{}
	for _, selector := range selectors {
		matches := []int{}
		for i, candidate := range candidates {
			if migrationCandidateMatches(candidate, selector) {
				matches = append(matches, i)
			}
		}
		if len(matches) == 0 {
			return nil, fmt.Errorf("migration candidate %q was not found", selector)
		}
		if len(matches) > 1 {
			return nil, fmt.Errorf("migration candidate %q is ambiguous; use the original path", selector)
		}
		if used[matches[0]] {
			continue
		}
		used[matches[0]] = true
		selected = append(selected, candidates[matches[0]])
	}
	return selected, nil
}

func migrationCandidateMatches(candidate discoveredResource, selector string) bool {
	normalizedSelector := filepath.ToSlash(strings.TrimSpace(selector))
	if normalizedSelector == "" {
		return false
	}
	return normalizedSelector == candidate.original ||
		normalizedSelector == candidate.resource.Path ||
		selector == candidate.resource.Name
}

type installOutcome struct {
	Status  string
	Version string
}

func (a *App) installResource(ctx context.Context, agents []string, resource *manifest.Resource, versionOverride string) (*installOutcome, error) {
	resolved, err := a.resolveResource(ctx, *resource, resolveOptions{
		VersionOverride:    versionOverride,
		UseRecordedVersion: versionOverride == "",
	})
	if err != nil {
		return nil, err
	}
	defer resolved.Close()
	var activeCLISnapshot string
	var activeCLIMode os.FileMode
	if resource.Name == "ctxpm" {
		activeCLI, err := currentExecutablePath()
		if err != nil {
			return nil, fmt.Errorf("cannot preserve the active ctxpm CLI: %w", err)
		}
		info, err := os.Stat(activeCLI)
		if err != nil {
			return nil, fmt.Errorf("cannot inspect the active ctxpm CLI: %w", err)
		}
		tmp, err := os.CreateTemp("", "ctxpm-active-*")
		if err != nil {
			return nil, err
		}
		activeCLISnapshot = tmp.Name()
		if err := tmp.Close(); err != nil {
			_ = os.Remove(activeCLISnapshot)
			return nil, err
		}
		defer os.Remove(activeCLISnapshot)
		activeCLIMode = info.Mode()
		if err := copyFile(activeCLI, activeCLISnapshot, activeCLIMode); err != nil {
			return nil, fmt.Errorf("cannot preserve the active ctxpm CLI: %w", err)
		}
	}
	destination := filepath.Join(a.Root, filepath.FromSlash(resource.Path))
	if err := replacePath(resolved.LocalPath, destination); err != nil {
		return nil, err
	}
	if activeCLISnapshot != "" {
		cliPath := filepath.Join(destination, "cli", "ctxpm")
		if err := copyFile(activeCLISnapshot, cliPath, activeCLIMode); err != nil {
			return nil, fmt.Errorf("cannot restore the project-local ctxpm CLI: %w", err)
		}
	}
	if err := ensureCompatibility(a.Root, agents, *resource); err != nil {
		return nil, err
	}
	return &installOutcome{Status: "installed", Version: resolved.Version}, nil
}

func (a *App) resolveLatestVersion(ctx context.Context, resource manifest.Resource) (string, error) {
	if version, isGitHub, err := resolveGitHubPathVersion(ctx, resource); isGitHub {
		return version, err
	}
	resolved, err := a.resolveResource(ctx, resource, resolveOptions{})
	if err != nil {
		return "", err
	}
	defer resolved.Close()
	return resolved.Version, nil
}

func fetchURL(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request to %s failed with status %s", rawURL, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func runGit(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func ensureCompatibility(root string, agents []string, resource manifest.Resource) error {
	for _, compat := range manifest.DerivedCompatibilityPaths(agents, resource) {
		linkPath := filepath.Join(root, filepath.FromSlash(compat))
		targetPath := filepath.Join(root, filepath.FromSlash(resource.Path))
		if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
			return err
		}
		relTarget, err := filepath.Rel(filepath.Dir(linkPath), targetPath)
		if err != nil {
			return err
		}
		if current, err := os.Readlink(linkPath); err == nil {
			if current == relTarget {
				continue
			}
			if err := os.Remove(linkPath); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			if _, statErr := os.Lstat(linkPath); statErr == nil {
				return fmt.Errorf("compatibility path %s already exists and is not a symlink", compat)
			}
		}
		if _, err := os.Lstat(linkPath); err == nil {
			return fmt.Errorf("compatibility path %s already exists and is not a symlink", compat)
		}
		if err := os.Symlink(relTarget, linkPath); err != nil {
			return err
		}
	}
	return nil
}

func ensureResourcePresence(root string, resource manifest.Resource, kind string) error {
	targetPath := filepath.Join(root, filepath.FromSlash(resource.Path))
	if err := validateResolvedResource(targetPath, resource); err != nil {
		if kind == "" {
			return err
		}
		return fmt.Errorf("%s %w", kind, err)
	}
	return nil
}

func replacePath(sourcePath, destination string) error {
	return replacePathSafely(sourcePath, destination)
}

func copyDir(sourcePath, destination string) error {
	return filepath.Walk(sourcePath, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(sourcePath, current)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(current, target, info.Mode())
	})
}

func copyFile(sourcePath, destination string, mode os.FileMode) error {
	input, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer input.Close()

	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	return err
}

func localStatus(root string, agents []string, resource manifest.Resource) string {
	if err := validateResolvedResource(filepath.Join(root, filepath.FromSlash(resource.Path)), resource); err != nil {
		return "missing"
	}
	for _, compat := range manifest.DerivedCompatibilityPaths(agents, resource) {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(compat))); err != nil {
			return "drifted"
		}
	}
	return "installed"
}

func upsertManagedDependency(resources *[]manifest.Resource, resource manifest.Resource) {
	for i := range *resources {
		if (*resources)[i].Name == resource.Name {
			(*resources)[i] = resource
			return
		}
	}
	*resources = append(*resources, resource)
}

func sortResources(resources []manifest.Resource) {
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
}

func removePaths(root string, agents []string, resource manifest.Resource) error {
	for _, compat := range manifest.DerivedCompatibilityPaths(agents, resource) {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(compat))); err != nil {
			return err
		}
	}
	return os.RemoveAll(filepath.Join(root, filepath.FromSlash(resource.Path)))
}

func ensureGitignoreRules(path string, rules []string) (bool, error) {
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	builder := strings.Builder{}
	builder.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		builder.WriteString("\n")
	}
	updated := false
	for _, rule := range rules {
		if strings.Contains(existing, rule) {
			continue
		}
		builder.WriteString(rule)
		builder.WriteString("\n")
		updated = true
	}
	if !updated && existing != "" {
		return false, nil
	}
	return updated, os.WriteFile(path, []byte(builder.String()), 0o644)
}

func compatibilityGitignoreRules(agents []string, resources []manifest.Resource) []string {
	rules := []string{}
	for _, resource := range resources {
		rules = append(rules, compatibilityIgnoreRules(agents, resource)...)
	}
	return dedupe(rules)
}

type cachedCheckState struct {
	SchemaVersion   int                `json:"schema_version"`
	Fingerprint     string             `json:"fingerprint"`
	Status          string             `json:"status"`
	CheckedAt       string             `json:"checked_at"`
	LastFullCheckAt string             `json:"last_full_check_at"`
	NextCheckAt     string             `json:"next_check_at"`
	Dependencies    []DependencyUpdate `json:"dependencies"`
}

func checkManifestFingerprint(m *manifest.Manifest) string {
	data, _ := json.Marshal(struct {
		Agents       []string
		Dependencies []manifest.Resource
	}{m.Agents, m.Dependencies})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func maybeReuseCheckState(statePath string, now time.Time, interval, fingerprint string) (*CheckUpdatesResult, bool) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, false
	}
	var cached cachedCheckState
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	if cached.SchemaVersion != 4 || cached.Fingerprint != fingerprint {
		return nil, false
	}
	for _, dep := range cached.Dependencies {
		if dep.Status == "unresolved" {
			return nil, false
		}
	}
	last, err := time.Parse(time.RFC3339, cached.CheckedAt)
	if err != nil {
		return nil, false
	}
	dur, err := parseInterval(interval)
	if err != nil {
		return nil, false
	}
	if now.Sub(last) < dur {
		next := last.Add(dur)
		return &CheckUpdatesResult{
			Status:          "not_due",
			CheckedAt:       now.Format(time.RFC3339),
			LastFullCheckAt: last.Format(time.RFC3339),
			NextCheckAt:     next.Format(time.RFC3339),
			Dependencies:    cached.Dependencies,
		}, true
	}
	return nil, false
}

func writeCheckState(statePath string, result *CheckUpdatesResult) error {
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cachedCheckState{
		SchemaVersion: 4,
		Fingerprint:   result.cacheFingerprint,
		Status:        result.Status,
		CheckedAt:     result.CheckedAt,
		Dependencies:  result.Dependencies,
	}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(statePath), "update-checks-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), statePath)
}

func parseInterval(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "1d"
	}
	if len(raw) < 2 {
		return 0, fmt.Errorf("invalid interval %q", raw)
	}
	unit := raw[len(raw)-1]
	value := raw[:len(raw)-1]
	var number int
	if _, err := fmt.Sscanf(value, "%d", &number); err != nil {
		return 0, fmt.Errorf("invalid interval %q", raw)
	}
	switch unit {
	case 's':
		return time.Duration(number) * time.Second, nil
	case 'm':
		return time.Duration(number) * time.Minute, nil
	case 'h':
		return time.Duration(number) * time.Hour, nil
	case 'd':
		return time.Duration(number) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid interval unit %q", string(unit))
	}
}

func dedupe(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func appendIfMissing(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func filterEmptyStrings(values []string) []string {
	filtered := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func resourceNames(resources []manifest.Resource) []string {
	names := []string{}
	for _, resource := range resources {
		names = append(names, resource.Name)
	}
	sort.Strings(names)
	return names
}

func findDependency(resources []manifest.Resource, name string) (manifest.Resource, bool) {
	for _, resource := range resources {
		if resource.Name == name {
			return resource, true
		}
	}
	return manifest.Resource{}, false
}

func fallbackString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func boolValue(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return *value
}

func ternary(condition bool, yes, no string) string {
	if condition {
		return yes
	}
	return no
}
