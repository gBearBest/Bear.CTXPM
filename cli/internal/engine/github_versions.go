package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

const githubAPIVersion = "2022-11-28"

var githubAPIBaseURL = "https://api.github.com"

// Leave time within the caller's deadline for the Git fallback.
var githubAPILookupTimeout = 10 * time.Second

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
	if scope, ok := ctx.Value(githubCheckScopeKey{}).(*githubCheckScope); ok {
		if version, handled, err := scope.resolve(ctx, owner, repo, strings.TrimSpace(resource.Source.Ref), resourcePath); handled {
			return version, true, err
		}
	}
	version, err := fetchGitHubCommit(ctx, owner, repo, resourcePath, strings.TrimSpace(resource.Source.Ref))
	return version, true, err
}

func fetchGitHubCommit(ctx context.Context, owner, repo, resourcePath, ref string) (string, error) {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	version, status, err := fetchGitHubCommitWithToken(ctx, owner, repo, resourcePath, ref, token)
	if err == nil || ctx.Err() != nil || (status != 401 && status != 403 && status != 404 && status != 429) {
		return version, err
	}
	// GH_TOKEN may be used by GitHub CLI independently of GITHUB_TOKEN.
	// Never include credential values or gh command output in errors.
	tryToken := func(candidate string) (string, bool) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == token {
			return "", false
		}
		result, _, lookupErr := fetchGitHubCommitWithToken(ctx, owner, repo, resourcePath, ref, candidate)
		if lookupErr != nil {
			err = fmt.Errorf("%v; GitHub CLI authenticated lookup failed: %w", err, lookupErr)
			return "", false
		}
		return result, true
	}
	if result, ok := tryToken(os.Getenv("GH_TOKEN")); ok {
		return result, nil
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	out, ghErr := githubCLIToken(ctx)
	if ghErr == nil {
		if result, ok := tryToken(out); ok {
			return result, nil
		}
	}
	return "", err
}

// A variable allows tests to exercise authentication without invoking a host's gh.
var githubCLIToken = func(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", "github.com")
	// These variables override gh's saved login, so omit them when retrieving it.
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GH_TOKEN=") && !strings.HasPrefix(env, "GITHUB_TOKEN=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	out, err := cmd.Output()
	return string(out), err
}

func fetchGitHubCommitWithToken(ctx context.Context, owner, repo, resourcePath, ref, token string) (string, int, error) {
	endpoint, err := url.Parse(strings.TrimRight(githubAPIBaseURL, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/commits")
	if err != nil {
		return "", 0, fmt.Errorf("invalid GitHub API endpoint: %w", err)
	}
	query := endpoint.Query()
	if resourcePath != "" {
		query.Set("path", resourcePath)
	}
	query.Set("per_page", "1")
	if ref != "" {
		query.Set("sha", ref)
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("GitHub commit lookup failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", resp.StatusCode, fmt.Errorf("GitHub commit lookup for %s/%s failed with status %s", owner, repo, resp.Status)
	}

	var commits []githubCommit
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return "", resp.StatusCode, fmt.Errorf("decode GitHub commit lookup for %s/%s: %w", owner, repo, err)
	}
	if len(commits) == 0 || !isGitObjectID(commits[0].SHA) {
		return "", resp.StatusCode, fmt.Errorf("GitHub commit lookup for %s/%s returned no valid commit", owner, repo)
	}
	return strings.ToLower(commits[0].SHA), resp.StatusCode, nil
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

// resolveGitHubVersion prefers the GitHub API, then uses Git's configured
// authentication and complete repository history when the API is unavailable.
func (a *App) resolveGitHubVersion(ctx context.Context, resource manifest.Resource) (string, bool, error) {
	apiCtx, cancel := context.WithTimeout(ctx, githubAPILookupTimeout)
	version, handled, apiErr := resolveGitHubPathVersion(apiCtx, resource)
	cancel()
	if !handled || apiErr == nil || ctx.Err() != nil {
		return version, handled, apiErr
	}
	version, gitErr := a.resolveGitPathVersion(ctx, resource)
	if gitErr != nil {
		return "", true, fmt.Errorf("GitHub API lookup failed: %v; Git fallback failed: %w", apiErr, gitErr)
	}
	return version, true, nil
}

func (a *App) resolveGitPathVersion(ctx context.Context, resource manifest.Resource) (string, error) {
	repo, release, err := a.gitCacheRepository(ctx, resource.Source.URL, "")
	if err != nil {
		return "", err
	}
	defer release()
	ref := strings.TrimSpace(resource.Source.Ref)
	if ref == "" {
		// fetch updates remote branches but does not refresh origin/HEAD when
		// the remote repository changes its default branch.
		if _, err := runGit(ctx, "-C", repo, "remote", "set-head", "origin", "--auto"); err != nil {
			return "", err
		}
		ref = "refs/remotes/origin/HEAD"
	} else if commit, err := runGit(ctx, "-C", repo, "rev-parse", "--verify", "refs/remotes/origin/"+ref+"^{commit}"); err == nil {
		ref = strings.TrimSpace(commit)
	}
	commit, err := runGit(ctx, "-C", repo, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	version, err := runGit(ctx, "-C", repo, "log", "-1", "--format=%H", strings.TrimSpace(commit), "--", resource.Source.Path)
	if err != nil {
		return "", err
	}
	version = strings.TrimSpace(version)
	if !isGitObjectID(version) {
		return "", fmt.Errorf("could not resolve latest git revision for %s", resource.Source.Path)
	}
	return strings.ToLower(version), nil
}
