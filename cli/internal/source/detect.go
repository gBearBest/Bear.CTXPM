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
	SourcePath   string
	TargetPath   string
	Ref          string
	Entry        string
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

	if gitInfo, ok := parseKnownTreeURL(rawURL); ok {
		if input.Ref != "" {
			gitInfo.Ref = input.Ref
		}
		return buildGitDetection(input, gitInfo)
	}

	if cloneURL, ok := detectGitRepo(ctx, rawURL); ok {
		return buildGitDetection(input, gitDetection{
			CloneURL: cloneURL,
			Ref:      input.Ref,
			Path:     input.SourcePath,
			RepoName: repoNameFromURL(cloneURL),
		})
	}

	return buildURLDetection(input, rawURL)
}

type gitDetection struct {
	CloneURL string
	Ref      string
	Path     string
	RepoName string
}

func buildGitDetection(input DetectionInput, info gitDetection) (*DetectionResult, error) {
	sourcePath := strings.Trim(info.Path, "/")
	if sourcePath == "" {
		return nil, errors.New("git repository URLs need --source-path unless the URL already points to a tree/blob subpath")
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = leafName(sourcePath)
	}
	canonical := input.TargetPath
	if canonical == "" {
		canonical = filepath.ToSlash(filepath.Join(".ctxpm", "dependencies", manifest.TypeDir(input.ResourceType), name))
	}
	return &DetectionResult{
		Name:       name,
		Canonical:  canonical,
		SourceKind: "git",
		Resource: manifest.Resource{
			Name: name,
			Type: input.ResourceType,
			Path: canonical,
			Source: &manifest.Source{
				Type: "git",
				URL:  info.CloneURL,
				Ref:  strings.TrimSpace(info.Ref),
				Path: sourcePath,
			},
		},
	}, nil
}

func buildURLDetection(input DetectionInput, rawURL string) (*DetectionResult, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid source URL %q: %w", rawURL, err)
	}
	base := path.Base(parsed.Path)
	if base == "." || base == "/" || base == "" {
		return nil, errors.New("direct URL resources need a concrete file URL")
	}
	entry := input.Entry
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
			Name: name,
			Type: input.ResourceType,
			Path: canonical,
			Source: &manifest.Source{
				Type:  "url",
				URL:   rawURL,
				Entry: entry,
			},
		},
	}, nil
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
