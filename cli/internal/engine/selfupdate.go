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
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

const (
	ctxpmReleaseGitHubOwner = "gBearBest"
	ctxpmReleaseGitHubRepo  = "Bear.CTXPM"
	CtxpmReleaseSyncEnv     = "CTXPM_INTERNAL_RELEASE_SYNC"
)

var ctxpmLatestReleaseURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", ctxpmReleaseGitHubOwner, ctxpmReleaseGitHubRepo)

var ctxpmReleaseUpdateHook func(a *App, ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error)
var ctxpmReleaseInstallHook func(a *App, ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error)

func runCtxpmReleaseUpdate(a *App, ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error) {
	if ctxpmReleaseUpdateHook != nil {
		return ctxpmReleaseUpdateHook(a, ctx, opts)
	}
	return a.UpdateCtxpmRelease(ctx, opts)
}

func runCtxpmReleaseInstall(a *App, ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error) {
	if ctxpmReleaseInstallHook != nil {
		return ctxpmReleaseInstallHook(a, ctx, opts)
	}
	return a.InstallCtxpmRelease(ctx, opts)
}

type CtxpmReleaseComponentCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type CtxpmReleaseCheck struct {
	Status         string                       `json:"status"`
	CurrentVersion string                       `json:"current_version,omitempty"`
	LatestVersion  string                       `json:"latest_version,omitempty"`
	Reason         string                       `json:"reason,omitempty"`
	Components     []CtxpmReleaseComponentCheck `json:"components"`
}

func (a *App) CheckCtxpmRelease(ctx context.Context, currentVersion string) *CtxpmReleaseCheck {
	result := a.checkCtxpmReleaseLocal(currentVersion)
	latest, err := fetchLatestReleaseTag(ctx)
	applyCtxpmReleaseCheck(result, latest, err)
	return result
}

func (a *App) checkCtxpmReleaseLocal(currentVersion string) *CtxpmReleaseCheck {
	currentVersion = strings.TrimSpace(currentVersion)
	if currentVersion == "" {
		currentVersion = readCurrentVersion()
	}
	currentVersion = canonicalCtxpmReleaseVersion(currentVersion)
	result := &CtxpmReleaseCheck{
		Status:         "up_to_date",
		CurrentVersion: currentVersion,
		Components: []CtxpmReleaseComponentCheck{
			{Name: "cli", Status: a.projectLocalCLIStatus()},
			{Name: "skill", Status: a.bundledSkillStatus(currentVersion)},
			{Name: "entrypoint", Status: a.bundledEntrypointStatus()},
		},
	}
	for _, component := range result.Components {
		if component.Status != "up_to_date" {
			result.Status = "update_available"
		}
	}
	return result
}

func applyCtxpmReleaseCheck(result *CtxpmReleaseCheck, latest string, err error) {
	if err != nil {
		result.Reason = err.Error()
		if result.Status == "up_to_date" {
			result.Status = "unresolved"
		}
		return
	}
	result.LatestVersion = canonicalCtxpmReleaseVersion(latest)
	if result.CurrentVersion != result.LatestVersion {
		result.Status = "update_available"
	}
}

func (a *App) projectLocalCLIStatus() string {
	projectCLI := filepath.Join(a.Root, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
	projectHash, err := hashFileVersion(projectCLI)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "unresolved"
	}
	currentCLI, err := currentExecutablePath()
	if err != nil {
		return "unresolved"
	}
	currentHash, err := hashFileVersion(currentCLI)
	if err != nil {
		return "unresolved"
	}
	if projectHash != currentHash {
		return "update_available"
	}
	return "up_to_date"
}

func (a *App) bundledSkillStatus(currentVersion string) string {
	m, _, err := manifest.Load(a.Root)
	if err != nil {
		return "unresolved"
	}
	dep, ok := findDependency(m.Dependencies, "ctxpm")
	if !ok {
		return "missing"
	}
	if isCtxpmReleaseVersion(currentVersion) && canonicalCtxpmReleaseVersion(dep.Version) != currentVersion {
		return "update_available"
	}
	root := filepath.Join(a.Root, ".ctxpm", "dependencies", "skills", "ctxpm")
	for relative, file := range bundledCtxpmSkillFiles {
		actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if errors.Is(err, os.ErrNotExist) {
			return "missing"
		}
		if err != nil {
			return "unresolved"
		}
		if string(actual) != file.Content {
			return "update_available"
		}
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return "unresolved"
		}
		if info.Mode().Perm() != file.Mode.Perm() {
			return "update_available"
		}
	}
	expectedDirectories := bundledCtxpmSkillDirectories()
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if info.IsDir() {
			if !expectedDirectories[relative] {
				return fmt.Errorf("unexpected bundled skill directory %s", relative)
			}
			return nil
		}
		if relative == "cli/ctxpm" || relative == "cli/ctxpm.exe" {
			return nil
		}
		if _, ok := bundledCtxpmSkillFiles[relative]; !ok || !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected bundled skill file %s", relative)
		}
		return nil
	}); err != nil {
		return "update_available"
	}
	return "up_to_date"
}

func (a *App) bundledEntrypointStatus() string {
	path := filepath.Join(a.Root, manifest.CanonicalEntrypointSourceFile())
	state, err := readManagedEntrypointState(path)
	if errors.Is(err, os.ErrNotExist) {
		return "missing"
	}
	if err != nil {
		return "unresolved"
	}
	if !state.HasManagedBlock {
		return "missing"
	}
	if state.Damaged {
		return "damaged"
	}
	if strings.TrimRight(state.Block, "\n") != strings.TrimRight(manifest.ManagedEntrypoint(), "\n") {
		return "update_available"
	}
	return "up_to_date"
}

type CtxpmReleaseUpdateOptions struct {
	Version        string
	CurrentVersion string
	DryRun         bool
	Force          bool
}

type CtxpmReleaseUpdateResult struct {
	Status         string `json:"status"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Downloaded     bool   `json:"downloaded"`
	Installed      bool   `json:"installed"`
	BundleSynced   bool   `json:"bundle_synced"`
	Message        string `json:"message,omitempty"`
}

func (r CtxpmReleaseUpdateResult) Text() string {
	lines := []string{fmt.Sprintf("ctxpm release update status: %s", r.Status)}
	if r.CurrentVersion != "" {
		lines = append(lines, fmt.Sprintf("Current version: %s", r.CurrentVersion))
	}
	if r.LatestVersion != "" {
		lines = append(lines, fmt.Sprintf("Latest version: %s", r.LatestVersion))
	}
	if r.BundleSynced {
		lines = append(lines, "Bundled skill and entrypoint synchronized: yes")
	}
	if r.Message != "" {
		lines = append(lines, r.Message)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) InstallCtxpmRelease(ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error) {
	currentVersion := canonicalCtxpmReleaseVersion(opts.CurrentVersion)
	if currentVersion == "" {
		currentVersion = canonicalCtxpmReleaseVersion(readCurrentVersion())
	}
	targetVersion := canonicalCtxpmReleaseVersion(opts.Version)
	if !isCtxpmReleaseVersion(targetVersion) {
		return nil, fmt.Errorf("invalid ctxpm release version %q", targetVersion)
	}

	if currentVersion == targetVersion {
		executable, err := currentExecutablePath()
		if err != nil {
			return nil, fmt.Errorf("cannot determine executable path: %w", err)
		}
		if _, err := a.Install(ctx, InstallOptions{Only: "ctxpm", CurrentVersion: currentVersion, BundledCtxpmRelease: true}); err != nil {
			return nil, fmt.Errorf("failed to synchronize bundled resources: %w", err)
		}
		if err := verifyInstalledCtxpmRelease(executable, a.Root, targetVersion); err != nil {
			return nil, fmt.Errorf("failed to verify bundled resources: %w", err)
		}
		return &CtxpmReleaseUpdateResult{
			Status:         "up_to_date",
			CurrentVersion: currentVersion,
			LatestVersion:  targetVersion,
			BundleSynced:   true,
			Message:        "Installed the locked ctxpm release from the active CLI bundle",
		}, nil
	}

	tmpBinary, err := downloadReleaseBinary(ctx, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to download locked ctxpm release: %w", err)
	}
	defer os.Remove(tmpBinary)
	if err := verifyDownloadedBinary(tmpBinary); err != nil {
		return nil, fmt.Errorf("downloaded binary verification failed: %w", err)
	}
	if err := synchronizeBundleWithUpdatedCLI(ctx, tmpBinary, a.Root, targetVersion); err != nil {
		return nil, fmt.Errorf("failed to install locked ctxpm release: %w", err)
	}
	return &CtxpmReleaseUpdateResult{
		Status:         "installed",
		CurrentVersion: currentVersion,
		LatestVersion:  targetVersion,
		Downloaded:     true,
		Installed:      true,
		BundleSynced:   true,
		Message:        fmt.Sprintf("Installed locked ctxpm release %s for this project", targetVersion),
	}, nil
}

func (a *App) UpdateCtxpmRelease(ctx context.Context, opts CtxpmReleaseUpdateOptions) (*CtxpmReleaseUpdateResult, error) {
	executable, err := currentExecutablePath()
	if err != nil {
		return nil, fmt.Errorf("cannot determine executable path: %w", err)
	}

	currentVersion := canonicalCtxpmReleaseVersion(opts.CurrentVersion)
	if currentVersion == "" {
		currentVersion = canonicalCtxpmReleaseVersion(readCurrentVersion())
	}

	targetVersion := canonicalCtxpmReleaseVersion(opts.Version)
	if targetVersion == "" || targetVersion == "latest" {
		latest, err := fetchLatestReleaseTag(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch latest version: %w", err)
		}
		targetVersion = canonicalCtxpmReleaseVersion(latest)
	}
	if !isCtxpmReleaseVersion(targetVersion) {
		return nil, fmt.Errorf("invalid ctxpm release version %q", targetVersion)
	}

	if !opts.Force && currentVersion != "" && currentVersion != "unknown" && currentVersion != "devel" {
		if currentVersion == targetVersion {
			if opts.DryRun {
				return &CtxpmReleaseUpdateResult{
					Status:         "up_to_date",
					CurrentVersion: currentVersion,
					LatestVersion:  targetVersion,
					Message:        "CLI is current; install would verify the bundled skill and entrypoint",
				}, nil
			}
			if _, err := a.Install(ctx, InstallOptions{Only: "ctxpm", CurrentVersion: currentVersion, BundledCtxpmRelease: true}); err != nil {
				return nil, fmt.Errorf("failed to synchronize bundled resources: %w", err)
			}
			if err := verifyInstalledCtxpmRelease(executable, a.Root, targetVersion); err != nil {
				return nil, fmt.Errorf("failed to verify bundled resources: %w", err)
			}
			return &CtxpmReleaseUpdateResult{
				Status:         "up_to_date",
				CurrentVersion: currentVersion,
				LatestVersion:  targetVersion,
				BundleSynced:   true,
				Message:        "CLI is current; bundled skill and entrypoint were synchronized",
			}, nil
		}
	}

	if opts.DryRun {
		return &CtxpmReleaseUpdateResult{
			Status:         "dry_run",
			CurrentVersion: currentVersion,
			LatestVersion:  targetVersion,
			Message:        fmt.Sprintf("Would update from %s to %s", currentVersion, targetVersion),
		}, nil
	}

	tmpBinary, err := downloadReleaseBinary(ctx, targetVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to download release: %w", err)
	}
	defer os.Remove(tmpBinary)

	if err := verifyDownloadedBinary(tmpBinary); err != nil {
		return nil, fmt.Errorf("downloaded binary verification failed: %w", err)
	}

	backupPath, err := replaceCurrentBinary(executable, tmpBinary)
	if err != nil {
		return nil, fmt.Errorf("failed to replace binary: %w", err)
	}
	if err := synchronizeBundleWithUpdatedCLI(ctx, executable, a.Root, targetVersion); err != nil {
		rollbackErr := restorePreviousBinary(executable, backupPath)
		if rollbackErr != nil {
			_ = os.Remove(backupPath)
			return nil, fmt.Errorf("CLI was updated but bundled resources could not be synchronized: %w; rollback failed: %v", err, rollbackErr)
		}
		bundleRollbackErr := synchronizeBundleWithUpdatedCLI(ctx, executable, a.Root, currentVersion)
		_ = os.Remove(backupPath)
		if bundleRollbackErr != nil {
			return nil, fmt.Errorf("ctxpm release update failed while synchronizing bundled resources: %w; the previous CLI was restored, but its bundled resources could not be restored: %v", err, bundleRollbackErr)
		}
		return nil, fmt.Errorf("ctxpm release update failed while synchronizing bundled resources; the previous CLI was restored: %w", err)
	}
	_ = os.Remove(backupPath)

	return &CtxpmReleaseUpdateResult{
		Status:         "updated",
		CurrentVersion: currentVersion,
		LatestVersion:  targetVersion,
		Downloaded:     true,
		Installed:      true,
		BundleSynced:   true,
		Message:        fmt.Sprintf("Successfully updated the CLI, bundled skill, and entrypoint from %s to %s", currentVersion, targetVersion),
	}, nil
}

func synchronizeBundleWithUpdatedCLI(ctx context.Context, executable, projectRoot, expectedVersion string) error {
	cmd := exec.CommandContext(ctx, executable, "install", "--only", "ctxpm", "--json")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(), CtxpmReleaseSyncEnv+"=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("updated CLI install failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if isCtxpmReleaseVersion(expectedVersion) {
		if _, err := manifest.UpdateResourceVersions(projectRoot, map[string]string{"ctxpm": canonicalCtxpmReleaseVersion(expectedVersion)}); err != nil {
			return fmt.Errorf("could not record the installed ctxpm release: %w", err)
		}
	}
	return verifyInstalledCtxpmRelease(executable, projectRoot, expectedVersion)
}

func verifyInstalledCtxpmRelease(executable, projectRoot, expectedVersion string) error {
	if err := verifyProjectLocalCLI(executable, projectRoot); err != nil {
		return err
	}
	if !isCtxpmReleaseVersion(expectedVersion) {
		return nil
	}
	m, _, err := manifest.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("could not verify the ctxpm manifest version: %w", err)
	}
	dep, ok := findDependency(m.Dependencies, "ctxpm")
	if !ok {
		return errors.New("ctxpm dependency is missing after release installation")
	}
	if canonicalCtxpmReleaseVersion(dep.Version) != canonicalCtxpmReleaseVersion(expectedVersion) {
		return fmt.Errorf("ctxpm manifest version is %q, expected %q", dep.Version, canonicalCtxpmReleaseVersion(expectedVersion))
	}
	return nil
}

func verifyProjectLocalCLI(executable, projectRoot string) error {
	expected, err := hashFileVersion(executable)
	if err != nil {
		return fmt.Errorf("could not hash the active CLI: %w", err)
	}
	projectCLI := filepath.Join(projectRoot, ".ctxpm", "dependencies", "skills", "ctxpm", "cli", "ctxpm")
	actual, err := hashFileVersion(projectCLI)
	if err != nil {
		return fmt.Errorf("could not verify the project-local CLI: %w", err)
	}
	if actual != expected {
		return errors.New("project-local CLI does not match the active CLI")
	}
	return nil
}

// currentExecutablePath resolves the path of the running binary. This keeps
// project-local installs under .ctxpm/dependencies/skills/ctxpm/cli rooted at
// that directory, including when invoked through a compatibility symlink.
func currentExecutablePath() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveExecutablePath(executable)
}

func resolveExecutablePath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	return path, nil
}

func readCurrentVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	ver := strings.TrimSpace(info.Main.Version)
	if ver == "" {
		ver = "devel"
	}
	revision := ""
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			revision = s.Value
		}
	}
	if revision == "" {
		return ver
	}
	short := revision
	if len(short) > 12 {
		short = short[:12]
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.modified" && s.Value == "true" {
			return fmt.Sprintf("%s+%s-dirty", ver, short)
		}
	}
	return fmt.Sprintf("%s+%s", ver, short)
}

func fetchLatestReleaseTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ctxpmLatestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("invalid GitHub API response: %w", err)
	}
	if release.TagName == "" {
		return "", errors.New("could not extract tag_name from GitHub API response")
	}
	return release.TagName, nil
}

func downloadReleaseBinary(ctx context.Context, version string) (string, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
	switch goos {
	case "darwin", "linux", "windows":
	default:
		return "", fmt.Errorf("unsupported operating system: %s", goos)
	}

	assetVersion := strings.TrimPrefix(version, "v")
	ext := "tar.gz"
	binaryName := "ctxpm"
	if goos == "windows" {
		ext = "zip"
		binaryName = "ctxpm.exe"
	}

	assetName := fmt.Sprintf("ctxpm_%s_%s_%s.%s", assetVersion, goos, goarch, ext)
	downloadURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s",
		ctxpmReleaseGitHubOwner, ctxpmReleaseGitHubRepo, version, assetName)
	checksumURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/checksums.txt",
		ctxpmReleaseGitHubOwner, ctxpmReleaseGitHubRepo, version)

	checksums, err := fetchURL(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("failed to download checksums: %w", err)
	}

	expectedChecksum := extractChecksumEntry(string(checksums), assetName)
	if expectedChecksum == "" {
		return "", fmt.Errorf("checksum not found for %s", assetName)
	}

	archiveData, err := fetchURL(ctx, downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download release archive: %w", err)
	}

	actualChecksum := hexSHA256(archiveData)
	if actualChecksum != expectedChecksum {
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	tmpDir, err := os.MkdirTemp("", "ctxpm-update-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, assetName)
	if err := os.WriteFile(archivePath, archiveData, 0644); err != nil {
		return "", err
	}

	extractDir := filepath.Join(tmpDir, "extract")
	if err := os.MkdirAll(extractDir, 0755); err != nil {
		return "", err
	}

	if err := extractReleaseArchive(archivePath, extractDir, ext); err != nil {
		return "", fmt.Errorf("failed to extract archive: %w", err)
	}

	binaryPath := filepath.Join(extractDir, binaryName)
	if _, err := os.Stat(binaryPath); err != nil {
		return "", fmt.Errorf("binary not found in archive: %w", err)
	}

	tmpBinary := filepath.Join(os.TempDir(), fmt.Sprintf("ctxpm-new-%d", time.Now().Unix()))
	if err := copyFile(binaryPath, tmpBinary, 0755); err != nil {
		return "", err
	}
	return tmpBinary, nil
}

func extractChecksumEntry(checksums, filename string) string {
	for _, line := range strings.Split(checksums, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 2 && strings.TrimPrefix(parts[1], "*") == filename {
			return parts[0]
		}
	}
	return ""
}

func hexSHA256(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func extractReleaseArchive(archivePath, destDir, ext string) error {
	var cmd *exec.Cmd
	if ext == "zip" {
		cmd = exec.Command("unzip", "-q", archivePath, "-d", destDir)
	} else {
		cmd = exec.Command("tar", "-xzf", archivePath, "-C", destDir)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extraction failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func verifyDownloadedBinary(path string) error {
	verCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(verCtx, path, "--version")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func replaceCurrentBinary(oldPath, newPath string) (string, error) {
	info, err := os.Stat(oldPath)
	if err != nil {
		return "", err
	}
	backupPath := oldPath + ".backup"
	if err := copyFile(oldPath, backupPath, info.Mode()); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}
	if err := copyFile(newPath, oldPath, info.Mode()); err != nil {
		_ = copyFile(backupPath, oldPath, info.Mode())
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("failed to replace binary: %w", err)
	}
	return backupPath, nil
}

func restorePreviousBinary(executable, backupPath string) error {
	info, err := os.Stat(backupPath)
	if err != nil {
		return err
	}
	return copyFile(backupPath, executable, info.Mode())
}

func canonicalCtxpmReleaseVersion(v string) string {
	v = strings.TrimSpace(v)
	if idx := strings.Index(v, "+"); idx != -1 {
		v = v[:idx]
	}
	if v == "" || v == "latest" || v == "unknown" || v == "devel" {
		return v
	}
	if !strings.HasPrefix(v, "v") && validSemanticVersionCore(v) {
		v = "v" + v
	}
	return v
}

func currentCtxpmManifestVersion(reportedVersion string) string {
	version := canonicalCtxpmReleaseVersion(reportedVersion)
	if version == "" {
		version = canonicalCtxpmReleaseVersion(readCurrentVersion())
	}
	if isCtxpmReleaseVersion(version) {
		return version
	}
	if revision := currentBuildRevision(); revision != "" {
		return revision
	}
	return version
}

func currentBuildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}
	return ""
}

func isCtxpmReleaseVersion(v string) bool {
	v = canonicalCtxpmReleaseVersion(v)
	if !strings.HasPrefix(v, "v") {
		return false
	}
	core := strings.TrimPrefix(v, "v")
	return validSemanticVersionCore(core)
}

func validSemanticVersionCore(core string) bool {
	if index := strings.Index(core, "-"); index >= 0 {
		core = core[:index]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}
