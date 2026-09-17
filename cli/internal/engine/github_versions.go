package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

const githubAPIVersion = "2022-11-28"

var githubAPIBaseURL = "https://api.github.com"

type githubCommit struct {
	SHA string `json:"sha"`
}

// resolveGitHubPathVersion returns the newest commit that changed source.path.
// The bool reports whether resource is hosted on github.com, even if resolving it failed.
func resolveGitHubPathVersion(ctx context.Context, resource manifest.Resource) (string, bool, error) {
	if resource.Source == nil || resource.Source.NormalizedType() != "git" {
		return "", false, nil
	}
	owner, repo, ok := githubRepository(resource.Source.URL)
	if !ok {
		return "", false, nil
	}

	resourcePath := strings.Trim(strings.TrimSpace(resource.Source.Path), "/")
	if resourcePath == "" {
		return "", true, fmt.Errorf("GitHub resource %q has no source.path", resource.Name)
	}

	endpoint, err := url.Parse(strings.TrimRight(githubAPIBaseURL, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/commits")
	if err != nil {
		return "", true, fmt.Errorf("invalid GitHub API endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("path", resourcePath)
	query.Set("per_page", "1")
	if ref := strings.TrimSpace(resource.Source.Ref); ref != "" {
		query.Set("sha", ref)
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", true, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", true, fmt.Errorf("GitHub commit lookup failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", true, fmt.Errorf("GitHub commit lookup for %s/%s failed with status %s", owner, repo, resp.Status)
	}

	var commits []githubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return "", true, fmt.Errorf("decode GitHub commit lookup for %s/%s: %w", owner, repo, err)
	}
	if len(commits) == 0 || !isGitObjectID(commits[0].SHA) {
		return "", true, fmt.Errorf("GitHub commit lookup for %s/%s returned no valid path commit", owner, repo)
	}
	return strings.ToLower(commits[0].SHA), true, nil
}

func githubRepository(rawURL string) (owner, repo string, ok bool) {
	rawURL = strings.TrimSpace(rawURL)
	if at := strings.Index(rawURL, "@github.com:"); at >= 0 {
		return githubRepositoryPath(rawURL[at+len("@github.com:"):])
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", false
	}
	return githubRepositoryPath(parsed.Path)
}

func githubRepositoryPath(rawPath string) (owner, repo string, ok bool) {
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	repo = strings.TrimSuffix(parts[1], ".git")
	if repo == "" {
		return "", "", false
	}
	return parts[0], repo, true
}

func isGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}
