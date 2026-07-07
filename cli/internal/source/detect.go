package source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

var gitSSHPattern = regexp.MustCompile(`^[^@]+@[^:]+:.+`)

type DetectionInput struct {
	RawURL       string
	ResourceType string
	Name         string
	Layout       string
	SourceType   string
	SourcePath   string
	TargetPath   string
	Ref          string
	Entry        string
	Files        []string
}

type DetectionResult struct {
	Name       string
	Canonical  string
	Resource   manifest.Resource
	SourceKind string
}

func Detect(ctx context.Context, input DetectionInput) (*DetectionResult, error) {
	rawURL := strings.TrimSpace(input.RawURL)
	if rawURL == "" {
		return nil, errors.New("source URL is required")
	}

	explicitSourceType := normalizedSourceType(input.SourceType)
	if explicitSourceType == "archive" || (explicitSourceType == "" && looksLikeArchiveURL(rawURL)) {
		return buildArchiveDetection(input, rawURL)
	}
	if gitInfo, ok := parseKnownTreeURL(rawURL); ok {
		if explicitSourceType != "" && explicitSourceType != "git" {
			return nil, fmt.Errorf("source URL %q resolved as git, but --source-type=%s was requested", rawURL, explicitSourceType)
		}
		if input.Ref != "" {
			gitInfo.Ref = input.Ref
		}
		return buildGitDetection(input, gitInfo)
	}

	if explicitSourceType != "url" {
		if cloneURL, ok := detectGitRepo(ctx, rawURL); ok {
			if explicitSourceType != "" && explicitSourceType != "git" {
				return nil, fmt.Errorf("source URL %q resolved as git, but --source-type=%s was requested", rawURL, explicitSourceType)
			}
			return buildGitDetection(input, gitDetection{
				CloneURL: cloneURL,
				Ref:      input.Ref,
				Path:     input.SourcePath,
				RepoName: repoNameFromURL(cloneURL),
			})
		}
	}

	if explicitSourceType != "" && explicitSourceType != "url" {
		return nil, fmt.Errorf("could not resolve %q as source type %s", rawURL, explicitSourceType)
	}
	return buildURLDetection(input, rawURL)
}

type resourceShape struct {
	Layout     string
	SourcePath string
	Entry      string
	Name       string
	Canonical  string
}

func buildGitDetection(input DetectionInput, info gitDetection) (*DetectionResult, error) {
	shape, err := inferGitOrArchiveShape(input, info.Path)
	if err != nil {
		return nil, err
	}
	return &DetectionResult{
		Name:       shape.Name,
		Canonical:  shape.Canonical,
		SourceKind: "git",
		Resource: manifest.Resource{
			Name:   shape.Name,
			Type:   input.ResourceType,
			Layout: shape.Layout,
			Path:   shape.Canonical,
			Entry:  shape.Entry,
			Source: &manifest.Source{
				Type:  "git",
				URL:   info.CloneURL,
				Ref:   strings.TrimSpace(info.Ref),
				Path:  shape.SourcePath,
				Entry: shape.Entry,
			},
		},
	}, nil
}

func buildArchiveDetection(input DetectionInput, rawURL string) (*DetectionResult, error) {
	shape, err := inferGitOrArchiveShape(input, input.SourcePath)
	if err != nil {
		return nil, err
	}
	return &DetectionResult{
		Name:       shape.Name,
		Canonical:  shape.Canonical,
		SourceKind: "archive",
		Resource: manifest.Resource{
			Name:   shape.Name,
			Type:   input.ResourceType,
			Layout: shape.Layout,
			Path:   shape.Canonical,
			Entry:  shape.Entry,
			Source: &manifest.Source{
				Type:  "archive",
				URL:   rawURL,
				Path:  shape.SourcePath,
				Entry: shape.Entry,
			},
		},
	}, nil
}

func buildURLDetection(input DetectionInput, rawURL string) (*DetectionResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid source URL %q: %w", rawURL, err)
	}
	files := cleanFiles(input.Files)
	if len(files) > 0 {
		entry := strings.Trim(strings.TrimSpace(input.Entry), "/")
		if entry == "" && input.ResourceType == "skill" {
			entry = "SKILL.md"
		}
		if entry == "" {
			return nil, errors.New("multi-file URL resources require --entry")
		}
		for _, file := range files {
			if file == entry {
				goto validEntry
			}
		}
		return nil, fmt.Errorf("entry %q must be included in --file values", entry)
	validEntry:
		layout := normalizedLayout(input.Layout)
		if layout == "" {
			layout = manifest.LayoutDir
		}
		if layout != manifest.LayoutDir {
			return nil, errors.New("multi-file URL resources require --layout dir")
		}
		name := strings.TrimSpace(input.Name)
		if name == "" {
			name = directoryNameFromURL(parsed)
			if name == "" {
				name = trimExt(path.Base(entry))
			}
		}
		canonical := input.TargetPath
		if canonical == "" {
			canonical = filepath.ToSlash(filepath.Join(".ctxpm", "dependencies", manifest.TypeDir(input.ResourceType), name))
		}
		return &DetectionResult{
			Name:       name,
			Canonical:  canonical,
			SourceKind: "url",
			Resource: manifest.Resource{
				Name:   name,
				Type:   input.ResourceType,
				Layout: manifest.LayoutDir,
				Path:   canonical,
				Entry:  entry,
				Source: &manifest.Source{
					Type:  "url",
					URL:   ensureTrailingSlash(rawURL),
					Entry: entry,
					Files: files,
				},
			},
		}, nil
	}

	base := path.Base(parsed.Path)
	if base == "." || base == "/" || base == "" {
		return nil, errors.New("direct URL resources need a concrete file URL unless --file is used")
	}
	layout := normalizedLayout(input.Layout)
	if layout == "" {
		layout = manifest.LayoutFile
	}
	if layout != manifest.LayoutFile {
		return nil, errors.New("single-file URL resources require --layout file")
	}
	entry := strings.Trim(strings.TrimSpace(input.Entry), "/")
	if entry == "" {
		entry = base
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = trimExt(base)
	}
	canonical := input.TargetPath
	if canonical == "" {
		canonical = filepath.ToSlash(filepath.Join(".ctxpm", "dependencies", manifest.TypeDir(input.ResourceType), base))
	}
	return &DetectionResult{
		Name:       name,
		Canonical:  canonical,
		SourceKind: "url",
		Resource: manifest.Resource{
			Name:   name,
			Type:   input.ResourceType,
			Layout: manifest.LayoutFile,
			Path:   canonical,
			Entry:  entry,
			Source: &manifest.Source{
				Type:  "url",
				URL:   rawURL,
				Entry: entry,
			},
		},
	}, nil
}

type gitDetection struct {
	CloneURL string
	Ref      string
	Path     string
	RepoName string
}

func inferGitOrArchiveShape(input DetectionInput, rawSourcePath string) (resourceShape, error) {
	sourcePath := strings.Trim(strings.TrimSpace(rawSourcePath), "/")
	if sourcePath == "" {
		return resourceShape{}, errors.New("git and archive sources need --source-path unless the URL already points to a tree/blob subpath")
	}
	layout := normalizedLayout(input.Layout)
	entry := strings.Trim(strings.TrimSpace(input.Entry), "/")
	base := path.Base(sourcePath)
	if strings.EqualFold(base, "SKILL.md") {
		if layout == "" {
			layout = manifest.LayoutDir
		}
		if entry == "" {
			entry = "SKILL.md"
		}
		if layout == manifest.LayoutDir {
			sourcePath = strings.Trim(path.Dir(sourcePath), "/")
		}
	}
	if layout == "" {
		if path.Ext(base) != "" {
			layout = manifest.LayoutFile
		} else {
			layout = manifest.LayoutDir
		}
	}
	switch layout {
	case manifest.LayoutFile:
		if entry == "" {
			entry = base
		}
	case manifest.LayoutDir:
		if entry == "" && input.ResourceType == "skill" {
			entry = "SKILL.md"
		}
		if entry == "" {
			return resourceShape{}, fmt.Errorf("%s directory resources require --entry", input.ResourceType)
		}
		if path.Base(sourcePath) == entry {
			sourcePath = strings.Trim(path.Dir(sourcePath), "/")
		}
	default:
		return resourceShape{}, fmt.Errorf("unsupported layout %q", input.Layout)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		if layout == manifest.LayoutDir {
			name = leafName(sourcePath)
		} else {
			name = trimExt(base)
		}
	}
	canonical := input.TargetPath
	if canonical == "" {
		if layout == manifest.LayoutDir {
			canonical = filepath.ToSlash(filepath.Join(".ctxpm", "dependencies", manifest.TypeDir(input.ResourceType), name))
		} else {
			canonical = filepath.ToSlash(filepath.Join(".ctxpm", "dependencies", manifest.TypeDir(input.ResourceType), base))
		}
	}
	return resourceShape{
		Layout:     layout,
		SourcePath: sourcePath,
		Entry:      entry,
		Name:       name,
		Canonical:  canonical,
	}, nil
}

func normalizedSourceType(value string) string {
	switch strings.TrimSpace(value) {
	case "", "git", "url", "archive":
		return strings.TrimSpace(value)
	case "github":
		return "git"
	default:
		return strings.TrimSpace(value)
	}
}

func normalizedLayout(value string) string {
	switch strings.TrimSpace(value) {
	case manifest.LayoutFile, manifest.LayoutDir:
		return strings.TrimSpace(value)
	default:
		return ""
	}
}

func looksLikeArchiveURL(raw string) bool {
	lower := strings.ToLower(strings.Split(strings.SplitN(raw, "?", 2)[0], "#")[0])
	return strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")
}

func cleanFiles(files []string) []string {
	cleaned := make([]string, 0, len(files))
	for _, file := range files {
		file = strings.Trim(strings.TrimSpace(file), "/")
		if file == "" {
			continue
		}
		cleaned = append(cleaned, file)
	}
	return cleaned
}

func ensureTrailingSlash(raw string) string {
	if strings.HasSuffix(raw, "/") {
		return raw
	}
	return raw + "/"
}

func directoryNameFromURL(parsed *url.URL) string {
	trimmed := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	if trimmed == "" {
		return ""
	}
	return trimExt(path.Base(trimmed))
}

func detectGitRepo(ctx context.Context, rawURL string) (string, bool) {
	candidates := []string{rawURL}
	if strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://") {
		if !strings.HasSuffix(rawURL, ".git") {
			candidates = append(candidates, rawURL+".git")
		}
	}
	if gitSSHPattern.MatchString(rawURL) || strings.HasPrefix(rawURL, "ssh://") || strings.HasSuffix(rawURL, ".git") {
		candidates = append([]string{rawURL}, candidates...)
	}

	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		cmd := exec.CommandContext(checkCtx, "git", "ls-remote", candidate, "HEAD")
		if err := cmd.Run(); err == nil {
			cancel()
			return candidate, true
		}
		cancel()
	}
	return "", false
}

func parseKnownTreeURL(raw string) (gitDetection, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return gitDetection{}, false
	}
	segments := splitPath(parsed.Path)
	if len(segments) < 4 {
		return gitDetection{}, false
	}

	host := strings.ToLower(parsed.Host)
	switch {
	case strings.Contains(host, "github.com"):
		if len(segments) >= 5 && (segments[2] == "tree" || segments[2] == "blob") {
			return gitDetection{
				CloneURL: fmt.Sprintf("%s://%s/%s/%s.git", parsed.Scheme, parsed.Host, segments[0], segments[1]),
				Ref:      segments[3],
				Path:     strings.Join(segments[4:], "/"),
				RepoName: segments[1],
			}, true
		}
	case len(segments) >= 6 && segments[2] == "-" && (segments[3] == "tree" || segments[3] == "blob"):
		return gitDetection{
			CloneURL: fmt.Sprintf("%s://%s/%s/%s.git", parsed.Scheme, parsed.Host, segments[0], segments[1]),
			Ref:      segments[4],
			Path:     strings.Join(segments[5:], "/"),
			RepoName: segments[1],
		}, true
	}
	return gitDetection{}, false
}

func splitPath(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func repoNameFromURL(raw string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(raw), "/")
	if gitSSHPattern.MatchString(trimmed) {
		parts := strings.Split(trimmed, ":")
		return trimExt(path.Base(parts[len(parts)-1]))
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return trimExt(path.Base(trimmed))
	}
	return trimExt(path.Base(parsed.Path))
}

func leafName(p string) string {
	return trimExt(path.Base(strings.TrimSuffix(p, "/")))
}

func trimExt(name string) string {
	ext := path.Ext(name)
	if ext == "" {
		return name
	}
	return strings.TrimSuffix(name, ext)
}
