package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type textResult interface {
	Text() string
}

type InitOptions struct {
	Agent       string
	ProjectName string
	Force       bool
	DryRun      bool
}

type InitResult struct {
	ManifestPath string   `json:"manifest_path"`
	Files        []string `json:"files"`
	DryRun       bool     `json:"dry_run"`
}

func (r InitResult) Text() string {
	lines := []string{
		fmt.Sprintf("Initialized ctxpm project (%s)", ternary(r.DryRun, "dry-run", "applied")),
		fmt.Sprintf("Manifest: %s", r.ManifestPath),
	}
	for _, item := range r.Files {
		lines = append(lines, "- "+item)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Init(opts InitOptions) (*InitResult, error) {
	if strings.TrimSpace(opts.Agent) == "" {
		opts.Agent = "codex"
	}
	projectName := strings.TrimSpace(opts.ProjectName)
	if projectName == "" {
		projectName = filepath.Base(a.Root)
	}

	m, manifestPath, err := manifest.Load(a.Root)
	switch {
	case err == nil:
		if !opts.Force {
			return nil, fmt.Errorf("ctxpm manifest already exists at %s; rerun with --force to refresh managed files", manifestPath)
		}
	case errors.Is(err, manifest.ErrNotFound):
		m = &manifest.Manifest{
			Version:      1,
			Project:      manifest.Project{Name: projectName},
			Agents:       []string{opts.Agent},
			UpdatePolicy: manifest.DefaultPolicy(),
			Dependencies: []manifest.Resource{},
			Packages:     []manifest.Resource{},
			Entrypoints: map[string]manifest.Entrypoint{
				opts.Agent: {
					File: manifest.EntrypointFile(opts.Agent),
					Mode: "managed",
				},
			},
		}
	default:
		return nil, err
	}

	if opts.Force && m.Project.Name == "" {
		m.Project.Name = projectName
	}
	if len(m.Agents) == 0 {
		m.Agents = []string{opts.Agent}
	}
	if m.Entrypoints == nil {
		m.Entrypoints = map[string]manifest.Entrypoint{}
	}
	if _, ok := m.Entrypoints[opts.Agent]; !ok {
		m.Entrypoints[opts.Agent] = manifest.Entrypoint{File: manifest.EntrypointFile(opts.Agent), Mode: "managed"}
	}

	files := []string{}
	dirs := []string{
		filepath.Join(a.Root, ".ctxpm", "dependencies", "skills"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "rules"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "specs"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "prompts"),
		filepath.Join(a.Root, ".ctxpm", "dependencies", "mcp"),
		filepath.Join(a.Root, ".ctxpm", "packages", "skills"),
		filepath.Join(a.Root, ".ctxpm", "packages", "rules"),
		filepath.Join(a.Root, ".ctxpm", "packages", "specs"),
		filepath.Join(a.Root, ".ctxpm", "packages", "prompts"),
		filepath.Join(a.Root, ".ctxpm", "packages", "mcp"),
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

	entrypointFile := filepath.Join(a.Root, manifest.EntrypointFile(opts.Agent))
	files = append(files, entrypointFile)
	if !opts.DryRun {
		if _, err := os.Stat(entrypointFile); errors.Is(err, os.ErrNotExist) || opts.Force {
			if err := os.WriteFile(entrypointFile, []byte(manifest.ManagedEntrypoint(opts.Agent)), 0o644); err != nil {
				return nil, err
			}
		}
	}

	gitignorePath := filepath.Join(a.Root, ".gitignore")
	files = append(files, gitignorePath)
	if !opts.DryRun {
		if err := ensureGitignoreRules(gitignorePath, []string{".ctxpm/dependencies/", ".ctxpm/state/"}); err != nil {
			return nil, err
		}
		if _, err := manifest.Save(a.Root, m); err != nil {
			return nil, err
		}
	}

	return &InitResult{
		ManifestPath: manifestPath,
		Files:        files,
		DryRun:       opts.DryRun,
	}, nil
}

type AddOptions struct {
	SourceURL  string
	Type       string
	Name       string
	SourcePath string
	TargetPath string
	Ref        string
	Entry      string
	DryRun     bool
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
	m, manifestPath, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	detected, err := source.Detect(ctx, source.DetectionInput{
		RawURL:       opts.SourceURL,
		ResourceType: opts.Type,
		Name:         opts.Name,
		SourcePath:   opts.SourcePath,
		TargetPath:   opts.TargetPath,
		Ref:          opts.Ref,
		Entry:        opts.Entry,
	})
	if err != nil {
		return nil, err
	}
	if m.HasResource(detected.Name) {
		return nil, fmt.Errorf("resource %q already exists in ctxpm.yaml", detected.Name)
	}
	detected.Resource.Compatibility = compatibilityPaths(m.Agents, detected.Resource)

	if !opts.DryRun {
		installed, err := a.installResource(ctx, &detected.Resource, "")
		if err != nil {
			return nil, err
		}
		if installed.Version != "" {
			detected.Resource.Version = installed.Version
		}
		if _, err := manifest.AddDependency(a.Root, detected.Resource); err != nil {
			return nil, err
		}
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
			Status:  localStatus(a.Root, resource),
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
	for _, dep := range m.Dependencies {
		abs := filepath.Join(a.Root, filepath.FromSlash(dep.Path))
		if _, err := os.Stat(abs); err != nil {
			issues = append(issues, fmt.Sprintf("dependency %q path %q is missing", dep.Name, dep.Path))
		}
		for _, compat := range dep.Compatibility {
			compatAbs := filepath.Join(a.Root, filepath.FromSlash(compat))
			if _, err := os.Lstat(compatAbs); err != nil {
				issues = append(issues, fmt.Sprintf("compatibility path %q is missing", compat))
			}
		}
	}
	for _, pkg := range m.Packages {
		abs := filepath.Join(a.Root, filepath.FromSlash(pkg.Path))
		if _, err := os.Stat(abs); err != nil {
			issues = append(issues, fmt.Sprintf("package %q path %q is missing", pkg.Name, pkg.Path))
		}
		for _, compat := range pkg.Compatibility {
			compatAbs := filepath.Join(a.Root, filepath.FromSlash(compat))
			if _, err := os.Lstat(compatAbs); err != nil {
				issues = append(issues, fmt.Sprintf("compatibility path %q is missing", compat))
			}
		}
	}
	return &ValidateResult{OK: len(issues) == 0, Issues: issues}, nil
}

type InstallOptions struct {
	Type   string
	Only   string
	DryRun bool
}

type InstallAction struct {
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

type InstallResult struct {
	Status  string          `json:"status"`
	Actions []InstallAction `json:"actions"`
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
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Install(ctx context.Context, opts InstallOptions) (*InstallResult, error) {
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	actions := []InstallAction{}
	versionUpdates := map[string]string{}
	for i := range m.Dependencies {
		dep := &m.Dependencies[i]
		if opts.Type != "" && dep.Type != opts.Type {
			continue
		}
		if opts.Only != "" && dep.Name != opts.Only {
			continue
		}
		if opts.DryRun {
			actions = append(actions, InstallAction{Kind: "dependency", Name: dep.Name, Status: "would_install", Version: dep.Version})
			continue
		}
		installed, err := a.installResource(ctx, dep, dep.Version)
		if err != nil {
			return nil, err
		}
		previousVersion := dep.Version
		dep.Version = installed.Version
		if previousVersion != dep.Version {
			versionUpdates[dep.Name] = dep.Version
		}
		actions = append(actions, InstallAction{Kind: "dependency", Name: dep.Name, Status: installed.Status, Version: dep.Version})
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
		if err := ensureResourcePresence(a.Root, *pkg, "package"); err != nil {
			return nil, err
		}
		if err := ensureCompatibility(a.Root, *pkg); err != nil {
			return nil, err
		}
		actions = append(actions, InstallAction{Kind: "package", Name: pkg.Name, Status: "linked"})
	}
	if !opts.DryRun && len(versionUpdates) > 0 {
		if _, err := manifest.UpdateResourceVersions(a.Root, versionUpdates); err != nil {
			return nil, err
		}
	}
	return &InstallResult{Status: ternary(opts.DryRun, "dry_run", "applied"), Actions: actions}, nil
}

type CheckUpdatesOptions struct {
	Force bool
}

type DependencyUpdate struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Path           string   `json:"path"`
	SourceType     string   `json:"source_type"`
	CurrentVersion string   `json:"current_version,omitempty"`
	LatestVersion  string   `json:"latest_version,omitempty"`
	Status         string   `json:"status"`
	Reason         string   `json:"reason,omitempty"`
	Compatibility  []string `json:"compatibility,omitempty"`
}

type CheckUpdatesResult struct {
	Status          string                `json:"status"`
	CheckedAt       string                `json:"checked_at,omitempty"`
	LastFullCheckAt string                `json:"last_full_check_at,omitempty"`
	NextCheckAt     string                `json:"next_check_at,omitempty"`
	Policy          manifest.UpdatePolicy `json:"policy"`
	Dependencies    []DependencyUpdate    `json:"dependencies"`
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
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) CheckUpdates(ctx context.Context, opts CheckUpdatesOptions) (*CheckUpdatesResult, error) {
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
	if !boolValue(policy.Enabled, true) {
		return &CheckUpdatesResult{
			Status:       "disabled",
			CheckedAt:    now.Format(time.RFC3339),
			Policy:       policy,
			Dependencies: []DependencyUpdate{},
		}, nil
	}

	if !opts.Force {
		if cached, ok := maybeReuseCheckState(statePath, now, policy.Interval); ok {
			cached.Policy = policy
			return cached, nil
		}
	}

	results := []DependencyUpdate{}
	for _, dep := range m.Dependencies {
		if dep.Name == "ctxpm" && !boolValue(policy.IncludeSelf, true) {
			continue
		}
		version, err := a.resolveLatestVersion(ctx, dep)
		if err != nil {
			results = append(results, DependencyUpdate{
				Name:           dep.Name,
				Type:           dep.Type,
				Path:           dep.Path,
				SourceType:     dep.Source.NormalizedType(),
				CurrentVersion: dep.Version,
				Status:         "unresolved",
				Reason:         err.Error(),
				Compatibility:  dep.Compatibility,
			})
			continue
		}
		status := "up_to_date"
		if dep.Version != version {
			status = "update_available"
		}
		results = append(results, DependencyUpdate{
			Name:           dep.Name,
			Type:           dep.Type,
			Path:           dep.Path,
			SourceType:     dep.Source.NormalizedType(),
			CurrentVersion: dep.Version,
			LatestVersion:  version,
			Status:         status,
			Compatibility:  dep.Compatibility,
		})
	}
	result := &CheckUpdatesResult{
		Status:       "checked",
		CheckedAt:    now.Format(time.RFC3339),
		Policy:       policy,
		Dependencies: results,
	}
	if err := writeCheckState(statePath, result); err != nil {
		return nil, err
	}
	return result, nil
}

type UpdateOptions struct {
	Names  []string
	All    bool
	DryRun bool
}

type UpdateAction struct {
	Name           string `json:"name"`
	Status         string `json:"status"`
	CurrentVersion string `json:"current_version,omitempty"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

type UpdateResult struct {
	Status  string         `json:"status"`
	Applied []UpdateAction `json:"applied"`
	Skipped []UpdateAction `json:"skipped"`
}

func (r UpdateResult) Text() string {
	lines := []string{"Update status: " + r.Status}
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

func (a *App) Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	check, err := a.CheckUpdates(ctx, CheckUpdatesOptions{Force: true})
	if err != nil {
		return nil, err
	}
	byName := map[string]DependencyUpdate{}
	for _, item := range check.Dependencies {
		byName[item.Name] = item
	}

	targets := map[string]bool{}
	if opts.All {
		for _, item := range check.Dependencies {
			if item.Status == "update_available" {
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
	versionUpdates := map[string]string{}
	for i := range m.Dependencies {
		dep := &m.Dependencies[i]
		if !targets[dep.Name] {
			continue
		}
		updateInfo, ok := byName[dep.Name]
		if !ok {
			return nil, fmt.Errorf("dependency %q was not found", dep.Name)
		}
		switch updateInfo.Status {
		case "up_to_date":
			result.Skipped = append(result.Skipped, UpdateAction{Name: dep.Name, Status: "up_to_date"})
		case "update_available":
			if opts.DryRun {
				result.Applied = append(result.Applied, UpdateAction{
					Name:           dep.Name,
					Status:         "would_update",
					CurrentVersion: dep.Version,
					LatestVersion:  updateInfo.LatestVersion,
				})
				continue
			}
			installed, err := a.installResource(ctx, dep, updateInfo.LatestVersion)
			if err != nil {
				return nil, err
			}
			current := dep.Version
			dep.Version = installed.Version
			if current != dep.Version {
				versionUpdates[dep.Name] = dep.Version
			}
			result.Applied = append(result.Applied, UpdateAction{
				Name:           dep.Name,
				Status:         "updated",
				CurrentVersion: current,
				LatestVersion:  dep.Version,
			})
		default:
			result.Skipped = append(result.Skipped, UpdateAction{Name: dep.Name, Status: updateInfo.Status, Reason: updateInfo.Reason})
		}
	}
	if !opts.DryRun && len(versionUpdates) > 0 {
		if _, err := manifest.UpdateResourceVersions(a.Root, versionUpdates); err != nil {
			return nil, err
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
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return nil, err
	}
	for _, dep := range m.Dependencies {
		if dep.Name != opts.Name {
			continue
		}
		if opts.DeleteFiles {
			if err := removePaths(a.Root, dep); err != nil {
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
			if err := removePaths(a.Root, pkg); err != nil {
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

type installOutcome struct {
	Status  string
	Version string
}

func (a *App) installResource(ctx context.Context, resource *manifest.Resource, versionOverride string) (*installOutcome, error) {
	if resource.Source == nil {
		return nil, fmt.Errorf("resource %q has no source", resource.Name)
	}
	switch resource.Source.NormalizedType() {
	case "git":
		version, err := a.installGitResource(ctx, resource, versionOverride)
		if err != nil {
			return nil, err
		}
		return &installOutcome{Status: "installed", Version: version}, nil
	case "url":
		version, err := a.installURLResource(ctx, resource, versionOverride)
		if err != nil {
			return nil, err
		}
		return &installOutcome{Status: "installed", Version: version}, nil
	default:
		return nil, fmt.Errorf("resource %q has unsupported source type %q", resource.Name, resource.Source.Type)
	}
}

func (a *App) resolveLatestVersion(ctx context.Context, resource manifest.Resource) (string, error) {
	switch resource.Source.NormalizedType() {
	case "git":
		return latestGitVersion(ctx, resource.Source.URL, resource.Source.Ref, resource.Source.Path)
	case "url":
		content, err := fetchURL(ctx, resource.Source.URL)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(content)
		return "sha256:" + hex.EncodeToString(sum[:]), nil
	default:
		return "", fmt.Errorf("unsupported source type %q", resource.Source.Type)
	}
}

func (a *App) installGitResource(ctx context.Context, resource *manifest.Resource, versionOverride string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "ctxpm-git-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	if _, err := runGit(ctx, "clone", "--quiet", resource.Source.URL, repoDir); err != nil {
		return "", err
	}

	targetVersion := strings.TrimSpace(versionOverride)
	if targetVersion == "" {
		targetVersion = strings.TrimSpace(resource.Version)
	}
	if targetVersion != "" {
		if _, err := runGit(ctx, "-C", repoDir, "checkout", "--quiet", targetVersion); err != nil {
			return "", err
		}
	} else if strings.TrimSpace(resource.Source.Ref) != "" {
		if _, err := runGit(ctx, "-C", repoDir, "checkout", "--quiet", resource.Source.Ref); err != nil {
			return "", err
		}
	}

	sourcePath := filepath.Join(repoDir, filepath.FromSlash(resource.Source.Path))
	if _, err := os.Stat(sourcePath); err != nil {
		return "", fmt.Errorf("resolved source path %q does not exist for %s", resource.Source.Path, resource.Name)
	}
	destination := filepath.Join(a.Root, filepath.FromSlash(resource.Path))
	if err := replacePath(sourcePath, destination); err != nil {
		return "", err
	}
	if err := ensureCompatibility(a.Root, *resource); err != nil {
		return "", err
	}
	version, err := runGit(ctx, "-C", repoDir, "log", "-1", "--format=%H", "HEAD", "--", resource.Source.Path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(version), nil
}

func (a *App) installURLResource(ctx context.Context, resource *manifest.Resource, versionOverride string) (string, error) {
	content, err := fetchURL(ctx, resource.Source.URL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	version := "sha256:" + hex.EncodeToString(sum[:])
	if expected := strings.TrimSpace(versionOverride); expected != "" && expected != version {
		return "", fmt.Errorf("downloaded content for %s does not match requested version %s", resource.Name, expected)
	}
	if expected := strings.TrimSpace(resource.Version); versionOverride == "" && expected != "" && expected != version {
		return "", fmt.Errorf("downloaded content for %s does not match manifest version %s", resource.Name, expected)
	}

	target := filepath.Join(a.Root, filepath.FromSlash(resource.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return "", err
	}
	if err := ensureCompatibility(a.Root, *resource); err != nil {
		return "", err
	}
	return version, nil
}

func latestGitVersion(ctx context.Context, cloneURL, ref, sourcePath string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "ctxpm-git-check-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	args := []string{"clone", "--quiet"}
	if strings.TrimSpace(ref) != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, cloneURL, repoDir)
	if _, err := runGit(ctx, args...); err != nil {
		return "", err
	}
	version, err := runGit(ctx, "-C", repoDir, "log", "-1", "--format=%H", "HEAD", "--", sourcePath)
	if err != nil {
		return "", err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return "", fmt.Errorf("could not resolve latest git revision for %s", sourcePath)
	}
	return version, nil
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

func ensureCompatibility(root string, resource manifest.Resource) error {
	for _, compat := range resource.Compatibility {
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
	if _, err := os.Stat(targetPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s %q path %q is missing", kind, resource.Name, resource.Path)
		}
		return err
	}
	return nil
}

func replacePath(sourcePath, destination string) error {
	if err := os.RemoveAll(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(sourcePath, destination)
	}
	return copyFile(sourcePath, destination, info.Mode())
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

func compatibilityPaths(agents []string, resource manifest.Resource) []string {
	paths := []string{}
	dir := manifest.TypeDir(resource.Type)
	leaf := pathLeaf(resource.Path)
	for _, agent := range agents {
		switch agent {
		case "codex", "generic":
			paths = append(paths, filepath.ToSlash(filepath.Join(".agents", dir, leaf)))
		case "claude-code":
			paths = append(paths, filepath.ToSlash(filepath.Join(".claude", dir, leaf)))
		case "antigravity":
			paths = append(paths, filepath.ToSlash(filepath.Join(".antigravity", dir, leaf)))
		}
	}
	return dedupe(paths)
}

func pathLeaf(resourcePath string) string {
	return filepath.Base(filepath.FromSlash(resourcePath))
}

func localStatus(root string, resource manifest.Resource) string {
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(resource.Path))); err != nil {
		return "missing"
	}
	for _, compat := range resource.Compatibility {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(compat))); err != nil {
			return "drifted"
		}
	}
	return "installed"
}

func sortResources(resources []manifest.Resource) {
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })
}

func removePaths(root string, resource manifest.Resource) error {
	for _, compat := range resource.Compatibility {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(compat))); err != nil {
			return err
		}
	}
	return os.RemoveAll(filepath.Join(root, filepath.FromSlash(resource.Path)))
}

func ensureGitignoreRules(path string, rules []string) error {
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	builder := strings.Builder{}
	builder.WriteString(existing)
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		builder.WriteString("\n")
	}
	for _, rule := range rules {
		if strings.Contains(existing, rule) {
			continue
		}
		builder.WriteString(rule)
		builder.WriteString("\n")
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

type cachedCheckState struct {
	Status          string             `json:"status"`
	CheckedAt       string             `json:"checked_at"`
	LastFullCheckAt string             `json:"last_full_check_at"`
	NextCheckAt     string             `json:"next_check_at"`
	Dependencies    []DependencyUpdate `json:"dependencies"`
}

func maybeReuseCheckState(statePath string, now time.Time, interval string) (*CheckUpdatesResult, bool) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		return nil, false
	}
	var cached cachedCheckState
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
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
		Status:       result.Status,
		CheckedAt:    result.CheckedAt,
		Dependencies: result.Dependencies,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath, append(data, '\n'), 0o644)
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
