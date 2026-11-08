package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const gitCacheLockWait = 30 * time.Second

type gitFetchScopeKey struct{}

type gitFetchScope struct {
	mu      sync.Mutex
	fetched map[string]bool
}

func withGitFetchScope(ctx context.Context) context.Context {
	if ctx.Value(gitFetchScopeKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, gitFetchScopeKey{}, &gitFetchScope{fetched: make(map[string]bool)})
}

func (a *App) gitCacheRepository(ctx context.Context, sourceURL, targetVersion string) (string, func(), error) {
	sum := sha256.Sum256([]byte(sourceURL))
	cacheKey := hex.EncodeToString(sum[:])
	// Isolate the cache from older ctxpm versions, which use a lock-file
	// protocol that can delete a live lock after 30 seconds.
	cacheRoot := filepath.Join(a.Root, ".ctxpm", "cache", "git", "v2", cacheKey)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", nil, err
	}
	release, err := acquireCacheLock(ctx, filepath.Join(a.Root, ".ctxpm", "cache", "locks", "v2", cacheKey+".lock"))
	if err != nil {
		return "", nil, err
	}
	cacheRepo := filepath.Join(cacheRoot, "repository")
	scope, _ := ctx.Value(gitFetchScopeKey{}).(*gitFetchScope)
	alreadyFetched := false
	if scope != nil {
		scope.mu.Lock()
		alreadyFetched = scope.fetched[cacheKey]
		scope.mu.Unlock()
	}
	if alreadyFetched && targetVersion != "" {
		if _, err := runGit(ctx, "-C", cacheRepo, "cat-file", "-e", targetVersion+"^{commit}"); err != nil {
			alreadyFetched = false
		}
	}
	if _, err := os.Stat(filepath.Join(cacheRepo, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := runGit(ctx, "clone", "--filter=blob:none", "--no-checkout", "--quiet", sourceURL, cacheRepo); err != nil {
			release()
			return "", nil, err
		}
	} else if err != nil {
		release()
		return "", nil, err
	} else if !alreadyFetched {
		if _, err := runGit(ctx, "-C", cacheRepo, "fetch", "--quiet", "--filter=blob:none", "origin"); err != nil {
			release()
			return "", nil, err
		}
	}
	if scope != nil {
		scope.mu.Lock()
		scope.fetched[cacheKey] = true
		scope.mu.Unlock()
	}
	return cacheRepo, release, nil
}

func acquireCacheLock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// Keep the lock file in place after unlocking. Removing an advisory lock's
	// inode allows another process to lock a different inode at the same path.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(gitCacheLockWait)
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		locked, err := tryLockCacheFile(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if locked {
			var once sync.Once
			return func() { once.Do(func() { _ = unlockCacheFile(file); _ = file.Close() }) }, nil
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, errCacheLockTimeout
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

var errCacheLockTimeout = errors.New("timed out waiting for ctxpm git cache lock")
