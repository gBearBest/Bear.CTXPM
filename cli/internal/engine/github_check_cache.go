package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

// A scope belongs to one detection operation. Repository heads are resolved
// once, then every path lookup is pinned to that immutable snapshot.
type githubCheckScopeKey struct{}

type githubCheckRecord struct {
	Head  string            `json:"head"`
	Paths map[string]string `json:"paths"`
}
type githubCheckCache struct {
	SchemaVersion int                          `json:"schema_version"`
	Groups        map[string]githubCheckRecord `json:"groups"`
}
type githubCheckPending struct {
	done    chan struct{}
	version string
	err     error
}
type githubCheckScope struct {
	root     string
	mu       sync.Mutex
	eligible map[string]bool
	groups   map[string]githubCheckRecord
	heads    map[string]*githubCheckPending
	paths    map[string]*githubCheckPending
	dirty    bool
}

func githubCheckGroupKey(owner, repo, ref string) string {
	// Credentials are deliberately never written to disk. Separate cache keys
	// for different tokens prevent a result from crossing permission contexts.
	sum := sha256.Sum256([]byte(strings.Join([]string{githubAPIBaseURL, strings.ToLower(owner), strings.ToLower(repo), ref, strings.TrimSpace(os.Getenv("GITHUB_TOKEN")), strings.TrimSpace(os.Getenv("GH_TOKEN"))}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func withGitHubCheckScope(ctx context.Context, root string, deps []manifest.Resource) (context.Context, *githubCheckScope) {
	counts := map[string]int{}
	for _, dep := range deps {
		if dep.Name == "ctxpm" || dep.Source == nil || dep.Source.NormalizedType() != "git" {
			continue
		}
		owner, repo, ok := githubRepository(dep.Source.URL)
		if !ok {
			continue
		}
		counts[githubCheckGroupKey(owner, repo, strings.TrimSpace(dep.Source.Ref))]++
	}
	scope := &githubCheckScope{root: root, eligible: map[string]bool{}, groups: map[string]githubCheckRecord{}, heads: map[string]*githubCheckPending{}, paths: map[string]*githubCheckPending{}}
	for key, count := range counts {
		if count > 1 {
			scope.eligible[key] = true
		}
	}
	data, err := os.ReadFile(filepath.Join(root, ".ctxpm", "state", "github-path-checks.json"))
	if err == nil {
		var cache githubCheckCache
		if json.Unmarshal(data, &cache) == nil && cache.SchemaVersion == 1 && cache.Groups != nil {
			scope.groups = cache.Groups
		}
	}
	return context.WithValue(ctx, githubCheckScopeKey{}, scope), scope
}

func (s *githubCheckScope) resolve(ctx context.Context, owner, repo, ref, path string) (string, bool, error) {
	key := githubCheckGroupKey(owner, repo, ref)
	if !s.eligible[key] {
		return "", false, nil
	}
	head, err := s.once(ctx, s.heads, key, func() (string, error) { return fetchGitHubCommit(ctx, owner, repo, "", ref) })
	if err != nil {
		return "", true, err
	}
	pathKey := key + "\x00" + head + "\x00" + path
	version, err := s.once(ctx, s.paths, pathKey, func() (string, error) {
		s.mu.Lock()
		record := s.groups[key]
		if record.Head == head && isGitObjectID(record.Paths[path]) {
			version := record.Paths[path]
			s.mu.Unlock()
			return version, nil
		}
		s.mu.Unlock()
		version, err := fetchGitHubCommit(ctx, owner, repo, path, head)
		if err != nil {
			return "", err
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		record = s.groups[key]
		if record.Head != head {
			record = githubCheckRecord{Head: head, Paths: map[string]string{}}
		}
		if record.Paths == nil {
			record.Paths = map[string]string{}
		}
		record.Paths[path] = version
		s.groups[key] = record
		s.dirty = true
		return version, nil
	})
	return version, true, err
}

func (s *githubCheckScope) once(ctx context.Context, pending map[string]*githubCheckPending, key string, fn func() (string, error)) (string, error) {
	s.mu.Lock()
	p, ok := pending[key]
	if !ok {
		p = &githubCheckPending{done: make(chan struct{})}
		pending[key] = p
	}
	s.mu.Unlock()
	if !ok {
		p.version, p.err = fn()
		close(p.done)
	} else {
		select {
		case <-p.done:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return p.version, p.err
}

func (s *githubCheckScope) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty {
		return nil
	}
	dir := filepath.Join(s.root, ".ctxpm", "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(githubCheckCache{SchemaVersion: 1, Groups: s.groups})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "github-path-checks-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, "github-path-checks.json"))
}
