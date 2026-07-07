package engine

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

type resolveOptions struct {
	VersionOverride    string
	UseRecordedVersion bool
}

type resolvedResource struct {
	LocalPath string
	Version   string
	cleanup   func()
}

func (r *resolvedResource) Close() {
	if r.cleanup != nil {
		r.cleanup()
	}
}

func (a *App) resolveResource(ctx context.Context, resource manifest.Resource, opts resolveOptions) (*resolvedResource, error) {
	if resource.Source == nil {
		return nil, fmt.Errorf("resource %q has no source", resource.Name)
	}
	switch resource.Source.NormalizedType() {
	case "git":
		return a.resolveGitResource(ctx, resource, opts)
	case "url":
		return a.resolveURLResource(ctx, resource, opts)
	case "archive":
		return a.resolveArchiveResource(ctx, resource, opts)
	default:
		return nil, fmt.Errorf("resource %q has unsupported source type %q", resource.Name, resource.Source.Type)
	}
}

func (a *App) resolveGitResource(ctx context.Context, resource manifest.Resource, opts resolveOptions) (*resolvedResource, error) {
	tmpDir, err := os.MkdirTemp("", "ctxpm-git-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	repoDir := filepath.Join(tmpDir, "repo")
	if _, err := runGit(ctx, "clone", "--quiet", resource.Source.URL, repoDir); err != nil {
		cleanup()
		return nil, err
	}

	targetVersion := strings.TrimSpace(opts.VersionOverride)
	if targetVersion == "" && opts.UseRecordedVersion {
		targetVersion = strings.TrimSpace(resource.Version)
	}
	if targetVersion != "" {
		if _, err := runGit(ctx, "-C", repoDir, "checkout", "--quiet", targetVersion); err != nil {
			cleanup()
			return nil, err
		}
	} else if strings.TrimSpace(resource.Source.Ref) != "" {
		if _, err := runGit(ctx, "-C", repoDir, "checkout", "--quiet", resource.Source.Ref); err != nil {
			cleanup()
			return nil, err
		}
	}

	localPath := filepath.Join(repoDir, filepath.FromSlash(resource.Source.Path))
	if err := validateResolvedResource(localPath, resource); err != nil {
		cleanup()
		return nil, err
	}
	version, err := runGit(ctx, "-C", repoDir, "log", "-1", "--format=%H", "HEAD", "--", resource.Source.Path)
	if err != nil {
		cleanup()
		return nil, err
	}
	version = strings.TrimSpace(version)
	if version == "" {
		cleanup()
		return nil, fmt.Errorf("could not resolve latest git revision for %s", resource.Source.Path)
	}
	return &resolvedResource{
		LocalPath: localPath,
		Version:   version,
		cleanup:   cleanup,
	}, nil
}

func (a *App) resolveURLResource(ctx context.Context, resource manifest.Resource, opts resolveOptions) (*resolvedResource, error) {
	files := resource.Source.Files
	if len(files) == 0 {
		content, err := fetchURL(ctx, resource.Source.URL)
		if err != nil {
			return nil, err
		}
		tmpDir, err := os.MkdirTemp("", "ctxpm-url-*")
		if err != nil {
			return nil, err
		}
		cleanup := func() { _ = os.RemoveAll(tmpDir) }
		entry := resource.Source.Entry
		if entry == "" {
			entry = resource.EffectiveEntry()
		}
		localPath := filepath.Join(tmpDir, filepath.FromSlash(entry))
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			cleanup()
			return nil, err
		}
		if err := os.WriteFile(localPath, content, 0o644); err != nil {
			cleanup()
			return nil, err
		}
		if err := validateResolvedResource(localPath, resource); err != nil {
			cleanup()
			return nil, err
		}
		version, err := hashFileVersion(localPath)
		if err != nil {
			cleanup()
			return nil, err
		}
		if err := ensureVersionMatches(version, resource, opts); err != nil {
			cleanup()
			return nil, err
		}
		return &resolvedResource{
			LocalPath: localPath,
			Version:   version,
			cleanup:   cleanup,
		}, nil
	}

	tmpDir, err := os.MkdirTemp("", "ctxpm-url-tree-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	baseURL, err := url.Parse(resource.Source.URL)
	if err != nil {
		cleanup()
		return nil, err
	}
	for _, file := range files {
		ref, err := url.Parse(file)
		if err != nil {
			cleanup()
			return nil, err
		}
		content, err := fetchURL(ctx, baseURL.ResolveReference(ref).String())
		if err != nil {
			cleanup()
			return nil, err
		}
		target := filepath.Join(tmpDir, filepath.FromSlash(file))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			cleanup()
			return nil, err
		}
		if err := os.WriteFile(target, content, 0o644); err != nil {
			cleanup()
			return nil, err
		}
	}
	if err := validateResolvedResource(tmpDir, resource); err != nil {
		cleanup()
		return nil, err
	}
	version, err := hashTreeVersion(tmpDir)
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := ensureVersionMatches(version, resource, opts); err != nil {
		cleanup()
		return nil, err
	}
	return &resolvedResource{
		LocalPath: tmpDir,
		Version:   version,
		cleanup:   cleanup,
	}, nil
}

func (a *App) resolveArchiveResource(ctx context.Context, resource manifest.Resource, opts resolveOptions) (*resolvedResource, error) {
	content, err := fetchURL(ctx, resource.Source.URL)
	if err != nil {
		return nil, err
	}
	tmpDir, err := os.MkdirTemp("", "ctxpm-archive-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }
	extractedRoot, err := extractArchive(content, resource.Source.URL, tmpDir)
	if err != nil {
		cleanup()
		return nil, err
	}
	baseRoot, err := stripSingleTopLevelDir(extractedRoot)
	if err != nil {
		cleanup()
		return nil, err
	}
	localPath := filepath.Join(baseRoot, filepath.FromSlash(resource.Source.Path))
	if err := validateResolvedResource(localPath, resource); err != nil {
		cleanup()
		return nil, err
	}
	version, err := computeNonGitVersion(localPath, resource.EffectiveLayout())
	if err != nil {
		cleanup()
		return nil, err
	}
	if err := ensureVersionMatches(version, resource, opts); err != nil {
		cleanup()
		return nil, err
	}
	return &resolvedResource{
		LocalPath: localPath,
		Version:   version,
		cleanup:   cleanup,
	}, nil
}

func validateResolvedResource(localPath string, resource manifest.Resource) error {
	info, err := os.Stat(localPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("resolved resource %q does not exist", localPath)
		}
		return err
	}
	switch resource.EffectiveLayout() {
	case manifest.LayoutDir:
		if !info.IsDir() {
			return fmt.Errorf("resource %q expected directory root at %q", resource.Name, resource.Path)
		}
		entryPath := filepath.Join(localPath, filepath.FromSlash(resource.EffectiveEntry()))
		entryInfo, err := os.Stat(entryPath)
		if err != nil {
			return fmt.Errorf("resource %q is missing entry %q", resource.Name, resource.EffectiveEntry())
		}
		if entryInfo.IsDir() {
			return fmt.Errorf("resource %q entry %q must be a file", resource.Name, resource.EffectiveEntry())
		}
	case manifest.LayoutFile:
		if info.IsDir() {
			return fmt.Errorf("resource %q expected file root at %q", resource.Name, resource.Path)
		}
	default:
		return fmt.Errorf("resource %q has unsupported layout %q", resource.Name, resource.Layout)
	}
	return nil
}

func ensureVersionMatches(version string, resource manifest.Resource, opts resolveOptions) error {
	expected := strings.TrimSpace(opts.VersionOverride)
	if expected == "" && opts.UseRecordedVersion {
		expected = strings.TrimSpace(resource.Version)
	}
	if expected != "" && expected != version {
		return fmt.Errorf("resolved content for %s does not match expected version %s", resource.Name, expected)
	}
	return nil
}

func computeNonGitVersion(localPath, layout string) (string, error) {
	switch layout {
	case manifest.LayoutFile:
		return hashFileVersion(localPath)
	case manifest.LayoutDir:
		return hashTreeVersion(localPath)
	default:
		return "", fmt.Errorf("unsupported layout %q", layout)
	}
}

func hashFileVersion(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return manifest.VersionPrefixSHA256 + hex.EncodeToString(sum[:]), nil
}

func hashTreeVersion(root string) (string, error) {
	entries := []string{}
	if err := filepath.Walk(root, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, current)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return "", err
	}
	sort.Strings(entries)
	manifestBuffer := bytes.Buffer{}
	for _, relative := range entries {
		fullPath := filepath.Join(root, filepath.FromSlash(relative))
		fileHash, err := hashFileVersion(fullPath)
		if err != nil {
			return "", err
		}
		manifestBuffer.WriteString(relative)
		manifestBuffer.WriteByte(0)
		manifestBuffer.WriteString(strings.TrimPrefix(fileHash, manifest.VersionPrefixSHA256))
		manifestBuffer.WriteByte('\n')
	}
	sum := sha256.Sum256(manifestBuffer.Bytes())
	return manifest.VersionPrefixSHA256Tree + hex.EncodeToString(sum[:]), nil
}

func extractArchive(content []byte, rawURL, destination string) (string, error) {
	lower := strings.ToLower(strings.Split(strings.SplitN(rawURL, "?", 2)[0], "#")[0])
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return destination, extractZipArchive(content, destination)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return destination, extractTarGzArchive(content, destination)
	default:
		return "", fmt.Errorf("unsupported archive type for %s", rawURL)
	}
}

func extractZipArchive(content []byte, destination string) error {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return err
	}
	for _, file := range reader.File {
		target, err := secureArchivePath(destination, file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, file.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, file.Mode())
		if err != nil {
			input.Close()
			return err
		}
		if _, err := io.Copy(output, input); err != nil {
			input.Close()
			output.Close()
			return err
		}
		input.Close()
		output.Close()
	}
	return nil
}

func extractTarGzArchive(content []byte, destination string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := secureArchivePath(destination, header.Name)
		if err != nil {
			return err
		}
		info := header.FileInfo()
		if info.IsDir() {
			if err := os.MkdirAll(target, info.Mode()); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		output, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(output, tarReader); err != nil {
			output.Close()
			return err
		}
		output.Close()
	}
}

func secureArchivePath(root, relative string) (string, error) {
	cleaned := filepath.Clean(relative)
	if cleaned == "." || cleaned == "" {
		return root, nil
	}
	if strings.HasPrefix(cleaned, "..") || filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("archive entry %q escapes extraction root", relative)
	}
	target := filepath.Join(root, cleaned)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) && target != root {
		return "", fmt.Errorf("archive entry %q escapes extraction root", relative)
	}
	return target, nil
}

func stripSingleTopLevelDir(root string) (string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	if len(entries) == 1 && entries[0].IsDir() {
		return filepath.Join(root, entries[0].Name()), nil
	}
	return root, nil
}
