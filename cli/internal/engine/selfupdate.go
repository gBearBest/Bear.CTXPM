package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

const (
	selfUpdateGitHubOwner = "gBearBest"
	selfUpdateGitHubRepo  = "Bear.CTXPM"
)

type SelfUpdateOptions struct {
	Version string
	DryRun  bool
	Force   bool
}

type SelfUpdateResult struct {
	Status         string `json:"status"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Downloaded     bool   `json:"downloaded"`
	Installed      bool   `json:"installed"`
	Message        string `json:"message,omitempty"`
}

func (r SelfUpdateResult) Text() string {
	lines := []string{fmt.Sprintf("Self-update status: %s", r.Status)}
	if r.CurrentVersion != "" {
		lines = append(lines, fmt.Sprintf("Current version: %s", r.CurrentVersion))
	}
	if r.LatestVersion != "" {
		lines = append(lines, fmt.Sprintf("Latest version: %s", r.LatestVersion))
	}
	if r.Message != "" {
		lines = append(lines, r.Message)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) SelfUpdate(ctx context.Context, opts SelfUpdateOptions) (*SelfUpdateResult, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("cannot determine executable path: %w", err)
	}

	currentVersion := readCurrentVersion()

	targetVersion := opts.Version
	if targetVersion == "" || targetVersion == "latest" {
		latest, err := fetchLatestReleaseTag(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch latest version: %w", err)
		}
		targetVersion = latest
	}

	if !opts.Force && currentVersion != "" && currentVersion != "unknown" && currentVersion != "devel" {
		if normalizeVersionTag(currentVersion) == normalizeVersionTag(targetVersion) {
			return &SelfUpdateResult{
				Status:         "up_to_date",
				CurrentVersion: currentVersion,
				LatestVersion:  targetVersion,
				Message:        "Already running the latest version",
			}, nil
		}
	}

	if opts.DryRun {
		return &SelfUpdateResult{
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

	if err := replaceCurrentBinary(executable, tmpBinary); err != nil {
		return nil, fmt.Errorf("failed to replace binary: %w", err)
	}

	return &SelfUpdateResult{
		Status:         "updated",
		CurrentVersion: currentVersion,
		LatestVersion:  targetVersion,
		Downloaded:     true,
		Installed:      true,
		Message:        fmt.Sprintf("Successfully updated from %s to %s", currentVersion, targetVersion),
	}, nil
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
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", selfUpdateGitHubOwner, selfUpdateGitHubRepo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	tagName := extractJSONStringField(string(body), "tag_name")
	if tagName == "" {
		return "", errors.New("could not extract tag_name from GitHub API response")
	}
	return tagName, nil
}

func extractJSONStringField(jsonBody, field string) string {
	prefix := `"` + field + `":"`
	start := strings.Index(jsonBody, prefix)
	if start == -1 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(jsonBody[start:], `"`)
	if end == -1 {
		return ""
	}
	return jsonBody[start : start+end]
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
		selfUpdateGitHubOwner, selfUpdateGitHubRepo, version, assetName)
	checksumURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/checksums.txt",
		selfUpdateGitHubOwner, selfUpdateGitHubRepo, version)

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
		if len(parts) >= 2 && parts[1] == filename {
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

func replaceCurrentBinary(oldPath, newPath string) error {
	info, err := os.Stat(oldPath)
	if err != nil {
		return err
	}
	backupPath := oldPath + ".backup"
	if err := copyFile(oldPath, backupPath, info.Mode()); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	if err := copyFile(newPath, oldPath, info.Mode()); err != nil {
		_ = copyFile(backupPath, oldPath, info.Mode())
		return fmt.Errorf("failed to replace binary: %w", err)
	}
	os.Remove(backupPath)
	return nil
}

func normalizeVersionTag(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexAny(v, "+-"); idx != -1 {
		v = v[:idx]
	}
	return v
}
